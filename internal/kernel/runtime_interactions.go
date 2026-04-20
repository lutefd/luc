package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/lutefd/luc/internal/history"
	luruntime "github.com/lutefd/luc/internal/runtime"
)

type recordingBroker struct {
	controller *Controller
	base       luruntime.UIBroker
}

func (c *Controller) recordingUIBroker() luruntime.UIBroker {
	return &recordingBroker{controller: c, base: c.UIBroker()}
}

func (b *recordingBroker) Publish(action luruntime.UIAction) error {
	ensureUIActionID(&action)
	b.controller.emitUIAction(action)
	err := b.base.Publish(action)
	data := map[string]any{}
	if err != nil {
		data["error"] = err.Error()
	}
	b.controller.emitUIResult(luruntime.UIResult{ActionID: action.ID, Accepted: err == nil, Data: data})
	return err
}

func (b *recordingBroker) Request(ctx context.Context, action luruntime.UIAction) (luruntime.UIResult, error) {
	ensureUIActionID(&action)
	b.controller.emitUIAction(action)
	result, err := b.base.Request(ctx, action)
	if err != nil {
		if result.Data == nil {
			result.Data = map[string]any{}
		}
		result.Data["error"] = err.Error()
	}
	b.controller.emitUIResult(result)
	return result, err
}

func ensureUIActionID(action *luruntime.UIAction) {
	if action == nil || strings.TrimSpace(action.ID) != "" {
		return
	}
	action.ID = nextID("ui")
}

func (c *Controller) emitUIAction(action luruntime.UIAction) {
	c.emit("ui.action", history.UIActionPayload{
		ID:        action.ID,
		Kind:      action.Kind,
		Blocking:  action.Blocking,
		Title:     action.Title,
		Body:      action.Body,
		ViewID:    action.ViewID,
		CommandID: action.CommandID,
		Context:   action.Context,
	})
}

func (c *Controller) emitUIResult(result luruntime.UIResult) {
	c.emit("ui.result", history.UIResultPayload{
		ActionID: result.ActionID,
		Accepted: result.Accepted,
		ChoiceID: result.ChoiceID,
		Data:     result.Data,
	})
}

func (c *Controller) maybeAuthorizeTool(ctx context.Context, toolName, rawArguments string) error {
	if !strings.EqualFold(strings.TrimSpace(c.config.UI.ApprovalsMode), "policy") {
		return nil
	}
	policy, ok := c.runtime.UI.ApprovalPolicyForTool(toolName)
	if !ok {
		return nil
	}
	switch strings.TrimSpace(policy.Mode) {
	case "deny":
		return fmt.Errorf("tool %s denied by approval policy %s", toolName, policy.ID)
	case "confirm":
		action, err := c.approvalActionForPolicy(policy, toolName, rawArguments)
		if err != nil {
			return err
		}
		result, err := c.recordingUIBroker().Request(ctx, action)
		if err != nil {
			return err
		}
		if !result.Accepted {
			return fmt.Errorf("tool %s not approved", toolName)
		}
		return nil
	default:
		return nil
	}
}

func (c *Controller) approvalActionForPolicy(policy luruntime.ApprovalPolicy, toolName, rawArguments string) (luruntime.UIAction, error) {
	args := map[string]any{}
	if strings.TrimSpace(rawArguments) != "" {
		if err := json.Unmarshal([]byte(rawArguments), &args); err != nil {
			return luruntime.UIAction{}, err
		}
	}
	body := strings.TrimSpace(policy.BodyTemplate)
	if body == "" {
		body = fmt.Sprintf("%s %s", toolName, rawArguments)
	}
	rendered, err := renderTemplateString(body, map[string]any{
		"tool_name": toolName,
		"arguments": args,
		"raw_args":  rawArguments,
	})
	if err != nil {
		return luruntime.UIAction{}, err
	}
	return luruntime.UIAction{
		ID:       fmt.Sprintf("policy_%s_%d", policy.ID, time.Now().UnixNano()),
		Kind:     "confirm.request",
		Blocking: true,
		Title:    strings.TrimSpace(kernelFirstNonEmpty(policy.Title, "Run "+toolName+"?")),
		Body:     rendered,
		Options: []luruntime.UIOption{
			{ID: "confirm", Label: kernelFirstNonEmpty(policy.ConfirmLabel, "Confirm"), Primary: true},
			{ID: "cancel", Label: kernelFirstNonEmpty(policy.CancelLabel, "Cancel")},
		},
		Context: map[string]any{
			"tool_name": toolName,
			"policy_id": policy.ID,
		},
	}, nil
}

func renderTemplateString(body string, data map[string]any) (string, error) {
	tmpl, err := template.New("policy").Option("missingkey=zero").Parse(body)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func (c *Controller) dispatchHooks(ev history.EventEnvelope) {
	if !c.config.Extensions.HooksEnabled {
		return
	}
	for _, hook := range c.runtime.Hooks.Subscribers(ev.Kind) {
		key := fmt.Sprintf("%s:%s:%d", hook.ID, ev.SessionID, ev.Seq)
		if !c.markHookSeen(key) {
			continue
		}
		go c.runHook(ev, hook)
	}
}

func (c *Controller) markHookSeen(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.hookSeen[key]; ok {
		return false
	}
	c.hookSeen[key] = struct{}{}
	return true
}

func (c *Controller) runHook(ev history.EventEnvelope, hook luruntime.HookSubscription) {
	c.emit("hook.started", history.HookPayload{HookID: hook.ID, EventKind: ev.Kind, SourcePath: hook.SourcePath})

	timeout := time.Duration(hook.Delivery.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "zsh", "-lc", hook.Runtime.Command)
	cmd.Dir = filepath.Dir(hook.SourcePath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		c.failHook(hook, ev.Kind, err)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		c.failHook(hook, ev.Kind, err)
		return
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		c.failHook(hook, ev.Kind, err)
		return
	}

	envelope := luruntime.HookRequestEnvelope{
		Event: ev,
		Workspace: map[string]any{
			"root":       c.workspace.Root,
			"project_id": c.workspace.ProjectID,
			"branch":     c.workspace.Branch,
		},
		Session: map[string]any{
			"session_id": c.session.SessionID,
			"provider":   c.session.Provider,
			"model":      c.session.Model,
		},
		HostCapabilities: c.HostCapabilities(),
	}
	if err := json.NewEncoder(stdin).Encode(envelope); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		c.failHook(hook, ev.Kind, err)
		return
	}
	stdinOpen := luruntime.HasCapability(hook.Runtime.Capabilities, luruntime.CapabilityClientAction)
	if !stdinOpen {
		_ = stdin.Close()
	}
	encoder := json.NewEncoder(stdin)

	decoder := json.NewDecoder(stdout)
	done := false
	for {
		var event luruntime.HookEvent
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, os.ErrClosed) {
				break
			}
			if errors.Is(err, context.DeadlineExceeded) {
				c.failHook(hook, ev.Kind, err)
				return
			}
			if errors.Is(err, io.EOF) {
				break
			}
			c.failHook(hook, ev.Kind, err)
			return
		}
		switch strings.TrimSpace(event.Type) {
		case "log":
			if message := kernelFirstNonEmpty(event.Text, event.Message); message != "" {
				c.logger.Ring.Add("info", fmt.Sprintf("hook %s: %s", hook.ID, message))
			}
		case "progress":
			if progress := kernelFirstNonEmpty(event.Progress, event.Message); progress != "" {
				c.logger.Ring.Add("info", fmt.Sprintf("hook %s: %s", hook.ID, progress))
			}
		case "client_action":
			if !luruntime.HasCapability(hook.Runtime.Capabilities, luruntime.CapabilityClientAction) {
				c.failHook(hook, ev.Kind, errors.New("hook emitted client_action without client_actions capability"))
				return
			}
			if event.Action == nil {
				c.failHook(hook, ev.Kind, errors.New("hook client_action is missing action payload"))
				return
			}
			action := *event.Action
			if strings.TrimSpace(action.ID) == "" {
				action.ID = nextID("hook_action")
			}
			var uiResult luruntime.UIResult
			if action.Blocking {
				uiResult, err = c.recordingUIBroker().Request(ctx, action)
			} else {
				err = c.recordingUIBroker().Publish(action)
				uiResult = luruntime.UIResult{ActionID: action.ID, Accepted: err == nil}
			}
			if err != nil {
				c.failHook(hook, ev.Kind, err)
				return
			}
			if err := encoder.Encode(luruntime.ClientResultEnvelope{Type: "client_result", Result: uiResult}); err != nil {
				c.failHook(hook, ev.Kind, err)
				return
			}
		case "done":
			done = true
		case "error":
			c.failHook(hook, ev.Kind, errors.New(kernelFirstNonEmpty(event.Error, event.Message, "hook failed")))
			return
		default:
			c.failHook(hook, ev.Kind, fmt.Errorf("unsupported hook event type %q", event.Type))
			return
		}
		if done {
			break
		}
	}
	if stdinOpen {
		_ = stdin.Close()
	}

	if err := cmd.Wait(); err != nil {
		if stderrText := strings.TrimSpace(stderr.String()); stderrText != "" {
			err = fmt.Errorf("%w: %s", err, stderrText)
		}
		c.failHook(hook, ev.Kind, err)
		return
	}
	c.emit("hook.finished", history.HookPayload{HookID: hook.ID, EventKind: ev.Kind, SourcePath: hook.SourcePath})
}

func kernelFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (c *Controller) failHook(hook luruntime.HookSubscription, eventKind string, err error) {
	c.logger.Ring.Add("error", fmt.Sprintf("hook %s failed: %v", hook.ID, err))
	c.emit("hook.failed", history.HookPayload{
		HookID:     hook.ID,
		EventKind:  eventKind,
		SourcePath: hook.SourcePath,
		Error:      err.Error(),
	})
}
