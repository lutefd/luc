package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

func (m *Model) handleComposerSelectionCollapse(msg tea.KeyPressMsg) {
	if !m.hasComposerSelection() {
		return
	}
	if isPrintableComposerKey(msg) || isComposerDeleteKey(m.input, msg) || key.Matches(msg, m.keys.Newline) {
		return
	}
	if isComposerMoveLeftKey(m.input, msg) || isComposerMoveRightKey(m.input, msg) {
		return
	}
	m.clearComposerSelection()
}

func (m *Model) selectAllComposer() bool {
	length := composerTextLen(m.input.Value())
	if length == 0 {
		return false
	}
	m.composerAnchor = 0
	m.composerActive = length
	m.setComposerCursor(length)
	return true
}

func (m *Model) extendComposerSelection(delta int) bool {
	if composerTextLen(m.input.Value()) == 0 || delta == 0 {
		return false
	}
	anchor := m.composerCursorOffset()
	if m.hasComposerSelection() {
		anchor = m.composerAnchor
	}
	active := clamp(m.composerCursorOffset()+delta, 0, composerTextLen(m.input.Value()))
	if anchor == active {
		m.clearComposerSelection()
		m.setComposerCursor(active)
		return true
	}
	m.composerAnchor = anchor
	m.composerActive = active
	m.setComposerCursor(active)
	return true
}

func (m *Model) collapseComposerSelection(toEnd bool) {
	start, end, ok := m.composerSelectionRange()
	if !ok {
		return
	}
	if toEnd {
		m.setComposerCursor(end)
	} else {
		m.setComposerCursor(start)
	}
	m.clearComposerSelection()
}

func (m *Model) replaceComposerSelection(replacement string) bool {
	start, end, ok := m.composerSelectionRange()
	if !ok {
		return false
	}
	runes := []rune(m.input.Value())
	updated := string(append(append([]rune(nil), runes[:start]...), append([]rune(replacement), runes[end:]...)...))
	m.input.SetValue(updated)
	m.clearComposerSelection()
	m.setComposerCursor(start + len([]rune(replacement)))
	return true
}

func (m *Model) selectedComposerText() string {
	start, end, ok := m.composerSelectionRange()
	if !ok {
		return ""
	}
	runes := []rune(m.input.Value())
	return string(runes[start:end])
}

func (m Model) composerSelectionRange() (int, int, bool) {
	if !m.hasComposerSelection() {
		return 0, 0, false
	}
	if m.composerAnchor < m.composerActive {
		return m.composerAnchor, m.composerActive, true
	}
	return m.composerActive, m.composerAnchor, true
}

func (m Model) hasComposerSelection() bool {
	return m.composerAnchor >= 0 && m.composerActive >= 0 && m.composerAnchor != m.composerActive
}

func (m *Model) clearComposerSelection() {
	m.composerAnchor = -1
	m.composerActive = -1
}

func (m Model) composerCursorOffset() int {
	return composerLineColToOffset(m.input.Value(), m.input.Line(), m.input.Column())
}

func (m *Model) setComposerCursor(offset int) {
	line, col := composerOffsetToLineCol(m.input.Value(), offset)
	m.input.MoveToBegin()
	for m.input.Line() < line {
		m.input.CursorDown()
	}
	for m.input.Line() > line {
		m.input.CursorUp()
	}
	m.input.SetCursorColumn(col)
}

func isPrintableComposerKey(msg tea.KeyPressMsg) bool {
	return msg.Text != "" && (msg.Mod == 0 || msg.Mod == tea.ModShift)
}

func isComposerDeleteKey(input textarea.Model, msg tea.KeyPressMsg) bool {
	return key.Matches(msg, input.KeyMap.DeleteCharacterBackward) ||
		key.Matches(msg, input.KeyMap.DeleteCharacterForward) ||
		key.Matches(msg, input.KeyMap.DeleteWordBackward) ||
		key.Matches(msg, input.KeyMap.DeleteWordForward) ||
		key.Matches(msg, input.KeyMap.DeleteBeforeCursor) ||
		key.Matches(msg, input.KeyMap.DeleteAfterCursor)
}

func isComposerMoveLeftKey(input textarea.Model, msg tea.KeyPressMsg) bool {
	return key.Matches(msg, input.KeyMap.CharacterBackward) ||
		key.Matches(msg, input.KeyMap.WordBackward) ||
		key.Matches(msg, input.KeyMap.LineStart) ||
		key.Matches(msg, input.KeyMap.InputBegin) ||
		key.Matches(msg, input.KeyMap.LinePrevious) ||
		key.Matches(msg, input.KeyMap.PageUp)
}

func isComposerMoveRightKey(input textarea.Model, msg tea.KeyPressMsg) bool {
	return key.Matches(msg, input.KeyMap.CharacterForward) ||
		key.Matches(msg, input.KeyMap.WordForward) ||
		key.Matches(msg, input.KeyMap.LineEnd) ||
		key.Matches(msg, input.KeyMap.InputEnd) ||
		key.Matches(msg, input.KeyMap.LineNext) ||
		key.Matches(msg, input.KeyMap.PageDown)
}

func composerTextLen(value string) int {
	return len([]rune(value))
}

func composerLineColToOffset(value string, line, col int) int {
	lines := strings.Split(value, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	line = clamp(line, 0, len(lines)-1)
	offset := 0
	for i := 0; i < line; i++ {
		offset += len([]rune(lines[i])) + 1
	}
	col = clamp(col, 0, len([]rune(lines[line])))
	return offset + col
}

func composerOffsetToLineCol(value string, offset int) (line, col int) {
	lines := strings.Split(value, "\n")
	if len(lines) == 0 {
		return 0, 0
	}
	offset = clamp(offset, 0, composerTextLen(value))
	for line = 0; line < len(lines); line++ {
		lineLen := len([]rune(lines[line]))
		if offset <= lineLen {
			return line, offset
		}
		offset -= lineLen
		if line < len(lines)-1 {
			if offset == 0 {
				return line, lineLen
			}
			offset--
		}
	}
	last := len(lines) - 1
	return last, len([]rune(lines[last]))
}
