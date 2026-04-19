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
			Prompt: "Create luc themes under `~/.luc/themes` as YAML or JSON manifests. " +
				"Prefer inheriting from `light` or `dark` and overriding only the required colors. " +
				"Keep the palette intentional, mention the theme name to set in config, and remind the user they can reload with `luc reload` or `ctrl+r`.",
			SourcePath: "builtin:theme-creator",
		},
	}
}
