package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/lutefd/luc/internal/kernel"
	modelspicker "github.com/lutefd/luc/internal/tui/models"
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

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
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
	if !strings.Contains(view.Content, "hello") {
		t.Fatalf("expected transcript content in view, got %q", view.Content)
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
	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m := updated.(Model)
	if got := m.input.Value(); got != "" {
		t.Fatalf("expected cleared input after send, got %q", got)
	}
	if cmd == nil {
		t.Fatal("expected submit command")
	}
}

func TestModelSwitchUpdatesInspectorOverview(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	model.inspector.SetSize(40, 12)
	updated, _ := model.Update(modelspicker.Selected{ModelID: "gpt-5.4"})
	m := updated.(Model)

	if view := m.inspector.SummaryView(); !strings.Contains(view, "gpt-5.4") {
		t.Fatalf("expected inspector summary to show switched model, got %q", view)
	}
}

func TestModelKeepsMouseCaptureEnabled(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	view := model.View()
	if view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("expected mouse capture by default, got %v", view.MouseMode)
	}

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	m := updated.(Model)
	view = m.View()
	if view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("expected mouse capture to stay enabled, got %v", view.MouseMode)
	}
}

func TestModelCopyKeyDispatchesCopyMessage(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	updated, cmd := model.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	_ = updated.(Model)
	if cmd == nil {
		t.Fatal("expected copy command")
	}
	msg := cmd()
	if _, ok := msg.(copySelectionMsg); !ok {
		t.Fatalf("expected copySelectionMsg, got %T", msg)
	}
}
