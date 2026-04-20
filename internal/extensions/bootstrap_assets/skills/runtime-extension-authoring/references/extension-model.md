# Extension Model

Use this guide when deciding how to extend `luc` without changing core code.

## Mental Model

`luc` has two extension styles:

- Declarative surfaces:
  - `luc.tool/v1` exec tools
  - `luc.tool/v2` hosted tools
  - `luc.ui/v1` commands, views, approval policies
  - `luc.hook/v1` async hooks
  - `luc.prompt/v1` prompt additions
  - providers, skills, themes
- Programmable surface:
  - `luc.extension/v1` long-lived extension hosts

Rule of thumb:

- If the behavior is static registration, use a declarative manifest.
- If the behavior needs session state, sync interception, or hosted tool execution, use `luc.extension/v1`.
- Prefer composing multiple supported surfaces instead of editing core code.

## Which Surface To Use

Use `luc.tool/v1` when:

- you need a normal tool call
- a one-shot shell or structured exec process is enough
- each invocation can be isolated

Use `luc.tool/v2` with `runtime.kind: extension` when:

- the tool should run inside a long-lived extension host
- the tool needs shared state across invocations in the same session
- the same host already owns related sync seams or storage

Use `luc.extension/v1` when:

- you need `input.transform`
- you need `prompt.context`
- you need `tool.preflight`
- you need `tool.result`
- you need observe events such as `message.assistant.final`
- you need session/workspace storage
- you need hosted tool handlers

Use `luc.hook/v1` when:

- you want async side effects only
- the session must continue even if the hook fails
- you do not need to block or mutate the active turn loop

Use `luc.ui/v1` when:

- you need commands in the command palette
- you need a runtime `inspector_tab` or `page`
- you need approval policies for tools

Use `luc.prompt/v1` when:

- you want small provider/model-targeted prompt additions
- you do not need sync code or session state

Use an exec provider when:

- you are adapting an upstream model API to luc's provider protocol
- the provider must stream `thinking`, `text_delta`, `tool_call`, or `client_action`

Use skills when:

- the goal is to teach the model how to use an existing capability
- the capability is procedural guidance, not runtime execution

## Common Choices

If the user wants a sidebar/inspector addition:

- usually `luc.ui/v1`
- pair with a tool if the view needs data

If the user wants a Slack or webhook notification after a reply:

- `luc.hook/v1`

If the user wants to rewrite the user's text before model submission:

- `luc.extension/v1` with `input.transform`

If the user wants to add hidden context before the provider request:

- `luc.extension/v1` with `prompt.context`

If the user wants to patch or block a tool call before execution:

- `luc.extension/v1` with `tool.preflight`
- optionally pair with `luc.ui/v1` approval policies

If the user wants to patch tool output before transcript persistence:

- `luc.extension/v1` with `tool.result`

If the user wants a stateful tool:

- `luc.extension/v1` plus a declarative `luc.tool/v2`

If the user wants reusable host UI from a tool/provider/hook/extension host:

- emit `client_action`
- define persistent views/commands in `luc.ui/v1`

## Surface Relationships

Typical combinations:

- `luc.tool/v1` + `luc.ui/v1`
  - exec tool plus runtime view or approval policy
- `luc.extension/v1` + `luc.tool/v2`
  - long-lived host plus hosted/stateful tool
- `luc.extension/v1` + `luc.ui/v1`
  - programmable logic plus host-owned commands/views
- exec provider + `luc.ui/v1`
  - provider protocol adapter plus runtime UI
- `luc.hook/v1` + `luc.ui/v1`
  - async side effect plus host view refresh or command trigger

## Minimal Shapes

### Simple Exec Tool

~~~yaml
name: repo_status
description: Show repository status.
command: git status --short
schema:
  type: object
  properties: {}
~~~

### Structured Exec Tool

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
  properties: {}
~~~

### Extension Host

~~~yaml
schema: luc.extension/v1
id: audit
protocol_version: 1
runtime:
  kind: exec
  command: ./host.py
subscriptions:
  - event: input.transform
    mode: sync
    failure_mode: closed
  - event: message.assistant.final
~~~

### Hosted Tool

~~~yaml
schema: luc.tool/v2
name: stateful_echo
description: Echo through the audit host.
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

### Async Hook

~~~yaml
schema: luc.hook/v1
id: slack_notify
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
~~~

### Runtime UI

~~~yaml
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
~~~

## Current Limits

- Runtime views are host-owned and read-only.
- No arbitrary custom TUI injection.
- No dynamic tool registration from extension code.
- Hosted tools must still be declared by manifest.
- Hooks are async-only and do not mutate the turn loop.
