# Runtime UI Actions

Runtime UI actions are host-owned. Tools, providers, hooks, and extension hosts can request them through `client_action` events, and `luc.ui/v1` command manifests can trigger some of the same actions declaratively.

Runtime `commands` are registered into luc's command palette alongside the built-in commands.

Supported action kinds in this slice:

- `modal.open`
- `confirm.request`
- `view.open`
- `view.refresh`
- `command.run`
- `tool.run`
- `session.handoff`

When to use each action:

- Use `modal.open` for a host-rendered modal dialog. It may include `render: markdown`, multiple `options`, and optional text `input`.
- Use `confirm.request` when the tool, provider, or hook needs an explicit user decision.
- Use `view.open` to open a runtime view declared in `luc.ui/v1`.
- Use `view.refresh` to rerun the active runtime view's `source_tool`.
- Use `command.run` to trigger another registered runtime command by ID.
- Use `tool.run` to execute an extension tool through luc's normal tool pipeline, including approval policies and extension preflight/result hooks.
- Use `session.handoff` to ask the host to create and switch to a fresh session carrying structured workflow context and optional initial composer text.

Rich blocking modal example from a structured tool or provider:

```json
{
	"type": "client_action",
	"action": {
		"id": "review_1",
		"kind": "modal.open",
		"blocking": true,
		"title": "Review Result",
		"body": "## Summary\n\nApprove these changes?",
		"render": "markdown",
		"options": [
			{ "id": "approve", "label": "Approve" },
			{ "id": "revise", "label": "Revise" },
			{ "id": "cancel", "label": "Cancel" }
		],
		"input": {
			"enabled": true,
			"multiline": true,
			"placeholder": "Revision notes"
		}
	}
}
```

Blocking response shape:

```json
{
	"type": "client_result",
	"action_id": "review_1",
	"choice_id": "revise",
	"data": {
		"input": "Please simplify step 3."
	}
}
```

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

Start a fresh continuation session from a command manifest:

```yaml
commands:
  - id: review.implement
    name: Implement Approved Review
    action:
      kind: session.handoff
      title: Start implementation
      handoff:
        title: Approved Review
        body: |
          ## Approved context

          Carry this review summary into the implementation session.
        render: markdown
      initial_input: Implement the approved changes.
```

Run a tool from a command manifest:

```yaml
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
```

Rules:

- Keep view definitions in `luc.ui/v1`; use actions to open or refresh them.
- Use `confirm.request` instead of inventing your own approval UI.
- Use `modal.open` only for host-owned modal content; supported rich fields are `render: markdown`, `options`, and `input` (`enabled`, `multiline`, `placeholder`, `value`).
- `modal.open` and `confirm.request` use the host's built-in dialog surface rather than arbitrary custom TUI layouts.
- If the flow depends on the user response, mark the action as blocking and expect a `client_result` envelope back over stdin/stdout.
- `session.handoff` is host-owned: extensions request it, but luc owns session creation, navigation, persistence, and composer seeding. It does not silently submit the initial input.
