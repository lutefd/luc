package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMergesGlobalAndProjectConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	globalDir := filepath.Join(home, ".config", "luc")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(`
provider:
  model: global-model
ui:
  inspector_open: false
  theme: dark
`), 0o644); err != nil {
		t.Fatal(err)
	}

	workspaceRoot := t.TempDir()
	projectDir := filepath.Join(workspaceRoot, ".luc")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "config.yaml"), []byte(`
provider:
  model: project-model
  temperature: 0.7
ui:
  inspector_position: right
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Provider.Model != "project-model" {
		t.Fatalf("expected project model override, got %q", cfg.Provider.Model)
	}
	if cfg.Provider.Temperature != 0.7 {
		t.Fatalf("expected temperature override, got %v", cfg.Provider.Temperature)
	}
	if cfg.UI.InspectorOpen {
		t.Fatalf("expected global false override for inspector_open")
	}
	if cfg.UI.InspectorPosition != "right" {
		t.Fatalf("expected project inspector position, got %q", cfg.UI.InspectorPosition)
	}
	if cfg.UI.Theme != "dark" {
		t.Fatalf("expected global theme override, got %q", cfg.UI.Theme)
	}
}

func TestLoadMissingFilesUsesDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	def := Default()
	if cfg != def {
		t.Fatalf("expected defaults, got %#v", cfg)
	}
}
