package extensions

import (
	"path/filepath"
	"testing"
)

func TestLoadPromptExtensionsMergesLayeredOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()

	mustWriteRuntimeManifest(t, filepath.Join(home, ".luc", "prompts", "provider.yaml"), `schema: luc.prompt/v1
id: provider-tune
description: Global provider tune
match:
  providers: [openai]
prompt: |
  Use the global provider tune.
`)
	mustWriteRuntimeManifest(t, filepath.Join(root, ".luc", "packages", "prompt-pack@1.0.0", "prompts", "family.yaml"), `schema: luc.prompt/v1
id: family-tune
match:
  model_prefixes: [gpt-5]
prompt: |
  Use the family tune.
`)
	mustWriteRuntimeManifest(t, filepath.Join(root, ".luc", "prompts", "provider.yaml"), `schema: luc.prompt/v1
id: provider-tune
description: Project provider tune
match:
  providers: [openai-compatible]
prompt: |
  Use the project provider tune.
`)

	exts, err := LoadPromptExtensions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(exts) != 2 {
		t.Fatalf("expected two prompt extensions, got %#v", exts)
	}
	if exts[0].ID != "family-tune" || exts[1].ID != "provider-tune" {
		t.Fatalf("expected stable id ordering, got %#v", exts)
	}
	if exts[1].Prompt != "Use the project provider tune." {
		t.Fatalf("expected project override prompt, got %#v", exts[1])
	}
	if exts[1].Description != "Project provider tune" {
		t.Fatalf("expected project override description, got %#v", exts[1])
	}
}

func TestPromptExtensionMatchesProviderAliasesAndModelFamilies(t *testing.T) {
	ext := PromptExtension{
		Providers:     []string{"openai"},
		ModelPrefixes: []string{"gpt-5"},
	}
	if !ext.Matches("openai-compatible", "gpt-5.4") {
		t.Fatalf("expected openai alias + gpt-5 family to match")
	}
	if ext.Matches("acme", "gpt-5.4") {
		t.Fatalf("did not expect provider mismatch to match")
	}
	if ext.Matches("openai-compatible", "claude-opus-4-7") {
		t.Fatalf("did not expect non-matching model family to match")
	}

	exact := PromptExtension{
		Models: []string{"claude-opus-4-7"},
	}
	if !exact.Matches("acme", "claude-opus-4-7") {
		t.Fatalf("expected exact model match")
	}
}
