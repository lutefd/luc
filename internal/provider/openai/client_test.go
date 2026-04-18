package openai

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/provider"
)

func TestNewRequiresAPIKeyEnv(t *testing.T) {
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

func TestStreamRecvParsesTextAndToolCalls(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"hello "}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"bash","arguments":"{\"command\":\"printf hi\"}"}}]}}]}`,
		`data: [DONE]`,
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
	if ev.Type != "text_delta" || ev.Text != "hello " {
		t.Fatalf("unexpected first event: %#v", ev)
	}

	ev, err = s.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "tool_call" || ev.ToolCall.Name != "bash" {
		t.Fatalf("unexpected tool call event: %#v", ev)
	}

	ev, err = s.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "done" || !ev.Completed {
		t.Fatalf("expected done event, got %#v", ev)
	}
}

func TestChatMessageFromProviderToolCallsUsesArguments(t *testing.T) {
	msg := chatMessageFromProvider(provider.Message{
		Role: "assistant",
		ToolCalls: []provider.ToolCall{
			{ID: "call_1", Name: "read", Arguments: `{"path":"go.mod"}`},
		},
	})

	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %#v", msg.ToolCalls)
	}
	if msg.ToolCalls[0].Function.Arguments != `{"path":"go.mod"}` {
		t.Fatalf("expected arguments field, got %#v", msg.ToolCalls[0].Function)
	}
}

func TestStartNameAndClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	t.Setenv("OPENAI_API_KEY", "token")
	client, err := New(config.ProviderConfig{
		BaseURL:   server.URL,
		Model:     "gpt-test",
		APIKeyEnv: "OPENAI_API_KEY",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Name() != "openai-compatible" {
		t.Fatalf("unexpected name %q", client.Name())
	}

	stream, err := client.Start(t.Context(), provider.Request{Model: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	ev, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if ev.Text != "hello" {
		t.Fatalf("expected streamed text, got %#v", ev)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
}
