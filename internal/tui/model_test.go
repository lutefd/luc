package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/lutefd/luc/internal/kernel"
	"github.com/lutefd/luc/internal/media"
	modelspicker "github.com/lutefd/luc/internal/tui/models"
)

const testImageDataURL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+nmZ0AAAAASUVORK5CYII="

// TestMain isolates the user-level state file for every test in this package.
// Tests here boot a real kernel.Controller, which reads ~/.luc/state.yaml at
// startup to overlay the user's persisted theme/model — without isolation the
// developer's real preferences bleed in and cause flaky assertions (e.g. a
// test that switches to gpt-5.4 already sees that model as the startup
// default).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "luc-tui-state-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	os.Setenv("LUC_STATE_DIR", dir)
	os.Exit(m.Run())
}

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

func TestModelEnterSendsPendingImageWithoutText(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	attachment, ok, err := attachmentFromPasteContent(testImageDataURL)
	if err != nil || !ok {
		t.Fatalf("expected test image attachment, ok=%v err=%v", ok, err)
	}
	model.pendingImages = []media.Attachment{attachment}

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m := updated.(Model)
	if len(m.pendingImages) != 0 {
		t.Fatalf("expected pending images to clear after send, got %d", len(m.pendingImages))
	}
	if cmd == nil {
		t.Fatal("expected submit command for attachment-only message")
	}
}

func TestModelPasteDataURLQueuesImageAttachment(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	updated, _ := model.Update(tea.PasteMsg{Content: testImageDataURL})
	m := updated.(Model)
	if len(m.pendingImages) != 1 {
		t.Fatalf("expected one pending image, got %d", len(m.pendingImages))
	}
	if got := m.pendingImages[0].MediaType; got != "image/png" {
		t.Fatalf("expected image/png attachment, got %q", got)
	}
}

func TestModelPasteShortcutQueuesClipboardImageForCtrlAndCmdV(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	attachment, ok, err := attachmentFromPasteContent(testImageDataURL)
	if err != nil || !ok {
		t.Fatalf("expected test image attachment, ok=%v err=%v", ok, err)
	}

	origImage := buildClipboardImageAttachment
	origText := readClipboardText
	defer func() {
		buildClipboardImageAttachment = origImage
		readClipboardText = origText
	}()
	buildClipboardImageAttachment = func() (media.Attachment, error) { return attachment, nil }
	readClipboardText = func() (string, error) { return "", nil }

	for _, msg := range []tea.KeyPressMsg{
		{Code: 'v', Mod: tea.ModCtrl},
		{Code: 'v', Mod: tea.ModSuper},
	} {
		model := New(controller)
		updated, cmd := model.Update(msg)
		m := updated.(Model)
		if cmd == nil {
			t.Fatalf("expected paste command for %#v", msg)
		}
		next, _ := m.Update(cmd())
		m = next.(Model)
		if len(m.pendingImages) != 1 {
			t.Fatalf("expected one pending image for %#v, got %d", msg, len(m.pendingImages))
		}
	}
}

func TestModelPendingImageRecomputesBodyHeight(t *testing.T) {
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
	before := m.bodyHeight()

	updated, _ = m.Update(tea.PasteMsg{Content: testImageDataURL})
	m = updated.(Model)
	after := m.bodyHeight()

	if after >= before {
		t.Fatalf("expected body height to shrink after attachment footer grows, before=%d after=%d", before, after)
	}
}
