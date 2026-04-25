# Hook Patterns

Use hooks for async side effects driven by live luc events. Do not use hooks to mutate the active turn loop.

Example:

```yaml
schema: luc.hook/v1
id: slack_notify
description: Send a Slack ping when an assistant turn completes.
events:
  - message.assistant.final
runtime:
  kind: exec
  command: ./notify.sh
  capabilities:
    - structured_io
delivery:
  mode: async
  timeout_seconds: 10
```

Hook rules:

- Hooks subscribe to live events only; they do not run during history replay or session reopen.
- Stable hook subscription event kinds today are `message.assistant.final` and `tool.finished`.
- Hook request envelopes include the triggering event, workspace/session metadata, and `host_capabilities`. The current JSON shape is:

```json
{
	"event": {
		"kind": "tool.finished",
		"payload": { "id": "call_1", "name": "bash" }
	},
	"workspace": {
		"root": "/abs/workspace",
		"project_id": "repo",
		"branch": "main"
	},
	"session": {
		"session_id": "sess_123",
		"provider": "openai",
		"model": "gpt-5.4"
	},
	"host_capabilities": ["hooks.live_events"]
}
```

- Parseable stdout event types today are `log`, `progress`, `client_action`, `done`, and `error`.
- Hooks may also emit `client_action` when `runtime.capabilities` includes `client_actions`. That lets a hook request host-owned actions such as `view.refresh` after it updates state.
- Hook stdin starts with one JSON request envelope line. When `client_actions` is enabled, luc keeps stdin open and sends `client_result` envelopes back on later lines.
- Hook stdout field names are exact: `log` uses `text` (luc also accepts `message` as a compatibility alias), `progress` uses `progress` (or `message`), `client_action` uses `action`, `error` uses `error` (or `message`), and `done` should set `done: true`.
- Supported hook `client_action.kind` values today are `modal.open`, `confirm.request`, `view.open`, `view.refresh`, `command.run`, and `tool.run`.
- Hook failures should be surfaced as diagnostics, not treated as fatal session errors.
- When a hook needs interactive host UI, prefer moving that behavior into a tool or provider plus `luc.ui/v1` instead of stretching hook responsibilities.

When not to use hooks:

- If the user wants to transform input before it reaches the model, use `luc.extension/v1` with `input.transform`.
- If the user wants to inject prompt context synchronously, use `luc.extension/v1` with `prompt.context`.
- If the user wants to patch/block a tool call or patch a tool result before persistence, use `luc.extension/v1` with `tool.preflight` and/or `tool.result`.
- If the behavior needs shared in-process session state across multiple tool calls, prefer a hosted `luc.tool/v2` tool backed by a `luc.extension/v1` host.
