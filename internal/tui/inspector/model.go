package inspector

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/logging"
	"github.com/lutefd/luc/internal/workspace"
)

type Model struct {
	width     int
	height    int
	tab       int
	tool      history.ToolResultPayload
	lastCall  history.ToolCallPayload
	logs      []logging.Entry
	workspace workspace.Info
	sessionID string
	modelName string
}

var tabs = []string{"Tool", "Logs", "Context"}

func New(workspace workspace.Info, sessionID, modelName string) Model {
	return Model{
		workspace: workspace,
		sessionID: sessionID,
		modelName: modelName,
	}
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) NextTab() {
	m.tab = (m.tab + 1) % len(tabs)
}

func (m *Model) SetLogs(entries []logging.Entry) {
	m.logs = entries
}

func (m *Model) Apply(ev history.EventEnvelope) {
	switch ev.Kind {
	case "tool.requested":
		m.lastCall = decode[history.ToolCallPayload](ev.Payload)
	case "tool.finished":
		m.tool = decode[history.ToolResultPayload](ev.Payload)
	}
}

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}

	headerParts := make([]string, 0, len(tabs))
	for i, tab := range tabs {
		style := lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("#adb5bd"))
		if i == m.tab {
			style = style.Bold(true).Foreground(lipgloss.Color("#8ecae6"))
		}
		headerParts = append(headerParts, style.Render(tab))
	}

	header := strings.Join(headerParts, " ")
	bodyWidth := max(10, m.width-2)
	bodyHeight := max(1, m.height-2)
	body := lipgloss.NewStyle().Width(bodyWidth).Height(bodyHeight).Render(m.body())

	return header + "\n" + body
}

func (m Model) body() string {
	switch tabs[m.tab] {
	case "Tool":
		return m.toolView()
	case "Logs":
		return m.logsView()
	default:
		return m.contextView()
	}
}

func (m Model) toolView() string {
	if m.lastCall.ID == "" {
		return "No tool activity yet."
	}

	args := m.lastCall.Arguments
	if args == "" {
		args = "{}"
	}
	result := m.tool.Content
	if result == "" {
		result = "Pending..."
	}

	if len(result) > 1200 {
		result = result[:1200] + "\n..."
	}

	return fmt.Sprintf("Name: %s\nID: %s\nArgs: %s\n\nResult:\n%s", m.lastCall.Name, m.lastCall.ID, args, result)
}

func (m Model) logsView() string {
	if len(m.logs) == 0 {
		return "No logs yet."
	}
	start := 0
	if len(m.logs) > 20 {
		start = len(m.logs) - 20
	}
	var lines []string
	for _, entry := range m.logs[start:] {
		lines = append(lines, fmt.Sprintf("%s [%s] %s", entry.Time.Format("15:04:05"), entry.Level, strings.TrimSpace(entry.Message)))
	}
	return strings.Join(lines, "\n")
}

func (m Model) contextView() string {
	payload := map[string]any{
		"workspace_root": m.workspace.Root,
		"project_id":     m.workspace.ProjectID,
		"session_id":     m.sessionID,
		"model":          m.modelName,
		"has_git":        m.workspace.HasGit,
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

func decode[T any](payload any) T {
	var out T
	data, _ := json.Marshal(payload)
	_ = json.Unmarshal(data, &out)
	return out
}
