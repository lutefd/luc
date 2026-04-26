package transcript

import (
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/lutefd/luc/internal/theme"
)

// renderDiff parses a unified-diff string and renders it side-by-side
// (crush-style): old content with line numbers on the left, new on the
// right. Additions get green bg, removals get red bg, context lines are
// shown on both sides.
func renderDiff(th theme.Theme, width int, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	type pair struct {
		oldNo   int
		newNo   int
		oldLine string
		newLine string
		oldKind byte // ' ', '-', 0 (empty slot)
		newKind byte // ' ', '+', 0 (empty slot)
	}

	var pairs []pair
	var hunks []string

	// Parse hunks, aligning - with + when adjacent.
	var pendingDel []struct {
		no   int
		text string
	}
	var oldNo, newNo int
	flushPending := func() {
		for _, d := range pendingDel {
			pairs = append(pairs, pair{
				oldNo:   d.no,
				oldLine: d.text,
				oldKind: '-',
				newKind: 0,
			})
		}
		pendingDel = pendingDel[:0]
	}

	for _, ln := range strings.Split(raw, "\n") {
		if strings.HasPrefix(ln, "--- ") || strings.HasPrefix(ln, "+++ ") {
			continue
		}
		if strings.HasPrefix(ln, "@@") {
			flushPending()
			if a, b, ok := parseHunkHeader(ln); ok {
				oldNo = a
				newNo = b
			}
			hunks = append(hunks, ln)
			// insert a hunk-marker row (oldNo=0 signals header)
			pairs = append(pairs, pair{oldNo: -1, oldLine: ln})
			continue
		}
		if ln == "" {
			flushPending()
			pairs = append(pairs, pair{oldNo: oldNo, newNo: newNo, oldKind: ' ', newKind: ' '})
			oldNo++
			newNo++
			continue
		}
		marker := ln[0]
		body := ln[1:]
		switch marker {
		case '-':
			pendingDel = append(pendingDel, struct {
				no   int
				text string
			}{no: oldNo, text: body})
			oldNo++
		case '+':
			// Pair with earliest pending delete if any.
			if len(pendingDel) > 0 {
				d := pendingDel[0]
				pendingDel = pendingDel[1:]
				pairs = append(pairs, pair{
					oldNo:   d.no,
					newNo:   newNo,
					oldLine: d.text,
					newLine: body,
					oldKind: '-',
					newKind: '+',
				})
			} else {
				pairs = append(pairs, pair{
					newNo:   newNo,
					newLine: body,
					oldKind: 0,
					newKind: '+',
				})
			}
			newNo++
		default:
			flushPending()
			pairs = append(pairs, pair{
				oldNo:   oldNo,
				newNo:   newNo,
				oldLine: body,
				newLine: body,
				oldKind: ' ',
				newKind: ' ',
			})
			oldNo++
			newNo++
		}
	}
	flushPending()
	_ = hunks

	// Each side width: half the available width minus gutter (line number).
	// Keep the rendered row bounded to width; forcing a minimum here makes
	// narrow cards overflow and can corrupt the viewport layout.
	const lnWidth = 4
	const markerW = 3 // " + ", " - ", "   "
	const gutter = 2  // " │"
	available := max(1, width-gutter)
	colW := max(1, (available/2)-lnWidth-markerW)

	renderCell := func(lineNo int, kind byte, text string, isLeft bool) string {
		var lnStr string
		if lineNo > 0 {
			lnStr = fmt.Sprintf("%*d", lnWidth, lineNo)
		} else {
			lnStr = strings.Repeat(" ", lnWidth)
		}

		var marker string
		var style lipgloss.Style
		switch kind {
		case '+':
			marker = " + "
			style = th.DiffAdd
		case '-':
			marker = " - "
			style = th.DiffDel
		case ' ':
			marker = "   "
			style = th.DiffContext
		default:
			// empty slot (e.g. right side of a pure deletion)
			return th.DiffGutter.Render(lnStr) + "   " + strings.Repeat(" ", colW)
		}
		body := padOrTruncate(text, colW)
		_ = isLeft
		return th.DiffGutter.Render(lnStr) + style.Width(markerW+colW).MaxWidth(markerW+colW).Render(marker+body)
	}

	rowW := min(width, 2*(lnWidth+markerW+colW)+gutter)
	var lines []string
	for _, p := range pairs {
		if p.oldNo == -1 {
			// hunk header — full row, muted, bounded to row width so it
			// doesn't overflow and cause the terminal to soft-wrap.
			lines = append(lines, th.DiffHunk.Render(padOrTruncate(p.oldLine, rowW)))
			continue
		}
		left := renderCell(p.oldNo, p.oldKind, p.oldLine, true)
		right := renderCell(p.newNo, p.newKind, p.newLine, false)
		divider := th.DiffGutter.Render(" │")
		lines = append(lines, left+divider+right)
	}
	return strings.Join(lines, "\n")
}

func parseHunkHeader(s string) (oldStart, newStart int, ok bool) {
	i := strings.Index(s, "-")
	j := strings.Index(s, "+")
	if i < 0 || j < 0 || j < i {
		return 0, 0, false
	}
	oldPart := strings.TrimSpace(s[i+1 : j])
	if idx := strings.Index(oldPart, ","); idx >= 0 {
		oldPart = oldPart[:idx]
	}
	rest := s[j+1:]
	if end := strings.Index(rest, " "); end >= 0 {
		rest = rest[:end]
	}
	if idx := strings.Index(rest, ","); idx >= 0 {
		rest = rest[:idx]
	}
	var a, b int
	if _, err := fmt.Sscanf(oldPart, "%d", &a); err != nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(rest, "%d", &b); err != nil {
		return 0, 0, false
	}
	return a, b, true
}

func padOrTruncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	n := lipgloss.Width(s)
	if n == width {
		return s
	}
	if n < width {
		return s + strings.Repeat(" ", width-n)
	}
	// Truncate by runes; may over-truncate with wide glyphs but safe for code.
	out := make([]rune, 0, width)
	w := 0
	for _, r := range s {
		if w >= width-1 {
			break
		}
		out = append(out, r)
		w++
	}
	return string(out) + "…"
}
