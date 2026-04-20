# Runtime Extensions

`luc` can now load several extension types at runtime without recompiling.

On a clean install, first launch bootstraps the global `~/.luc` tree and seeds
the bundled helper skills `runtime-extension-authoring`, `skill-usage`, and
`theme-creator` there if they are missing. Existing user files are left
untouched.

Lookup order:

1. Global user layer: `~/.luc/...`
2. Installed package assets: `<workspace>/.luc/packages/*/...`
3. Project override layer: `<workspace>/.luc/...`

Later layers override earlier ones. Within the same layer, later lexicographic
manifest wins.

## Supported Runtime Surfaces

### Tools

Runtime tools live in:

- `~/.luc/tools`
- `<workspace>/.luc/tools`

Supported manifest formats:

- `.yaml`
- `.yml`
- `.json`

Example:

```yaml
name: repo_status
description: Print a compact git status summary.
command: git status --short
timeout_seconds: 10
schema:
  type: object
  properties: {}
ui:
  default_collapsed: true
  collapsed_summary: Repository status captured.
```

Capability-enabled tools can opt into structured stdio instead of the legacy
shell-only behavior:

```yaml
schema: luc.tool/v1
name: provider_status
description: Show provider status.
runtime:
  kind: exec
  command: ./.luc/tools/provider_status.sh
  capabilities:
    - structured_io
    - client_actions
input_schema:
  type: object
  properties: {}
timeout_seconds: 30
```

Tool capability notes:

- Omitting `runtime.capabilities` keeps today's legacy behavior.
- `structured_io` means luc writes a JSON request envelope to stdin and expects JSONL events on stdout.
- `client_actions` means the tool may emit `client_action` events and receive `client_result` responses over stdin/stdout.
- Structured request envelopes include `tool_name`, typed `arguments`, `workspace`, `session_id`, `agent_id`, `host_capabilities`, and optional `view_context`.
- Supported structured tool stdout event types today are `stdout`, `stderr`, `progress`, `client_action`, `result`, `done`, and `error`.
- Structured tool stdout field names are exact: `stdout`, `stderr`, and `progress` use `text`; `client_action` uses `action`; `result` uses `result`; `error` uses `error`; `done` should set `done: true`.
- Supported `client_action.kind` values today are `modal.open`, `confirm.request`, `view.open`, `view.refresh`, and `command.run`.
- Structured tools should emit a `result` event carrying the final tool payload, then emit `done` to terminate the stream.

Template variables available in `command` and `ui.collapsed_summary`:

- top-level args from the tool call JSON
- `.args`
- `.workspace`
- `.session_id`
- `.agent_id`
- `.command`
- `.output`
- `.timed_out`

Example with arguments:

```yaml
name: grep_todo
description: Search for TODO comments.
command: rg --line-number '{{ .pattern }}' '{{ .path }}'
schema:
  type: object
  properties:
    pattern:
      type: string
    path:
      type: string
  required: [pattern, path]
ui:
  default_collapsed: true
  collapsed_summary: Found TODO matches for {{ .pattern }} in {{ .path }}.
```

### Providers

Runtime providers live in:

- `~/.luc/providers`
- `<workspace>/.luc/providers`

Supported manifest formats:

- `.yaml`
- `.yml`
- `.json`

Providers currently support two runtime types:

- `openai-compatible` (default): static `base_url` + optional `api_key_env`
- `exec`: launch a local adapter command that speaks luc's provider JSON protocol over stdio

Example:

```yaml
id: openrouter
name: OpenRouter
base_url: https://openrouter.ai/api/v1
api_key_env: OPENROUTER_API_KEY
models:
  - id: openai/gpt-5
    name: GPT-5
    description: Routed through OpenRouter.
    context_k: 400
  - id: openai/gpt-5-thinking
    name: GPT-5 thinking
    description: Routed reasoning model.
    context_k: 400
    reasoning: true
```

For private gateways that do not require auth, omit `api_key_env` entirely:

```yaml
name: Local Gateway
base_url: http://localhost:8080/v1
models:
  - id: local-model
    name: Local Model
```

Notes:

- Provider `id` defaults to the manifest filename when omitted.
- Project manifests override global manifests with the same provider `id`.
- Manifest `base_url` and `api_key_env` define the runtime transport for that provider.

`exec` provider example:

```yaml
id: meli
name: Meli Gateway
type: exec
command: ./adapter.sh
args: [--stream]
env:
  GATEWAY_MODE: internal
models:
  - id: claude-opus-4-7
    name: Claude Opus 4.7
  - id: gpt-5.4-2026-03-05
    name: GPT-5.4
```

For `exec` providers:

- `command` is required.
- Relative command paths resolve from the provider manifest directory.
- The adapter receives one JSON request on stdin and emits JSONL provider events on stdout.
- Provider request envelopes include `request` and `host_capabilities`.
- Supported streamed event types today are `thinking`, `text_delta`, `tool_call`, `client_action`, and `done`.
- `thinking` and `text_delta` use `text`; `tool_call` uses `tool_call` with `id`, `name`, and JSON-string `arguments`; `client_action` uses `action`; fatal adapter failures may also return an `error` string.
- Tool execution still happens inside luc; the adapter only translates the upstream API into luc provider events.
- Providers may declare `capabilities: [client_actions]` to request host-owned UI actions and receive `client_result` responses back over stdin/stdout.
- Supported provider `client_action.kind` values today are `modal.open`, `confirm.request`, `view.open`, `view.refresh`, and `command.run`.
- Capability-enabled provider requests include `host_capabilities` alongside the normal provider request envelope.

### Runtime UI

Runtime UI manifests live in:

- `~/.luc/ui`
- `<workspace>/.luc/packages/*/ui`
- `<workspace>/.luc/ui`

Example:

```yaml
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
```

Supported runtime UI primitives in this slice:

- Command actions: `view.open`, `view.refresh`, `command.run`
- Client actions: `modal.open`, `confirm.request`, `view.open`, `view.refresh`, `command.run`
- View placements: `inspector_tab`, `page`
- View renderers: `markdown`, `json`, `table`, `kv`

Runtime UI notes:

- Runtime commands are registered into luc's command palette alongside the built-in commands.
- Runtime views are host-owned and read-only in this slice.
- A view's `source_tool` runs when the view opens or refreshes.
- `render: markdown` uses luc's built-in glamour-based terminal markdown renderer.
- `modal.open` and `confirm.request` are host-rendered dialog actions; they do not provide arbitrary custom TUI layout injection.
- Approval policies only auto-intercept tools when `ui.approvals_mode: policy`.
- In `trusted` mode, normal tool execution is unchanged; explicit client confirmation requests still render.

### Hooks

Runtime hook manifests live in:

- `~/.luc/hooks`
- `<workspace>/.luc/packages/*/hooks`
- `<workspace>/.luc/hooks`

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

Hook notes:

- Hooks subscribe to live events only; they do not run during history replay or session reopen.
- Hooks are async-only in this slice and never block the turn loop.
- Stable hook subscription event kinds today are `message.assistant.final` and `tool.finished`.
- Hook request envelopes include the live event, workspace/session metadata, and `host_capabilities`. The current JSON shape is:

```json
{
  "event": {"kind": "tool.finished", "payload": {"id": "call_1", "name": "bash"}},
  "workspace": {"root": "/abs/workspace", "project_id": "repo", "branch": "main"},
  "session": {"session_id": "sess_123", "provider": "openai", "model": "gpt-5.4"},
  "host_capabilities": ["hooks.live_events"]
}
```

- Parseable hook stdout event types today are `log`, `progress`, `client_action`, `done`, and `error`.
- Hooks may emit `client_action` only when `runtime.capabilities` includes `client_actions`. That lets a hook request host-owned actions such as `view.refresh` after it updates state.
- Hook stdin starts with one JSON request envelope line. When `client_actions` is enabled, luc keeps stdin open and sends `client_result` envelopes back on later lines.
- Hook stdout field names are exact: `log` uses `text` (luc also accepts `message` as a compatibility alias), `progress` uses `progress` (or `message`), `client_action` uses `action`, `error` uses `error` (or `message`), and `done` should set `done: true`.
- Supported hook `client_action.kind` values today are `modal.open`, `confirm.request`, `view.open`, `view.refresh`, and `command.run`.
- Hook failures are logged and surfaced through `hook.failed` history events, but they do not break the session.

### Extension Hosts

Runtime extension host manifests live in:

- `~/.luc/extensions`
- `<workspace>/.luc/packages/*/extensions`
- `<workspace>/.luc/extensions`

Phase 1 is observe-only. Extension hosts are long-lived child processes that
stay attached to the active session over JSONL on stdin/stdout.

Example:

```yaml
schema: luc.extension/v1
id: audit
protocol_version: 1
runtime:
  kind: exec
  command: ./host.py
  args: [--jsonl]
subscriptions:
  - event: session.start
  - event: message.assistant.final
  - event: tool.finished
```

Extension host notes:

- Hosts are started on session start/open and restarted on `luc reload`.
- `runtime.command` is executed relative to the extension manifest directory; `runtime.args` and `runtime.env` are optional.
- Startup sends `hello`, then `storage_snapshot`, then `session_start`.
- Observe events are currently limited to `session.start`, `session.reload`, `message.assistant.final`, `tool.finished`, `tool.error`, and `compaction.completed`.
- `session_shutdown` is sent before luc tears a host down for reload, close, or session switch.
- Host stdout message types in this slice are `ready`, `log`, `progress`, `client_action`, `storage_update`, `error`, and `done`.
- `client_action` uses the same host-owned action kinds as tools/providers/hooks: `modal.open`, `confirm.request`, `view.open`, `view.refresh`, and `command.run`.
- Extension hosts are trusted local processes in this phase. Failures are logged and surfaced through `extension.failed` history events, but the session continues.
- Sync interception seams and hosted tools are planned, but they are not part of this observe-only slice yet.

### Skills

Runtime skills live in:

- `~/.agents/skills`
- `~/.luc/skills`
- `<workspace>/.agents/skills`
- `<workspace>/.luc/skills`

Preferred format:

- `skill-name/SKILL.md`

Example layout:

```text
~/.luc/skills/
  rails/
    SKILL.md
  weaver/
    luc.yaml
    SKILL.md
```

`SKILL.md` is the canonical skill body. `luc.yaml` is optional metadata and UI
overlay for the skill package. `luc` also supports top-level standalone `.md`
files for backward compatibility, but directory-based skills are preferred.

Example:

```yaml
interface:
  display_name: "Weaver"
  short_description: "Operate local Git branch stacks"
```

Optional manifest fields:

- `name`
- `description`
- `short_description`
- `default_prompt`
- `triggers`
- `always`
- `interface.display_name`
- `interface.short_description`
- `interface.default_prompt`

Current skill behavior:

- Skills are discovered at startup and reload, but only as metadata.
- `skill-name/SKILL.md` is the canonical instruction source.
- `skill-name/luc.yaml` is metadata only when `SKILL.md` exists.
- Every request gets a compact skill catalog in the system prompt: `name`, optional display name, and description.
- If a skill declares `triggers`, luc may also add a short "likely relevant skills" hint for matching requests.
- The model loads a skill by calling `load_skill` with the exact skill name.
- `load_skill` returns the full `SKILL.md` body once per session; repeated loads return an already-loaded note.
- If a loaded skill references bundled files, the model can read them with `read_skill_resource`.
- Built-in creator skills always exist in the catalog: `skill-creator`, `plugin-creator`, and `theme-creator`.
- `default_prompt` is only used when a manifest-backed skill has no `SKILL.md`.

### Themes

Runtime themes live in:

- `~/.luc/themes`
- `<workspace>/.luc/themes`

Supported manifest formats:

- `.yaml`
- `.yml`
- `.json`

Example:

```yaml
inherits: light
colors:
  accent: "#ff5500"
  panel: "#fff7f2"
  line: "#e2c6b8"
  text: "#2b211c"
  muted: "#7e6355"
  blue: "#cc4b00"
  cyan: "#006d77"
```

Set `ui.theme` in config to the theme name, for example:

```yaml
ui:
  theme: sunrise
```

If the theme file is not found, `luc` falls back to built-in `light` / `dark`
resolution.

### System Prompt

Base prompt override files:

- `~/.luc/prompts/system.md`
- `<workspace>/.luc/prompts/system.md`

Project prompt overrides the global prompt when both exist.

Prompt extension manifests live in:

- `~/.luc/prompts`
- `<workspace>/.luc/packages/*/prompts`
- `<workspace>/.luc/prompts`

Supported manifest formats:

- `.yaml`
- `.yml`
- `.json`

Use prompt extensions when you want to append a small tuning block for only
some providers or model families instead of replacing the entire system prompt.
Later layers override earlier ones when they share the same `id`; distinct
extensions are composed together in stable `id` order.

Example:

```yaml
schema: luc.prompt/v1
id: gpt5-tight-loop
description: Keep GPT-5-family turns compact and execution-first.
match:
  providers: [openai, openai-compatible]
  model_prefixes: [gpt-5]
prompt: |
  Keep preambles to one short sentence.
  Prefer doing the work over describing the plan.
  Use tool calls deliberately and avoid redundant retries.
```

Prompt extension fields:

- `schema`: must be `luc.prompt/v1`
- `id`: optional; defaults to the filename when omitted
- `description`: optional metadata
- `match.providers`: optional exact provider IDs
- `match.models`: optional exact model IDs
- `match.model_prefixes`: optional model family prefixes
- `prompt`: required instruction text appended after the base system prompt

Matching behavior:

- Empty `match` applies globally.
- Provider `openai` and `openai-compatible` are treated as aliases.
- Within the model matcher, `models` and `model_prefixes` are additive: either
  one can match.

## Config

```yaml
ui:
  approvals_mode: trusted

extensions:
  hooks_enabled: true
```

Allowed approval modes:

- `trusted`
- `policy`

## Host Capability Gating

UI and hook manifests may declare `requires_host_capabilities`. luc compares
those requirements against the host capability set it advertises to structured
tools/providers/hooks, for example:

- `ui.modal`
- `ui.confirm`
- `ui.view.open`
- `ui.command`
- `hooks.live_events`
- `extensions.observe_events`
- `extensions.storage.session`
- `extensions.storage.workspace`

Unsupported required capabilities do not crash reload. luc skips that
contribution and records a reload diagnostic instead.

## Reloading

Changes are picked up on:

- app startup
- `luc reload`
- `ctrl+r` in the TUI

## Current Limits

These runtime surfaces work today without recompiling:

- tools
- providers (`openai-compatible` and `exec`)
- runtime UI manifests (commands, views, approval policies)
- hooks
- extension hosts (`luc.extension/v1`, observe-only)
- skills
- themes
- system prompt

These still need core code changes today:

- arbitrary custom TUI layout injection beyond the documented runtime actions and view surfaces
- new runtime action kinds beyond `modal.open`, `confirm.request`, `view.open`, `view.refresh`, and `command.run`
- modifying the built-in `Overview` tab directly instead of adding a runtime `inspector_tab` or `page`
