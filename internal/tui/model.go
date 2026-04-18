package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/kernel"
	"github.com/lutefd/luc/internal/theme"
	"github.com/lutefd/luc/internal/tui/inspector"
	"github.com/lutefd/luc/internal/tui/transcript"
)

type appEventMsg history.EventEnvelope
type submitDoneMsg struct{ err error }
type reloadDoneMsg struct{ err error }

type keyMap struct {
	Send       key.Binding
	Newline    key.Binding
	TogglePane key.Binding
	ScrollUp   key.Binding
	ScrollDown key.Binding
	Reload     key.Binding
	Quit       key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.Newline, k.TogglePane, k.Reload, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Send, k.Newline, k.TogglePane, k.ScrollUp, k.ScrollDown, k.Reload, k.Quit}}
}

type Model struct {
	controller    *kernel.Controller
	transcript    transcript.Model
	inspector     inspector.Model
	input         textarea.Model
	keys          keyMap
	theme         theme.Theme
	width         int
	height        int
	inspectorOpen bool
	status        string
}

func New(controller *kernel.Controller) Model {
	variant := theme.ResolveVariant(controller.Config().UI.Theme)
	th := theme.Default(variant)
	input := textarea.New()
	input.Placeholder = "Tell luc what to inspect or change..."
	input.Focus()
	input.SetHeight(3)
	input.ShowLineNumbers = false
	input.Prompt = "> "
	input.CharLimit = 0
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter"), key.WithHelp("shift+enter", "newline"))
	input.FocusedStyle.Base = th.InputText
	input.FocusedStyle.CursorLine = th.InputText
	input.FocusedStyle.Placeholder = th.InputPlaceholder
	input.FocusedStyle.Prompt = th.InputPrompt
	input.BlurredStyle = input.FocusedStyle

	model := Model{
		controller:    controller,
		transcript:    transcript.New(th, variant),
		inspector:     inspector.New(controller.Workspace(), controller.Session().SessionID, controller.Config().Provider.Model, th),
		input:         input,
		theme:         th,
		inspectorOpen: controller.Config().UI.InspectorOpen,
		keys: keyMap{
			Send:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
			Newline:    key.NewBinding(key.WithKeys("shift+enter", "alt+enter"), key.WithHelp("shift+enter", "newline")),
			TogglePane: key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "details")),
			ScrollUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "scroll up")),
			ScrollDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdown", "scroll down")),
			Reload:     key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "reload")),
			Quit:       key.NewBinding(key.WithKeys("ctrl+c", "ctrl+q"), key.WithHelp("ctrl+c", "quit")),
		},
	}

	for _, ev := range controller.InitialEvents() {
		model.transcript.Apply(ev)
		model.inspector.Apply(ev)
	}
	model.inspector.SetLogs(controller.LogEntries())
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
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Send):
			value := strings.TrimSpace(m.input.Value())
			if value == "" {
				return m, nil
			}
			m.status = "Sending..."
			m.input.Reset()
			m.input.SetValue("")
			m.input.Focus()
			return m, submitCmd(m.controller, value)
		case key.Matches(msg, m.keys.TogglePane):
			m.inspectorOpen = !m.inspectorOpen
			m.resize()
			return m, nil
		case key.Matches(msg, m.keys.ScrollUp):
			m.transcript.UpdateViewport(msg)
			return m, nil
		case key.Matches(msg, m.keys.ScrollDown):
			m.transcript.UpdateViewport(msg)
			return m, nil
		case key.Matches(msg, m.keys.Reload):
			m.status = "Reloading..."
			return m, reloadCmd(m.controller)
		}
	case appEventMsg:
		ev := history.EventEnvelope(msg)
		m.transcript.Apply(ev)
		m.inspector.Apply(ev)
		m.inspector.SetLogs(m.controller.LogEntries())
		switch ev.Kind {
		case "message.assistant.final":
			m.status = "Ready"
		case "reload.finished":
			m.status = "Reloaded"
		case "reload.failed", "system.error":
			m.status = "Error"
		}
		return m, waitForEvent(m.controller.Events())
	case submitDoneMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
		} else {
			m.status = "Ready"
		}
		return m, nil
	case reloadDoneMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	header := m.renderHeader()
	body := m.renderBody()
	footer := m.renderFooter()
	view := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	if m.width <= 0 || m.height <= 0 {
		return view
	}
	return m.theme.App.Width(m.width).Height(m.height).Render(view)
}

func (m *Model) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	transcriptHeight := max(1, m.bodyHeight())
	m.transcript.SetSize(m.transcriptWidth(), transcriptHeight)
	m.inspector.SetSize(m.inspectorWidth(), m.inspectorHeight())
	m.input.SetWidth(max(24, m.transcriptWidth()-4))
}

func (m Model) transcriptWidth() int {
	if !m.hasSidebar() {
		return max(24, m.width-4)
	}
	return max(24, m.width-m.inspectorWidth()-3)
}

func (m Model) inspectorWidth() int {
	switch {
	case m.width < 120:
		if m.inspectorOpen {
			return max(24, m.width-4)
		}
		return 0
	case m.inspectorOpen:
		return 42
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

func (m Model) hasSidebar() bool {
	return m.inspectorOpen
}

func (m Model) renderHeader() string {
	usable := max(0, m.width-2)
	ruleWidth := max(4, usable-36)
	brand := m.theme.HeaderBrand.Render("luc")
	rule := m.theme.HeaderRule.Render(strings.Repeat("/", ruleWidth))
	meta := m.theme.HeaderMeta.Render(fmt.Sprintf("%s • %s", m.controller.Workspace().Root, m.controller.Config().Provider.Model))
	return lipgloss.JoinHorizontal(lipgloss.Center, brand, " ", rule, " ", meta)
}

func (m Model) renderBody() string {
	transcriptView := m.theme.Body.Width(m.transcriptWidth()).Height(m.bodyHeight()).Render(m.transcript.View())

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
		return lipgloss.JoinHorizontal(lipgloss.Top, transcriptView, " ", sidebar)
	default:
		return transcriptView
	}
}

func (m Model) renderFooter() string {
	frame := m.theme.InputFrame.Width(max(24, m.transcriptWidth())).Render(m.input.View())
	statusStyle := m.theme.StatusReady
	switch {
	case strings.Contains(strings.ToLower(m.status), "error"):
		statusStyle = m.theme.StatusError
	case strings.Contains(strings.ToLower(m.status), "send"), strings.Contains(strings.ToLower(m.status), "reload"):
		statusStyle = m.theme.StatusBusy
	}
	hints := m.theme.Footer.Render("enter send  •  shift+enter newline  •  ctrl+o details  •  ctrl+r reload  •  ctrl+c quit")
	status := statusStyle.Render(strings.TrimSpace(m.status))
	return lipgloss.JoinVertical(lipgloss.Left, frame, hints, status)
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
