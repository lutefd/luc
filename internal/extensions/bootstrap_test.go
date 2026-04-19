package extensions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureGlobalRuntimeCreatesDirsAndSeedsAssets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := EnsureGlobalRuntime(); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(home, ".luc")
	for _, dir := range []string{
		root,
		filepath.Join(root, "tools"),
		filepath.Join(root, "providers"),
		filepath.Join(root, "skills"),
		filepath.Join(root, "themes"),
		filepath.Join(root, "prompts"),
		filepath.Join(root, "skills", "runtime-extension-authoring"),
		filepath.Join(root, "skills", "skill-usage"),
		filepath.Join(root, "skills", "theme-creator"),
	} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("expected %q to exist: %v", dir, err)
		}
	}

	for _, path := range []string{
		filepath.Join(root, "skills", "runtime-extension-authoring", "luc.yaml"),
		filepath.Join(root, "skills", "runtime-extension-authoring", "SKILL.md"),
		filepath.Join(root, "skills", "skill-usage", "luc.yaml"),
		filepath.Join(root, "skills", "skill-usage", "SKILL.md"),
		filepath.Join(root, "skills", "theme-creator", "luc.yaml"),
		filepath.Join(root, "skills", "theme-creator", "SKILL.md"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected seeded asset %q: %v", path, err)
		}
		if len(data) == 0 {
			t.Fatalf("expected seeded asset %q to be non-empty", path)
		}
	}
}

func TestEnsureGlobalRuntimeDoesNotOverwriteExistingAssets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, ".luc", "skills", "skill-usage", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureGlobalRuntime(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "custom" {
		t.Fatalf("expected existing asset to be preserved, got %q", string(data))
	}
}
