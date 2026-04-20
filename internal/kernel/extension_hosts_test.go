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
	"github.com/lutefd/luc/internal/provider"
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
