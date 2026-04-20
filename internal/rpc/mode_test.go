package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/kernel"
)

const testImageBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+nmZ0AAAAASUVORK5CYII="

type workspaceOptions struct {
	withSecondProvider bool
	withRuntimeView    bool
	withHook           bool
}

type rpcFrame struct {
	Response *Response
	Event    *EventFrame
}

type rpcHarness struct {
	t      *testing.T
	input  *io.PipeWriter
	frames chan rpcFrame
	done   chan error
}

func TestRPCGetStatePromptAndImageAttachment(t *testing.T) {
	root := setupRPCWorkspace(t, workspaceOptions{})
	controller := newRPCController(t, root)
	h := newRPCHarness(t, controller)
	defer h.Close()

	h.Send(Command{ID: "state-1", Type: "get_state"})
	state := decodeResponseData[StateResponseData](t, h.WaitResponse("state-1"))
	if state.ProtocolVersion != ProtocolVersion {
		t.Fatalf("unexpected protocol version %#v", state)
	}
	if state.Provider.Kind != "alpha" || state.Provider.Model != "alpha-model" {
		t.Fatalf("unexpected provider state %#v", state.Provider)
	}

	h.Send(Command{ID: "prompt-1", Type: "prompt", Message: "hello"})
	if resp := h.WaitResponse("prompt-1"); !resp.Success {
		t.Fatalf("expected prompt success, got %#v", resp)
	}
	user := h.WaitEvent(t, func(ev history.EventEnvelope) bool { return ev.Kind == "message.user" })
	if payload := history.DecodePayload[history.MessagePayload](user.Payload); payload.Content != "hello" {
		t.Fatalf("unexpected user payload %#v", payload)
	}
	final := h.WaitEvent(t, func(ev history.EventEnvelope) bool { return ev.Kind == "message.assistant.final" })
	if payload := history.DecodePayload[history.MessagePayload](final.Payload); payload.Content != "echo:hello" {
		t.Fatalf("unexpected assistant payload %#v", payload)
	}

	h.Send(Command{
		ID:      "prompt-2",
		Type:    "prompt",
		Message: "",
		Attachments: []AttachmentInput{{
			Type:      "image",
			Name:      "pixel.png",
			MediaType: "image/png",
			Data:      testImageBase64,
		}},
	})
	if resp := h.WaitResponse("prompt-2"); !resp.Success {
		t.Fatalf("expected image prompt success, got %#v", resp)
	}
	imageFinal := h.WaitEvent(t, func(ev history.EventEnvelope) bool {
		if ev.Kind != "message.assistant.final" {
			return false
		}
		payload := history.DecodePayload[history.MessagePayload](ev.Payload)
		return payload.Content == "saw image"
	})
	if payload := history.DecodePayload[history.MessagePayload](imageFinal.Payload); payload.Content != "saw image" {
		t.Fatalf("unexpected image final payload %#v", payload)
	}
}

func TestRPCBusyAndAbort(t *testing.T) {
	root := setupRPCWorkspace(t, workspaceOptions{})
	controller := newRPCController(t, root)
	h := newRPCHarness(t, controller)
	defer h.Close()

	h.Send(Command{ID: "prompt-wait", Type: "prompt", Message: "wait"})
	if resp := h.WaitResponse("prompt-wait"); !resp.Success {
		t.Fatalf("expected slow prompt acceptance, got %#v", resp)
	}

	h.Send(Command{ID: "prompt-busy", Type: "prompt", Message: "second"})
	resp := h.WaitResponse("prompt-busy")
	if resp.Success || !strings.Contains(resp.Error, "busy") {
		t.Fatalf("expected busy rejection, got %#v", resp)
	}

	h.Send(Command{ID: "abort-1", Type: "abort"})
	if resp := h.WaitResponse("abort-1"); !resp.Success {
		t.Fatalf("expected abort success, got %#v", resp)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.Send(Command{ID: "state-after-abort", Type: "get_state"})
		state := decodeResponseData[StateResponseData](t, h.WaitResponse("state-after-abort"))
		if !state.TurnActive {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("expected turn to stop after abort")
}

func TestRPCSessionSwitchAndReplay(t *testing.T) {
	root := setupRPCWorkspace(t, workspaceOptions{})
	controller := newRPCController(t, root)
	h := newRPCHarness(t, controller)

	h.Send(Command{ID: "prompt-save", Type: "prompt", Message: "saved"})
	if resp := h.WaitResponse("prompt-save"); !resp.Success {
		t.Fatalf("expected save prompt success, got %#v", resp)
	}
	h.WaitEvent(t, func(ev history.EventEnvelope) bool { return ev.Kind == "message.assistant.final" })
	firstSessionID := controller.Session().SessionID
	h.Close()

	controller2 := newRPCController(t, root)
	h2 := newRPCHarness(t, controller2)
	defer h2.Close()

	h2.Send(Command{ID: "new-1", Type: "new_session"})
	newSession := decodeResponseData[SessionSwitchResponseData](t, h2.WaitResponse("new-1"))
	if newSession.State.Session.Meta.SessionID == firstSessionID {
		t.Fatalf("expected new session id, got %#v", newSession.State)
	}
	if len(newSession.Events) != 0 {
		t.Fatalf("expected fresh session replay to be empty, got %#v", newSession.Events)
	}

	h2.Send(Command{ID: "open-1", Type: "open_session", SessionID: firstSessionID})
	openSession := decodeResponseData[SessionSwitchResponseData](t, h2.WaitResponse("open-1"))
	if openSession.State.Session.Meta.SessionID != firstSessionID {
		t.Fatalf("expected reopened session %q, got %#v", firstSessionID, openSession.State)
	}
	if !eventsContainKind(openSession.Events, "message.user") || !eventsContainKind(openSession.Events, "message.assistant.final") {
		t.Fatalf("expected replay events in %#v", openSession.Events)
	}
}

func TestRPCGetEventsVisibleVsRawAfterCompaction(t *testing.T) {
	root := setupRPCWorkspace(t, workspaceOptions{})
	mustWriteFile(t, filepath.Join(root, ".luc", "config.yaml"), `provider:
  kind: alpha
  model: alpha-model
ui:
  approvals_mode: trusted
compaction:
  enabled: true
  keep_recent_tokens: 1
`)
	controller := newRPCController(t, root)
	h := newRPCHarness(t, controller)
	defer h.Close()

	h.Send(Command{ID: "prompt-a", Type: "prompt", Message: "first"})
	if resp := h.WaitResponse("prompt-a"); !resp.Success {
		t.Fatalf("expected prompt-a success, got %#v", resp)
	}
	h.WaitEvent(t, func(ev history.EventEnvelope) bool { return ev.Kind == "message.assistant.final" })

	h.Send(Command{ID: "prompt-b", Type: "prompt", Message: "second"})
	if resp := h.WaitResponse("prompt-b"); !resp.Success {
		t.Fatalf("expected prompt-b success, got %#v", resp)
	}
	h.WaitEvent(t, func(ev history.EventEnvelope) bool { return ev.Kind == "message.assistant.final" })

	h.Send(Command{ID: "compact-1", Type: "compact", Instructions: "keep summary short"})
	if resp := h.WaitResponse("compact-1"); !resp.Success {
		t.Fatalf("expected compact success, got %#v", resp)
	}

	h.Send(Command{ID: "events-visible", Type: "get_events"})
	visible := decodeResponseData[EventsResponseData](t, h.WaitResponse("events-visible"))
	h.Send(Command{ID: "events-raw", Type: "get_events", Scope: "raw"})
	raw := decodeResponseData[EventsResponseData](t, h.WaitResponse("events-raw"))

	if len(raw.Events) <= len(visible.Events) {
		t.Fatalf("expected raw events to exceed visible events, raw=%d visible=%d", len(raw.Events), len(visible.Events))
	}
	if !eventsContainKind(raw.Events, "session.compaction") || !eventsContainUserText(raw.Events, "first") {
		t.Fatalf("expected raw events to keep compaction and original prompt, got %#v", raw.Events)
	}
	if !eventsContainKind(visible.Events, "session.compaction") {
		t.Fatalf("expected visible events to include compaction, got %#v", visible.Events)
	}
}

func TestRPCSetModelReloadAndRuntimeUI(t *testing.T) {
	root := setupRPCWorkspace(t, workspaceOptions{withSecondProvider: true, withRuntimeView: true})
	controller := newRPCController(t, root)
	h := newRPCHarness(t, controller)
	defer h.Close()

	h.Send(Command{ID: "models-1", Type: "list_models"})
	modelsResp := h.WaitResponse("models-1")
	if !modelsResp.Success {
		t.Fatalf("expected list_models success, got %#v", modelsResp)
	}
	models := decodeResponseData[struct {
		Models []map[string]any `json:"models"`
	}](t, modelsResp)
	if !modelsContain(models.Models, "alpha-model") || !modelsContain(models.Models, "alpha-reasoner") || !modelsContain(models.Models, "beta-model") {
		t.Fatalf("expected runtime models in %#v", models.Models)
	}

	h.Send(Command{ID: "set-same", Type: "set_model", ModelID: "alpha-reasoner"})
	sameProvider := decodeResponseData[StateResponseData](t, h.WaitResponse("set-same"))
	if sameProvider.Provider.Kind != "alpha" || sameProvider.Provider.Model != "alpha-reasoner" {
		t.Fatalf("unexpected same-provider state %#v", sameProvider.Provider)
	}

	h.Send(Command{ID: "set-cross", Type: "set_model", ModelID: "beta-model"})
	crossProvider := decodeResponseData[StateResponseData](t, h.WaitResponse("set-cross"))
	if crossProvider.Provider.Kind != "beta" || crossProvider.Provider.Model != "beta-model" {
		t.Fatalf("unexpected cross-provider state %#v", crossProvider.Provider)
	}

	mustWriteFile(t, filepath.Join(root, ".luc", "ui", "missing.yaml"), `schema: luc.ui/v1
id: missing
requires_host_capabilities:
  - ui.never
`)

	h.Send(Command{ID: "reload-1", Type: "reload"})
	if resp := h.WaitResponse("reload-1"); !resp.Success {
		t.Fatalf("expected reload success, got %#v", resp)
	}

	h.Send(Command{ID: "ui-1", Type: "get_runtime_ui"})
	ui := decodeResponseData[RuntimeUIResponseData](t, h.WaitResponse("ui-1"))
	if len(ui.Commands) == 0 || len(ui.Views) == 0 {
		t.Fatalf("expected runtime ui contributions, got %#v", ui)
	}
	if len(ui.Diagnostics) == 0 {
		t.Fatalf("expected runtime diagnostics after reload, got %#v", ui.Diagnostics)
	}
}

func TestRPCRenderViewAndToolUIBridge(t *testing.T) {
	root := setupRPCWorkspace(t, workspaceOptions{withRuntimeView: true})
	controller := newRPCController(t, root)
	h := newRPCHarness(t, controller)
	defer h.Close()

	h.Send(Command{ID: "render-1", Type: "render_view", ViewID: "provider.status"})
	action := h.WaitEvent(t, func(ev history.EventEnvelope) bool {
		if ev.Kind != "ui.action" {
			return false
		}
		payload := history.DecodePayload[history.UIActionPayload](ev.Payload)
		return payload.ID == "tool_confirm_1"
	})
	actionPayload := history.DecodePayload[history.UIActionPayload](action.Payload)
	if actionPayload.Kind != "confirm.request" {
		t.Fatalf("unexpected tool ui action %#v", actionPayload)
	}

	h.Send(Command{ID: "ui-tool", Type: "ui_response", ActionID: "tool_confirm_1", Accepted: true, ChoiceID: "approve"})
	if resp := h.WaitResponse("ui-tool"); !resp.Success {
		t.Fatalf("expected ui_response success, got %#v", resp)
	}
	resultEvent := h.WaitEvent(t, func(ev history.EventEnvelope) bool {
		if ev.Kind != "ui.result" {
			return false
		}
		payload := history.DecodePayload[history.UIResultPayload](ev.Payload)
		return payload.ActionID == "tool_confirm_1"
	})
	if payload := history.DecodePayload[history.UIResultPayload](resultEvent.Payload); !payload.Accepted {
		t.Fatalf("expected accepted tool ui result, got %#v", payload)
	}

	render := decodeResponseData[RenderViewResponseData](t, h.WaitResponse("render-1"))
	if !strings.Contains(render.Result.Content, `"status": "ok"`) {
		t.Fatalf("expected raw result content, got %#v", render.Result)
	}
	if !strings.Contains(render.RenderedText, `"approved": true`) {
		t.Fatalf("expected rendered json view, got %q", render.RenderedText)
	}
}

func TestRPCProviderAndHookUIBridge(t *testing.T) {
	root := setupRPCWorkspace(t, workspaceOptions{withHook: true})
	controller := newRPCController(t, root)
	h := newRPCHarness(t, controller)
	defer h.Close()

	h.Send(Command{ID: "provider-ui", Type: "prompt", Message: "provider ui"})
	if resp := h.WaitResponse("provider-ui"); !resp.Success {
		t.Fatalf("expected provider prompt success, got %#v", resp)
	}
	providerAction := h.WaitEvent(t, func(ev history.EventEnvelope) bool {
		if ev.Kind != "ui.action" {
			return false
		}
		payload := history.DecodePayload[history.UIActionPayload](ev.Payload)
		return payload.ID == "confirm_provider_1"
	})
	if payload := history.DecodePayload[history.UIActionPayload](providerAction.Payload); payload.Kind != "confirm.request" {
		t.Fatalf("unexpected provider action %#v", payload)
	}

	h.Send(Command{ID: "provider-ui-response", Type: "ui_response", ActionID: "confirm_provider_1", Accepted: true, ChoiceID: "approve"})
	if resp := h.WaitResponse("provider-ui-response"); !resp.Success {
		t.Fatalf("expected provider ui_response success, got %#v", resp)
	}
	h.WaitEvent(t, func(ev history.EventEnvelope) bool {
		if ev.Kind != "ui.result" {
			return false
		}
		payload := history.DecodePayload[history.UIResultPayload](ev.Payload)
		return payload.ActionID == "confirm_provider_1"
	})
	providerFinal := h.WaitEvent(t, func(ev history.EventEnvelope) bool {
		if ev.Kind != "message.assistant.final" {
			return false
		}
		payload := history.DecodePayload[history.MessagePayload](ev.Payload)
		return payload.Content == "provider-ui:true"
	})
	if payload := history.DecodePayload[history.MessagePayload](providerFinal.Payload); payload.Content != "provider-ui:true" {
		t.Fatalf("unexpected provider final %#v", payload)
	}

	hookAction := h.WaitEvent(t, func(ev history.EventEnvelope) bool {
		if ev.Kind != "ui.action" {
			return false
		}
		payload := history.DecodePayload[history.UIActionPayload](ev.Payload)
		return payload.ID == "hook_confirm_1"
	})
	if payload := history.DecodePayload[history.UIActionPayload](hookAction.Payload); payload.Kind != "confirm.request" {
		t.Fatalf("unexpected hook action %#v", payload)
	}
	h.Send(Command{ID: "hook-ui-response", Type: "ui_response", ActionID: "hook_confirm_1", Accepted: true, ChoiceID: "approve"})
	if resp := h.WaitResponse("hook-ui-response"); !resp.Success {
		t.Fatalf("expected hook ui_response success, got %#v", resp)
	}
	hookResult := h.WaitEvent(t, func(ev history.EventEnvelope) bool {
		if ev.Kind != "ui.result" {
			return false
		}
		payload := history.DecodePayload[history.UIResultPayload](ev.Payload)
		return payload.ActionID == "hook_confirm_1"
	})
	if payload := history.DecodePayload[history.UIResultPayload](hookResult.Payload); !payload.Accepted {
		t.Fatalf("unexpected hook result %#v", payload)
	}
}

func TestRPCParseAndUIResponseErrors(t *testing.T) {
	root := setupRPCWorkspace(t, workspaceOptions{})
	controller := newRPCController(t, root)
	h := newRPCHarness(t, controller)
	defer h.Close()

	h.SendRaw(`{"type":`)
	parseResp := h.WaitResponse("")
	if parseResp.Success || parseResp.Command != "parse" {
		t.Fatalf("expected parse error, got %#v", parseResp)
	}

	h.Send(Command{ID: "unknown-1", Type: "mystery"})
	unknown := h.WaitResponse("unknown-1")
	if unknown.Success || !strings.Contains(unknown.Error, "unknown command") {
		t.Fatalf("expected unknown command error, got %#v", unknown)
	}

	h.Send(Command{ID: "ui-miss", Type: "ui_response", ActionID: "missing"})
	missing := h.WaitResponse("ui-miss")
	if missing.Success || !strings.Contains(missing.Error, "no pending ui action") {
		t.Fatalf("expected missing ui action error, got %#v", missing)
	}
}

func setupRPCWorkspace(t *testing.T, opts workspaceOptions) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LUC_STATE_DIR", filepath.Join(home, "state"))

	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example\n")
	mustMkdir(t, filepath.Join(root, ".luc"))
	mustWriteFile(t, filepath.Join(root, ".luc", "config.yaml"), `provider:
  kind: alpha
  model: alpha-model
ui:
  approvals_mode: trusted
`)

	providerDir := filepath.Join(root, ".luc", "providers")
	mustMkdir(t, providerDir)
	mustWriteExecutable(t, filepath.Join(providerDir, "alpha.py"), `#!/usr/bin/env python3
import json, sys, time

payload = json.loads(sys.stdin.readline())
request = payload.get("request", {})
system = request.get("system", "")
messages = request.get("messages", [])

def text_and_image(message):
    parts = message.get("parts") or []
    if parts:
        text = "".join(part.get("text", "") for part in parts if part.get("type", "text") == "text")
        has_image = any(part.get("type") == "image" for part in parts)
        return text, has_image
    return message.get("content", ""), False

last_user = ""
has_image = False
for message in reversed(messages):
    if message.get("role") == "user":
        last_user, has_image = text_and_image(message)
        break

if "context summarization assistant" in system:
    print(json.dumps({"type": "text_delta", "text": "## Goal\ncompact\n\n## Constraints & Preferences\n- (none)\n\n## Progress\n### Done\n- [x] compacted\n\n### In Progress\n- [ ] keep going\n\n### Blocked\n- (none)\n\n## Key Decisions\n- **RPC**: luc-native\n\n## Next Steps\n1. continue\n\n## Critical Context\n- (none)"}), flush=True)
    print(json.dumps({"type": "done", "completed": True}), flush=True)
elif any(message.get("tool_call_id") == "call_1" for message in messages):
    print(json.dumps({"type": "text_delta", "text": "tool finished"}), flush=True)
    print(json.dumps({"type": "done", "completed": True}), flush=True)
elif "tool please" in last_user:
    print(json.dumps({"type": "tool_call", "tool_call": {"id": "call_1", "name": "read", "arguments": "{\"path\":\"go.mod\"}"}}), flush=True)
    print(json.dumps({"type": "done", "completed": True}), flush=True)
elif "wait" in last_user:
    time.sleep(2)
    print(json.dumps({"type": "text_delta", "text": "late"}), flush=True)
    print(json.dumps({"type": "done", "completed": True}), flush=True)
elif "provider ui" in last_user:
    print(json.dumps({"type": "client_action", "action": {"id": "confirm_provider_1", "kind": "confirm.request", "blocking": True, "title": "Provider?", "body": "Approve provider action?", "options": [{"id": "approve", "label": "Approve", "primary": True}]}}), flush=True)
    response = json.loads(sys.stdin.readline())
    accepted = response.get("result", {}).get("accepted", False)
    print(json.dumps({"type": "text_delta", "text": "provider-ui:" + str(accepted).lower()}), flush=True)
    print(json.dumps({"type": "done", "completed": True}), flush=True)
elif has_image:
    print(json.dumps({"type": "text_delta", "text": "saw image"}), flush=True)
    print(json.dumps({"type": "done", "completed": True}), flush=True)
else:
    print(json.dumps({"type": "text_delta", "text": "echo:" + last_user}), flush=True)
    print(json.dumps({"type": "done", "completed": True}), flush=True)
`)
	mustWriteFile(t, filepath.Join(providerDir, "alpha.yaml"), `id: alpha
name: Alpha Provider
type: exec
command: ./alpha.py
capabilities:
  - client_actions
models:
  - id: alpha-model
    name: Alpha Model
  - id: alpha-reasoner
    name: Alpha Reasoner
`)

	if opts.withSecondProvider {
		mustWriteExecutable(t, filepath.Join(providerDir, "beta.py"), `#!/usr/bin/env python3
import json, sys
payload = json.loads(sys.stdin.read())
messages = payload.get("request", {}).get("messages", [])
last = ""
for message in reversed(messages):
    if message.get("role") == "user":
        last = message.get("content", "")
        break
print(json.dumps({"type": "text_delta", "text": "beta:" + last}), flush=True)
print(json.dumps({"type": "done", "completed": True}), flush=True)
`)
		mustWriteFile(t, filepath.Join(providerDir, "beta.yaml"), `id: beta
name: Beta Provider
type: exec
command: ./beta.py
models:
  - id: beta-model
    name: Beta Model
`)
	}

	if opts.withRuntimeView {
		toolDir := filepath.Join(root, ".luc", "tools")
		mustMkdir(t, toolDir)
		mustWriteExecutable(t, filepath.Join(toolDir, "provider_status.py"), `#!/usr/bin/env python3
import json, sys
_ = json.loads(sys.stdin.readline())
print(json.dumps({"type": "client_action", "action": {"id": "tool_confirm_1", "kind": "confirm.request", "blocking": True, "title": "Run tool?", "body": "Approve tool action?", "options": [{"id": "approve", "label": "Approve", "primary": True}]}}), flush=True)
response = json.loads(sys.stdin.readline())
approved = response.get("result", {}).get("accepted", False)
print(json.dumps({"type": "result", "result": {"content": json.dumps({"status": "ok", "approved": approved})}}), flush=True)
print(json.dumps({"type": "done", "done": True}), flush=True)
`)
		mustWriteFile(t, filepath.Join(toolDir, "provider_status.yaml"), `schema: luc.tool/v1
name: provider_status
description: Show provider status.
runtime:
  kind: exec
  command: ./.luc/tools/provider_status.py
  capabilities:
    - structured_io
    - client_actions
input_schema:
  type: object
  properties: {}
`)

		uiDir := filepath.Join(root, ".luc", "ui")
		mustMkdir(t, uiDir)
		mustWriteFile(t, filepath.Join(uiDir, "provider_status.yaml"), `schema: luc.ui/v1
id: provider-status
commands:
  - id: provider.status.open
    name: Open provider status
    action:
      kind: view.open
      view_id: provider.status
views:
  - id: provider.status
    title: Provider Status
    placement: page
    source_tool: provider_status
    render: json
`)
	}

	if opts.withHook {
		hookDir := filepath.Join(root, ".luc", "hooks")
		mustMkdir(t, hookDir)
		mustWriteExecutable(t, filepath.Join(hookDir, "notify.py"), `#!/usr/bin/env python3
import json, sys
_ = json.loads(sys.stdin.readline())
print(json.dumps({"type": "client_action", "action": {"id": "hook_confirm_1", "kind": "confirm.request", "blocking": True, "title": "Hook?", "body": "Approve hook action?", "options": [{"id": "approve", "label": "Approve", "primary": True}]}}), flush=True)
_ = json.loads(sys.stdin.readline())
print(json.dumps({"type": "done", "done": True}), flush=True)
`)
		mustWriteFile(t, filepath.Join(hookDir, "notify.yaml"), `schema: luc.hook/v1
id: rpc_notify
description: Notify on assistant completion.
events:
  - message.assistant.final
runtime:
  kind: exec
  command: ./notify.py
  capabilities:
    - client_actions
delivery:
  mode: async
  timeout_seconds: 5
`)
	}

	return root
}

func newRPCController(t *testing.T, root string) *kernel.Controller {
	t.Helper()

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return controller
}

func newRPCHarness(t *testing.T, controller *kernel.Controller) *rpcHarness {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	done := make(chan error, 1)
	go func() {
		defer cancel()
		err := Run(ctx, Options{
			Controller: controller,
			Stdin:      inReader,
			Stdout:     outWriter,
		})
		_ = outWriter.Close()
		done <- err
	}()

	frames := make(chan rpcFrame, 128)
	go func() {
		defer close(frames)
		scanner := bufio.NewScanner(outReader)
		scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			var head struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(line, &head); err != nil {
				continue
			}
			switch head.Type {
			case "response":
				var resp Response
				if err := json.Unmarshal(line, &resp); err == nil {
					frames <- rpcFrame{Response: &resp}
				}
			case "event":
				var event EventFrame
				if err := json.Unmarshal(line, &event); err == nil {
					frames <- rpcFrame{Event: &event}
				}
			}
		}
	}()

	return &rpcHarness{
		t:      t,
		input:  inWriter,
		frames: frames,
		done:   done,
	}
}

func (h *rpcHarness) Close() {
	h.t.Helper()

	_ = h.input.Close()
	select {
	case err := <-h.done:
		if err != nil && !errors.Is(err, io.ErrClosedPipe) {
			h.t.Fatalf("rpc run failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		h.t.Fatal("timed out waiting for rpc mode to exit")
	}
}

func (h *rpcHarness) Send(command Command) {
	h.t.Helper()

	if err := json.NewEncoder(h.input).Encode(command); err != nil {
		h.t.Fatalf("send command: %v", err)
	}
}

func (h *rpcHarness) SendRaw(line string) {
	h.t.Helper()

	if _, err := io.WriteString(h.input, line+"\n"); err != nil {
		h.t.Fatalf("send raw line: %v", err)
	}
}

func (h *rpcHarness) WaitResponse(id string) Response {
	h.t.Helper()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case frame, ok := <-h.frames:
			if !ok {
				h.t.Fatalf("rpc output closed while waiting for response %q", id)
			}
			if frame.Response != nil && frame.Response.ID == id {
				return *frame.Response
			}
		case <-deadline:
			h.t.Fatalf("timed out waiting for response %q", id)
		}
	}
}

func (h *rpcHarness) WaitEvent(t *testing.T, match func(history.EventEnvelope) bool) history.EventEnvelope {
	t.Helper()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case frame, ok := <-h.frames:
			if !ok {
				t.Fatal("rpc output closed while waiting for event")
			}
			if frame.Event != nil && match(frame.Event.Event) {
				return frame.Event.Event
			}
		case <-deadline:
			t.Fatal("timed out waiting for event")
		}
	}
}

func decodeResponseData[T any](t *testing.T, response Response) T {
	t.Helper()

	if !response.Success {
		t.Fatalf("expected successful response, got %#v", response)
	}
	var out T
	data, err := json.Marshal(response.Data)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func eventsContainKind(events []history.EventEnvelope, kind string) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func eventsContainUserText(events []history.EventEnvelope, text string) bool {
	for _, event := range events {
		if event.Kind != "message.user" {
			continue
		}
		payload := history.DecodePayload[history.MessagePayload](event.Payload)
		if payload.Content == text {
			return true
		}
	}
	return false
}

func modelsContain(models []map[string]any, id string) bool {
	for _, model := range models {
		if fmt.Sprint(model["ID"]) == id || fmt.Sprint(model["id"]) == id {
			return true
		}
	}
	return false
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustWriteExecutable(t *testing.T, path, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
