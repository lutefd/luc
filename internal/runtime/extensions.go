package runtime

import "strings"

const (
	ExtensionModeObserve = "observe"
	ExtensionModeSync    = "sync"
)

const (
	ExtensionFailureModeOpen   = "open"
	ExtensionFailureModeClosed = "closed"
)

const (
	ExtensionEventSessionStart    = "session.start"
	ExtensionEventSessionReload   = "session.reload"
	ExtensionEventSessionShutdown = "session.shutdown"
	ExtensionEventMessageFinal    = "message.assistant.final"
	ExtensionEventToolFinished    = "tool.finished"
	ExtensionEventToolError       = "tool.error"
	ExtensionEventCompactionDone  = "compaction.completed"
	ExtensionEventInputTransform  = "input.transform"
	ExtensionEventPromptContext   = "prompt.context"
	ExtensionEventToolPreflight   = "tool.preflight"
	ExtensionEventToolResult      = "tool.result"
)

type ExtensionRuntime struct {
	Kind    string            `json:"kind"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

type ExtensionSubscription struct {
	Event       string `json:"event"`
	Mode        string `json:"mode"`
	TimeoutMS   int    `json:"timeout_ms,omitempty"`
	FailureMode string `json:"failure_mode,omitempty"`
}

type ExtensionHost struct {
	ID                       string                  `json:"id"`
	ProtocolVersion          int                     `json:"protocol_version"`
	Runtime                  ExtensionRuntime        `json:"runtime"`
	Capabilities             []string                `json:"capabilities,omitempty"`
	Subscriptions            []ExtensionSubscription `json:"subscriptions,omitempty"`
	RequiresHostCapabilities []string                `json:"requires_host_capabilities,omitempty"`
	SourcePath               string                  `json:"source_path,omitempty"`
}

type ExtensionRegistry struct {
	hosts          []ExtensionHost
	observeByEvent map[string][]ExtensionHost
	syncByEvent    map[string][]ExtensionBinding
}

type ExtensionBinding struct {
	Host         ExtensionHost
	Subscription ExtensionSubscription
}

func NewExtensionRegistry(hosts []ExtensionHost) ExtensionRegistry {
	reg := ExtensionRegistry{
		hosts:          append([]ExtensionHost(nil), hosts...),
		observeByEvent: map[string][]ExtensionHost{},
		syncByEvent:    map[string][]ExtensionBinding{},
	}
	for _, host := range reg.hosts {
		for _, subscription := range host.Subscriptions {
			event := strings.TrimSpace(subscription.Event)
			if event == "" {
				continue
			}
			switch strings.TrimSpace(subscription.Mode) {
			case ExtensionModeObserve:
				reg.observeByEvent[event] = append(reg.observeByEvent[event], host)
			case ExtensionModeSync:
				reg.syncByEvent[event] = append(reg.syncByEvent[event], ExtensionBinding{
					Host:         host,
					Subscription: subscription,
				})
			}
		}
	}
	return reg
}

func (r ExtensionRegistry) Hosts() []ExtensionHost {
	out := make([]ExtensionHost, len(r.hosts))
	copy(out, r.hosts)
	return out
}

func (r ExtensionRegistry) ObserveSubscribers(event string) []ExtensionHost {
	return append([]ExtensionHost(nil), r.observeByEvent[strings.TrimSpace(event)]...)
}

func (r ExtensionRegistry) SyncSubscribers(event string) []ExtensionBinding {
	return append([]ExtensionBinding(nil), r.syncByEvent[strings.TrimSpace(event)]...)
}

func ExtensionHostHasCapability(host ExtensionHost, capability string) bool {
	return HasCapability(host.Capabilities, capability)
}

func SupportsObserveEvent(event string) bool {
	switch strings.TrimSpace(event) {
	case ExtensionEventSessionStart,
		ExtensionEventSessionReload,
		ExtensionEventSessionShutdown,
		ExtensionEventMessageFinal,
		ExtensionEventToolFinished,
		ExtensionEventToolError,
		ExtensionEventCompactionDone:
		return true
	default:
		return false
	}
}

func SupportsSyncEvent(event string) bool {
	switch strings.TrimSpace(event) {
	case ExtensionEventInputTransform,
		ExtensionEventPromptContext,
		ExtensionEventToolPreflight,
		ExtensionEventToolResult:
		return true
	default:
		return false
	}
}

type ExtensionHelloEnvelope struct {
	Type             string   `json:"type"`
	ProtocolVersion  int      `json:"protocol_version"`
	ExtensionID      string   `json:"extension_id,omitempty"`
	HostCapabilities []string `json:"host_capabilities,omitempty"`
}

type ExtensionReadyEnvelope struct {
	Type            string `json:"type"`
	ProtocolVersion int    `json:"protocol_version"`
}

type ExtensionSessionEnvelope struct {
	Type      string         `json:"type"`
	Session   map[string]any `json:"session,omitempty"`
	Workspace map[string]any `json:"workspace,omitempty"`
}

type ExtensionShutdownEnvelope struct {
	Type   string `json:"type"`
	Reason string `json:"reason,omitempty"`
}

type ExtensionStorageSnapshotEnvelope struct {
	Type      string `json:"type"`
	Session   any    `json:"session,omitempty"`
	Workspace any    `json:"workspace,omitempty"`
}

type ExtensionEventEnvelope struct {
	Type      string         `json:"type"`
	Event     string         `json:"event"`
	RequestID string         `json:"request_id,omitempty"`
	Sequence  uint64         `json:"sequence,omitempty"`
	At        string         `json:"at,omitempty"`
	Payload   any            `json:"payload,omitempty"`
	Session   map[string]any `json:"session,omitempty"`
	Workspace map[string]any `json:"workspace,omitempty"`
}

type ExtensionDecisionEnvelope struct {
	Type                string         `json:"type"`
	RequestID           string         `json:"request_id,omitempty"`
	Decision            string         `json:"decision,omitempty"`
	Message             string         `json:"message,omitempty"`
	Text                string         `json:"text,omitempty"`
	SystemAppend        []string       `json:"system_append,omitempty"`
	HiddenContext       []string       `json:"hidden_context,omitempty"`
	Arguments           map[string]any `json:"arguments,omitempty"`
	Content             string         `json:"content,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`
	Error               string         `json:"error,omitempty"`
	CollapsedSummary    string         `json:"collapsed_summary,omitempty"`
	ErrorClassification string         `json:"error_classification,omitempty"`
}

type ExtensionHostEvent struct {
	Type                string              `json:"type"`
	RequestID           string              `json:"request_id,omitempty"`
	Text                string              `json:"text,omitempty"`
	Message             string              `json:"message,omitempty"`
	Progress            string              `json:"progress,omitempty"`
	Action              *UIAction           `json:"action,omitempty"`
	Decision            string              `json:"decision,omitempty"`
	SystemAppend        []string            `json:"system_append,omitempty"`
	HiddenContext       []string            `json:"hidden_context,omitempty"`
	Arguments           map[string]any      `json:"arguments,omitempty"`
	Content             string              `json:"content,omitempty"`
	Metadata            map[string]any      `json:"metadata,omitempty"`
	Result              *ToolResultEnvelope `json:"result,omitempty"`
	Tools               []DynamicToolDef    `json:"tools,omitempty"`
	Error               string              `json:"error,omitempty"`
	CollapsedSummary    string              `json:"collapsed_summary,omitempty"`
	ErrorClassification string              `json:"error_classification,omitempty"`
	Scope               string              `json:"scope,omitempty"`
	Value               any                 `json:"value,omitempty"`
	Data                map[string]any      `json:"data,omitempty"`
	ProtocolVersion     int                 `json:"protocol_version,omitempty"`
	Done                bool                `json:"done,omitempty"`
}

func DefaultSyncTimeoutMS(event string) int {
	switch strings.TrimSpace(event) {
	case ExtensionEventInputTransform:
		return 300
	case ExtensionEventPromptContext:
		return 500
	case ExtensionEventToolPreflight:
		return 500
	case ExtensionEventToolResult:
		return 750
	default:
		return 500
	}
}
