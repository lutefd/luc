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
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
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
  short_description: "Create or update luc runtime tools, providers, skills, themes, and prompt overrides."
`,
		},
		{
			RelativePath: filepath.Join("skills", "runtime-extension-authoring", "SKILL.md"),
			Content: `---
name: runtime-extension-authoring
description: How luc expands itself at runtime through tools, providers, themes, prompts, and skills.
---
When the task is about extending luc, prefer runtime extension mechanisms before
proposing core code changes.

Use this lookup order:

1. Global base layer in ` + "`~/.luc/...`" + `
2. Project-local override in ` + "`<workspace>/.luc/...`" + `

Supported runtime extension types:

- Tools in ` + "`~/.luc/tools`" + ` and ` + "`<workspace>/.luc/tools`" + `
- Providers in ` + "`~/.luc/providers`" + ` and ` + "`<workspace>/.luc/providers`" + `
- Skills in ` + "`~/.luc/skills`" + `, ` + "`<workspace>/.luc/skills`" + `, ` + "`~/.agents/skills`" + `, and ` + "`<workspace>/.agents/skills`" + `
- Themes in ` + "`~/.luc/themes`" + ` and ` + "`<workspace>/.luc/themes`" + `
- System prompt overrides in ` + "`~/.luc/prompts/system.md`" + ` and ` + "`<workspace>/.luc/prompts/system.md`" + `

Rules:

- Prefer ` + "`~/.luc`" + ` when the capability should apply across projects.
- Use project ` + "`.luc`" + ` only for repo-specific overrides or specialized workflow.
- Do not suggest recompiling for tools, providers, skills, or themes unless runtime limits make that unavoidable.
- If creating a runtime tool, provide a manifest with ` + "`name`" + `, ` + "`description`" + `, ` + "`command`" + `, ` + "`schema`" + `, and optional ` + "`ui`" + `.
- If a tool should not flood the transcript, set ` + "`ui.default_collapsed: true`" + `.
- If creating a runtime provider, use either:
  ` + "`type: openai-compatible`" + ` with ` + "`id`" + `, ` + "`name`" + `, ` + "`base_url`" + `, optional ` + "`api_key_env`" + `, and ` + "`models`" + `; or
  ` + "`type: exec`" + ` with ` + "`id`" + `, ` + "`name`" + `, ` + "`command`" + `, optional ` + "`args`" + `, optional ` + "`env`" + `, and ` + "`models`" + `.
- For ` + "`type: exec`" + `, assume the adapter receives one JSON request on stdin and emits JSONL provider events on stdout.
- The adapter should translate upstream model/tool semantics into luc events; luc still executes the actual tools and renders the existing UI cards.
- If creating a runtime skill, treat ` + "`skill-name/SKILL.md`" + ` as the canonical instruction body.
- Use ` + "`skill-name/luc.yaml`" + ` only for metadata such as ` + "`interface.display_name`" + ` and ` + "`interface.short_description`" + `.
- If a skill needs bundled references or scripts, keep them in the same skill directory and assume they will be read through ` + "`read_skill_resource`" + `.
- If creating a runtime theme, inherit from ` + "`light`" + ` or ` + "`dark`" + ` and override only the necessary colors.
- Remind the user they can reload with ` + "`luc reload`" + ` or ` + "`ctrl+r`" + `.

Current limits:

- Runtime tools, providers, skills, themes, and prompts are supported.
- Custom runtime modals/overlays are not yet manifest-driven.
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
- When the model decides a skill is relevant, it calls ` + "`load_skill`" + ` with the skill name.
- ` + "`load_skill`" + ` returns the full ` + "`SKILL.md`" + ` body once per session, so luc does not reinject the body into the system prompt on every turn.
- If a loaded skill references bundled files, the model can fetch them with ` + "`read_skill_resource`" + `.
- Top-level standalone ` + "`.md`" + ` files still work as a compatibility fallback.

What this means in practice:

- Skills work well for workflow nudges, conventions, extension guidance, and domain-specific instructions.
- The model decides when to activate a skill based on the catalog, rather than luc doing keyword matching in the harness.
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
