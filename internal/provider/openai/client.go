package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/provider"
)

type Client struct {
	baseURL    string
	model      string
	apiKey     string
	httpClient *http.Client
}

func New(cfg config.ProviderConfig) (*Client, error) {
	key := os.Getenv(cfg.APIKeyEnv)
	if key == "" {
		return nil, fmt.Errorf("%s is not set", cfg.APIKeyEnv)
	}

	return &Client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		model:      cfg.Model,
		apiKey:     key,
		httpClient: http.DefaultClient,
	}, nil
}

func (c *Client) Name() string {
	return "openai-compatible"
}

type stream struct {
	body    io.Closer
	scanner *bufio.Scanner
	pending []provider.Event
	calls   map[int]provider.ToolCall
}

func (c *Client) Start(ctx context.Context, req provider.Request) (provider.Stream, error) {
	payload := chatCompletionRequest{
		Model:       req.Model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      true,
		Messages:    make([]chatMessage, 0, len(req.Messages)+1),
		Tools:       make([]chatTool, 0, len(req.Tools)),
	}

	if req.System != "" {
		payload.Messages = append(payload.Messages, chatMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	for _, msg := range req.Messages {
		payload.Messages = append(payload.Messages, chatMessageFromProvider(msg))
	}

	for _, spec := range req.Tools {
		payload.Tools = append(payload.Tools, chatTool{
			Type: "function",
			Function: chatToolFunction{
				Name:        spec.Name,
				Description: spec.Description,
				Parameters:  spec.Schema,
			},
		})
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	return &stream{
		body:    resp.Body,
		scanner: scanner,
		calls:   make(map[int]provider.ToolCall),
	}, nil
}

func (s *stream) Recv() (provider.Event, error) {
	if len(s.pending) > 0 {
		ev := s.pending[0]
		s.pending = s.pending[1:]
		return ev, nil
	}

	for s.scanner.Scan() {
		line := strings.TrimSpace(s.scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			for i := 0; i < len(s.calls); i++ {
				call := s.calls[i]
				if call.ID == "" {
					call.ID = fmt.Sprintf("tool_%d", i)
				}
				s.pending = append(s.pending, provider.Event{
					Type:     "tool_call",
					ToolCall: call,
				})
			}
			s.pending = append(s.pending, provider.Event{Type: "done", Completed: true})
			return s.Recv()
		}

		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return provider.Event{}, err
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			return provider.Event{Type: "text_delta", Text: choice.Delta.Content}, nil
		}

		for _, tc := range choice.Delta.ToolCalls {
			call := s.calls[tc.Index]
			if tc.ID != "" {
				call.ID = tc.ID
			}
			if tc.Function.Name != "" {
				call.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				call.Arguments += tc.Function.Arguments
			}
			s.calls[tc.Index] = call
		}
	}

	if err := s.scanner.Err(); err != nil {
		return provider.Event{}, err
	}

	return provider.Event{}, io.EOF
}

func (s *stream) Close() error {
	if s.body == nil {
		return nil
	}
	return s.body.Close()
}

type chatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Tools       []chatTool    `json:"tools,omitempty"`
	Temperature float32       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream"`
}

type chatMessage struct {
	Role       string                `json:"role"`
	Content    any                   `json:"content,omitempty"`
	Name       string                `json:"name,omitempty"`
	ToolCallID string                `json:"tool_call_id,omitempty"`
	ToolCalls  []chatMessageToolCall `json:"tool_calls,omitempty"`
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatToolCall struct {
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatMessageToolCall struct {
	ID       string                  `json:"id,omitempty"`
	Type     string                  `json:"type"`
	Function chatMessageToolFunction `json:"function"`
}

type chatMessageToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatCompletionChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

func chatMessageFromProvider(msg provider.Message) chatMessage {
	out := chatMessage{
		Role:       msg.Role,
		Name:       msg.Name,
		ToolCallID: msg.ToolCallID,
	}

	switch {
	case len(msg.ToolCalls) > 0:
		out.ToolCalls = make([]chatMessageToolCall, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			out.ToolCalls = append(out.ToolCalls, chatMessageToolCall{
				ID:   call.ID,
				Type: "function",
				Function: chatMessageToolFunction{
					Name:      call.Name,
					Arguments: call.Arguments,
				},
			})
		}
	case msg.Role == "tool":
		out.Content = msg.Content
	default:
		out.Content = msg.Content
	}

	return out
}

var _ provider.Provider = (*Client)(nil)

var ErrUnsupported = errors.New("unsupported provider")
