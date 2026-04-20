// Package themes implements the theme-selection modal. It reads available
// themes from the extensions runtime (workspace + user `themes/` dirs) and
// always exposes the built-in light/dark variants so users can return to a
// default even if no custom themes are installed.
package themes

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/lutefd/luc/internal/theme"
)

// Selected is emitted when the user confirms a theme pick. ThemeName is the
// identifier to pass to theme.Load (empty string means "restore default").
type Selected struct {
	ThemeName string
}

// Entry represents a single theme available in the picker.
type Entry struct {
	// Name is the identifier passed back to the controller (empty string means
	// "restore default").
	Name string
	// Display is what the user sees. Useful to decorate built-ins with a tag.
	Display string
	// BuiltIn marks the entry as a compiled-in variant (light/dark) so the
	// picker can render it distinctly from user-defined themes.
	BuiltIn bool
}

type Model struct {
	theme   theme.Theme
	input   textinput.Model
	entries []Entry
	active  int
	width   int
	height  int
	open    bool
	current string
	keys    keys
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
	in.Placeholder = "filter themes..."
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

// Open shows the modal with the supplied entries. currentName is highlighted
// with a ● marker so users can see where they are.
func (m *Model) Open(entries []Entry, currentName string) {
	m.entries = entries
	m.current = currentName
	m.open = true
	m.input.Reset()
	m.input.Focus()
	m.active = 0
}

func (m *Model) Close() {
	m.open = false
	m.input.Blur()
}

func (m Model) filtered() []Entry {
	q := strings.ToLower(strings.TrimSpace(m.input.Value()))
	if q == "" {
		out := make([]Entry, len(m.entries))
		copy(out, m.entries)
		return out
	}
	var out []Entry
	for _, e := range m.entries {
		if strings.Contains(strings.ToLower(e.Name), q) ||
			strings.Contains(strings.ToLower(e.Display), q) {
			out = append(out, e)
		}
	}
	return out
}

// Update handles key events while the modal is open. Returns (selectedMsg, closed, handled).
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
		if m.active < len(m.filtered())-1 {
			m.active++
		}
		return nil, false, true
	case key.Matches(msg, m.keys.Select):
		rows := m.filtered()
		if m.active >= 0 && m.active < len(rows) {
			sel := Selected{ThemeName: rows[m.active].Name}
			m.Close()
			return func() tea.Msg { return sel }, true, true
		}
		return nil, true, true
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if rows := m.filtered(); m.active >= len(rows) {
		if len(rows) > 0 {
			m.active = len(rows) - 1
		} else {
			m.active = 0
		}
	}
	_ = cmd
	return nil, false, true
}

func (m Model) View() string {
	if !m.open {
		return ""
	}
	boxW := min(72, max(44, m.width*2/3))
	innerW := max(24, boxW-6)

	title := m.theme.HeaderBrand.Render("Select theme")
	ruleW := max(4, innerW-lipgloss.Width(title)-1)
	header := title + " " + m.theme.HeaderRule.Render(strings.Repeat("/", ruleW))

	prompt := m.theme.InputPrompt.Render("> ")
	var inputLine string
	if v := m.input.Value(); v == "" {
		inputLine = prompt + m.theme.InputPlaceholder.Render("filter themes...")
	} else {
		inputLine = prompt + m.theme.InputText.Render(v)
	}

	rows := m.filtered()
	var rendered []string
	for i, e := range rows {
		rendered = append(rendered, renderEntry(m.theme, e, innerW, i == m.active, e.Name == m.current))
	}
	if len(rendered) == 0 {
		rendered = append(rendered, m.theme.Muted.Render("  no matches"))
	}

	hint := m.theme.Footer.Render("↑↓ choose  •  enter select  •  esc cancel")
	body := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		inputLine,
		"",
		strings.Join(rendered, "\n"),
		"",
		hint,
	)
	surface := m.theme.PaletteSurface.Width(innerW).Render(body)
	return m.theme.PaletteFrame.Width(boxW).Render(surface)
}

func renderEntry(th theme.Theme, e Entry, width int, active, isCurrent bool) string {
	marker := "  "
	if isCurrent {
		marker = th.StatusReady.Render("● ")
	}

	label := e.Display
	if label == "" {
		label = e.Name
	}
	if label == "" {
		label = "(default)"
	}

	tag := ""
	if e.BuiltIn {
		tag = th.Muted.Render("built-in")
	}

	leftW := lipgloss.Width(marker) + lipgloss.Width(label)
	rightW := lipgloss.Width(tag)
	gap := width - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	line := marker + label + strings.Repeat(" ", gap) + tag

	if active {
		return th.PaletteActive.Width(width).Render(line)
	}
	return line
}
