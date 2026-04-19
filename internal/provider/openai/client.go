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
	key := ""
	if env := strings.TrimSpace(cfg.APIKeyEnv); env != "" {
		key = os.Getenv(env)
		if key == "" {
			return nil, fmt.Errorf("%s is not set", env)
		}
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
	body          io.Closer
	scanner       *bufio.Scanner
	pending       []provider.Event
	calls         map[int]provider.ToolCall
	thinkingShown bool
}

func (c *Client) Start(ctx context.Context, req provider.Request) (provider.Stream, error) {
	payload := responseRequest{
		Model:           firstNonEmpty(req.Model, c.model),
		Instructions:    req.System,
		Temperature:     req.Temperature,
		MaxOutputTokens: req.MaxTokens,
		Stream:          true,
		Input:           responseInputFromProvider(req.Messages),
		Tools:           responseToolsFromProvider(req.Tools),
	}
	applyModelCompatibility(&payload)

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

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
			return provider.Event{Type: "done", Completed: true}, nil
		}

		var ev responseStreamEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return provider.Event{}, err
		}

		switch ev.Type {
		case "response.created", "response.in_progress":
			s.queueThinking()
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta",
			"response.reasoning_summary_part.added", "response.reasoning_text.done":
			s.queueThinking()
		case "response.output_text.delta":
			if ev.Delta != "" {
				return provider.Event{Type: "text_delta", Text: ev.Delta}, nil
			}
		case "response.output_item.added":
			if ev.Item.Type == "function_call" {
				s.calls[ev.OutputIndex] = toolCallFromResponseItem(ev.Item, ev.OutputIndex)
			}
			if strings.HasPrefix(ev.Item.Type, "reasoning") {
				s.queueThinking()
			}
		case "response.function_call_arguments.delta":
			call := s.calls[ev.OutputIndex]
			if call.ID == "" && ev.CallID != "" {
				call.ID = ev.CallID
			}
			if call.Name == "" && ev.Name != "" {
				call.Name = ev.Name
			}
			call.Arguments += ev.Delta
			s.calls[ev.OutputIndex] = call
		case "response.function_call_arguments.done":
			call := s.calls[ev.OutputIndex]
			if ev.Item.Type == "function_call" {
				call = toolCallFromResponseItem(ev.Item, ev.OutputIndex)
			}
			if call.ID == "" && ev.CallID != "" {
				call.ID = ev.CallID
			}
			if call.Name == "" && ev.Name != "" {
				call.Name = ev.Name
			}
			if ev.Arguments != "" {
				call.Arguments = ev.Arguments
			}
			if call.ID == "" {
				call.ID = fmt.Sprintf("tool_%d", ev.OutputIndex)
			}
			return provider.Event{Type: "tool_call", ToolCall: call}, nil
		case "response.completed":
			return provider.Event{Type: "done", Completed: true}, nil
		case "response.failed", "error":
			return provider.Event{}, streamError(ev)
		}

		if len(s.pending) > 0 {
			return s.Recv()
		}
	}

	if err := s.scanner.Err(); err != nil {
		return provider.Event{}, err
	}

	return provider.Event{}, io.EOF
}

func (s *stream) queueThinking() {
	if s.thinkingShown {
		return
	}
	s.thinkingShown = true
	s.pending = append(s.pending, provider.Event{Type: "thinking", Text: "Thinking..."})
}

func (s *stream) Close() error {
	if s.body == nil {
		return nil
	}
	return s.body.Close()
}

type responseRequest struct {
	Model           string         `json:"model"`
	Instructions    string         `json:"instructions,omitempty"`
	Input           []any          `json:"input,omitempty"`
	Tools           []responseTool `json:"tools,omitempty"`
	Temperature     float32        `json:"temperature,omitempty"`
	MaxOutputTokens int            `json:"max_output_tokens,omitempty"`
	Stream          bool           `json:"stream"`
}

type responseTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      bool            `json:"strict"`
}

type responseMessageInput struct {
	Type    string                `json:"type"`
	Role    string                `json:"role"`
	Content []responseContentPart `json:"content"`
}

type responseContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type responseFunctionCallItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type responseFunctionCallOutputItem struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

type responseStreamEvent struct {
	Type        string              `json:"type"`
	Delta       string              `json:"delta"`
	Name        string              `json:"name"`
	Arguments   string              `json:"arguments"`
	CallID      string              `json:"call_id"`
	OutputIndex int                 `json:"output_index"`
	Item        responseStreamItem  `json:"item"`
	Error       *responseErrorField `json:"error"`
}

type responseStreamItem struct {
	Type      string `json:"type"`
	ID        string `json:"id,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type responseErrorField struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

func responseInputFromProvider(messages []provider.Message) []any {
	out := make([]any, 0, len(messages))
	for _, msg := range messages {
		switch {
		case len(msg.ToolCalls) > 0:
			for _, call := range msg.ToolCalls {
				out = append(out, responseFunctionCallItem{
					Type:      "function_call",
					CallID:    call.ID,
					Name:      call.Name,
					Arguments: call.Arguments,
				})
			}
		case msg.Role == "tool":
			out = append(out, responseFunctionCallOutputItem{
				Type:   "function_call_output",
				CallID: msg.ToolCallID,
				Output: msg.Content,
			})
		default:
			parts := responsePartsFromMessage(msg)
			if len(parts) == 0 {
				continue
			}
			out = append(out, responseMessageInput{
				Type:    "message",
				Role:    msg.Role,
				Content: parts,
			})
		}
	}
	return out
}

func responsePartsFromMessage(msg provider.Message) []responseContentPart {
	parts := msg.ContentParts()
	if len(parts) == 0 {
		return nil
	}

	out := make([]responseContentPart, 0, len(parts))
	for _, part := range parts {
		switch strings.TrimSpace(part.Type) {
		case "", "text":
			contentType := "input_text"
			if msg.Role == "assistant" {
				contentType = "output_text"
			}
			out = append(out, responseContentPart{
				Type: contentType,
				Text: part.Text,
			})
		case "image":
			imageURL := strings.TrimSpace(part.URL)
			if imageURL == "" && strings.TrimSpace(part.Data) != "" && strings.TrimSpace(part.MediaType) != "" {
				imageURL = "data:" + part.MediaType + ";base64," + part.Data
			}
			if imageURL == "" {
				continue
			}
			out = append(out, responseContentPart{
				Type:     "input_image",
				ImageURL: imageURL,
			})
		}
	}
	return out
}

func responseToolsFromProvider(specs []provider.ToolSpec) []responseTool {
	if len(specs) == 0 {
		return nil
	}
	out := make([]responseTool, 0, len(specs))
	for _, spec := range specs {
		out = append(out, responseTool{
			Type:        "function",
			Name:        spec.Name,
			Description: spec.Description,
			Parameters:  spec.Schema,
			Strict:      false,
		})
	}
	return out
}

func applyModelCompatibility(req *responseRequest) {
	model := strings.ToLower(strings.TrimSpace(req.Model))
	if req.Temperature != 0 && isGPT5Model(model) {
		req.Temperature = 0
	}
}

func isGPT5Model(model string) bool {
	return strings.HasPrefix(model, "gpt-5")
}

func toolCallFromResponseItem(item responseStreamItem, outputIndex int) provider.ToolCall {
	call := provider.ToolCall{
		ID:        item.CallID,
		Name:      item.Name,
		Arguments: item.Arguments,
	}
	if call.ID == "" {
		call.ID = fmt.Sprintf("tool_%d", outputIndex)
	}
	return call
}

func streamError(ev responseStreamEvent) error {
	if ev.Error == nil {
		return errors.New("provider stream failed")
	}
	msg := strings.TrimSpace(ev.Error.Message)
	if msg == "" {
		msg = "provider stream failed"
	}
	return errors.New(msg)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var _ provider.Provider = (*Client)(nil)

var ErrUnsupported = errors.New("unsupported provider")
