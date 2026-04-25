package tui

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
)

type agentStatusTickMsg struct{}

type agentStatusState struct {
	active  bool
	started time.Time
	text    string
	index   int
}

const agentStatusTickEvery = time.Second

func agentStatusTickCmd() tea.Cmd {
	return tea.Tick(agentStatusTickEvery, func(time.Time) tea.Msg {
		return agentStatusTickMsg{}
	})
}

func (m *Model) startAgentStatus(seed string) tea.Cmd {
	statuses := m.agentStatuses()
	if len(statuses) == 0 {
		return nil
	}
	idx := statusIndex(seed, len(statuses))
	m.agentStatus = agentStatusState{
		active:  true,
		started: time.Now(),
		text:    statuses[idx],
		index:   idx,
	}
	m.transcript.SetEphemeralStatus(m.renderAgentStatusBlock())
	m.invalidateBody()
	return agentStatusTickCmd()
}

func (m *Model) stopAgentStatus() {
	if !m.agentStatus.active {
		return
	}
	m.agentStatus = agentStatusState{}
	m.transcript.SetEphemeralStatus("")
	m.invalidateBody()
}

func (m *Model) updateAgentStatusTick() tea.Cmd {
	if !m.agentStatus.active {
		return nil
	}
	statuses := m.agentStatuses()
	if len(statuses) > 0 && m.agentStatus.started.Add(time.Duration(m.agentStatus.index+1)*8*time.Second).Before(time.Now()) {
		m.agentStatus.index = (m.agentStatus.index + 1) % len(statuses)
		m.agentStatus.text = statuses[m.agentStatus.index]
	}
	m.transcript.SetEphemeralStatus(m.renderAgentStatusBlock())
	m.invalidateBody()
	return agentStatusTickCmd()
}

func (m Model) renderAgentStatusBlock() string {
	if !m.agentStatus.active {
		return ""
	}
	elapsed := max(0, int(time.Since(m.agentStatus.started).Round(time.Second)/time.Second))
	status := strings.TrimSpace(m.agentStatus.text)
	if status == "" {
		status = "Thinking..."
	}
	line := lipgloss.JoinHorizontal(
		lipgloss.Left,
		m.theme.StatusBusy.Render("· "+status),
		" ",
		m.theme.Muted.Render(fmt.Sprintf("(%ds)", elapsed)),
	)
	return lipgloss.NewStyle().Width(max(20, m.transcriptWidth()-4)).Render(line)
}

func (m Model) agentStatuses() []string {
	configured := m.controller.Config().UI.AgentStatuses
	statuses := make([]string, 0, len(configured))
	for _, status := range configured {
		if trimmed := strings.TrimSpace(status); trimmed != "" {
			statuses = append(statuses, trimmed)
		}
	}
	return statuses
}

func statusIndex(seed string, n int) int {
	if n <= 1 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return int(h.Sum32() % uint32(n))
}
