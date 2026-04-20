package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/lutefd/luc/internal/extensions"
	"github.com/lutefd/luc/internal/provider"
	luruntime "github.com/lutefd/luc/internal/runtime"
	"github.com/lutefd/luc/internal/shell"
)

type Request struct {
	Name             string
	Arguments        string
	Workspace        string
	SessionID        string
	AgentID          string
	HostCapabilities []string
	ViewContext      *luruntime.ViewContext
	UIBroker         luruntime.UIBroker
}

type Result struct {
	Content          string
	Metadata         map[string]any
	DefaultCollapsed bool
	CollapsedSummary string
}

func (r Result) RenderContent() string {
	return r.Content
}

const (
	MetadataUIDefaultCollapsed = "ui_default_collapsed"
	MetadataUICollapsedSummary = "ui_collapsed_summary"
	MetadataUIHideContent      = "ui_hide_content"
	MetadataUILabel            = "ui_label"
)

type Tool interface {
	Spec() provider.ToolSpec
	Run(ctx context.Context, req Request) (Result, error)
}

type HostedToolInvoker interface {
	InvokeHostedTool(ctx context.Context, def extensions.ToolDef, req Request) (Result, error)
}

type Manager struct {
	workspace     string
	tools         map[string]Tool
	hostedInvoker HostedToolInvoker
}

func NewManager(workspaceRoot string) (*Manager, error) {
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

	defs, err := extensions.LoadToolDefs(workspaceRoot)
	if err != nil {
		return nil, err
	}
	for _, def := range defs {
		m.tools[def.Name] = &runtimeTool{workspace: workspaceRoot, def: def}
	}

	return m, nil
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
	result, err := tool.Run(ctx, req)
	return normalizeResult(result), err
}

func (m *Manager) SetHostedToolInvoker(invoker HostedToolInvoker) {
	m.hostedInvoker = invoker
	for _, tool := range m.tools {
		if runtimeTool, ok := tool.(*runtimeTool); ok {
			runtimeTool.hostedInvoker = invoker
		}
	}
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
		Content:          content,
		DefaultCollapsed: true,
		CollapsedSummary: readSummary(path, start, end-start),
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

	cmd := shell.Command(execCtx, args.Command)
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
		Content:          string(output),
		Metadata:         metadata,
		DefaultCollapsed: true,
		CollapsedSummary: summarizeOutput(string(output), execCtx.Err() == context.DeadlineExceeded),
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

type runtimeTool struct {
	workspace     string
	def           extensions.ToolDef
	hostedInvoker HostedToolInvoker
}

func (t *runtimeTool) Spec() provider.ToolSpec {
	return provider.ToolSpec{
		Name:        t.def.Name,
		Description: t.def.Description,
		Schema:      t.def.Schema,
	}
}

func (t *runtimeTool) Run(ctx context.Context, req Request) (Result, error) {
	if t.def.RuntimeKind == "extension" {
		if t.hostedInvoker == nil {
			return Result{}, fmt.Errorf("hosted tool %s has no extension invoker", t.def.Name)
		}
		return t.hostedInvoker.InvokeHostedTool(ctx, t.def, req)
	}
	if luruntime.HasCapability(t.def.Capabilities, luruntime.CapabilityStructuredIO) {
		return t.runStructured(ctx, req)
	}

	args, err := decodeArgsMap(req.Arguments)
	if err != nil {
		return Result{}, err
	}
	command, err := renderTemplate(t.def.Command, t.templateData(args, req))
	if err != nil {
		return Result{}, err
	}

	timeout := 30 * time.Second
	if t.def.TimeoutSeconds > 0 {
		timeout = time.Duration(t.def.TimeoutSeconds) * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := shell.Command(execCtx, command)
	cmd.Dir = t.workspace
	output, runErr := cmd.CombinedOutput()
	timedOut := execCtx.Err() == context.DeadlineExceeded

	metadata := map[string]any{
		"command": command,
		"timeout": timeout.String(),
	}
	if timedOut {
		metadata["timed_out"] = true
	}

	result := Result{
		Content:          string(output),
		Metadata:         metadata,
		DefaultCollapsed: t.def.UI.DefaultCollapsed,
	}
	if strings.TrimSpace(t.def.UI.CollapsedSummary) != "" {
		summary, err := renderTemplate(t.def.UI.CollapsedSummary, t.templateDataWithOutput(args, req, command, string(output), timedOut))
		if err != nil {
			return Result{}, err
		}
		result.CollapsedSummary = summary
	} else if result.DefaultCollapsed {
		result.CollapsedSummary = summarizeOutput(string(output), timedOut)
	}
	return result, runErr
}

func (t *runtimeTool) runStructured(ctx context.Context, req Request) (Result, error) {
	args, err := decodeArgsMap(req.Arguments)
	if err != nil {
		return Result{}, err
	}

	timeout := 30 * time.Second
	if t.def.TimeoutSeconds > 0 {
		timeout = time.Duration(t.def.TimeoutSeconds) * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := shell.Command(execCtx, t.def.Command)
	cmd.Dir = t.workspace

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return Result{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return Result{}, err
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return Result{}, err
	}

	encoder := json.NewEncoder(stdin)
	if err := encoder.Encode(luruntime.ToolRequestEnvelope{
		ToolName:         req.Name,
		Arguments:        args,
		Workspace:        req.Workspace,
		SessionID:        req.SessionID,
		AgentID:          req.AgentID,
		HostCapabilities: append([]string(nil), req.HostCapabilities...),
		ViewContext:      req.ViewContext,
	}); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		_ = cmd.Wait()
		return Result{}, err
	}
	stdinOpen := true
	if !luruntime.HasCapability(t.def.Capabilities, luruntime.CapabilityClientAction) {
		_ = stdin.Close()
		stdinOpen = false
	}

	var stderrBuf bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderrBuf, stderr)
		close(stderrDone)
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var result Result
	var stdoutBuf strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event luruntime.ToolEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			if stdinOpen {
				_ = stdin.Close()
			}
			_ = stdout.Close()
			_ = cmd.Wait()
			return Result{}, err
		}

		switch strings.TrimSpace(event.Type) {
		case "stdout", "stderr":
			if strings.TrimSpace(event.Text) != "" {
				stdoutBuf.WriteString(event.Text)
				if !strings.HasSuffix(event.Text, "\n") {
					stdoutBuf.WriteString("\n")
				}
			}
		case "progress":
			if result.Metadata == nil {
				result.Metadata = map[string]any{}
			}
			result.Metadata["progress"] = event.Text
		case "client_action":
			if !luruntime.HasCapability(t.def.Capabilities, luruntime.CapabilityClientAction) {
				return Result{}, errors.New("structured tool emitted client_action without client_actions capability")
			}
			if event.Action == nil {
				return Result{}, errors.New("structured tool client_action is missing action payload")
			}
			action := *event.Action
			if strings.TrimSpace(action.ID) == "" {
				action.ID = nextActionID(req.Name)
			}
			broker := req.UIBroker
			if broker == nil {
				broker = luruntime.NewDefaultBroker("trusted", nil)
			}
			var uiResult luruntime.UIResult
			if action.Blocking {
				uiResult, err = broker.Request(execCtx, action)
			} else {
				err = broker.Publish(action)
				uiResult = luruntime.UIResult{ActionID: action.ID, Accepted: err == nil}
			}
			if err != nil {
				return Result{}, err
			}
			if err := encoder.Encode(luruntime.ClientResultEnvelope{Type: "client_result", Result: uiResult}); err != nil {
				return Result{}, err
			}
		case "result":
			if event.Result != nil {
				result.Content = event.Result.Content
				result.Metadata = cloneMetadata(event.Result.Metadata)
				result.DefaultCollapsed = event.Result.DefaultCollapsed
				result.CollapsedSummary = event.Result.CollapsedSummary
			}
		case "done":
			goto done
		case "error":
			if strings.TrimSpace(event.Error) == "" {
				event.Error = "structured tool failed"
			}
			return Result{}, errors.New(event.Error)
		default:
			return Result{}, fmt.Errorf("unsupported structured tool event type %q", event.Type)
		}
	}
	if err := scanner.Err(); err != nil {
		return Result{}, err
	}

done:
	if stdinOpen {
		_ = stdin.Close()
	}
	_ = stdout.Close()
	waitErr := cmd.Wait()
	<-stderrDone

	if result.Content == "" {
		result.Content = strings.TrimSpace(stdoutBuf.String())
	}
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	if stderrText := strings.TrimSpace(stderrBuf.String()); stderrText != "" {
		result.Metadata["stderr"] = stderrText
	}
	if execCtx.Err() == context.DeadlineExceeded {
		result.Metadata["timed_out"] = true
	}
	return result, waitErr
}

func cloneMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func nextActionID(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "runtime"
	}
	return fmt.Sprintf("%s_action_%d", prefix, time.Now().UnixNano())
}

func normalizeResult(result Result) Result {
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	if result.DefaultCollapsed {
		result.Metadata[MetadataUIDefaultCollapsed] = true
	}
	if strings.TrimSpace(result.CollapsedSummary) != "" {
		result.Metadata[MetadataUICollapsedSummary] = strings.TrimSpace(result.CollapsedSummary)
	}
	return result
}

func readSummary(path string, offset, lines int) string {
	lineLabel := "lines"
	if lines == 1 {
		lineLabel = "line"
	}
	return fmt.Sprintf("Read %d %s from %s starting at line %d.", lines, lineLabel, path, offset+1)
}

func summarizeOutput(content string, timedOut bool) string {
	trimmed := strings.TrimSpace(content)
	lines := 0
	if trimmed != "" {
		for _, line := range strings.Split(trimmed, "\n") {
			if strings.TrimSpace(line) != "" {
				lines++
			}
		}
		if lines == 0 {
			lines = len(strings.Split(trimmed, "\n"))
		}
	}
	summary := fmt.Sprintf("Collapsed output: %d line(s), %d byte(s).", lines, len(trimmed))
	if strings.TrimSpace(trimmed) == "" {
		summary = "No output."
	}
	if timedOut {
		summary += " Timed out."
	}
	return summary
}

func decodeArgsMap(raw string) (map[string]any, error) {
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

func (t *runtimeTool) templateData(args map[string]any, req Request) map[string]any {
	data := templateData(args, req)
	data["tool_dir"] = filepath.Dir(t.def.SourcePath)
	return data
}

func (t *runtimeTool) templateDataWithOutput(args map[string]any, req Request, command, output string, timedOut bool) map[string]any {
	data := t.templateData(args, req)
	data["command"] = command
	data["output"] = output
	data["timed_out"] = timedOut
	return data
}

func templateData(args map[string]any, req Request) map[string]any {
	data := map[string]any{
		"args":       args,
		"workspace":  req.Workspace,
		"session_id": req.SessionID,
		"agent_id":   req.AgentID,
	}
	for key, value := range args {
		data[key] = value
	}
	return data
}

func renderTemplate(raw string, data map[string]any) (string, error) {
	tpl, err := template.New("runtime-tool").Option("missingkey=error").Parse(raw)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}
