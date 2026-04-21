---
name: theme-creator
description: Create or update luc themes that can be inserted at runtime.
---
Create luc themes as YAML or JSON manifests.

Location:

- Prefer `~/.luc/themes/<name>.yaml` for a user-wide hand-authored theme.
- Use `luc pkg install` when the theme should ship as a reusable package; packaged themes are discovered from `~/.luc/packages/*/themes` and `<workspace>/.luc/packages/*/themes`.
- Use `<workspace>/.luc/themes/<name>.yaml` only when the user wants a project-local override.
- Later layers win: project-local theme files override package-installed themes, and project package themes override user-scope themes with the same name.
- The filename without the extension is the theme ID users select in luc.

Manifest shape:

- Required: `inherits: light` or `inherits: dark`.
- Add a `colors` map and override only the keys that need to change.
- Available color keys:
  `bg`, `panel`, `panel_alt`, `line`, `accent`, `accent_alt`, `text`, `muted`, `subtle`, `success`, `warn`, `blue`, `cyan`, `error_text`, `diff_add_bg`, `diff_add_fg`, `diff_del_bg`, `diff_del_fg`
- Colors should be hex strings such as `#RRGGBB`.

Rendering notes:

- `bg` is applied to the terminal background, so it is the dominant canvas color.
- Most UI surfaces use foreground colors only, which means the background shows through.
- `accent` is the main interactive highlight color and matters a lot for active selections.
- Diff colors need to contrast against each other because they render with explicit backgrounds.
- Avoid trying to fake separate panel backgrounds everywhere; keep the palette coherent instead.

Checklist:

- `text` contrasts clearly against `bg`.
- `muted` and `subtle` are still readable.
- `accent` works both as text and as a highlight background.
- Diff colors remain readable.
- Warning and error colors remain legible on the chosen background.

Activation:

- Users can switch themes at runtime through the theme switcher.
- `luc reload` or `ctrl+r` picks up changes to an existing theme file.
- To persist a theme across launches, set `ui.theme: <name>` in `~/.config/luc/config.yaml` or the workspace `.luc/config.yaml`.

When creating a theme for the user:

- Prefer the global `~/.luc/themes` layer for direct local authoring unless they explicitly want a project-specific override or a packaged distribution path.
- Keep the palette intentional instead of changing every token by default.
- Mention the theme name they should select or persist in config.
