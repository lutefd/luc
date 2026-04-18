package transcript

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/theme"
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
	model.UpdateViewport(tea.KeyMsg{Type: tea.KeyPgDown})

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
