package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/media"
	"github.com/lutefd/luc/internal/provider"
	luruntime "github.com/lutefd/luc/internal/runtime"
	"github.com/lutefd/luc/internal/tools"
)

func (c *Controller) applyInputTransforms(ctx context.Context, text string, attachments []media.Attachment) (string, bool, string, error) {
	currentText := strings.TrimSpace(text)
	for _, binding := range c.runtime.Extensions.SyncSubscribers(luruntime.ExtensionEventInputTransform) {
		decision, err := c.requestExtensionDecision(ctx, binding, luruntime.ExtensionEventInputTransform, map[string]any{
			"text":        currentText,
			"attachments": summarizeAttachments(attachments),
		})
		if err != nil {
			if extensionFailureClosed(binding) {
				return "", false, "", fmt.Errorf("input blocked because extension %s failed: %w", binding.Host.ID, err)
			}
			continue
		}

		switch strings.TrimSpace(decision.Decision) {
		case "", "continue", "noop":
			continue
		case "transform":
			currentText = strings.TrimSpace(decision.Text)
		case "handled":
			return currentText, true, kernelFirstNonEmpty(decision.Message, decision.Text), nil
		default:
			err := fmt.Errorf("extension %s returned unsupported %s decision %q", binding.Host.ID, luruntime.ExtensionEventInputTransform, decision.Decision)
			c.logger.Ring.Add("warn", err.Error())
			if extensionFailureClosed(binding) {
				return "", false, "", err
			}
		}
	}
	return currentText, false, "", nil
}

func (c *Controller) composeTurnSystemPrompt(ctx context.Context, input string, attachments []media.Attachment) (string, error) {
	base, err := c.composeSystemPrompt(input)
	if err != nil {
		return "", err
	}

	var extraBlocks []string
	for _, binding := range c.runtime.Extensions.SyncSubscribers(luruntime.ExtensionEventPromptContext) {
		decision, err := c.requestExtensionDecision(ctx, binding, luruntime.ExtensionEventPromptContext, map[string]any{
			"input": map[string]any{
				"text":        strings.TrimSpace(input),
				"attachments": summarizeAttachments(attachments),
			},
			"system_prompt": base,
		})
		if err != nil {
			continue
		}

		switch strings.TrimSpace(decision.Decision) {
		case "", "noop", "continue", "system_append", "append", "context", "hidden_context":
			extraBlocks = append(extraBlocks, nonEmptyStrings(decision.SystemAppend)...)
			extraBlocks = append(extraBlocks, nonEmptyStrings(decision.HiddenContext)...)
		default:
			c.logger.Ring.Add("warn", fmt.Sprintf("extension %s returned unsupported %s decision %q", binding.Host.ID, luruntime.ExtensionEventPromptContext, decision.Decision))
		}
	}

	if len(extraBlocks) == 0 {
		return base, nil
	}
	return strings.TrimSpace(base + "\n\n" + strings.Join(extraBlocks, "\n\n")), nil
}

func (c *Controller) prepareToolCall(ctx context.Context, call provider.ToolCall) (provider.ToolCall, error) {
	args, err := parseToolArgs(call.Arguments)
	if err != nil {
		return call, err
	}

	currentArgs := cloneAnyMap(args)
	for _, binding := range c.runtime.Extensions.SyncSubscribers(luruntime.ExtensionEventToolPreflight) {
		decision, err := c.requestExtensionDecision(ctx, binding, luruntime.ExtensionEventToolPreflight, map[string]any{
			"tool_name": call.Name,
			"arguments": currentArgs,
		})
		if err != nil {
			if extensionFailureClosed(binding) {
				return call, fmt.Errorf("tool %s blocked because extension %s failed: %w", call.Name, binding.Host.ID, err)
			}
			continue
		}

		switch strings.TrimSpace(decision.Decision) {
		case "", "allow", "continue", "noop":
			continue
		case "patch":
			currentArgs = cloneAnyMap(decision.Arguments)
			if currentArgs == nil {
				currentArgs = map[string]any{}
			}
		case "block":
			reason := kernelFirstNonEmpty(decision.Message, decision.Error, decision.Text)
			if reason == "" {
				reason = fmt.Sprintf("tool %s blocked by extension %s", call.Name, binding.Host.ID)
			}
			call.Arguments = mustMarshalArgs(currentArgs)
			return call, errors.New(reason)
		default:
			err := fmt.Errorf("extension %s returned unsupported %s decision %q", binding.Host.ID, luruntime.ExtensionEventToolPreflight, decision.Decision)
			c.logger.Ring.Add("warn", err.Error())
			if extensionFailureClosed(binding) {
				return call, err
			}
		}
	}

	call.Arguments = mustMarshalArgs(currentArgs)
	return call, nil
}

func (c *Controller) runPreparedToolCall(ctx context.Context, call provider.ToolCall) (tools.Result, error) {
	switch call.Name {
	case skillToolName:
		return c.runLoadSkillTool(call.Arguments)
	case skillResourceToolName:
		return c.runReadSkillResourceTool(call.Arguments)
	default:
		if err := c.maybeAuthorizeTool(ctx, call.Name, call.Arguments); err != nil {
			return tools.Result{}, err
		}
		return c.tools.Run(ctx, tools.Request{
			Name:             call.Name,
			Arguments:        call.Arguments,
			Workspace:        c.workspace.Root,
			SessionID:        c.session.SessionID,
			AgentID:          "root",
			HostCapabilities: c.HostCapabilities(),
			UIBroker:         c.recordingUIBroker(),
		})
	}
}

func (c *Controller) applyToolResultPatches(ctx context.Context, call provider.ToolCall, payload history.ToolResultPayload) history.ToolResultPayload {
	for _, binding := range c.runtime.Extensions.SyncSubscribers(luruntime.ExtensionEventToolResult) {
		decision, err := c.requestExtensionDecision(ctx, binding, luruntime.ExtensionEventToolResult, map[string]any{
			"tool_name": call.Name,
			"arguments": parseToolArgsOrEmpty(call.Arguments),
			"result": map[string]any{
				"id":       payload.ID,
				"name":     payload.Name,
				"content":  payload.Content,
				"metadata": payload.Metadata,
				"error":    payload.Error,
			},
		})
		if err != nil {
			continue
		}

		switch strings.TrimSpace(decision.Decision) {
		case "", "allow", "continue", "noop":
			continue
		case "patch":
			if strings.TrimSpace(decision.Content) != "" {
				payload.Content = decision.Content
			}
			if strings.TrimSpace(decision.Error) != "" {
				payload.Error = decision.Error
			}
			if len(decision.Metadata) > 0 {
				if payload.Metadata == nil {
					payload.Metadata = map[string]any{}
				}
				for key, value := range decision.Metadata {
					payload.Metadata[key] = value
				}
			}
			if summary := strings.TrimSpace(decision.CollapsedSummary); summary != "" {
				if payload.Metadata == nil {
					payload.Metadata = map[string]any{}
				}
				payload.Metadata[tools.MetadataUICollapsedSummary] = summary
			}
			if classification := strings.TrimSpace(decision.ErrorClassification); classification != "" {
				if payload.Metadata == nil {
					payload.Metadata = map[string]any{}
				}
				payload.Metadata["error_classification"] = classification
			}
		default:
			c.logger.Ring.Add("warn", fmt.Sprintf("extension %s returned unsupported %s decision %q", binding.Host.ID, luruntime.ExtensionEventToolResult, decision.Decision))
		}
	}
	return payload
}

func (c *Controller) requestExtensionDecision(ctx context.Context, binding luruntime.ExtensionBinding, event string, payload any) (luruntime.ExtensionDecisionEnvelope, error) {
	if c.extensionHosts == nil {
		return luruntime.ExtensionDecisionEnvelope{}, errors.New("extension hosts are not available")
	}
	return c.extensionHosts.RequestDecision(ctx, binding, event, payload)
}

func extensionFailureClosed(binding luruntime.ExtensionBinding) bool {
	return strings.EqualFold(strings.TrimSpace(binding.Subscription.FailureMode), luruntime.ExtensionFailureModeClosed)
}

func summarizeAttachments(attachments []media.Attachment) []map[string]any {
	if len(attachments) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(attachments))
	for _, attachment := range attachments {
		out = append(out, map[string]any{
			"id":         attachment.ID,
			"name":       attachment.Name,
			"type":       attachment.Type,
			"media_type": attachment.MediaType,
			"width":      attachment.Width,
			"height":     attachment.Height,
		})
	}
	return out
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func mustMarshalArgs(args map[string]any) string {
	data, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func parseToolArgsOrEmpty(raw string) map[string]any {
	args, err := parseToolArgs(raw)
	if err != nil || args == nil {
		return map[string]any{}
	}
	return args
}

func parseToolArgs(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}
