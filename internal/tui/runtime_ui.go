package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/lutefd/luc/internal/kernel"
	luruntime "github.com/lutefd/luc/internal/runtime"
	"github.com/lutefd/luc/internal/runtime/viewrender"
	"github.com/lutefd/luc/internal/theme"
	"github.com/lutefd/luc/internal/tui/commands"
)

type uiBrokerRequest struct {
	action   luruntime.UIAction
	response chan uiBrokerResponse
}

type uiBrokerResponse struct {
	result luruntime.UIResult
	err    error
}

type uiBrokerActionMsg struct {
	request uiBrokerRequest
}

type runRuntimeCommandMsg struct {
	CommandID string
}

type runtimeViewLoadedMsg struct {
	ViewID    string
	Placement string
	Rendered  string
	Err       error
}

type runtimeToolActionDoneMsg struct {
	ActionID     string
	ToolName     string
	Presentation string
	Err          error
}

type runtimePageState struct {
	open         bool
	view         luruntime.RuntimeView
	content      string
	err          string
	loading      bool
	activeAction int
}

type runtimeDialogState struct {
	open     bool
	action   luruntime.UIAction
	active   int
	input    textarea.Model
	response chan uiBrokerResponse
}

type teaUIBroker struct {
	actions chan uiBrokerRequest
}

func newTeaUIBroker() *teaUIBroker {
	return &teaUIBroker{actions: make(chan uiBrokerRequest, 32)}
}

func (b *teaUIBroker) Publish(action luruntime.UIAction) error {
	b.actions <- uiBrokerRequest{action: action}
	return nil
}

func (b *teaUIBroker) Request(ctx context.Context, action luruntime.UIAction) (luruntime.UIResult, error) {
	response := make(chan uiBrokerResponse, 1)
	request := uiBrokerRequest{action: action, response: response}
	select {
	case b.actions <- request:
	case <-ctx.Done():
		return luruntime.UIResult{}, ctx.Err()
	}
	select {
	case reply := <-response:
		return reply.result, reply.err
	case <-ctx.Done():
		return luruntime.UIResult{}, ctx.Err()
	}
}

func waitForUIBroker(ch <-chan uiBrokerRequest) tea.Cmd {
	return func() tea.Msg {
		request, ok := <-ch
		if !ok {
			return nil
		}
		return uiBrokerActionMsg{request: request}
	}
}

func runtimeToolActionCmd(controller *kernel.Controller, action luruntime.UIAction, response chan uiBrokerResponse) tea.Cmd {
	return func() tea.Msg {
		result, err := controller.RunRuntimeToolAction(context.Background(), action.ToolName, action.Arguments)
		if response != nil {
			reply := uiBrokerResponse{
				result: luruntime.UIResult{
					ActionID: action.ID,
					Accepted: err == nil,
					Data: map[string]any{
						"tool_name": action.ToolName,
						"content":   result.Content,
						"metadata":  result.Metadata,
					},
				},
				err: err,
			}
			select {
			case response <- reply:
			default:
			}
		}
		return runtimeToolActionDoneMsg{
			ActionID:     action.ID,
			ToolName:     action.ToolName,
			Presentation: action.Result.Presentation,
			Err:          err,
		}
	}
}

func runtimeViewCmd(controller *kernel.Controller, viewID string) tea.Cmd {
	return func() tea.Msg {
		view, result, err := controller.RenderRuntimeView(context.Background(), viewID)
		if err != nil {
			return runtimeViewLoadedMsg{ViewID: viewID, Err: err}
		}
		return runtimeViewLoadedMsg{
			ViewID:    view.ID,
			Placement: view.Placement,
			Rendered:  viewrender.Render(controller.Config().UI.Theme, controller.Workspace().Root, view, result),
		}
	}
}

func (m *Model) installRuntimeUI() {
	if m.runtimeBroker == nil {
		m.runtimeBroker = newTeaUIBroker()
	}
	m.controller.SetUIBroker(m.runtimeBroker)
	m.syncRuntimeUI()
}

func (m *Model) syncRuntimeUI() {
	m.rebuildCommandRegistry()
	m.inspector.SetRuntimeViews(m.controller.RuntimeContributions().UI.InspectorViews())
	if m.runtimePage.open {
		if view, ok := m.controller.RuntimeContributions().UI.View(m.runtimePage.view.ID); ok {
			m.runtimePage.view = view
			if m.runtimePage.activeAction >= len(view.Actions) {
				m.runtimePage.activeAction = 0
			}
		} else {
			m.runtimePage = runtimePageState{}
		}
	}
}

func (m *Model) rebuildCommandRegistry() {
	registry := commands.NewRegistry()
	m.registerBuiltInCommands(registry)
	for _, command := range m.controller.RuntimeContributions().UI.Commands() {
		commandID := command.ID
		registry.Register(commands.Command{
			ID:          command.ID,
			Name:        command.Name,
			Description: command.Description,
			Category:    command.Category,
			Shortcut:    command.Shortcut,
			Run: func() tea.Cmd {
				return func() tea.Msg {
					return runRuntimeCommandMsg{CommandID: commandID}
				}
			},
		})
	}
	m.registry = registry
	m.palette = commands.New(registry, m.theme)
	m.palette.SetSize(m.width, m.height)
}

func (m *Model) registerBuiltInCommands(registry *commands.Registry) {
	registry.Register(commands.Command{
		ID: "input.clear", Name: "Clear input", Shortcut: "esc",
		Run: func() tea.Cmd { return func() tea.Msg { return clearComposerMsg{} } },
	})
	registry.Register(commands.Command{
		ID: "turn.stop", Name: "Stop current turn", Shortcut: "ctrl+.",
		Run: func() tea.Cmd { return func() tea.Msg { return stopTurnMsg{} } },
	})
	registry.Register(commands.Command{
		ID: "reload", Name: "Reload runtime", Shortcut: "ctrl+r",
		Run: func() tea.Cmd { return reloadCmd(m.controller) },
	})
	registry.Register(commands.Command{
		ID: "selection.copy", Name: "Copy selection", Shortcut: "ctrl+y/cmd+c",
		Run: func() tea.Cmd { return func() tea.Msg { return copySelectionMsg{} } },
	})
	registry.Register(commands.Command{
		ID: "toggle.inspector", Name: "Toggle inspector details", Shortcut: "ctrl+o",
		Run: func() tea.Cmd { return func() tea.Msg { return toggleInspectorMsg{} } },
	})
	registry.Register(commands.Command{
		ID: "inspector.tab.next", Name: "Inspector: next tab", Shortcut: "ctrl+]",
		Run: func() tea.Cmd { return func() tea.Msg { return nextTabMsg{} } },
	})
	registry.Register(commands.Command{
		ID: "model.switch", Name: "Switch model…", Shortcut: "ctrl+m",
		Run: func() tea.Cmd { return func() tea.Msg { return openModelPickerMsg{} } },
	})
	registry.Register(commands.Command{
		ID: "session.new", Name: "New session",
		Run: func() tea.Cmd { return func() tea.Msg { return newSessionMsg{} } },
	})
	registry.Register(commands.Command{
		ID: "session.switch", Name: "Switch session…", Shortcut: "ctrl+l",
		Run: func() tea.Cmd { return func() tea.Msg { return openSessionPickerMsg{} } },
	})
	registry.Register(commands.Command{
		ID: "theme.switch", Name: "Switch theme…",
		Run: func() tea.Cmd { return func() tea.Msg { return openThemePickerMsg{} } },
	})
	registry.Register(commands.Command{
		ID: "theme.reset", Name: "Reset theme to default",
		Run: func() tea.Cmd { return func() tea.Msg { return resetThemeMsg{} } },
	})
	registry.Register(commands.Command{
		ID: "quit", Name: "Quit", Shortcut: "ctrl+c",
		Run: func() tea.Cmd { return tea.Quit },
	})
}

func (m *Model) handleRuntimeCommandShortcut(msg tea.KeyPressMsg) tea.Cmd {
	shortcut := strings.ToLower(strings.TrimSpace(msg.Keystroke()))
	if shortcut == "" {
		shortcut = strings.ToLower(strings.TrimSpace(msg.String()))
	}
	for _, command := range m.controller.RuntimeContributions().UI.Commands() {
		if strings.EqualFold(strings.TrimSpace(command.Shortcut), shortcut) {
			return m.handleRuntimeCommand(command.ID)
		}
	}
	return nil
}

func (m *Model) handleRuntimeCommand(commandID string) tea.Cmd {
	command, ok := m.controller.RuntimeContributions().UI.Command(commandID)
	if !ok {
		m.setStatus("Unknown runtime command: " + commandID)
		return nil
	}
	return m.handleRuntimeAction(luruntime.UIAction{
		ID:        "runtime.command." + command.ID,
		Kind:      command.ActionKind,
		ViewID:    command.ViewID,
		CommandID: command.CommandID,
		ToolName:  command.ToolName,
		Arguments: command.Arguments,
		Result: luruntime.UIActionResult{
			Presentation: command.Result.Presentation,
		},
	}, nil)
}

func uiActionFromRuntimeAction(id string, action luruntime.RuntimeAction) luruntime.UIAction {
	return luruntime.UIAction{
		ID:        id,
		Kind:      action.Kind,
		Title:     action.Title,
		Body:      action.Body,
		Render:    action.Render,
		Input:     action.Input,
		Options:   action.Options,
		ViewID:    action.ViewID,
		CommandID: action.CommandID,
		ToolName:  action.ToolName,
		Arguments: action.Arguments,
		Result: luruntime.UIActionResult{
			Presentation: action.Result.Presentation,
		},
	}
}

func (m *Model) handleRuntimeAction(action luruntime.UIAction, response chan uiBrokerResponse) tea.Cmd {
	switch strings.TrimSpace(action.Kind) {
	case "modal.open", "confirm.request":
		m.runtimeDialog = m.newRuntimeDialogState(action, response)
		return nil
	case "view.open":
		view, ok := m.controller.RuntimeContributions().UI.View(action.ViewID)
		if !ok {
			m.replyRuntimeAction(response, luruntime.UIResult{ActionID: action.ID}, fmt.Errorf("runtime view %q not found", action.ViewID))
			return nil
		}
		m.replyRuntimeAction(response, luruntime.UIResult{ActionID: action.ID, Accepted: true}, nil)
		return m.openRuntimeView(view)
	case "view.refresh":
		m.replyRuntimeAction(response, luruntime.UIResult{ActionID: action.ID, Accepted: true}, nil)
		if viewID := strings.TrimSpace(action.ViewID); viewID != "" {
			return runtimeViewCmd(m.controller, viewID)
		}
		if m.runtimePage.open {
			return runtimeViewCmd(m.controller, m.runtimePage.view.ID)
		}
		if view, ok := m.inspector.ActiveRuntimeView(); ok {
			return runtimeViewCmd(m.controller, view.ID)
		}
		return nil
	case "command.run":
		m.replyRuntimeAction(response, luruntime.UIResult{ActionID: action.ID, Accepted: true}, nil)
		return m.handleRuntimeCommand(action.CommandID)
	case "tool.run":
		m.setStatus("Running " + action.ToolName + "...")
		return runtimeToolActionCmd(m.controller, action, response)
	default:
		m.replyRuntimeAction(response, luruntime.UIResult{ActionID: action.ID}, fmt.Errorf("unsupported runtime action %q", action.Kind))
		return nil
	}
}

func (m *Model) openRuntimeView(view luruntime.RuntimeView) tea.Cmd {
	if strings.EqualFold(view.Placement, "inspector_tab") {
		m.inspectorOpen = true
		m.inspector.ActivateRuntimeView(view.ID)
		m.resize()
		return runtimeViewCmd(m.controller, view.ID)
	}
	m.runtimePage = runtimePageState{
		open:    true,
		view:    view,
		loading: true,
	}
	return runtimeViewCmd(m.controller, view.ID)
}

func (m *Model) maybeRefreshActiveRuntimeView() tea.Cmd {
	if view, ok := m.inspector.ActiveRuntimeView(); ok {
		if strings.TrimSpace(m.inspector.RuntimeViewContent(view.ID)) == "" {
			return runtimeViewCmd(m.controller, view.ID)
		}
	}
	return nil
}

func (m *Model) newRuntimeDialogState(action luruntime.UIAction, response chan uiBrokerResponse) runtimeDialogState {
	state := runtimeDialogState{open: true, action: action, response: response}
	if action.Input.Enabled {
		input := textarea.New()
		input.Placeholder = strings.TrimSpace(action.Input.Placeholder)
		input.SetValue(action.Input.Value)
		input.Focus()
		input.ShowLineNumbers = false
		input.Prompt = "> "
		input.CharLimit = 0
		if action.Input.Multiline {
			input.SetHeight(4)
			input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter"), key.WithHelp("shift+enter", "newline"))
		} else {
			input.SetHeight(1)
		}
		applyInputTheme(&input, m.theme, theme.ResolveVariant(m.controller.Config().UI.Theme))
		state.input = input
	}
	return state
}

func (m *Model) replyRuntimeAction(response chan uiBrokerResponse, result luruntime.UIResult, err error) {
	if response == nil {
		return
	}
	select {
	case response <- uiBrokerResponse{result: result, err: err}:
	default:
	}
}

func (m *Model) handleRuntimeDialogKey(msg tea.KeyPressMsg) tea.Cmd {
	if !m.runtimeDialog.open {
		return nil
	}
	options := runtimeDialogOptions(m.runtimeDialog.action)

	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc", "ctrl+g"))):
		action := m.runtimeDialog.action
		m.replyRuntimeAction(m.runtimeDialog.response, luruntime.UIResult{ActionID: action.ID}, nil)
		m.runtimeDialog = runtimeDialogState{}
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("shift+tab"))) || (!m.runtimeDialog.action.Input.Enabled && key.Matches(msg, key.NewBinding(key.WithKeys("left")))):
		if m.runtimeDialog.active > 0 {
			m.runtimeDialog.active--
		}
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))) || (!m.runtimeDialog.action.Input.Enabled && key.Matches(msg, key.NewBinding(key.WithKeys("right")))):
		if m.runtimeDialog.active < len(options)-1 {
			m.runtimeDialog.active++
		}
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		action := m.runtimeDialog.action
		choice := options[m.runtimeDialog.active]
		result := luruntime.UIResult{ActionID: action.ID, Accepted: choice.ID != "cancel", ChoiceID: choice.ID}
		if action.Input.Enabled {
			result.Data = map[string]any{"input": m.runtimeDialog.input.Value()}
		}
		m.replyRuntimeAction(m.runtimeDialog.response, result, nil)
		m.runtimeDialog = runtimeDialogState{}
		return nil
	}
	if m.runtimeDialog.action.Input.Enabled {
		var cmd tea.Cmd
		m.runtimeDialog.input, cmd = m.runtimeDialog.input.Update(msg)
		return cmd
	}
	return nil
}

func runtimeDialogOptions(action luruntime.UIAction) []luruntime.UIOption {
	options := action.Options
	if len(options) == 0 {
		return []luruntime.UIOption{{ID: "ok", Label: "OK", Primary: true}, {ID: "cancel", Label: "Cancel"}}
	}
	return options
}

func (m Model) renderRuntimeDialog() string {
	if !m.runtimeDialog.open {
		return ""
	}
	action := m.runtimeDialog.action
	title := strings.TrimSpace(action.Title)
	if title == "" {
		title = "Runtime Dialog"
	}
	body := strings.TrimSpace(action.Body)
	if body == "" {
		body = "The extension requested input."
	}
	body = m.renderRuntimeDialogBody(body, action.Render)
	options := runtimeDialogOptions(action)
	var buttons []string
	for i, option := range options {
		label := strings.TrimSpace(option.Label)
		if label == "" {
			label = strings.TrimSpace(option.ID)
		}
		if label == "" {
			label = "Option"
		}
		if i == m.runtimeDialog.active {
			buttons = append(buttons, m.theme.PaletteActive.Render(" "+label+" "))
		} else {
			buttons = append(buttons, m.theme.PaletteFrame.Render(" "+label+" "))
		}
	}
	parts := []string{
		m.theme.HeaderBrand.Render(title),
		"",
		m.theme.SidebarValue.Render(body),
	}
	if action.Input.Enabled {
		parts = append(parts, "", m.runtimeDialog.input.View())
	}
	parts = append(parts,
		"",
		strings.Join(buttons, " "),
		"",
		m.theme.Footer.Render(runtimeDialogHelp(action)),
	)
	content := lipgloss.JoinVertical(lipgloss.Left, parts...)
	box := m.theme.PaletteFrame.Width(min(72, max(40, m.width*2/3))).Render(m.theme.PaletteSurface.Render(content))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box, lipgloss.WithWhitespaceChars(" "))
}

func (m Model) renderRuntimeDialogBody(body, render string) string {
	if !strings.EqualFold(strings.TrimSpace(render), "markdown") {
		return body
	}
	_, variant, err := theme.Load(m.controller.Config().UI.Theme, m.controller.Workspace().Root)
	if err != nil {
		return body
	}
	renderer, err := theme.NewMarkdownRenderer(min(72, max(40, m.width*2/3))-4, variant)
	if err != nil {
		return body
	}
	rendered, err := renderer.Render(body)
	if err != nil {
		return body
	}
	return strings.TrimSpace(rendered)
}

func runtimeDialogHelp(action luruntime.UIAction) string {
	if action.Input.Enabled && action.Input.Multiline {
		return "tab choose  •  shift+enter newline  •  enter confirm  •  esc cancel"
	}
	return "tab choose  •  enter confirm  •  esc cancel"
}

func (m *Model) handleRuntimePageKey(msg tea.KeyPressMsg) tea.Cmd {
	if !m.runtimePage.open {
		return nil
	}
	actions := m.runtimePage.view.Actions
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		m.runtimePage = runtimePageState{}
		return nil
	case msg.Text == "r" || msg.Text == "R":
		return runtimeViewCmd(m.controller, m.runtimePage.view.ID)
	case key.Matches(msg, key.NewBinding(key.WithKeys("up", "shift+tab"))):
		if m.runtimePage.activeAction > 0 {
			m.runtimePage.activeAction--
		}
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("down", "tab"))):
		if m.runtimePage.activeAction < len(actions)-1 {
			m.runtimePage.activeAction++
		}
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		if len(actions) == 0 {
			return nil
		}
		return m.runRuntimeViewAction(m.runtimePage.view, actions[m.runtimePage.activeAction])
	}
	for _, action := range actions {
		if strings.EqualFold(strings.TrimSpace(action.Shortcut), strings.TrimSpace(msg.String())) || strings.EqualFold(strings.TrimSpace(action.Shortcut), strings.TrimSpace(msg.Keystroke())) {
			return m.runRuntimeViewAction(m.runtimePage.view, action)
		}
	}
	return nil
}

func (m *Model) runRuntimeViewAction(view luruntime.RuntimeView, action luruntime.RuntimeViewAction) tea.Cmd {
	id := "runtime.view." + view.ID + ".action." + action.ID
	return m.handleRuntimeAction(uiActionFromRuntimeAction(id, action.Action), nil)
}

func (m Model) renderRuntimeViewActions(actions []luruntime.RuntimeViewAction, active int) string {
	if len(actions) == 0 {
		return ""
	}
	lines := []string{m.theme.SidebarLabel.Render("Actions")}
	for i, action := range actions {
		label := strings.TrimSpace(action.Label)
		if label == "" {
			label = action.ID
		}
		if shortcut := strings.TrimSpace(action.Shortcut); shortcut != "" {
			label = fmt.Sprintf("%s (%s)", label, shortcut)
		}
		prefix := "  "
		style := m.theme.SidebarValue
		if i == active {
			prefix = "› "
			style = m.theme.PaletteActive
		}
		lines = append(lines, style.Render(prefix+label))
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderRuntimePage() string {
	if !m.runtimePage.open {
		return ""
	}
	title := strings.TrimSpace(m.runtimePage.view.Title)
	if title == "" {
		title = m.runtimePage.view.ID
	}
	body := m.runtimePage.content
	if strings.TrimSpace(m.runtimePage.err) != "" {
		body = m.runtimePage.err
	} else if strings.TrimSpace(body) == "" {
		body = "Loading..."
	}
	panelWidth := max(40, m.width-6)
	panelHeight := max(8, m.height-4)
	parts := []string{
		m.theme.HeaderBrand.Render(title),
		"",
		m.theme.SidebarValue.Width(panelWidth - 8).Render(body),
	}
	if actions := m.renderRuntimeViewActions(m.runtimePage.view.Actions, m.runtimePage.activeAction); actions != "" {
		parts = append(parts, "", actions)
	}
	parts = append(parts, "", m.theme.Footer.Render("esc close  •  r refresh  •  tab action  •  enter run"))
	content := lipgloss.JoinVertical(lipgloss.Left, parts...)
	box := m.theme.PaletteFrame.Width(panelWidth).Height(panelHeight).Render(m.theme.PaletteSurface.Width(panelWidth - 6).Render(content))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box, lipgloss.WithWhitespaceChars(" "))
}
