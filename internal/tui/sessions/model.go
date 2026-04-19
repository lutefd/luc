package sessions

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/theme"
)

type Selected struct {
	SessionID string
}

type Model struct {
	theme    theme.Theme
	input    textinput.Model
	active   int
	width    int
	height   int
	open     bool
	current  string
	sessions []history.SessionMeta
	keys     keys
}

type keys struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Cancel key.Binding
}

func New(th theme.Theme) Model {
	in := textinput.New()
	in.Prompt = "> "
	in.Placeholder = "search sessions..."
	in.CharLimit = 0
	return Model{
		theme: th,
		input: in,
		keys: keys{
			Up:     key.NewBinding(key.WithKeys("up", "ctrl+p")),
			Down:   key.NewBinding(key.WithKeys("down", "ctrl+n")),
			Select: key.NewBinding(key.WithKeys("enter")),
			Cancel: key.NewBinding(key.WithKeys("esc", "ctrl+g")),
		},
	}
}

func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }
func (m Model) IsOpen() bool      { return m.open }

func (m *Model) Open(current string, sessions []history.SessionMeta) {
	m.open = true
	m.current = current
	m.sessions = append([]history.SessionMeta(nil), sessions...)
	m.input.Reset()
	m.input.Focus()
	m.active = 0
}

func (m *Model) Close() {
	m.open = false
	m.input.Blur()
}

func (m Model) filtered() []history.SessionMeta {
	q := strings.ToLower(strings.TrimSpace(m.input.Value()))
	if q == "" {
		return append([]history.SessionMeta(nil), m.sessions...)
	}
	var out []history.SessionMeta
	for _, sess := range m.sessions {
		if strings.Contains(strings.ToLower(sess.Title), q) ||
			strings.Contains(strings.ToLower(sess.SessionID), q) ||
			strings.Contains(strings.ToLower(sess.Model), q) {
			out = append(out, sess)
		}
	}
	return out
}

func (m *Model) Update(msg tea.KeyPressMsg) (tea.Cmd, bool, bool) {
	if !m.open {
		return nil, false, false
	}
	switch {
	case key.Matches(msg, m.keys.Cancel):
		m.Close()
		return nil, true, true
	case key.Matches(msg, m.keys.Up):
		if m.active > 0 {
			m.active--
		}
		return nil, false, true
	case key.Matches(msg, m.keys.Down):
		filtered := m.filtered()
		if m.active < len(filtered)-1 {
			m.active++
		}
		return nil, false, true
	case key.Matches(msg, m.keys.Select):
		filtered := m.filtered()
		if m.active >= 0 && m.active < len(filtered) {
			sel := Selected{SessionID: filtered[m.active].SessionID}
			m.Close()
			return func() tea.Msg { return sel }, true, true
		}
		return nil, true, true
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if filtered := m.filtered(); m.active >= len(filtered) {
		m.active = max(0, len(filtered)-1)
	}
	_ = cmd
	return nil, false, true
}

func (m Model) View() string {
	if !m.open {
		return ""
	}
	boxW := min(96, max(52, m.width*4/5))
	innerW := max(32, boxW-6)

	title := m.theme.HeaderBrand.Render("Sessions")
	ruleW := max(4, innerW-lipgloss.Width(title)-1)
	header := title + " " + m.theme.HeaderRule.Render(strings.Repeat("/", ruleW))

	prompt := m.theme.InputPrompt.Render("> ")
	inputLine := prompt + m.theme.InputPlaceholder.Render("search sessions...")
	if v := m.input.Value(); v != "" {
		inputLine = prompt + v
	}

	filtered := m.filtered()
	var rows []string
	for i, sess := range filtered {
		rows = append(rows, renderSessionRow(m.theme, sess, innerW, i == m.active, sess.SessionID == m.current))
	}
	if len(rows) == 0 {
		rows = append(rows, m.theme.Muted.Render("  no matches"))
	}

	hint := m.theme.Footer.Render("↑↓ choose  •  enter open  •  esc cancel")
	body := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		inputLine,
		"",
		strings.Join(rows, "\n"),
		"",
		hint,
	)
	surface := m.theme.PaletteSurface.Width(innerW).Render(body)
	return m.theme.PaletteFrame.Width(boxW).Render(surface)
}

func renderSessionRow(th theme.Theme, sess history.SessionMeta, width int, active, current bool) string {
	marker := "  "
	if current && !active {
		marker = th.StatusReady.Render("● ")
	} else if current {
		marker = "● "
	}

	title := strings.TrimSpace(sess.Title)
	if title == "" {
		title = sess.SessionID
	}
	left := marker + title

	rightParts := []string{}
	if sess.Model != "" {
		rightParts = append(rightParts, sess.Model)
	}
	rightParts = append(rightParts, sess.UpdatedAt.Local().Format("2006-01-02 15:04"))
	rightText := strings.Join(rightParts, "  ")
	right := th.Muted.Render(rightText)
	if active {
		right = rightText
	}

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := width - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	if active {
		return th.PaletteActive.Width(width).Render(line)
	}
	return line
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
