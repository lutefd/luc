package tui

import (
	"context"
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

func mustWriteRuntimeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
