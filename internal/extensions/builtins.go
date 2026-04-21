package extensions

func builtinSkills() []Skill {
	return []Skill{
		{
			Name:        "skill-creator",
			DisplayName: "Skill Creator",
			Description: "Create or update luc skills using the runtime skill package format.",
			Prompt: "Create or update luc skills using `~/.luc/skills/<skill-name>/luc.yaml` as the preferred shape. " +
				"Keep the manifest minimal with `interface.display_name`, `interface.short_description`, and optional `interface.default_prompt`. " +
				"Only add `SKILL.md` when the skill needs deeper procedural instructions or bundled references. " +
				"Prefer the global `~/.luc` layer unless the user explicitly wants a project-local override, and remind them they can reload with `luc reload` or `ctrl+r`.",
			SourcePath: "builtin:skill-creator",
		},
		{
			Name:        "plugin-creator",
			DisplayName: "Plugin Creator",
			Description: "Create luc plugins and runtime extensions without recompiling core code.",
			Prompt: "When the task is about extending luc, prefer runtime extension mechanisms before changing core code. " +
				"Use `~/.luc` as the main install surface and project `.luc` only as an override. " +
				"Guide the user toward runtime tools, skills, themes, or prompt overrides first, and be explicit about any current limits for true runtime plugins or overlays.",
			SourcePath: "builtin:plugin-creator",
		},
		{
			Name:        "theme-creator",
			DisplayName: "Theme Creator",
			Description: "Create or update luc themes that can be inserted at runtime.",
			Prompt: `Create luc themes as YAML or JSON manifests. Follow these rules:

LOCATION
- Prefer ` + "`~/.luc/themes/<name>.yaml`" + ` for a user-wide hand-authored theme.
- Use ` + "`luc pkg install`" + ` when the theme should ship as a reusable package; packaged themes are discovered from ` + "`~/.luc/packages/*/themes`" + ` and ` + "`<workspace>/.luc/packages/*/themes`" + `.
- Use ` + "`<workspace>/.luc/themes/<name>.yaml`" + ` only when the user wants a project-local override. Project-local theme files take precedence over package-installed and home themes with the same name.
- The filename (sans extension) is the theme ID used to select it.

MANIFEST SHAPE
- Required: ` + "`inherits: light|dark`" + ` — sets the base palette the custom overrides merge onto.
- ` + "`colors`" + ` map overrides only the keys you care about; unlisted keys fall through to the inherited variant.
- Available color keys: ` + "`bg`, `panel`, `panel_alt`, `line`, `accent`, `accent_alt`, `text`, `muted`, `subtle`, `success`, `warn`, `blue`, `cyan`, `error_text`, `diff_add_bg`, `diff_add_fg`, `diff_del_bg`, `diff_del_fg`" + `.
- Colors must be hex strings ("#RRGGBB"). Empty values are treated as "not overridden".

HOW THE RENDERING WORKS (so you choose colors that actually show up)
- ` + "`bg`" + ` is applied to the terminal itself via OSC 11 (tea.View.BackgroundColor). It paints EVERY cell of the alt-screen that no style explicitly overrides. Pick a ` + "`bg`" + ` that reads well with ` + "`text`" + ` — it is the dominant surface color.
- Every text surface (messages, labels, status, header, footer, input, sidebar, palette frame) is rendered with ONLY a foreground color. No per-cell background painting. The OSC 11 canvas shows through.
- The ONLY surfaces that paint their own background are genuine highlights: ` + "`PaletteActive`" + ` (selection) uses ` + "`accent`" + ` as bg with ` + "`bg`" + ` as fg; ` + "`DiffAdd`/`DiffDel`" + ` use their dedicated diff colors. If you want your theme to feel "alive", ` + "`accent`" + ` is the color you style most aggressively.
- Never try to force a different "panel" or container color by mixing OSC 11 with per-cell Background — terminals render OSC 11 and SGR 48;2 through different paths, and identical hex comes out as visibly different shades. Bands result. If you want the sidebar or palette to contrast, change their foreground role colors, not their backgrounds.

CHECKLIST BEFORE SAVING
- ` + "`text`" + ` vs ` + "`bg`" + ` contrast passes WCAG AA (4.5:1 for body text).
- ` + "`muted`" + ` and ` + "`subtle`" + ` are dimmer than ` + "`text`" + ` but still readable — they're used heavily in the footer, hints, and inspector labels.
- ` + "`accent`" + ` contrasts with ` + "`bg`" + ` AND with itself-as-bg (selection text uses ` + "`bg`" + ` color on ` + "`accent`" + ` background).
- ` + "`diff_add_bg`/`diff_add_fg`" + ` contrast with each other; same for del.
- ` + "`error_text`" + ` and ` + "`warn`" + ` contrast with ` + "`bg`" + `.

ACTIVATION
- Switch at runtime via ` + "`ctrl+p`" + ` → "Switch theme…" (theme.switch command) — the new theme takes effect immediately for the current session.
- ` + "`ctrl+p`" + ` → "Reset theme to default" (theme.reset) restores the built-in ` + "`light`" + ` theme.
- Runtime switches are NOT persisted. For the theme to stick across launches, set ` + "`ui.theme: <name>`" + ` in ` + "`~/.config/luc/config.yaml`" + ` or the workspace's ` + "`.luc/config.yaml`" + `.
- ` + "`luc reload`" + ` or ` + "`ctrl+r`" + ` picks up edits to an already-active theme file.`,
			SourcePath: "builtin:theme-creator",
		},
	}
}
