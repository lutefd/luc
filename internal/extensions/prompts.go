package extensions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type PromptExtension struct {
	ID            string
	Description   string
	Prompt        string
	Providers     []string
	Models        []string
	ModelPrefixes []string
	SourcePath    string
}

func LoadPromptExtensions(workspaceRoot string) ([]PromptExtension, error) {
	files, err := layeredManifestFiles(workspaceRoot, "prompts")
	if err != nil {
		return nil, err
	}

	byID := map[string]PromptExtension{}
	var errs []error
	for _, path := range files {
		ext, err := parsePromptExtension(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		byID[strings.ToLower(ext.ID)] = ext
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]PromptExtension, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out, nil
}

func (p PromptExtension) Matches(providerID, modelID string) bool {
	providers := normalizedValues(p.Providers)
	if len(providers) > 0 {
		matchedProvider := false
		for _, alias := range providerAliases(providerID) {
			if containsValue(providers, alias) {
				matchedProvider = true
				break
			}
		}
		if !matchedProvider {
			return false
		}
	}

	models := normalizedValues(p.Models)
	prefixes := normalizedValues(p.ModelPrefixes)
	if len(models) == 0 && len(prefixes) == 0 {
		return true
	}

	model := strings.ToLower(strings.TrimSpace(modelID))
	if model == "" {
		return false
	}
	if containsValue(models, model) {
		return true
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}

func parsePromptExtension(path string) (PromptExtension, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PromptExtension{}, err
	}

	var raw struct {
		Schema      string `yaml:"schema" json:"schema"`
		ID          string `yaml:"id" json:"id"`
		Description string `yaml:"description" json:"description"`
		Prompt      string `yaml:"prompt" json:"prompt"`
		Match       struct {
			Providers     []string `yaml:"providers" json:"providers"`
			Models        []string `yaml:"models" json:"models"`
			ModelPrefixes []string `yaml:"model_prefixes" json:"model_prefixes"`
		} `yaml:"match" json:"match"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return PromptExtension{}, fmt.Errorf("%s: %w", path, err)
	}
	if strings.TrimSpace(raw.Schema) != "luc.prompt/v1" {
		return PromptExtension{}, fmt.Errorf("%s: unsupported prompt manifest schema %q", path, raw.Schema)
	}
	id := strings.TrimSpace(raw.ID)
	if id == "" {
		id = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if id == "" {
		return PromptExtension{}, fmt.Errorf("%s: id is required", path)
	}
	prompt := strings.TrimSpace(raw.Prompt)
	if prompt == "" {
		return PromptExtension{}, fmt.Errorf("%s: prompt is required", path)
	}

	return PromptExtension{
		ID:            id,
		Description:   strings.TrimSpace(raw.Description),
		Prompt:        prompt,
		Providers:     normalizedValues(raw.Match.Providers),
		Models:        normalizedValues(raw.Match.Models),
		ModelPrefixes: normalizedValues(raw.Match.ModelPrefixes),
		SourcePath:    path,
	}, nil
}

func providerAliases(providerID string) []string {
	id := strings.ToLower(strings.TrimSpace(providerID))
	switch id {
	case "":
		return nil
	case "openai":
		return []string{"openai", "openai-compatible"}
	case "openai-compatible":
		return []string{"openai-compatible", "openai"}
	default:
		return []string{id}
	}
}

func normalizedValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func containsValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
