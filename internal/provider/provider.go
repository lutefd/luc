package provider

import (
	"context"
	"encoding/json"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolSpec struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

type Event struct {
	Type      string
	Text      string
	ToolCall  ToolCall
	Error     error
	Usage     map[string]any
	Completed bool
}

type Stream interface {
	Recv() (Event, error)
	Close() error
}

type Request struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []ToolSpec
	Temperature float32
	MaxTokens   int
}

type Provider interface {
	Name() string
	Start(ctx context.Context, req Request) (Stream, error)
}
