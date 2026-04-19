---
name: skill-usage
description: How luc currently uses runtime skills and what its limits are.
---
luc does know how to use runtime skills, but the behavior is currently simple.

What happens now:

- Skills are discovered from `~/.luc/skills`, `<workspace>/.luc/skills`, `~/.agents/skills`, and `<workspace>/.agents/skills`, with project-local overrides winning.
- Preferred skill shape is `skill-name/SKILL.md`.
- `luc.yaml` is optional metadata for the skill package, mainly `interface.display_name` and `interface.short_description`.
- On each user request, luc sends a compact skill catalog in the system prompt rather than preloading full skill bodies.
- When the model decides a skill is relevant, it calls `load_skill` with the skill name.
- `load_skill` returns the full `SKILL.md` body once per session, so luc does not reinject the body into the system prompt on every turn.
- If a loaded skill references bundled files, the model can fetch them with `read_skill_resource`.
- Top-level standalone `.md` files still work as a compatibility fallback.

What this means in practice:

- Skills work well for workflow nudges, conventions, extension guidance, and domain-specific instructions.
- The model decides when to activate a skill based on the catalog, rather than luc doing keyword matching in the harness.
- Once activated, the skill content stays in the conversation history as a tool result.
- Skills are not shown in the UI yet.
- Skills are not yet individually toggled on/off per session.

When explaining this to a user, be explicit that skill support exists today but
uses progressive disclosure: catalog first, full `SKILL.md` only on activation.
