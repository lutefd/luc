package kernel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/logging"
	"github.com/lutefd/luc/internal/provider"
	luruntime "github.com/lutefd/luc/internal/runtime"
)

type approvingBroker struct {
	action luruntime.UIAction
}

func (b *approvingBroker) Publish(action luruntime.UIAction) error {
	b.action = action
	return nil
}

func (b *approvingBroker) Request(ctx context.Context, action luruntime.UIAction) (luruntime.UIResult, error) {
	_ = ctx
	b.action = action
	return luruntime.UIResult{ActionID: action.ID, Accepted: true, ChoiceID: "confirm"}, nil
}

func TestControllerAppliesApprovalPoliciesInPolicyMode(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{
			{
				{Type: "tool_call", ToolCall: provider.ToolCall{ID: "call_1", Name: "bash", Arguments: `{"command":"printf hello"}`}},
				{Type: "done", Completed: true},
			},
			{
				{Type: "text_delta", Text: "done"},
				{Type: "done", Completed: true},
			},
		},
	}
	newProvider = func(cfg config.ProviderConfig) (provider.Provider, error) {
		_ = cfg
		return providerStub, nil
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "config.yaml"), "ui:\n  approvals_mode: policy\n")
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "ui", "policy.yaml"), `schema: luc.ui/v1
id: approvals
approval_policies:
  - id: guarded-bash
    tool_names: [bash]
    mode: confirm
    title: Run shell command?
    body_template: "{{ index .arguments \"command\" }}"
    confirm_label: Run
    cancel_label: Cancel
`)

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	broker := &approvingBroker{}
	controller.SetUIBroker(broker)

	if err := controller.Submit(context.Background(), "run it"); err != nil {
		t.Fatal(err)
	}
	if broker.action.Kind != "confirm.request" || !strings.Contains(broker.action.Body, "printf hello") {
		t.Fatalf("expected approval dialog for bash, got %#v", broker.action)
	}
}

func TestControllerRunRuntimeToolActionUsesToolPipeline(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "config.yaml"), "ui:\n  approvals_mode: policy\n")
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "tools", "review_set_state.yaml"), `name: review_set_state
description: Set review state.
command: printf '%s' '{{ .action }}'
schema:
  type: object
  properties:
    action:
      type: string
`)
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "ui", "policy.yaml"), `schema: luc.ui/v1
id: approvals
approval_policies:
  - id: review-state
    tool_names: [review_set_state]
    mode: confirm
    title: Change review state?
    body_template: "{{ index .arguments \"action\" }}"
`)
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "extensions", "patch.yaml"), `schema: luc.extension/v1
id: review_patch
protocol_version: 1
runtime:
  kind: exec
  command: ./patch.py
subscriptions:
  - event: tool.preflight
    mode: sync
  - event: tool.result
    mode: sync
`)
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "extensions", "patch.py"), `#!/usr/bin/env python3
import json, sys
for line in sys.stdin:
    msg = json.loads(line)
    if msg.get("type") == "hello":
        print(json.dumps({"type":"ready","protocol_version":1}), flush=True)
    elif msg.get("type") == "event" and msg.get("event") == "tool.preflight" and msg.get("payload", {}).get("tool_name") == "review_set_state":
        args = dict(msg.get("payload", {}).get("arguments") or {})
        args["action"] = "approve-patched"
        print(json.dumps({"type":"decision","request_id":msg["request_id"],"decision":"patch","arguments":args}), flush=True)
    elif msg.get("type") == "event" and msg.get("event") == "tool.result" and msg.get("payload", {}).get("tool_name") == "review_set_state":
        print(json.dumps({"type":"decision","request_id":msg["request_id"],"decision":"patch","content":"patched result"}), flush=True)
    elif msg.get("type") == "event":
        print(json.dumps({"type":"decision","request_id":msg["request_id"],"decision":"allow"}), flush=True)
    elif msg.get("type") == "session_shutdown":
        break
`)
	if err := os.Chmod(filepath.Join(root, ".luc", "extensions", "patch.py"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	broker := &approvingBroker{}
	controller.SetUIBroker(broker)

	result, err := controller.RunRuntimeToolAction(context.Background(), "review_set_state", map[string]any{"action": "approve"})
	if err != nil {
		t.Fatal(err)
	}
	if broker.action.Kind != "confirm.request" || !strings.Contains(broker.action.Body, "approve-patched") {
		t.Fatalf("expected approval after preflight patch, got %#v", broker.action)
	}
	if result.Content != "patched result" {
		t.Fatalf("expected patched tool result, got %#v", result)
	}
	var finished history.ToolResultPayload
	for _, ev := range controller.SessionEvents() {
		if ev.Kind == "tool.finished" {
			finished = history.DecodePayload[history.ToolResultPayload](ev.Payload)
		}
	}
	if finished.Content != "patched result" {
		t.Fatalf("expected patched emitted tool result, got %#v", finished)
	}
}

func TestControllerRunsHooksOnlyForLiveEvents(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{{
			{Type: "text_delta", Text: "final"},
			{Type: "done", Completed: true},
		}},
	}
	newProvider = func(cfg config.ProviderConfig) (provider.Provider, error) {
		_ = cfg
		return providerStub, nil
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	hookOutput := filepath.Join(root, "hook-output.json")
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "hooks", "slack.yaml"), `schema: luc.hook/v1
id: slack_notify
description: Send a ping.
events:
  - message.assistant.final
runtime:
  kind: exec
  command: ./notify.sh
  capabilities:
    - structured_io
delivery:
  mode: async
  timeout_seconds: 5
`)
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "hooks", "notify.sh"), "#!/bin/sh\nIFS= read -r request\nprintf '%s' \"$request\" > \""+hookOutput+"\"\nprintf '%s\\n' '{\"type\":\"done\",\"done\":true}'\n")
	if err := os.Chmod(filepath.Join(root, ".luc", "hooks", "notify.sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}

	waitForFile(t, hookOutput)

	reopened, err := Open(context.Background(), root, controller.Session().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(hookOutput)
	if err != nil {
		t.Fatal(err)
	}
	before := string(data)
	time.Sleep(200 * time.Millisecond)
	data, err = os.ReadFile(hookOutput)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != before {
		t.Fatalf("expected hook output to stay unchanged on replay/open, got %q -> %q", before, string(data))
	}
	events := reopened.InitialEvents()
	foundHook := false
	for _, ev := range events {
		if ev.Kind == "hook.started" || ev.Kind == "hook.finished" {
			foundHook = true
			break
		}
	}
	if !foundHook {
		t.Fatalf("expected live hook events to be persisted, got %#v", events)
	}
}

func TestControllerHookStructuredIOAcceptsMessageAlias(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{{
			{Type: "text_delta", Text: "final"},
			{Type: "done", Completed: true},
		}},
	}
	newProvider = func(cfg config.ProviderConfig) (provider.Provider, error) {
		_ = cfg
		return providerStub, nil
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "hooks", "slack.yaml"), `schema: luc.hook/v1
id: slack_notify
description: Send a ping.
events:
  - message.assistant.final
runtime:
  kind: exec
  command: ./notify.sh
  capabilities:
    - structured_io
delivery:
  mode: async
  timeout_seconds: 5
`)
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "hooks", "notify.sh"), "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"log\",\"message\":\"alias log\"}'\nprintf '%s\\n' '{\"type\":\"error\",\"message\":\"alias error\"}'\n")
	if err := os.Chmod(filepath.Join(root, ".luc", "hooks", "notify.sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}

	waitForHookLog(t, controller, "hook slack_notify failed: alias error")

	entries := controller.LogEntries()
	if !logEntriesContain(entries, "hook slack_notify: alias log") {
		t.Fatalf("expected hook log alias to be recorded, got %#v", entries)
	}
	if !logEntriesContain(entries, "hook slack_notify failed: alias error") {
		t.Fatalf("expected hook error alias to be recorded, got %#v", entries)
	}

	stored, err := controller.store.Load(controller.Session().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	foundFailure := false
	for _, ev := range stored {
		if ev.Kind != "hook.failed" {
			continue
		}
		payload := decode[history.HookPayload](ev.Payload)
		if payload.HookID == "slack_notify" && payload.Error == "alias error" {
			foundFailure = true
			break
		}
	}
	if !foundFailure {
		t.Fatalf("expected hook.failed to persist compatibility alias error, got %#v", stored)
	}
}

func TestControllerHookClientActionPublishesViewRefresh(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{{
			{Type: "text_delta", Text: "final"},
			{Type: "done", Completed: true},
		}},
	}
	newProvider = func(cfg config.ProviderConfig) (provider.Provider, error) {
		_ = cfg
		return providerStub, nil
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "hooks", "refresh.yaml"), `schema: luc.hook/v1
id: refresh_view
description: Refresh the cowboy tab.
events:
  - message.assistant.final
runtime:
  kind: exec
  command: ./refresh.py
  capabilities:
    - structured_io
    - client_actions
delivery:
  mode: async
  timeout_seconds: 5
`)
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "hooks", "refresh.py"), "#!/usr/bin/env python3\nimport json, sys\n_ = sys.stdin.readline()\nsys.stdout.write(json.dumps({\"type\":\"client_action\",\"action\":{\"id\":\"refresh_1\",\"kind\":\"view.refresh\",\"view_id\":\"cowboy\"}})+\"\\n\")\nsys.stdout.flush()\n_ = sys.stdin.readline()\nsys.stdout.write('{\"type\":\"done\",\"done\":true}\\n')\nsys.stdout.flush()\n")
	if err := os.Chmod(filepath.Join(root, ".luc", "hooks", "refresh.py"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	broker := &approvingBroker{}
	controller.SetUIBroker(broker)

	if err := controller.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}

	waitForHookAction(t, broker, "view.refresh", "cowboy")
	if broker.action.Kind != "view.refresh" || broker.action.ViewID != "cowboy" {
		t.Fatalf("expected hook to publish view.refresh action, got %#v", broker.action)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func waitForHookLog(t *testing.T, controller *Controller, needle string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if logEntriesContain(controller.LogEntries(), needle) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for log entry %q", needle)
}

func waitForHookAction(t *testing.T, broker *approvingBroker, kind, viewID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if broker.action.Kind == kind && broker.action.ViewID == viewID {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for action %q on view %q", kind, viewID)
}

func logEntriesContain(entries []logging.Entry, needle string) bool {
	for _, entry := range entries {
		if strings.Contains(entry.Message, needle) {
			return true
		}
	}
	return false
}

func mustWriteKernelFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
