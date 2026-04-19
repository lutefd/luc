package runtime

import (
	"path/filepath"
	"strings"
)

type Diagnostic struct {
	SourcePath string `json:"source_path,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Message    string `json:"message"`
}

type RuntimeCommand struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ActionKind string `json:"action_kind,omitempty"`
	ViewID     string `json:"view_id,omitempty"`
	CommandID  string `json:"command_id,omitempty"`
	SourcePath string `json:"source_path,omitempty"`
}

type RuntimeView struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Placement  string `json:"placement"`
	SourceTool string `json:"source_tool"`
	Render     string `json:"render"`
	SourcePath string `json:"source_path,omitempty"`
}

type ApprovalPolicy struct {
	ID           string   `json:"id"`
	ToolNames    []string `json:"tool_names,omitempty"`
	Mode         string   `json:"mode"`
	Title        string   `json:"title,omitempty"`
	BodyTemplate string   `json:"body_template,omitempty"`
	ConfirmLabel string   `json:"confirm_label,omitempty"`
	CancelLabel  string   `json:"cancel_label,omitempty"`
	SourcePath   string   `json:"source_path,omitempty"`
}

type HookRuntime struct {
	Kind         string   `json:"kind"`
	Command      string   `json:"command"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type HookDelivery struct {
	Mode           string `json:"mode"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type HookSubscription struct {
	ID          string       `json:"id"`
	Description string       `json:"description,omitempty"`
	Events      []string     `json:"events,omitempty"`
	Runtime     HookRuntime  `json:"runtime"`
	Delivery    HookDelivery `json:"delivery"`
	SourcePath  string       `json:"source_path,omitempty"`
}

type UIRegistry struct {
	commands  []RuntimeCommand
	views     []RuntimeView
	policies  []ApprovalPolicy
	commandBy map[string]RuntimeCommand
	viewBy    map[string]RuntimeView
}

type HookRegistry struct {
	hooks   []HookSubscription
	byEvent map[string][]HookSubscription
}

type ContributionSet struct {
	UI          UIRegistry
	Hooks       HookRegistry
	Diagnostics []Diagnostic
}

func NewUIRegistry(commands []RuntimeCommand, views []RuntimeView, policies []ApprovalPolicy) UIRegistry {
	reg := UIRegistry{
		commands:  append([]RuntimeCommand(nil), commands...),
		views:     append([]RuntimeView(nil), views...),
		policies:  append([]ApprovalPolicy(nil), policies...),
		commandBy: make(map[string]RuntimeCommand, len(commands)),
		viewBy:    make(map[string]RuntimeView, len(views)),
	}
	for _, command := range reg.commands {
		reg.commandBy[strings.ToLower(command.ID)] = command
	}
	for _, view := range reg.views {
		reg.viewBy[strings.ToLower(view.ID)] = view
	}
	return reg
}

func (r UIRegistry) Commands() []RuntimeCommand {
	out := make([]RuntimeCommand, len(r.commands))
	copy(out, r.commands)
	return out
}

func (r UIRegistry) Views() []RuntimeView {
	out := make([]RuntimeView, len(r.views))
	copy(out, r.views)
	return out
}

func (r UIRegistry) InspectorViews() []RuntimeView {
	var out []RuntimeView
	for _, view := range r.views {
		if strings.EqualFold(view.Placement, "inspector_tab") {
			out = append(out, view)
		}
	}
	return out
}

func (r UIRegistry) PageViews() []RuntimeView {
	var out []RuntimeView
	for _, view := range r.views {
		if strings.EqualFold(view.Placement, "page") {
			out = append(out, view)
		}
	}
	return out
}

func (r UIRegistry) Command(id string) (RuntimeCommand, bool) {
	command, ok := r.commandBy[strings.ToLower(strings.TrimSpace(id))]
	return command, ok
}

func (r UIRegistry) View(id string) (RuntimeView, bool) {
	view, ok := r.viewBy[strings.ToLower(strings.TrimSpace(id))]
	return view, ok
}

func (r UIRegistry) ApprovalPolicies() []ApprovalPolicy {
	out := make([]ApprovalPolicy, len(r.policies))
	copy(out, r.policies)
	return out
}

func (r UIRegistry) ApprovalPolicyForTool(toolName string) (ApprovalPolicy, bool) {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return ApprovalPolicy{}, false
	}
	for i := len(r.policies) - 1; i >= 0; i-- {
		policy := r.policies[i]
		for _, candidate := range policy.ToolNames {
			if strings.EqualFold(strings.TrimSpace(candidate), toolName) {
				return policy, true
			}
		}
	}
	return ApprovalPolicy{}, false
}

func NewHookRegistry(hooks []HookSubscription) HookRegistry {
	reg := HookRegistry{
		hooks:   append([]HookSubscription(nil), hooks...),
		byEvent: map[string][]HookSubscription{},
	}
	for _, hook := range reg.hooks {
		for _, event := range hook.Events {
			event = strings.TrimSpace(event)
			if event == "" {
				continue
			}
			reg.byEvent[event] = append(reg.byEvent[event], hook)
		}
	}
	return reg
}

func (r HookRegistry) Hooks() []HookSubscription {
	out := make([]HookSubscription, len(r.hooks))
	copy(out, r.hooks)
	return out
}

func (r HookRegistry) Subscribers(event string) []HookSubscription {
	out := append([]HookSubscription(nil), r.byEvent[strings.TrimSpace(event)]...)
	return out
}

func DiagnosticForMissingCapabilities(sourcePath, kind string, missing []string) Diagnostic {
	name := strings.TrimSpace(filepath.Base(sourcePath))
	if name == "" {
		name = sourcePath
	}
	return Diagnostic{
		SourcePath: sourcePath,
		Kind:       kind,
		Message:    name + " skipped because the host is missing required capabilities: " + strings.Join(missing, ", "),
	}
}

func BaseName(path string) string {
	return strings.TrimSpace(filepath.Base(path))
}
