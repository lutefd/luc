package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRingAddAndSnapshot(t *testing.T) {
	ring := NewRing(2)
	ring.Add("info", "one")
	ring.Add("info", "two")
	ring.Add("warn", "three")

	snapshot := ring.Snapshot()
	if len(snapshot) != 2 {
		t.Fatalf("expected ring limit to apply, got %d entries", len(snapshot))
	}
	if snapshot[0].Message != "two" || snapshot[1].Message != "three" {
		t.Fatalf("unexpected snapshot %#v", snapshot)
	}
}

func TestNewCreatesLogFile(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stateDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}

	manager, err := New(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	manager.Logger.Info("hello")

	data, err := os.ReadFile(filepath.Join(stateDir, "logs", "luc.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("expected log output, got %q", string(data))
	}
	if len(manager.Ring.Snapshot()) == 0 {
		t.Fatal("expected mirrored ring entries")
	}
}
