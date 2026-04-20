# Hybrid Extension Architecture for luc

## Status

Current implementation status in the repo:

- Phase 1 is implemented:
  - `luc.extension/v1` manifest loading and layered discovery
  - long-lived extension host supervision over JSONL stdin/stdout
  - observe events for `session.start`, `session.reload`, `message.assistant.final`, `tool.finished`, `tool.error`, and `compaction.completed`
  - host-managed session/workspace storage snapshots and updates
- Phase 2 is implemented:
  - sync seams for `input.transform`, `prompt.context`, `tool.preflight`, and `tool.result`
  - request/response correlation with `request_id`
  - timeout and `failure_mode` handling for supported guard seams
- Phase 3 is implemented:
  - `luc.tool/v2` with `runtime.kind: extension`
  - hosted tool routing through long-lived extension hosts via `tool_invoke` / `tool_result`
- Phase 4 is implemented:
  - protocol conformance coverage for malformed hosts, restart behavior, and diagnostics
  - restart/backoff hardening for unhealthy extension hosts
  - runtime diagnostics for broken extension hosts
  - direct Python/Go authoring docs and hybrid package examples
- Still pending from this plan:
  - official JS/TS SDK

## Summary

Adopt a **protocol-first hybrid** extension model where luc’s current manifest-driven, exec-based runtime remains the canonical extension contract, and add an **optional long-lived extension host protocol** plus an official **JS/TS SDK** as a convenience layer on top of that protocol.

This keeps luc language-agnostic and local-first while unlocking selected high-value programmable behaviors that today are only practical in systems like pi. The SDK is **not** the source of truth. The source of truth is a versioned manifest and RPC/JSONL protocol that any language can implement.

The planned end state is:

- Existing runtime surfaces remain first-class and stable:
  - `luc.tool/v1` and `luc.tool/v1` structured exec tools
  - `luc.hook/v1` async hooks
  - `luc.ui/v1` commands, views, approval policies
  - runtime providers, prompt extensions, skills, themes
- New optional programmable surface:
  - `luc.extension/v1` long-lived extension host processes
- Selected synchronous extension seams only:
  - `input.transform`
  - `prompt.context`
  - `tool.preflight`
  - `tool.result`
- Observe-only lifecycle events:
  - `session.start`
  - `session.reload`
  - `session.shutdown`
  - `message.assistant.final`
  - `tool.finished`
  - `tool.error`
  - `compaction.completed`
- Trusted-local-first scope:
  - local and project-installed extensions first
  - package sharing supported by the existing package asset model
  - no marketplace/signing in this phase
- No arbitrary custom TUI injection and no pi-style full mutable lifecycle parity

## Goals

- Preserve luc’s cross-language extension story.
- Avoid making JS/TS the only serious extension path.
- Add a richer programmable surface without replacing existing manifests.
- Make stateful and guard-style extensions possible without forcing every extension into raw exec tools and hooks.
- Keep host-owned UI, approval, prompt, and transcript behavior predictable.
- Keep extension failures isolated and diagnosable.

## Non-Goals

- Full pi-style in-process extension SDK.
- Arbitrary custom TUI component injection.
- Full session/tree mutation from extensions.
- Marketplace, signing, remote discovery, or trust attestation.
- Replacing existing manifest formats with SDK registration calls.
- Giving extensions unrestricted control over compaction or replay semantics in the first hybrid version.

## Architecture

## 1. Canonical Model

- The canonical extension contract remains **manifest + protocol**, not SDK.
- Every programmable extension capability must have a protocol shape that can be implemented without Node.
- The official JS/TS SDK is a wrapper over that protocol.
- Existing manifest-based surfaces continue to load exactly as they do today unless explicitly extended.

## 2. New Programmable Surface

- Introduce a new manifest schema: `luc.extension/v1`.
- New discovery locations:
  - `~/.luc/extensions`
  - `<workspace>/.luc/extensions`
  - `<workspace>/.luc/packages/*/extensions`
- Precedence follows the existing runtime layering model:
  - global user layer
  - installed package layer
  - project layer
- Within a layer, later lexicographic manifest continues to win.

## 3. Extension Host Runtime Model

- A `luc.extension/v1` entry launches a **long-lived child process** for the session.
- The child process communicates with luc over stdin/stdout using JSONL.
- One process is launched per enabled extension host per active session.
- On `reload`, luc shuts down all extension hosts and starts fresh ones.
- On session reopen, hosts are recreated and rehydrated from host-provided state/context.
- Extension hosts are isolated from luc internals. They do not run in-process.

## 4. Hybrid Composition Rule

- Declarative manifests remain the preferred way to define:
  - tools
  - UI commands/views/policies
  - prompts
  - hooks
  - providers
- Programmable extension hosts are used for:
  - selected lifecycle interception
  - stateful logic
  - hosted tool implementations
  - richer policy/guard behavior
- Declarative and programmable contributions can coexist in the same package.

## Public APIs, Interfaces, and Types

## 1. New Manifest: `luc.extension/v1`

Required fields:

- `schema: luc.extension/v1`
- `id`
- `runtime.kind: exec`
- `runtime.command`
- optional `runtime.args`
- optional `runtime.env`
- `protocol_version: 1`
- `subscriptions`
- optional `hosted_tools`
- optional `requires_host_capabilities`

Each subscription entry must include:

- `event`
- `mode: observe | sync`
- optional `timeout_ms`
- optional `failure_mode: open | closed`

Default subscription behavior:

- `mode: observe` events are async and never block the turn loop.
- `mode: sync` events block only the specific supported seam.
- default `failure_mode` is `open`.
- `failure_mode: closed` is allowed only for `tool.preflight` and `input.transform`.

## 2. Supported Events

Observe-only events:

- `session.start`
- `session.reload`
- `session.shutdown`
- `message.assistant.final`
- `tool.finished`
- `tool.error`
- `compaction.completed`

Synchronous events:

- `input.transform`
- `prompt.context`
- `tool.preflight`
- `tool.result`

No other synchronous events are in scope for this end state.

## 3. Sync Event Contracts

`input.transform`

- Host sends raw input text, attachments summary, session/model/workspace metadata.
- Extension returns one of:
  - `continue`
  - `transform` with new text and optional attachment directives
  - `handled` with optional user-visible message
- If multiple extensions transform input, transformations apply in precedence order.
- First `handled` result short-circuits remaining input handlers.

`prompt.context`

- Host sends current request context after input is finalized and before provider request assembly.
- Extension may return:
  - `system_append` blocks
  - hidden context blocks for prompt inclusion
  - no-op
- Extensions may not replace the entire system prompt.
- Host concatenates prompt additions in precedence order.

`tool.preflight`

- Host sends final parsed tool name and arguments before execution and before approval policy rendering.
- Extension may return:
  - `allow`
  - `patch` with revised arguments
  - `block` with reason
- Patched arguments become the canonical arguments for approval policies and execution.
- First `block` result short-circuits execution.

`tool.result`

- Host sends tool result before transcript/replay persistence.
- Extension may patch:
  - result content
  - structured metadata
  - collapsed summary
  - error classification
- Patches are applied in precedence order.
- Result patching is limited to the existing tool result envelope; extensions do not write arbitrary transcript events here.

## 4. Hosted Tool Execution

Add a new tool runtime mode:

- `luc.tool/v2` with `runtime.kind: extension`

Required tool fields in this mode:

- `name`
- `description`
- `input_schema`
- `runtime.kind: extension`
- `runtime.extension_id`
- `runtime.handler`

Execution model:

- Tool remains declaratively registered and discoverable by luc.
- Execution is delegated to the named extension host process.
- The extension host returns the same normalized tool result shape luc already expects.
- Hosted tool handlers may share extension process state across invocations.

Non-goal:

- dynamic ad hoc tool registration from SDK code in this phase

## 5. Extension Protocol

Host to extension messages:

- `hello`
- `session_start`
- `session_reload`
- `session_shutdown`
- `event`
- `tool_invoke`
- `storage_snapshot`
- `ping`

Extension to host messages:

- `ready`
- `decision`
- `tool_result`
- `log`
- `progress`
- `client_action`
- `storage_update`
- `error`
- `done`

Protocol rules:

- JSONL only
- protocol version negotiated on `hello` / `ready`
- unknown capabilities are ignored with diagnostics
- invalid message shapes mark the extension unhealthy and are surfaced in diagnostics/logs
- sync request/response correlation uses explicit request IDs

## 6. SDK Surface

Ship an official package, for example `@lutefd/luc-extension-sdk`.

SDK capabilities:

- subscribe to supported events
- register hosted tool handlers declared by manifest
- read/write namespaced storage
- send host-owned UI requests through `client_action`
- expose typed request/response helpers for the supported sync seams

SDK design rule:

- every SDK call must map cleanly to public protocol messages
- no hidden privileged backdoor APIs

## 7. Storage API

Provide two host-managed namespaced stores per extension:

- `session storage`
- `workspace storage`

Session storage:

- persisted with session state
- not sent to the model unless the extension explicitly injects it via `prompt.context`
- restored on session reopen

Workspace storage:

- persisted under luc state for the workspace
- available across sessions in the same workspace
- intended for caches, preferences, and non-transcript operational state

Both stores use JSON values with explicit size limits and whole-value replacement semantics in v1.

## Data Flow

## 1. Startup / Reload

- Load existing declarative contributions exactly as today.
- Load `luc.extension/v1` manifests.
- Start extension host processes.
- Send `hello`, then `session_start`.
- Mark host healthy only after `ready`.
- Unhealthy hosts are skipped and surfaced in runtime diagnostics.

## 2. User Request Path

- Apply `input.transform` subscriptions in precedence order.
- Compose base prompt plus runtime prompt extensions plus compaction summary plus `prompt.context` additions.
- Run tool loop as today.
- Before each tool execution, run `tool.preflight`.
- After tool execution, run `tool.result`.
- Persist the final result/event stream.
- Fire observe-only events asynchronously.

## 3. Reload Path

- Emit `session_shutdown` to extension hosts.
- Terminate lingering child processes after timeout.
- Re-read declarative runtime assets.
- Restart hosts and resend `session_start`.

## 4. Hosted Tool Path

- Tool is discovered from manifest at startup/reload.
- LLM calls tool by normal name.
- luc routes invocation to owning extension host.
- Extension host returns normalized result envelope.
- luc applies normal transcript, inspector, approval, and replay behavior.

## Failure and Safety Semantics

- Default for sync subscriptions is fail-open.
- `failure_mode: closed` is allowed only for guard-style seams:
  - `input.transform`
  - `tool.preflight`
- Observe events never fail the session.
- Extension crash during observe event logs `extension.failed` and the session continues.
- Extension crash during sync event follows that subscription’s `failure_mode`.
- Hosted tool handler failure is surfaced as a normal tool error.
- Timeouts are enforced per subscription.
- Recommended defaults:
  - `input.transform`: 300ms
  - `prompt.context`: 500ms
  - `tool.preflight`: 500ms
  - `tool.result`: 750ms
  - observe events: 5s soft timeout
- Extension hosts are fully trusted local processes in this phase.

## UI and UX Rules

- Host-owned UI remains host-owned.
- Extension hosts may request only existing `client_action` kinds:
  - `modal.open`
  - `confirm.request`
  - `view.open`
  - `view.refresh`
  - `command.run`
- No arbitrary runtime-rendered custom component API is added in this phase.
- Rich UI remains declarative through `luc.ui/v1`.
- Packages can combine:
  - `luc.ui/v1` for commands/views/policies
  - `luc.extension/v1` for programmable logic
  - `luc.tool/v2 runtime.kind: extension` for stateful tools

## Compatibility and Migration

- Existing tools, hooks, providers, UI manifests, prompts, skills, and themes remain supported without change.
- Existing `luc.hook/v1` async hooks remain the preferred simple mechanism for fire-and-forget side effects.
- `luc.extension/v1` is additive, not a replacement.
- `runtime.kind: exec` tools remain valid and do not migrate automatically.
- Hosted tools are opt-in through the new `runtime.kind: extension`.
- Current package asset structure remains valid. New extension hosts slot into the same package layering model.

## Implementation Phases

## Phase 1: Foundation

Status: implemented

- Add `luc.extension/v1` manifest loader and discovery.
- Add extension host supervisor and JSONL protocol transport.
- Add health, reload, diagnostics, and process lifecycle management.
- Add observe-only events first.
- Add host-managed storage.
- Add protocol docs and fixture examples.
- Add a minimal JS/TS SDK that can consume observe-only events.

## Phase 2: Selected Sync Hooks

Status: implemented

- Add `input.transform`
- Add `prompt.context`
- Add `tool.preflight`
- Add `tool.result`
- Add ordering, timeout, and fail-open/fail-closed semantics.
- Add inspector/log diagnostics for sync hook behavior.

## Phase 3: Hosted Tools

Status: implemented except SDK helper work

- Add `luc.tool/v2 runtime.kind: extension`
- Add hosted tool handler routing
- Add SDK helper for hosted tool registration
- Add stateful tool examples
- Validate coexistence with approval policies and transcript rendering

## Phase 4: Hardening

Status: not implemented

- Add protocol conformance tests
- Add restart/backoff behavior for unhealthy hosts
- Add diagnostics UX for broken extensions
- Add package examples showing declarative + programmable hybrid composition
- Add authoring docs for direct protocol implementations in Python and Go

## Test Cases and Scenarios

- Extension host loads from global, package, and project layers with correct precedence.
- Reload restarts extension hosts and rehydrates storage correctly.
- Observe-only events never block a normal request.
- `input.transform` can continue, transform, and handle input correctly.
- Multiple `input.transform` subscribers compose in precedence order.
- `tool.preflight` can patch arguments and approval policy sees the patched arguments.
- `tool.preflight` block short-circuits tool execution and returns a user-visible reason.
- `tool.result` patching updates transcript/inspector output and replay payloads correctly.
- Sync hook timeout with `failure_mode: open` allows request flow to continue.
- Sync hook timeout with `failure_mode: closed` blocks the guarded seam correctly.
- Hosted tool execution works through the normal LLM tool path.
- Hosted tool handlers can use session storage and workspace storage.
- Extension crash during observe event does not kill luc.
- Extension crash during hosted tool invocation becomes a tool error, not a session crash.
- JS/TS SDK implementation and a direct Python protocol implementation can both handle the same event/tool flow.
- Existing `luc.hook/v1` async hooks still work unchanged beside new extension hosts.
- Compaction continues to be host-owned, and extensions receive only `compaction.completed` observe events in this end state.

## Acceptance Criteria

- A trusted local extension written in JS/TS using the SDK can:
  - transform input
  - append prompt context
  - guard or patch a tool call
  - patch a tool result
  - implement a stateful hosted tool
  - use host-owned dialogs/views/commands
- The same core behaviors are possible from a non-JS extension that implements the protocol directly.
- Existing manifest-based extensions continue to function unchanged.
- The host remains stable when an extension hangs, crashes, or emits malformed protocol messages.
- The architecture remains clearly protocol-first in docs, code ownership, and public API boundaries.

## Assumptions and Defaults

- Primary contract: protocol-first, SDK second.
- Hook depth: limited interception only, not full pi-style lifecycle mutation.
- Ecosystem scope: trusted local first, no marketplace/signing in this plan.
- Compaction customization is out of scope for this end state; only observe-after-compaction is included.
- No arbitrary custom TUI rendering API is added.
- Hosted tool registration is declarative, not dynamic.
- Existing manifest surfaces remain the preferred authoring path unless stateful or intercepting behavior is required.
