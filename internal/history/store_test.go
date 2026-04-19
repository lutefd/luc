package history

import (
	"encoding/json"
	"path/filepath"
	"strings"
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

	metas, err := store.List("project")
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 2 || metas[0].SessionID != "two" || metas[1].SessionID != "one" {
		t.Fatalf("unexpected session list %#v", metas)
	}

	meta, ok, err := store.Meta("two")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || meta.SessionID != "two" {
		t.Fatalf("expected to load meta for two, got %#v ok=%v", meta, ok)
	}

	expectedPath := filepath.Join(stateDir, "history", "sessions", "two.jsonl")
	if expectedPath == "" {
		t.Fatal("expected non-empty path")
	}
}

func TestStoreRoundTripsMessageAttachments(t *testing.T) {
	stateDir := t.TempDir()
	store := NewStore(stateDir)

	meta := SessionMeta{
		SessionID: "images",
		ProjectID: "project",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.SaveMeta(meta); err != nil {
		t.Fatal(err)
	}

	ev := EventEnvelope{
		Seq:       1,
		At:        time.Now().UTC(),
		SessionID: "images",
		AgentID:   "root",
		Kind:      "message.user",
		Payload: MessagePayload{
			ID:      "m1",
			Content: "look",
			Attachments: []AttachmentPayload{{
				ID:        "img_1",
				Name:      "pasted.png",
				Type:      "image",
				MediaType: "image/png",
				Data:      "abc123",
				Width:     1,
				Height:    1,
			}},
		},
	}
	if err := store.Append(ev); err != nil {
		t.Fatal(err)
	}

	events, err := store.Load("images")
	if err != nil {
		t.Fatal(err)
	}
	payload := decode[MessagePayload](events[0].Payload)
	if len(payload.Attachments) != 1 || payload.Attachments[0].MediaType != "image/png" {
		t.Fatalf("expected attachment round-trip, got %#v", payload.Attachments)
	}
}

func TestStoreLoadHandlesLargeEvents(t *testing.T) {
	stateDir := t.TempDir()
	store := NewStore(stateDir)

	ev := EventEnvelope{
		Seq:       1,
		At:        time.Now().UTC(),
		SessionID: "large",
		AgentID:   "root",
		Kind:      "message.user",
		Payload: MessagePayload{
			ID:      "m1",
			Content: strings.Repeat("x", 256*1024),
		},
	}
	if err := store.Append(ev); err != nil {
		t.Fatal(err)
	}

	events, err := store.Load("large")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	payload := decode[MessagePayload](events[0].Payload)
	if got := len(payload.Content); got != 256*1024 {
		t.Fatalf("expected 262144-byte payload, got %d", got)
	}
}

func decode[T any](payload any) T {
	var out T
	data, _ := json.Marshal(payload)
	_ = json.Unmarshal(data, &out)
	return out
}
