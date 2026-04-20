package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/kernel"
	"github.com/lutefd/luc/internal/media"
	"github.com/lutefd/luc/internal/tools"
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

	updated, _ = m.Update(appEventsMsg{{
		Kind:    "message.user",
		Payload: map[string]any{"id": "u1", "content": "hello"},
	}})
	m = updated.(Model)
	view := m.View()
	if !strings.Contains(view.Content, "hello") {
		t.Fatalf("expected transcript content in view, got %q", view.Content)
	}
}

func TestCompactHeaderPathPrefersHomeRelativeTail(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("home directory unavailable")
	}

	got := compactHeaderPath(filepath.Join(home, "dev", "p", "luc"), 18)
	if strings.Contains(got, filepath.ToSlash(home)) {
		t.Fatalf("expected home prefix to be hidden, got %q", got)
	}
	if !strings.HasPrefix(got, "~/") {
		t.Fatalf("expected home-relative path, got %q", got)
	}
	if !strings.Contains(got, "luc") {
		t.Fatalf("expected project tail to remain visible, got %q", got)
	}
}

func TestCompactHeaderPathKeepsUsefulTailWhenTrimmed(t *testing.T) {
	got := compactHeaderPath("/Users/lfdourado/dev/fury_mshops/fury_mshops-frontend-wrapper-go", 22)
	if !strings.Contains(got, "frontend-wrapper-go") {
		t.Fatalf("expected useful tail to remain, got %q", got)
	}
	if strings.Contains(got, "/Users/lfdourado") {
		t.Fatalf("expected leading path to be collapsed, got %q", got)
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

func TestModelEscapeClearsComposerAndPendingImages(t *testing.T) {
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

	model := New(controller)
	model.input.SetValue("draft")
	model.pendingImages = []media.Attachment{attachment}

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m := updated.(Model)
	if cmd != nil {
		t.Fatal("expected escape clear to avoid extra command dispatch")
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("expected cleared input, got %q", got)
	}
	if len(m.pendingImages) != 0 {
		t.Fatalf("expected pending images cleared, got %d", len(m.pendingImages))
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
	for _, keyMsg := range []tea.KeyPressMsg{
		{Code: 'y', Mod: tea.ModCtrl},
		{Code: 'c', Mod: tea.ModSuper},
	} {
		updated, cmd := model.Update(keyMsg)
		_ = updated.(Model)
		if cmd == nil {
			t.Fatalf("expected copy command for %#v", keyMsg)
		}
		msg := cmd()
		if _, ok := msg.(copySelectionMsg); !ok {
			t.Fatalf("expected copySelectionMsg for %#v, got %T", keyMsg, msg)
		}
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

func TestModelEscapeClearsComposerWithoutCancelingActiveTurn(t *testing.T) {
	t.Setenv("LUC_STATE_DIR", t.TempDir())

	root := newExecProviderWorkspace(t, `#!/bin/sh
cat >/dev/null
sleep 30
printf '%s\n' '{"type":"done","completed":true}'
`)

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	attachment, ok, err := attachmentFromPasteContent(testImageDataURL)
	if err != nil || !ok {
		t.Fatalf("expected test image attachment, ok=%v err=%v", ok, err)
	}

	model := New(controller)
	model.input.SetValue("draft")
	model.pendingImages = []media.Attachment{attachment}

	done := make(chan error, 1)
	go func() {
		done <- controller.Submit(context.Background(), "long running")
	}()
	waitForTurnState(t, controller, true)

	updated, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m := updated.(Model)
	if cmd != nil {
		t.Fatal("expected escape clear to avoid stop command")
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("expected cleared input, got %q", got)
	}
	if len(m.pendingImages) != 0 {
		t.Fatalf("expected pending images cleared, got %d", len(m.pendingImages))
	}
	if !controller.TurnActive() {
		t.Fatal("expected active turn to remain active after escape clear")
	}

	if !controller.CancelTurn() {
		t.Fatal("expected cleanup cancellation to succeed")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected canceled submit to return nil, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for canceled submit")
	}
}

func TestModelStopKeyCancelsActiveTurnWithoutClearingDraft(t *testing.T) {
	t.Setenv("LUC_STATE_DIR", t.TempDir())

	root := newExecProviderWorkspace(t, `#!/bin/sh
cat >/dev/null
sleep 30
printf '%s\n' '{"type":"done","completed":true}'
`)

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	attachment, ok, err := attachmentFromPasteContent(testImageDataURL)
	if err != nil || !ok {
		t.Fatalf("expected test image attachment, ok=%v err=%v", ok, err)
	}

	model := New(controller)
	model.input.SetValue("draft stays")
	model.pendingImages = []media.Attachment{attachment}

	done := make(chan error, 1)
	go func() {
		done <- controller.Submit(context.Background(), "long running")
	}()
	waitForTurnState(t, controller, true)

	updated, cmd := model.Update(tea.KeyPressMsg{Code: '.', Mod: tea.ModCtrl})
	m := updated.(Model)
	if cmd == nil {
		t.Fatal("expected stop command")
	}
	next, _ := m.Update(cmd())
	m = next.(Model)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected canceled submit to return nil, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for canceled submit")
	}

	if controller.TurnActive() {
		t.Fatal("expected turn to be inactive after stop key")
	}
	if got := m.input.Value(); got != "draft stays" {
		t.Fatalf("expected draft input to remain, got %q", got)
	}
	if len(m.pendingImages) != 1 || m.pendingImages[0].ID != attachment.ID {
		t.Fatalf("expected pending image to remain after stop, got %#v", m.pendingImages)
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

func TestModelMouseDragSelectionExtendsWithoutLeftButtonOnMotion(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := updated.(Model)

	updated, _ = m.Update(appEventsMsg{
		{Kind: "message.user", Payload: history.MessagePayload{ID: "u1", Content: "first"}},
		{Kind: "message.assistant.final", Payload: history.MessagePayload{ID: "a1", Content: strings.Repeat("wrapped assistant content ", 12)}},
		{Kind: "message.user", Payload: history.MessagePayload{ID: "u2", Content: "second"}},
	})
	m = updated.(Model)

	headerH, _, bodyH := m.layoutHeights()
	findRow := func(id string) int {
		for row := 0; row < bodyH; row++ {
			if got, ok := m.transcript.BlockIDAtRow(row); ok && got == id {
				return row
			}
		}
		return -1
	}

	startRow := findRow("a1")
	endRow := findRow("u2")
	if startRow < 0 || endRow < 0 {
		t.Fatalf("expected selectable rows for assistant/user blocks, start=%d end=%d", startRow, endRow)
	}

	updated, _ = m.Update(tea.MouseClickMsg{X: 1, Y: headerH + startRow, Button: tea.MouseLeft})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseMotionMsg{X: 1, Y: headerH + endRow})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseReleaseMsg{X: 1, Y: headerH + endRow, Button: tea.MouseLeft})
	m = updated.(Model)

	if !m.transcript.HasSelection() {
		t.Fatal("expected mouse drag to keep a selection")
	}
	selected := m.transcript.SelectedText()
	if !strings.Contains(selected, "wrapped assistant content") || !strings.Contains(selected, "second") {
		t.Fatalf("expected dragged selection to include assistant and trailing user block, got %q", selected)
	}
}

func TestModelDoubleClickExpandsCollapsedBlocks(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := updated.(Model)

	updated, _ = m.Update(appEventsMsg{
		{
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
		},
	})
	m = updated.(Model)

	headerH, _, bodyH := m.layoutHeights()
	row := -1
	for i := 0; i < bodyH; i++ {
		if got, ok := m.transcript.BlockIDAtRow(i); ok && got == "bash1" {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatal("expected tool block row")
	}

	click := tea.MouseClickMsg{X: 1, Y: headerH + row, Button: tea.MouseLeft}
	updated, _ = m.Update(click)
	m = updated.(Model)
	updated, _ = m.Update(click)
	m = updated.(Model)

	view := m.transcript.View()
	if !strings.Contains(view, "Double-click to collapse.") {
		t.Fatalf("expected double click to expand collapsed block, got %q", view)
	}
}

func TestModelThemeSwitchKeepsSessionConversation(t *testing.T) {
	t.Setenv("LUC_STATE_DIR", t.TempDir())

	root := newExecProviderWorkspace(t, `#!/bin/sh
cat >/dev/null
printf '%s\n' '{"type":"text_delta","text":"assistant response"}'
printf '%s\n' '{"type":"done","completed":true}'
`)

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = updated.(Model)
	if err := controller.Submit(context.Background(), "hello from user"); err != nil {
		t.Fatal(err)
	}

	updated, _ = model.Update(appEventsMsg(drainControllerEvents(controller.Events())))
	m := updated.(Model)
	before := ansi.Strip(m.View().Content)
	for _, want := range []string{"hello from user", "assistant response"} {
		if !strings.Contains(before, want) {
			t.Fatalf("expected %q in transcript before theme switch, got %q", want, before)
		}
	}

	m.applyTheme("dark")
	after := ansi.Strip(m.View().Content)
	for _, want := range []string{"hello from user", "assistant response"} {
		if !strings.Contains(after, want) {
			t.Fatalf("expected %q in transcript after theme switch, got %q", want, after)
		}
	}
}

func TestModelFooterHintsOnlyShowStopWhileTurnActive(t *testing.T) {
	t.Setenv("LUC_STATE_DIR", t.TempDir())

	root := newExecProviderWorkspace(t, `#!/bin/sh
cat >/dev/null
sleep 30
printf '%s\n' '{"type":"done","completed":true}'
`)

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m := updated.(Model)

	if hints := ansi.Strip(m.renderFooterHints()); !strings.Contains(hints, "esc clear") {
		t.Fatalf("expected clear hint, got %q", hints)
	} else if !strings.Contains(hints, "ctrl+p") {
		t.Fatalf("expected command palette hint, got %q", hints)
	} else if strings.Contains(hints, "ctrl+. stop") {
		t.Fatalf("expected stop hint to stay hidden while idle, got %q", hints)
	}

	done := make(chan error, 1)
	go func() {
		done <- controller.Submit(context.Background(), "long running")
	}()
	waitForTurnState(t, controller, true)

	m.invalidateFooter()
	if hints := ansi.Strip(m.renderFooterHints()); !strings.Contains(hints, "ctrl+. stop") {
		t.Fatalf("expected stop hint while active, got %q", hints)
	} else if !strings.Contains(hints, "ctrl+p") {
		t.Fatalf("expected command palette hint while active, got %q", hints)
	}

	if !controller.CancelTurn() {
		t.Fatal("expected cancellation to succeed")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected canceled submit to return nil, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for canceled submit")
	}

	m.invalidateFooter()
	if hints := ansi.Strip(m.renderFooterHints()); strings.Contains(hints, "ctrl+. stop") {
		t.Fatalf("expected stop hint to hide after cancellation, got %q", hints)
	}
}

func newExecProviderWorkspace(t *testing.T, script string) string {
	t.Helper()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".luc", "providers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "config.yaml"), []byte(`provider:
  kind: theme-test
  model: local-model
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "providers", "theme-test.yaml"), []byte(`id: theme-test
name: Theme Test
type: exec
command: ./provider.sh
models:
  - id: local-model
    name: Local Model
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "providers", "provider.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func drainControllerEvents(ch <-chan history.EventEnvelope) []history.EventEnvelope {
	var events []history.EventEnvelope
	for {
		select {
		case ev := <-ch:
			events = append(events, ev)
		default:
			return events
		}
	}
}

func waitForTurnState(t *testing.T, controller *kernel.Controller, active bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for controller.TurnActive() != active {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for turn active=%t", active)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
