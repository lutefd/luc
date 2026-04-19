package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type ContentPart struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type Message struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	Parts      []ContentPart `json:"parts,omitempty"`
	Name       string        `json:"name,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
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
	Model       string     `json:"model"`
	System      string     `json:"system,omitempty"`
	Messages    []Message  `json:"messages,omitempty"`
	Tools       []ToolSpec `json:"tools,omitempty"`
	Temperature float32    `json:"temperature,omitempty"`
	MaxTokens   int        `json:"max_tokens,omitempty"`
}

type Provider interface {
	Name() string
	Start(ctx context.Context, req Request) (Stream, error)
}

var ErrExceededToolLimits = errors.New("exceeded_tool_limits")

func (m Message) ContentParts() []ContentPart {
	if len(m.Parts) > 0 {
		out := make([]ContentPart, len(m.Parts))
		copy(out, m.Parts)
		return out
	}
	if m.Content == "" {
		return nil
	}
	return []ContentPart{{
		Type: "text",
		Text: m.Content,
	}}
}

func (m Message) TextContent() string {
	if len(m.Parts) == 0 {
		return m.Content
	}
	var builder strings.Builder
	for _, part := range m.Parts {
		if part.Type == "text" {
			builder.WriteString(part.Text)
		}
	}
	return builder.String()
}
