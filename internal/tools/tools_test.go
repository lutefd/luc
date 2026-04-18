package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadWriteEditAndListTools(t *testing.T) {
	root := t.TempDir()
	manager := NewManager(root)
	ctx := context.Background()

	writeArgs, _ := json.Marshal(map[string]any{
		"path":    "notes.txt",
		"content": "line1\nline2\nline3",
	})
	if _, err := manager.Run(ctx, Request{Name: "write", Arguments: string(writeArgs)}); err != nil {
		t.Fatal(err)
	}

	readArgs, _ := json.Marshal(map[string]any{
		"path":   "notes.txt",
		"offset": 1,
		"limit":  1,
	})
	readResult, err := manager.Run(ctx, Request{Name: "read", Arguments: string(readArgs)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(readResult.Content) != "line2" {
		t.Fatalf("expected line2, got %q", readResult.Content)
	}

	editArgs, _ := json.Marshal(map[string]any{
		"path": "notes.txt",
		"edits": []map[string]any{
			{"old_text": "line2", "new_text": "changed"},
			{"old_text": "line3", "new_text": "done", "replace_all": true},
		},
	})
	if _, err := manager.Run(ctx, Request{Name: "edit", Arguments: string(editArgs)}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "line1\nchanged\ndone" {
		t.Fatalf("unexpected edited content %q", got)
	}

	listResult, err := manager.Run(ctx, Request{Name: "list_tools", Arguments: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	for _, toolName := range []string{"read", "write", "edit", "bash", "list_tools"} {
		if !strings.Contains(listResult.Content, toolName) {
			t.Fatalf("expected tool %q in list output", toolName)
		}
	}
}

func TestEditMissingTargetAndPathEscape(t *testing.T) {
	root := t.TempDir()
	manager := NewManager(root)
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]any{
		"path":     "file.txt",
		"old_text": "missing",
		"new_text": "world",
	})
	if _, err := manager.Run(ctx, Request{Name: "edit", Arguments: string(args)}); err == nil {
		t.Fatal("expected missing target error")
	}

	escapeArgs, _ := json.Marshal(map[string]any{
		"path": "../outside.txt",
	})
	if _, err := manager.Run(ctx, Request{Name: "read", Arguments: string(escapeArgs)}); err == nil {
		t.Fatal("expected path escape error")
	}
}

func TestBashToolSuccessAndTimeout(t *testing.T) {
	root := t.TempDir()
	manager := NewManager(root)
	ctx := context.Background()

	successArgs, _ := json.Marshal(map[string]any{
		"command": "printf hello",
	})
	result, err := manager.Run(ctx, Request{Name: "bash", Arguments: string(successArgs)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Content) != "hello" {
		t.Fatalf("expected hello, got %q", result.Content)
	}

	timeoutArgs, _ := json.Marshal(map[string]any{
		"command":         "sleep 2",
		"timeout_seconds": 1,
	})
	result, err = manager.Run(ctx, Request{Name: "bash", Arguments: string(timeoutArgs)})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if timedOut, _ := result.Metadata["timed_out"].(bool); !timedOut {
		t.Fatalf("expected timed_out metadata, got %#v", result.Metadata)
	}
}

func TestUnknownTool(t *testing.T) {
	manager := NewManager(t.TempDir())
	if _, err := manager.Run(context.Background(), Request{Name: "missing"}); err == nil {
		t.Fatal("expected unknown tool error")
	}
}
