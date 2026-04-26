package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
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

type runtimeSessionHandoffMsg struct {
	action   luruntime.UIAction
	response chan uiBrokerResponse
	err      error
}

type runtimeTimelineNoteMsg struct {
	action   luruntime.UIAction
	response chan uiBrokerResponse
	err      error
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
	open         bool
	action       luruntime.UIAction
	active       int
	choiceScroll int
	body         viewport.Model
	input        textarea.Model
	response     chan uiBrokerResponse
}

type teaUIBroker struct {
	actions chan uiBrokerRequest
}

func newTeaUIBroker() *teaUIBroker {
	return &teaUIBroker{actions: make(chan uiBrokerRequest, 32)}
}

func (b *teaUIBroker) Publish(action luruntime.UIAction) error {
	if strings.TrimSpace(action.Kind) == "session.handoff" {
		return fmt.Errorf("session.handoff requires a blocking action")
	}
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
		rendered := result.RenderContent()
		if !strings.EqualFold(strings.TrimSpace(view.Render), "markdown") || !strings.EqualFold(strings.TrimSpace(view.Placement), "inspector_tab") {
			rendered = viewrender.Render(controller.Config().UI.Theme, controller.Workspace().Root, view, result)
		}
		return runtimeViewLoadedMsg{
			ViewID:    view.ID,
			Placement: view.Placement,
			Rendered:  rendered,
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
		ID:           "runtime.command." + command.ID,
		Kind:         command.ActionKind,
		Title:        command.Title,
		Body:         command.Body,
		Render:       command.Render,
		ViewID:       command.ViewID,
		CommandID:    command.CommandID,
		ToolName:     command.ToolName,
		Arguments:    command.Arguments,
		Handoff:      command.Handoff,
		InitialInput: command.InitialInput,
		Result: luruntime.UIActionResult{
			Presentation: command.Result.Presentation,
		},
	}, nil)
}

func uiActionFromRuntimeAction(id string, action luruntime.RuntimeAction) luruntime.UIAction {
	return luruntime.UIAction{
		ID:           id,
		Kind:         action.Kind,
		Title:        action.Title,
		Body:         action.Body,
		Render:       action.Render,
		Input:        action.Input,
		Options:      action.Options,
		ViewID:       action.ViewID,
		CommandID:    action.CommandID,
		ToolName:     action.ToolName,
		Arguments:    action.Arguments,
		Handoff:      action.Handoff,
		InitialInput: action.InitialInput,
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
	case "session.handoff":
		if response != nil && !action.Blocking {
			m.replyRuntimeAction(response, luruntime.UIResult{ActionID: action.ID}, fmt.Errorf("session.handoff requires a blocking action"))
			return nil
		}
		return m.runtimeSessionHandoff(action, response)
	case "timeline.note":
		return m.runtimeTimelineNote(action, response)
	default:
		m.replyRuntimeAction(response, luruntime.UIResult{ActionID: action.ID}, fmt.Errorf("unsupported runtime action %q", action.Kind))
		return nil
	}
}

func (m *Model) runtimeSessionHandoff(action luruntime.UIAction, response chan uiBrokerResponse) tea.Cmd {
	return func() tea.Msg {
		return runtimeSessionHandoffMsg{action: action, response: response, err: m.controller.HandoffSession(action)}
	}
}

func (m *Model) runtimeTimelineNote(action luruntime.UIAction, response chan uiBrokerResponse) tea.Cmd {
	return func() tea.Msg {
		return runtimeTimelineNoteMsg{action: action, response: response, err: m.controller.TimelineNote(action)}
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
	bodyWidth, bodyHeight := m.runtimeDialogBodySize(action)
	body := viewport.New()
	body.SetWidth(bodyWidth)
	body.SetHeight(bodyHeight)
	state := runtimeDialogState{open: true, action: action, body: body, response: response}
	state.body.SetContent(m.renderRuntimeDialogBody(action, bodyWidth))
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
	case key.Matches(msg, key.NewBinding(key.WithKeys("pgup"))):
		m.runtimeDialog.body, _ = m.runtimeDialog.body.Update(msg)
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("pgdown"))):
		m.runtimeDialog.body, _ = m.runtimeDialog.body.Update(msg)
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("up", "shift+tab"))) || (!m.runtimeDialog.action.Input.Enabled && key.Matches(msg, key.NewBinding(key.WithKeys("left")))):
		if m.runtimeDialog.active > 0 {
			m.runtimeDialog.active--
		}
		m.ensureRuntimeDialogChoiceVisible(len(options))
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("down", "tab"))) || (!m.runtimeDialog.action.Input.Enabled && key.Matches(msg, key.NewBinding(key.WithKeys("right")))):
		if m.runtimeDialog.active < len(options)-1 {
			m.runtimeDialog.active++
		}
		m.ensureRuntimeDialogChoiceVisible(len(options))
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
	bodyWidth, bodyHeight := m.runtimeDialogBodySize(action)
	m.runtimeDialog.body.SetWidth(bodyWidth)
	m.runtimeDialog.body.SetHeight(bodyHeight)
	m.runtimeDialog.body.SetContent(m.renderRuntimeDialogBody(action, bodyWidth))
	bodyScrollable := m.runtimeDialog.body.TotalLineCount() > m.runtimeDialog.body.Height()
	body := m.renderRuntimeDialogBodyViewport(bodyScrollable)
	parts := []string{
		m.theme.HeaderBrand.Render(title),
		"",
		body,
	}
	if action.Input.Enabled {
		parts = append(parts, "", m.runtimeDialog.input.View())
	}
	parts = append(parts,
		"",
		m.renderRuntimeDialogChoices(runtimeDialogOptions(action)),
		"",
		m.theme.Footer.Render(runtimeDialogHelp(action, bodyScrollable)),
	)
	content := lipgloss.JoinVertical(lipgloss.Left, parts...)
	box := m.theme.PaletteFrame.Width(min(72, max(40, m.width*2/3))).Render(m.theme.PaletteSurface.Render(content))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box, lipgloss.WithWhitespaceChars(" "))
}

func (m Model) renderRuntimeDialogBodyViewport(scrollable bool) string {
	body := m.theme.SidebarValue.Render(m.runtimeDialog.body.View())
	if !scrollable || m.runtimeDialog.body.Height() <= 0 {
		return body
	}
	height := m.runtimeDialog.body.Height()
	lines := make([]string, height)
	for i := range lines {
		lines[i] = " "
	}
	limit := max(0, m.runtimeDialog.body.TotalLineCount()-height)
	pos := 0
	if limit > 0 && height > 1 {
		pos = m.runtimeDialog.body.YOffset() * (height - 1) / limit
	}
	lines[pos] = m.theme.HeaderRule.Render("│")
	return lipgloss.JoinHorizontal(lipgloss.Top, body, strings.Join(lines, "\n"))
}

func (m *Model) ensureRuntimeDialogChoiceVisible(total int) {
	maxRows := runtimeDialogChoiceMaxRows(m.height)
	if m.runtimeDialog.active < m.runtimeDialog.choiceScroll {
		m.runtimeDialog.choiceScroll = m.runtimeDialog.active
	}
	if m.runtimeDialog.active >= m.runtimeDialog.choiceScroll+maxRows {
		m.runtimeDialog.choiceScroll = m.runtimeDialog.active - maxRows + 1
	}
	limit := max(0, total-maxRows)
	if m.runtimeDialog.choiceScroll > limit {
		m.runtimeDialog.choiceScroll = limit
	}
	if m.runtimeDialog.choiceScroll < 0 {
		m.runtimeDialog.choiceScroll = 0
	}
}

func runtimeDialogChoiceMaxRows(height int) int {
	return max(2, min(8, height/4))
}

func (m Model) renderRuntimeDialogChoices(options []luruntime.UIOption) string {
	if len(options) == 0 {
		return ""
	}
	maxRows := runtimeDialogChoiceMaxRows(m.height)
	scroll := m.runtimeDialog.choiceScroll
	if scroll < 0 {
		scroll = 0
	}
	if scroll > max(0, len(options)-maxRows) {
		scroll = max(0, len(options)-maxRows)
	}
	end := min(len(options), scroll+maxRows)
	var lines []string
	if scroll > 0 {
		lines = append(lines, m.theme.Muted.Render("  ↑ "+strconv.Itoa(scroll)+" more"))
	}
	for i, option := range options[scroll:end] {
		index := scroll + i
		label := strings.TrimSpace(option.Label)
		if label == "" {
			label = strings.TrimSpace(option.ID)
		}
		if label == "" {
			label = "Option"
		}
		line := "  " + label
		if index == m.runtimeDialog.active {
			line = "› " + label
			lines = append(lines, m.theme.PaletteActive.Render(line))
		} else {
			lines = append(lines, m.theme.SidebarValue.Render(line))
		}
	}
	below := len(options) - end
	if below > 0 {
		lines = append(lines, m.theme.Muted.Render("  ↓ "+strconv.Itoa(below)+" more"))
	}
	return strings.Join(lines, "\n")
}

func (m Model) runtimeDialogBodySize(action luruntime.UIAction) (int, int) {
	width := min(72, max(40, m.width*2/3)) - 4
	height := max(3, min(12, m.height/3))
	if action.Input.Enabled {
		height = max(3, height-2)
	}
	return width, height
}

func (m Model) renderRuntimeDialogBody(action luruntime.UIAction, width int) string {
	body := strings.TrimSpace(action.Body)
	if body == "" {
		body = "The extension requested input."
	}
	if !strings.EqualFold(strings.TrimSpace(action.Render), "markdown") {
		return body
	}
	_, variant, err := theme.Load(m.controller.Config().UI.Theme, m.controller.Workspace().Root)
	if err != nil {
		return body
	}
	renderer, err := theme.NewMarkdownRenderer(width, variant)
	if err != nil {
		return body
	}
	rendered, err := renderer.Render(body)
	if err != nil {
		return body
	}
	return strings.TrimSpace(rendered)
}

func runtimeDialogHelp(action luruntime.UIAction, scrollable bool) string {
	parts := []string{"↑/↓ choose"}
	if scrollable {
		parts = append(parts, "mouse wheel scroll content", "pgup/pgdown scroll content")
	}
	if action.Input.Enabled && action.Input.Multiline {
		parts = append(parts, "shift+enter newline")
	}
	parts = append(parts, "enter confirm", "esc cancel")
	return strings.Join(parts, "  •  ")
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
