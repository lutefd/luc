package openai

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/provider"
)

func TestNewRequiresConfiguredAPIKeyEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	_, err := New(config.ProviderConfig{
		BaseURL:   "https://api.openai.com/v1",
		Model:     "gpt-4.1",
		APIKeyEnv: "OPENAI_API_KEY",
	})
	if err == nil {
		t.Fatal("expected missing env error")
	}
}

func TestNewAllowsProvidersWithoutAPIKeyEnv(t *testing.T) {
	client, err := New(config.ProviderConfig{
		BaseURL: "http://localhost:8080/v1",
		Model:   "gateway-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.apiKey != "" {
		t.Fatalf("expected empty API key, got %#v", client)
	}
}

func TestStreamRecvParsesThinkingTextAndToolCalls(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.created"}`,
		`data: {"type":"response.output_text.delta","delta":"hello "}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_1","name":"bash"}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"command\":\""}`,
		`data: {"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"command\":\"printf hi\"}","call_id":"call_1","name":"bash"}`,
		`data: {"type":"response.completed"}`,
		"",
	}, "\n")

	s := &stream{
		body:    io.NopCloser(strings.NewReader(body)),
		scanner: bufio.NewScanner(strings.NewReader(body)),
		calls:   make(map[int]provider.ToolCall),
	}

	ev, err := s.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "thinking" || ev.Text != "Thinking..." {
		t.Fatalf("unexpected first event: %#v", ev)
	}

	ev, err = s.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "text_delta" || ev.Text != "hello " {
		t.Fatalf("unexpected text event: %#v", ev)
	}

	ev, err = s.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "tool_call" || ev.ToolCall.Name != "bash" || ev.ToolCall.ID != "call_1" {
		t.Fatalf("unexpected tool call event: %#v", ev)
	}
	if ev.ToolCall.Arguments != `{"command":"printf hi"}` {
		t.Fatalf("unexpected tool arguments: %#v", ev.ToolCall)
	}

	ev, err = s.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "done" || !ev.Completed {
		t.Fatalf("expected done event, got %#v", ev)
	}
}

func TestResponseInputFromProviderPreservesToolMessages(t *testing.T) {
	input := responseInputFromProvider([]provider.Message{
		{Role: "user", Content: "which model are u?"},
		{Role: "assistant", Content: "gpt-5.4"},
		{
			Role: "assistant",
			ToolCalls: []provider.ToolCall{
				{ID: "call_1", Name: "read", Arguments: `{"path":"go.mod"}`},
			},
		},
		{Role: "tool", ToolCallID: "call_1", Content: "module luc"},
	})

	if len(input) != 4 {
		t.Fatalf("expected four input items, got %#v", input)
	}

	assistantMsg, ok := input[1].(responseMessageInput)
	if !ok {
		t.Fatalf("expected assistant message item, got %#v", input[1])
	}
	if assistantMsg.Content[0].Type != "output_text" {
		t.Fatalf("expected assistant history to use output_text, got %#v", assistantMsg)
	}

	toolCall, ok := input[2].(responseFunctionCallItem)
	if !ok {
		t.Fatalf("expected function call item, got %#v", input[2])
	}
	if toolCall.CallID != "call_1" || toolCall.Name != "read" {
		t.Fatalf("unexpected tool call item: %#v", toolCall)
	}

	toolOut, ok := input[3].(responseFunctionCallOutputItem)
	if !ok {
		t.Fatalf("expected function call output item, got %#v", input[3])
	}
	if toolOut.CallID != "call_1" || toolOut.Output != "module luc" {
		t.Fatalf("unexpected tool output item: %#v", toolOut)
	}
}

func TestResponseInputFromProviderSupportsImageParts(t *testing.T) {
	input := responseInputFromProvider([]provider.Message{
		{
			Role: "user",
			Parts: []provider.ContentPart{
				{Type: "text", Text: "describe this"},
				{Type: "image", MediaType: "image/png", Data: "aGVsbG8="},
			},
		},
	})

	if len(input) != 1 {
		t.Fatalf("expected one input item, got %#v", input)
	}

	msg, ok := input[0].(responseMessageInput)
	if !ok {
		t.Fatalf("expected message input, got %#v", input[0])
	}
	if len(msg.Content) != 2 {
		t.Fatalf("expected two content parts, got %#v", msg)
	}
	if msg.Content[0].Type != "input_text" || msg.Content[0].Text != "describe this" {
		t.Fatalf("unexpected text content %#v", msg.Content[0])
	}
	if msg.Content[1].Type != "input_image" || msg.Content[1].ImageURL != "data:image/png;base64,aGVsbG8=" {
		t.Fatalf("unexpected image content %#v", msg.Content[1])
	}
}

func TestStartUsesResponsesAPIAndOmitsTemperatureForGPT5(t *testing.T) {
	var (
		path string
		got  map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.created\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\"}\n\n")
	}))
	defer server.Close()

	t.Setenv("OPENAI_API_KEY", "token")
	client, err := New(config.ProviderConfig{
		BaseURL:   server.URL,
		Model:     "gpt-5.4",
		APIKeyEnv: "OPENAI_API_KEY",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Name() != "openai-compatible" {
		t.Fatalf("unexpected name %q", client.Name())
	}

	stream, err := client.Start(t.Context(), provider.Request{
		Model:       "gpt-5.4",
		System:      "keep it short",
		Messages:    []provider.Message{{Role: "user", Content: "hi"}},
		Tools:       []provider.ToolSpec{{Name: "read", Description: "Read a file", Schema: json.RawMessage(`{"type":"object"}`)}},
		Temperature: 0.2,
		MaxTokens:   128,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	ev, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "thinking" {
		t.Fatalf("expected thinking event, got %#v", ev)
	}
	ev, err = stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Text != "hello" {
		t.Fatalf("expected streamed text, got %#v", ev)
	}

	if path != "/responses" {
		t.Fatalf("expected responses endpoint, got %q", path)
	}
	if got["instructions"] != "keep it short" {
		t.Fatalf("expected instructions in request, got %#v", got)
	}
	if _, ok := got["temperature"]; ok {
		t.Fatalf("expected temperature to be omitted, got %#v", got)
	}
	if got["max_output_tokens"] != float64(128) {
		t.Fatalf("expected max_output_tokens 128, got %#v", got["max_output_tokens"])
	}

	tools, ok := got["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected one tool, got %#v", got["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected tool payload %#v", tools[0])
	}
	if tool["strict"] != false {
		t.Fatalf("expected strict=false for responses tools, got %#v", tool)
	}
}

func TestStartOmitsAuthorizationHeaderWhenNoAPIKeyConfigured(t *testing.T) {
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\"}\n\n")
	}))
	defer server.Close()

	client, err := New(config.ProviderConfig{
		BaseURL: server.URL,
		Model:   "gateway-model",
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := client.Start(t.Context(), provider.Request{
		Model:    "gateway-model",
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		t.Fatalf("expected no authorization header, got %q", auth)
	}
}
