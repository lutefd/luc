package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/lutefd/luc/internal/kernel"
	luruntime "github.com/lutefd/luc/internal/runtime"
	"github.com/lutefd/luc/internal/tools"
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

type runtimePageState struct {
	open    bool
	view    luruntime.RuntimeView
	content string
	err     string
	loading bool
}

type runtimeDialogState struct {
	open     bool
	action   luruntime.UIAction
	active   int
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

func runtimeViewCmd(controller *kernel.Controller, viewID string) tea.Cmd {
	return func() tea.Msg {
		view, result, err := controller.RenderRuntimeView(context.Background(), viewID)
		if err != nil {
			return runtimeViewLoadedMsg{ViewID: viewID, Err: err}
		}
		return runtimeViewLoadedMsg{
			ViewID:    view.ID,
			Placement: view.Placement,
			Rendered:  renderRuntimeView(view, result),
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
		if _, ok := m.controller.RuntimeContributions().UI.View(m.runtimePage.view.ID); !ok {
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
			ID:   command.ID,
			Name: command.Name,
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
		ID: "reload", Name: "Reload runtime", Shortcut: "ctrl+r",
		Run: func() tea.Cmd { return reloadCmd(m.controller) },
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
		ID: "selection.copy", Name: "Copy selection", Shortcut: "ctrl+y",
		Run: func() tea.Cmd { return func() tea.Msg { return copySelectionMsg{} } },
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
	}, nil)
}

func (m *Model) handleRuntimeAction(action luruntime.UIAction, response chan uiBrokerResponse) tea.Cmd {
	switch strings.TrimSpace(action.Kind) {
	case "modal.open", "confirm.request":
		m.runtimeDialog = runtimeDialogState{open: true, action: action, response: response}
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
	options := m.runtimeDialog.action.Options
	if len(options) == 0 {
		options = []luruntime.UIOption{
			{ID: "ok", Label: "OK", Primary: true},
			{ID: "cancel", Label: "Cancel"},
		}
	}

	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc", "ctrl+g"))):
		action := m.runtimeDialog.action
		m.replyRuntimeAction(m.runtimeDialog.response, luruntime.UIResult{ActionID: action.ID}, nil)
		m.runtimeDialog = runtimeDialogState{}
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("left", "shift+tab"))):
		if m.runtimeDialog.active > 0 {
			m.runtimeDialog.active--
		}
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("right", "tab"))):
		if m.runtimeDialog.active < len(options)-1 {
			m.runtimeDialog.active++
		}
		return nil
	case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
		action := m.runtimeDialog.action
		choice := options[m.runtimeDialog.active]
		result := luruntime.UIResult{ActionID: action.ID, Accepted: choice.ID != "cancel", ChoiceID: choice.ID}
		m.replyRuntimeAction(m.runtimeDialog.response, result, nil)
		m.runtimeDialog = runtimeDialogState{}
		return nil
	}
	return nil
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
	options := action.Options
	if len(options) == 0 {
		options = []luruntime.UIOption{{ID: "ok", Label: "OK", Primary: true}, {ID: "cancel", Label: "Cancel"}}
	}
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
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		m.theme.HeaderBrand.Render(title),
		"",
		m.theme.SidebarValue.Render(body),
		"",
		strings.Join(buttons, " "),
		"",
		m.theme.Footer.Render("tab choose  •  enter confirm  •  esc cancel"),
	)
	box := m.theme.PaletteFrame.Width(min(72, max(40, m.width*2/3))).Render(m.theme.PaletteSurface.Render(content))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box, lipgloss.WithWhitespaceChars(" "))
}

func (m *Model) handleRuntimePageKey(msg tea.KeyPressMsg) tea.Cmd {
	if !m.runtimePage.open {
		return nil
	}
	switch {
	case key.Matches(msg, key.NewBinding(key.WithKeys("esc"))):
		m.runtimePage = runtimePageState{}
		return nil
	case msg.Text == "r" || msg.Text == "R":
		return runtimeViewCmd(m.controller, m.runtimePage.view.ID)
	}
	return nil
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
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		m.theme.HeaderBrand.Render(title),
		"",
		m.theme.SidebarValue.Width(panelWidth-8).Render(body),
		"",
		m.theme.Footer.Render("esc close  •  r refresh"),
	)
	box := m.theme.PaletteFrame.Width(panelWidth).Height(panelHeight).Render(m.theme.PaletteSurface.Width(panelWidth - 6).Render(content))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box, lipgloss.WithWhitespaceChars(" "))
}

func renderRuntimeView(view luruntime.RuntimeView, result tools.Result) string {
	content := strings.TrimSpace(result.Content)
	switch strings.TrimSpace(view.Render) {
	case "json":
		var decoded any
		if err := json.Unmarshal([]byte(content), &decoded); err != nil {
			return content
		}
		data, _ := json.MarshalIndent(decoded, "", "  ")
		return string(data)
	case "kv":
		var decoded map[string]any
		if err := json.Unmarshal([]byte(content), &decoded); err != nil {
			return content
		}
		keys := make([]string, 0, len(decoded))
		for key := range decoded {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		lines := make([]string, 0, len(keys))
		for _, key := range keys {
			lines = append(lines, fmt.Sprintf("%s: %v", key, decoded[key]))
		}
		return strings.Join(lines, "\n")
	case "table":
		return renderRuntimeTable(content)
	default:
		return content
	}
}

func renderRuntimeTable(content string) string {
	var rows []map[string]any
	if err := json.Unmarshal([]byte(content), &rows); err != nil {
		return content
	}
	if len(rows) == 0 {
		return ""
	}
	columnSet := map[string]struct{}{}
	for _, row := range rows {
		for key := range row {
			columnSet[key] = struct{}{}
		}
	}
	columns := make([]string, 0, len(columnSet))
	for key := range columnSet {
		columns = append(columns, key)
	}
	sort.Strings(columns)

	widths := make(map[string]int, len(columns))
	for _, column := range columns {
		widths[column] = len(column)
	}
	for _, row := range rows {
		for _, column := range columns {
			widths[column] = max(widths[column], len(fmt.Sprintf("%v", row[column])))
		}
	}

	var lines []string
	lines = append(lines, formatTableRow(columns, widths, func(column string) string { return column }))
	lines = append(lines, formatTableRow(columns, widths, func(column string) string { return strings.Repeat("-", widths[column]) }))
	for _, row := range rows {
		lines = append(lines, formatTableRow(columns, widths, func(column string) string {
			return fmt.Sprintf("%v", row[column])
		}))
	}
	return strings.Join(lines, "\n")
}

func formatTableRow(columns []string, widths map[string]int, value func(string) string) string {
	parts := make([]string, 0, len(columns))
	for _, column := range columns {
		parts = append(parts, padRight(value(column), widths[column]))
	}
	return strings.Join(parts, " | ")
}

func padRight(value string, width int) string {
	if len(value) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-len(value))
}
