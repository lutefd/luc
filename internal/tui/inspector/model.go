package inspector

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/logging"
	"github.com/lutefd/luc/internal/theme"
	"github.com/lutefd/luc/internal/workspace"
)

type Tab int

const (
	TabOverview Tab = iota
	TabTool
	TabLogs
	TabContext
	tabCount
)

var tabNames = [tabCount]string{"Overview", "Tool", "Logs", "Context"}

type Model struct {
	width          int
	height         int
	session        history.SessionMeta
	tool           history.ToolResultPayload
	lastCall       history.ToolCallPayload
	logs           []logging.Entry
	workspace      workspace.Info
	status         string
	lastUser       string
	lastAssistant  string
	userTurns      int
	assistantTurns int
	toolCalls      int
	errorCount     int
	reloadVersion  uint64
	theme          theme.Theme
	activeTab      Tab
	viewport       viewport.Model
}

func New(ws workspace.Info, session history.SessionMeta, th theme.Theme) Model {
	vp := viewport.New()
	vp.MouseWheelEnabled = false
	vp.SoftWrap = true
	return Model{
		workspace: ws,
		session:   session,
		status:    "Ready",
		theme:     th,
		activeTab: TabOverview,
		viewport:  vp,
	}
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.viewport.SetWidth(max(1, width-4))
	m.viewport.SetHeight(max(1, height-4))
	m.refreshContent()
}

func (m *Model) SetLogs(entries []logging.Entry) {
	m.logs = entries
	if m.activeTab == TabLogs {
		m.refreshContent()
	}
}

func (m *Model) SetSessionMeta(meta history.SessionMeta) {
	m.session = meta
	if m.activeTab == TabOverview || m.activeTab == TabContext {
		m.refreshContent()
	}
}

func (m *Model) SetStatus(status string) {
	status = strings.TrimSpace(status)
	if status == "" {
		status = "Ready"
	}
	if status == m.status {
		return
	}
	m.status = status
	if m.activeTab == TabOverview || m.activeTab == TabContext {
		m.refreshContent()
	}
}

func (m *Model) Apply(ev history.EventEnvelope) {
	switch ev.Kind {
	case "message.user":
		payload := decode[history.MessagePayload](ev.Payload)
		m.userTurns++
		m.lastUser = clampString(ansi.Strip(payload.Content), 180)
		m.status = "Waiting for response"
	case "message.assistant.delta":
		if strings.TrimSpace(m.status) == "" || strings.EqualFold(m.status, "Waiting for response") {
			m.status = "Responding"
		}
	case "message.assistant.final":
		payload := decode[history.MessagePayload](ev.Payload)
		m.assistantTurns++
		m.lastAssistant = clampString(ansi.Strip(payload.Content), 180)
		m.status = "Ready"
	case "message.assistant.tool_calls":
		m.status = "Running tools"
	case "tool.requested":
		payload := decode[history.ToolCallPayload](ev.Payload)
		if shouldHideTool(payload.Name) {
			return
		}
		m.lastCall = payload
		m.toolCalls++
		m.status = "Running " + m.lastCall.Name
	case "tool.finished":
		payload := decode[history.ToolResultPayload](ev.Payload)
		if shouldHideTool(payload.Name) {
			return
		}
		m.tool = payload
		if m.tool.Error != "" {
			m.errorCount++
			m.status = "Tool failed: " + m.tool.Name
		} else {
			m.status = "Tool finished: " + m.tool.Name
		}
	case "reload.finished":
		payload := decode[history.ReloadPayload](ev.Payload)
		m.reloadVersion = payload.Version
		m.status = fmt.Sprintf("Reloaded v%d", payload.Version)
	case "reload.failed":
		payload := decode[history.ReloadPayload](ev.Payload)
		m.errorCount++
		m.status = "Reload failed"
		if payload.Version > 0 {
			m.reloadVersion = payload.Version
		}
	case "status.thinking":
		payload := decode[history.StatusPayload](ev.Payload)
		if strings.TrimSpace(payload.Text) != "" {
			m.status = payload.Text
		} else {
			m.status = "Thinking..."
		}
	case "system.error":
		m.errorCount++
		m.status = "Error"
	}
	if m.activeTab == TabOverview || m.activeTab == TabTool || m.activeTab == TabContext {
		m.refreshContent()
	}
}

func (m *Model) NextTab() {
	m.activeTab = (m.activeTab + 1) % tabCount
	m.refreshContent()
}

func (m *Model) PrevTab() {
	m.activeTab = (m.activeTab - 1 + tabCount) % tabCount
	m.refreshContent()
}

func (m *Model) UpdateViewport(msg tea.Msg) {
	m.viewport, _ = m.viewport.Update(msg)
}

func (m *Model) HandleWheel(msg tea.MouseWheelMsg) {
	switch msg.Button {
	case tea.MouseWheelUp:
		m.viewport.ScrollUp(1)
	case tea.MouseWheelDown:
		m.viewport.ScrollDown(1)
	}
}

func (m Model) SummaryView() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	content := m.overviewView()
	return m.theme.Sidebar.Width(max(24, m.width)).Height(max(1, m.height)).Render(content)
}

func (m Model) DetailView() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	tabBar := m.renderTabBar()
	body := m.viewport.View()
	inner := lipgloss.JoinVertical(lipgloss.Left, tabBar, "", body)
	return m.theme.Sidebar.Width(max(24, m.width)).Height(max(1, m.height)).Render(inner)
}

func (m *Model) refreshContent() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	var content string
	switch m.activeTab {
	case TabOverview:
		content = m.overviewView()
	case TabTool:
		content = m.toolView(true)
	case TabLogs:
		content = m.logsView()
	case TabContext:
		content = m.contextView()
	}
	m.viewport.SetContent(content)
}

func (m Model) renderTabBar() string {
	var tabs []string
	for i := Tab(0); i < tabCount; i++ {
		name := tabNames[i]
		if i == m.activeTab {
			tabs = append(tabs, m.theme.SidebarTabActive.Render(name))
		} else {
			tabs = append(tabs, m.theme.SidebarTab.Render(name))
		}
	}
	bar := strings.Join(tabs, m.theme.SidebarSection.Render(" │ "))
	sep := m.theme.SidebarSection.Render(strings.Repeat("─", max(8, m.width-6)))
	return lipgloss.JoinVertical(lipgloss.Left, bar, sep)
}

func (m Model) sessionView() string {
	return m.overviewView()
}

func (m Model) overviewView() string {
	project := m.workspace.Root
	if idx := strings.LastIndex(project, "/"); idx >= 0 && idx < len(project)-1 {
		project = project[idx+1:]
	}
	title := strings.TrimSpace(m.session.Title)
	if title == "" {
		title = project
	}
	provider := strings.TrimSpace(m.session.Provider)
	if provider == "" {
		provider = "unknown"
	}
	model := strings.TrimSpace(m.session.Model)
	if model == "" {
		model = "unknown"
	}
	status := strings.TrimSpace(m.status)
	if status == "" {
		status = "Ready"
	}
	activity := fmt.Sprintf("%d user  •  %d assistant  •  %d tools", m.userTurns, m.assistantTurns, m.toolCalls)
	workspaceSummary := project
	if m.workspace.HasGit {
		workspaceSummary += "  •  git"
		if branch := strings.TrimSpace(m.workspace.Branch); branch != "" {
			workspaceSummary += "  •  " + branch
		}
	} else {
		workspaceSummary += "  •  no git"
	}

	lines := []string{
		m.theme.SidebarTitle.Render("luc"),
		m.theme.SidebarValue.Render(title),
		"",
		m.theme.SidebarLabel.Render("Status"),
		m.renderStatus(status),
		m.theme.SidebarLabel.Render("Provider"),
		m.theme.SidebarValue.Render(provider),
		m.theme.SidebarLabel.Render("Model"),
		m.theme.SidebarValue.Render(model),
		m.theme.SidebarLabel.Render("Activity"),
		m.theme.SidebarValue.Render(activity),
		m.theme.SidebarLabel.Render("Workspace"),
		m.theme.SidebarValue.Render(workspaceSummary),
	}

	if m.reloadVersion > 0 {
		lines = append(lines,
			m.theme.SidebarLabel.Render("Runtime"),
			m.theme.SidebarValue.Render(fmt.Sprintf("v%d", m.reloadVersion)),
		)
	}
	if !m.session.CreatedAt.IsZero() {
		lines = append(lines,
			m.theme.SidebarLabel.Render("Created"),
			m.theme.SidebarValue.Render(formatTimestamp(m.session.CreatedAt)),
		)
	}
	if !m.session.UpdatedAt.IsZero() {
		lines = append(lines,
			m.theme.SidebarLabel.Render("Updated"),
			m.theme.SidebarValue.Render(formatTimestamp(m.session.UpdatedAt)),
		)
	}
	if summary := m.lastToolSummary(); summary != "" {
		lines = append(lines,
			m.theme.SidebarLabel.Render("Last Tool"),
			m.theme.SidebarValue.Render(summary),
		)
	}
	if m.errorCount > 0 {
		lines = append(lines,
			m.theme.SidebarLabel.Render("Errors"),
			m.theme.SidebarValue.Render(itoa(m.errorCount)),
		)
	}
	return strings.Join(lines, "\n")
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

	limit := 900
	if !expanded {
		limit = 220
	}
	result = clampString(result, limit)
	diff := ""
	if raw, ok := m.tool.Metadata["diff"].(string); ok {
		diff = clampString(ansi.Strip(raw), limit)
	}

	lines := []string{
		m.theme.SidebarLabel.Render("Last Tool"),
		m.theme.SidebarValue.Render(m.lastCall.Name),
	}
	if command, ok := m.tool.Metadata["command"].(string); ok && strings.TrimSpace(command) != "" {
		lines = append(lines,
			m.theme.SidebarLabel.Render("Command"),
			m.theme.SidebarValue.Render(clampString(ansi.Strip(command), 220)),
		)
	}
	lines = append(lines,
		m.theme.SidebarLabel.Render("Args"),
		m.theme.SidebarValue.Render(clampString(args, 180)),
		m.theme.SidebarLabel.Render("Result"),
		m.theme.SidebarValue.Render(result),
	)
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
	var lines []string
	for _, entry := range m.logs {
		lines = append(lines, fmt.Sprintf("%s [%s] %s", entry.Time.Format("15:04:05"), entry.Level, clampString(strings.TrimSpace(entry.Message), 160)))
	}
	return strings.Join([]string{
		m.theme.SidebarLabel.Render("Logs"),
		m.theme.SidebarValue.Render(strings.Join(lines, "\n")),
	}, "\n")
}

func (m Model) contextView() string {
	payload := map[string]any{
		"workspace_root":  m.workspace.Root,
		"project_id":      m.workspace.ProjectID,
		"session_id":      m.session.SessionID,
		"title":           m.session.Title,
		"provider":        m.session.Provider,
		"model":           m.session.Model,
		"status":          m.status,
		"user_turns":      m.userTurns,
		"assistant_turns": m.assistantTurns,
		"tool_calls":      m.toolCalls,
		"errors":          m.errorCount,
		"reload_version":  m.reloadVersion,
		"has_git":         m.workspace.HasGit,
		"branch":          m.workspace.Branch,
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return strings.Join([]string{
		m.theme.SidebarLabel.Render("Context"),
		m.theme.SidebarValue.Render(string(data)),
	}, "\n")
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

func (m Model) lastToolSummary() string {
	if strings.TrimSpace(m.lastCall.Name) == "" && strings.TrimSpace(m.tool.Name) == "" {
		return ""
	}
	name := strings.TrimSpace(m.tool.Name)
	if name == "" {
		name = strings.TrimSpace(m.lastCall.Name)
	}
	status := "pending"
	switch {
	case strings.TrimSpace(m.tool.Error) != "":
		status = "error"
	case strings.TrimSpace(m.tool.Name) != "":
		status = "done"
	}
	summary := name + "  •  " + status
	if path, ok := m.tool.Metadata["path"].(string); ok && strings.TrimSpace(path) != "" {
		summary += "\n" + clampString(path, 120)
	}
	return summary
}

func formatTimestamp(ts time.Time) string {
	return ts.Local().Format("2006-01-02 15:04")
}

func (m Model) renderStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		status = "Ready"
	}

	style := m.theme.StatusReady
	lower := strings.ToLower(status)
	switch {
	case strings.Contains(lower, "error"), strings.Contains(lower, "fail"):
		style = m.theme.StatusError
	case strings.Contains(lower, "send"),
		strings.Contains(lower, "reload"),
		strings.Contains(lower, "think"),
		strings.Contains(lower, "run"),
		strings.Contains(lower, "wait"),
		strings.Contains(lower, "respond"):
		style = m.theme.StatusBusy
	}

	return lipgloss.NewStyle().
		Inherit(style).
		Bold(true).
		Render("● " + status)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func shouldHideTool(name string) bool {
	return strings.TrimSpace(name) == "list_tools"
}
