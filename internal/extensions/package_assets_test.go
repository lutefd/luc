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
