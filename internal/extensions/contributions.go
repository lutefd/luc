package extensions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	luruntime "github.com/lutefd/luc/internal/runtime"
	"gopkg.in/yaml.v3"
)

type uiManifest struct {
	Schema                   string   `yaml:"schema" json:"schema"`
	ID                       string   `yaml:"id" json:"id"`
	RequiresHostCapabilities []string `yaml:"requires_host_capabilities" json:"requires_host_capabilities"`
	Commands                 []struct {
		ID          string             `yaml:"id" json:"id"`
		Name        string             `yaml:"name" json:"name"`
		Description string             `yaml:"description" json:"description"`
		Category    string             `yaml:"category" json:"category"`
		Shortcut    string             `yaml:"shortcut" json:"shortcut"`
		Action      viewActionManifest `yaml:"action" json:"action"`
	} `yaml:"commands" json:"commands"`
	Views []struct {
		ID         string `yaml:"id" json:"id"`
		Title      string `yaml:"title" json:"title"`
		Placement  string `yaml:"placement" json:"placement"`
		SourceTool string `yaml:"source_tool" json:"source_tool"`
		Render     string `yaml:"render" json:"render"`
		Actions    []struct {
			ID       string             `yaml:"id" json:"id"`
			Label    string             `yaml:"label" json:"label"`
			Shortcut string             `yaml:"shortcut" json:"shortcut"`
			Action   viewActionManifest `yaml:"action" json:"action"`
		} `yaml:"actions" json:"actions"`
	} `yaml:"views" json:"views"`
	ApprovalPolicies []struct {
		ID           string   `yaml:"id" json:"id"`
		ToolNames    []string `yaml:"tool_names" json:"tool_names"`
		Mode         string   `yaml:"mode" json:"mode"`
		Title        string   `yaml:"title" json:"title"`
		BodyTemplate string   `yaml:"body_template" json:"body_template"`
		ConfirmLabel string   `yaml:"confirm_label" json:"confirm_label"`
		CancelLabel  string   `yaml:"cancel_label" json:"cancel_label"`
	} `yaml:"approval_policies" json:"approval_policies"`
}

type viewActionManifest struct {
	Kind   string `yaml:"kind" json:"kind"`
	Title  string `yaml:"title" json:"title"`
	Body   string `yaml:"body" json:"body"`
	Render string `yaml:"render" json:"render"`
	Input  struct {
		Enabled     bool   `yaml:"enabled" json:"enabled"`
		Multiline   bool   `yaml:"multiline" json:"multiline"`
		Placeholder string `yaml:"placeholder" json:"placeholder"`
		Value       string `yaml:"value" json:"value"`
	} `yaml:"input" json:"input"`
	Options []struct {
		ID      string `yaml:"id" json:"id"`
		Label   string `yaml:"label" json:"label"`
		Primary bool   `yaml:"primary" json:"primary"`
	} `yaml:"options" json:"options"`
	ViewID    string         `yaml:"view_id" json:"view_id"`
	CommandID string         `yaml:"command_id" json:"command_id"`
	ToolName  string         `yaml:"tool_name" json:"tool_name"`
	Arguments map[string]any `yaml:"arguments" json:"arguments"`
	Result    struct {
		Presentation string `yaml:"presentation" json:"presentation"`
	} `yaml:"result" json:"result"`
	Handoff struct {
		Title  string `yaml:"title" json:"title"`
		Body   string `yaml:"body" json:"body"`
		Render string `yaml:"render" json:"render"`
	} `yaml:"handoff" json:"handoff"`
	InitialInput string `yaml:"initial_input" json:"initial_input"`
}

type hookManifest struct {
	Schema                   string   `yaml:"schema" json:"schema"`
	ID                       string   `yaml:"id" json:"id"`
	Description              string   `yaml:"description" json:"description"`
	Events                   []string `yaml:"events" json:"events"`
	RequiresHostCapabilities []string `yaml:"requires_host_capabilities" json:"requires_host_capabilities"`
	Runtime                  struct {
		Kind         string   `yaml:"kind" json:"kind"`
		Command      string   `yaml:"command" json:"command"`
		Capabilities []string `yaml:"capabilities" json:"capabilities"`
	} `yaml:"runtime" json:"runtime"`
	Delivery struct {
		Mode           string `yaml:"mode" json:"mode"`
		TimeoutSeconds int    `yaml:"timeout_seconds" json:"timeout_seconds"`
	} `yaml:"delivery" json:"delivery"`
}

type extensionManifest struct {
	Schema                   string   `yaml:"schema" json:"schema"`
	ID                       string   `yaml:"id" json:"id"`
	ProtocolVersion          int      `yaml:"protocol_version" json:"protocol_version"`
	RequiresHostCapabilities []string `yaml:"requires_host_capabilities" json:"requires_host_capabilities"`
	Runtime                  struct {
		Kind    string            `yaml:"kind" json:"kind"`
		Command string            `yaml:"command" json:"command"`
		Args    []string          `yaml:"args" json:"args"`
		Env     map[string]string `yaml:"env" json:"env"`
	} `yaml:"runtime" json:"runtime"`
	Subscriptions []struct {
		Event       string `yaml:"event" json:"event"`
		Mode        string `yaml:"mode" json:"mode"`
		TimeoutMS   int    `yaml:"timeout_ms" json:"timeout_ms"`
		FailureMode string `yaml:"failure_mode" json:"failure_mode"`
	} `yaml:"subscriptions" json:"subscriptions"`
}

func LoadRuntimeContributions(workspaceRoot string, hostCapabilities []string) (luruntime.ContributionSet, error) {
	hostCapabilities = luruntime.NormalizeCapabilities(hostCapabilities)

	commands, views, policies, uiDiagnostics, err := loadUIRegistry(workspaceRoot, hostCapabilities)
	if err != nil {
		return luruntime.ContributionSet{}, err
	}
	hooks, hookDiagnostics, err := loadHookRegistry(workspaceRoot, hostCapabilities)
	if err != nil {
		return luruntime.ContributionSet{}, err
	}
	extensionHosts, extensionDiagnostics, err := loadExtensionRegistry(workspaceRoot, hostCapabilities)
	if err != nil {
		return luruntime.ContributionSet{}, err
	}

	diagnostics := append(uiDiagnostics, hookDiagnostics...)
	diagnostics = append(diagnostics, extensionDiagnostics...)
	return luruntime.ContributionSet{
		UI:          luruntime.NewUIRegistry(commands, views, policies),
		Hooks:       luruntime.NewHookRegistry(hooks),
		Extensions:  luruntime.NewExtensionRegistry(extensionHosts),
		Diagnostics: diagnostics,
	}, nil
}

func loadUIRegistry(workspaceRoot string, hostCapabilities []string) ([]luruntime.RuntimeCommand, []luruntime.RuntimeView, []luruntime.ApprovalPolicy, []luruntime.Diagnostic, error) {
	files, err := layeredManifestFiles(workspaceRoot, "ui")
	if err != nil {
		return nil, nil, nil, nil, err
	}

	commandOrder := []string{}
	viewOrder := []string{}
	policyOrder := []string{}
	commandByID := map[string]luruntime.RuntimeCommand{}
	viewByID := map[string]luruntime.RuntimeView{}
	policyByID := map[string]luruntime.ApprovalPolicy{}
	var diagnostics []luruntime.Diagnostic

	for _, path := range files {
		manifest, err := parseUIManifest(path)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if check := luruntime.CheckHostRequirements(manifest.RequiresHostCapabilities, hostCapabilities); !check.Supported() {
			diagnostics = append(diagnostics, luruntime.DiagnosticForMissingCapabilities(path, "ui", check.Missing))
			continue
		}
		for _, command := range manifest.Commands {
			id := strings.TrimSpace(command.ID)
			if id == "" {
				return nil, nil, nil, nil, fmt.Errorf("%s: commands[].id is required", path)
			}
			if _, ok := commandByID[id]; !ok {
				commandOrder = append(commandOrder, id)
			}
			commandByID[id] = luruntime.RuntimeCommand{
				ID:          id,
				Name:        strings.TrimSpace(firstNonEmpty(command.Name, id)),
				Description: strings.TrimSpace(command.Description),
				Category:    strings.TrimSpace(command.Category),
				Shortcut:    strings.TrimSpace(command.Shortcut),
				ActionKind:  strings.TrimSpace(command.Action.Kind),
				Title:       strings.TrimSpace(command.Action.Title),
				Body:        command.Action.Body,
				Render:      strings.TrimSpace(command.Action.Render),
				ViewID:      strings.TrimSpace(command.Action.ViewID),
				CommandID:   strings.TrimSpace(command.Action.CommandID),
				ToolName:    strings.TrimSpace(command.Action.ToolName),
				Arguments:   cloneStringAnyMap(command.Action.Arguments),
				Result: luruntime.RuntimeActionResult{
					Presentation: strings.TrimSpace(command.Action.Result.Presentation),
				},
				Handoff: luruntime.RuntimeHandoff{
					Title:  strings.TrimSpace(command.Action.Handoff.Title),
					Body:   command.Action.Handoff.Body,
					Render: strings.TrimSpace(command.Action.Handoff.Render),
				},
				InitialInput: command.Action.InitialInput,
				SourcePath:   path,
			}
		}
		for _, view := range manifest.Views {
			id := strings.TrimSpace(view.ID)
			if id == "" {
				return nil, nil, nil, nil, fmt.Errorf("%s: views[].id is required", path)
			}
			for _, action := range view.Actions {
				if strings.TrimSpace(action.ID) == "" {
					return nil, nil, nil, nil, fmt.Errorf("%s: views[%s].actions[].id is required", path, id)
				}
			}
			if _, ok := viewByID[id]; !ok {
				viewOrder = append(viewOrder, id)
			}
			viewByID[id] = luruntime.RuntimeView{
				ID:         id,
				Title:      strings.TrimSpace(firstNonEmpty(view.Title, id)),
				Placement:  strings.TrimSpace(view.Placement),
				SourceTool: strings.TrimSpace(view.SourceTool),
				Render:     strings.TrimSpace(view.Render),
				Actions:    runtimeViewActions(view.Actions, path),
				SourcePath: path,
			}
		}
		for _, policy := range manifest.ApprovalPolicies {
			id := strings.TrimSpace(policy.ID)
			if id == "" {
				return nil, nil, nil, nil, fmt.Errorf("%s: approval_policies[].id is required", path)
			}
			if _, ok := policyByID[id]; !ok {
				policyOrder = append(policyOrder, id)
			}
			policyByID[id] = luruntime.ApprovalPolicy{
				ID:           id,
				ToolNames:    append([]string(nil), policy.ToolNames...),
				Mode:         strings.TrimSpace(policy.Mode),
				Title:        strings.TrimSpace(policy.Title),
				BodyTemplate: policy.BodyTemplate,
				ConfirmLabel: strings.TrimSpace(policy.ConfirmLabel),
				CancelLabel:  strings.TrimSpace(policy.CancelLabel),
				SourcePath:   path,
			}
		}
	}

	commands := make([]luruntime.RuntimeCommand, 0, len(commandOrder))
	for _, id := range commandOrder {
		commands = append(commands, commandByID[id])
	}
	views := make([]luruntime.RuntimeView, 0, len(viewOrder))
	for _, id := range viewOrder {
		views = append(views, viewByID[id])
	}
	diagnostics = append(diagnostics, diagnoseRuntimeCommandShortcuts(commands)...)
	diagnostics = append(diagnostics, diagnoseRuntimeActionReferences(commands, views)...)
	policies := make([]luruntime.ApprovalPolicy, 0, len(policyOrder))
	for _, id := range policyOrder {
		policies = append(policies, policyByID[id])
	}
	return commands, views, policies, diagnostics, nil
}

func diagnoseRuntimeActionReferences(commands []luruntime.RuntimeCommand, views []luruntime.RuntimeView) []luruntime.Diagnostic {
	commandByID := map[string]struct{}{}
	viewByID := map[string]struct{}{}
	for _, command := range commands {
		commandByID[strings.ToLower(strings.TrimSpace(command.ID))] = struct{}{}
	}
	for _, view := range views {
		viewByID[strings.ToLower(strings.TrimSpace(view.ID))] = struct{}{}
	}
	var diagnostics []luruntime.Diagnostic
	for _, command := range commands {
		action := luruntime.RuntimeAction{
			Kind:         command.ActionKind,
			Title:        command.Title,
			Body:         command.Body,
			Render:       command.Render,
			ViewID:       command.ViewID,
			CommandID:    command.CommandID,
			ToolName:     command.ToolName,
			Handoff:      command.Handoff,
			InitialInput: command.InitialInput,
		}
		diagnostics = append(diagnostics, diagnoseRuntimeActionReference(command.SourcePath, "ui.command", "command "+command.ID, action, commandByID, viewByID)...)
	}
	for _, view := range views {
		for _, action := range view.Actions {
			diagnostics = append(diagnostics, diagnoseRuntimeActionReference(action.SourcePath, "ui.view.action", "view "+view.ID+" action "+action.ID, action.Action, commandByID, viewByID)...)
		}
	}
	return diagnostics
}

func diagnoseRuntimeActionReference(sourcePath, kind, owner string, action luruntime.RuntimeAction, commandByID, viewByID map[string]struct{}) []luruntime.Diagnostic {
	actionKind := strings.TrimSpace(action.Kind)
	var diagnostics []luruntime.Diagnostic
	switch actionKind {
	case "view.open", "view.refresh":
		viewID := strings.TrimSpace(action.ViewID)
		if viewID == "" {
			diagnostics = append(diagnostics, runtimeDiagnostic(sourcePath, kind, "%s action %q is missing view_id", owner, actionKind))
		} else if _, ok := viewByID[strings.ToLower(viewID)]; !ok {
			diagnostics = append(diagnostics, runtimeDiagnostic(sourcePath, kind, "%s action %q references unknown view %q", owner, actionKind, viewID))
		}
	case "command.run":
		commandID := strings.TrimSpace(action.CommandID)
		if commandID == "" {
			diagnostics = append(diagnostics, runtimeDiagnostic(sourcePath, kind, "%s action %q is missing command_id", owner, actionKind))
		} else if _, ok := commandByID[strings.ToLower(commandID)]; !ok {
			diagnostics = append(diagnostics, runtimeDiagnostic(sourcePath, kind, "%s action %q references unknown command %q", owner, actionKind, commandID))
		}
	case "tool.run":
		if strings.TrimSpace(action.ToolName) == "" {
			diagnostics = append(diagnostics, runtimeDiagnostic(sourcePath, kind, "%s action %q is missing tool_name", owner, actionKind))
		}
	case "session.handoff":
		if strings.TrimSpace(action.Handoff.Body) == "" && strings.TrimSpace(action.InitialInput) == "" {
			diagnostics = append(diagnostics, runtimeDiagnostic(sourcePath, kind, "%s action %q should include handoff.body or initial_input", owner, actionKind))
		}
	case "timeline.note":
		if strings.TrimSpace(action.Title) == "" && strings.TrimSpace(action.Body) == "" {
			diagnostics = append(diagnostics, runtimeDiagnostic(sourcePath, kind, "%s action %q should include title or body", owner, actionKind))
		}
	}
	return diagnostics
}

func runtimeDiagnostic(sourcePath, kind, format string, args ...any) luruntime.Diagnostic {
	return luruntime.Diagnostic{SourcePath: sourcePath, Kind: kind, Message: fmt.Sprintf(format, args...)}
}

func diagnoseRuntimeCommandShortcuts(commands []luruntime.RuntimeCommand) []luruntime.Diagnostic {
	byShortcut := map[string]luruntime.RuntimeCommand{}
	var diagnostics []luruntime.Diagnostic
	for _, command := range commands {
		shortcut := normalizeCommandShortcut(command.Shortcut)
		if shortcut == "" {
			continue
		}
		if isReservedBuiltInShortcut(shortcut) {
			diagnostics = append(diagnostics, luruntime.Diagnostic{
				SourcePath: command.SourcePath,
				Kind:       "ui.command",
				Message:    fmt.Sprintf("runtime command %q shortcut %q conflicts with a built-in shortcut", command.ID, command.Shortcut),
			})
			continue
		}
		if previous, ok := byShortcut[shortcut]; ok {
			diagnostics = append(diagnostics, luruntime.Diagnostic{
				SourcePath: command.SourcePath,
				Kind:       "ui.command",
				Message:    fmt.Sprintf("runtime command %q shortcut %q conflicts with runtime command %q", command.ID, command.Shortcut, previous.ID),
			})
			continue
		}
		byShortcut[shortcut] = command
	}
	return diagnostics
}

func normalizeCommandShortcut(shortcut string) string {
	return strings.ToLower(strings.TrimSpace(shortcut))
}

func isReservedBuiltInShortcut(shortcut string) bool {
	switch shortcut {
	case "esc", "ctrl+.", "ctrl+r", "ctrl+o", "ctrl+]", "ctrl+\\", "ctrl+m", "ctrl+l", "ctrl+p", "ctrl+c", "ctrl+q", "enter", "shift+enter", "alt+enter", "pgup", "pgdown", "ctrl+d", "ctrl+v", "cmd+v", "ctrl+y", "cmd+c":
		return true
	default:
		return false
	}
}

func loadHookRegistry(workspaceRoot string, hostCapabilities []string) ([]luruntime.HookSubscription, []luruntime.Diagnostic, error) {
	files, err := layeredManifestFiles(workspaceRoot, "hooks")
	if err != nil {
		return nil, nil, err
	}

	order := []string{}
	byID := map[string]luruntime.HookSubscription{}
	var diagnostics []luruntime.Diagnostic
	for _, path := range files {
		manifest, err := parseHookManifest(path)
		if err != nil {
			return nil, nil, err
		}
		if check := luruntime.CheckHostRequirements(manifest.RequiresHostCapabilities, hostCapabilities); !check.Supported() {
			diagnostics = append(diagnostics, luruntime.DiagnosticForMissingCapabilities(path, "hook", check.Missing))
			continue
		}
		id := strings.TrimSpace(manifest.ID)
		if _, ok := byID[id]; !ok {
			order = append(order, id)
		}
		byID[id] = luruntime.HookSubscription{
			ID:          id,
			Description: strings.TrimSpace(manifest.Description),
			Events:      append([]string(nil), manifest.Events...),
			Runtime: luruntime.HookRuntime{
				Kind:         strings.TrimSpace(manifest.Runtime.Kind),
				Command:      strings.TrimSpace(manifest.Runtime.Command),
				Capabilities: luruntime.NormalizeCapabilities(manifest.Runtime.Capabilities),
			},
			Delivery: luruntime.HookDelivery{
				Mode:           strings.TrimSpace(firstNonEmpty(manifest.Delivery.Mode, "async")),
				TimeoutSeconds: manifest.Delivery.TimeoutSeconds,
			},
			SourcePath: path,
		}
	}

	hooks := make([]luruntime.HookSubscription, 0, len(order))
	for _, id := range order {
		hooks = append(hooks, byID[id])
	}
	return hooks, diagnostics, nil
}

func loadExtensionRegistry(workspaceRoot string, hostCapabilities []string) ([]luruntime.ExtensionHost, []luruntime.Diagnostic, error) {
	files, err := layeredManifestFiles(workspaceRoot, "extensions")
	if err != nil {
		return nil, nil, err
	}

	order := []string{}
	byID := map[string]luruntime.ExtensionHost{}
	var diagnostics []luruntime.Diagnostic
	for _, path := range files {
		host, err := parseExtensionManifest(path)
		if err != nil {
			return nil, nil, err
		}
		if check := luruntime.CheckHostRequirements(host.RequiresHostCapabilities, hostCapabilities); !check.Supported() {
			diagnostics = append(diagnostics, luruntime.DiagnosticForMissingCapabilities(path, "extension", check.Missing))
			continue
		}
		if _, ok := byID[host.ID]; !ok {
			order = append(order, host.ID)
		}
		byID[host.ID] = host
	}

	hosts := make([]luruntime.ExtensionHost, 0, len(order))
	for _, id := range order {
		hosts = append(hosts, byID[id])
	}
	return hosts, diagnostics, nil
}

func layeredManifestFiles(workspaceRoot, category string) ([]string, error) {
	globalRoot, err := configRoot()
	if err != nil {
		return nil, err
	}

	var files []string
	add := func(dir string) error {
		paths, err := listManifestFiles(dir, false)
		if err != nil {
			return err
		}
		files = append(files, paths...)
		return nil
	}

	if err := add(filepath.Join(globalRoot, category)); err != nil {
		return nil, err
	}

	packageDirs, err := runtimePackageDirs(workspaceRoot, category)
	if err != nil {
		return nil, err
	}
	for _, dir := range packageDirs {
		if err := add(dir); err != nil {
			return nil, err
		}
	}

	if err := add(filepath.Join(workspaceRoot, ".luc", category)); err != nil {
		return nil, err
	}
	return files, nil
}

func runtimePackageDirs(workspaceRoot, category string) ([]string, error) {
	var dirs []string
	roots := []string{}
	globalRoot, err := configRoot()
	if err != nil {
		return nil, err
	}
	roots = append(roots, filepath.Join(globalRoot, "packages"))
	roots = append(roots, filepath.Join(workspaceRoot, ".luc", "packages"))

	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dirs = append(dirs, filepath.Join(root, entry.Name(), category))
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

func runtimeActionFromManifest(action viewActionManifest) luruntime.RuntimeAction {
	options := make([]luruntime.UIOption, 0, len(action.Options))
	for _, option := range action.Options {
		options = append(options, luruntime.UIOption{
			ID:      strings.TrimSpace(option.ID),
			Label:   strings.TrimSpace(option.Label),
			Primary: option.Primary,
		})
	}
	return luruntime.RuntimeAction{
		Kind:   strings.TrimSpace(action.Kind),
		Title:  strings.TrimSpace(action.Title),
		Body:   action.Body,
		Render: strings.TrimSpace(action.Render),
		Input: luruntime.UIActionInput{
			Enabled:     action.Input.Enabled,
			Multiline:   action.Input.Multiline,
			Placeholder: strings.TrimSpace(action.Input.Placeholder),
			Value:       action.Input.Value,
		},
		Options:   options,
		ViewID:    strings.TrimSpace(action.ViewID),
		CommandID: strings.TrimSpace(action.CommandID),
		ToolName:  strings.TrimSpace(action.ToolName),
		Arguments: cloneStringAnyMap(action.Arguments),
		Result: luruntime.RuntimeActionResult{
			Presentation: strings.TrimSpace(action.Result.Presentation),
		},
		Handoff: luruntime.RuntimeHandoff{
			Title:  strings.TrimSpace(action.Handoff.Title),
			Body:   action.Handoff.Body,
			Render: strings.TrimSpace(action.Handoff.Render),
		},
		InitialInput: action.InitialInput,
	}
}

func runtimeViewActions(actions []struct {
	ID       string             `yaml:"id" json:"id"`
	Label    string             `yaml:"label" json:"label"`
	Shortcut string             `yaml:"shortcut" json:"shortcut"`
	Action   viewActionManifest `yaml:"action" json:"action"`
}, sourcePath string) []luruntime.RuntimeViewAction {
	if len(actions) == 0 {
		return nil
	}
	out := make([]luruntime.RuntimeViewAction, 0, len(actions))
	for _, action := range actions {
		id := strings.TrimSpace(action.ID)
		if id == "" {
			continue
		}
		out = append(out, luruntime.RuntimeViewAction{
			ID:         id,
			Label:      strings.TrimSpace(firstNonEmpty(action.Label, id)),
			Shortcut:   strings.TrimSpace(action.Shortcut),
			Action:     runtimeActionFromManifest(action.Action),
			SourcePath: sourcePath,
		})
	}
	return out
}

func cloneStringAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func parseUIManifest(path string) (uiManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return uiManifest{}, err
	}
	var manifest uiManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return uiManifest{}, fmt.Errorf("%s: %w", path, err)
	}
	if strings.TrimSpace(manifest.Schema) != "luc.ui/v1" {
		return uiManifest{}, fmt.Errorf("%s: unsupported ui manifest schema %q", path, manifest.Schema)
	}
	if strings.TrimSpace(manifest.ID) == "" {
		return uiManifest{}, fmt.Errorf("%s: id is required", path)
	}
	for _, command := range manifest.Commands {
		switch strings.TrimSpace(command.Action.Kind) {
		case "", "view.open", "view.refresh", "command.run", "tool.run", "session.handoff", "timeline.note":
		default:
			return uiManifest{}, fmt.Errorf("%s: unsupported command action kind %q", path, command.Action.Kind)
		}
	}
	for _, view := range manifest.Views {
		switch placement := strings.TrimSpace(view.Placement); placement {
		case "inspector_tab", "page":
		default:
			return uiManifest{}, fmt.Errorf("%s: unsupported view placement %q", path, view.Placement)
		}
		switch render := strings.TrimSpace(view.Render); render {
		case "markdown", "json", "table", "kv":
		default:
			return uiManifest{}, fmt.Errorf("%s: unsupported view renderer %q", path, view.Render)
		}
		if strings.TrimSpace(view.SourceTool) == "" {
			return uiManifest{}, fmt.Errorf("%s: views[%s].source_tool is required", path, view.ID)
		}
		for _, action := range view.Actions {
			switch strings.TrimSpace(action.Action.Kind) {
			case "", "view.open", "view.refresh", "command.run", "tool.run", "modal.open", "confirm.request", "session.handoff", "timeline.note":
			default:
				return uiManifest{}, fmt.Errorf("%s: unsupported view action kind %q", path, action.Action.Kind)
			}
		}
	}
	for _, policy := range manifest.ApprovalPolicies {
		switch strings.TrimSpace(policy.Mode) {
		case "confirm", "deny":
		default:
			return uiManifest{}, fmt.Errorf("%s: unsupported approval policy mode %q", path, policy.Mode)
		}
	}
	return manifest, nil
}

func parseHookManifest(path string) (hookManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return hookManifest{}, err
	}
	var manifest hookManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return hookManifest{}, fmt.Errorf("%s: %w", path, err)
	}
	if strings.TrimSpace(manifest.Schema) != "luc.hook/v1" {
		return hookManifest{}, fmt.Errorf("%s: unsupported hook manifest schema %q", path, manifest.Schema)
	}
	if strings.TrimSpace(manifest.ID) == "" {
		return hookManifest{}, fmt.Errorf("%s: id is required", path)
	}
	if len(manifest.Events) == 0 {
		return hookManifest{}, fmt.Errorf("%s: events are required", path)
	}
	if kind := strings.TrimSpace(firstNonEmpty(manifest.Runtime.Kind, "exec")); kind != "exec" {
		return hookManifest{}, fmt.Errorf("%s: unsupported hook runtime kind %q", path, kind)
	}
	if strings.TrimSpace(manifest.Runtime.Command) == "" {
		return hookManifest{}, fmt.Errorf("%s: runtime.command is required", path)
	}
	if mode := strings.TrimSpace(firstNonEmpty(manifest.Delivery.Mode, "async")); mode != "async" {
		return hookManifest{}, fmt.Errorf("%s: unsupported delivery mode %q", path, manifest.Delivery.Mode)
	}
	return manifest, nil
}

func parseExtensionManifest(path string) (luruntime.ExtensionHost, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return luruntime.ExtensionHost{}, err
	}

	var manifest extensionManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return luruntime.ExtensionHost{}, fmt.Errorf("%s: %w", path, err)
	}
	if strings.TrimSpace(manifest.Schema) != "luc.extension/v1" {
		return luruntime.ExtensionHost{}, fmt.Errorf("%s: unsupported extension schema %q", path, manifest.Schema)
	}
	id := strings.TrimSpace(manifest.ID)
	if id == "" {
		return luruntime.ExtensionHost{}, fmt.Errorf("%s: id is required", path)
	}
	if manifest.ProtocolVersion != 1 {
		return luruntime.ExtensionHost{}, fmt.Errorf("%s: unsupported protocol_version %d", path, manifest.ProtocolVersion)
	}
	if kind := strings.TrimSpace(manifest.Runtime.Kind); kind != "exec" {
		return luruntime.ExtensionHost{}, fmt.Errorf("%s: unsupported extension runtime kind %q", path, kind)
	}
	if strings.TrimSpace(manifest.Runtime.Command) == "" {
		return luruntime.ExtensionHost{}, fmt.Errorf("%s: runtime.command is required", path)
	}
	if len(manifest.Subscriptions) == 0 {
		return luruntime.ExtensionHost{}, fmt.Errorf("%s: subscriptions are required", path)
	}

	subscriptions := make([]luruntime.ExtensionSubscription, 0, len(manifest.Subscriptions))
	for i, subscription := range manifest.Subscriptions {
		event := strings.TrimSpace(subscription.Event)
		if event == "" {
			return luruntime.ExtensionHost{}, fmt.Errorf("%s: subscriptions[%d].event is required", path, i)
		}
		mode := strings.TrimSpace(firstNonEmpty(subscription.Mode, luruntime.ExtensionModeObserve))
		switch mode {
		case luruntime.ExtensionModeObserve:
			if !luruntime.SupportsObserveEvent(event) {
				return luruntime.ExtensionHost{}, fmt.Errorf("%s: subscriptions[%d].event %q is not supported for observe mode", path, i, event)
			}
		case luruntime.ExtensionModeSync:
			if !luruntime.SupportsSyncEvent(event) {
				return luruntime.ExtensionHost{}, fmt.Errorf("%s: subscriptions[%d].event %q is not supported for sync mode", path, i, event)
			}
		default:
			return luruntime.ExtensionHost{}, fmt.Errorf("%s: subscriptions[%d].mode %q is invalid", path, i, mode)
		}
		failureMode := strings.TrimSpace(firstNonEmpty(subscription.FailureMode, luruntime.ExtensionFailureModeOpen))
		switch failureMode {
		case luruntime.ExtensionFailureModeOpen:
		case luruntime.ExtensionFailureModeClosed:
			if event != luruntime.ExtensionEventInputTransform && event != luruntime.ExtensionEventToolPreflight {
				return luruntime.ExtensionHost{}, fmt.Errorf("%s: subscriptions[%d].failure_mode %q is only allowed for %q and %q", path, i, failureMode, luruntime.ExtensionEventInputTransform, luruntime.ExtensionEventToolPreflight)
			}
		default:
			return luruntime.ExtensionHost{}, fmt.Errorf("%s: subscriptions[%d].failure_mode %q is invalid", path, i, failureMode)
		}
		subscriptions = append(subscriptions, luruntime.ExtensionSubscription{
			Event:       event,
			Mode:        mode,
			TimeoutMS:   subscription.TimeoutMS,
			FailureMode: failureMode,
		})
	}

	return luruntime.ExtensionHost{
		ID:              id,
		ProtocolVersion: manifest.ProtocolVersion,
		Runtime: luruntime.ExtensionRuntime{
			Kind:    strings.TrimSpace(manifest.Runtime.Kind),
			Command: strings.TrimSpace(manifest.Runtime.Command),
			Args:    append([]string(nil), manifest.Runtime.Args...),
			Env:     cloneStringMap(manifest.Runtime.Env),
		},
		Subscriptions:            subscriptions,
		RequiresHostCapabilities: luruntime.NormalizeCapabilities(manifest.RequiresHostCapabilities),
		SourcePath:               path,
	}, nil
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}
