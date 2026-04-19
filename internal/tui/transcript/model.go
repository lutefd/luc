package transcript

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/media"
	"github.com/lutefd/luc/internal/theme"
	"github.com/lutefd/luc/internal/tools"
)

type Block struct {
	ID          string
	Kind        string
	Content     string
	Diff        string
	State       string
	Attachments []media.Attachment
	Meta        map[string]string
}

type blockSpan struct {
	start int
	end   int
}

const (
	diffCollapseLineThreshold = 160
	diffCollapseByteThreshold = 12000
)

type Model struct {
	viewport   viewport.Model
	width      int
	height     int
	blocks     []Block
	spans      []blockSpan
	cache      map[string]string
	expanded   map[string]bool
	autoFollow bool
	selAnchor  int
	selFocus   int
	selecting  bool
	theme      theme.Theme
	renderer   RenderFunc
}

type RenderFunc func(width int, text string) (string, error)

func New(th theme.Theme, variant string) Model {
	vp := viewport.New()
	vp.MouseWheelEnabled = false
	vp.SoftWrap = true
	return Model{
		viewport:   vp,
		cache:      make(map[string]string),
		expanded:   make(map[string]bool),
		autoFollow: true,
		selAnchor:  -1,
		selFocus:   -1,
		theme:      th,
		renderer: func(width int, text string) (string, error) {
			renderer, err := theme.NewMarkdownRenderer(width, variant)
			if err != nil {
				return "", err
			}
			return renderer.Render(text)
		},
	}
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.viewport.SetWidth(width)
	m.viewport.SetHeight(height)
	m.render()
}

func (m *Model) UpdateViewport(msg any) {
	m.updateAutoFollow(msg)
	m.viewport, _ = m.viewport.Update(msg)
	if m.viewport.AtBottom() {
		m.autoFollow = true
	}
}

func (m *Model) HandleWheel(msg tea.MouseWheelMsg) {
	m.updateAutoFollow(msg)
	switch msg.Button {
	case tea.MouseWheelUp:
		m.viewport.ScrollUp(1)
	case tea.MouseWheelDown:
		m.viewport.ScrollDown(1)
	}
	if m.viewport.AtBottom() {
		m.autoFollow = true
	}
}

func (m *Model) BeginSelection(row int) {
	idx, ok := m.blockIndexAtRow(row)
	if !ok {
		m.ClearSelection()
		return
	}
	m.selAnchor = idx
	m.selFocus = idx
	m.selecting = true
	m.render()
}

func (m *Model) ExtendSelection(row int) {
	if !m.selecting {
		return
	}
	idx, ok := m.blockIndexAtRow(row)
	if !ok || idx == m.selFocus {
		return
	}
	m.selFocus = idx
	m.render()
}

func (m *Model) EndSelection() {
	m.selecting = false
}

func (m *Model) ClearSelection() {
	if m.selAnchor < 0 && m.selFocus < 0 && !m.selecting {
		return
	}
	m.selAnchor = -1
	m.selFocus = -1
	m.selecting = false
	m.render()
}

func (m Model) HasSelection() bool {
	_, _, ok := m.selectionRange()
	return ok
}

func (m Model) SelectedText() string {
	start, end, ok := m.selectionRange()
	if !ok {
		return ""
	}
	parts := make([]string, 0, end-start+1)
	for i := start; i <= end; i++ {
		text := strings.TrimSpace(m.plainTextForBlock(m.blocks[i]))
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func (m Model) BlockIDAtRow(row int) (string, bool) {
	idx, ok := m.blockIndexAtRow(row)
	if !ok || idx < 0 || idx >= len(m.blocks) {
		return "", false
	}
	return m.blocks[idx].ID, true
}

// ToggleBlockExpansionAtRow flips the expanded state of a tool or error
// block at the given viewport row. Returns true if the toggle was applied.
func (m *Model) ToggleBlockExpansionAtRow(row int) bool {
	idx, ok := m.blockIndexAtRow(row)
	if !ok || idx < 0 || idx >= len(m.blocks) {
		return false
	}
	block := m.blocks[idx]
	if !m.isExpandableBlock(block) {
		return false
	}
	m.expanded[block.ID] = !m.expanded[block.ID]
	m.render()
	return true
}

// ToggleToolExpansionAtRow is kept as an alias for ToggleBlockExpansionAtRow
// so existing tests and callers keep compiling. New code should use the
// generalized name.
func (m *Model) ToggleToolExpansionAtRow(row int) bool {
	return m.ToggleBlockExpansionAtRow(row)
}

func (m *Model) Apply(ev history.EventEnvelope) {
	switch ev.Kind {
	case "message.user":
		payload := decode[history.MessagePayload](ev.Payload)
		m.blocks = append(m.blocks, Block{
			ID:          payload.ID,
			Kind:        "user",
			Content:     cleanText(payload.Content),
			State:       "done",
			Attachments: media.FromHistoryPayloads(payload.Attachments),
		})
	case "message.assistant.delta":
		payload := decode[history.MessageDeltaPayload](ev.Payload)
		block := m.findOrAdd(payload.ID, "assistant")
		block.Content += cleanText(payload.Delta)
		block.State = "streaming"
	case "message.assistant.final":
		payload := decode[history.MessagePayload](ev.Payload)
		block := m.findOrAdd(payload.ID, "assistant")
		block.Content = cleanText(payload.Content)
		block.State = "done"
	case "tool.requested":
		payload := decode[history.ToolCallPayload](ev.Payload)
		if shouldHideTool(payload.Name) {
			return
		}
		m.blocks = append(m.blocks, Block{
			ID:      payload.ID,
			Kind:    "tool",
			Content: cleanText(fmt.Sprintf("%s %s", payload.Name, payload.Arguments)),
			State:   "pending",
			Meta:    map[string]string{"name": payload.Name},
		})
	case "tool.finished":
		payload := decode[history.ToolResultPayload](ev.Payload)
		if shouldHideTool(payload.Name) {
			return
		}
		block := m.findOrAdd(payload.ID, "tool")
		block.State = "done"
		if payload.Error != "" {
			block.State = "error"
		}
		block.Content = cleanText(payload.Content)
		if diff, ok := payload.Metadata["diff"].(string); ok && diff != "" {
			block.Diff = cleanText(diff)
		}
		if block.Meta == nil {
			block.Meta = map[string]string{}
		}
		block.Meta["name"] = payload.Name
		if path, ok := payload.Metadata["path"].(string); ok {
			block.Meta["path"] = path
		}
		if command, ok := payload.Metadata["command"].(string); ok {
			block.Meta["command"] = cleanText(command)
		}
		if timeout, ok := payload.Metadata["timeout"].(string); ok {
			block.Meta["timeout"] = cleanText(timeout)
		}
		if timedOut, ok := payload.Metadata["timed_out"].(bool); ok && timedOut {
			block.Meta["timed_out"] = "true"
		}
		if collapsed, ok := payload.Metadata[tools.MetadataUIDefaultCollapsed].(bool); ok && collapsed {
			block.Meta[tools.MetadataUIDefaultCollapsed] = "true"
		}
		if summary, ok := payload.Metadata[tools.MetadataUICollapsedSummary].(string); ok {
			block.Meta[tools.MetadataUICollapsedSummary] = cleanText(summary)
		}
		if hidden, ok := payload.Metadata[tools.MetadataUIHideContent].(bool); ok && hidden {
			block.Meta[tools.MetadataUIHideContent] = "true"
		}
		if label, ok := payload.Metadata[tools.MetadataUILabel].(string); ok {
			block.Meta[tools.MetadataUILabel] = cleanText(label)
		}
	case "system.note", "system.error":
		payload := decode[history.MessagePayload](ev.Payload)
		kind := "note"
		if ev.Kind == "system.error" {
			kind = "error"
		}
		m.blocks = append(m.blocks, Block{ID: payload.ID, Kind: kind, Content: cleanText(payload.Content), State: "done"})
	case "reload.finished":
		payload := decode[history.ReloadPayload](ev.Payload)
		m.blocks = append(m.blocks, Block{
			ID:      fmt.Sprintf("reload_%d", payload.Version),
			Kind:    "note",
			Content: fmt.Sprintf("reloaded runtime to version %d", payload.Version),
			State:   "done",
		})
	case "reload.failed":
		payload := decode[history.ReloadPayload](ev.Payload)
		m.blocks = append(m.blocks, Block{
			ID:      fmt.Sprintf("reload_failed_%d", payload.Version),
			Kind:    "error",
			Content: fmt.Sprintf("reload failed: %s", payload.Error),
			State:   "done",
		})
	}
	m.render()
}

func (m Model) View() string {
	return m.viewport.View()
}

func (m *Model) findOrAdd(id, kind string) *Block {
	for i := range m.blocks {
		if m.blocks[i].ID == id {
			return &m.blocks[i]
		}
	}
	m.blocks = append(m.blocks, Block{ID: id, Kind: kind})
	return &m.blocks[len(m.blocks)-1]
}

func (m *Model) render() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

	var views []string
	m.spans = m.spans[:0]
	line := 0
	for i, block := range m.blocks {
		key := fmt.Sprintf(
			"%s:%s:%s:%s:%s:%t:%d",
			block.Kind,
			block.State,
			block.Content,
			block.Diff,
			attachmentsCacheKey(block.Attachments),
			m.isExpanded(block.ID),
			m.width,
		)
		if block.State == "done" {
			if cached, ok := m.cache[key]; ok {
				rendered := cached
				if m.isSelectedBlock(i) {
					rendered = m.decorateSelected(rendered)
				}
				views = append(views, rendered)
				height := lipgloss.Height(rendered)
				m.spans = append(m.spans, blockSpan{start: line, end: line + max(0, height-1)})
				line += height + 2
				continue
			}
		}

		rendered := m.safeRenderBlock(block)
		baseRendered := rendered
		if m.isSelectedBlock(i) {
			rendered = m.decorateSelected(rendered)
		}
		if block.State == "done" {
			m.cache[key] = baseRendered
		}
		views = append(views, rendered)
		height := lipgloss.Height(rendered)
		m.spans = append(m.spans, blockSpan{start: line, end: line + max(0, height-1)})
		line += height + 2
	}

	m.viewport.SetContent(strings.Join(views, "\n\n"))
	if m.autoFollow {
		m.viewport.GotoBottom()
	}
}

func (m *Model) updateAutoFollow(msg any) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.Code {
		case tea.KeyPgUp, tea.KeyUp:
			m.autoFollow = false
		case tea.KeyPgDown, tea.KeyDown, tea.KeyEnd:
			// Re-enable once the viewport reaches the bottom after the update.
		case tea.KeyHome:
			m.autoFollow = false
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.autoFollow = false
		case tea.MouseWheelDown:
			// Re-enable once the viewport reaches the bottom after the update.
		}
	}
}

func (m Model) safeRenderBlock(block Block) (rendered string) {
	defer func() {
		if r := recover(); r != nil {
			rendered = lipgloss.NewStyle().Width(max(20, m.width-4)).Render(cleanText(block.Content))
		}
	}()
	return m.renderBlock(block)
}

func (m Model) decorateSelected(rendered string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color("#1a7f37")).
		PaddingLeft(1).
		Render(rendered)
}

func (m Model) renderBlock(block Block) string {
	width := max(20, m.width-4)
	switch block.Kind {
	case "user":
		// Crush-style: per-line left prefix (cyan bar) + content, label above.
		label := m.theme.UserLabel.Render("You")
		body := prefixLines(block.Content, width-2, m.theme.UserPrefix.Render("▎"))
		parts := []string{label}
		if strings.TrimSpace(block.Content) != "" {
			parts = append(parts, body)
		}
		if images := m.renderUserAttachments(block, width); images != "" {
			parts = append(parts, images)
		}
		return lipgloss.JoinVertical(lipgloss.Left, parts...)
	case "assistant":
		content := block.Content
		if block.State == "done" {
			if rendered, err := m.renderer(width-2, content); err == nil {
				if strings.TrimSpace(rendered) != "" {
					content = rendered
				}
			}
		}
		label := m.theme.AssistantLabel.Render("◆ Luc")
		body := prefixLines(content, width-2, m.theme.AssistantPrefix.Render("▎"))
		return lipgloss.JoinVertical(lipgloss.Left, label, body)
	case "tool":
		return m.renderToolBlock(block, width)
	case "error":
		return m.renderErrorBlock(block, width)
	default:
		return lipgloss.NewStyle().Width(width).Render(m.theme.Muted.Render(block.Content))
	}
}

func (m Model) renderToolBlock(block Block, width int) string {
	title := block.Meta["name"]
	if title == "" {
		title = "tool"
	}
	if label := strings.TrimSpace(block.Meta[tools.MetadataUILabel]); label != "" {
		title = label
	}
	path := block.Meta["path"]

	style := m.theme.ToolCard
	if block.State == "error" {
		style = m.theme.ErrorCard
	}

	// Header line: "✓ edit path/to/file  (+3 -1)" style
	statusGlyph := "●"
	switch block.State {
	case "done":
		statusGlyph = "✓"
	case "error":
		statusGlyph = "✗"
	case "pending", "streaming":
		statusGlyph = "…"
	}

	header := m.theme.ToolTitle.Render(statusGlyph + " " + title)
	if path != "" {
		header = lipgloss.JoinHorizontal(lipgloss.Left, header, " ", m.theme.Muted.Render(path))
	}
	if m.shouldHideToolContent(block) {
		return style.Width(width).Render(header)
	}

	content := strings.TrimSpace(block.Content)
	var body string
	switch {
	case block.Diff != "" && m.shouldCollapseDiff(block) && !m.isExpanded(block.ID):
		body = m.renderCollapsedDiffBody(block, width)
	case block.Diff != "":
		body = m.renderDiffBody(block, width)
	case m.shouldCollapseToolBlock(block) && !m.isExpanded(block.ID):
		body = m.renderCollapsedToolBody(block, width)
	case m.isExpanded(block.ID):
		body = m.renderExpandedToolBody(block, width)
	case content != "":
		body = lipgloss.NewStyle().Width(width - 4).Render(content)
	default:
		body = m.theme.Muted.Render(block.State)
	}

	card := lipgloss.JoinVertical(lipgloss.Left, header, "", body)
	return style.Width(width).Render(card)
}

func (m Model) renderCollapsedToolBody(block Block, width int) string {
	innerW := max(1, width-4)
	command := strings.TrimSpace(block.Meta["command"])
	lines := []string{}
	if command != "" {
		lines = append(lines,
			m.theme.Muted.Render("Command"),
			lipgloss.NewStyle().Width(innerW).Render("$ "+command),
		)
	}

	summary := strings.TrimSpace(block.Meta[tools.MetadataUICollapsedSummary])
	if summary == "" {
		summary = summarizeCollapsedOutput(block.Content)
	}
	lines = append(lines,
		"",
		m.theme.Muted.Render("Summary"),
		m.theme.Muted.Render(summary),
	)
	if strings.TrimSpace(block.Content) != "" {
		lines = append(lines, m.theme.Muted.Render("Double-click to expand."))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m Model) renderExpandedToolBody(block Block, width int) string {
	innerW := max(1, width-4)
	command := strings.TrimSpace(block.Meta["command"])
	lines := []string{}
	if command != "" {
		lines = append(lines,
			m.theme.Muted.Render("Command"),
			lipgloss.NewStyle().Width(innerW).Render("$ "+command),
			"",
		)
	}
	content := strings.TrimSpace(block.Content)
	if content == "" {
		content = "No output."
	}
	lines = append(lines,
		m.theme.Muted.Render("Output"),
		lipgloss.NewStyle().Width(innerW).Render(content),
		"",
		m.theme.Muted.Render("Double-click to collapse."),
	)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m Model) renderDiffBody(block Block, width int) string {
	if !m.isExpanded(block.ID) {
		return renderDiff(m.theme, width-4, block.Diff)
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		renderDiff(m.theme, width-4, block.Diff),
		"",
		m.theme.Muted.Render("Double-click to collapse."),
	)
}

func (m Model) renderCollapsedDiffBody(block Block, width int) string {
	lines := strings.Split(strings.TrimSpace(block.Diff), "\n")
	changed := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			changed++
		}
	}
	summary := fmt.Sprintf("Collapsed diff: %d line(s), %d changed line(s).", len(lines), changed)
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.theme.Muted.Render(summary),
		m.theme.Muted.Render("Double-click to expand."),
	)
}

// prefixLines prepends `prefix` to each line of `content`, word-wrapping
// to `width`. The prefix itself counts as 1 display column so body width
// gets width-1 cells.
func prefixLines(content string, width int, prefix string) string {
	body := lipgloss.NewStyle().Width(max(1, width-1)).Render(content)
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		lines[i] = prefix + " " + l
	}
	return strings.Join(lines, "\n")
}

func cleanText(s string) string {
	clean := ansi.Strip(s)
	clean = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case unicode.IsControl(r):
			return -1
		default:
			return r
		}
	}, clean)
	return strings.TrimRight(clean, "\r")
}

func summarizeCollapsedOutput(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	lines := strings.Split(trimmed, "\n")
	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}
	if nonEmpty == 0 {
		nonEmpty = len(lines)
	}
	return fmt.Sprintf("Collapsed output: %d line(s), %d byte(s).", nonEmpty, len(trimmed))
}

func shouldHideTool(name string) bool {
	return strings.TrimSpace(name) == "list_tools"
}

func (m Model) isExpanded(blockID string) bool {
	return m.expanded[blockID]
}

func (m Model) shouldCollapseToolBlock(block Block) bool {
	if block.Diff != "" {
		return false
	}
	if m.shouldHideToolContent(block) {
		return false
	}
	return block.Meta[tools.MetadataUIDefaultCollapsed] == "true"
}

// isExpandableBlock reports whether a block supports the expand/collapse
// interaction. Currently tools and errors qualify; both share the same
// m.expanded[block.ID] state.
func (m Model) isExpandableBlock(block Block) bool {
	switch block.Kind {
	case "tool":
		return m.isExpandableToolBlock(block)
	case "error":
		return strings.TrimSpace(block.Content) != ""
	}
	return false
}

// renderErrorBlock renders a stylized, collapsible error card. By default
// the card shows only the header and a one-line summary; double-clicking
// expands it to the full error body for debugging. This mirrors the tool
// block treatment — the expanded state is tracked in m.expanded[block.ID].
func (m Model) renderErrorBlock(block Block, width int) string {
	style := m.theme.ErrorCard
	header := m.theme.StatusError.Render("✗ Error")

	content := strings.TrimSpace(block.Content)
	if content == "" {
		// No details worth expanding — render a minimal card with just the
		// header so the user still gets a signal something went wrong.
		return style.Width(width).Render(header)
	}

	summary := firstMeaningfulLine(content)
	innerW := max(1, width-4)
	var body string
	if m.isExpanded(block.ID) {
		body = lipgloss.JoinVertical(
			lipgloss.Left,
			m.theme.Muted.Render("Details"),
			lipgloss.NewStyle().Width(innerW).Render(content),
			"",
			m.theme.Muted.Render("Double-click to collapse."),
		)
	} else {
		body = lipgloss.JoinVertical(
			lipgloss.Left,
			lipgloss.NewStyle().Width(innerW).Render(summary),
			"",
			m.theme.Muted.Render("Double-click to expand."),
		)
	}

	card := lipgloss.JoinVertical(lipgloss.Left, header, "", body)
	return style.Width(width).Render(card)
}

// firstMeaningfulLine returns the first non-empty trimmed line of s. Used
// to show a compact summary in collapsed error cards without dumping a
// multi-line stack trace.
func firstMeaningfulLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return strings.TrimSpace(s)
}

func (m Model) isExpandableToolBlock(block Block) bool {
	if block.Diff != "" {
		return m.shouldCollapseDiff(block)
	}
	if m.shouldHideToolContent(block) {
		return false
	}
	return m.shouldCollapseToolBlock(block) && strings.TrimSpace(block.Content) != ""
}

func (m Model) shouldHideToolContent(block Block) bool {
	return block.Meta[tools.MetadataUIHideContent] == "true"
}

func (m Model) shouldCollapseDiff(block Block) bool {
	if strings.TrimSpace(block.Diff) == "" {
		return false
	}
	return strings.Count(block.Diff, "\n")+1 > diffCollapseLineThreshold || len(block.Diff) > diffCollapseByteThreshold
}

func decode[T any](payload any) T {
	var out T
	data, _ := json.Marshal(payload)
	_ = json.Unmarshal(data, &out)
	return out
}

func (m Model) blockIndexAtRow(row int) (int, bool) {
	if row < 0 {
		return 0, false
	}
	absRow := m.viewport.YOffset() + row
	for i, span := range m.spans {
		if absRow >= span.start && absRow <= span.end {
			return i, true
		}
	}
	return 0, false
}

func (m Model) selectionRange() (int, int, bool) {
	if m.selAnchor < 0 || m.selFocus < 0 || len(m.blocks) == 0 {
		return 0, 0, false
	}
	start, end := m.selAnchor, m.selFocus
	if start > end {
		start, end = end, start
	}
	if start < 0 || end >= len(m.blocks) {
		return 0, 0, false
	}
	return start, end, true
}

func (m Model) isSelectedBlock(idx int) bool {
	start, end, ok := m.selectionRange()
	if !ok {
		return false
	}
	return idx >= start && idx <= end
}

func (m Model) plainTextForBlock(block Block) string {
	if block.Kind == "user" && len(block.Attachments) > 0 {
		summary := media.AttachmentsSummary(block.Attachments)
		switch content := strings.TrimSpace(block.Content); {
		case content == "":
			return "[" + summary + "]"
		case summary == "":
			return content
		default:
			return content + "\n[" + summary + "]"
		}
	}
	switch block.Kind {
	case "tool":
		if block.Diff != "" {
			return block.Diff
		}
		if name := strings.TrimSpace(block.Meta["name"]); name != "" {
			if content := strings.TrimSpace(block.Content); content != "" {
				return name + "\n" + content
			}
			return name
		}
	}
	return block.Content
}

func (m Model) renderUserAttachments(block Block, width int) string {
	if len(block.Attachments) == 0 {
		return ""
	}

	cards := make([]string, 0, len(block.Attachments))
	for _, attachment := range block.Attachments {
		card := m.renderAttachmentCardBody(attachment)
		cards = append(cards, prefixLines(card, width-2, m.theme.UserPrefix.Render("▎")))
	}
	return lipgloss.JoinVertical(lipgloss.Left, cards...)
}

func (m Model) renderAttachmentCardBody(attachment media.Attachment) string {
	label := m.theme.UserLabel.Render("image")
	name := strings.TrimSpace(attachment.Name)
	if name == "" {
		name = "attachment"
	}

	meta := []string{}
	if attachment.Width > 0 && attachment.Height > 0 {
		meta = append(meta, fmt.Sprintf("%dx%d", attachment.Width, attachment.Height))
	}
	if mediaType := strings.TrimSpace(attachment.MediaType); mediaType != "" {
		meta = append(meta, mediaType)
	}

	lines := []string{
		lipgloss.JoinHorizontal(lipgloss.Left, label, " ", name),
	}
	if len(meta) > 0 {
		lines = append(lines, m.theme.Muted.Render(strings.Join(meta, " • ")))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func attachmentsCacheKey(attachments []media.Attachment) string {
	if len(attachments) == 0 {
		return ""
	}
	parts := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		parts = append(parts, fmt.Sprintf("%s:%s:%d:%d", attachment.Name, attachment.MediaType, attachment.Width, attachment.Height))
	}
	return strings.Join(parts, "|")
}
