package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lutefd/luc/internal/kernel"
)

func TestModelHandlesResizeToggleAndEvents(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m := updated.(Model)
	if m.transcriptWidth() <= 0 {
		t.Fatalf("expected transcript width to be set")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = updated.(Model)
	if !m.inspectorOpen {
		t.Fatal("expected inspector to toggle open")
	}

	updated, _ = m.Update(appEventMsg{
		Kind:    "message.user",
		Payload: map[string]any{"id": "u1", "content": "hello"},
	})
	m = updated.(Model)
	view := m.View()
	if !strings.Contains(view, "hello") {
		t.Fatalf("expected transcript content in view, got %q", view)
	}
}

func TestModelEnterSendsAndClearsInput(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	model.input.SetValue("inspect this")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(Model)
	if got := m.input.Value(); got != "" {
		t.Fatalf("expected cleared input after send, got %q", got)
	}
	if cmd == nil {
		t.Fatal("expected submit command")
	}
}
