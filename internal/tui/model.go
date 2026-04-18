package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
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
	Send          key.Binding
	TogglePane    key.Binding
	RotateTab     key.Binding
	ScrollUp      key.Binding
	ScrollDown    key.Binding
	Reload        key.Binding
	Quit          key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.TogglePane, k.RotateTab, k.Reload, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Send, k.TogglePane, k.RotateTab, k.ScrollUp, k.ScrollDown, k.Reload, k.Quit}}
}

type Model struct {
	controller    *kernel.Controller
	transcript    transcript.Model
	inspector     inspector.Model
	input         textarea.Model
	help          help.Model
	keys          keyMap
	theme         theme.Theme
	width         int
	height        int
	inspectorOpen bool
	status        string
}

func New(controller *kernel.Controller) Model {
	th := theme.Default()
	input := textarea.New()
	input.Placeholder = "Ask luc to inspect or change the workspace. Ctrl+S to send."
	input.Focus()
	input.SetHeight(4)
	input.ShowLineNumbers = false
	input.Prompt = "│ "

	model := Model{
		controller:    controller,
		transcript:    transcript.New(th),
		inspector:     inspector.New(controller.Workspace(), controller.Session().SessionID, controller.Config().Provider.Model),
		input:         input,
		help:          help.New(),
		theme:         th,
		inspectorOpen: true,
		keys: keyMap{
			Send:       key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "send")),
			TogglePane: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "toggle inspector")),
			RotateTab:  key.NewBinding(key.WithKeys("ctrl+]"), key.WithHelp("ctrl+]", "next inspector tab")),
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
			return m, submitCmd(m.controller, value)
		case key.Matches(msg, m.keys.TogglePane):
			m.inspectorOpen = !m.inspectorOpen
			m.resize()
			return m, nil
		case key.Matches(msg, m.keys.RotateTab):
			m.inspector.NextTab()
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
	header := m.theme.Header.Render(fmt.Sprintf("luc  %s  %s", m.controller.Workspace().Root, m.controller.Config().Provider.Model))
	status := m.theme.Muted.Render(m.status)

	transcriptView := lipgloss.NewStyle().Width(m.transcriptWidth()).Height(max(1, m.height-9)).Render(m.transcript.View())
	body := transcriptView
	if m.inspectorOpen {
		inspectorView := lipgloss.NewStyle().
			Width(m.inspectorWidth()).
			Height(max(1, m.height-9)).
			Border(lipgloss.NormalBorder(), true, false, false, true).
			BorderForeground(lipgloss.Color("#4a6d7c")).
			Render(m.inspector.View())
		body = lipgloss.JoinHorizontal(lipgloss.Top, transcriptView, inspectorView)
	}

	footer := lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.NewStyle().BorderTop(true).BorderForeground(lipgloss.Color("#4a6d7c")).Render(m.input.View()),
		m.theme.Footer.Render(m.help.View(m.keys)),
		status,
	)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m *Model) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	transcriptHeight := max(1, m.height-9)
	m.transcript.SetSize(m.transcriptWidth(), transcriptHeight)
	if m.inspectorOpen {
		m.inspector.SetSize(m.inspectorWidth(), transcriptHeight)
	}
	m.input.SetWidth(max(20, m.width-4))
}

func (m Model) transcriptWidth() int {
	if !m.inspectorOpen {
		return max(20, m.width-2)
	}
	return max(20, int(float64(m.width)*0.66)-1)
}

func (m Model) inspectorWidth() int {
	return max(20, m.width-m.transcriptWidth()-1)
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
