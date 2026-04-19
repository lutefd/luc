# Capability-Enabled Tools

Use this pattern when the tool needs structured stdin/stdout, streaming events, or host-owned UI/client actions. Keep using the legacy shell manifest for simple one-shot shell commands.

Minimal manifest:

~~~yaml
schema: luc.tool/v1
name: provider_status
description: Show provider status.
runtime:
  kind: exec
  command: ./provider_status.sh
  capabilities:
    - structured_io
    - client_actions
input_schema:
  type: object
  properties:
    provider:
      type: string
  required: [provider]
timeout_seconds: 30
ui:
  default_collapsed: true
  collapsed_summary: Provider status captured for {{ .provider }}.
~~~

Rules:

- Use `runtime.kind: exec` for capability-enabled tools.
- `structured_io` means luc sends one JSON request envelope on stdin and expects JSONL events on stdout.
- `client_actions` allows the tool to ask the host to open views, request confirmation, or run host-owned commands.
- Supported tool `client_action.kind` values today are `modal.open`, `confirm.request`, `view.open`, `view.refresh`, and `command.run`.
- Prefer `input_schema` instead of legacy `schema` for `luc.tool/v1` tools.
- Use `ui.default_collapsed: true` when the tool would otherwise spam the transcript.

Structured request envelope includes:

- `tool_name`
- typed `arguments`
- `workspace`
- `session_id`
- `agent_id`
- `host_capabilities`
- optional `view_context`

Typical stdout event flow:

1. `progress` or `log` while work runs
2. optional `client_action` when the tool needs host UI
3. `result` with `result.content` and optional metadata
4. `done` to terminate the stream

Structured stdout field names are exact:

- `stdout`, `stderr`, and `progress` use `text`
- `client_action` uses `action`
- `result` uses `result`
- `error` uses `error`
- `done` should set `done: true`

When the tool needs a persistent inspector or page, also create a matching `luc.ui/v1` manifest instead of baking UI rules into the tool itself.
