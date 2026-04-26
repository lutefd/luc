package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lutefd/luc/internal/kernel"
	luruntime "github.com/lutefd/luc/internal/runtime"
)

func TestModelRegistersRuntimeCommandsFromUIManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "ui", "provider.yaml"), `schema: luc.ui/v1
id: provider-tools
commands:
  - id: provider.status.open
    name: Open provider status
    description: Show provider health details.
    category: Provider
    shortcut: ctrl+shift+p
    action:
      kind: view.open
      view_id: provider.status
views:
  - id: provider.status
    title: Provider Status
    placement: inspector_tab
    source_tool: provider_status
    render: markdown
`)

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	found := false
	for _, command := range model.registry.All() {
		if command.ID == "provider.status.open" {
			found = command.Description == "Show provider health details." && command.Category == "Provider" && command.Shortcut == "ctrl+shift+p"
			break
		}
	}
	if !found {
		t.Fatalf("expected runtime command in palette, got %#v", model.registry.All())
	}
}

func TestModelDispatchesRuntimeCommandShortcut(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "tools", "provider_status.yaml"), `name: provider_status
description: Show provider status.
command: printf 'shortcut ok'
schema:
  type: object
  properties: {}
`)
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "ui", "provider.yaml"), `schema: luc.ui/v1
id: provider-tools
commands:
  - id: provider.status.open
    name: Open provider status
    shortcut: ctrl+shift+r
    action:
      kind: view.open
      view_id: provider.status
views:
  - id: provider.status
    title: Provider Status
    placement: inspector_tab
    source_tool: provider_status
    render: markdown
`)

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m := updated.(Model)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl | tea.ModShift})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected runtime shortcut to load view")
	}
	m.inspector.SetSize(48, 18)
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if view := ansi.Strip(m.inspector.DetailView()); !strings.Contains(view, "shortcut ok") {
		t.Fatalf("expected runtime inspector content, got %q", view)
	}
}

func TestModelRendersRuntimeTimelineNoteMarkdown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "ui", "timeline.yaml"), `schema: luc.ui/v1
id: timeline-ui
commands:
  - id: plan.updated
    name: Plan updated
    action:
      kind: timeline.note
      title: Updated Plan
      body: |
        ### Updated Plan

        - [x] Inspect shell environment and confirm basic repo state
      render: markdown
`)

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m := updated.(Model)
	updated, cmd := m.Update(runRuntimeCommandMsg{CommandID: "plan.updated"})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected timeline note command")
	}
	updated, cmd = m.Update(cmd())
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected timeline note event watcher")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	transcript := ansi.Strip(m.transcript.View())
	if strings.Contains(transcript, "### Updated Plan") {
		t.Fatalf("expected markdown heading marker to be styled away, got %q", transcript)
	}
	if !strings.Contains(transcript, "Updated Plan") || !strings.Contains(transcript, "Inspect shell environment") {
		t.Fatalf("expected rendered timeline note in transcript, got %q", transcript)
	}
}

func TestModelRunsRuntimeTimelineNoteAction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "ui", "timeline.yaml"), `schema: luc.ui/v1
id: timeline-ui
commands:
  - id: review.approved
    name: Review approved
    action:
      kind: timeline.note
      title: Review approved
      body: Ready for implementation.
      render: markdown
`)

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m := updated.(Model)
	updated, cmd := m.Update(runRuntimeCommandMsg{CommandID: "review.approved"})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected timeline note command")
	}
	updated, cmd = m.Update(cmd())
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected timeline note event watcher")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.status != "Timeline note added" {
		t.Fatalf("expected timeline status, got %q", m.status)
	}
	if transcript := ansi.Strip(m.transcript.View()); !strings.Contains(transcript, "Review approved") || !strings.Contains(transcript, "Ready for implementation.") {
		t.Fatalf("expected timeline note in transcript, got %q", transcript)
	}
}

func TestModelRunsRuntimeSessionHandoffAction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "tools", "review_summary.yaml"), `name: review_summary
description: Show review summary.
command: printf 'approved review'
schema:
  type: object
  properties: {}
`)
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "ui", "review.yaml"), `schema: luc.ui/v1
id: review-ui
commands:
  - id: review.open
    name: Open review
    action:
      kind: view.open
      view_id: review.summary
views:
  - id: review.summary
    title: Review Summary
    placement: page
    source_tool: review_summary
    render: markdown
    actions:
      - id: implement
        label: Implement
        action:
          kind: session.handoff
          title: Start implementation
          handoff:
            title: Approved Review
            body: approved context
            render: markdown
          initial_input: Implement the approved changes.
`)

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	oldSession := controller.Session().SessionID

	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m := updated.(Model)
	updated, cmd := m.Update(runRuntimeCommandMsg{CommandID: "review.open"})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected runtime page open command")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected session handoff command")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if controller.Session().SessionID == oldSession {
		t.Fatal("expected session handoff to create a new session")
	}
	if got := m.input.Value(); got != "Implement the approved changes." {
		t.Fatalf("expected handoff initial input in composer, got %q", got)
	}
	if events := controller.SessionEvents(); len(events) != 1 || events[0].Kind != "session.handoff" {
		t.Fatalf("expected visible session handoff event, got %#v", events)
	}
}

func TestModelRunsRuntimeInspectorViewAction(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "tools", "provider_status.yaml"), `name: provider_status
description: Show provider status.
command: printf 'provider ok'
schema:
  type: object
  properties: {}
`)
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "tools", "review_set_state.yaml"), `name: review_set_state
description: Set review state.
command: printf 'approved'
schema:
  type: object
  properties:
    action:
      type: string
`)
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "ui", "provider.yaml"), `schema: luc.ui/v1
id: provider-tools
commands:
  - id: provider.status.open
    name: Open provider status
    action:
      kind: view.open
      view_id: provider.status
views:
  - id: provider.status
    title: Provider Status
    placement: inspector_tab
    source_tool: provider_status
    render: markdown
    actions:
      - id: approve
        label: Approve
        shortcut: a
        action:
          kind: tool.run
          tool_name: review_set_state
          arguments:
            action: approve
          result:
            presentation: status
`)

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m := updated.(Model)
	updated, cmd := m.Update(runRuntimeCommandMsg{CommandID: "provider.status.open"})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected runtime view load command")
	}
	m.inspector.SetSize(48, 18)
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if view := ansi.Strip(m.inspector.DetailView()); !strings.Contains(view, "Approve") {
		t.Fatalf("expected runtime inspector action, got %q", view)
	}
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected runtime inspector action command")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.status != "Tool finished: review_set_state" {
		t.Fatalf("expected status presentation, got %q", m.status)
	}
}

func TestModelOpensRuntimeInspectorViewAndRendersSourceTool(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "tools", "provider_status.yaml"), `name: provider_status
description: Show provider status.
command: printf 'provider ok'
schema:
  type: object
  properties: {}
`)
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "ui", "provider.yaml"), `schema: luc.ui/v1
id: provider-tools
commands:
  - id: provider.status.open
    name: Open provider status
    action:
      kind: view.open
      view_id: provider.status
views:
  - id: provider.status
    title: Provider Status
    placement: inspector_tab
    source_tool: provider_status
    render: markdown
`)

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m := updated.(Model)
	updated, cmd := m.Update(runRuntimeCommandMsg{CommandID: "provider.status.open"})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected runtime view load command")
	}
	m.inspector.SetSize(48, 18)
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if view := ansi.Strip(m.inspector.DetailView()); !strings.Contains(view, "provider ok") {
		t.Fatalf("expected runtime inspector content, got %q", view)
	}
}

func TestModelRunsRuntimeToolActionFromCommand(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "tools", "review_set_state.yaml"), `name: review_set_state
description: Set review state.
command: printf 'review approved'
schema:
  type: object
  properties:
    action:
      type: string
`)
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "ui", "review.yaml"), `schema: luc.ui/v1
id: review-tools
commands:
  - id: review.approve
    name: Approve Review
    action:
      kind: tool.run
      tool_name: review_set_state
      arguments:
        action: approve
      result:
        presentation: status
`)

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m := updated.(Model)
	updated, cmd := m.Update(runRuntimeCommandMsg{CommandID: "review.approve"})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected runtime tool action command")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.status != "Tool finished: review_set_state" {
		t.Fatalf("expected status presentation, got %q", m.status)
	}
	var found bool
	for _, ev := range controller.SessionEvents() {
		if ev.Kind == "tool.finished" && strings.Contains(fmt.Sprint(ev.Payload), "review approved") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tool.finished event for runtime tool action, got %#v", controller.SessionEvents())
	}
}

func TestModelRunsRuntimePageViewAction(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "tools", "provider_status.yaml"), `name: provider_status
description: Show provider status.
command: printf '{"status":"ok"}'
schema:
  type: object
  properties: {}
`)
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "tools", "review_set_state.yaml"), `name: review_set_state
description: Set review state.
command: printf 'approved'
schema:
  type: object
  properties:
    action:
      type: string
`)
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "ui", "provider.yaml"), `schema: luc.ui/v1
id: provider-tools
commands:
  - id: provider.status.page
    name: Open provider page
    action:
      kind: view.open
      view_id: provider.status
views:
  - id: provider.status
    title: Provider Status
    placement: page
    source_tool: provider_status
    render: json
    actions:
      - id: approve
        label: Approve
        action:
          kind: tool.run
          tool_name: review_set_state
          arguments:
            action: approve
          result:
            presentation: status
`)

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m := updated.(Model)
	updated, cmd := m.Update(runRuntimeCommandMsg{CommandID: "provider.status.page"})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected runtime page open command")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if rendered := m.renderRuntimePage(); !strings.Contains(rendered, "Approve") {
		t.Fatalf("expected runtime page action, got %q", rendered)
	}
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected runtime page action command")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.status != "Tool finished: review_set_state" {
		t.Fatalf("expected status presentation, got %q", m.status)
	}
}

func TestModelOpensAndClosesRuntimePageView(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "tools", "provider_status.yaml"), `name: provider_status
description: Show provider status.
command: printf '{"status":"ok"}'
schema:
  type: object
  properties: {}
`)
	mustWriteRuntimeFile(t, filepath.Join(root, ".luc", "ui", "provider.yaml"), `schema: luc.ui/v1
id: provider-tools
commands:
  - id: provider.status.page
    name: Open provider page
    action:
      kind: view.open
      view_id: provider.status
views:
  - id: provider.status
    title: Provider Status
    placement: page
    source_tool: provider_status
    render: json
`)

	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m := updated.(Model)
	updated, cmd := m.Update(runRuntimeCommandMsg{CommandID: "provider.status.page"})
	m = updated.(Model)
	if !m.runtimePage.open || cmd == nil {
		t.Fatalf("expected runtime page to open, page=%#v cmd=%v", m.runtimePage, cmd)
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if !strings.Contains(m.renderRuntimePage(), "\"status\": \"ok\"") {
		t.Fatalf("expected rendered runtime page content, got %q", m.renderRuntimePage())
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)
	if m.runtimePage.open {
		t.Fatal("expected runtime page to close on escape")
	}
}

func TestModelHandlesBlockingRuntimeConfirmDialog(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	m := updated.(Model)

	response := make(chan uiBrokerResponse, 1)
	updated, _ = m.Update(uiBrokerActionMsg{
		request: uiBrokerRequest{
			action: luruntime.UIAction{
				ID:       "confirm_1",
				Kind:     "confirm.request",
				Blocking: true,
				Title:    "Run shell command?",
				Body:     "printf hello",
				Options: []luruntime.UIOption{
					{ID: "run", Label: "Run", Primary: true},
					{ID: "cancel", Label: "Cancel"},
				},
			},
			response: response,
		},
	})
	m = updated.(Model)
	if !m.runtimeDialog.open {
		t.Fatal("expected runtime dialog to open")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.runtimeDialog.open {
		t.Fatal("expected runtime dialog to close after confirmation")
	}
	reply := <-response
	if !reply.result.Accepted || reply.result.ChoiceID != "run" {
		t.Fatalf("unexpected runtime dialog result %#v", reply.result)
	}
}

func TestModelHandlesRichRuntimeModalWithMarkdownChoicesAndInput(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	m := updated.(Model)

	response := make(chan uiBrokerResponse, 1)
	updated, _ = m.Update(uiBrokerActionMsg{
		request: uiBrokerRequest{
			action: luruntime.UIAction{
				ID:     "review_1",
				Kind:   "modal.open",
				Title:  "Review Result",
				Body:   "## Summary\n\nApprove these changes?",
				Render: "markdown",
				Options: []luruntime.UIOption{
					{ID: "approve", Label: "Approve"},
					{ID: "revise", Label: "Revise"},
					{ID: "cancel", Label: "Cancel"},
				},
				Input: luruntime.UIActionInput{Enabled: true, Multiline: true, Placeholder: "Revision notes"},
			},
			response: response,
		},
	})
	m = updated.(Model)
	if !m.runtimeDialog.open {
		t.Fatal("expected rich runtime modal to open")
	}
	if rendered := ansi.Strip(m.renderRuntimeDialog()); !strings.Contains(rendered, "Summary") || !strings.Contains(rendered, "Approve these changes?") || !strings.Contains(rendered, "Revision notes") {
		t.Fatalf("expected markdown modal body and input placeholder, got %q", rendered)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(Model)
	for _, r := range "Please simplify step 3." {
		updated, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = updated.(Model)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.runtimeDialog.open {
		t.Fatal("expected rich runtime modal to close after choice")
	}
	reply := <-response
	if !reply.result.Accepted || reply.result.ChoiceID != "revise" || reply.result.Data["input"] != "Please simplify step 3." {
		t.Fatalf("unexpected rich modal result %#v", reply.result)
	}
}

func TestRuntimeDialogMarkdownBodyScrolls(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 18})
	m := updated.(Model)
	body := "# Long note\n\n" + strings.Join([]string{"line 1", "line 2", "line 3", "line 4", "line 5", "line 6", "line 7", "line 8", "line 9", "line 10", "line 11", "line 12"}, "\n\n")
	updated, _ = m.Update(uiBrokerActionMsg{request: uiBrokerRequest{action: luruntime.UIAction{ID: "long", Kind: "modal.open", Title: "Long", Body: body, Render: "markdown"}}})
	m = updated.(Model)
	if !m.runtimeDialog.open {
		t.Fatal("expected runtime modal to open")
	}
	before := ansi.Strip(m.renderRuntimeDialog())
	if strings.Contains(before, "line 12") {
		t.Fatalf("expected long body to be clipped before scrolling, got %q", before)
	}
	for i := 0; i < 20; i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		m = updated.(Model)
	}
	after := ansi.Strip(m.renderRuntimeDialog())
	if !strings.Contains(after, "line 12") {
		t.Fatalf("expected long body to scroll, got %q", after)
	}
}

func TestRuntimeDialogChoiceListScrollsIndependently(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 18})
	m := updated.(Model)
	options := make([]luruntime.UIOption, 12)
	for i := range options {
		options[i] = luruntime.UIOption{ID: fmt.Sprintf("choice-%02d", i+1), Label: fmt.Sprintf("Choice %02d", i+1)}
	}
	updated, _ = m.Update(uiBrokerActionMsg{request: uiBrokerRequest{action: luruntime.UIAction{ID: "choices", Kind: "modal.open", Title: "Choices", Body: "Pick one", Options: options}}})
	m = updated.(Model)
	if !m.runtimeDialog.open {
		t.Fatal("expected runtime modal to open")
	}
	before := ansi.Strip(m.renderRuntimeDialog())
	if !strings.Contains(before, "Choice 01") || strings.Contains(before, "Choice 12") {
		t.Fatalf("expected first choices only before scrolling, got %q", before)
	}
	for i := 0; i < 11; i++ {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m = updated.(Model)
	}
	after := ansi.Strip(m.renderRuntimeDialog())
	if !strings.Contains(after, "Choice 12") || strings.Contains(after, "Choice 01") {
		t.Fatalf("expected choice list to scroll independently, got %q", after)
	}
}

func TestRuntimeDialogMouseWheelScrollsBodyContent(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	controller, err := kernel.New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	model := New(controller)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 18})
	m := updated.(Model)
	body := "# Long note\n\n" + strings.Join([]string{"line 1", "line 2", "line 3", "line 4", "line 5", "line 6", "line 7", "line 8", "line 9", "line 10", "line 11", "line 12"}, "\n\n")
	updated, _ = m.Update(uiBrokerActionMsg{request: uiBrokerRequest{action: luruntime.UIAction{ID: "long-mouse", Kind: "modal.open", Title: "Long", Body: body, Render: "markdown"}}})
	m = updated.(Model)
	before := ansi.Strip(m.renderRuntimeDialog())
	if strings.Contains(before, "line 12") {
		t.Fatalf("expected long body to be clipped before scrolling, got %q", before)
	}
	for i := 0; i < 20; i++ {
		updated, _ = m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		m = updated.(Model)
	}
	after := ansi.Strip(m.renderRuntimeDialog())
	if !strings.Contains(after, "line 12") {
		t.Fatalf("expected mouse wheel to scroll modal body, got %q", after)
	}
}

func mustWriteRuntimeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
