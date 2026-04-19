package extensions

import (
	"os"
	"path/filepath"
	"testing"

	luruntime "github.com/lutefd/luc/internal/runtime"
)

func TestLoadRuntimeContributionsMergesGlobalPackageAndProjectUI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()

	mustWriteRuntimeManifest(t, filepath.Join(home, ".luc", "ui", "global.yaml"), `schema: luc.ui/v1
id: global-ui
commands:
  - id: provider.status.open
    name: Open provider status
    action:
      kind: view.open
      view_id: provider.status
views:
  - id: provider.status
    title: Global Provider Status
    placement: inspector_tab
    source_tool: provider_status
    render: markdown
approval_policies:
  - id: guarded-bash
    tool_names: [bash]
    mode: deny
`)
	mustWriteRuntimeManifest(t, filepath.Join(root, ".luc", "packages", "provider-console@1.0.0", "ui", "package.yaml"), `schema: luc.ui/v1
id: package-ui
views:
  - id: provider.status
    title: Package Provider Status
    placement: page
    source_tool: provider_status
    render: json
`)
	mustWriteRuntimeManifest(t, filepath.Join(root, ".luc", "ui", "project.yaml"), `schema: luc.ui/v1
id: project-ui
approval_policies:
  - id: guarded-bash
    tool_names: [bash]
    mode: confirm
    title: Run shell command?
`)

	set, err := LoadRuntimeContributions(root, luruntime.DefaultHostCapabilities())
	if err != nil {
		t.Fatal(err)
	}

	view, ok := set.UI.View("provider.status")
	if !ok {
		t.Fatal("expected merged runtime view")
	}
	if view.Title != "Package Provider Status" || view.Placement != "page" {
		t.Fatalf("expected package override for view, got %#v", view)
	}
	policy, ok := set.UI.ApprovalPolicyForTool("bash")
	if !ok {
		t.Fatal("expected approval policy")
	}
	if policy.Mode != "confirm" || policy.Title != "Run shell command?" {
		t.Fatalf("expected project override for policy, got %#v", policy)
	}
}

func TestLoadRuntimeContributionsSkipsRequirementGatedManifestsWithDiagnostics(t *testing.T) {
	root := t.TempDir()
	mustWriteRuntimeManifest(t, filepath.Join(root, ".luc", "ui", "gated.yaml"), `schema: luc.ui/v1
id: gated-ui
requires_host_capabilities:
  - ui.view.open
commands:
  - id: open
    name: Open
    action:
      kind: view.open
      view_id: something
`)

	set, err := LoadRuntimeContributions(root, []string{luruntime.HostCapabilityUIConfirm})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.UI.Commands()) != 0 {
		t.Fatalf("expected gated ui contribution to be skipped, got %#v", set.UI.Commands())
	}
	if len(set.Diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %#v", set.Diagnostics)
	}
}

func TestLoadRuntimeContributionsLoadsHookSubscriptionsWithPackagePrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()

	mustWriteRuntimeManifest(t, filepath.Join(home, ".luc", "hooks", "global.yaml"), `schema: luc.hook/v1
id: slack_notify
description: Global hook
events:
  - message.assistant.final
runtime:
  kind: exec
  command: ./notify-global.py
delivery:
  mode: async
  timeout_seconds: 10
`)
	mustWriteRuntimeManifest(t, filepath.Join(root, ".luc", "packages", "slack@1.0.0", "hooks", "package.yaml"), `schema: luc.hook/v1
id: slack_notify
description: Package hook
events:
  - message.assistant.final
runtime:
  kind: exec
  command: ./notify-package.py
  capabilities:
    - structured_io
delivery:
  mode: async
  timeout_seconds: 5
`)

	set, err := LoadRuntimeContributions(root, luruntime.DefaultHostCapabilities())
	if err != nil {
		t.Fatal(err)
	}

	hooks := set.Hooks.Subscribers("message.assistant.final")
	if len(hooks) != 1 {
		t.Fatalf("expected one hook subscriber, got %#v", hooks)
	}
	if hooks[0].Runtime.Command != "./notify-package.py" || hooks[0].Delivery.TimeoutSeconds != 5 {
		t.Fatalf("expected package hook override, got %#v", hooks[0])
	}
}

func mustWriteRuntimeManifest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
