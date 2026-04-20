package extensions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	luruntime "github.com/lutefd/luc/internal/runtime"
	"gopkg.in/yaml.v3"
)

type ToolUI struct {
	DefaultCollapsed bool   `yaml:"default_collapsed" json:"default_collapsed"`
	CollapsedSummary string `yaml:"collapsed_summary" json:"collapsed_summary"`
}

type ToolDef struct {
	Name           string
	Description    string
	Schema         json.RawMessage
	SchemaVersion  string
	RuntimeKind    string
	Command        string
	ExtensionID    string
	Handler        string
	Capabilities   []string
	TimeoutSeconds int
	UI             ToolUI
	SourcePath     string
}

type Skill struct {
	Name        string
	DisplayName string
	Description string
	Triggers    []string
	Always      bool
	Prompt      string
	BodyPath    string
	BaseDir     string
	SourcePath  string
}

type ThemeColors struct {
	Bg        string `yaml:"bg" json:"bg"`
	Panel     string `yaml:"panel" json:"panel"`
	PanelAlt  string `yaml:"panel_alt" json:"panel_alt"`
	Line      string `yaml:"line" json:"line"`
	Accent    string `yaml:"accent" json:"accent"`
	AccentAlt string `yaml:"accent_alt" json:"accent_alt"`
	Text      string `yaml:"text" json:"text"`
	Muted     string `yaml:"muted" json:"muted"`
	Subtle    string `yaml:"subtle" json:"subtle"`
	Success   string `yaml:"success" json:"success"`
	Warn      string `yaml:"warn" json:"warn"`
	Blue      string `yaml:"blue" json:"blue"`
	Cyan      string `yaml:"cyan" json:"cyan"`
	ErrorText string `yaml:"error_text" json:"error_text"`
	DiffAddBG string `yaml:"diff_add_bg" json:"diff_add_bg"`
	DiffAddFG string `yaml:"diff_add_fg" json:"diff_add_fg"`
	DiffDelBG string `yaml:"diff_del_bg" json:"diff_del_bg"`
	DiffDelFG string `yaml:"diff_del_fg" json:"diff_del_fg"`
}

type ThemeDef struct {
	Name       string
	Inherits   string
	Colors     ThemeColors
	SourcePath string
}

type ProviderModelDef struct {
	ID          string
	Name        string
	Description string
	ContextK    int
	Reasoning   bool
}

type ProviderDef struct {
	ID           string
	Name         string
	Type         string
	BaseURL      string
	APIKeyEnv    string
	Command      string
	Args         []string
	Env          map[string]string
	Capabilities []string
	Models       []ProviderModelDef
	SourcePath   string
}

func LoadToolDefs(workspaceRoot string) ([]ToolDef, error) {
	dirs, err := categoryDirs(workspaceRoot, "tools")
	if err != nil {
		return nil, err
	}

	byName := map[string]ToolDef{}
	var errs []error
	for _, dir := range dirs {
		paths, err := listManifestFiles(dir, true)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, path := range paths {
			def, err := parseToolDef(path)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			byName[def.Name] = def
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]ToolDef, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out, nil
}

func LoadProviderDefs(workspaceRoot string) ([]ProviderDef, error) {
	dirs, err := categoryDirs(workspaceRoot, "providers")
	if err != nil {
		return nil, err
	}

	byID := map[string]ProviderDef{}
	var errs []error
	for _, dir := range dirs {
		paths, err := listManifestFiles(dir, true)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, path := range paths {
			def, err := parseProviderDef(path)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			byID[strings.ToLower(def.ID)] = def
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]ProviderDef, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out, nil
}

func LoadSkills(workspaceRoot string) ([]Skill, error) {
	byName := map[string]Skill{}
	for _, skill := range builtinSkills() {
		byName[strings.ToLower(skill.Name)] = skill
	}

	dirs, err := skillDirs(workspaceRoot)
	if err != nil {
		return nil, err
	}

	var errs []error
	for _, dir := range dirs {
		sources, err := listSkillSources(dir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, source := range sources {
			skill, err := parseSkillSource(source)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			byName[strings.ToLower(skill.Name)] = skill
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]Skill, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out, nil
}

func skillDirs(workspaceRoot string) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	packageDirs, err := runtimePackageDirs(workspaceRoot, "skills")
	if err != nil {
		return nil, err
	}
	dirs := []string{
		filepath.Join(home, ".agents", "skills"),
		filepath.Join(home, ".luc", "skills"),
	}
	dirs = append(dirs, packageDirs...)
	dirs = append(dirs,
		filepath.Join(workspaceRoot, ".agents", "skills"),
		filepath.Join(workspaceRoot, ".luc", "skills"),
	)
	return dirs, nil
}

// ListThemes returns the names of theme files discovered under the workspace
// `.luc/themes/` directory and the user's `~/.luc/themes/` directory. Names
// are deduplicated (workspace wins over home on collision, matching the
// lookup order in LoadTheme) and sorted alphabetically. Only files with a
// supported extension (.yaml/.yml/.json) are considered; the returned names
// have the extension stripped so they can be passed straight back to
// LoadTheme.
func ListThemes(workspaceRoot string) ([]string, error) {
	homeDir, err := configRoot()
	if err != nil {
		return nil, err
	}
	packageDirs, err := runtimePackageDirs(workspaceRoot, "themes")
	if err != nil {
		return nil, err
	}
	dirs := []string{filepath.Join(homeDir, "themes")}
	dirs = append(dirs, packageDirs...)
	dirs = append(dirs, filepath.Join(workspaceRoot, ".luc", "themes"))

	seen := map[string]struct{}{}
	var names []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			switch ext {
			case ".yaml", ".yml", ".json":
			default:
				continue
			}
			name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func LoadTheme(workspaceRoot, name string) (ThemeDef, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ThemeDef{}, false, nil
	}

	homeDir, err := configRoot()
	if err != nil {
		return ThemeDef{}, false, err
	}
	themeDirs := []string{filepath.Join(homeDir, "themes")}
	packageDirs, err := runtimePackageDirs(workspaceRoot, "themes")
	if err != nil {
		return ThemeDef{}, false, err
	}
	themeDirs = append(themeDirs, packageDirs...)
	themeDirs = append(themeDirs, filepath.Join(workspaceRoot, ".luc", "themes"))
	var candidates []string
	for i := len(themeDirs) - 1; i >= 0; i-- {
		dir := themeDirs[i]
		candidates = append(candidates,
			filepath.Join(dir, name+".yaml"),
			filepath.Join(dir, name+".yml"),
			filepath.Join(dir, name+".json"),
		)
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return ThemeDef{}, false, err
		}
		var raw struct {
			Name     string      `yaml:"name" json:"name"`
			Inherits string      `yaml:"inherits" json:"inherits"`
			Colors   ThemeColors `yaml:"colors" json:"colors"`
		}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return ThemeDef{}, false, fmt.Errorf("%s: %w", path, err)
		}
		if raw.Name == "" {
			raw.Name = name
		}
		return ThemeDef{
			Name:       raw.Name,
			Inherits:   raw.Inherits,
			Colors:     raw.Colors,
			SourcePath: path,
		}, true, nil
	}
	return ThemeDef{}, false, nil
}

func categoryDirs(workspaceRoot, category string) ([]string, error) {
	homeDir, err := configRoot()
	if err != nil {
		return nil, err
	}
	packageDirs, err := runtimePackageDirs(workspaceRoot, category)
	if err != nil {
		return nil, err
	}
	dirs := []string{filepath.Join(homeDir, category)}
	dirs = append(dirs, packageDirs...)
	dirs = append(dirs, filepath.Join(workspaceRoot, ".luc", category))
	return dirs, nil
}

func configRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".luc"), nil
}

func listManifestFiles(dir string, recursive bool) ([]string, error) {
	paths, err := walkFiles(dir, recursive, func(path string, d fs.DirEntry) bool {
		if d.IsDir() {
			return false
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".yaml", ".yml", ".json":
			return true
		default:
			return false
		}
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

type skillSource struct {
	ManifestPath string
	PromptPath   string
	LegacyPath   string
}

func (s skillSource) SourcePath() string {
	switch {
	case s.ManifestPath != "":
		return s.ManifestPath
	case s.PromptPath != "":
		return s.PromptPath
	default:
		return s.LegacyPath
	}
}

func listSkillSources(dir string) ([]skillSource, error) {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	sourcesByKey := map[string]*skillSource{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir || d.IsDir() {
			return nil
		}

		base := strings.ToLower(filepath.Base(path))
		switch base {
		case "luc.yaml", "luc.yml", "luc.json":
			key := "dir:" + filepath.Dir(path)
			source := sourcesByKey[key]
			if source == nil {
				source = &skillSource{}
				sourcesByKey[key] = source
			}
			source.ManifestPath = path
		case "skill.md":
			key := "dir:" + filepath.Dir(path)
			source := sourcesByKey[key]
			if source == nil {
				source = &skillSource{}
				sourcesByKey[key] = source
			}
			source.PromptPath = path
		default:
			if strings.EqualFold(filepath.Ext(path), ".md") && filepath.Dir(path) == dir {
				key := "file:" + path
				sourcesByKey[key] = &skillSource{LegacyPath: path}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(sourcesByKey))
	for key := range sourcesByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]skillSource, 0, len(keys))
	for _, key := range keys {
		out = append(out, *sourcesByKey[key])
	}
	return out, nil
}

func walkFiles(dir string, recursive bool, include func(path string, d fs.DirEntry) bool) ([]string, error) {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	var paths []string
	if !recursive {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if include(path, entry) {
				paths = append(paths, path)
			}
		}
		return paths, nil
	}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		if include(path, d) {
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}

func parseToolDef(path string) (ToolDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ToolDef{}, err
	}
	var raw struct {
		Name           string `yaml:"name" json:"name"`
		Description    string `yaml:"description" json:"description"`
		Schema         any    `yaml:"schema" json:"schema"`
		InputSchema    any    `yaml:"input_schema" json:"input_schema"`
		Command        string `yaml:"command" json:"command"`
		TimeoutSeconds int    `yaml:"timeout_seconds" json:"timeout_seconds"`
		UI             ToolUI `yaml:"ui" json:"ui"`
		Runtime        struct {
			Kind         string   `yaml:"kind" json:"kind"`
			Command      string   `yaml:"command" json:"command"`
			ExtensionID  string   `yaml:"extension_id" json:"extension_id"`
			Handler      string   `yaml:"handler" json:"handler"`
			Capabilities []string `yaml:"capabilities" json:"capabilities"`
		} `yaml:"runtime" json:"runtime"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return ToolDef{}, fmt.Errorf("%s: %w", path, err)
	}
	if strings.TrimSpace(raw.Name) == "" {
		return ToolDef{}, fmt.Errorf("%s: name is required", path)
	}
	if strings.TrimSpace(raw.Description) == "" {
		return ToolDef{}, fmt.Errorf("%s: description is required", path)
	}
	schema := json.RawMessage(`{"type":"object","properties":{}}`)
	schemaSource := raw.Schema
	schemaVersion := "luc.tool/v1"
	if version, ok := raw.Schema.(string); ok {
		schemaVersion = strings.TrimSpace(version)
		if schemaVersion != "luc.tool/v1" && schemaVersion != "luc.tool/v2" {
			return ToolDef{}, fmt.Errorf("%s: unsupported tool schema version %q", path, version)
		}
		schemaSource = raw.InputSchema
	}
	if raw.Schema == nil && raw.InputSchema != nil {
		schemaSource = raw.InputSchema
	}
	if schemaSource != nil {
		schemaData, err := json.Marshal(schemaSource)
		if err != nil {
			return ToolDef{}, fmt.Errorf("%s: invalid schema: %w", path, err)
		}
		schema = schemaData
	}

	runtimeKind := strings.TrimSpace(firstNonEmpty(raw.Runtime.Kind, "exec"))
	switch runtimeKind {
	case "exec":
		command := strings.TrimSpace(firstNonEmpty(raw.Runtime.Command, raw.Command))
		if command == "" {
			return ToolDef{}, fmt.Errorf("%s: command is required", path)
		}
		return ToolDef{
			Name:           raw.Name,
			Description:    raw.Description,
			Schema:         schema,
			SchemaVersion:  schemaVersion,
			RuntimeKind:    runtimeKind,
			Command:        command,
			Capabilities:   luruntime.NormalizeCapabilities(raw.Runtime.Capabilities),
			TimeoutSeconds: raw.TimeoutSeconds,
			UI:             raw.UI,
			SourcePath:     path,
		}, nil
	case "extension":
		if schemaVersion != "luc.tool/v2" {
			return ToolDef{}, fmt.Errorf("%s: runtime.kind %q requires schema luc.tool/v2", path, runtimeKind)
		}
		extensionID := strings.TrimSpace(raw.Runtime.ExtensionID)
		if extensionID == "" {
			return ToolDef{}, fmt.Errorf("%s: runtime.extension_id is required for extension tools", path)
		}
		handler := strings.TrimSpace(raw.Runtime.Handler)
		if handler == "" {
			return ToolDef{}, fmt.Errorf("%s: runtime.handler is required for extension tools", path)
		}
		return ToolDef{
			Name:           raw.Name,
			Description:    raw.Description,
			Schema:         schema,
			SchemaVersion:  schemaVersion,
			RuntimeKind:    runtimeKind,
			ExtensionID:    extensionID,
			Handler:        handler,
			TimeoutSeconds: raw.TimeoutSeconds,
			UI:             raw.UI,
			SourcePath:     path,
		}, nil
	default:
		return ToolDef{}, fmt.Errorf("%s: unsupported runtime kind %q", path, runtimeKind)
	}
}

func parseProviderDef(path string) (ProviderDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProviderDef{}, err
	}

	var raw struct {
		ID           string            `yaml:"id" json:"id"`
		Name         string            `yaml:"name" json:"name"`
		Type         string            `yaml:"type" json:"type"`
		Kind         string            `yaml:"kind" json:"kind"`
		BaseURL      string            `yaml:"base_url" json:"base_url"`
		APIKeyEnv    string            `yaml:"api_key_env" json:"api_key_env"`
		Command      string            `yaml:"command" json:"command"`
		Args         []string          `yaml:"args" json:"args"`
		Env          map[string]string `yaml:"env" json:"env"`
		Capabilities []string          `yaml:"capabilities" json:"capabilities"`
		Models       []struct {
			ID          string `yaml:"id" json:"id"`
			Name        string `yaml:"name" json:"name"`
			Description string `yaml:"description" json:"description"`
			ContextK    int    `yaml:"context_k" json:"context_k"`
			Reasoning   bool   `yaml:"reasoning" json:"reasoning"`
		} `yaml:"models" json:"models"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return ProviderDef{}, fmt.Errorf("%s: %w", path, err)
	}

	id := strings.TrimSpace(raw.ID)
	if id == "" {
		id = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if id == "" {
		return ProviderDef{}, fmt.Errorf("%s: provider id is required", path)
	}

	providerType := strings.TrimSpace(firstNonEmpty(raw.Type, raw.Kind, "openai-compatible"))
	switch providerType {
	case "openai-compatible", "openai", "exec":
	default:
		return ProviderDef{}, fmt.Errorf("%s: unsupported provider type %q", path, providerType)
	}

	baseURL := strings.TrimSpace(raw.BaseURL)
	command := strings.TrimSpace(raw.Command)
	switch providerType {
	case "openai-compatible", "openai":
		if baseURL == "" {
			return ProviderDef{}, fmt.Errorf("%s: base_url is required", path)
		}
	case "exec":
		if command == "" {
			return ProviderDef{}, fmt.Errorf("%s: command is required for exec providers", path)
		}
	}
	if len(raw.Models) == 0 {
		return ProviderDef{}, fmt.Errorf("%s: at least one model is required", path)
	}

	models := make([]ProviderModelDef, 0, len(raw.Models))
	for i, model := range raw.Models {
		modelID := strings.TrimSpace(model.ID)
		if modelID == "" {
			return ProviderDef{}, fmt.Errorf("%s: models[%d].id is required", path, i)
		}
		models = append(models, ProviderModelDef{
			ID:          modelID,
			Name:        strings.TrimSpace(firstNonEmpty(model.Name, modelID)),
			Description: strings.TrimSpace(model.Description),
			ContextK:    model.ContextK,
			Reasoning:   model.Reasoning,
		})
	}

	return ProviderDef{
		ID:           id,
		Name:         strings.TrimSpace(firstNonEmpty(raw.Name, id)),
		Type:         providerType,
		BaseURL:      baseURL,
		APIKeyEnv:    strings.TrimSpace(raw.APIKeyEnv),
		Command:      command,
		Args:         append([]string(nil), raw.Args...),
		Env:          mapsClone(raw.Env),
		Capabilities: luruntime.NormalizeCapabilities(raw.Capabilities),
		Models:       models,
		SourcePath:   path,
	}, nil
}

func parseSkillSource(source skillSource) (Skill, error) {
	switch {
	case source.LegacyPath != "":
		return parseSkillMarkdown(source.LegacyPath)
	case source.ManifestPath != "":
		return parseManifestSkill(source)
	case source.PromptPath != "":
		return parseSkillMarkdown(source.PromptPath)
	default:
		return Skill{}, fmt.Errorf("invalid skill source")
	}
}

func parseSkillMarkdown(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	return parseSkillMarkdownContent(path, string(data))
}

func parseSkillMarkdownContent(path, content string) (Skill, error) {
	frontmatter, body, err := splitFrontmatter(content)
	if err != nil {
		return Skill{}, fmt.Errorf("%s: %w", path, err)
	}

	var raw struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Triggers    []string `yaml:"triggers"`
		Always      bool     `yaml:"always"`
	}
	if strings.TrimSpace(frontmatter) != "" {
		if err := yaml.Unmarshal([]byte(frontmatter), &raw); err != nil {
			return Skill{}, fmt.Errorf("%s: %w", path, err)
		}
	}

	name := strings.TrimSpace(raw.Name)
	if name == "" {
		if filepath.Base(path) == "SKILL.md" {
			name = filepath.Base(filepath.Dir(path))
		} else {
			name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
	}
	body = strings.TrimSpace(body)
	description := strings.TrimSpace(raw.Description)
	if description == "" && body != "" {
		description = inferSkillDescription(body)
	}
	prompt := ""
	if body != "" {
		prompt = body
	} else {
		prompt = synthesizeSkillPrompt(name, description)
	}
	if strings.TrimSpace(prompt) == "" {
		return Skill{}, fmt.Errorf("%s: skill prompt is empty", path)
	}

	return Skill{
		Name:        name,
		DisplayName: firstNonEmpty(raw.Name, name),
		Description: description,
		Triggers:    append([]string(nil), raw.Triggers...),
		Always:      raw.Always,
		Prompt:      prompt,
		BaseDir:     filepath.Dir(path),
		SourcePath:  path,
	}, nil
}

func parseManifestSkill(source skillSource) (Skill, error) {
	data, err := os.ReadFile(source.ManifestPath)
	if err != nil {
		return Skill{}, err
	}

	var raw struct {
		Name          string   `yaml:"name" json:"name"`
		Description   string   `yaml:"description" json:"description"`
		ShortDesc     string   `yaml:"short_description" json:"short_description"`
		Triggers      []string `yaml:"triggers" json:"triggers"`
		Always        bool     `yaml:"always" json:"always"`
		DefaultPrompt string   `yaml:"default_prompt" json:"default_prompt"`
		Interface     struct {
			DisplayName   string `yaml:"display_name" json:"display_name"`
			ShortDesc     string `yaml:"short_description" json:"short_description"`
			DefaultPrompt string `yaml:"default_prompt" json:"default_prompt"`
		} `yaml:"interface" json:"interface"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Skill{}, fmt.Errorf("%s: %w", source.ManifestPath, err)
	}

	var fallback Skill
	if source.PromptPath != "" {
		fallback, err = parseSkillMarkdown(source.PromptPath)
		if err != nil {
			return Skill{}, err
		}
	}

	name := strings.TrimSpace(firstNonEmpty(raw.Name, fallback.Name, filepath.Base(filepath.Dir(source.ManifestPath))))
	if name == "" {
		return Skill{}, fmt.Errorf("%s: skill name is required", source.ManifestPath)
	}
	displayName := strings.TrimSpace(firstNonEmpty(raw.Interface.DisplayName, raw.Name, fallback.DisplayName, name))
	description := strings.TrimSpace(firstNonEmpty(raw.Interface.ShortDesc, raw.ShortDesc, raw.Description, fallback.Description))
	prompt := ""
	bodyPath := ""
	if source.PromptPath != "" {
		bodyPath = source.PromptPath
	} else {
		prompt = strings.TrimSpace(firstNonEmpty(raw.Interface.DefaultPrompt, raw.DefaultPrompt, fallback.Prompt))
		if prompt == "" {
			prompt = synthesizeSkillPrompt(displayName, description)
		}
		if prompt == "" {
			return Skill{}, fmt.Errorf("%s: skill prompt is empty", source.ManifestPath)
		}
	}

	return Skill{
		Name:        name,
		DisplayName: displayName,
		Description: description,
		Triggers:    append([]string(nil), raw.Triggers...),
		Always:      raw.Always || fallback.Always,
		Prompt:      prompt,
		BodyPath:    bodyPath,
		BaseDir:     filepath.Dir(source.ManifestPath),
		SourcePath:  source.ManifestPath,
	}, nil
}

func ResolveSkillPrompt(skill Skill) (string, error) {
	if strings.TrimSpace(skill.Prompt) != "" {
		return strings.TrimSpace(skill.Prompt), nil
	}
	if strings.TrimSpace(skill.BodyPath) == "" {
		return synthesizeSkillPrompt(skill.DisplayName, skill.Description), nil
	}
	data, err := os.ReadFile(skill.BodyPath)
	if err != nil {
		return "", err
	}
	_, body, err := splitFrontmatter(string(data))
	if err != nil {
		return "", err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return synthesizeSkillPrompt(skill.DisplayName, skill.Description), nil
	}
	return body, nil
}

func splitFrontmatter(content string) (string, string, error) {
	trimmed := strings.TrimLeft(content, "\ufeff")
	if !strings.HasPrefix(trimmed, "---\n") {
		return "", trimmed, nil
	}
	rest := strings.TrimPrefix(trimmed, "---\n")
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return "", "", fmt.Errorf("unterminated frontmatter")
	}
	return rest[:idx], rest[idx+5:], nil
}

func inferSkillDescription(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		if line != "" {
			return line
		}
	}
	return ""
}

func synthesizeSkillPrompt(name, description string) string {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	switch {
	case name != "" && description != "":
		return "Use the " + name + " skill when it is relevant.\n" + description
	case description != "":
		return description
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func mapsClone(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
