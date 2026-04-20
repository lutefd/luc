// Package models implements the model-selection modal. It reads providers
// and their models from provider.Registry so extensions that register extra
// providers show up automatically.
package models

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/lutefd/luc/internal/provider"
	"github.com/lutefd/luc/internal/theme"
)

// Selected is emitted when the user confirms a model pick.
type Selected struct {
	ProviderID string
	ModelID    string
}

type Model struct {
	registry *provider.Registry
	theme    theme.Theme
	input    textinput.Model
	active   int
	scroll   int
	width    int
	height   int
	open     bool
	current  string // currently-active model ID (rendered with a ● marker)
	keys     keys
}

type keys struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Cancel key.Binding
}

func New(registry *provider.Registry, th theme.Theme) Model {
	in := textinput.New()
	in.Prompt = "> "
	in.Placeholder = "filter models..."
	in.CharLimit = 0
	return Model{
		registry: registry,
		theme:    th,
		input:    in,
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

func (m *Model) Open(currentProviderID, currentModelID string) {
	m.open = true
	m.current = currentProviderID + ":" + currentModelID
	m.input.Reset()
	m.input.Focus()
	m.active = 0
	m.scroll = 0
}

func (m *Model) Close() {
	m.open = false
	m.input.Blur()
}

type row struct {
	provider provider.ProviderDef
	model    provider.ModelDef
	isHeader bool
}

// filtered returns a flat list grouped by provider: a header row for each
// provider followed by its matching model rows.
func (m Model) filtered() []row {
	q := strings.ToLower(strings.TrimSpace(m.input.Value()))
	var out []row
	for _, p := range m.registry.Providers() {
		var matches []provider.ModelDef
		for _, md := range p.Models {
			if q == "" ||
				strings.Contains(strings.ToLower(md.ID), q) ||
				strings.Contains(strings.ToLower(md.Name), q) ||
				strings.Contains(strings.ToLower(md.Description), q) {
				matches = append(matches, md)
			}
		}
		if len(matches) == 0 {
			continue
		}
		out = append(out, row{provider: p, isHeader: true})
		for _, md := range matches {
			out = append(out, row{provider: p, model: md})
		}
	}
	return out
}

// selectableIndex maps the visible `active` cursor to the underlying row,
// skipping header rows.
func selectableAt(rows []row, cursor int) (row, int, bool) {
	idx := 0
	for _, r := range rows {
		if r.isHeader {
			continue
		}
		if idx == cursor {
			return r, idx, true
		}
		idx++
	}
	return row{}, 0, false
}

func selectableCount(rows []row) int {
	n := 0
	for _, r := range rows {
		if !r.isHeader {
			n++
		}
	}
	return n
}

func countSelectableInRendered(rows []row, fromRendered, toRendered int) int {
	idx := 0
	n := 0
	for _, r := range rows {
		if r.isHeader {
			idx += 2
			continue
		}
		if idx >= fromRendered && idx < toRendered {
			n++
		}
		idx++
	}
	return n
}

func (m *Model) listMaxRows() int {
	// Reserve lines for: header, blank, input, blank, blank, hint = 6
	available := m.height - 6
	if available < 4 {
		available = 4
	}
	if available > 20 {
		available = 20
	}
	return available
}

// renderedIndex returns the rendered[] index for the active selectable row,
// plus the rendered[] index of its provider header if this is the first model
// in the group (so ensureVisible can include the header when scrolling up).
func renderedIndex(rows []row, active int) (modelIdx, headerIdx int) {
	idx := 0
	selIdx := 0
	lastHeaderIdx := -1
	firstInGroup := false
	for _, r := range rows {
		if r.isHeader {
			lastHeaderIdx = idx
			firstInGroup = true
			idx += 2
			continue
		}
		if selIdx == active {
			h := -1
			if firstInGroup {
				h = lastHeaderIdx
			}
			return idx, h
		}
		firstInGroup = false
		selIdx++
		idx++
	}
	return idx, -1
}

func (m *Model) ensureVisible(rows []row) {
	maxR := m.listMaxRows()
	target, headerIdx := renderedIndex(rows, m.active)
	if target < m.scroll {
		scrollTo := target
		if headerIdx >= 0 {
			scrollTo = headerIdx
		}
		m.scroll = scrollTo
	}
	if target >= m.scroll+maxR {
		m.scroll = target - maxR + 1
	}
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
		m.ensureVisible(m.filtered())
		return nil, false, true
	case key.Matches(msg, m.keys.Down):
		rows := m.filtered()
		if m.active < selectableCount(rows)-1 {
			m.active++
		}
		m.ensureVisible(rows)
		return nil, false, true
	case key.Matches(msg, m.keys.Select):
		rows := m.filtered()
		if r, _, ok := selectableAt(rows, m.active); ok {
			sel := Selected{ProviderID: r.provider.ID, ModelID: r.model.ID}
			m.Close()
			return func() tea.Msg { return sel }, true, true
		}
		return nil, true, true
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if cnt := selectableCount(m.filtered()); m.active >= cnt {
		if cnt > 0 {
			m.active = cnt - 1
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
	boxW := min(84, max(48, m.width*3/4))
	innerW := max(28, boxW-6)

	title := m.theme.HeaderBrand.Render("Select model")
	ruleW := max(4, innerW-lipgloss.Width(title)-1)
	header := title + " " + m.theme.HeaderRule.Render(strings.Repeat("/", ruleW))

	prompt := m.theme.InputPrompt.Render("> ")
	var inputLine string
	if v := m.input.Value(); v == "" {
		inputLine = prompt + m.theme.InputPlaceholder.Render("filter models...")
	} else {
		inputLine = prompt + m.theme.InputText.Render(v)
	}

	rows := m.filtered()
	var rendered []string
	cursorIdx := 0
	for _, r := range rows {
		if r.isHeader {
			rendered = append(rendered, "")
			rendered = append(rendered, m.theme.SidebarTitle.Render(r.provider.Name))
			continue
		}
		rendered = append(rendered, renderModelRow(m.theme, r.model, innerW, cursorIdx == m.active, r.provider.ID+":"+r.model.ID == m.current))
		cursorIdx++
	}
	if len(rendered) == 0 {
		rendered = append(rendered, m.theme.Muted.Render("  no matches"))
	}

	// Apply scroll window.
	maxRows := m.listMaxRows()
	scroll := m.scroll
	if scroll < 0 {
		scroll = 0
	}
	visible := rendered
	if scroll < len(rendered) {
		visible = rendered[scroll:]
	} else {
		visible = nil
	}
	if len(visible) > maxRows {
		visible = visible[:maxRows]
	}

	// Count only selectable model rows (not headers/blanks) for the indicators.
	aboveModels := countSelectableInRendered(rows, 0, scroll)
	belowStart := scroll + maxRows
	belowModels := countSelectableInRendered(rows, belowStart, len(rendered))

	if aboveModels > 0 {
		visible = append([]string{m.theme.Muted.Render("  ↑ " + itoa(aboveModels) + " more")}, visible...)
	}
	if belowModels > 0 {
		visible = append(visible, m.theme.Muted.Render("  ↓ "+itoa(belowModels)+" more"))
	}

	hint := m.theme.Footer.Render("↑↓ choose  •  enter select  •  esc cancel")
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
	surface := m.theme.PaletteSurface.Width(innerW).Render(body)
	return m.theme.PaletteFrame.Width(boxW).Render(surface)
}

// renderModelRow renders a model row: "  ● model-id    Display name — desc    128k"
// Active selection gets the highlight style; current (running) model gets a
// filled dot on the left.
func renderModelRow(th theme.Theme, md provider.ModelDef, width int, active, isCurrent bool) string {
	marker := "  "
	if isCurrent {
		marker = th.StatusReady.Render("● ")
	}

	// Left: ID + name + reasoning tag
	left := md.ID
	if md.Reasoning {
		left = md.ID + " " + th.Muted.Render("(reasoning)")
	}

	// Right: context window badge
	right := ""
	if md.ContextK > 0 {
		if md.ContextK >= 1000 {
			right = th.Muted.Render("1M ctx")
		} else {
			right = th.Muted.Render(itoa(md.ContextK) + "k ctx")
		}
	}

	leftW := lipgloss.Width(marker) + lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := width - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	line := marker + left + strings.Repeat(" ", gap) + right

	if active {
		return th.PaletteActive.Width(width).Render(line)
	}
	return line
}

// itoa: small helper to avoid strconv import for a single call site.
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
