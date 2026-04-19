package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	// Point at a fresh temp dir with no state file.
	t.Setenv("LUC_STATE_DIR", t.TempDir())

	s, err := Load()
	if err != nil {
		t.Fatalf("expected nil error for missing state, got %v", err)
	}
	if (s != State{}) {
		t.Fatalf("expected zero State, got %+v", s)
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LUC_STATE_DIR", dir)

	want := State{Theme: "purple", ProviderKind: "openai", Model: "gpt-5.4"}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "state.yaml")); err != nil {
		t.Fatalf("state.yaml not created: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, want)
	}
}

func TestUpdateMergesPartial(t *testing.T) {
	t.Setenv("LUC_STATE_DIR", t.TempDir())

	if err := Save(State{Theme: "purple", Model: "gpt-5.4", ProviderKind: "openai"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Only change the theme; other fields must be preserved.
	if err := Update(func(s *State) { s.Theme = "dark" }); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := State{Theme: "dark", Model: "gpt-5.4", ProviderKind: "openai"}
	if got != want {
		t.Fatalf("after partial update got %+v want %+v", got, want)
	}
}

func TestUpdateOnMalformedFileOverwrites(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LUC_STATE_DIR", dir)

	// Write garbage to simulate a corrupt state file.
	if err := os.WriteFile(filepath.Join(dir, "state.yaml"), []byte(":::not yaml:::"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Update should still succeed, writing a fresh file with just the
	// mutation applied. Pragmatic recovery is intentional — see state.go.
	if err := Update(func(s *State) { s.Theme = "light" }); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load after recovery: %v", err)
	}
	want := State{Theme: "light"}
	if got != want {
		t.Fatalf("recovery got %+v want %+v", got, want)
	}
}
