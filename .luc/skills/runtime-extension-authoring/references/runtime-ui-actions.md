# Runtime UI Actions

Runtime UI actions are host-owned. Tools, providers, hooks, and extension hosts can request them through `client_action` events, and `luc.ui/v1` command manifests can trigger some of the same actions declaratively.

Runtime `commands` are registered into luc's command palette alongside the built-in commands.

Supported action kinds in this slice:

- `modal.open`
- `confirm.request`
- `view.open`
- `view.refresh`
- `command.run`

When to use each action:

- Use `modal.open` for a host-rendered modal dialog.
- Use `confirm.request` when the tool, provider, or hook needs an explicit user decision.
- Use `view.open` to open a runtime view declared in `luc.ui/v1`.
- Use `view.refresh` to rerun the active runtime view's `source_tool`.
- Use `command.run` to trigger another registered runtime command by ID.

Blocking confirmation example from a structured tool or provider:

```json
{
	"type": "client_action",
	"action": {
		"id": "confirm_1",
		"kind": "confirm.request",
		"blocking": true,
		"title": "Reset activity counters?",
		"body": "This clears the current activity summary state.",
		"options": [
			{ "id": "reset", "label": "Reset", "primary": true },
			{ "id": "cancel", "label": "Cancel" }
		]
	}
}
```

Open a runtime view from a command manifest:

```yaml
schema: luc.ui/v1
id: activity-ui
commands:
  - id: activity.summary.open
    name: Open activity summary
    action:
      kind: view.open
      view_id: activity.summary
views:
  - id: activity.summary
    title: Activity Summary
    placement: inspector_tab
    source_tool: activity_summary
    render: json
```

Refresh the active runtime view from a command manifest:

```yaml
commands:
  - id: activity.summary.refresh
    name: Refresh activity summary
    action:
      kind: view.refresh
      view_id: activity.summary
```

Run another command from a command manifest:

```yaml
commands:
  - id: activity.summary.reset.confirm
    name: Reset activity summary
    action:
      kind: command.run
      command_id: activity.summary.reset
```

Rules:

- Keep view definitions in `luc.ui/v1`; use actions to open or refresh them.
- Use `confirm.request` instead of inventing your own approval UI.
- Use `modal.open` only for host-owned modal content; do not assume custom freeform modal rendering unless the host already supports the requested payload shape.
- `modal.open` and `confirm.request` currently use the host's built-in dialog surface rather than arbitrary custom TUI layouts.
- If the flow depends on the user response, mark the action as blocking and expect a `client_result` envelope back over stdin/stdout.
