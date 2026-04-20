package kernel

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/provider"
	"github.com/lutefd/luc/internal/tools"
)

func TestControllerExtensionHostsObserveEventsAndRehydrateStorage(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{
			{
				{Type: "text_delta", Text: "final"},
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

	logPath := filepath.Join(root, "extension-input.jsonl")
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "extensions", "audit.yaml"), `schema: luc.extension/v1
id: audit
protocol_version: 1
runtime:
  kind: exec
  command: ./host.py
subscriptions:
  - event: session.start
  - event: session.reload
  - event: message.assistant.final
`)
	hostScript := "#!/usr/bin/env python3\n" +
		"import json, sys\n" +
		"log_path = r'" + logPath + "'\n" +
		"def emit(obj):\n" +
		"    sys.stdout.write(json.dumps(obj) + '\\n')\n" +
		"    sys.stdout.flush()\n" +
		"for line in sys.stdin:\n" +
		"    line = line.strip()\n" +
		"    if not line:\n" +
		"        continue\n" +
		"    with open(log_path, 'a', encoding='utf-8') as fh:\n" +
		"        fh.write(line + '\\n')\n" +
		"    msg = json.loads(line)\n" +
		"    if msg.get('type') == 'hello':\n" +
		"        emit({'type': 'ready', 'protocol_version': 1})\n" +
		"    elif msg.get('type') == 'event' and msg.get('event') == 'message.assistant.final':\n" +
		"        emit({'type': 'storage_update', 'scope': 'session', 'value': {'last_event': msg.get('event')}})\n" +
		"        emit({'type': 'storage_update', 'scope': 'workspace', 'value': {'messages': 1}})\n" +
		"    elif msg.get('type') == 'session_shutdown':\n" +
		"        break\n"
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "extensions", "host.py"), hostScript)
	if err := os.Chmod(filepath.Join(root, ".luc", "extensions", "host.py"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	waitForExtensionMessages(t, logPath, func(messages []map[string]any) bool {
		return extensionMessageSeen(messages, "event", "session.start")
	})

	if err := controller.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	waitForExtensionMessages(t, logPath, func(messages []map[string]any) bool {
		return extensionMessageSeen(messages, "event", "message.assistant.final")
	})

	sessionID := controller.Session().SessionID
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	waitForExtensionMessages(t, logPath, func(messages []map[string]any) bool {
		return extensionMessageTypeCount(messages, "session_shutdown") >= 1
	})

	reopened, err := Open(context.Background(), root, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	waitForExtensionMessages(t, logPath, func(messages []map[string]any) bool {
		snapshots := extensionMessagesOfType(messages, "storage_snapshot")
		if len(snapshots) < 2 {
			return false
		}
		last := snapshots[len(snapshots)-1]
		session, _ := last["session"].(map[string]any)
		workspace, _ := last["workspace"].(map[string]any)
		return session["last_event"] == "message.assistant.final" && workspace["messages"] == float64(1)
	})

	if err := reopened.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitForExtensionMessages(t, logPath, func(messages []map[string]any) bool {
		return extensionMessageSeen(messages, "event", "session.reload")
	})

	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	waitForExtensionMessages(t, logPath, func(messages []map[string]any) bool {
		return extensionMessageTypeCount(messages, "session_shutdown") >= 2
	})
}

func TestControllerSyncExtensionsTransformInputAndAppendPromptContext(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{{
			{Type: "text_delta", Text: "ok"},
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

	mustWriteKernelFile(t, filepath.Join(root, ".luc", "extensions", "sync.yaml"), `schema: luc.extension/v1
id: sync
protocol_version: 1
runtime:
  kind: exec
  command: ./host.py
subscriptions:
  - event: input.transform
    mode: sync
  - event: prompt.context
    mode: sync
`)
	script := "#!/usr/bin/env python3\n" +
		"import json, sys\n" +
		"def emit(obj):\n" +
		"    sys.stdout.write(json.dumps(obj) + '\\n')\n" +
		"    sys.stdout.flush()\n" +
		"for line in sys.stdin:\n" +
		"    msg = json.loads(line)\n" +
		"    if msg.get('type') == 'hello':\n" +
		"        emit({'type': 'ready', 'protocol_version': 1})\n" +
		"    elif msg.get('type') == 'event' and msg.get('event') == 'input.transform':\n" +
		"        emit({'type': 'decision', 'request_id': msg['request_id'], 'decision': 'transform', 'text': 'rewritten request'})\n" +
		"    elif msg.get('type') == 'event' and msg.get('event') == 'prompt.context':\n" +
		"        emit({'type': 'decision', 'request_id': msg['request_id'], 'decision': 'system_append', 'system_append': ['SYNC PROMPT BLOCK']})\n" +
		"    elif msg.get('type') == 'session_shutdown':\n" +
		"        break\n"
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "extensions", "host.py"), script)
	if err := os.Chmod(filepath.Join(root, ".luc", "extensions", "host.py"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()

	if err := controller.Submit(context.Background(), "original request"); err != nil {
		t.Fatal(err)
	}
	if got := providerStub.lastRequest.System; !strings.Contains(got, "SYNC PROMPT BLOCK") {
		t.Fatalf("expected prompt context block in system prompt, got %q", got)
	}
	if len(providerStub.lastRequest.Messages) == 0 {
		t.Fatalf("expected provider request messages, got %#v", providerStub.lastRequest.Messages)
	}
	if len(providerStub.lastRequest.Messages[0].Parts) == 0 || providerStub.lastRequest.Messages[0].Parts[0].Text != "rewritten request" {
		t.Fatalf("expected transformed user input in provider request, got %#v", providerStub.lastRequest.Messages)
	}
	events := controller.SessionEvents()
	for _, ev := range events {
		if ev.Kind != "message.user" {
			continue
		}
		payload := decode[history.MessagePayload](ev.Payload)
		if payload.Content != "rewritten request" {
			t.Fatalf("expected transformed user event, got %#v", payload)
		}
		return
	}
	t.Fatal("expected message.user event")
}

func TestControllerSyncExtensionsPatchToolPreflightAndResult(t *testing.T) {
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
    body_template: "{{ index .arguments \"command\" }}"
`)
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "extensions", "sync.yaml"), `schema: luc.extension/v1
id: sync
protocol_version: 1
runtime:
  kind: exec
  command: ./host.py
subscriptions:
  - event: tool.preflight
    mode: sync
    failure_mode: closed
  - event: tool.result
    mode: sync
`)
	script := "#!/usr/bin/env python3\n" +
		"import json, sys\n" +
		"def emit(obj):\n" +
		"    sys.stdout.write(json.dumps(obj) + '\\n')\n" +
		"    sys.stdout.flush()\n" +
		"for line in sys.stdin:\n" +
		"    msg = json.loads(line)\n" +
		"    if msg.get('type') == 'hello':\n" +
		"        emit({'type': 'ready', 'protocol_version': 1})\n" +
		"    elif msg.get('type') == 'event' and msg.get('event') == 'tool.preflight':\n" +
		"        emit({'type': 'decision', 'request_id': msg['request_id'], 'decision': 'patch', 'arguments': {'command': 'printf patched'}})\n" +
		"    elif msg.get('type') == 'event' and msg.get('event') == 'tool.result':\n" +
		"        emit({'type': 'decision', 'request_id': msg['request_id'], 'decision': 'patch', 'content': 'rewritten tool output', 'collapsed_summary': 'patched summary'})\n" +
		"    elif msg.get('type') == 'session_shutdown':\n" +
		"        break\n"
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "extensions", "host.py"), script)
	if err := os.Chmod(filepath.Join(root, ".luc", "extensions", "host.py"), 0o755); err != nil {
		t.Fatal(err)
	}

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
	if !strings.Contains(broker.action.Body, "printf patched") {
		t.Fatalf("expected approval prompt to use patched args, got %#v", broker.action)
	}

	events := controller.SessionEvents()
	for _, ev := range events {
		if ev.Kind != "tool.finished" {
			continue
		}
		payload := decode[history.ToolResultPayload](ev.Payload)
		if payload.Content != "rewritten tool output" {
			t.Fatalf("expected patched tool result content, got %#v", payload)
		}
		if got, _ := payload.Metadata[tools.MetadataUICollapsedSummary].(string); got != "patched summary" {
			t.Fatalf("expected patched collapsed summary, got %#v", payload.Metadata)
		}
		return
	}
	t.Fatal("expected tool.finished event")
}

func TestControllerSyncInputTransformFailClosedBlocksTurn(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{}
	newProvider = func(cfg config.ProviderConfig) (provider.Provider, error) {
		_ = cfg
		return providerStub, nil
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "extensions", "sync.yaml"), `schema: luc.extension/v1
id: sync
protocol_version: 1
runtime:
  kind: exec
  command: ./host.py
subscriptions:
  - event: input.transform
    mode: sync
    timeout_ms: 50
    failure_mode: closed
`)
	script := "#!/usr/bin/env python3\n" +
		"import json, sys, time\n" +
		"def emit(obj):\n" +
		"    sys.stdout.write(json.dumps(obj) + '\\n')\n" +
		"    sys.stdout.flush()\n" +
		"for line in sys.stdin:\n" +
		"    msg = json.loads(line)\n" +
		"    if msg.get('type') == 'hello':\n" +
		"        emit({'type': 'ready', 'protocol_version': 1})\n" +
		"    elif msg.get('type') == 'event' and msg.get('event') == 'input.transform':\n" +
		"        time.sleep(0.2)\n" +
		"    elif msg.get('type') == 'session_shutdown':\n" +
		"        break\n"
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "extensions", "host.py"), script)
	if err := os.Chmod(filepath.Join(root, ".luc", "extensions", "host.py"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()

	err = controller.Submit(context.Background(), "blocked")
	if err == nil || !strings.Contains(err.Error(), "input blocked") {
		t.Fatalf("expected fail-closed input transform error, got %v", err)
	}
	if len(providerStub.requests) != 0 {
		t.Fatalf("expected provider not to be called, got %#v", providerStub.requests)
	}
}

func TestControllerHostedExtensionToolRunsThroughPersistentHost(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{
			{
				{Type: "tool_call", ToolCall: provider.ToolCall{ID: "call_1", Name: "stateful_echo", Arguments: `{"text":"one"}`}},
				{Type: "done", Completed: true},
			},
			{
				{Type: "tool_call", ToolCall: provider.ToolCall{ID: "call_2", Name: "stateful_echo", Arguments: `{"text":"two"}`}},
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

	mustWriteKernelFile(t, filepath.Join(root, ".luc", "extensions", "audit.yaml"), `schema: luc.extension/v1
id: audit
protocol_version: 1
runtime:
  kind: exec
  command: ./host.py
subscriptions:
  - event: session.start
`)
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "tools", "stateful_echo.yaml"), `schema: luc.tool/v2
name: stateful_echo
description: Echo through the audit host.
runtime:
  kind: extension
  extension_id: audit
  handler: echo
input_schema:
  type: object
  properties:
    text:
      type: string
  required: [text]
`)
	script := "#!/usr/bin/env python3\n" +
		"import json, sys\n" +
		"count = 0\n" +
		"def emit(obj):\n" +
		"    sys.stdout.write(json.dumps(obj) + '\\n')\n" +
		"    sys.stdout.flush()\n" +
		"for line in sys.stdin:\n" +
		"    msg = json.loads(line)\n" +
		"    if msg.get('type') == 'hello':\n" +
		"        emit({'type': 'ready', 'protocol_version': 1})\n" +
		"    elif msg.get('type') == 'tool_invoke':\n" +
		"        count += 1\n" +
		"        text = msg['tool']['arguments'].get('text', '')\n" +
		"        emit({'type': 'tool_result', 'request_id': msg['request_id'], 'result': {'content': f'count {count}: {text}', 'metadata': {'count': count}, 'collapsed_summary': f'call {count}'}})\n" +
		"    elif msg.get('type') == 'session_shutdown':\n" +
		"        break\n"
	mustWriteKernelFile(t, filepath.Join(root, ".luc", "extensions", "host.py"), script)
	if err := os.Chmod(filepath.Join(root, ".luc", "extensions", "host.py"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()

	if err := controller.Submit(context.Background(), "run it"); err != nil {
		t.Fatal(err)
	}

	var contents []string
	for _, ev := range controller.SessionEvents() {
		if ev.Kind != "tool.finished" {
			continue
		}
		payload := decode[history.ToolResultPayload](ev.Payload)
		if payload.Name == "stateful_echo" {
			contents = append(contents, payload.Content)
		}
	}
	if len(contents) != 2 {
		t.Fatalf("expected two hosted tool results, got %#v", contents)
	}
	if contents[0] != "count 1: one" || contents[1] != "count 2: two" {
		t.Fatalf("expected persistent hosted tool state, got %#v", contents)
	}
}

func waitForExtensionMessages(t *testing.T, path string, predicate func([]map[string]any) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		messages, err := readExtensionMessages(path)
		if err == nil && predicate(messages) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	messages, err := readExtensionMessages(path)
	if err != nil {
		t.Fatalf("timed out waiting for extension messages at %s: %v", path, err)
	}
	t.Fatalf("timed out waiting for extension messages at %s; saw %#v", path, messages)
}

func readExtensionMessages(path string) ([]map[string]any, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var messages []map[string]any
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var message map[string]any
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, scanner.Err()
}

func extensionMessageSeen(messages []map[string]any, typ, event string) bool {
	for _, message := range messages {
		if message["type"] != typ {
			continue
		}
		if event == "" || message["event"] == event {
			return true
		}
	}
	return false
}

func extensionMessageTypeCount(messages []map[string]any, typ string) int {
	count := 0
	for _, message := range messages {
		if message["type"] == typ {
			count++
		}
	}
	return count
}

func extensionMessagesOfType(messages []map[string]any, typ string) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		if message["type"] == typ {
			out = append(out, message)
		}
	}
	return out
}
