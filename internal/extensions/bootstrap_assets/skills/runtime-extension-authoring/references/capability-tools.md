# Capability-Enabled Tools

Use this pattern when the tool needs structured stdin/stdout, streaming events, host-owned UI/client actions, or a stateful hosted handler behind a long-lived extension host. Keep using the legacy shell manifest for simple one-shot shell commands.

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
- Supported tool `client_action.kind` values today are `modal.open`, `confirm.request`, `view.open`, `view.refresh`, `command.run`, `tool.run`, `session.handoff`, and `timeline.note`. `session.handoff` must be blocking when emitted as a client action.
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

Hosted tool variant:

~~~yaml
schema: luc.tool/v2
name: stateful_echo
description: Echo through the audit extension host.
runtime:
  kind: extension
  extension_id: audit
  handler: echo
input_schema:
  type: object
  properties:
    text:
      type: string
  required: [text]
~~~

Hosted tool rules:

- Use `runtime.kind: extension` when the tool should run inside a long-lived `luc.extension/v1` host and share its session-local state.
- Hosted tools should usually be declared with `luc.tool/v2`. Extension hosts may register dynamic tools only for advanced integrations where the tool catalog is discovered at runtime, such as MCP adapters. Dynamic tools require host capability `tools.dynamic`, are session-scoped, are owned by the registering extension host, use source `dynamic:<extension-id>`, and cannot replace existing built-in or manifest-declared tools.
- Hosted invocation is sent to the host as `tool_invoke` with `request_id`, `handler`, and the normal tool request envelope nested under `tool`.
- The host answers with `tool_result` carrying the same normalized result shape luc already uses: `content`, optional `metadata`, `default_collapsed`, and `collapsed_summary`.
- Hosted tools may emit `client_action` and receive `client_result` the same way structured exec tools do.
- Prefer hosted tools when the capability needs persistent process state across invocations, or when the same extension host already owns related sync seams such as `tool.preflight` or `tool.result`.
