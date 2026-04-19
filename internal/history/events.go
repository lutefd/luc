package history

import "time"

type EventEnvelope struct {
	Seq        uint64    `json:"seq"`
	At         time.Time `json:"at"`
	SessionID  string    `json:"session_id"`
	AgentID    string    `json:"agent_id"`
	ParentTask string    `json:"parent_task,omitempty"`
	Kind       string    `json:"kind"`
	Payload    any       `json:"payload"`
}

type SessionMeta struct {
	SessionID string    `json:"session_id"`
	ProjectID string    `json:"project_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	Title     string    `json:"title"`
}

type MessagePayload struct {
	ID          string              `json:"id"`
	Content     string              `json:"content"`
	Attachments []AttachmentPayload `json:"attachments,omitempty"`
}

type AttachmentPayload struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}

type MessageDeltaPayload struct {
	ID    string `json:"id"`
	Delta string `json:"delta"`
}

type ToolCallPayload struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolCallBatchPayload struct {
	ID    string            `json:"id"`
	Calls []ToolCallPayload `json:"calls"`
}

type ToolResultPayload struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Error    string         `json:"error,omitempty"`
}

type ReloadPayload struct {
	Version uint64 `json:"version"`
	Error   string `json:"error,omitempty"`
}

type StatusPayload struct {
	Text string `json:"text"`
}
