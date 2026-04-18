package inspector

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/logging"
	"github.com/lutefd/luc/internal/theme"
	"github.com/lutefd/luc/internal/workspace"
)

type Model struct {
	width     int
	height    int
	tool      history.ToolResultPayload
	lastCall  history.ToolCallPayload
	logs      []logging.Entry
	workspace workspace.Info
	sessionID string
	modelName string
	theme     theme.Theme
}

func New(workspace workspace.Info, sessionID, modelName string, th theme.Theme) Model {
	return Model{
		workspace: workspace,
		sessionID: sessionID,
		modelName: modelName,
		theme:     th,
	}
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
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

func (m Model) SummaryView() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	return m.layout(false)
}

func (m Model) DetailView() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	return m.layout(true)
}

func (m Model) layout(expanded bool) string {
	sections := []string{
		m.sessionView(),
		m.toolView(expanded),
	}
	if expanded {
		sections = append(sections, m.logsView(), m.contextView())
	} else {
		sections = append(sections, m.logsPreview())
	}
	content := strings.Join(sections, "\n\n"+m.divider()+"\n\n")
	return m.theme.Sidebar.Width(max(24, m.width)).Height(max(1, m.height)).Render(content)
}

func (m Model) sessionView() string {
	project := m.workspace.Root
	if idx := strings.LastIndex(project, "/"); idx >= 0 && idx < len(project)-1 {
		project = project[idx+1:]
	}
	return strings.Join([]string{
		m.theme.SidebarTitle.Render("luc"),
		m.theme.SidebarLabel.Render("Session"),
		m.theme.SidebarValue.Render(project),
		m.theme.SidebarLabel.Render("Model"),
		m.theme.SidebarValue.Render(m.modelName),
	}, "\n")
}

func (m Model) toolView(expanded bool) string {
	if m.lastCall.ID == "" {
		return strings.Join([]string{
			m.theme.SidebarLabel.Render("Tools"),
			m.theme.SidebarValue.Render("No tool activity yet."),
		}, "\n")
	}

	args := ansi.Strip(m.lastCall.Arguments)
	if args == "" {
		args = "{}"
	}
	result := ansi.Strip(m.tool.Content)
	if result == "" {
		result = "Pending..."
	}

	limit := 220
	if expanded {
		limit = 900
	}
	result = clampString(result, limit)
	diff := ""
	if raw, ok := m.tool.Metadata["diff"].(string); ok {
		diff = clampString(ansi.Strip(raw), limit)
	}

	lines := []string{
		m.theme.SidebarLabel.Render("Last Tool"),
		m.theme.SidebarValue.Render(m.lastCall.Name),
		m.theme.SidebarLabel.Render("Args"),
		m.theme.SidebarValue.Render(clampString(args, 180)),
		m.theme.SidebarLabel.Render("Result"),
		m.theme.SidebarValue.Render(result),
	}
	if diff != "" {
		lines = append(lines, m.theme.SidebarLabel.Render("Diff"), m.theme.SidebarValue.Render(diff))
	}
	return strings.Join(lines, "\n")
}

func (m Model) logsView() string {
	if len(m.logs) == 0 {
		return strings.Join([]string{
			m.theme.SidebarLabel.Render("Logs"),
			m.theme.SidebarValue.Render("No logs yet."),
		}, "\n")
	}
	start := 0
	if len(m.logs) > 12 {
		start = len(m.logs) - 12
	}
	var lines []string
	for _, entry := range m.logs[start:] {
		lines = append(lines, fmt.Sprintf("%s [%s] %s", entry.Time.Format("15:04:05"), entry.Level, clampString(strings.TrimSpace(entry.Message), 160)))
	}
	return strings.Join([]string{
		m.theme.SidebarLabel.Render("Logs"),
		m.theme.SidebarValue.Render(strings.Join(lines, "\n")),
	}, "\n")
}

func (m Model) logsPreview() string {
	if len(m.logs) == 0 {
		return strings.Join([]string{
			m.theme.SidebarLabel.Render("Logs"),
			m.theme.SidebarValue.Render("No logs yet."),
		}, "\n")
	}

	entry := m.logs[len(m.logs)-1]
	line := fmt.Sprintf("%s [%s] %s", entry.Time.Format("15:04"), entry.Level, clampString(strings.TrimSpace(entry.Message), 48))
	return strings.Join([]string{
		m.theme.SidebarLabel.Render("Logs"),
		m.theme.SidebarValue.Render(line),
	}, "\n")
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
	return strings.Join([]string{
		m.theme.SidebarLabel.Render("Context"),
		m.theme.SidebarValue.Render(string(data)),
	}, "\n")
}

func (m Model) divider() string {
	return m.theme.SidebarSection.Render(strings.Repeat("-", max(8, m.width-4)))
}

func clampString(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	return strings.TrimSpace(s[:limit]) + "..."
}

func decode[T any](payload any) T {
	var out T
	data, _ := json.Marshal(payload)
	_ = json.Unmarshal(data, &out)
	return out
}
