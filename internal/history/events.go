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
	Synthetic   bool                `json:"synthetic,omitempty"`
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

type UIActionPayload struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Blocking  bool           `json:"blocking,omitempty"`
	Title     string         `json:"title,omitempty"`
	Body      string         `json:"body,omitempty"`
	ViewID    string         `json:"view_id,omitempty"`
	CommandID string         `json:"command_id,omitempty"`
	Context   map[string]any `json:"context,omitempty"`
}

type UIResultPayload struct {
	ActionID string         `json:"action_id"`
	Accepted bool           `json:"accepted,omitempty"`
	ChoiceID string         `json:"choice_id,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

type HookPayload struct {
	HookID     string `json:"hook_id"`
	EventKind  string `json:"event_kind,omitempty"`
	SourcePath string `json:"source_path,omitempty"`
	Error      string `json:"error,omitempty"`
}
