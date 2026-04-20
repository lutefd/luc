package execprovider

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/provider"
)

func TestExecProviderStreamsEventsAndReceivesStructuredRequest(t *testing.T) {
	dir := t.TempDir()
	requestPath := filepath.Join(dir, "request.json")
	scriptPath := filepath.Join(dir, "adapter.sh")
	if err := os.WriteFile(scriptPath, []byte(`#!/bin/sh
request_path="$1"
input="$(cat)"
printf '%s' "$input" > "$request_path"
cat <<'EOF'
{"type":"thinking","text":"Thinking..."}
{"type":"text_delta","text":"hello "}
{"type":"tool_call","tool_call":{"id":"call_1","name":"read","arguments":"{\"path\":\"go.mod\"}"}}
{"type":"done","completed":true}
EOF
`), 0o755); err != nil {
		t.Fatal(err)
	}

	client, err := New(config.ProviderConfig{}, Spec{
		Name:    "Acme Gateway",
		Command: "./adapter.sh",
		Args:    []string{requestPath},
		Dir:     dir,
		Env: map[string]string{
			"TEST_MODE": "1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := client.Start(t.Context(), provider.Request{
		Model:  "claude-opus-4-7",
		System: "be terse",
		Messages: []provider.Message{{
			Role: "user",
			Parts: []provider.ContentPart{
				{Type: "text", Text: "describe"},
				{Type: "image", MediaType: "image/png", Data: "aGVsbG8="},
			},
		}},
		Tools: []provider.ToolSpec{{
			Name:        "read",
			Description: "Read a file",
			Schema:      json.RawMessage(`{"type":"object"}`),
		}},
		MaxTokens: 20000,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	ev, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "thinking" || ev.Text != "Thinking..." {
		t.Fatalf("unexpected first event %#v", ev)
	}

	ev, err = stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "text_delta" || ev.Text != "hello " {
		t.Fatalf("unexpected text event %#v", ev)
	}

	ev, err = stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "tool_call" || ev.ToolCall.ID != "call_1" || ev.ToolCall.Name != "read" {
		t.Fatalf("unexpected tool event %#v", ev)
	}

	ev, err = stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "done" || !ev.Completed {
		t.Fatalf("unexpected done event %#v", ev)
	}

	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("expected EOF after adapter stream, got %v", err)
	}

	data, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	requestRaw, ok := raw["request"].(map[string]any)
	if !ok {
		t.Fatalf("expected raw request object, got %#v", raw)
	}
	if requestRaw["model"] != "claude-opus-4-7" {
		t.Fatalf("expected lowercase model key in raw request, got %#v", requestRaw)
	}
	if _, exists := requestRaw["Model"]; exists {
		t.Fatalf("did not expect Go-style capitalized model key in raw request: %#v", requestRaw)
	}
	toolsRaw, ok := requestRaw["tools"].([]any)
	if !ok || len(toolsRaw) != 1 {
		t.Fatalf("expected raw tools array, got %#v", requestRaw["tools"])
	}
	toolRaw, ok := toolsRaw[0].(map[string]any)
	if !ok || toolRaw["name"] != "read" {
		t.Fatalf("expected lowercase tool fields, got %#v", requestRaw["tools"])
	}

	var got struct {
		Request provider.Request `json:"request"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Request.Model != "claude-opus-4-7" || got.Request.System != "be terse" {
		t.Fatalf("unexpected request envelope %#v", got.Request)
	}
	if len(got.Request.Messages) != 1 || len(got.Request.Messages[0].Parts) != 2 {
		t.Fatalf("expected structured message parts, got %#v", got.Request.Messages)
	}
	if got.Request.Messages[0].Parts[1].Type != "image" || got.Request.Messages[0].Parts[1].Data != "aGVsbG8=" {
		t.Fatalf("expected image part in request, got %#v", got.Request.Messages[0].Parts)
	}
	if len(got.Request.Tools) != 1 || got.Request.Tools[0].Name != "read" {
		t.Fatalf("expected tools in request, got %#v", got.Request.Tools)
	}
}

func TestExecProviderReturnsAdapterErrors(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "adapter.sh")
	if err := os.WriteFile(scriptPath, []byte(`#!/bin/sh
cat >/dev/null
printf '%s\n' '{"error":"adapter failed"}'
`), 0o755); err != nil {
		t.Fatal(err)
	}

	client, err := New(config.ProviderConfig{}, Spec{
		Command: "./adapter.sh",
		Dir:     dir,
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := client.Start(t.Context(), provider.Request{
		Model:    "test-model",
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	if _, err := stream.Recv(); err == nil || err.Error() != "adapter failed" {
		t.Fatalf("expected adapter error, got %v", err)
	}
}
