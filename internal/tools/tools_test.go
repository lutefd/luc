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
	manager, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
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
	if diff, _ := manager.Run(ctx, Request{Name: "read", Arguments: string(readArgs)}); diff.Content == "" {
		t.Fatal("expected read result content")
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

func TestWriteAndEditIncludeDiffMetadata(t *testing.T) {
	root := t.TempDir()
	manager, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	writeArgs, _ := json.Marshal(map[string]any{
		"path":    "notes.txt",
		"content": "alpha\nbeta",
	})
	writeResult, err := manager.Run(ctx, Request{Name: "write", Arguments: string(writeArgs)})
	if err != nil {
		t.Fatal(err)
	}
	if diff, _ := writeResult.Metadata["diff"].(string); !strings.Contains(diff, "@@") {
		t.Fatalf("expected unified diff in write metadata, got %#v", writeResult.Metadata)
	}

	editArgs, _ := json.Marshal(map[string]any{
		"path":     "notes.txt",
		"old_text": "beta",
		"new_text": "gamma",
	})
	editResult, err := manager.Run(ctx, Request{Name: "edit", Arguments: string(editArgs)})
	if err != nil {
		t.Fatal(err)
	}
	if diff, _ := editResult.Metadata["diff"].(string); !strings.Contains(diff, "-beta") || !strings.Contains(diff, "+gamma") {
		t.Fatalf("expected edit diff metadata, got %#v", editResult.Metadata)
	}
}

func TestEditMissingTargetAndPathEscape(t *testing.T) {
	root := t.TempDir()
	manager, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
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
	manager, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
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
	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Run(context.Background(), Request{Name: "missing"}); err == nil {
		t.Fatal("expected unknown tool error")
	}
}

func TestRuntimeToolLoadsFromGlobalLucDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".luc", "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `name: echo
description: Echo text through a runtime extension
command: printf '{{ .text }}'
schema:
  type: object
  properties:
    text:
      type: string
  required: [text]
ui:
  default_collapsed: true
  collapsed_summary: Echoed {{ .text }}
`
	if err := os.WriteFile(filepath.Join(home, ".luc", "tools", "echo.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]any{"text": "hello"})
	result, err := manager.Run(context.Background(), Request{Name: "echo", Arguments: string(args)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Content) != "hello" {
		t.Fatalf("expected runtime tool output, got %q", result.Content)
	}
	if collapsed, _ := result.Metadata[MetadataUIDefaultCollapsed].(bool); !collapsed {
		t.Fatalf("expected runtime tool collapsed metadata, got %#v", result.Metadata)
	}
	if summary, _ := result.Metadata[MetadataUICollapsedSummary].(string); summary != "Echoed hello" {
		t.Fatalf("expected runtime tool collapsed summary, got %#v", result.Metadata)
	}
}

func TestRuntimeToolProjectOverrideWinsOverGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".luc", "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	globalManifest := `name: echo
description: global echo
command: printf global
schema:
  type: object
`
	if err := os.WriteFile(filepath.Join(home, ".luc", "tools", "echo.yaml"), []byte(globalManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".luc", "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	localManifest := `name: echo
description: local echo
command: printf local
schema:
  type: object
`
	if err := os.WriteFile(filepath.Join(root, ".luc", "tools", "echo.yaml"), []byte(localManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Run(context.Background(), Request{Name: "echo", Arguments: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Content) != "local" {
		t.Fatalf("expected project override tool output, got %q", result.Content)
	}
}

func TestRuntimeToolTemplateExposesToolDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".luc", "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(home, ".luc", "tools", "echo.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nprintf tool-dir-ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `name: tool_dir_echo
description: Echo through a bundled script.
command: "{{ .tool_dir }}/echo.sh"
schema:
  type: object
  properties: {}
`
	if err := os.WriteFile(filepath.Join(home, ".luc", "tools", "echo.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	result, err := manager.Run(context.Background(), Request{Name: "tool_dir_echo", Arguments: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Content) != "tool-dir-ok" {
		t.Fatalf("expected tool_dir script output, got %q", result.Content)
	}
}

func TestReadToolDefaultsToBoundedChunks(t *testing.T) {
	root := t.TempDir()
	manager, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	lines := make([]string, 6500)
	for i := range lines {
		lines[i] = "line"
	}
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]any{"path": "large.txt"})
	result, err := manager.Run(context.Background(), Request{Name: "read", Arguments: string(args)})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(strings.Split(result.Content, "\n")); got != defaultReadLineLimit {
		t.Fatalf("expected %d lines, got %d", defaultReadLineLimit, got)
	}
	if truncated, _ := result.Metadata["truncated"].(bool); !truncated {
		t.Fatalf("expected truncated metadata, got %#v", result.Metadata)
	}
}
