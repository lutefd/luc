# Runtime Extensions

`luc` can now load several extension types at runtime without recompiling.

For a single surface-selection guide, see [extension-model.md](/Users/lfdourado/dev/p/luc/docs/extension-model.md).

For direct Python/Go extension-host implementations, see [extension-host-protocol.md](/Users/lfdourado/dev/p/luc/docs/extension-host-protocol.md).

On a clean install, first launch bootstraps the global `~/.luc` tree and seeds
the bundled helper skills `runtime-extension-authoring`, `skill-usage`, and
`theme-creator` there if they are missing. Existing user files are left
untouched.

Lookup order:

1. Global user layer: `~/.luc/...`
2. User installed package assets: `~/.luc/packages/*/...`
3. Project installed package assets: `<workspace>/.luc/packages/*/...`
4. Project override layer: `<workspace>/.luc/...`

Later layers override earlier ones. Within the same layer, later lexicographic
manifest wins.

The user and project package layers are populated by `luc pkg install`.

## Supported Runtime Surfaces

### Tools

Runtime tools live in:

- `~/.luc/tools`
- `~/.luc/packages/*/tools`
- `<workspace>/.luc/packages/*/tools`
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

Hosted tools can also be declared and routed through a long-lived extension
host:

```yaml
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
```

Tool capability notes:

- Omitting `runtime.capabilities` keeps today's legacy behavior.
- `structured_io` means luc writes a JSON request envelope to stdin and expects JSONL events on stdout.
- `client_actions` means the tool may emit `client_action` events and receive `client_result` responses over stdin/stdout.
- Structured request envelopes include `tool_name`, typed `arguments`, `workspace`, `session_id`, `agent_id`, `host_capabilities`, and optional `view_context`.
- Supported structured tool stdout event types today are `stdout`, `stderr`, `progress`, `client_action`, `result`, `done`, and `error`.
- Structured tool stdout field names are exact: `stdout`, `stderr`, and `progress` use `text`; `client_action` uses `action`; `result` uses `result`; `error` uses `error`; `done` should set `done: true`.
- Supported `client_action.kind` values today are `modal.open`, `confirm.request`, `view.open`, `view.refresh`, `command.run`, and `tool.run`.
- Structured tools should emit a `result` event carrying the final tool payload, then emit `done` to terminate the stream.
- Hosted tools use `schema: luc.tool/v2` with `runtime.kind: extension`, `runtime.extension_id`, and `runtime.handler`.
- Hosted tool discovery remains declarative; luc does not support dynamic tool registration from extension code in this slice.
- Hosted tool execution is routed to the named extension host over `tool_invoke`, and the host replies with `tool_result`.
- Hosted tools return the same normalized result envelope luc already uses for other tools: `content`, optional `metadata`, `default_collapsed`, and `collapsed_summary`.

Template variables available in `command` and `ui.collapsed_summary`:

- top-level args from the tool call JSON
- `.args`
- `.workspace`
- `.session_id`
- `.agent_id`
- `.tool_dir`
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
- `~/.luc/packages/*/providers`
- `<workspace>/.luc/packages/*/providers`
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
id: acme
name: Acme Gateway
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
- `~/.luc/packages/*/ui`
- `<workspace>/.luc/packages/*/ui`
- `<workspace>/.luc/ui`

Example:

```yaml
schema: luc.ui/v1
id: provider-tools
commands:
  - id: provider.status.open
    name: Open provider status
    description: Show the current provider status panel.
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
  - id: review.implement
    name: Implement Approved Review
    action:
      kind: session.handoff
      title: Start implementation
      handoff:
        title: Approved Review
        body: |
          ## Approved context

          Carry this review summary into a fresh implementation session.
        render: markdown
      initial_input: Implement the approved changes.
views:
  - id: provider.status
    title: Provider Status
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
          result:
            presentation: status
      - id: refresh
        label: Refresh
        action:
          kind: view.refresh
          view_id: provider.status
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

- Command metadata: `description`, `category`, and `shortcut`
- Command shortcuts use Bubble Tea keystroke syntax such as `ctrl+shift+p`; built-in shortcut collisions and duplicate runtime shortcut collisions are reported as diagnostics.
- Command actions: `view.open`, `view.refresh`, `command.run`, `tool.run`, `session.handoff`
- `tool.run` executes the named tool through luc's normal tool pipeline, including extension preflight/result hooks and approval policies. `result.presentation: status` reports completion in the status line.
- `session.handoff` creates and switches to a fresh host-owned session, records a visible handoff event, and seeds `initial_input` into the composer without auto-submitting.
- Client actions: `modal.open`, `confirm.request`, `view.open`, `view.refresh`, `command.run`, `tool.run`, `session.handoff`
- View placements: `inspector_tab`, `page`
- View renderers: `markdown`, `json`, `table`, `kv`
- View actions: declarative `actions[]` render as host-owned selectable rows in runtime inspector tabs and pages. Navigate with tab/arrows, press `enter` to activate, or use an action `shortcut`.

Runtime UI notes:

- Runtime commands are registered into luc's command palette alongside the built-in commands.
- Runtime views are host-owned. View content is rendered from `source_tool`; optional view `actions[]` are host-rendered controls that trigger existing runtime action kinds.
- A view's `source_tool` runs when the view opens or refreshes.
- `render: markdown` uses luc's built-in glamour-based terminal markdown renderer.
- `modal.open` and `confirm.request` are host-rendered dialog actions. `modal.open` supports `render: markdown`, multiple `options`, and optional text `input` for blocking workflows, but does not provide arbitrary custom TUI layout injection.
- Approval policies only auto-intercept tools when `ui.approvals_mode: policy`.
- In `trusted` mode, normal tool execution is unchanged; explicit client confirmation requests still render.

### Hooks

Runtime hook manifests live in:

- `~/.luc/hooks`
- `~/.luc/packages/*/hooks`
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
- `~/.luc/packages/*/extensions`
- `<workspace>/.luc/packages/*/extensions`
- `<workspace>/.luc/extensions`

Extension hosts are long-lived child processes that stay attached to the
active session over JSONL on stdin/stdout.

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
  - event: tool.preflight
    mode: sync
    failure_mode: closed
```

Extension host notes:

- Hosts are started on session start/open and restarted on `luc reload`.
- `runtime.command` is executed relative to the extension manifest directory; `runtime.args` and `runtime.env` are optional.
- Startup sends `hello`, then `storage_snapshot`, then `session_start`.
- Observe events are currently limited to `session.start`, `session.reload`, `message.assistant.final`, `tool.finished`, `tool.error`, and `compaction.completed`.
- Sync seams currently supported are `input.transform`, `prompt.context`, `tool.preflight`, and `tool.result`.
- `mode` defaults to `observe`. Sync subscriptions block only their seam and use per-subscription `timeout_ms` or the built-in defaults.
- `failure_mode` defaults to `open`. `closed` is currently allowed only for `input.transform` and `tool.preflight`.
- `session_shutdown` is sent before luc tears a host down for reload, close, or session switch.
- Sync requests are sent as `event` envelopes with a `request_id`; the extension answers with `decision` carrying the same `request_id`.
- Host stdout message types in this slice are `ready`, `decision`, `tool_result`, `log`, `progress`, `client_action`, `storage_update`, `error`, and `done`.
- `client_action` uses the same host-owned action kinds as tools/providers/hooks: `modal.open`, `confirm.request`, `view.open`, `view.refresh`, `command.run`, and `tool.run`.
- Rich `modal.open` actions may set `render: markdown`, provide multiple `options`, and enable text `input`; blocking responses include the selected `choice_id` and `data.input` when input is enabled.
- Extension hosts are trusted local processes in this phase.
- Host crashes, hangs, and malformed protocol output mark the host unhealthy, surface runtime diagnostics, and trigger bounded automatic restart with exponential backoff.
- Current restart defaults are 250 ms base delay, 2 s max delay, and 4 retry attempts per session before the host is disabled for the rest of that session.
- Broken-host diagnostics are exposed through runtime diagnostics and inspector logs, and they clear automatically after the host becomes healthy again.
- `extension.failed` history events are still emitted when a running host fails, but the session continues.
- Hosted tools are supported through declarative `luc.tool/v2` manifests backed by `luc.extension/v1` hosts.
- For a complete hybrid package example, see `examples/packages/hybrid-audit`.

### Skills

Runtime skills live in:

- `~/.agents/skills`
- `~/.luc/skills`
- `~/.luc/packages/*/skills`
- `<workspace>/.luc/packages/*/skills`
- `<workspace>/.agents/skills`
- `<workspace>/.luc/skills`

That means `luc pkg install` can ship reusable skills without copying them into
`~/.luc/skills` directly.

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
- `~/.luc/packages/*/themes`
- `<workspace>/.luc/packages/*/themes`
- `<workspace>/.luc/themes`

That means `luc pkg install` can ship reusable themes without copying them into
`~/.luc/themes` directly.

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
- `~/.luc/packages/*/prompts`
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

luc merges config in this order:

1. `~/.config/luc/config.yaml`
2. `~/.luc/config.yaml`
3. `<workspace>/.luc/config.yaml`

Later files override earlier files. Use `~/.luc/config.yaml` for user-wide luc customizations that live with the rest of your runtime extensions, and project `.luc/config.yaml` for workspace-specific behavior.

```yaml
ui:
  approvals_mode: trusted
  agent_statuses:
    - Churning...
    - Consulting the rubber duck...
    - Reticulating splines...

extensions:
  hooks_enabled: true
```

### Temporary agent status messages

When a normal agent turn is in flight, the TUI appends a temporary chat-style status line with elapsed seconds, for example:

```text
· Churning... (4s)
```

These messages are deliberately playful and are not the real execution state. The real state stays in the inspector/overview. The temporary message stays under the transcript while the assistant is thinking or running tools, then is removed from the chat view once the assistant starts streaming its final text response.

Customize the pool with `ui.agent_statuses` in either user or project config:

```yaml
# ~/.luc/config.yaml or <workspace>/.luc/config.yaml
ui:
  agent_statuses:
    - Churning...
    - Counting tiny robots...
    - Asking the code goblin nicely...
    - Reticulating splines...
```

Notes:

- Empty strings are ignored.
- Project config replaces the user/global list when `agent_statuses` is set.
- Changes are picked up on app startup; use `luc reload` / `ctrl+r` for other runtime assets.

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
- extension hosts (`luc.extension/v1`, observe plus selected sync seams)
- skills
- themes
- system prompt

These still need core code changes today:

- arbitrary custom TUI layout injection beyond the documented runtime actions and view surfaces
- new runtime action kinds beyond `modal.open`, `confirm.request`, `view.open`, `view.refresh`, `command.run`, and `tool.run`
- modifying the built-in `Overview` tab directly instead of adding a runtime `inspector_tab` or `page`
