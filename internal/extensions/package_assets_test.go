package extensions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadToolDefsIncludesInstalledPackageAssets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := t.TempDir()
	path := filepath.Join(root, ".luc", "packages", "provider-console@0.1.0", "tools", "provider_console_status.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`name: provider_console_status
description: Show provider console status.
command: printf ok
schema:
  type: object
  properties: {}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	defs, err := LoadToolDefs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Name != "provider_console_status" {
		t.Fatalf("expected package tool def, got %#v", defs)
	}
}

func TestLoadToolDefsIncludesUserScopePackagesAndProjectOverridesThem(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := t.TempDir()

	globalPath := filepath.Join(home, ".luc", "packages", "github.com_acme_pkg@v1.0.0", "tools", "status.yaml")
	if err := os.MkdirAll(filepath.Dir(globalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalPath, []byte(`name: status
description: Global status.
command: printf global
schema:
  type: object
  properties: {}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectPath := filepath.Join(root, ".luc", "packages", "github.com_acme_pkg@v1.1.0", "tools", "status.yaml")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(`name: status
description: Project status.
command: printf project
schema:
  type: object
  properties: {}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	defs, err := LoadToolDefs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected one merged tool def, got %#v", defs)
	}
	if defs[0].Command != "printf project" || defs[0].Description != "Project status." {
		t.Fatalf("expected project package to override user package, got %#v", defs[0])
	}
}
