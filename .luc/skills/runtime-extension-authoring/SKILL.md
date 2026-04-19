---
name: runtime-extension-authoring
description: How luc expands itself at runtime through tools, themes, prompts, and skills.
---
When the task is about extending luc, prefer runtime extension mechanisms before
proposing core code changes.

Use this lookup order:

1. Global base layer in `~/.luc/...`
2. Project-local override in `<workspace>/.luc/...`

Supported runtime extension types:

- Tools in `~/.luc/tools` and `<workspace>/.luc/tools`
- Skills in `~/.luc/skills`, `<workspace>/.luc/skills`, `~/.agents/skills`, and `<workspace>/.agents/skills`
- Themes in `~/.luc/themes` and `<workspace>/.luc/themes`
- System prompt overrides in `~/.luc/prompts/system.md` and `<workspace>/.luc/prompts/system.md`

Rules:

- Prefer `~/.luc` when the capability should apply across projects.
- Use project `.luc` only for repo-specific overrides or specialized workflow.
- Do not suggest recompiling for tools, skills, or themes unless runtime limits make that unavoidable.
- If creating a runtime tool, provide a manifest with `name`, `description`, `command`, `schema`, and optional `ui`.
- If a tool should not flood the transcript, set `ui.default_collapsed: true`.
- If creating a runtime skill, treat `skill-name/SKILL.md` as the canonical instruction body.
- Use `skill-name/luc.yaml` only for metadata such as `interface.display_name` and `interface.short_description`.
- If a skill needs bundled references or scripts, keep them in the same skill directory and assume they will be read through `read_skill_resource`.
- If creating a runtime theme, inherit from `light` or `dark` and override only the necessary colors.
- Remind the user they can reload with `luc reload` or `ctrl+r`.

Current limits:

- Runtime tools, skills, themes, and prompts are supported.
- Runtime providers and custom runtime modals/overlays are not yet manifest-driven.
