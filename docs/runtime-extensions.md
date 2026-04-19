# Runtime Extensions

`luc` can now load several extension types at runtime without recompiling.

Lookup order:

1. Global user layer: `~/.luc/...`
2. Project override layer: `<workspace>/.luc/...`

If the same theme/tool/skill exists in both places, the project copy wins.

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

## Reloading

Changes are picked up on:

- app startup
- `luc reload`
- `ctrl+r` in the TUI

## Current Limits

These runtime surfaces work today without recompiling:

- tools
- skills
- themes
- system prompt

These still need core code changes today:

- custom providers
- custom TUI overlays/modals
- custom command-palette actions from external manifests
