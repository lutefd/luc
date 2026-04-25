package kernel

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lutefd/luc/internal/extensions"
	"github.com/lutefd/luc/internal/provider"
	"github.com/lutefd/luc/internal/tools"
)

const defaultSystemPrompt = "You are luc, the local coding agent running inside luc for this workspace. Use luc tools to inspect files, edit code, and run commands instead of guessing. Be concise, prefer the smallest correct change, and verify important changes with targeted tool calls. Stay anchored to the user's stated behavior: find the smallest owner of that behavior and fix it there. Do not traverse call graphs or inspect related files unless needed to make the fix safe, update callers, or resolve a failing test. If the user clarifies intent, immediately abandon the prior path and re-scope around the clarified invariant."

func (c *Controller) loadSystemPrompt() string {
	base := defaultSystemPrompt
	paths := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".luc", "prompts", "system.md"))
	}
	paths = append(paths, filepath.Join(c.workspace.StateDir, "prompts", "system.md"))

	content := base
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if candidate := strings.TrimSpace(string(data)); candidate != "" {
			content = candidate
		}
	}
	return content
}

func (c *Controller) composeSystemPrompt(input string) (string, error) {
	var builder strings.Builder
	builder.WriteString(c.systemPrompt)
	if summary := strings.TrimSpace(c.compactionSummary); summary != "" {
		builder.WriteString("\n\nSession summary from earlier compacted context:\n")
		builder.WriteString(summary)
	}
	if loaded := strings.TrimSpace(c.loadedSkillPromptBlock()); loaded != "" {
		builder.WriteString("\n\n")
		builder.WriteString(loaded)
	}
	for _, ext := range c.matchingPromptExtensions() {
		builder.WriteString("\n\n")
		builder.WriteString(ext.Prompt)
	}
	if relevant := c.relevantSkills(input); len(relevant) > 0 {
		builder.WriteString("\n\nLikely relevant skills for this request:\n")
		builder.WriteString("Before editing luc core code or this repo for luc itself, load the most relevant skill first when the task is about extending luc or adding a runtime capability.\n")
		if c.hasRelevantSkill(relevant, "runtime-extension-authoring") {
			builder.WriteString("luc does support runtime UI manifests via `luc.ui/v1`. New runtime `inspector_tab` and `page` views are supported; only the built-in `Overview` tab remains core-owned.\n")
		}
		for _, skill := range relevant {
			builder.WriteString("- ")
			builder.WriteString(skill.Name)
			label := strings.TrimSpace(skill.DisplayName)
			if label != "" && label != skill.Name {
				builder.WriteString(" (")
				builder.WriteString(label)
				builder.WriteString(")")
			}
			if desc := strings.TrimSpace(skill.Description); desc != "" {
				builder.WriteString(": ")
				builder.WriteString(desc)
			}
			builder.WriteString("\n")
		}
		builder.WriteString("Prefer implementing supported capabilities under `~/.luc` or `<workspace>/.luc` before changing core code.\n")
	}
	if catalog := c.skillCatalog(); strings.TrimSpace(catalog) != "" {
		builder.WriteString("\n\nAvailable skills:\n")
		builder.WriteString("Use the `load_skill` tool when a task matches a skill's description or the user explicitly names one.\n")
		builder.WriteString("After loading a skill, follow its instructions and use `read_skill_resource` for referenced bundled files when needed.\n\n")
		builder.WriteString(catalog)
	}
	return strings.TrimSpace(builder.String()), nil
}

func (c *Controller) matchingPromptExtensions() []extensions.PromptExtension {
	if len(c.promptExts) == 0 {
		return nil
	}

	out := make([]extensions.PromptExtension, 0, len(c.promptExts))
	for _, ext := range c.promptExts {
		if ext.Matches(c.config.Provider.Kind, c.config.Provider.Model) {
			out = append(out, ext)
		}
	}
	return out
}

func (c *Controller) hasRelevantSkill(skills []extensions.Skill, name string) bool {
	target := strings.ToLower(strings.TrimSpace(name))
	for _, skill := range skills {
		if strings.ToLower(strings.TrimSpace(skill.Name)) == target {
			return true
		}
	}
	return false
}

func (c *Controller) relevantSkills(input string) []extensions.Skill {
	text := strings.ToLower(strings.TrimSpace(input))
	if text == "" {
		return nil
	}
	var out []extensions.Skill
	for _, skill := range c.skills {
		if skill.Always {
			out = append(out, skill)
			continue
		}
		for _, trigger := range skill.Triggers {
			trigger = strings.ToLower(strings.TrimSpace(trigger))
			if trigger == "" {
				continue
			}
			if strings.Contains(text, trigger) {
				out = append(out, skill)
				break
			}
		}
	}
	return out
}

func (c *Controller) skillCatalog() string {
	if len(c.skills) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, skill := range c.skills {
		label := strings.TrimSpace(skill.DisplayName)
		if label == "" {
			label = skill.Name
		}
		builder.WriteString("- ")
		builder.WriteString(skill.Name)
		if label != "" && label != skill.Name {
			builder.WriteString(" (")
			builder.WriteString(label)
			builder.WriteString(")")
		}
		if desc := strings.TrimSpace(skill.Description); desc != "" {
			builder.WriteString(": ")
			builder.WriteString(desc)
		}
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func (c *Controller) toolSpecs() []provider.ToolSpec {
	specs := append([]provider.ToolSpec(nil), c.tools.Specs()...)
	if spec := c.skillToolSpec(); spec.Name != "" {
		specs = append(specs, spec)
	}
	if spec := c.skillResourceToolSpec(); spec.Name != "" {
		specs = append(specs, spec)
	}
	return specs
}

func (c *Controller) skillToolSpec() provider.ToolSpec {
	if len(c.skills) == 0 {
		return provider.ToolSpec{}
	}

	enum := make([]string, 0, len(c.skills))
	for _, skill := range c.skills {
		enum = append(enum, fmt.Sprintf("%q", skill.Name))
	}

	return provider.ToolSpec{
		Name: skillToolName,
		Description: "Load the full instructions for an available skill by name. " +
			"Use this when a task matches a skill's description or the user explicitly names a skill.",
		Schema: json.RawMessage(fmt.Sprintf(`{
			"type":"object",
			"properties":{
				"name":{"type":"string","enum":[%s]}
			},
			"required":["name"]
		}`, strings.Join(enum, ","))),
	}
}

func (c *Controller) skillResourceToolSpec() provider.ToolSpec {
	if len(c.skills) == 0 {
		return provider.ToolSpec{}
	}

	enum := make([]string, 0, len(c.skills))
	for _, skill := range c.skills {
		if strings.TrimSpace(skill.BaseDir) == "" {
			continue
		}
		enum = append(enum, fmt.Sprintf("%q", skill.Name))
	}
	if len(enum) == 0 {
		return provider.ToolSpec{}
	}

	return provider.ToolSpec{
		Name: skillResourceToolName,
		Description: "Read a bundled file referenced by a previously loaded skill. " +
			"Paths are relative to the skill directory.",
		Schema: json.RawMessage(fmt.Sprintf(`{
			"type":"object",
			"properties":{
				"name":{"type":"string","enum":[%s]},
				"path":{"type":"string"}
			},
			"required":["name","path"]
		}`, strings.Join(enum, ","))),
	}
}

func (c *Controller) runLoadSkillTool(raw string) (tools.Result, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return tools.Result{}, err
	}
	skill, ok := c.skillByName(args.Name)
	if !ok {
		return tools.Result{}, fmt.Errorf("unknown skill %q", args.Name)
	}
	if _, loaded := c.loadedSkills[strings.ToLower(skill.Name)]; loaded {
		return normalizeCustomToolResult(tools.Result{
			Content: fmt.Sprintf("skill %s is already loaded in this session", skill.Name),
			Metadata: map[string]any{
				"skill_name":                skill.Name,
				"already_loaded":            true,
				tools.MetadataUIHideContent: true,
				tools.MetadataUILabel:       fmt.Sprintf("skill loaded %s", skill.Name),
			},
		}), nil
	}
	prompt, err := extensions.ResolveSkillPrompt(skill)
	if err != nil {
		return tools.Result{}, err
	}
	c.loadedSkills[strings.ToLower(skill.Name)] = struct{}{}

	label := strings.TrimSpace(skill.DisplayName)
	if label == "" {
		label = skill.Name
	}
	content := renderSkillContent(skill, label, prompt)
	return normalizeCustomToolResult(tools.Result{
		Content: content,
		Metadata: map[string]any{
			"skill_name":                skill.Name,
			"skill_path":                skill.BodyPath,
			"skill_dir":                 skill.BaseDir,
			"resources":                 skillResources(skill),
			tools.MetadataUIHideContent: true,
			tools.MetadataUILabel:       fmt.Sprintf("skill loaded %s", skill.Name),
		},
	}), nil
}

func (c *Controller) runReadSkillResourceTool(raw string) (tools.Result, error) {
	var args struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return tools.Result{}, err
	}
	skill, ok := c.skillByName(args.Name)
	if !ok {
		return tools.Result{}, fmt.Errorf("unknown skill %q", args.Name)
	}
	if strings.TrimSpace(skill.BaseDir) == "" {
		return tools.Result{}, fmt.Errorf("skill %s has no bundled resources", skill.Name)
	}
	path, err := safeSkillPath(skill.BaseDir, args.Path)
	if err != nil {
		return tools.Result{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tools.Result{}, err
	}
	return normalizeCustomToolResult(tools.Result{
		Content:          string(data),
		DefaultCollapsed: true,
		CollapsedSummary: fmt.Sprintf("Read %s from skill %s.", args.Path, skill.Name),
		Metadata: map[string]any{
			"skill_name": skill.Name,
			"path":       path,
		},
	}), nil
}

func (c *Controller) skillByName(name string) (extensions.Skill, bool) {
	target := strings.ToLower(strings.TrimSpace(name))
	for _, skill := range c.skills {
		if strings.ToLower(skill.Name) == target {
			return skill, true
		}
	}
	return extensions.Skill{}, false
}

func renderSkillContent(skill extensions.Skill, label, prompt string) string {
	var builder strings.Builder
	builder.WriteString("<skill_content name=\"")
	builder.WriteString(skill.Name)
	builder.WriteString("\">\n")
	builder.WriteString("# ")
	builder.WriteString(label)
	builder.WriteString("\n\n")
	if desc := strings.TrimSpace(skill.Description); desc != "" {
		builder.WriteString(desc)
		builder.WriteString("\n\n")
	}
	builder.WriteString(prompt)
	if dir := strings.TrimSpace(skill.BaseDir); dir != "" {
		builder.WriteString("\n\nSkill directory: ")
		builder.WriteString(dir)
		builder.WriteString("\nRelative paths in this skill are relative to the skill directory.")
		if resources := skillResources(skill); len(resources) > 0 {
			builder.WriteString("\n\n<skill_resources>\n")
			for _, resource := range resources {
				builder.WriteString("<file>")
				builder.WriteString(resource)
				builder.WriteString("</file>\n")
			}
			builder.WriteString("</skill_resources>")
		}
	}
	builder.WriteString("\n</skill_content>")
	return builder.String()
}

func skillResources(skill extensions.Skill) []string {
	if strings.TrimSpace(skill.BaseDir) == "" {
		return nil
	}
	var out []string
	_ = filepath.WalkDir(skill.BaseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if samePath(path, skill.SourcePath) || samePath(path, skill.BodyPath) {
			return nil
		}
		rel, err := filepath.Rel(skill.BaseDir, path)
		if err != nil {
			return nil
		}
		out = append(out, rel)
		if len(out) >= 64 {
			return fs.SkipAll
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func safeSkillPath(root, target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", errors.New("path is required")
	}
	path := target
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes skill directory", target)
	}
	return path, nil
}

func samePath(a, b string) bool {
	return strings.TrimSpace(a) != "" && strings.TrimSpace(a) == strings.TrimSpace(b)
}

func normalizeCustomToolResult(result tools.Result) tools.Result {
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	if hidden, _ := result.Metadata[tools.MetadataUIHideContent].(bool); hidden {
		delete(result.Metadata, tools.MetadataUIDefaultCollapsed)
		delete(result.Metadata, tools.MetadataUICollapsedSummary)
		return result
	}
	if result.DefaultCollapsed {
		result.Metadata[tools.MetadataUIDefaultCollapsed] = true
	}
	if summary := strings.TrimSpace(result.CollapsedSummary); summary != "" {
		result.Metadata[tools.MetadataUICollapsedSummary] = summary
	}
	return result
}
