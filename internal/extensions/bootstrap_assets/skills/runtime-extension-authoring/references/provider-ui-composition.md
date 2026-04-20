# Provider and UI Composition

Use this pattern when a capability spans an exec provider plus host-owned runtime UI.

Example exec provider:

~~~yaml
id: acme
name: Acme Gateway
type: exec
command: ./adapter.sh
args: [--stream]
capabilities:
  - client_actions
models:
  - id: gpt-5.4-2026-03-05
    name: GPT-5.4
~~~

Provider notes:

- `type: exec` providers receive one JSON request on stdin and emit JSONL provider events on stdout.
- Provider request envelopes include `request` and `host_capabilities`.
- Provider events can stream `thinking`, `text_delta`, `tool_call`, `client_action`, and `done`.
- `thinking` and `text_delta` use `text`; `tool_call` uses `tool_call` with `id`, `name`, and JSON-string `arguments`; `client_action` uses `action`; fatal adapter failures may also return an `error` string.
- If the provider declares `capabilities: [client_actions]`, luc includes `host_capabilities` in the request and the adapter may emit `client_action` events.
- Supported provider `client_action.kind` values today are `modal.open`, `confirm.request`, `view.open`, `view.refresh`, and `command.run`.
- The provider adapts upstream APIs. luc still owns tool execution and UI rendering.

Example matching UI manifest:

~~~yaml
schema: luc.ui/v1
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
approval_policies:
  - id: guarded-bash
    tool_names: [bash]
    mode: confirm
    title: Run shell command?
    body_template: "{{ index .arguments \"command\" }}"
    confirm_label: Run
    cancel_label: Cancel
~~~

Composition rules:

- Put protocol translation in the provider or tool, not in the UI manifest.
- Put reusable commands, views, and approval policies in `luc.ui/v1`.
- Use `view.open` or `view.refresh` for persistent host-owned views.
- If the workflow needs explicit confirmation, use approval policies or `confirm.request` client actions.
- Keep runtime views declarative and read-only in this slice.
