package viewrender

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	luruntime "github.com/lutefd/luc/internal/runtime"
	"github.com/lutefd/luc/internal/theme"
)

const runtimeMarkdownWrapWidth = 80

type Result interface {
	RenderContent() string
}

func Render(themeName, workspaceRoot string, view luruntime.RuntimeView, result Result) string {
	content := strings.TrimSpace(result.RenderContent())
	switch strings.TrimSpace(view.Render) {
	case "markdown":
		_, variant, err := theme.Load(themeName, workspaceRoot)
		if err != nil {
			return content
		}
		renderer, err := theme.NewMarkdownRenderer(runtimeMarkdownWrapWidth, variant)
		if err != nil {
			return content
		}
		rendered, err := renderer.Render(content)
		if err != nil {
			return content
		}
		return strings.TrimSpace(rendered)
	case "json":
		var decoded any
		if err := json.Unmarshal([]byte(content), &decoded); err != nil {
			return content
		}
		data, _ := json.MarshalIndent(decoded, "", "  ")
		return string(data)
	case "kv":
		var decoded map[string]any
		if err := json.Unmarshal([]byte(content), &decoded); err != nil {
			return content
		}
		keys := make([]string, 0, len(decoded))
		for key := range decoded {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		lines := make([]string, 0, len(keys))
		for _, key := range keys {
			lines = append(lines, fmt.Sprintf("%s: %v", key, decoded[key]))
		}
		return strings.Join(lines, "\n")
	case "table":
		return renderTable(content)
	default:
		return content
	}
}

func renderTable(content string) string {
	var rows []map[string]any
	if err := json.Unmarshal([]byte(content), &rows); err != nil {
		return content
	}
	if len(rows) == 0 {
		return ""
	}
	columnSet := map[string]struct{}{}
	for _, row := range rows {
		for key := range row {
			columnSet[key] = struct{}{}
		}
	}
	columns := make([]string, 0, len(columnSet))
	for key := range columnSet {
		columns = append(columns, key)
	}
	sort.Strings(columns)

	widths := make(map[string]int, len(columns))
	for _, column := range columns {
		widths[column] = len(column)
	}
	for _, row := range rows {
		for _, column := range columns {
			widths[column] = max(widths[column], len(fmt.Sprintf("%v", row[column])))
		}
	}

	var lines []string
	lines = append(lines, formatTableRow(columns, widths, func(column string) string { return column }))
	lines = append(lines, formatTableRow(columns, widths, func(column string) string { return strings.Repeat("-", widths[column]) }))
	for _, row := range rows {
		lines = append(lines, formatTableRow(columns, widths, func(column string) string {
			return fmt.Sprintf("%v", row[column])
		}))
	}
	return strings.Join(lines, "\n")
}

func formatTableRow(columns []string, widths map[string]int, value func(string) string) string {
	cells := make([]string, 0, len(columns))
	for _, column := range columns {
		cells = append(cells, fmt.Sprintf("%-*s", widths[column], value(column)))
	}
	return strings.Join(cells, "  ")
}
