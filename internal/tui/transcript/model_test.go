package transcript

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/theme"
	"github.com/lutefd/luc/internal/tools"
)

func TestTranscriptApplyAndView(t *testing.T) {
	model := New(theme.Default(theme.VariantLight), theme.VariantLight)
	model.SetSize(80, 20)

	events := []history.EventEnvelope{
		{Kind: "message.user", Payload: history.MessagePayload{ID: "u1", Content: "hello"}},
		{Kind: "message.assistant.delta", Payload: history.MessageDeltaPayload{ID: "a1", Delta: "# Hi"}},
		{Kind: "message.assistant.final", Payload: history.MessagePayload{ID: "a1", Content: "# Hi\n\nworld"}},
		{Kind: "tool.requested", Payload: history.ToolCallPayload{ID: "t1", Name: "read", Arguments: `{"path":"go.mod"}`}},
		{Kind: "tool.finished", Payload: history.ToolResultPayload{ID: "t1", Name: "read", Content: "module github.com/lutefd/luc"}},
		{Kind: "reload.finished", Payload: history.ReloadPayload{Version: 2}},
	}
	for _, ev := range events {
		model.Apply(ev)
	}
	model.UpdateViewport(tea.KeyPressMsg{Code: tea.KeyPgDown})

	view := ansi.Strip(model.View())
	for _, want := range []string{"hello", "Hi", "module github.com/lutefd/luc", "reloaded runtime to version 2"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in transcript view:\n%s", want, view)
		}
	}
	if strings.Contains(view, "# Hi") {
		t.Fatalf("expected rendered markdown rather than raw heading markers:\n%s", view)
	}
}

func TestTranscriptHandlesErrors(t *testing.T) {
	model := New(theme.Default(theme.VariantLight), theme.VariantLight)
	model.SetSize(80, 20)
	model.Apply(history.EventEnvelope{
		Kind: "system.error",
		Payload: history.MessagePayload{
			ID:      "err1",
			Content: "boom",
		},
	})
	if !strings.Contains(ansi.Strip(model.View()), "boom") {
		t.Fatalf("expected error content in view, got %q", model.View())
	}
}

func TestTranscriptSanitizesTerminalResponses(t *testing.T) {
	model := New(theme.Default(theme.VariantLight), theme.VariantLight)
	model.SetSize(80, 20)
	model.Apply(history.EventEnvelope{
		Kind: "message.assistant.final",
		Payload: history.MessagePayload{
			ID:      "a1",
			Content: "\x1b]11;rgb:efef/f1f1/f\x07# Title",
		},
	})
	view := ansi.Strip(model.View())
	if strings.Contains(view, "]11;rgb") {
		t.Fatalf("expected terminal response to be stripped, got %q", view)
	}
}

func TestTranscriptStopsAutoFollowAfterManualScroll(t *testing.T) {
	model := New(theme.Default(theme.VariantLight), theme.VariantLight)
	model.SetSize(40, 4)

	for i := 0; i < 12; i++ {
		model.Apply(history.EventEnvelope{
			Kind:    "system.note",
			Payload: history.MessagePayload{ID: string(rune('a' + i)), Content: strings.Repeat("line ", 4)},
		})
	}
	if !model.viewport.AtBottom() {
		t.Fatalf("expected initial auto-follow to land at bottom, got offset %d", model.viewport.YOffset())
	}

	model.UpdateViewport(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if model.autoFollow {
		t.Fatal("expected auto-follow to disable after manual scroll")
	}
	offset := model.viewport.YOffset()

	model.Apply(history.EventEnvelope{
		Kind:    "system.note",
		Payload: history.MessagePayload{ID: "z", Content: "new content"},
	})

	if model.viewport.YOffset() != offset {
		t.Fatalf("expected viewport offset to stay at %d, got %d", offset, model.viewport.YOffset())
	}
}

func TestTranscriptSelectionCopiesPlainTextAcrossBlocks(t *testing.T) {
	model := New(theme.Default(theme.VariantLight), theme.VariantLight)
	model.SetSize(60, 12)

	model.Apply(history.EventEnvelope{
		Kind:    "message.user",
		Payload: history.MessagePayload{ID: "u1", Content: "first"},
	})
	model.Apply(history.EventEnvelope{
		Kind:    "message.assistant.final",
		Payload: history.MessagePayload{ID: "a1", Content: "second"},
	})
	model.Apply(history.EventEnvelope{
		Kind:    "tool.finished",
		Payload: history.ToolResultPayload{ID: "t1", Name: "read", Content: "third"},
	})

	model.viewport.GotoTop()
	startRow := model.spans[0].start - model.viewport.YOffset()
	endRow := model.spans[2].start - model.viewport.YOffset()
	model.BeginSelection(startRow)
	model.ExtendSelection(endRow)
	model.EndSelection()

	if !model.HasSelection() {
		t.Fatal("expected selection to be active")
	}

	want := "first\n\nsecond\n\nread\nthird"
	if got := model.SelectedText(); got != want {
		t.Fatalf("expected selected text %q, got %q", want, got)
	}
}

func TestTranscriptCollapsesBashOutputInToolCards(t *testing.T) {
	model := New(theme.Default(theme.VariantLight), theme.VariantLight)
	model.SetSize(80, 20)
	model.Apply(history.EventEnvelope{
		Kind: "tool.finished",
		Payload: history.ToolResultPayload{
			ID:      "bash1",
			Name:    "bash",
			Content: "first line\nsecond line\nthird line",
			Metadata: map[string]any{
				"command":                        "npm test",
				tools.MetadataUIDefaultCollapsed: true,
				tools.MetadataUICollapsedSummary: "Collapsed output: 3 line(s), 33 byte(s).",
			},
		},
	})

	view := ansi.Strip(model.View())
	for _, want := range []string{"$ npm test", "Collapsed output: 3 line(s)", "Double-click to expand."} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q in transcript view:\n%s", want, view)
		}
	}
	if strings.Contains(view, "second line") {
		t.Fatalf("expected bash output to stay collapsed in transcript view:\n%s", view)
	}
}

func TestTranscriptToggleToolExpansionAtRow(t *testing.T) {
	model := New(theme.Default(theme.VariantLight), theme.VariantLight)
	model.SetSize(80, 20)
	model.Apply(history.EventEnvelope{
		Kind: "tool.finished",
		Payload: history.ToolResultPayload{
			ID:      "bash1",
			Name:    "bash",
			Content: "first line\nsecond line",
			Metadata: map[string]any{
				"command":                        "npm test",
				tools.MetadataUIDefaultCollapsed: true,
				tools.MetadataUICollapsedSummary: "Collapsed output: 2 line(s), 22 byte(s).",
			},
		},
	})

	row := model.spans[0].start - model.viewport.YOffset()
	if ok := model.ToggleToolExpansionAtRow(row); !ok {
		t.Fatal("expected toggle to succeed for bash block")
	}
	view := ansi.Strip(model.View())
	if !strings.Contains(view, "second line") || !strings.Contains(view, "Double-click to collapse.") {
		t.Fatalf("expected expanded bash output in transcript view:\n%s", view)
	}
}

func TestTranscriptHidesLoadSkillBodyAndShowsOnlyLabel(t *testing.T) {
	model := New(theme.Default(theme.VariantLight), theme.VariantLight)
	model.SetSize(80, 20)
	model.Apply(history.EventEnvelope{
		Kind: "tool.finished",
		Payload: history.ToolResultPayload{
			ID:      "skill1",
			Name:    "load_skill",
			Content: "<skill_content name=\"rails\">Prefer bin/rails.</skill_content>",
			Metadata: map[string]any{
				tools.MetadataUIHideContent: true,
				tools.MetadataUILabel:       "skill loaded rails",
			},
		},
	})

	view := ansi.Strip(model.View())
	if !strings.Contains(view, "skill loaded rails") {
		t.Fatalf("expected skill label in transcript view:\n%s", view)
	}
	if strings.Contains(view, "Prefer bin/rails") || strings.Contains(view, "Double-click") {
		t.Fatalf("expected skill body to stay hidden and non-expandable:\n%s", view)
	}

	row := model.spans[0].start - model.viewport.YOffset()
	if ok := model.ToggleToolExpansionAtRow(row); ok {
		t.Fatalf("expected hidden skill tool block to be non-expandable")
	}
}

func TestTranscriptHidesListToolsFromView(t *testing.T) {
	model := New(theme.Default(theme.VariantLight), theme.VariantLight)
	model.SetSize(80, 20)
	model.Apply(history.EventEnvelope{
		Kind:    "tool.requested",
		Payload: history.ToolCallPayload{ID: "lt1", Name: "list_tools", Arguments: `{}`},
	})
	model.Apply(history.EventEnvelope{
		Kind:    "tool.finished",
		Payload: history.ToolResultPayload{ID: "lt1", Name: "list_tools", Content: "read\nwrite\nedit\nbash"},
	})

	view := ansi.Strip(model.View())
	if strings.Contains(view, "list_tools") || strings.Contains(view, "read\nwrite") {
		t.Fatalf("expected list_tools to stay hidden from transcript view:\n%s", view)
	}
}

func TestTranscriptShowsSmallDiffInlineAndCollapsesLargeDiff(t *testing.T) {
	model := New(theme.Default(theme.VariantLight), theme.VariantLight)
	model.SetSize(100, 30)

	smallDiff := strings.Join([]string{
		"@@ -1,2 +1,2 @@",
		"-old",
		"+new",
	}, "\n")
	model.Apply(history.EventEnvelope{
		Kind: "tool.finished",
		Payload: history.ToolResultPayload{
			ID:      "edit-small",
			Name:    "edit",
			Content: "applied 1 edit",
			Metadata: map[string]any{
				"diff": smallDiff,
			},
		},
	})

	view := ansi.Strip(model.View())
	if !strings.Contains(view, "@@ -1,2 +1,2 @@") || strings.Contains(view, "Collapsed diff:") {
		t.Fatalf("expected small diff inline in transcript view:\n%s", view)
	}

	largeLines := make([]string, 0, diffCollapseLineThreshold+5)
	largeLines = append(largeLines, "@@ -1,1 +1,1 @@")
	for i := 0; i < diffCollapseLineThreshold+4; i++ {
		largeLines = append(largeLines, fmt.Sprintf("+line %d", i))
	}
	largeDiff := strings.Join(largeLines, "\n")
	model.Apply(history.EventEnvelope{
		Kind: "tool.finished",
		Payload: history.ToolResultPayload{
			ID:      "edit-large",
			Name:    "edit",
			Content: "applied many edits",
			Metadata: map[string]any{
				"diff": largeDiff,
			},
		},
	})

	view = ansi.Strip(model.View())
	if !strings.Contains(view, "Collapsed diff:") || !strings.Contains(view, "Double-click to expand.") {
		t.Fatalf("expected large diff to collapse in transcript view:\n%s", view)
	}
}
