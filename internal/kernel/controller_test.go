package kernel

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/provider"
)

type fakeProvider struct {
	streams [][]provider.Event
	index   int
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Start(ctx context.Context, req provider.Request) (provider.Stream, error) {
	_ = ctx
	_ = req
	events := p.streams[p.index]
	p.index++
	return &fakeStream{events: events}, nil
}

type fakeStream struct {
	events []provider.Event
	index  int
}

func (s *fakeStream) Recv() (provider.Event, error) {
	if s.index >= len(s.events) {
		return provider.Event{}, io.EOF
	}
	ev := s.events[s.index]
	s.index++
	return ev, nil
}

func (s *fakeStream) Close() error { return nil }

func TestControllerSubmitRunsToolLoopAndRestoresSession(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{
			{
				{Type: "tool_call", ToolCall: provider.ToolCall{
					ID:        "call_1",
					Name:      "bash",
					Arguments: `{"command":"printf hello"}`,
				}},
				{Type: "done", Completed: true},
			},
			{
				{Type: "text_delta", Text: "tool finished"},
				{Type: "done", Completed: true},
			},
		},
	}
	newProvider = func(cfg config.ProviderConfig) (provider.Provider, error) {
		_ = cfg
		return providerStub, nil
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	if err := controller.Submit(context.Background(), "run the command"); err != nil {
		t.Fatal(err)
	}

	events := controller.InitialEvents()
	if len(events) != 0 {
		t.Fatalf("expected no initial events for new session, got %d", len(events))
	}

	stored, err := controller.store.Load(controller.Session().SessionID)
	if err != nil {
		t.Fatal(err)
	}

	kinds := []string{}
	for _, ev := range stored {
		kinds = append(kinds, ev.Kind)
	}
	for _, kind := range []string{
		"message.user",
		"message.assistant.tool_calls",
		"tool.requested",
		"tool.finished",
		"message.assistant.final",
	} {
		found := false
		for _, got := range kinds {
			if got == kind {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected event kind %q in %#v", kind, kinds)
		}
	}

	reloadedProvider := &fakeProvider{streams: [][]provider.Event{}}
	newProvider = func(cfg config.ProviderConfig) (provider.Provider, error) {
		_ = cfg
		return reloadedProvider, nil
	}

	reloaded, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	if reloaded.Session().SessionID != controller.Session().SessionID {
		t.Fatalf("expected session reuse, got %q vs %q", reloaded.Session().SessionID, controller.Session().SessionID)
	}
	if len(reloaded.InitialEvents()) == 0 {
		t.Fatal("expected replayed initial events")
	}
	if len(reloaded.snapshotConversation()) < 3 {
		t.Fatalf("expected replayed conversation, got %#v", reloaded.snapshotConversation())
	}
}

func TestControllerCommandsAndReload(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	newProvider = func(cfg config.ProviderConfig) (provider.Provider, error) {
		_ = cfg
		return &fakeProvider{}, nil
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".luc", "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "prompts", "system.md"), []byte("custom prompt"), 0o644); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got := controller.Workspace().Root; got != root {
		t.Fatalf("expected workspace root %q, got %q", root, got)
	}
	if got := controller.Config().Provider.Kind; got != "openai-compatible" {
		t.Fatalf("unexpected provider kind %q", got)
	}
	if len(controller.LogEntries()) != 0 {
		t.Fatalf("expected empty startup logs, got %#v", controller.LogEntries())
	}

	if err := controller.Submit(context.Background(), "/help"); err != nil {
		t.Fatal(err)
	}
	if err := controller.Submit(context.Background(), "/unknown"); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if controller.systemPrompt != "custom prompt" {
		t.Fatalf("expected custom system prompt, got %q", controller.systemPrompt)
	}
	if controller.version.Load() < 2 {
		t.Fatalf("expected reload to increment version, got %d", controller.version.Load())
	}

	stored, err := controller.store.Load(controller.Session().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var foundHelp, foundUnknown, foundReload bool
	for _, ev := range stored {
		switch ev.Kind {
		case "system.note":
			foundHelp = true
		case "system.error":
			foundUnknown = true
		case "reload.finished":
			foundReload = true
		}
	}
	if !foundHelp || !foundUnknown || !foundReload {
		t.Fatalf("expected command/reload events, got %#v", stored)
	}
}
