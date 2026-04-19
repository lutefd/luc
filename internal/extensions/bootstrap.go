package extensions

import (
	"errors"
	"os"
	"path/filepath"
)

type bundledRuntimeAsset struct {
	RelativePath string
	Content      string
}

func EnsureGlobalRuntime() error {
	root, err := configRoot()
	if err != nil {
		return err
	}

	dirs := []string{
		root,
		filepath.Join(root, "tools"),
		filepath.Join(root, "providers"),
		filepath.Join(root, "ui"),
		filepath.Join(root, "hooks"),
		filepath.Join(root, "skills"),
		filepath.Join(root, "themes"),
		filepath.Join(root, "prompts"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	for _, asset := range bundledRuntimeAssets() {
		path := filepath.Join(root, asset.RelativePath)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(asset.Content), 0o644); err != nil {
			return err
		}
	}

	return nil
}

func bundledRuntimeAssets() []bundledRuntimeAsset {
	return []bundledRuntimeAsset{
		{
			RelativePath: filepath.Join("skills", "runtime-extension-authoring", "luc.yaml"),
			Content: `interface:
  display_name: "Runtime Extension Authoring"
  short_description: "Use when asked to create or modify luc extensions, runtime capabilities, plugins, UI tabs/pages, hooks, tools, providers, themes, prompts, or other host features without editing core code first."
triggers:
  - extension
  - runtime extension
  - plugin
  - capability
  - runtime capability
  - ui manifest
  - view
  - runtime view
  - inspector view
  - inspector tab
  - tab
  - panel
  - page view
  - hook
  - provider
  - tool
  - theme
  - prompt override
  - overview tab
`,
		},
		{
			RelativePath: filepath.Join("skills", "runtime-extension-authoring", "SKILL.md"),
			Content: `---
name: runtime-extension-authoring
description: How luc expands itself at runtime through tools, providers, UI, hooks, themes, prompts, and skills.
---
When the task is about extending luc, prefer runtime extension mechanisms before
proposing core code changes.

Use this lookup order:

1. Global base layer in ` + "`~/.luc/...`" + `
2. Project-local override in ` + "`<workspace>/.luc/...`" + `

Supported runtime extension types:

- Tools in ` + "`~/.luc/tools`" + ` and ` + "`<workspace>/.luc/tools`" + `
- Providers in ` + "`~/.luc/providers`" + ` and ` + "`<workspace>/.luc/providers`" + `
- UI manifests in ` + "`~/.luc/ui`" + `, ` + "`<workspace>/.luc/packages/*/ui`" + `, and ` + "`<workspace>/.luc/ui`" + `
- Hook manifests in ` + "`~/.luc/hooks`" + `, ` + "`<workspace>/.luc/packages/*/hooks`" + `, and ` + "`<workspace>/.luc/hooks`" + `
- Skills in ` + "`~/.luc/skills`" + `, ` + "`<workspace>/.luc/skills`" + `, ` + "`~/.agents/skills`" + `, and ` + "`<workspace>/.agents/skills`" + `
- Themes in ` + "`~/.luc/themes`" + ` and ` + "`<workspace>/.luc/themes`" + `
- System prompt overrides in ` + "`~/.luc/prompts/system.md`" + ` and ` + "`<workspace>/.luc/prompts/system.md`" + `

Authoring workflow:

1. Decide which runtime surfaces own the capability.
2. Prefer ` + "`~/.luc`" + ` when the capability should apply across projects.
3. Use project ` + "`.luc`" + ` only for repo-specific overrides or specialized workflow.
4. Compose host-owned runtime surfaces instead of pushing behavior into core:
   - tools/providers/hooks handle execution and protocol translation
   - UI manifests handle commands, views, and approval policies
   - skills/prompts teach the model how to use the new capability
5. Remind the user they can reload with ` + "`luc reload`" + ` or ` + "`ctrl+r`" + `.

Rules:

- Do not suggest recompiling for tools, providers, skills, themes, UI, or hooks unless runtime limits make that unavoidable.
- For a simple shell-style runtime tool, provide ` + "`name`" + `, ` + "`description`" + `, ` + "`command`" + `, ` + "`schema`" + `, and optional ` + "`ui`" + `.
- For a capability-enabled tool, use ` + "`schema: luc.tool/v1`" + ` with ` + "`runtime.kind: exec`" + `, optional ` + "`runtime.capabilities`" + `, and ` + "`input_schema`" + `.
- ` + "`structured_io`" + ` means luc writes a JSON request envelope to stdin and expects JSONL events on stdout.
- ` + "`client_actions`" + ` means the tool or provider may emit host-owned ` + "`client_action`" + ` events and receive ` + "`client_result`" + ` responses.
- Capability-enabled tools and providers should be paired with ` + "`luc.ui/v1`" + ` manifests when they need reusable commands, inspector/page views, or approval policy wiring.
- Runtime UI stays host-owned in this slice. Views are declarative and read-only.
- luc does support creating brand-new runtime views with ` + "`luc.ui/v1`" + `. New ` + "`inspector_tab`" + ` and ` + "`page`" + ` views are valid runtime extension targets.
- Built-in inspector tabs such as ` + "`Overview`" + ` are core-owned. Runtime UI can add new ` + "`inspector_tab`" + ` or ` + "`page`" + ` views, but cannot inject a new field directly into the built-in ` + "`Overview`" + ` tab.
- If the user asks for an overview/sidebar addition, prefer a new runtime ` + "`inspector_tab`" + ` or ` + "`page`" + ` as the supported extension path. Only edit core TUI code when they explicitly want to change the built-in ` + "`Overview`" + ` implementation itself.
- If creating a runtime provider, use either:
  ` + "`type: openai-compatible`" + ` with ` + "`id`" + `, ` + "`name`" + `, ` + "`base_url`" + `, optional ` + "`api_key_env`" + `, and ` + "`models`" + `; or
  ` + "`type: exec`" + ` with ` + "`id`" + `, ` + "`name`" + `, ` + "`command`" + `, optional ` + "`args`" + `, optional ` + "`env`" + `, optional ` + "`capabilities`" + `, and ` + "`models`" + `.
- For ` + "`type: exec`" + ` providers, assume the adapter receives one JSON request on stdin and emits JSONL provider events on stdout. The adapter translates upstream API semantics into luc provider events; luc still executes the actual tools and renders the UI cards.
- If creating hooks, use ` + "`luc.hook/v1`" + ` with ` + "`runtime.kind: exec`" + ` and optional ` + "`runtime.capabilities`" + `; hooks are async side effects over stdio, not turn-loop mutations.
- If creating a runtime skill, treat ` + "`skill-name/SKILL.md`" + ` as the canonical instruction body.
- Use ` + "`skill-name/luc.yaml`" + ` only for metadata such as ` + "`interface.display_name`" + ` and ` + "`interface.short_description`" + `.
- If a skill needs bundled references or scripts, keep them in the same skill directory and assume they will be read through ` + "`read_skill_resource`" + `.
- If creating a runtime theme, inherit from ` + "`light`" + ` or ` + "`dark`" + ` and override only the necessary colors.

Read bundled references when you need exact manifest shapes or end-to-end composition:

- ` + "`references/capability-tools.md`" + ` for ` + "`luc.tool/v1`" + `, ` + "`structured_io`" + `, ` + "`client_actions`" + `, and tool request envelopes.
- ` + "`references/provider-ui-composition.md`" + ` for ` + "`type: exec`" + ` providers, host capabilities, and matching ` + "`luc.ui/v1`" + ` manifests.
- ` + "`references/runtime-ui-views.md`" + ` for new runtime ` + "`inspector_tab`" + ` and ` + "`page`" + ` views with concrete generic examples.
- ` + "`references/runtime-ui-actions.md`" + ` for host-owned UI actions such as ` + "`modal.open`" + `, ` + "`confirm.request`" + `, ` + "`view.open`" + `, ` + "`view.refresh`" + `, and ` + "`command.run`" + `.
- ` + "`references/hook-patterns.md`" + ` for ` + "`luc.hook/v1`" + ` manifests and async event delivery.

Current limits:

- Runtime tools, providers, UI manifests, hooks, skills, themes, and prompts are supported.
- Runtime views are read-only in this slice.
`,
		},
		{
			RelativePath: filepath.Join("skills", "runtime-extension-authoring", "references", "capability-tools.md"),
			Content: `# Capability-Enabled Tools

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

- Use ` + "`runtime.kind: exec`" + ` for capability-enabled tools.
- ` + "`structured_io`" + ` means luc sends one JSON request envelope on stdin and expects JSONL events on stdout.
- ` + "`client_actions`" + ` allows the tool to ask the host to open views, request confirmation, or run host-owned commands.
- Prefer ` + "`input_schema`" + ` instead of legacy ` + "`schema`" + ` for ` + "`luc.tool/v1`" + ` tools.
- Use ` + "`ui.default_collapsed: true`" + ` when the tool would otherwise spam the transcript.

Structured request envelope includes:

- ` + "`tool_name`" + `
- typed ` + "`arguments`" + `
- ` + "`workspace`" + `
- ` + "`session_id`" + `
- ` + "`agent_id`" + `
- ` + "`host_capabilities`" + `
- optional ` + "`view_context`" + `

Typical stdout event flow:

1. ` + "`progress`" + ` or ` + "`log`" + ` while work runs
2. optional ` + "`client_action`" + ` when the tool needs host UI
3. ` + "`result`" + ` with ` + "`result.content`" + ` and optional metadata
4. ` + "`done`" + ` to terminate the stream

When the tool needs a persistent inspector or page, also create a matching ` + "`luc.ui/v1`" + ` manifest instead of baking UI rules into the tool itself.
`,
		},
		{
			RelativePath: filepath.Join("skills", "runtime-extension-authoring", "references", "provider-ui-composition.md"),
			Content: `# Provider and UI Composition

Use this pattern when a capability spans an exec provider plus host-owned runtime UI.

Example exec provider:

~~~yaml
id: meli
name: Meli Gateway
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

- ` + "`type: exec`" + ` providers receive one JSON request on stdin and emit JSONL provider events on stdout.
- Provider events can stream ` + "`thinking`" + `, ` + "`text_delta`" + `, ` + "`tool_call`" + `, and ` + "`done`" + `.
- If the provider declares ` + "`capabilities: [client_actions]`" + `, luc includes ` + "`host_capabilities`" + ` in the request and the adapter may emit ` + "`client_action`" + ` events.
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
- Put reusable commands, views, and approval policies in ` + "`luc.ui/v1`" + `.
- Use ` + "`view.open`" + ` or ` + "`view.refresh`" + ` for persistent host-owned views.
- If the workflow needs explicit confirmation, use approval policies or ` + "`confirm.request`" + ` client actions.
- Keep runtime views declarative and read-only in this slice.
`,
		},
		{
			RelativePath: filepath.Join("skills", "runtime-extension-authoring", "references", "runtime-ui-views.md"),
			Content: `# Runtime UI Views

Yes: luc supports creating brand-new runtime views with ` + "`luc.ui/v1`" + `.

Supported placements in this slice:

- ` + "`inspector_tab`" + ` for a new tab in the inspector
- ` + "`page`" + ` for a dedicated runtime page

Important limit:

- You can add a new runtime view such as ` + "`activity.summary`" + `.
- You cannot inject a new field directly into the built-in ` + "`Overview`" + ` tab through runtime manifests.

Concrete inspector-tab example:

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

Use this when the user asks for:

- a new inspector tab
- a new inspector panel
- a custom runtime view
- a sidebar-adjacent runtime UI surface

If they ask for "put this in Overview", translate that to one of two paths:

1. runtime extension path: add a new ` + "`inspector_tab`" + ` or ` + "`page`" + `
2. core-code path: only if they explicitly want to alter the built-in ` + "`Overview`" + ` implementation
`,
		},
		{
			RelativePath: filepath.Join("skills", "runtime-extension-authoring", "references", "runtime-ui-actions.md"),
			Content: `# Runtime UI Actions

Runtime UI actions are host-owned. Tools and providers can request them through ` + "`client_action`" + ` events, and ` + "`luc.ui/v1`" + ` command manifests can trigger some of the same actions declaratively.

Supported action kinds in this slice:

- ` + "`modal.open`" + `
- ` + "`confirm.request`" + `
- ` + "`view.open`" + `
- ` + "`view.refresh`" + `
- ` + "`command.run`" + `

When to use each action:

- Use ` + "`modal.open`" + ` for a host-rendered modal dialog.
- Use ` + "`confirm.request`" + ` when the tool or provider needs an explicit user decision.
- Use ` + "`view.open`" + ` to open a runtime view declared in ` + "`luc.ui/v1`" + `.
- Use ` + "`view.refresh`" + ` to rerun the active runtime view's ` + "`source_tool`" + `.
- Use ` + "`command.run`" + ` to trigger another registered runtime command by ID.

Blocking confirmation example from a structured tool or provider:

~~~json
{"type":"client_action","action":{
  "id":"confirm_1",
  "kind":"confirm.request",
  "blocking":true,
  "title":"Reset activity counters?",
  "body":"This clears the current activity summary state.",
  "options":[
    {"id":"reset","label":"Reset","primary":true},
    {"id":"cancel","label":"Cancel"}
  ]
}}
~~~

Open a runtime view from a command manifest:

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

Refresh the active runtime view from a command manifest:

~~~yaml
commands:
  - id: activity.summary.refresh
    name: Refresh activity summary
    action:
      kind: view.refresh
      view_id: activity.summary
~~~

Run another command from a command manifest:

~~~yaml
commands:
  - id: activity.summary.reset.confirm
    name: Reset activity summary
    action:
      kind: command.run
      command_id: activity.summary.reset
~~~

Rules:

- Keep view definitions in ` + "`luc.ui/v1`" + `; use actions to open or refresh them.
- Use ` + "`confirm.request`" + ` instead of inventing your own approval UI.
- Use ` + "`modal.open`" + ` only for host-owned modal content; do not assume custom freeform modal rendering unless the host already supports the requested payload shape.
- If the flow depends on the user response, mark the action as blocking and expect a ` + "`client_result`" + ` envelope back over stdin/stdout.
`,
		},
		{
			RelativePath: filepath.Join("skills", "runtime-extension-authoring", "references", "hook-patterns.md"),
			Content: `# Hook Patterns

Use hooks for async side effects driven by live luc events. Do not use hooks to mutate the active turn loop.

Example:

~~~yaml
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
~~~

Hook rules:

- Hooks subscribe to live events only; they do not run during history replay or session reopen.
- Stable hook subscription event kinds today are ` + "`message.assistant.final`" + ` and ` + "`tool.finished`" + `.
- Hook request envelopes include the triggering event, workspace/session metadata, and ` + "`host_capabilities`" + `. The current JSON shape is:

~~~json
{
  "event": {"kind": "tool.finished", "payload": {"id": "call_1", "name": "bash"}},
  "workspace": {"root": "/abs/workspace", "project_id": "repo", "branch": "main"},
  "session": {"session_id": "sess_123", "provider": "openai", "model": "gpt-5.4"},
  "host_capabilities": ["hooks.live_events"]
}
~~~

- Supported stdout event types are ` + "`log`" + `, ` + "`progress`" + `, ` + "`done`" + `, and ` + "`error`" + `.
- Hook stdout field names are exact: ` + "`log`" + ` uses ` + "`text`" + ` (luc also accepts ` + "`message`" + ` as a compatibility alias), ` + "`progress`" + ` uses ` + "`progress`" + ` (or ` + "`message`" + `), ` + "`error`" + ` uses ` + "`error`" + ` (or ` + "`message`" + `), and ` + "`done`" + ` should set ` + "`done: true`" + `.
- Hook failures should be surfaced as diagnostics, not treated as fatal session errors.
- When a hook needs interactive host UI, prefer moving that behavior into a tool or provider plus ` + "`luc.ui/v1`" + ` instead of stretching hook responsibilities.
`,
		},
		{
			RelativePath: filepath.Join("skills", "skill-usage", "luc.yaml"),
			Content: `interface:
  display_name: "Skill Usage"
  short_description: "Explain how luc discovers, catalogs, loads, and reuses skills."
`,
		},
		{
			RelativePath: filepath.Join("skills", "skill-usage", "SKILL.md"),
			Content: `---
name: skill-usage
description: How luc currently uses runtime skills and what its limits are.
---
luc does know how to use runtime skills, but the behavior is currently simple.

What happens now:

- Skills are discovered from ` + "`~/.luc/skills`" + `, ` + "`<workspace>/.luc/skills`" + `, ` + "`~/.agents/skills`" + `, and ` + "`<workspace>/.agents/skills`" + `, with project-local overrides winning.
- Preferred skill shape is ` + "`skill-name/SKILL.md`" + `.
- ` + "`luc.yaml`" + ` is optional metadata for the skill package, mainly ` + "`interface.display_name`" + ` and ` + "`interface.short_description`" + `.
- On each user request, luc sends a compact skill catalog in the system prompt rather than preloading full skill bodies.
- If a skill declares ` + "`triggers`" + `, luc may also add a short "likely relevant skills" hint to the system prompt for matching requests.
- When the model decides a skill is relevant, it calls ` + "`load_skill`" + ` with the skill name.
- ` + "`load_skill`" + ` returns the full ` + "`SKILL.md`" + ` body once per session, so luc does not reinject the body into the system prompt on every turn.
- If a loaded skill references bundled files, the model can fetch them with ` + "`read_skill_resource`" + `.
- Top-level standalone ` + "`.md`" + ` files still work as a compatibility fallback.

What this means in practice:

- Skills work well for workflow nudges, conventions, extension guidance, and domain-specific instructions.
- The model still decides when to call ` + "`load_skill`" + `, but luc can bias discovery with trigger-based "likely relevant skills" hints when a skill manifest declares ` + "`triggers`" + `.
- Once activated, the skill content stays in the conversation history as a tool result.
- Skills are not shown in the UI yet.
- Skills are not yet individually toggled on/off per session.

When explaining this to a user, be explicit that skill support exists today but
uses progressive disclosure: catalog first, full ` + "`SKILL.md`" + ` only on activation.
`,
		},
		{
			RelativePath: filepath.Join("skills", "theme-creator", "luc.yaml"),
			Content: `interface:
  display_name: "Theme Creator"
  short_description: "Create or update luc themes that can be inserted at runtime."
`,
		},
		{
			RelativePath: filepath.Join("skills", "theme-creator", "SKILL.md"),
			Content: `---
name: theme-creator
description: Create or update luc themes that can be inserted at runtime.
---
Create luc themes as YAML or JSON manifests.

Location:

- Prefer ` + "`~/.luc/themes/<name>.yaml`" + ` for user-wide themes.
- Use ` + "`<workspace>/.luc/themes/<name>.yaml`" + ` only when the user wants a project-local override.
- Workspace themes override home themes with the same name.
- The filename without the extension is the theme ID users select in luc.

Manifest shape:

- Required: ` + "`inherits: light`" + ` or ` + "`inherits: dark`" + `.
- Add a ` + "`colors`" + ` map and override only the keys that need to change.
- Available color keys:
  ` + "`bg`, `panel`, `panel_alt`, `line`, `accent`, `accent_alt`, `text`, `muted`, `subtle`, `success`, `warn`, `blue`, `cyan`, `error_text`, `diff_add_bg`, `diff_add_fg`, `diff_del_bg`, `diff_del_fg`" + `
- Colors should be hex strings such as ` + "`#RRGGBB`" + `.

Rendering notes:

- ` + "`bg`" + ` is applied to the terminal background, so it is the dominant canvas color.
- Most UI surfaces use foreground colors only, which means the background shows through.
- ` + "`accent`" + ` is the main interactive highlight color and matters a lot for active selections.
- Diff colors need to contrast against each other because they render with explicit backgrounds.
- Avoid trying to fake separate panel backgrounds everywhere; keep the palette coherent instead.

Checklist:

- ` + "`text`" + ` contrasts clearly against ` + "`bg`" + `.
- ` + "`muted`" + ` and ` + "`subtle`" + ` are still readable.
- ` + "`accent`" + ` works both as text and as a highlight background.
- Diff colors remain readable.
- Warning and error colors remain legible on the chosen background.

Activation:

- Users can switch themes at runtime through the theme switcher.
- ` + "`luc reload`" + ` or ` + "`ctrl+r`" + ` picks up changes to an existing theme file.
- To persist a theme across launches, set ` + "`ui.theme: <name>`" + ` in ` + "`~/.config/luc/config.yaml`" + ` or the workspace ` + "`.luc/config.yaml`" + `.

When creating a theme for the user:

- Prefer the global ` + "`~/.luc/themes`" + ` layer unless they explicitly want a project-specific override.
- Keep the palette intentional instead of changing every token by default.
- Mention the theme name they should select or persist in config.
`,
		},
	}
}
