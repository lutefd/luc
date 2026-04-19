package extensions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSkillsPrefersManifestSkillsAndSupportsMarkdownFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".luc", "skills", "rails"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".luc", "skills", "rails", "luc.yaml"), []byte(`interface:
  display_name: Rails
  short_description: Rails conventions for migrations and generators.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".luc", "skills", "rails", "SKILL.md"), []byte(`---
name: rails
description: Rails conventions for migrations and generators.
---
Prefer bin/rails and reversible migrations.`), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".luc", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "skills", "notes.md"), []byte(`---
description: Top-level fallback
---
Use the lightweight notes workflow.`), 0o644); err != nil {
		t.Fatal(err)
	}

	skills, err := LoadSkills(root)
	if err != nil {
		t.Fatal(err)
	}

	notes := findSkill(skills, "notes")
	if notes == nil {
		t.Fatalf("expected fallback markdown skill, got %#v", skills)
	}
	if prompt, err := ResolveSkillPrompt(*notes); err != nil || prompt != "Use the lightweight notes workflow." {
		t.Fatalf("expected fallback markdown prompt, got %q err=%v", prompt, err)
	}

	rails := findSkill(skills, "rails")
	if rails == nil {
		t.Fatalf("expected manifest skill to load, got %#v", skills)
	}
	if rails.DisplayName != "Rails" {
		t.Fatalf("expected manifest display name, got %#v", rails)
	}
	if rails.Description != "Rails conventions for migrations and generators." {
		t.Fatalf("expected manifest description, got %#v", rails)
	}
	if prompt, err := ResolveSkillPrompt(*rails); err != nil || prompt != "Prefer bin/rails and reversible migrations." {
		t.Fatalf("expected SKILL.md prompt, got %q err=%v", prompt, err)
	}
	if rails.BaseDir != filepath.Join(home, ".luc", "skills", "rails") {
		t.Fatalf("expected skill base dir, got %#v", rails)
	}
}

func TestLoadSkillsProjectOverrideWinsOverGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".luc", "skills", "rails"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".luc", "skills", "rails", "luc.yaml"), []byte(`interface:
  short_description: Global rails workflow.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".luc", "skills", "rails", "SKILL.md"), []byte(`---
name: rails
description: Global rails workflow.
---
Use the global rails workflow.`), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".luc", "skills", "rails"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "skills", "rails", "luc.yaml"), []byte(`interface:
  short_description: Project rails workflow.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "skills", "rails", "SKILL.md"), []byte(`---
name: rails
description: Project rails workflow.
---
Use the project rails workflow.`), 0o644); err != nil {
		t.Fatal(err)
	}

	skills, err := LoadSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	rails := findSkill(skills, "rails")
	if rails == nil {
		t.Fatalf("expected merged rails skill, got %#v", skills)
	}
	if rails.Description != "Project rails workflow." {
		t.Fatalf("expected project override description, got %#v", rails)
	}
	if prompt, err := ResolveSkillPrompt(*rails); err != nil || prompt != "Use the project rails workflow." {
		t.Fatalf("expected project override SKILL.md body, got %q err=%v", prompt, err)
	}
}

func TestLoadSkillsFallsBackToSynthesizedPromptWithoutSkillMarkdown(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".luc", "skills", "weaver"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "skills", "weaver", "luc.yaml"), []byte(`interface:
  display_name: Weaver
  short_description: Operate local Git branch stacks with the installed weaver CLI.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	skills, err := LoadSkills(root)
	if err != nil {
		t.Fatal(err)
	}
	weaver := findSkill(skills, "weaver")
	if weaver == nil {
		t.Fatalf("expected weaver skill, got %#v", skills)
	}
	if weaver.Prompt == "" {
		t.Fatalf("expected synthesized inline prompt, got %#v", weaver)
	}
	if prompt, err := ResolveSkillPrompt(*weaver); err != nil || prompt == "" {
		t.Fatalf("expected synthesized prompt, got %q err=%v", prompt, err)
	}
}

func TestLoadProviderDefsProjectOverrideWinsOverGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".luc", "providers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".luc", "providers", "gateway.yaml"), []byte(`id: gateway
name: Global Gateway
base_url: https://global.example/v1
api_key_env: GLOBAL_GATEWAY_KEY
models:
  - id: global-model
    name: Global Model
    description: Global model
    context_k: 128
`), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".luc", "providers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "providers", "gateway.yaml"), []byte(`id: gateway
name: Project Gateway
base_url: https://project.example/v1
models:
  - id: project-model
    name: Project Model
    description: Project model
    context_k: 256
    reasoning: true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	defs, err := LoadProviderDefs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected one merged provider, got %#v", defs)
	}
	def := defs[0]
	if def.Name != "Project Gateway" {
		t.Fatalf("expected project override name, got %#v", def)
	}
	if def.BaseURL != "https://project.example/v1" {
		t.Fatalf("expected project override base URL, got %#v", def)
	}
	if def.APIKeyEnv != "" {
		t.Fatalf("expected optional API key env to stay empty, got %#v", def)
	}
	if len(def.Models) != 1 || def.Models[0].ID != "project-model" || !def.Models[0].Reasoning {
		t.Fatalf("expected project override models, got %#v", def.Models)
	}
}

func TestLoadProviderDefsDefaultsTypeAndFilenameID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".luc", "providers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "providers", "private-gateway.yaml"), []byte(`name: Private Gateway
base_url: http://localhost:8080/v1
models:
  - id: local-model
`), 0o644); err != nil {
		t.Fatal(err)
	}

	defs, err := LoadProviderDefs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected one provider, got %#v", defs)
	}
	if defs[0].ID != "private-gateway" {
		t.Fatalf("expected filename-derived ID, got %#v", defs[0])
	}
	if defs[0].Type != "openai-compatible" {
		t.Fatalf("expected default provider type, got %#v", defs[0])
	}
	if defs[0].Models[0].Name != "local-model" {
		t.Fatalf("expected model name fallback to ID, got %#v", defs[0].Models[0])
	}
}

func TestLoadProviderDefsSupportsExecProviders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".luc", "providers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "providers", "meli.yaml"), []byte(`id: meli
name: Meli Gateway
type: exec
command: ./adapter.sh
args:
  - --stream
env:
  GATEWAY_MODE: internal
models:
  - id: claude-opus-4-7
    name: Claude Opus 4.7
`), 0o644); err != nil {
		t.Fatal(err)
	}

	defs, err := LoadProviderDefs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected one provider, got %#v", defs)
	}
	def := defs[0]
	if def.Type != "exec" {
		t.Fatalf("expected exec provider type, got %#v", def)
	}
	if def.Command != "./adapter.sh" {
		t.Fatalf("expected exec command, got %#v", def)
	}
	if len(def.Args) != 1 || def.Args[0] != "--stream" {
		t.Fatalf("expected exec args, got %#v", def.Args)
	}
	if def.Env["GATEWAY_MODE"] != "internal" {
		t.Fatalf("expected exec env, got %#v", def.Env)
	}
}

func findSkill(skills []Skill, name string) *Skill {
	for i := range skills {
		if skills[i].Name == name {
			return &skills[i]
		}
	}
	return nil
}
