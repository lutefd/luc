package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/kernel"
	"github.com/lutefd/luc/internal/theme"
	"github.com/lutefd/luc/internal/tui/commands"
	"github.com/lutefd/luc/internal/tui/inspector"
	modelspicker "github.com/lutefd/luc/internal/tui/models"
	sessionpicker "github.com/lutefd/luc/internal/tui/sessions"
	"github.com/lutefd/luc/internal/tui/transcript"
)

type appEventMsg history.EventEnvelope
type submitDoneMsg struct{ err error }
type reloadDoneMsg struct{ err error }
type toggleInspectorMsg struct{}
type nextTabMsg struct{}
type openModelPickerMsg struct{}
type openSessionPickerMsg struct{}
type newSessionMsg struct{}
type copySelectionMsg struct{}

type keyMap struct {
	Send        key.Binding
	Newline     key.Binding
	TogglePane  key.Binding
	NextTab     key.Binding
	PrevTab     key.Binding
	ScrollUp    key.Binding
	ScrollDown  key.Binding
	Reload      key.Binding
	Palette     key.Binding
	ModelPick   key.Binding
	SessionPick key.Binding
	Copy        key.Binding
	Quit        key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.Newline, k.TogglePane, k.Reload, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Send, k.Newline, k.TogglePane, k.NextTab, k.PrevTab, k.ScrollUp, k.ScrollDown, k.ModelPick, k.SessionPick, k.Reload, k.Quit}}
}

type Model struct {
	controller    *kernel.Controller
	transcript    transcript.Model
	inspector     inspector.Model
	input         textarea.Model
	palette       commands.Model
	modelPicker   modelspicker.Model
	sessionPicker sessionpicker.Model
	registry      *commands.Registry
	keys          keyMap
	theme         theme.Theme
	width         int
	height        int
	inspectorOpen bool
	status        string
	lastClickAt   time.Time
	lastClickID   string
}

func New(controller *kernel.Controller) Model {
	variant := theme.ResolveVariant(controller.Config().UI.Theme)
	th := theme.Default(variant)
	isDark := variant == theme.VariantDark

	input := textarea.New()
	input.Placeholder = "Tell luc what to inspect or change..."
	input.Focus()
	input.SetHeight(3)
	input.ShowLineNumbers = false
	input.Prompt = "> "
	input.CharLimit = 0
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter"), key.WithHelp("shift+enter", "newline"))

	styles := textarea.DefaultStyles(isDark)
	styles.Focused.Base = th.InputText
	styles.Focused.CursorLine = th.InputText
	styles.Focused.Placeholder = th.InputPlaceholder
	styles.Focused.Prompt = th.InputPrompt
	styles.Blurred = styles.Focused
	input.SetStyles(styles)

	registry := commands.NewRegistry()
	model := Model{
		controller:    controller,
		transcript:    transcript.New(th, variant),
		inspector:     inspector.New(controller.Workspace(), controller.Session(), th),
		input:         input,
		palette:       commands.New(registry, th),
		modelPicker:   modelspicker.New(controller.Registry(), th),
		sessionPicker: sessionpicker.New(th),
		registry:      registry,
		theme:         th,
		inspectorOpen: controller.Config().UI.InspectorOpen,
		keys: keyMap{
			Send:        key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
			Newline:     key.NewBinding(key.WithKeys("shift+enter", "alt+enter"), key.WithHelp("shift+enter", "newline")),
			TogglePane:  key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "details")),
			NextTab:     key.NewBinding(key.WithKeys("ctrl+]"), key.WithHelp("ctrl+]", "next tab")),
			PrevTab:     key.NewBinding(key.WithKeys("ctrl+\\"), key.WithHelp("ctrl+\\", "prev tab")),
			ScrollUp:    key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "scroll up")),
			ScrollDown:  key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdown", "scroll down")),
			Reload:      key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "reload")),
			Palette:     key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "commands")),
			ModelPick:   key.NewBinding(key.WithKeys("ctrl+m"), key.WithHelp("ctrl+m", "model")),
			SessionPick: key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("ctrl+l", "sessions")),
			Copy:        key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "copy")),
			Quit:        key.NewBinding(key.WithKeys("ctrl+c", "ctrl+q"), key.WithHelp("ctrl+c", "quit")),
		},
	}

	// Seed built-in commands. Extensions can append more via model.registry.Register(...).
	registry.Register(commands.Command{
		ID: "reload", Name: "Reload runtime", Shortcut: "ctrl+r",
		Run: func() tea.Cmd { return reloadCmd(controller) },
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
		ID: "session.new", Name: "New session", Shortcut: "",
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
		ID: "quit", Name: "Quit", Shortcut: "ctrl+c",
		Run: func() tea.Cmd { return tea.Quit },
	})

	for _, ev := range controller.InitialEvents() {
		model.transcript.Apply(ev)
		model.inspector.Apply(ev)
	}
	model.inspector.SetLogs(controller.LogEntries())
	model.setStatus("Ready")
	return model
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, waitForEvent(m.controller.Events()))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil
	case tea.MouseWheelMsg:
		if m.sessionPicker.IsOpen() || m.modelPicker.IsOpen() || m.palette.IsOpen() {
			return m, nil
		}
		if !m.wheelInBody(msg) {
			return m, nil
		}
		if m.inspectorOpen && m.hasSidebar() && msg.X >= m.transcriptWidth()+1 {
			m.inspector.HandleWheel(msg)
		} else {
			m.transcript.HandleWheel(msg)
		}
		return m, nil
	case tea.MouseClickMsg:
		if m.sessionPicker.IsOpen() || m.modelPicker.IsOpen() || m.palette.IsOpen() {
			return m, nil
		}
		if msg.Button == tea.MouseLeft {
			if row, ok := m.transcriptMouseRow(msg.X, msg.Y); ok {
				if blockID, ok := m.transcript.BlockIDAtRow(row); ok {
					now := time.Now()
					if blockID != "" && blockID == m.lastClickID && now.Sub(m.lastClickAt) <= 400*time.Millisecond {
						m.lastClickAt = time.Time{}
						m.lastClickID = ""
						m.transcript.ClearSelection()
						if m.transcript.ToggleToolExpansionAtRow(row) {
							return m, nil
						}
					}
					m.lastClickAt = now
					m.lastClickID = blockID
				}
				m.transcript.BeginSelection(row)
			} else {
				m.lastClickAt = time.Time{}
				m.lastClickID = ""
				m.transcript.ClearSelection()
			}
		}
		return m, nil
	case tea.MouseMotionMsg:
		if m.sessionPicker.IsOpen() || m.modelPicker.IsOpen() || m.palette.IsOpen() {
			return m, nil
		}
		if msg.Button == tea.MouseLeft {
			if row, ok := m.transcriptMouseRow(msg.X, msg.Y); ok {
				m.transcript.ExtendSelection(row)
			}
		}
		return m, nil
	case tea.MouseReleaseMsg:
		if msg.Button == tea.MouseLeft {
			m.transcript.EndSelection()
		}
		return m, nil
	case tea.KeyPressMsg:
		// Route to open modal first.
		if m.sessionPicker.IsOpen() {
			cmd, _, handled := m.sessionPicker.Update(msg)
			if handled {
				return m, cmd
			}
		}
		if m.modelPicker.IsOpen() {
			cmd, _, handled := m.modelPicker.Update(msg)
			if handled {
				return m, cmd
			}
		}
		if m.palette.IsOpen() {
			cmd, _, handled := m.palette.Update(msg)
			if handled {
				return m, cmd
			}
		}
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Palette):
			m.palette.Open()
			return m, nil
		case key.Matches(msg, m.keys.ModelPick):
			m.modelPicker.Open(m.controller.Config().Provider.Model)
			return m, nil
		case key.Matches(msg, m.keys.SessionPick):
			sessions, err := m.controller.Sessions()
			if err != nil {
				m.setStatus("Error: " + err.Error())
				return m, nil
			}
			m.sessionPicker.Open(m.controller.Session().SessionID, sessions)
			return m, nil
		case key.Matches(msg, m.keys.Copy):
			return m, copySelectionCmd()
		case key.Matches(msg, m.keys.Send):
			value := strings.TrimSpace(m.input.Value())
			if value == "" {
				return m, nil
			}
			m.setStatus("Sending...")
			m.input.Reset()
			m.input.SetValue("")
			m.input.Focus()
			return m, submitCmd(m.controller, value)
		case key.Matches(msg, m.keys.TogglePane):
			m.inspectorOpen = !m.inspectorOpen
			m.resize()
			return m, nil
		case key.Matches(msg, m.keys.NextTab):
			if m.inspectorOpen {
				m.inspector.NextTab()
			}
			return m, nil
		case key.Matches(msg, m.keys.PrevTab):
			if m.inspectorOpen {
				m.inspector.PrevTab()
			}
			return m, nil
		case key.Matches(msg, m.keys.ScrollUp):
			m.transcript.UpdateViewport(msg)
			return m, nil
		case key.Matches(msg, m.keys.ScrollDown):
			m.transcript.UpdateViewport(msg)
			return m, nil
		case key.Matches(msg, m.keys.Reload):
			m.setStatus("Reloading...")
			return m, reloadCmd(m.controller)
		}
	case appEventMsg:
		ev := history.EventEnvelope(msg)
		m.transcript.Apply(ev)
		m.inspector.Apply(ev)
		m.inspector.SetSessionMeta(m.controller.Session())
		m.inspector.SetLogs(m.controller.LogEntries())
		switch ev.Kind {
		case "status.thinking":
			payload := decode[history.StatusPayload](ev.Payload)
			if strings.TrimSpace(payload.Text) != "" {
				m.setStatus(payload.Text)
			}
		case "message.assistant.final":
			m.setStatus("Ready")
		case "reload.finished":
			m.setStatus("Reloaded")
		case "reload.failed", "system.error":
			m.setStatus("Error")
		}
		m.inspector.SetStatus(m.status)
		return m, waitForEvent(m.controller.Events())
	case toggleInspectorMsg:
		m.inspectorOpen = !m.inspectorOpen
		m.resize()
		return m, nil
	case nextTabMsg:
		if m.inspectorOpen {
			m.inspector.NextTab()
		}
		return m, nil
	case openModelPickerMsg:
		m.modelPicker.Open(m.controller.Config().Provider.Model)
		return m, nil
	case openSessionPickerMsg:
		sessions, err := m.controller.Sessions()
		if err != nil {
			m.setStatus("Error: " + err.Error())
			return m, nil
		}
		m.sessionPicker.Open(m.controller.Session().SessionID, sessions)
		return m, nil
	case newSessionMsg:
		if err := m.controller.NewSession(); err != nil {
			m.setStatus("Error: " + err.Error())
			return m, nil
		}
		m.resetSessionViews()
		m.setStatus("New session: " + m.controller.Session().SessionID)
		return m, nil
	case copySelectionMsg:
		text := m.transcript.SelectedText()
		if strings.TrimSpace(text) == "" {
			m.setStatus("Nothing selected")
			return m, nil
		}
		if err := clipboard.WriteAll(text); err != nil {
			m.setStatus("Copy failed: " + err.Error())
		} else {
			m.setStatus("Copied selection")
		}
		return m, nil
	case modelspicker.Selected:
		if err := m.controller.SwitchModel(msg.ModelID); err != nil {
			m.setStatus("Error: " + err.Error())
		} else {
			m.inspector.SetSessionMeta(m.controller.Session())
			m.setStatus("Model: " + msg.ModelID)
		}
		return m, nil
	case sessionpicker.Selected:
		if err := m.controller.OpenSession(msg.SessionID); err != nil {
			m.setStatus("Error: " + err.Error())
			return m, nil
		}
		m.resetSessionViews()
		m.setStatus("Session: " + msg.SessionID)
		return m, nil
	case submitDoneMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error())
		} else {
			m.setStatus("Ready")
		}
		m.inspector.SetSessionMeta(m.controller.Session())
		return m, nil
	case reloadDoneMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error())
		}
		m.inspector.SetSessionMeta(m.controller.Session())
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) View() tea.View {
	if m.width <= 0 || m.height <= 0 {
		v := tea.NewView("")
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}

	// Render the parts, measure footer, and give the body the remainder.
	// This guarantees the footer (input + hints + status) is ALWAYS visible.
	header := m.renderHeader()
	footer := m.renderFooter()
	headerH := lipgloss.Height(header)
	footerH := lipgloss.Height(footer)
	bodyH := max(1, m.height-headerH-footerH)

	body := m.renderBodyWithHeight(bodyH)

	// Clamp each section to its target height so nothing overflows.
	headerSection := lipgloss.NewStyle().Width(m.width).Height(headerH).MaxHeight(headerH).Render(header)
	bodySection := lipgloss.NewStyle().Width(m.width).Height(bodyH).MaxHeight(bodyH).Render(body)
	footerSection := lipgloss.NewStyle().Width(m.width).Height(footerH).MaxHeight(footerH).Render(footer)

	content := lipgloss.JoinVertical(lipgloss.Left, headerSection, bodySection, footerSection)

	// Overlay modals (model picker wins over command palette).
	switch {
	case m.sessionPicker.IsOpen():
		content = lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.sessionPicker.View(),
			lipgloss.WithWhitespaceChars(" "),
		)
	case m.modelPicker.IsOpen():
		content = lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.modelPicker.View(),
			lipgloss.WithWhitespaceChars(" "),
		)
	case m.palette.IsOpen():
		content = lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.palette.View(),
			lipgloss.WithWhitespaceChars(" "),
		)
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *Model) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	transcriptHeight := max(1, m.bodyHeight())
	m.transcript.SetSize(m.transcriptWidth(), transcriptHeight)
	m.inspector.SetSize(m.inspectorWidth(), m.inspectorHeight())
	m.input.SetWidth(max(24, m.transcriptWidth()-4))
	m.palette.SetSize(m.width, m.height)
	m.modelPicker.SetSize(m.width, m.height)
	m.sessionPicker.SetSize(m.width, m.height)
}

func (m Model) transcriptWidth() int {
	iw := m.inspectorWidth()
	if iw == 0 {
		return max(24, m.width-4)
	}
	return max(24, m.width-iw-3)
}

func (m Model) inspectorWidth() int {
	switch {
	case m.width < 120:
		if m.inspectorOpen {
			return max(24, m.width-4)
		}
		return 0
	case m.inspectorOpen:
		return min(56, max(42, m.width/4))
	default:
		return 30
	}
}

func (m Model) inspectorHeight() int {
	if m.width < 120 && m.inspectorOpen {
		return max(8, m.height/3)
	}
	return max(1, m.bodyHeight())
}

func (m Model) bodyHeight() int {
	return max(1, m.height-8)
}

func (m Model) wheelInBody(msg tea.MouseWheelMsg) bool {
	headerH := lipgloss.Height(m.renderHeader())
	footerH := lipgloss.Height(m.renderFooter())
	bodyH := max(1, m.height-headerH-footerH)
	return msg.Y >= headerH && msg.Y < headerH+bodyH
}

func (m Model) transcriptMouseRow(x, y int) (int, bool) {
	headerH := lipgloss.Height(m.renderHeader())
	footerH := lipgloss.Height(m.renderFooter())
	bodyH := max(1, m.height-headerH-footerH)
	if y < headerH || y >= headerH+bodyH {
		return 0, false
	}
	if x < 0 || x >= m.transcriptWidth() {
		return 0, false
	}
	return y - headerH, true
}

func (m Model) hasSidebar() bool {
	return m.width >= 120
}

func (m Model) renderHeader() string {
	usable := max(0, m.width-2)
	ruleWidth := max(4, usable-36)
	brand := m.theme.HeaderBrand.Render("luc")
	rule := m.theme.HeaderRule.Render(strings.Repeat("─", ruleWidth))
	meta := m.theme.HeaderMeta.Render(fmt.Sprintf("%s • %s", m.controller.Workspace().Root, m.controller.Config().Provider.Model))
	return lipgloss.JoinHorizontal(lipgloss.Center, brand, " ", rule, " ", meta)
}

func (m Model) renderBody() string {
	return m.renderBodyWithHeight(m.bodyHeight())
}

func (m Model) renderBodyWithHeight(bodyH int) string {
	transcriptView := m.theme.Body.Width(m.transcriptWidth()).Height(bodyH).MaxHeight(bodyH).Render(m.transcript.View())

	switch {
	case m.width < 120 && m.inspectorOpen:
		detail := m.inspector.DetailView()
		return lipgloss.JoinVertical(
			lipgloss.Left,
			transcriptView,
			lipgloss.NewStyle().Height(m.inspectorHeight()).Render(detail),
		)
	case m.hasSidebar():
		sidebar := m.inspector.SummaryView()
		if m.inspectorOpen {
			sidebar = m.inspector.DetailView()
		}
		sep := m.theme.Subtle.Render(strings.Repeat("│\n", max(1, bodyH)))
		return lipgloss.JoinHorizontal(lipgloss.Top, transcriptView, sep, sidebar)
	default:
		return transcriptView
	}
}

func (m Model) renderFooter() string {
	frame := m.theme.InputFrame.Width(max(24, m.transcriptWidth())).Render(m.input.View())

	// Build hints dynamically: pick the shortest set that fits the width so
	// the hint line never wraps (which would silently clip the footer).
	bindings := []string{
		"enter send",
		"shift+enter newline",
		"ctrl+p commands",
		"ctrl+m model",
		"ctrl+l sessions",
		"ctrl+y copy",
		"ctrl+o details",
	}
	if m.inspectorOpen {
		bindings = append(bindings, "ctrl+] tab")
	}
	bindings = append(bindings, "ctrl+r reload", "ctrl+c quit")

	sep := "  •  "
	hintStr := strings.Join(bindings, sep)
	if lipgloss.Width(hintStr) > m.width {
		// Fallback to essentials when the terminal is narrow.
		essentials := []string{"enter send", "ctrl+p cmds", "ctrl+c quit"}
		hintStr = strings.Join(essentials, sep)
	}
	hints := m.theme.Footer.Render(hintStr)
	return lipgloss.JoinVertical(lipgloss.Left, frame, hints)
}

func waitForEvent(ch <-chan history.EventEnvelope) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return appEventMsg(ev)
	}
}

func submitCmd(controller *kernel.Controller, value string) tea.Cmd {
	return func() tea.Msg {
		return submitDoneMsg{err: controller.Submit(context.Background(), value)}
	}
}

func reloadCmd(controller *kernel.Controller) tea.Cmd {
	return func() tea.Msg {
		return reloadDoneMsg{err: controller.Reload(context.Background())}
	}
}

func copySelectionCmd() tea.Cmd {
	return func() tea.Msg {
		return copySelectionMsg{}
	}
}

func (m *Model) resetSessionViews() {
	variant := theme.ResolveVariant(m.controller.Config().UI.Theme)
	m.transcript = transcript.New(m.theme, variant)
	m.inspector = inspector.New(m.controller.Workspace(), m.controller.Session(), m.theme)
	for _, ev := range m.controller.InitialEvents() {
		m.transcript.Apply(ev)
		m.inspector.Apply(ev)
	}
	m.inspector.SetLogs(m.controller.LogEntries())
	m.inspector.SetStatus(m.status)
	m.resize()
}

func (m *Model) setStatus(status string) {
	m.status = status
	m.inspector.SetStatus(status)
}

func decode[T any](payload any) T {
	var out T
	data, _ := json.Marshal(payload)
	_ = json.Unmarshal(data, &out)
	return out
}
