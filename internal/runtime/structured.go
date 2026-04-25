package runtime

type ToolRequestEnvelope struct {
	ToolName         string         `json:"tool_name"`
	Arguments        map[string]any `json:"arguments,omitempty"`
	Workspace        string         `json:"workspace,omitempty"`
	SessionID        string         `json:"session_id,omitempty"`
	AgentID          string         `json:"agent_id,omitempty"`
	HostCapabilities []string       `json:"host_capabilities,omitempty"`
	ViewContext      *ViewContext   `json:"view_context,omitempty"`
}

type ToolEvent struct {
	Type   string              `json:"type"`
	Text   string              `json:"text,omitempty"`
	Action *UIAction           `json:"action,omitempty"`
	Result *ToolResultEnvelope `json:"result,omitempty"`
	Error  string              `json:"error,omitempty"`
	Data   map[string]any      `json:"data,omitempty"`
	Done   bool                `json:"done,omitempty"`
}

type ToolResultEnvelope struct {
	Content          string         `json:"content,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	DefaultCollapsed bool           `json:"default_collapsed,omitempty"`
	CollapsedSummary string         `json:"collapsed_summary,omitempty"`
}

type ClientResultEnvelope struct {
	Type   string   `json:"type"`
	Result UIResult `json:"result"`
}

type DynamicToolDef struct {
	Name           string           `json:"name"`
	Description    string           `json:"description,omitempty"`
	InputSchema    map[string]any   `json:"input_schema,omitempty"`
	Schema         map[string]any   `json:"schema,omitempty"`
	Handler        string           `json:"handler,omitempty"`
	TimeoutSeconds int              `json:"timeout_seconds,omitempty"`
	UI             DynamicToolUIDef `json:"ui,omitempty"`
}

type DynamicToolUIDef struct {
	DefaultCollapsed bool   `json:"default_collapsed,omitempty"`
	CollapsedSummary string `json:"collapsed_summary,omitempty"`
}

type HostedToolInvokeEnvelope struct {
	Type      string              `json:"type"`
	RequestID string              `json:"request_id,omitempty"`
	Handler   string              `json:"handler"`
	Tool      ToolRequestEnvelope `json:"tool"`
}

type HookRequestEnvelope struct {
	Event            any            `json:"event"`
	Workspace        map[string]any `json:"workspace,omitempty"`
	Session          map[string]any `json:"session,omitempty"`
	HostCapabilities []string       `json:"host_capabilities,omitempty"`
}

type HookEvent struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	Message  string         `json:"message,omitempty"`
	Action   *UIAction      `json:"action,omitempty"`
	Progress string         `json:"progress,omitempty"`
	Error    string         `json:"error,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Done     bool           `json:"done,omitempty"`
}
