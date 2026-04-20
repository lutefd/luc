package scrollbar

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/lutefd/luc/internal/theme"
)

const (
	GutterWidth = 1
	hideDelay   = 700 * time.Millisecond
)

type hideMsg struct {
	at time.Time
	id uint64
}

type State struct {
	id        uint64
	hideAt    time.Time
	scheduled bool
	visible   bool
}

func (s *State) Activate() tea.Cmd {
	now := time.Now()
	if s.id == 0 {
		nextScrollbarID++
		s.id = nextScrollbarID
	}
	s.visible = true
	s.hideAt = now.Add(hideDelay)
	if s.scheduled {
		return nil
	}
	s.scheduled = true
	return s.waitUntil(s.hideAt)
}

func (s *State) Update(msg tea.Msg) (bool, tea.Cmd) {
	hide, ok := msg.(hideMsg)
	if !ok || hide.id != s.id || !s.scheduled {
		return false, nil
	}
	if hide.at.Before(s.hideAt) {
		return false, s.waitUntil(s.hideAt)
	}
	s.scheduled = false
	if !s.visible {
		return false, nil
	}
	s.visible = false
	return true, nil
}

func (s State) Visible() bool {
	return s.visible
}

func (s State) waitUntil(deadline time.Time) tea.Cmd {
	delay := time.Until(deadline)
	if delay < 0 {
		delay = 0
	}
	return tea.Tick(delay, func(at time.Time) tea.Msg {
		return hideMsg{id: s.id, at: at}
	})
}

func Window(total, active, maxVisible int) (start, end int) {
	if total <= 0 {
		return 0, 0
	}
	if maxVisible <= 0 || total <= maxVisible {
		return 0, total
	}
	if active < 0 {
		active = 0
	}
	if active >= total {
		active = total - 1
	}
	start = active - maxVisible/2
	if start < 0 {
		start = 0
	}
	end = start + maxVisible
	if end > total {
		end = total
		start = end - maxVisible
	}
	return start, end
}

func Render(th theme.Theme, content string, width, height, total, visible, offset int, active bool) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	bodyWidth := max(1, width-GutterWidth)
	body := lipgloss.NewStyle().
		Width(bodyWidth).
		Height(height).
		MaxHeight(height).
		Render(content)

	lines := make([]string, height)
	for i := range lines {
		lines[i] = " "
	}
	if active && total > visible && visible > 0 {
		thumbHeight := max(1, height*visible/max(1, total))
		if thumbHeight > height {
			thumbHeight = height
		}
		limit := max(0, total-visible)
		posLimit := max(0, height-thumbHeight)
		pos := 0
		if limit > 0 && posLimit > 0 {
			pos = offset * posLimit / limit
		}
		for i := 0; i < thumbHeight && pos+i < len(lines); i++ {
			lines[pos+i] = th.HeaderRule.Render("│")
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, body, strings.Join(lines, "\n"))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var nextScrollbarID uint64
