package kernel

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/provider"
	"github.com/lutefd/luc/internal/tools"
)

// TestMain isolates the user-preference state file (~/.luc/state.yaml) from
// every test in this package. Without this, controller tests read the real
// user's persisted theme/provider/model and wedge on values the test didn't
// anticipate — e.g. "unexpected provider kind 'meli'" when the developer
// has a custom provider selected.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "luc-kernel-state-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	os.Setenv("LUC_STATE_DIR", dir)
	os.Exit(m.Run())
}

type fakeProvider struct {
	streams     [][]provider.Event
	index       int
	lastRequest provider.Request
	requests    []provider.Request
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Start(ctx context.Context, req provider.Request) (provider.Stream, error) {
	_ = ctx
	p.lastRequest = req
	p.requests = append(p.requests, req)
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

func TestControllerSubmitRunsToolLoopAndCanReopenSession(t *testing.T) {
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

	reloaded, err := Open(context.Background(), root, controller.Session().SessionID)
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

func TestNewStartsFreshSessionInsteadOfLatest(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{
			{
				{Type: "text_delta", Text: "saved"},
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

	first, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Submit(context.Background(), "/help"); err != nil {
		t.Fatal(err)
	}
	if err := first.Submit(context.Background(), "save this session"); err != nil {
		t.Fatal(err)
	}

	second, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if second.Session().SessionID == first.Session().SessionID {
		t.Fatalf("expected a fresh session, got reused %q", second.Session().SessionID)
	}
	if len(second.InitialEvents()) != 0 {
		t.Fatalf("expected new session to start empty, got %#v", second.InitialEvents())
	}
}

func TestNewBootstrapsGlobalRuntime(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	newProvider = func(cfg config.ProviderConfig) (provider.Provider, error) {
		_ = cfg
		return &fakeProvider{}, nil
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(home, ".luc")); !os.IsNotExist(err) {
		t.Fatalf("expected clean home before bootstrap, err=%v", err)
	}

	if _, err := New(context.Background(), root); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		filepath.Join(home, ".luc", "tools"),
		filepath.Join(home, ".luc", "providers"),
		filepath.Join(home, ".luc", "skills", "runtime-extension-authoring", "SKILL.md"),
		filepath.Join(home, ".luc", "skills", "skill-usage", "SKILL.md"),
		filepath.Join(home, ".luc", "skills", "theme-creator", "SKILL.md"),
		filepath.Join(home, ".luc", "themes"),
		filepath.Join(home, ".luc", "prompts"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected bootstrap path %q: %v", path, err)
		}
	}
}

func TestControllerCommandsAndReload(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{
			{
				{Type: "text_delta", Text: "ok"},
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

	if err := controller.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
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

func TestControllerAdvertisesSkillsAndLoadsBodyOnToolCall(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{
			{
				{Type: "tool_call", ToolCall: provider.ToolCall{
					ID:        "call_skill",
					Name:      skillToolName,
					Arguments: `{"name":"rails"}`,
				}},
				{Type: "done", Completed: true},
			},
			{
				{Type: "text_delta", Text: "ok"},
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
	if err := os.MkdirAll(filepath.Join(root, ".luc", "skills", "rails"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "skills", "rails", "luc.yaml"), []byte(`interface:
  display_name: Rails
  short_description: Ruby on Rails workflow for migrations and generators.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	skill := `---
name: rails
description: Ruby on Rails workflow for migrations and generators.
---
Prefer bin/rails, migrations, and framework conventions.
`
	if err := os.WriteFile(filepath.Join(root, ".luc", "skills", "rails", "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Submit(context.Background(), "help with rails migration"); err != nil {
		t.Fatal(err)
	}
	if len(providerStub.requests) != 2 {
		t.Fatalf("expected two provider requests, got %d", len(providerStub.requests))
	}
	first := providerStub.requests[0]
	if !strings.Contains(first.System, "Available skills:") || !strings.Contains(first.System, "rails (Rails): Ruby on Rails workflow for migrations and generators.") {
		t.Fatalf("expected skill catalog in system prompt, got %q", first.System)
	}
	if strings.Contains(first.System, "Prefer bin/rails") {
		t.Fatalf("did not expect full skill body in initial system prompt, got %q", first.System)
	}
	second := providerStub.requests[1]
	found := false
	for _, msg := range second.Messages {
		if msg.Role == "tool" && msg.Name == skillToolName && strings.Contains(msg.Content, "<skill_content name=\"rails\">") && strings.Contains(msg.Content, "Prefer bin/rails") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected loaded skill content in follow-up messages, got %#v", second.Messages)
	}

	stored, err := controller.store.Load(controller.Session().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	hidden := false
	for _, ev := range stored {
		if ev.Kind != "tool.finished" {
			continue
		}
		payload := decode[history.ToolResultPayload](ev.Payload)
		if payload.Name != skillToolName {
			continue
		}
		if got, _ := payload.Metadata[tools.MetadataUIHideContent].(bool); got {
			hidden = true
			break
		}
	}
	if !hidden {
		t.Fatal("expected load_skill tool results to hide transcript content")
	}
}

func TestControllerProjectSkillOverrideWinsOverGlobal(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{
			{
				{Type: "tool_call", ToolCall: provider.ToolCall{
					ID:        "call_skill",
					Name:      skillToolName,
					Arguments: `{"name":"rails"}`,
				}},
				{Type: "done", Completed: true},
			},
			{
				{Type: "text_delta", Text: "ok"},
				{Type: "done", Completed: true},
			},
		},
	}
	newProvider = func(cfg config.ProviderConfig) (provider.Provider, error) {
		_ = cfg
		return providerStub, nil
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".luc", "skills", "rails"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".luc", "skills", "rails", "luc.yaml"), []byte(`interface:
  short_description: Global rails workflow.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".luc", "skills", "rails", "SKILL.md"), []byte(`---
name: rails
description: Global rails workflow.
---
Use the global rails workflow.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".luc", "skills", "rails"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "skills", "rails", "luc.yaml"), []byte(`interface:
  short_description: Project-specific rails workflow.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "skills", "rails", "SKILL.md"), []byte(`---
name: rails
description: Project-specific rails workflow.
---
Use the project-specific rails workflow.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Submit(context.Background(), "rails help"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(providerStub.requests[0].System, "Project-specific rails workflow.") || strings.Contains(providerStub.requests[0].System, "Global rails workflow.") {
		t.Fatalf("expected project skill override in catalog, got %q", providerStub.requests[0].System)
	}
	found := false
	for _, msg := range providerStub.requests[1].Messages {
		if msg.Role == "tool" && msg.Name == skillToolName && strings.Contains(msg.Content, "Use the project-specific rails workflow.") && !strings.Contains(msg.Content, "Use the global rails workflow.") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected project skill body in tool output, got %#v", providerStub.requests[1].Messages)
	}
}

func TestControllerSkillCatalogIncludesBuiltins(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{
			{
				{Type: "text_delta", Text: "ok"},
				{Type: "done", Completed: true},
			},
			{
				{Type: "text_delta", Text: "ok"},
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
	if err := controller.Submit(context.Background(), "show me the current branch"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(providerStub.lastRequest.System, "theme-creator (Theme Creator): Create or update luc themes that can be inserted at runtime.") {
		t.Fatalf("expected builtin theme skill in catalog, got %q", providerStub.lastRequest.System)
	}
}

func TestControllerLoadSkillOnlyReturnsFullBodyOncePerSession(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{
			{
				{Type: "tool_call", ToolCall: provider.ToolCall{ID: "call_skill_1", Name: skillToolName, Arguments: `{"name":"rails"}`}},
				{Type: "done", Completed: true},
			},
			{
				{Type: "text_delta", Text: "ok"},
				{Type: "done", Completed: true},
			},
			{
				{Type: "tool_call", ToolCall: provider.ToolCall{ID: "call_skill_2", Name: skillToolName, Arguments: `{"name":"rails"}`}},
				{Type: "done", Completed: true},
			},
			{
				{Type: "text_delta", Text: "ok"},
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
	if err := os.MkdirAll(filepath.Join(root, ".luc", "skills", "rails"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "skills", "rails", "SKILL.md"), []byte(`---
name: rails
description: Rails workflow.
---
Prefer bin/rails.
`), 0o644); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Submit(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if err := controller.Submit(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}

	var firstTool, secondTool string
	for _, msg := range providerStub.requests[1].Messages {
		if msg.Role == "tool" && msg.Name == skillToolName {
			firstTool = msg.Content
		}
	}
	for _, msg := range providerStub.requests[3].Messages {
		if msg.Role == "tool" && msg.Name == skillToolName {
			secondTool = msg.Content
		}
	}
	if !strings.Contains(firstTool, "Prefer bin/rails.") {
		t.Fatalf("expected first load to include full skill body, got %q", firstTool)
	}
	if !strings.Contains(secondTool, "already loaded in this session") {
		t.Fatalf("expected second load to dedupe, got %q", secondTool)
	}
}

func TestControllerSwitchModelUpdatesSessionMeta(t *testing.T) {
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

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	if err := controller.SwitchModel("gpt-5.4"); err != nil {
		t.Fatal(err)
	}
	if controller.Session().Model != "gpt-5.4" {
		t.Fatalf("expected session model to update, got %q", controller.Session().Model)
	}
	if controller.SessionSaved() {
		t.Fatal("expected session to remain unsaved before first message")
	}

	meta, ok, err := controller.store.Latest(controller.Workspace().ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("expected no stored session yet, got %#v", meta)
	}

	providerStub := &fakeProvider{
		streams: [][]provider.Event{
			{
				{Type: "text_delta", Text: "hi"},
				{Type: "done", Completed: true},
			},
		},
	}
	newProvider = func(cfg config.ProviderConfig) (provider.Provider, error) {
		_ = cfg
		return providerStub, nil
	}
	if err := controller.configureSessionProvider(controller.Session()); err != nil {
		t.Fatal(err)
	}
	if err := controller.Submit(context.Background(), "persist it"); err != nil {
		t.Fatal(err)
	}

	meta, ok, err = controller.store.Latest(controller.Workspace().ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected stored session metadata")
	}
	if meta.Model != "gpt-5.4" {
		t.Fatalf("expected persisted model gpt-5.4, got %q", meta.Model)
	}
}

func TestLoadProviderRegistryIncludesRuntimeProviders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".luc", "providers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "providers", "gateway.yaml"), []byte(`id: gateway
name: Private Gateway
base_url: http://localhost:8080/v1
models:
  - id: private-model
    name: Private Model
    description: Local gateway model
    context_k: 512
    reasoning: true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := loadProviderRegistry(root)
	if err != nil {
		t.Fatal(err)
	}

	model, providerDef, ok := reg.FindModel("private-model")
	if !ok {
		t.Fatalf("expected runtime provider model in registry, got %#v", reg.AllModels())
	}
	if providerDef.ID != "gateway" || providerDef.Name != "Private Gateway" {
		t.Fatalf("unexpected runtime provider def: %#v", providerDef)
	}
	if model.Provider != "gateway" || model.ContextK != 512 || !model.Reasoning {
		t.Fatalf("unexpected runtime model def: %#v", model)
	}
}

func TestControllerSwitchModelUsesRuntimeProviderRegistry(t *testing.T) {
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
	if err := os.MkdirAll(filepath.Join(root, ".luc", "providers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".luc", "providers", "gateway.yaml"), []byte(`id: gateway
name: Private Gateway
base_url: http://localhost:8080/v1
models:
  - id: private-model
    name: Private Model
`), 0o644); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := controller.Registry().FindModel("private-model"); !ok {
		t.Fatalf("expected runtime provider model in controller registry, got %#v", controller.Registry().AllModels())
	}

	if err := controller.SwitchModel("private-model"); err != nil {
		t.Fatal(err)
	}
	if controller.Session().Provider != "gateway" {
		t.Fatalf("expected runtime provider kind, got %#v", controller.Session())
	}
	if controller.Session().Model != "private-model" {
		t.Fatalf("expected runtime provider model, got %#v", controller.Session())
	}
}

func TestControllerExecRuntimeProviderRunsToolLoop(t *testing.T) {
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
	providerDir := filepath.Join(root, ".luc", "providers")
	if err := os.MkdirAll(providerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(providerDir, "adapter.sh")
	if err := os.WriteFile(scriptPath, []byte(`#!/bin/sh
input="$(cat)"
if printf '%s' "$input" | grep -q '"tool_call_id":"call_1"'; then
  cat <<'EOF'
{"type":"text_delta","text":"tool finished"}
{"type":"done","completed":true}
EOF
else
  cat <<'EOF'
{"type":"tool_call","tool_call":{"id":"call_1","name":"read","arguments":"{\"path\":\"go.mod\"}"}}
{"type":"done","completed":true}
EOF
fi
`), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(providerDir, "meli.yaml"), []byte(`id: meli
name: Meli Gateway
type: exec
command: ./adapter.sh
models:
  - id: meli-model
    name: Meli Model
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module luc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	controller, err := New(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.SwitchModel("meli-model"); err != nil {
		t.Fatal(err)
	}
	if err := controller.Submit(context.Background(), "run the adapter"); err != nil {
		t.Fatal(err)
	}

	conversation := controller.snapshotConversation()
	if len(conversation) < 4 {
		t.Fatalf("expected conversation with tool loop, got %#v", conversation)
	}
	last := conversation[len(conversation)-1]
	if last.Role != "assistant" || last.Content != "tool finished" {
		t.Fatalf("expected final assistant text from exec provider, got %#v", last)
	}

	stored, err := controller.store.Load(controller.Session().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var sawTool bool
	for _, ev := range stored {
		if ev.Kind == "tool.finished" {
			sawTool = true
			break
		}
	}
	if !sawTool {
		t.Fatalf("expected tool.finished event in %#v", stored)
	}
}

func TestControllerNewAndOpenSession(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{
			{
				{Type: "text_delta", Text: "saved"},
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
	firstID := controller.Session().SessionID
	if err := controller.Submit(context.Background(), "save the first session"); err != nil {
		t.Fatal(err)
	}

	if err := controller.NewSession(); err != nil {
		t.Fatal(err)
	}
	if controller.Session().SessionID == firstID {
		t.Fatalf("expected NewSession to change session id, still %q", controller.Session().SessionID)
	}
	if len(controller.InitialEvents()) != 0 {
		t.Fatalf("expected fresh session after NewSession, got %#v", controller.InitialEvents())
	}

	if err := controller.OpenSession(firstID); err != nil {
		t.Fatal(err)
	}
	if controller.Session().SessionID != firstID {
		t.Fatalf("expected OpenSession to restore %q, got %q", firstID, controller.Session().SessionID)
	}
	if len(controller.InitialEvents()) == 0 {
		t.Fatal("expected restored session events")
	}
}

func TestControllerOnlyPersistsAfterFirstUserMessage(t *testing.T) {
	oldFactory := newProvider
	defer func() { newProvider = oldFactory }()

	providerStub := &fakeProvider{
		streams: [][]provider.Event{
			{
				{Type: "text_delta", Text: "ok"},
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
	if controller.SessionSaved() {
		t.Fatal("expected fresh session to start unsaved")
	}
	if _, ok, err := controller.store.Meta(controller.Session().SessionID); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("expected no meta file before first user message")
	}

	if err := controller.Submit(context.Background(), "/help"); err != nil {
		t.Fatal(err)
	}
	if controller.SessionSaved() {
		t.Fatal("expected commands to keep session unsaved")
	}
	if _, ok, err := controller.store.Meta(controller.Session().SessionID); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("expected no meta file after command-only activity")
	}

	if err := controller.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if !controller.SessionSaved() {
		t.Fatal("expected session to persist after first user message")
	}
	if _, ok, err := controller.store.Meta(controller.Session().SessionID); err != nil {
		t.Fatal(err)
	} else if !ok {
		t.Fatal("expected meta file after first user message")
	}
}
