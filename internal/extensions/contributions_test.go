package extensions

import (
	"os"
	"path/filepath"
	"strings"
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
    description: Show provider health details.
    category: Provider
    shortcut: ctrl+shift+p
    action:
      kind: view.open
      view_id: provider.status
  - id: review.approve
    name: Approve Review
    action:
      kind: tool.run
      tool_name: review_set_state
      arguments:
        action: approve
      result:
        presentation: status
views:
  - id: provider.status
    title: Global Provider Status
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
    actions:
      - id: refresh
        label: Refresh
        action:
          kind: session.handoff
          title: Start implementation
          handoff:
            body: Approved changes.
            render: markdown
          initial_input: Implement the approved changes.
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

	command, ok := set.UI.Command("provider.status.open")
	if !ok {
		t.Fatal("expected merged runtime command")
	}
	if command.Description != "Show provider health details." || command.Category != "Provider" || command.Shortcut != "ctrl+shift+p" {
		t.Fatalf("expected runtime command metadata, got %#v", command)
	}
	toolCommand, ok := set.UI.Command("review.approve")
	if !ok {
		t.Fatal("expected tool.run runtime command")
	}
	if toolCommand.ActionKind != "tool.run" || toolCommand.ToolName != "review_set_state" || toolCommand.Arguments["action"] != "approve" || toolCommand.Result.Presentation != "status" {
		t.Fatalf("expected tool.run action metadata, got %#v", toolCommand)
	}

	view, ok := set.UI.View("provider.status")
	if !ok {
		t.Fatal("expected merged runtime view")
	}
	if view.Title != "Package Provider Status" || view.Placement != "page" {
		t.Fatalf("expected package override for view, got %#v", view)
	}
	if len(view.Actions) != 1 || view.Actions[0].ID != "refresh" || view.Actions[0].Action.Kind != "session.handoff" || view.Actions[0].Action.InitialInput != "Implement the approved changes." {
		t.Fatalf("expected package override for view actions, got %#v", view.Actions)
	}
	policy, ok := set.UI.ApprovalPolicyForTool("bash")
	if !ok {
		t.Fatal("expected approval policy")
	}
	if policy.Mode != "confirm" || policy.Title != "Run shell command?" {
		t.Fatalf("expected project override for policy, got %#v", policy)
	}
}

func TestLoadRuntimeContributionsReportsRuntimeCommandShortcutCollisions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()

	mustWriteRuntimeManifest(t, filepath.Join(home, ".luc", "ui", "global.yaml"), `schema: luc.ui/v1
id: global-ui
commands:
  - id: global.review
    name: Global Review
    shortcut: ctrl+shift+r
    action:
      kind: view.open
      view_id: review
`)
	mustWriteRuntimeManifest(t, filepath.Join(root, ".luc", "ui", "project.yaml"), `schema: luc.ui/v1
id: project-ui
commands:
  - id: project.review
    name: Project Review
    shortcut: ctrl+shift+r
    action:
      kind: view.open
      view_id: review
  - id: project.reload
    name: Runtime Reload Conflict
    shortcut: ctrl+r
    action:
      kind: view.open
      view_id: review
`)

	set, err := LoadRuntimeContributions(root, luruntime.DefaultHostCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Diagnostics) != 2 {
		t.Fatalf("expected shortcut diagnostics, got %#v", set.Diagnostics)
	}
	messages := set.Diagnostics[0].Message + "\n" + set.Diagnostics[1].Message
	if !strings.Contains(messages, "conflicts with runtime command") || !strings.Contains(messages, "conflicts with a built-in shortcut") {
		t.Fatalf("expected runtime and built-in shortcut collision diagnostics, got %q", messages)
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

func TestLoadRuntimeContributionsLoadsExtensionHostsWithPackagePrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()

	mustWriteRuntimeManifest(t, filepath.Join(home, ".luc", "extensions", "global.yaml"), `schema: luc.extension/v1
id: audit
protocol_version: 1
runtime:
  kind: exec
  command: ./global-host.py
subscriptions:
  - event: message.assistant.final
`)
	mustWriteRuntimeManifest(t, filepath.Join(root, ".luc", "packages", "audit@1.0.0", "extensions", "package.yaml"), `schema: luc.extension/v1
id: audit
protocol_version: 1
runtime:
  kind: exec
  command: ./package-host.py
  args: [--jsonl]
subscriptions:
  - event: message.assistant.final
  - event: compaction.completed
`)

	set, err := LoadRuntimeContributions(root, luruntime.DefaultHostCapabilities())
	if err != nil {
		t.Fatal(err)
	}

	hosts := set.Extensions.Hosts()
	if len(hosts) != 1 {
		t.Fatalf("expected one extension host, got %#v", hosts)
	}
	if hosts[0].Runtime.Command != "./package-host.py" || len(hosts[0].Subscriptions) != 2 {
		t.Fatalf("expected package override for extension host, got %#v", hosts[0])
	}
}

func TestLoadRuntimeContributionsAcceptsSyncExtensionSubscriptions(t *testing.T) {
	root := t.TempDir()
	mustWriteRuntimeManifest(t, filepath.Join(root, ".luc", "extensions", "sync.yaml"), `schema: luc.extension/v1
id: sync
protocol_version: 1
runtime:
  kind: exec
  command: ./host.py
subscriptions:
  - event: input.transform
    mode: sync
    timeout_ms: 250
    failure_mode: closed
  - event: prompt.context
    mode: sync
`)

	set, err := LoadRuntimeContributions(root, luruntime.DefaultHostCapabilities())
	if err != nil {
		t.Fatal(err)
	}

	bindings := set.Extensions.SyncSubscribers("input.transform")
	if len(bindings) != 1 {
		t.Fatalf("expected one sync subscriber, got %#v", bindings)
	}
	if bindings[0].Subscription.FailureMode != luruntime.ExtensionFailureModeClosed || bindings[0].Subscription.TimeoutMS != 250 {
		t.Fatalf("unexpected sync subscription %#v", bindings[0])
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
