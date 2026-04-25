package runtime

import "strings"

const (
	CapabilityStructuredIO = "structured_io"
	CapabilityClientAction = "client_actions"
)

const (
	HostCapabilityUIModal                 = "ui.modal"
	HostCapabilityUIConfirm               = "ui.confirm"
	HostCapabilityUIViewOpen              = "ui.view.open"
	HostCapabilityUICommand               = "ui.command"
	HostCapabilityUICommandShortcut       = "ui.command.shortcut"
	HostCapabilityUIToolRun               = "tool.run"
	HostCapabilityLiveHooks               = "hooks.live_events"
	HostCapabilityExtensionObserveEvents  = "extensions.observe_events"
	HostCapabilityExtensionSessionStorage = "extensions.storage.session"
	HostCapabilityExtensionWorkspaceStore = "extensions.storage.workspace"
)

type UIAction struct {
	ID        string         `json:"id,omitempty"`
	Kind      string         `json:"kind"`
	Blocking  bool           `json:"blocking,omitempty"`
	Title     string         `json:"title,omitempty"`
	Body      string         `json:"body,omitempty"`
	Render    string         `json:"render,omitempty"`
	Input     UIActionInput  `json:"input,omitempty"`
	ViewID    string         `json:"view_id,omitempty"`
	CommandID string         `json:"command_id,omitempty"`
	ToolName  string         `json:"tool_name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Result    UIActionResult `json:"result,omitempty"`
	Options   []UIOption     `json:"options,omitempty"`
	Context   map[string]any `json:"context,omitempty"`
}

type UIActionInput struct {
	Enabled     bool   `json:"enabled,omitempty"`
	Multiline   bool   `json:"multiline,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Value       string `json:"value,omitempty"`
}

type UIActionResult struct {
	Presentation string `json:"presentation,omitempty"`
}

type UIOption struct {
	ID      string `json:"id,omitempty"`
	Label   string `json:"label,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

type UIResult struct {
	ActionID string         `json:"action_id,omitempty"`
	Accepted bool           `json:"accepted,omitempty"`
	ChoiceID string         `json:"choice_id,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

type ViewContext struct {
	ViewID    string         `json:"view_id,omitempty"`
	Placement string         `json:"placement,omitempty"`
	Context   map[string]any `json:"context,omitempty"`
}

type HostRequirementResult struct {
	Missing []string
}

func (r HostRequirementResult) Supported() bool {
	return len(r.Missing) == 0
}

func NormalizeCapabilities(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func HasCapability(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func CheckHostRequirements(required, supported []string) HostRequirementResult {
	required = NormalizeCapabilities(required)
	supported = NormalizeCapabilities(supported)
	if len(required) == 0 {
		return HostRequirementResult{}
	}

	supportedSet := make(map[string]struct{}, len(supported))
	for _, capability := range supported {
		supportedSet[capability] = struct{}{}
	}

	result := HostRequirementResult{}
	for _, capability := range required {
		if _, ok := supportedSet[capability]; ok {
			continue
		}
		result.Missing = append(result.Missing, capability)
	}
	return result
}

func DefaultHostCapabilities() []string {
	return []string{
		HostCapabilityUIModal,
		HostCapabilityUIConfirm,
		HostCapabilityUIViewOpen,
		HostCapabilityUICommand,
		HostCapabilityUICommandShortcut,
		HostCapabilityUIToolRun,
		HostCapabilityLiveHooks,
		HostCapabilityExtensionObserveEvents,
		HostCapabilityExtensionSessionStorage,
		HostCapabilityExtensionWorkspaceStore,
	}
}
