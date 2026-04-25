// Package commands implements a reusable command-palette overlay.
//
// Consumers register Commands through a Registry and render the palette as
// an overlay when opened. Commands are extensible: future plugins/extensions
// can append to the registry via Register().
package commands

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/lutefd/luc/internal/theme"
)

// Command is a single palette entry. Run is invoked when the user selects it;
// it returns a tea.Cmd that the host program executes.
type Command struct {
	ID          string
	Name        string
	Description string
	Category    string
	Shortcut    string
	Hint        string
	Run         func() tea.Cmd
}

// Registry is a mutable list of commands, safe to extend at startup.
type Registry struct {
	items []Command
}

func NewRegistry() *Registry { return &Registry{} }

// Register appends a command to the registry.
func (r *Registry) Register(c Command) {
	r.items = append(r.items, c)
}

// All returns a copy of registered commands.
func (r *Registry) All() []Command {
	out := make([]Command, len(r.items))
	copy(out, r.items)
	return out
}

// Model is the palette overlay state.
type Model struct {
	registry *Registry
	theme    theme.Theme
	input    textinput.Model
	active   int
	scroll   int
	width    int
	height   int
	open     bool
	keys     paletteKeys
}

type paletteKeys struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Cancel key.Binding
}

func New(registry *Registry, th theme.Theme) Model {
	in := textinput.New()
	in.Prompt = "> "
	in.Placeholder = "search commands..."
	in.CharLimit = 0

	return Model{
		registry: registry,
		theme:    th,
		input:    in,
		keys: paletteKeys{
			Up:     key.NewBinding(key.WithKeys("up", "ctrl+p")),
			Down:   key.NewBinding(key.WithKeys("down", "ctrl+n")),
			Select: key.NewBinding(key.WithKeys("enter")),
			Cancel: key.NewBinding(key.WithKeys("esc", "ctrl+g")),
		},
	}
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m Model) IsOpen() bool { return m.open }

func (m *Model) Open() {
	m.open = true
	m.input.Reset()
	m.input.Focus()
	m.active = 0
	m.scroll = 0
}

func (m *Model) listMaxRows() int {
	available := m.height - 6
	if available < 4 {
		available = 4
	}
	if available > 16 {
		available = 16
	}
	return available
}

func (m *Model) ensureVisible() {
	maxR := m.listMaxRows()
	if m.active < m.scroll {
		m.scroll = m.active
	}
	if m.active >= m.scroll+maxR {
		m.scroll = m.active - maxR + 1
	}
}

func (m *Model) Close() {
	m.open = false
	m.input.Blur()
}

// Update handles key events while the palette is open. Returns (selectedCmd, closeRequested, handled).
// If handled is false, the caller should process the key themselves.
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
		m.ensureVisible()
		return nil, false, true
	case key.Matches(msg, m.keys.Down):
		filtered := m.filtered()
		if m.active < len(filtered)-1 {
			m.active++
		}
		m.ensureVisible()
		return nil, false, true
	case key.Matches(msg, m.keys.Select):
		filtered := m.filtered()
		if m.active >= 0 && m.active < len(filtered) {
			cmd := filtered[m.active].Run
			m.Close()
			if cmd != nil {
				return cmd(), true, true
			}
		}
		return nil, true, true
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	// Clamp active index after filter changes.
	if filtered := m.filtered(); m.active >= len(filtered) {
		m.active = max(0, len(filtered)-1)
	}
	_ = cmd
	return nil, false, true
}

func (m Model) filtered() []Command {
	q := strings.ToLower(strings.TrimSpace(m.input.Value()))
	all := m.registry.All()
	if q == "" {
		return all
	}
	var out []Command
	for _, c := range all {
		if strings.Contains(strings.ToLower(c.Name), q) ||
			strings.Contains(strings.ToLower(c.Description), q) ||
			strings.Contains(strings.ToLower(c.Category), q) ||
			strings.Contains(strings.ToLower(c.ID), q) {
			out = append(out, c)
		}
	}
	return out
}

// View renders the palette as a bordered overlay string. Returns empty if closed.
func (m Model) View() string {
	if !m.open {
		return ""
	}
	boxW := min(72, max(40, m.width*2/3))
	// Inner width = outer - border(2) - padding(2*2) = boxW - 6
	innerW := max(20, boxW-6)

	title := m.theme.HeaderBrand.Render("Commands")
	titleW := lipgloss.Width(title)
	ruleW := max(4, innerW-titleW-1)
	rule := m.theme.HeaderRule.Render(strings.Repeat("/", ruleW))
	header := title + " " + rule

	prompt := m.theme.InputPrompt.Render("> ")
	var inputLine string
	if v := m.input.Value(); v == "" {
		inputLine = prompt + m.theme.InputPlaceholder.Render("search commands...")
	} else {
		inputLine = prompt + m.theme.InputText.Render(v)
	}

	filtered := m.filtered()
	var items []string
	for i, c := range filtered {
		items = append(items, renderItem(m.theme, c, innerW, i == m.active))
	}
	if len(items) == 0 {
		items = append(items, m.theme.Muted.Render("  no matches"))
	}

	maxRows := m.listMaxRows()
	scroll := m.scroll
	if scroll < 0 {
		scroll = 0
	}
	visible := items
	if scroll < len(items) {
		visible = items[scroll:]
	} else {
		visible = nil
	}
	if len(visible) > maxRows {
		visible = visible[:maxRows]
	}
	if scroll > 0 {
		visible = append([]string{m.theme.Muted.Render("  ↑ " + itoa(scroll) + " more")}, visible...)
	}
	below := len(items) - scroll - maxRows
	if below > 0 {
		visible = append(visible, m.theme.Muted.Render("  ↓ "+itoa(below)+" more"))
	}

	hint := m.theme.Footer.Render("↑↓ choose  •  enter confirm  •  esc cancel")

	body := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		inputLine,
		"",
		strings.Join(visible, "\n"),
		"",
		hint,
	)
	// Force the panel background across the full inner width; otherwise
	// short rows leave their right-padding as bare spaces that show the
	// terminal's OSC 11 bg instead of the palette's panel color.
	surface := m.theme.PaletteSurface.Width(innerW).Render(body)
	return m.theme.PaletteFrame.Width(boxW).Render(surface)
}

// renderItem renders a single palette row: name left-aligned, shortcut
// right-aligned, gap of spaces between. Uses visual width (lipgloss.Width)
// so ANSI-styled text measures correctly.
func renderItem(th theme.Theme, c Command, width int, active bool) string {
	name := " " + c.Name
	if strings.TrimSpace(c.Category) != "" {
		name = " " + c.Category + ": " + c.Name
	}
	shortcut := c.Shortcut
	if shortcut != "" {
		shortcut = shortcut + " "
	}
	nameW := lipgloss.Width(name)
	scW := lipgloss.Width(shortcut)
	gap := width - nameW - scW
	if gap < 1 {
		gap = 1
	}
	line := name + strings.Repeat(" ", gap)
	if active {
		// Render shortcut inside active highlight too.
		full := line + shortcut
		return th.PaletteActive.Width(width).Render(full)
	}
	// Inactive: shortcut rendered muted.
	return line + th.Muted.Render(shortcut)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
