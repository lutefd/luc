package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/lutefd/luc/internal/provider"
)

type Request struct {
	Name      string
	Arguments string
	Workspace string
	SessionID string
	AgentID   string
}

type Result struct {
	Content  string
	Metadata map[string]any
}

type Tool interface {
	Spec() provider.ToolSpec
	Run(ctx context.Context, req Request) (Result, error)
}

type Manager struct {
	workspace string
	tools     map[string]Tool
}

func NewManager(workspaceRoot string) *Manager {
	m := &Manager{
		workspace: workspaceRoot,
		tools:     make(map[string]Tool),
	}

	for _, tool := range []Tool{
		&readTool{workspace: workspaceRoot},
		&writeTool{workspace: workspaceRoot},
		&editTool{workspace: workspaceRoot},
		&bashTool{workspace: workspaceRoot},
		&listToolsTool{manager: m},
	} {
		m.tools[tool.Spec().Name] = tool
	}

	return m
}

func (m *Manager) Specs() []provider.ToolSpec {
	out := make([]provider.ToolSpec, 0, len(m.tools))
	for _, tool := range m.tools {
		out = append(out, tool.Spec())
	}
	return out
}

func (m *Manager) Run(ctx context.Context, req Request) (Result, error) {
	tool, ok := m.tools[req.Name]
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", req.Name)
	}
	return tool.Run(ctx, req)
}

func safePath(root, target string) (string, error) {
	if target == "" {
		return "", errors.New("path is required")
	}

	path := target
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes workspace", target)
	}

	return path, nil
}

type readArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

type readTool struct {
	workspace string
}

func (t *readTool) Spec() provider.ToolSpec {
	return provider.ToolSpec{
		Name:        "read",
		Description: "Read a file from the workspace with optional line offset and limit.",
		Schema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"path":{"type":"string"},
				"offset":{"type":"integer","minimum":0},
				"limit":{"type":"integer","minimum":1}
			},
			"required":["path"]
		}`),
	}
}

func (t *readTool) Run(ctx context.Context, req Request) (Result, error) {
	_ = ctx
	var args readArgs
	if err := json.Unmarshal([]byte(req.Arguments), &args); err != nil {
		return Result{}, err
	}

	path, err := safePath(t.workspace, args.Path)
	if err != nil {
		return Result{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}

	lines := strings.Split(string(data), "\n")
	start := args.Offset
	if start > len(lines) {
		start = len(lines)
	}
	end := len(lines)
	if args.Limit > 0 && start+args.Limit < end {
		end = start + args.Limit
	}

	content := strings.Join(lines[start:end], "\n")
	return Result{
		Content: content,
		Metadata: map[string]any{
			"path":   path,
			"offset": start,
			"lines":  end - start,
		},
	}, nil
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type writeTool struct {
	workspace string
}

func (t *writeTool) Spec() provider.ToolSpec {
	return provider.ToolSpec{
		Name:        "write",
		Description: "Replace a file's full contents within the workspace.",
		Schema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"path":{"type":"string"},
				"content":{"type":"string"}
			},
			"required":["path","content"]
		}`),
	}
}

func (t *writeTool) Run(ctx context.Context, req Request) (Result, error) {
	_ = ctx
	var args writeArgs
	if err := json.Unmarshal([]byte(req.Arguments), &args); err != nil {
		return Result{}, err
	}

	path, err := safePath(t.workspace, args.Path)
	if err != nil {
		return Result{}, err
	}
	before, beforeErr := os.ReadFile(path)
	if beforeErr != nil && !errors.Is(beforeErr, os.ErrNotExist) {
		return Result{}, beforeErr
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(path, []byte(args.Content), 0o644); err != nil {
		return Result{}, err
	}

	return Result{
		Content: fmt.Sprintf("wrote %d bytes to %s", len(args.Content), path),
		Metadata: map[string]any{
			"path":  path,
			"bytes": len(args.Content),
			"diff":  buildDiff(path, string(before), args.Content),
		},
	}, nil
}

type editOperation struct {
	OldText    string `json:"old_text"`
	NewText    string `json:"new_text"`
	ReplaceAll bool   `json:"replace_all"`
}

type editArgs struct {
	Path    string          `json:"path"`
	Edits   []editOperation `json:"edits"`
	OldText string          `json:"old_text"`
	NewText string          `json:"new_text"`
}

type editTool struct {
	workspace string
}

func (t *editTool) Spec() provider.ToolSpec {
	return provider.ToolSpec{
		Name:        "edit",
		Description: "Apply targeted text replacements to a workspace file.",
		Schema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"path":{"type":"string"},
				"edits":{
					"type":"array",
					"items":{
						"type":"object",
						"properties":{
							"old_text":{"type":"string"},
							"new_text":{"type":"string"},
							"replace_all":{"type":"boolean"}
						},
						"required":["old_text","new_text"]
					}
				},
				"old_text":{"type":"string"},
				"new_text":{"type":"string"}
			},
			"required":["path"]
		}`),
	}
}

func (t *editTool) Run(ctx context.Context, req Request) (Result, error) {
	_ = ctx
	var args editArgs
	if err := json.Unmarshal([]byte(req.Arguments), &args); err != nil {
		return Result{}, err
	}

	if len(args.Edits) == 0 && args.OldText != "" {
		args.Edits = []editOperation{{OldText: args.OldText, NewText: args.NewText}}
	}
	if len(args.Edits) == 0 {
		return Result{}, errors.New("at least one edit is required")
	}

	path, err := safePath(t.workspace, args.Path)
	if err != nil {
		return Result{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}

	before := string(data)
	content := before
	totalReplacements := 0
	for _, edit := range args.Edits {
		if edit.OldText == "" {
			return Result{}, errors.New("old_text cannot be empty")
		}
		if !strings.Contains(content, edit.OldText) {
			return Result{}, fmt.Errorf("target text not found in %s", path)
		}

		if edit.ReplaceAll {
			count := strings.Count(content, edit.OldText)
			content = strings.ReplaceAll(content, edit.OldText, edit.NewText)
			totalReplacements += count
			continue
		}

		content = strings.Replace(content, edit.OldText, edit.NewText, 1)
		totalReplacements++
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return Result{}, err
	}

	return Result{
		Content: fmt.Sprintf("applied %d edit(s) to %s", totalReplacements, path),
		Metadata: map[string]any{
			"path":         path,
			"replacements": totalReplacements,
			"diff":         buildDiff(path, before, content),
		},
	}, nil
}

type bashArgs struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type bashTool struct {
	workspace string
}

func (t *bashTool) Spec() provider.ToolSpec {
	return provider.ToolSpec{
		Name:        "bash",
		Description: "Run a shell command in the workspace.",
		Schema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"command":{"type":"string"},
				"timeout_seconds":{"type":"integer","minimum":1}
			},
			"required":["command"]
		}`),
	}
}

func (t *bashTool) Run(ctx context.Context, req Request) (Result, error) {
	var args bashArgs
	if err := json.Unmarshal([]byte(req.Arguments), &args); err != nil {
		return Result{}, err
	}
	if args.Command == "" {
		return Result{}, errors.New("command is required")
	}

	timeout := 30 * time.Second
	if args.TimeoutSeconds > 0 {
		timeout = time.Duration(args.TimeoutSeconds) * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "zsh", "-lc", args.Command)
	cmd.Dir = t.workspace
	output, err := cmd.CombinedOutput()

	metadata := map[string]any{
		"command": args.Command,
		"timeout": timeout.String(),
	}
	if execCtx.Err() == context.DeadlineExceeded {
		metadata["timed_out"] = true
	}

	return Result{
		Content:  string(output),
		Metadata: metadata,
	}, err
}

type listToolsTool struct {
	manager *Manager
}

func (t *listToolsTool) Spec() provider.ToolSpec {
	return provider.ToolSpec{
		Name:        "list_tools",
		Description: "List available luc tools and their descriptions.",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
	}
}

func (t *listToolsTool) Run(ctx context.Context, req Request) (Result, error) {
	_ = ctx
	_ = req
	var lines []string
	for _, spec := range t.manager.Specs() {
		lines = append(lines, fmt.Sprintf("- %s: %s", spec.Name, spec.Description))
	}
	return Result{Content: strings.Join(lines, "\n")}, nil
}

func buildDiff(path, before, after string) string {
	diff := udiff.Unified(path, path, before, after)
	return strings.TrimSpace(diff)
}
