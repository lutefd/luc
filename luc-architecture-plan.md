# `luc` Architecture Plan

## Summary

Build `luc` as a greenfield Go project at `github.com/lutefd/luc` with a TUI-first product shell. The UI stack is fixed: Bubble Tea as the single MVU runtime, Bubbles for viewport/textarea/help primitives, Lip Gloss for layout/styling, Glamour for markdown rendering, and `charmbracelet/log` for structured logs. That stack matches the product goals well: Bubble Tea is built around an Elm-style update loop and a high-performance renderer, Bubbles already provides the viewport and textarea primitives we need, Lip Gloss is intended for terminal layout/styling, Glamour provides configurable markdown rendering, and `log` supports leveled structured output. ([github.com](https://github.com/charmbracelet/bubbletea?utm_source=openai))

`luc` should feel “self-extending” without a self-mutating core. The kernel stays small and boring; it discovers prompts, skills, themes, tools, and packages from known directories and swaps them in through runtime snapshots. Hot reload is a first-class feature, but it is a snapshot swap, not live mutation of running handlers. Bubble Tea’s own docs also recommend file logging for TUIs because stdout is occupied by the program, so `luc` should separate user-visible tool/event visualization from debug logs written to file. ([github.com](https://github.com/charmbracelet/bubbletea))

The first implementation should ship:
- TUI-first chat experience with low-flicker streaming
- OpenAI-compatible provider backend only
- Trusted local execution with no confirmation prompts
- Inline tool cards plus inspector pane
- Persistent project history across runs
- Git/GitHub package install flow
- Hybrid extension model:
  - `exec` tools: immediately reloadable
  - `go-build` tools: built to cached binaries, soft-reloadable after explicit rebuild
- Sub-agent-ready data model, but no sub-agent execution in the first shipped build

## Product Scope

### In Scope For First Build
- Single local agent session per TUI window
- Streaming assistant responses
- Built-in tools:
  - `read`
  - `write`
  - `edit`
  - `bash`
  - `list_tools`
- Project-local config, prompts, skills, themes, tools, packages, and history
- Package install from Git/GitHub refs
- File watching plus explicit `/reload`
- Explicit `/reload --rebuild` for rebuildable Go tool sources
- Durable sessions and project history
- Inline transcript + inspector workflow

### Out Of Scope For First Build
- Sub-agent execution/orchestration UI
- Remote registry service
- MCP
- Sandboxing and approvals
- In-process Go `plugin` loading
- Database storage
- Multi-window TUI
- Browser or GUI client

## Non-Negotiable Design Rules

1. Keep the core as a kernel, not a feature pile.
2. Any new capability must try to live as a skill, prompt, theme, tool, or package before it is allowed into core.
3. Do not load Go extensions with `plugin`; all tools run as subprocesses.
4. Do not hot-patch live handlers; build a new runtime snapshot and swap atomically.
5. Do not re-render full markdown on every token.
6. Do not log operational/debug output to stdout while the TUI is active. ([github.com](https://github.com/charmbracelet/bubbletea))

## Repo Layout

```text
cmd/luc/
internal/app/            // process bootstrap, subcommand dispatch
internal/tui/            // Bubble Tea root model and child models
internal/tui/transcript/ // transcript model, block cache, markdown pipeline
internal/tui/inspector/  // tool/log/context/package inspector panes
internal/kernel/         // session orchestration, turn loop, event bus
internal/provider/       // provider interface + OpenAI-compatible backend
internal/tools/          // built-in tools + external tool runner
internal/extensions/     // discovery, manifests, runtime snapshot assembly
internal/packages/       // git install, lockfile, asset materialization
internal/history/        // JSONL session/event persistence
internal/config/         // global + project config loading and merge
internal/watch/          // fsnotify watcher + debounce + reload trigger
internal/theme/          // Lip Gloss + Glamour theme compilation
internal/logging/        // file logger + in-memory ring buffer
internal/workspace/      // root detection, path policy, project identity
```

## CLI Surface

`luc` will use stdlib flag parsing with a thin custom dispatcher. Do not add Cobra.

Commands:
- `luc`
  - same as `luc tui`
- `luc tui`
  - launches the full-screen TUI
- `luc reload`
  - forces a soft reload
- `luc reload --rebuild`
  - rebuilds all stale `go-build` tools, then reloads
- `luc pkg add <source>`
  - installs a package from git/GitHub
- `luc pkg list`
  - lists installed packages
- `luc scaffold skill <name>`
- `luc scaffold tool <name> --runtime=exec|go-build`
- `luc scaffold package <name>`
- `luc doctor`
  - verifies `git`, workspace access, config, package state, provider env

Accepted package source syntax:
- `github:owner/repo@ref`
- `github:owner/repo/subdir@ref`
- full git URL with optional `@ref`

## Filesystem Layout

Project-local state lives in `.luc/`:

```text
.luc/
  config.yaml
  packages.lock.json
  prompts/
    system.md
  skills/
    *.md
  themes/
    default.yaml
  tools/
    <tool-name>/
      tool.yaml
      ...runtime files...
  packages/
    <pkg-name>@<version>/
      luc.package.yaml
      prompts/
      skills/
      themes/
      tools/
  history/
    project.json
    sessions/
      <session-id>.jsonl
      <session-id>.meta.json
  logs/
    luc.log
```

Global config lives at:
- `~/.config/luc/config.yaml`

Config precedence:
1. CLI flags
2. project `.luc/config.yaml`
3. global `~/.config/luc/config.yaml`
4. built-in defaults

## Core Runtime Model

### Runtime Snapshot

All runtime-loaded assets are assembled into an immutable snapshot:

```go
type RuntimeSnapshot struct {
    Config        ResolvedConfig
    Theme         CompiledTheme
    Prompts       PromptSet
    Skills        map[string]Skill
    Tools         map[string]ToolHandle
    Packages      map[string]InstalledPackage
    Provider      Provider
    Builtins      map[string]BuiltinTool
    Version       uint64
    LoadedAt      time.Time
}
```

Reload behavior:
- Build next snapshot in isolation
- Validate all manifests and references
- If valid, atomically swap `*RuntimeSnapshot`
- If invalid, keep current snapshot and surface errors in UI/logs
- In-flight turns keep the old snapshot
- New turns use the new snapshot

### Event Envelope

Everything in `luc` moves through one event stream:

```go
type EventEnvelope struct {
    Seq        uint64
    At         time.Time
    SessionID  string
    AgentID    string
    ParentTask string
    Kind       string
    Payload    json.RawMessage
}
```

Required event kinds:
- `session.started`
- `message.user`
- `message.assistant.delta`
- `message.assistant.final`
- `tool.requested`
- `tool.started`
- `tool.stdout`
- `tool.stderr`
- `tool.finished`
- `reload.started`
- `reload.finished`
- `reload.failed`
- `package.installed`
- `package.removed`
- `system.note`
- `system.error`

`AgentID` and `ParentTask` exist now so the persistence and UI model are sub-agent-ready without shipping sub-agents yet.

### Turn Loop

```go
type TurnRunner interface {
    Run(ctx context.Context, turn TurnInput) error
}
```

Concrete flow:
1. User submits prompt from TUI input
2. Kernel appends `message.user`
3. Provider stream starts
4. Provider events are normalized into envelopes
5. Tool calls execute immediately when requested
6. Tool outputs feed transcript cards + inspector
7. Final assistant message is committed as a completed transcript entry
8. All events append to session JSONL

## Provider Interface

Only OpenAI-compatible providers ship initially.

```go
type Provider interface {
    Name() string
    StartTurn(ctx context.Context, req TurnRequest) (TurnStream, error)
}

type TurnRequest struct {
    Model       string
    System      string
    Messages    []Message
    Tools       []ToolSpec
    Temperature float32
    MaxTokens   int
}

type TurnStream interface {
    Recv() (ProviderEvent, error)
    Close() error
}
```

`ProviderEvent.Kind` values:
- `assistant_text_delta`
- `assistant_done`
- `tool_call`
- `usage`
- `error`

Config shape:

```yaml
provider:
  kind: openai-compatible
  base_url: https://api.openai.com/v1
  model: gpt-4.1
  api_key_env: OPENAI_API_KEY
  temperature: 0.2
  max_tokens: 8192
```

Secrets are referenced by env var name only. No secret store in v1.

## Tool System

### Built-In Tools

Built-ins are core-owned and always available:
- `read`
- `write`
- `edit`
- `bash`
- `list_tools`

Policies:
- trusted local mode
- no confirmations
- default workspace root is detected project root if git exists, else current cwd
- `bash` runs with cwd rooted at workspace root unless explicitly overridden by the model/tool manifest
- write/edit tools may only target paths under workspace root in v1

### External Tool Manifest

```yaml
schema: luc.tool/v1
name: git_status
description: Show git status
runtime:
  kind: exec # exec | go-build
  command: ./git-status
  package: "" # only for go-build
input_schema:
  type: object
  properties: {}
  required: []
timeout: 30s
env: []
```

Runtime rules:
- `exec`: run the declared command directly
- `go-build`: build the declared Go package into `.luc/tools/<name>/.cache/<hash>/bin/<name>` and then execute that binary

Invocation protocol:
- stdin: JSON request
- stdout: JSON response
- stderr: captured separately for inspector/logs

Request:

```json
{
  "args": {},
  "context": {
    "cwd": "/abs/workspace",
    "session_id": "sess_123",
    "agent_id": "root",
    "tool_name": "git_status"
  }
}
```

Response:

```json
{
  "content": "working tree clean",
  "metadata": {
    "exit_code": 0
  }
}
```

### Hybrid Extension Policy

This is the chosen interpretation of “hybrid” while keeping the kernel minimal:
- `exec` tools are first-class and hot-reloadable
- `go-build` tools are still subprocess tools, but their binary is produced by `luc`
- source changes to `go-build` tools mark them `stale`
- stale tools are not auto-rebuilt during soft reload
- `luc reload --rebuild` or `/reload --rebuild` rebuilds all stale `go-build` tools, then swaps a new snapshot

This avoids in-process plugin complexity while still letting `luc` create Go-native extensions for itself.

## Packages

### Package Manifest

```yaml
schema: luc.package/v1
name: github.com/example/luc-git
version: 0.1.0
exports:
  prompts:
    - prompts/*.md
  skills:
    - skills/*.md
  themes:
    - themes/*.yaml
  tools:
    - tools/*/tool.yaml
```

Install flow:
1. Resolve source ref
2. Clone shallow into cache with `git`
3. Validate `luc.package.yaml`
4. Materialize exported assets into `.luc/packages/<name>@<version>/`
5. Update `.luc/packages.lock.json`
6. Trigger soft reload

Lock file shape:

```json
{
  "packages": [
    {
      "name": "github.com/example/luc-git",
      "version": "0.1.0",
      "source": "github:example/luc-git@main",
      "installed_at": "2026-04-18T00:00:00Z"
    }
  ]
}
```

No remote registry in v1. Git/GitHub is the package transport.

## Skills, Prompts, Themes

### Skill Format

Each skill is a markdown file with YAML front matter:

```md
---
name: git-triage
description: Analyze git status and propose next actions
tags: [git, workflow]
---

Use this skill when...
```

### Prompt Files
- `.luc/prompts/system.md` is the base system prompt
- package-provided prompt files append in deterministic order:
  1. base system
  2. installed package prompts
  3. project-local prompts

### Theme Files

Theme YAML must compile both:
- Lip Gloss styles for UI chrome
- Glamour style selection/options for markdown rendering

Use Glamour through a reusable term renderer and width-aware cache; Glamour explicitly supports custom renderers, styles, and environment-selected styles. ([github.com](https://github.com/charmbracelet/glamour?utm_source=openai))

## TUI Architecture

### Root Layout

Use one full-screen Bubble Tea program in alt-screen mode.

Layout:
- header
  - project path
  - provider/model
  - reload state
  - package count
  - trusted mode badge
- body
  - transcript pane
  - inspector pane
- footer
  - multiline input
  - help row
  - status line

Inspector placement:
- width `>= 140`: right-hand pane at 34% width
- width `< 140`: bottom drawer at 35% height, toggled open/closed

Inspector tabs:
- `Tool`
- `Output`
- `Logs`
- `Context`
- `Packages`

Use Bubble Tea as the single state/update loop, Bubbles `textarea` for input, `viewport` for transcript scrolling, and `help` for the footer key map. Bubbles’ viewport component also explicitly supports a high-performance mode for alt-screen applications, which aligns with the low-flicker requirement. ([github.com](https://github.com/charmbracelet/bubbletea?utm_source=openai))

### Root Model

```go
type RootModel struct {
    width       int
    height      int
    sessionID   string
    transcript  transcript.Model
    inspector   inspector.Model
    input       textarea.Model
    help        help.Model
    status      StatusModel
    runtime     *RuntimeHandle
    events      <-chan EventEnvelope
}
```

### Transcript Representation

Do not render transcript rows as a generic list. Use a block-based transcript model:

```go
type TranscriptBlock struct {
    ID          string
    Kind        string // user | assistant | tool-card | note
    RawMarkdown string
    Rendered    CachedRender
    State       string // streaming | done | failed
    Meta        map[string]string
}
```

Block kinds:
- user message
- assistant message
- tool card
- system note

Tool cards are compact in the transcript:
- tool name
- status
- elapsed time
- short summary

Full detail lives in inspector:
- args
- stdout
- stderr
- JSON result
- structured logs
- timing

### Low-Flicker Rendering Strategy

1. Keep one authoritative event store; views derive from it.
2. Coalesce provider token deltas to at most one UI update every `33ms`.
3. Do not invoke Glamour on every delta.
4. While an assistant message is streaming:
   - completed markdown blocks are rendered and cached
   - the unfinished tail is displayed as lightly wrapped plain text
5. Re-render the tail with Glamour only:
   - on `75ms` idle
   - on markdown block boundary
   - on final message completion
6. Cache rendered assistant blocks by:
   - content hash
   - width
   - theme version
7. On resize:
   - invalidate only width-sensitive caches
   - keep message model order and metadata intact
8. Auto-scroll only if the user is already at the bottom
9. If the user has scrolled up, preserve position during stream updates

This is the key implementation choice for “avoid flicker as much as possible.”

## Logging And Tool Visualization

Use `charmbracelet/log` for application and debug logs, configured with:
- JSON formatter for file output
- in-memory ring buffer mirror for inspector
- text formatting only for non-TUI/dev commands if needed

Because Bubble Tea occupies stdout during TUI execution, all debug logs go to `.luc/logs/luc.log`; the TUI inspector reads from the in-memory ring buffer, not from stdout scraping. Bubble Tea’s docs explicitly call out file logging for this case, and `charmbracelet/log` supports leveled structured output and JSON/text/logfmt formatting. ([github.com](https://github.com/charmbracelet/bubbletea))

## Hot Reload

### Triggers
- file watcher
- `/reload`
- `luc reload`
- `/reload --rebuild`
- `luc reload --rebuild`

### Watched Paths
- `.luc/config.yaml`
- `.luc/prompts/**`
- `.luc/skills/**`
- `.luc/themes/**`
- `.luc/tools/**/tool.yaml`
- `.luc/packages/**`

Watcher rules:
- debounce window: `150ms`
- collapse burst edits into one reload
- show reload state in header
- append `reload.*` events to history

Reload semantics:
- soft reload:
  - config
  - prompts
  - skills
  - themes
  - package assets
  - `exec` tool manifests
- rebuild reload:
  - everything above
  - compile stale `go-build` tools
  - then swap runtime snapshot

Failure handling:
- reload errors never kill the TUI
- failed reload leaves old snapshot active
- header shows `reload failed`
- inspector `Logs` and `Packages` tabs show diagnostics

## Persistence And History

Storage model:
- append-only JSONL event log per session
- metadata file per session
- project summary file for recent sessions

Session metadata:

```json
{
  "session_id": "sess_123",
  "project_id": "proj_abc",
  "created_at": "2026-04-18T00:00:00Z",
  "updated_at": "2026-04-18T00:05:00Z",
  "provider": "openai-compatible",
  "model": "gpt-4.1",
  "title": "Refactor extension loader"
}
```

Behavior:
- reopening `luc` restores the most recent session for the current project
- session switcher is project-scoped
- history includes user messages, assistant deltas/finals, tool events, reloads, and package installs
- raw event logs remain inspectable and grep-friendly

No SQLite in v1.

## Self-Extension Workflow

`luc` extends itself through ordinary assets and tools, not by rewriting the kernel.

Supported self-extension flows:
- agent creates a new skill markdown file
- agent creates a new prompt file
- agent scaffolds a new tool manifest
- agent scaffolds a new Go tool package
- user or agent runs `/reload` or `/reload --rebuild`
- new capability appears in the next runtime snapshot

Scaffold outputs:
- `luc scaffold skill foo`
  - `.luc/skills/foo.md`
- `luc scaffold tool git-status --runtime=exec`
  - `.luc/tools/git-status/tool.yaml`
  - `.luc/tools/git-status/README.md`
- `luc scaffold tool repo-map --runtime=go-build`
  - `.luc/tools/repo-map/tool.yaml`
  - `.luc/tools/repo-map/go.mod`
  - `.luc/tools/repo-map/main.go`
  - `.luc/tools/repo-map/main_test.go`
- `luc scaffold package my-pack`
  - `luc.package.yaml`
  - `skills/`
  - `prompts/`
  - `themes/`
  - `tools/`

## Delivery Phases

### Phase 1: Foundation
- initialize module `github.com/lutefd/luc`
- implement config loader
- workspace root detection
- file logger + ring buffer
- event envelope and JSONL session store
- provider interface + OpenAI-compatible backend
- built-in tools

Exit criteria:
- headless turn loop works
- events persist correctly
- tools execute and emit envelopes

### Phase 2: TUI Shell
- Bubble Tea root model
- textarea input
- transcript viewport
- inspector pane
- header/footer/help
- streaming assistant output
- inline tool cards

Exit criteria:
- interactive conversation works
- tool execution is visible inline and in inspector
- no stdout logging conflicts

### Phase 3: Markdown And Low-Flicker Pipeline
- transcript block model
- Glamour renderer integration
- render cache
- token coalescing
- resize invalidation
- auto-follow scroll logic

Exit criteria:
- long assistant outputs stream smoothly
- no full markdown rerender per token
- resize does not corrupt transcript state

### Phase 4: Packages And Reload
- package manifest parser
- git/GitHub install flow
- lock file
- fs watcher
- soft reload
- rebuild reload for `go-build` tools

Exit criteria:
- package install adds skills/tools without restart
- skill/prompt/theme edits reload automatically
- stale `go-build` tools rebuild on explicit command

### Phase 5: Persistence Polish
- project recent-session index
- restore last project session
- session switcher
- session titles

Exit criteria:
- close/reopen resumes project state
- history remains readable and consistent

### Phase 6: Sub-Agent-Ready Hardening
- keep current single-agent UX
- validate `AgentID`/`ParentTask` persistence and view model assumptions
- do not implement orchestration yet

Exit criteria:
- no refactor required later to add sub-agent event streams

## Important Public Interfaces And Formats

### Go Interfaces

```go
type Provider interface {
    Name() string
    StartTurn(ctx context.Context, req TurnRequest) (TurnStream, error)
}

type ToolRunner interface {
    Name() string
    Run(ctx context.Context, req ToolRequest) (ToolResult, error)
}

type SnapshotLoader interface {
    Load(ctx context.Context, ws Workspace) (*RuntimeSnapshot, error)
}

type SessionStore interface {
    Append(ctx context.Context, ev EventEnvelope) error
    Load(ctx context.Context, sessionID string) ([]EventEnvelope, error)
    RecentSessions(ctx context.Context, projectID string) ([]SessionMeta, error)
}
```

### Package Manifest
- `schema: luc.package/v1`
- required fields: `name`, `version`, `exports`

### Tool Manifest
- `schema: luc.tool/v1`
- required fields: `name`, `runtime.kind`, `input_schema`

### Skill File
- markdown with required front matter `name`, `description`

### Lock File
- `.luc/packages.lock.json`

## Testing And Acceptance

### Unit Tests
- config precedence merge
- workspace root detection
- package manifest validation
- tool manifest validation
- runtime snapshot assembly
- stale `go-build` detection
- event envelope serialization
- JSONL append/load behavior
- transcript render cache keying
- theme compilation

### Integration Tests
- OpenAI-compatible streaming turn with mocked provider
- built-in tool execution
- external `exec` tool execution
- `go-build` tool build + run
- package add from local git fixture
- soft reload atomic swap
- failed reload rollback
- project session restore

### TUI Tests
- root model init
- inspector toggle and pane placement on resize
- transcript auto-follow behavior
- transcript preserve-position behavior when user scrolls up
- inline tool card state transitions
- markdown cache invalidation on width/theme change

### End-To-End Scenarios
1. Start `luc` in a new repo, send a prompt, receive a streamed markdown answer.
2. Trigger a tool call; see compact transcript card plus full inspector details.
3. Edit a skill file; watcher reloads it; next turn uses updated skill.
4. Change a `go-build` tool source file; UI marks tool stale; `/reload --rebuild` makes it active.
5. Install a package from GitHub ref; package assets appear without app restart.
6. Break a theme or manifest; reload fails safely and previous snapshot stays active.
7. Close and reopen `luc`; last session for the current project restores.

### Performance Acceptance
- assistant token streaming never triggers full transcript rerender
- transcript remains responsive with:
  - 1,000 transcript blocks
  - 200 tool events
  - ongoing assistant stream
- resize operations complete without panic or layout corruption
- render cache hit rate is high after steady-state scrolling and message settle

## Assumptions And Defaults

- The repo starts empty and will be bootstrapped from scratch.
- Module path is `github.com/lutefd/luc`.
- Use latest stable non-pre-release versions of Bubble Tea, Bubbles, Lip Gloss, Glamour, `charmbracelet/log`, `fsnotify`, and `yaml.v3` at implementation time; do not adopt alpha/beta majors in the first build.
- `git` is an explicit runtime dependency for package install and package fixture tests.
- Trusted local mode is intentional: no confirmation UI in v1.
- OpenAI-compatible backend is the only shipped provider initially.
- Sub-agents are architecturally prepared for, but not implemented in the first build.
- Persistent history is local, file-based, and project-scoped.
- Packages install from git/GitHub refs only; no registry service.
- The TUI is the primary product shell; headless/print mode is deferred.
