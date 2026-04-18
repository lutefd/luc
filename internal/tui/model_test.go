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

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.inspectorOpen {
		t.Fatal("expected inspector to toggle closed")
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
