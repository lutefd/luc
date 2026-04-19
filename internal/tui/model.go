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
	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/extensions"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/kernel"
	"github.com/lutefd/luc/internal/media"
	"github.com/lutefd/luc/internal/theme"
	"github.com/lutefd/luc/internal/tui/commands"
	"github.com/lutefd/luc/internal/tui/inspector"
	modelspicker "github.com/lutefd/luc/internal/tui/models"
	sessionpicker "github.com/lutefd/luc/internal/tui/sessions"
	themepicker "github.com/lutefd/luc/internal/tui/themes"
	"github.com/lutefd/luc/internal/tui/transcript"
)

type appEventsMsg []history.EventEnvelope
type submitDoneMsg struct{ err error }
type reloadDoneMsg struct{ err error }
type toggleInspectorMsg struct{}
type nextTabMsg struct{}
type openModelPickerMsg struct{}
type openSessionPickerMsg struct{}
type openThemePickerMsg struct{}
type resetThemeMsg struct{}
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
	Paste       key.Binding
	RemoveImage key.Binding
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
	themePicker   themepicker.Model
	pendingImages []media.Attachment
	registry      *commands.Registry
	keys          keyMap
	theme         theme.Theme
	width         int
	height        int
	inspectorOpen bool
	status        string
	lastClickAt   time.Time
	lastClickID   string
	logsDirty     bool
	runtimeBroker *teaUIBroker
	runtimePage   runtimePageState
	runtimeDialog runtimeDialogState
}

func New(controller *kernel.Controller) Model {
	th, variant, _ := theme.Load(controller.Config().UI.Theme, controller.Workspace().Root)

	input := textarea.New()
	input.Placeholder = "Tell luc what to inspect or change..."
	input.Focus()
	input.SetHeight(3)
	input.ShowLineNumbers = false
	input.Prompt = "> "
	input.CharLimit = 0
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter"), key.WithHelp("shift+enter", "newline"))
	applyInputTheme(&input, th, variant)

	registry := commands.NewRegistry()
	model := Model{
		controller:    controller,
		transcript:    transcript.New(th, variant),
		inspector:     inspector.New(controller.Workspace(), controller.Session(), th),
		input:         input,
		palette:       commands.New(registry, th),
		modelPicker:   modelspicker.New(controller.Registry(), th),
		sessionPicker: sessionpicker.New(th),
		themePicker:   themepicker.New(th),
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
			Paste:       key.NewBinding(key.WithKeys("ctrl+v", "super+v"), key.WithHelp("ctrl/cmd+v", "paste")),
			RemoveImage: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "drop image")),
			Copy:        key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "copy")),
			Quit:        key.NewBinding(key.WithKeys("ctrl+c", "ctrl+q"), key.WithHelp("ctrl+c", "quit")),
		},
	}
	model.installRuntimeUI()

	model.transcript.ApplyBatch(controller.InitialEvents())
	model.inspector.ApplyBatch(controller.InitialEvents())
	model.logsDirty = true
	model.syncInspectorLogs(true)
	model.setStatus("Ready")
	return model
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, waitForEvent(m.controller.Events()), waitForUIBroker(m.runtimeBroker.actions))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil
	case tea.MouseWheelMsg:
		if m.sessionPicker.IsOpen() || m.modelPicker.IsOpen() || m.themePicker.IsOpen() || m.palette.IsOpen() || m.runtimeDialog.open || m.runtimePage.open {
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
		if m.sessionPicker.IsOpen() || m.modelPicker.IsOpen() || m.themePicker.IsOpen() || m.palette.IsOpen() || m.runtimeDialog.open || m.runtimePage.open {
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
						if m.transcript.ToggleBlockExpansionAtRow(row) {
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
		if m.sessionPicker.IsOpen() || m.modelPicker.IsOpen() || m.themePicker.IsOpen() || m.palette.IsOpen() || m.runtimeDialog.open || m.runtimePage.open {
			return m, nil
		}
		if msg.Button == tea.MouseLeft || m.transcript.IsSelecting() {
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
	case tea.PasteMsg:
		if m.sessionPicker.IsOpen() || m.modelPicker.IsOpen() || m.themePicker.IsOpen() || m.palette.IsOpen() || m.runtimeDialog.open || m.runtimePage.open {
			return m, nil
		}
		if attachment, ok, err := attachmentFromPasteContent(msg.Content); err != nil {
			m.setStatus("Paste failed: " + err.Error())
			return m, nil
		} else if ok {
			m.pendingImages = append(m.pendingImages, attachment)
			m.resize()
			m.setStatus("Attached image: " + attachment.Name)
			return m, nil
		}
		if strings.TrimSpace(msg.Content) == "" {
			return m, readClipboardImageCmd()
		}
	case tea.KeyPressMsg:
		if m.runtimeDialog.open {
			return m, m.handleRuntimeDialogKey(msg)
		}
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
		if m.themePicker.IsOpen() {
			cmd, _, handled := m.themePicker.Update(msg)
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
		if m.runtimePage.open {
			if cmd := m.handleRuntimePageKey(msg); cmd != nil {
				return m, cmd
			}
			return m, nil
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
		case key.Matches(msg, m.keys.Paste):
			return m, readClipboardCmd(false)
		case key.Matches(msg, m.keys.Send):
			value := strings.TrimSpace(m.input.Value())
			if value == "" && len(m.pendingImages) == 0 {
				return m, nil
			}
			attachments := append([]media.Attachment(nil), m.pendingImages...)
			m.setStatus("Sending...")
			m.input.Reset()
			m.input.SetValue("")
			m.pendingImages = nil
			m.input.Focus()
			m.resize()
			return m, submitCmd(m.controller, value, attachments)
		case key.Matches(msg, m.keys.RemoveImage):
			if len(m.pendingImages) == 0 {
				m.setStatus("No pending image")
				return m, nil
			}
			removed := m.pendingImages[len(m.pendingImages)-1]
			m.pendingImages = m.pendingImages[:len(m.pendingImages)-1]
			m.resize()
			m.setStatus("Removed image: " + removed.Name)
			return m, nil
		case key.Matches(msg, m.keys.TogglePane):
			m.inspectorOpen = !m.inspectorOpen
			m.resize()
			return m, nil
		case key.Matches(msg, m.keys.NextTab):
			if m.inspectorOpen {
				m.inspector.NextTab()
				m.syncInspectorLogs(false)
			}
			return m, m.maybeRefreshActiveRuntimeView()
		case key.Matches(msg, m.keys.PrevTab):
			if m.inspectorOpen {
				m.inspector.PrevTab()
				m.syncInspectorLogs(false)
			}
			return m, m.maybeRefreshActiveRuntimeView()
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
	case appEventsMsg:
		events := []history.EventEnvelope(msg)
		m.transcript.ApplyBatch(events)
		m.inspector.ApplyBatch(events)
		m.inspector.SetSessionMeta(m.controller.Session())
		if appEventsTouchLogs(events) {
			m.logsDirty = true
			m.syncInspectorLogs(false)
		}
		syncRuntimeUI := false
		for _, ev := range events {
			switch ev.Kind {
			case "status.thinking":
				payload := decode[history.StatusPayload](ev.Payload)
				if strings.TrimSpace(payload.Text) != "" {
					m.status = payload.Text
				}
			case "message.assistant.final":
				m.status = "Ready"
			case "reload.finished":
				m.status = "Reloaded"
				syncRuntimeUI = true
			case "reload.failed", "system.error":
				m.status = "Error"
			}
		}
		if syncRuntimeUI {
			m.syncRuntimeUI()
		}
		m.inspector.SetStatus(m.status)
		return m, waitForEvent(m.controller.Events())
	case uiBrokerActionMsg:
		return m, tea.Batch(m.handleRuntimeAction(msg.request.action, msg.request.response), waitForUIBroker(m.runtimeBroker.actions))
	case toggleInspectorMsg:
		m.inspectorOpen = !m.inspectorOpen
		m.resize()
		return m, nil
	case nextTabMsg:
		if m.inspectorOpen {
			m.inspector.NextTab()
			m.syncInspectorLogs(false)
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
	case openThemePickerMsg:
		entries := m.availableThemes()
		m.themePicker.Open(entries, m.controller.Config().UI.Theme)
		return m, nil
	case resetThemeMsg:
		m.applyTheme(config.Default().UI.Theme)
		return m, nil
	case themepicker.Selected:
		m.applyTheme(msg.ThemeName)
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
	case clipboardPasteMsg:
		if msg.err != nil {
			m.setStatus("Paste failed: " + msg.err.Error())
			return m, nil
		}
		if msg.attached.ID == "" {
			if msg.text != "" {
				m.input.InsertString(msg.text)
			}
			return m, nil
		}
		m.pendingImages = append(m.pendingImages, msg.attached)
		m.resize()
		m.setStatus("Attached image: " + msg.attached.Name)
		return m, nil
	case runRuntimeCommandMsg:
		return m, m.handleRuntimeCommand(msg.CommandID)
	case runtimeViewLoadedMsg:
		if msg.Err != nil {
			if m.runtimePage.open && m.runtimePage.view.ID == msg.ViewID {
				m.runtimePage.loading = false
				m.runtimePage.err = msg.Err.Error()
			}
			m.inspector.SetRuntimeViewContent(msg.ViewID, "Error: "+msg.Err.Error())
			m.setStatus("Runtime view error")
			return m, nil
		}
		m.inspector.SetRuntimeViewContent(msg.ViewID, msg.Rendered)
		if m.runtimePage.open && m.runtimePage.view.ID == msg.ViewID {
			m.runtimePage.loading = false
			m.runtimePage.content = msg.Rendered
			m.runtimePage.err = ""
		}
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

	// Clamp each section to its target height so nothing overflows. Each
	// section inherits the app background so unpainted gutter cells (padding
	// introduced by the clamp, whitespace between header/body/footer) pick up
	// the theme color instead of leaking the terminal's default background.
	sectionStyle := m.theme.App
	headerSection := sectionStyle.Width(m.width).Height(headerH).MaxHeight(headerH).Render(header)
	bodySection := sectionStyle.Width(m.width).Height(bodyH).MaxHeight(bodyH).Render(body)
	footerSection := sectionStyle.Width(m.width).Height(footerH).MaxHeight(footerH).Render(footer)

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
	case m.themePicker.IsOpen():
		content = lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.themePicker.View(),
			lipgloss.WithWhitespaceChars(" "),
		)
	case m.palette.IsOpen():
		content = lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			m.palette.View(),
			lipgloss.WithWhitespaceChars(" "),
		)
	case m.runtimeDialog.open:
		content = m.renderRuntimeDialog()
	case m.runtimePage.open:
		content = m.renderRuntimePage()
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	// BackgroundColor is painted by the terminal (OSC 11), so every cell of
	// the alt-screen — including cells we never style with lipgloss — picks
	// up the theme bg. Without this, any row or column a child renderer
	// forgets to paint (e.g. bubbles textarea's untouched rows, padding
	// gutters, viewport empties) leaks the user's terminal background.
	// Built-in themes leave this nil so the terminal's native bg stays.
	v.BackgroundColor = m.theme.Background
	return v
}

func (m *Model) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	transcriptHeight := m.bodyHeight()
	m.transcript.SetSize(m.transcriptWidth(), transcriptHeight)
	m.inspector.SetSize(m.inspectorWidth(), m.inspectorHeight())
	m.input.SetWidth(max(24, m.transcriptWidth()-4))
	m.palette.SetSize(m.width, m.height)
	m.modelPicker.SetSize(m.width, m.height)
	m.sessionPicker.SetSize(m.width, m.height)
	m.themePicker.SetSize(m.width, m.height)
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
	_, _, bodyH := m.layoutHeights()
	return bodyH
}

func (m Model) layoutHeights() (headerH, footerH, bodyH int) {
	headerH = lipgloss.Height(m.renderHeader())
	footerH = lipgloss.Height(m.renderFooter())
	bodyH = max(1, m.height-headerH-footerH)
	return headerH, footerH, bodyH
}

func (m Model) wheelInBody(msg tea.MouseWheelMsg) bool {
	headerH, _, bodyH := m.layoutHeights()
	return msg.Y >= headerH && msg.Y < headerH+bodyH
}

func (m Model) transcriptMouseRow(x, y int) (int, bool) {
	headerH, _, bodyH := m.layoutHeights()
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
		"ctrl/cmd+v paste",
		"ctrl+p commands",
		"ctrl+m model",
		"ctrl+l sessions",
		"ctrl+y copy",
		"ctrl+o details",
	}
	if len(m.pendingImages) > 0 {
		bindings = append(bindings, "ctrl+d drop image")
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
	if len(m.pendingImages) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, frame, hints)
	}
	return lipgloss.JoinVertical(lipgloss.Left, frame, m.renderPendingImages(), hints)
}

func waitForEvent(ch <-chan history.EventEnvelope) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		batch := []history.EventEnvelope{ev}
	drain:
		for len(batch) < 256 {
			select {
			case ev, ok := <-ch:
				if !ok {
					break drain
				}
				batch = append(batch, ev)
			default:
				break drain
			}
		}
		return appEventsMsg(compactAppEvents(batch))
	}
}

func compactAppEvents(events []history.EventEnvelope) []history.EventEnvelope {
	if len(events) < 2 {
		return events
	}

	out := make([]history.EventEnvelope, 0, len(events))
	for _, ev := range events {
		if ev.Kind != "message.assistant.delta" {
			out = append(out, ev)
			continue
		}
		payload := decode[history.MessageDeltaPayload](ev.Payload)
		if len(out) == 0 || out[len(out)-1].Kind != "message.assistant.delta" {
			out = append(out, ev)
			continue
		}
		prev := decode[history.MessageDeltaPayload](out[len(out)-1].Payload)
		if prev.ID != payload.ID {
			out = append(out, ev)
			continue
		}
		prev.Delta += payload.Delta
		out[len(out)-1].Seq = ev.Seq
		out[len(out)-1].At = ev.At
		out[len(out)-1].Payload = prev
	}
	return out
}

func appEventsTouchLogs(events []history.EventEnvelope) bool {
	for _, ev := range events {
		switch ev.Kind {
		case "tool.finished", "reload.failed", "system.error", "hook.failed":
			return true
		}
	}
	return false
}

func submitCmd(controller *kernel.Controller, value string, attachments []media.Attachment) tea.Cmd {
	return func() tea.Msg {
		return submitDoneMsg{err: controller.SubmitMessage(context.Background(), value, attachments)}
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

func (m *Model) syncInspectorLogs(force bool) {
	if !force && (!m.logsDirty || !m.inspector.IsLogsActive()) {
		return
	}
	m.inspector.SetLogs(m.controller.LogEntries())
	m.logsDirty = false
}

func (m *Model) resetSessionViews() {
	th, variant, _ := theme.Load(m.controller.Config().UI.Theme, m.controller.Workspace().Root)
	m.theme = th
	m.transcript = transcript.New(m.theme, variant)
	m.inspector = inspector.New(m.controller.Workspace(), m.controller.Session(), m.theme)
	m.palette = commands.New(m.registry, m.theme)
	m.modelPicker = modelspicker.New(m.controller.Registry(), m.theme)
	m.sessionPicker = sessionpicker.New(m.theme)
	m.pendingImages = nil
	applyInputTheme(&m.input, m.theme, variant)
	m.runtimeDialog = runtimeDialogState{}
	m.runtimePage = runtimePageState{}
	m.transcript.ApplyBatch(m.controller.InitialEvents())
	m.inspector.ApplyBatch(m.controller.InitialEvents())
	m.logsDirty = true
	m.syncInspectorLogs(true)
	m.inspector.SetStatus(m.status)
	m.syncRuntimeUI()
	m.resize()
}

func (m *Model) setStatus(status string) {
	m.status = status
	m.inspector.SetStatus(status)
}

// availableThemes enumerates the entries shown by the theme picker: the two
// compiled-in variants (light/dark) plus every YAML/JSON file found under the
// workspace and user theme directories.
func (m Model) availableThemes() []themepicker.Entry {
	entries := []themepicker.Entry{
		{Name: theme.VariantLight, Display: "light", BuiltIn: true},
		{Name: theme.VariantDark, Display: "dark", BuiltIn: true},
	}
	names, err := extensions.ListThemes(m.controller.Workspace().Root)
	if err == nil {
		for _, name := range names {
			if name == theme.VariantLight || name == theme.VariantDark {
				continue
			}
			entries = append(entries, themepicker.Entry{Name: name, Display: name})
		}
	}
	return entries
}

// applyTheme swaps the active theme and rebuilds every view that caches
// styles. The transcript is re-populated from the controller's initial event
// log so no session content is lost. The active scroll position is
// intentionally not preserved — re-theming is uncommon and avoiding a partial
// redraw is simpler than selectively recomputing styles.
func (m *Model) applyTheme(name string) {
	m.controller.SetTheme(name)
	th, variant, err := theme.Load(name, m.controller.Workspace().Root)
	if err != nil {
		m.setStatus("Theme error: " + err.Error())
		return
	}
	m.theme = th
	m.transcript = transcript.New(m.theme, variant)
	m.inspector = inspector.New(m.controller.Workspace(), m.controller.Session(), m.theme)
	m.palette = commands.New(m.registry, m.theme)
	m.modelPicker = modelspicker.New(m.controller.Registry(), m.theme)
	m.sessionPicker = sessionpicker.New(m.theme)
	m.themePicker = themepicker.New(m.theme)
	applyInputTheme(&m.input, m.theme, variant)
	m.transcript.ApplyBatch(m.controller.InitialEvents())
	m.inspector.ApplyBatch(m.controller.InitialEvents())
	m.logsDirty = true
	m.syncInspectorLogs(true)
	m.inspector.SetStatus(m.status)
	m.resize()

	if name == "" {
		m.setStatus("Theme: default")
	} else {
		m.setStatus("Theme: " + name)
	}
}

func applyInputTheme(input *textarea.Model, th theme.Theme, variant string) {
	// The bubbles textarea has eight distinct style slots per focus state
	// (Base, Text, LineNumber, CursorLineNumber, CursorLine, EndOfBuffer,
	// Placeholder, Prompt). Leaving any of them at bubbles' default means
	// some cell in the input area will render with bubbles' assumed
	// background instead of our theme's — visible as a light band inside
	// the rounded input frame on dark themes. Set every slot explicitly.
	styles := textarea.DefaultStyles(variant == theme.VariantDark)
	base := th.InputText
	state := textarea.StyleState{
		Base:             base,
		Text:             base,
		LineNumber:       base,
		CursorLineNumber: base,
		CursorLine:       base,
		EndOfBuffer:      base,
		Placeholder:      th.InputPlaceholder,
		Prompt:           th.InputPrompt,
	}
	styles.Focused = state
	styles.Blurred = state
	input.SetStyles(styles)
}

func decode[T any](payload any) T {
	var out T
	data, _ := json.Marshal(payload)
	_ = json.Unmarshal(data, &out)
	return out
}
