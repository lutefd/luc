package history

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreAppendLoadAndLatest(t *testing.T) {
	stateDir := t.TempDir()
	store := NewStore(stateDir)

	meta1 := SessionMeta{
		SessionID: "one",
		ProjectID: "project",
		CreatedAt: time.Now().Add(-time.Hour),
		UpdatedAt: time.Now().Add(-time.Hour),
	}
	meta2 := SessionMeta{
		SessionID: "two",
		ProjectID: "project",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.SaveMeta(meta1); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMeta(meta2); err != nil {
		t.Fatal(err)
	}

	ev := EventEnvelope{
		Seq:       1,
		At:        time.Now().UTC(),
		SessionID: "two",
		AgentID:   "root",
		Kind:      "message.user",
		Payload:   MessagePayload{ID: "m1", Content: "hello"},
	}
	if err := store.Append(ev); err != nil {
		t.Fatal(err)
	}

	events, err := store.Load("two")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "message.user" {
		t.Fatalf("unexpected events: %#v", events)
	}

	latest, ok, err := store.Latest("project")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected latest session")
	}
	if latest.SessionID != "two" {
		t.Fatalf("expected latest session two, got %q", latest.SessionID)
	}

	expectedPath := filepath.Join(stateDir, "history", "sessions", "two.jsonl")
	if expectedPath == "" {
		t.Fatal("expected non-empty path")
	}
}
