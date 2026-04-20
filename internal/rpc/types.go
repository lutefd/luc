package rpc

import (
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/logging"
	luruntime "github.com/lutefd/luc/internal/runtime"
	"github.com/lutefd/luc/internal/tools"
	"github.com/lutefd/luc/internal/workspace"
)

const ProtocolVersion = "luc-rpc-v1"

type AttachmentInput struct {
	Type      string `json:"type"`
	Name      string `json:"name,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

type Command struct {
	ID           string            `json:"id,omitempty"`
	Type         string            `json:"type"`
	Scope        string            `json:"scope,omitempty"`
	SinceSeq     uint64            `json:"since_seq,omitempty"`
	SessionID    string            `json:"session_id,omitempty"`
	ProviderID   string            `json:"provider_id,omitempty"`
	ModelID      string            `json:"model_id,omitempty"`
	Message      string            `json:"message,omitempty"`
	Attachments  []AttachmentInput `json:"attachments,omitempty"`
	Instructions string            `json:"instructions,omitempty"`
	ViewID       string            `json:"view_id,omitempty"`
	ActionID     string            `json:"action_id,omitempty"`
	Accepted     bool              `json:"accepted,omitempty"`
	ChoiceID     string            `json:"choice_id,omitempty"`
	Data         map[string]any    `json:"data,omitempty"`
}

type Response struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

type EventFrame struct {
	Type  string                `json:"type"`
	Event history.EventEnvelope `json:"event"`
}

type WorkspaceState struct {
	Root      string `json:"root"`
	ProjectID string `json:"project_id"`
	Branch    string `json:"branch,omitempty"`
	HasGit    bool   `json:"has_git"`
}

type SessionState struct {
	Meta              history.SessionMeta `json:"meta"`
	Saved             bool                `json:"saved"`
	VisibleEventCount int                 `json:"visible_event_count"`
	RawEventCount     int                 `json:"raw_event_count"`
}

type ProviderState struct {
	Kind  string `json:"kind"`
	Model string `json:"model"`
}

type StateResponseData struct {
	ProtocolVersion  string         `json:"protocol_version"`
	Workspace        WorkspaceState `json:"workspace"`
	Session          SessionState   `json:"session"`
	Provider         ProviderState  `json:"provider"`
	TurnActive       bool           `json:"turn_active"`
	ApprovalsMode    string         `json:"approvals_mode"`
	HostCapabilities []string       `json:"host_capabilities,omitempty"`
}

type EventsResponseData struct {
	Scope   string                  `json:"scope"`
	Events  []history.EventEnvelope `json:"events"`
	LastSeq uint64                  `json:"last_seq"`
}

type LogsResponseData struct {
	Entries []logging.Entry `json:"entries"`
}

type SessionSwitchResponseData struct {
	State  StateResponseData       `json:"state"`
	Events []history.EventEnvelope `json:"events"`
}

type RuntimeUIResponseData struct {
	Commands    []luruntime.RuntimeCommand `json:"commands"`
	Views       []luruntime.RuntimeView    `json:"views"`
	Diagnostics []luruntime.Diagnostic     `json:"diagnostics"`
}

type RenderViewResponseData struct {
	View         luruntime.RuntimeView `json:"view"`
	Result       tools.Result          `json:"result"`
	RenderedText string                `json:"rendered_text"`
}

func buildWorkspaceState(info workspace.Info) WorkspaceState {
	return WorkspaceState{
		Root:      info.Root,
		ProjectID: info.ProjectID,
		Branch:    info.Branch,
		HasGit:    info.HasGit,
	}
}
