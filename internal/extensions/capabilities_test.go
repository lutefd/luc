package extensions

import (
	"os"
	"path/filepath"
	"testing"

	luruntime "github.com/lutefd/luc/internal/runtime"
)

func TestParseToolDefSupportsCapabilityManifestShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider-status.yaml")
	content := `schema: luc.tool/v1
name: provider_status
description: Show provider status.
runtime:
  kind: exec
  command: ./provider_status.py
  capabilities:
    - structured_io
    - client_actions
input_schema:
  type: object
  properties: {}
timeout_seconds: 30
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	def, err := parseToolDef(path)
	if err != nil {
		t.Fatal(err)
	}
	if def.Command != "./provider_status.py" {
		t.Fatalf("expected runtime command, got %#v", def)
	}
	if len(def.Capabilities) != 2 || def.Capabilities[0] != luruntime.CapabilityStructuredIO || def.Capabilities[1] != luruntime.CapabilityClientAction {
		t.Fatalf("unexpected capabilities %#v", def.Capabilities)
	}
	if def.TimeoutSeconds != 30 {
		t.Fatalf("expected timeout, got %#v", def)
	}
}

func TestParseProviderDefSupportsCapabilities(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meli.yaml")
	content := `id: meli
name: Meli Gateway
type: exec
command: ./adapter.py
capabilities:
  - client_actions
models:
  - id: gpt-5.4
    name: GPT-5.4
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	def, err := parseProviderDef(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Capabilities) != 1 || def.Capabilities[0] != luruntime.CapabilityClientAction {
		t.Fatalf("unexpected capabilities %#v", def.Capabilities)
	}
}
