package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/extensions"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/logging"
	"github.com/lutefd/luc/internal/provider"
	execprovider "github.com/lutefd/luc/internal/provider/exec"
	"github.com/lutefd/luc/internal/provider/openai"
	"github.com/lutefd/luc/internal/tools"
	"github.com/lutefd/luc/internal/workspace"
)

const (
	skillToolName         = "load_skill"
	skillResourceToolName = "read_skill_resource"
)

var newProvider = func(cfg config.ProviderConfig) (provider.Provider, error) {
	// Prefer a provider registered in the registry by ID if one matches
	// the config's Kind; fall back to the built-in OpenAI client so
	// existing configs ("openai-compatible") keep working unchanged.
	reg := provider.DefaultRegistry()
	if def, ok := reg.Provider(cfg.Kind); ok && def.Factory != nil {
		return def.Factory(cfg)
	}
	return openai.New(cfg)
}

func init() {
	// Seed the default provider registry at package load so the TUI
	// model-selection modal can enumerate built-ins immediately.
	provider.SetDefaultRegistry(seedDefaultRegistry())
}

type Controller struct {
	workspace workspace.Info
	config    config.Config
	store     *history.Store
	logger    *logging.Manager
	provider  provider.Provider
	registry  *provider.Registry
	tools     *tools.Manager

	session history.SessionMeta
	events  chan history.EventEnvelope
	initial []history.EventEnvelope

	seq     atomic.Uint64
	version atomic.Uint64

	mu           sync.Mutex
	turnMu       sync.Mutex
	sessionSaved bool
	conversation []provider.Message
	systemPrompt string
	skills       []extensions.Skill
	loadedSkills map[string]struct{}
}

func New(ctx context.Context, cwd string) (*Controller, error) {
	controller, err := newController(ctx, cwd)
	if err != nil {
		return nil, err
	}
	if err := controller.startNewSession(); err != nil {
		return nil, err
	}
	return controller, nil
}

func Open(ctx context.Context, cwd, sessionID string) (*Controller, error) {
	controller, err := newController(ctx, cwd)
	if err != nil {
		return nil, err
	}
	if err := controller.loadSessionByID(sessionID); err != nil {
		return nil, err
	}
	return controller, nil
}

func ResumeLatest(ctx context.Context, cwd string) (*Controller, error) {
	controller, err := newController(ctx, cwd)
	if err != nil {
		return nil, err
	}
	if err := controller.loadLatestSession(); err != nil {
		return nil, err
	}
	return controller, nil
}

func newController(ctx context.Context, cwd string) (*Controller, error) {
	_ = ctx
	ws, err := workspace.Detect(cwd)
	if err != nil {
		return nil, err
	}

	if err := extensions.EnsureGlobalRuntime(); err != nil {
		return nil, err
	}

	cfg, err := config.Load(ws.Root)
	if err != nil {
		return nil, err
	}

	logger, err := logging.New(ws.StateDir)
	if err != nil {
		return nil, err
	}

	registry, err := loadProviderRegistry(ws.Root)
	if err != nil {
		return nil, err
	}
	provider.SetDefaultRegistry(registry)

	store := history.NewStore(ws.StateDir)
	providerClient, err := newProvider(cfg.Provider)
	if err != nil {
		logger.Ring.Add("error", err.Error())
	}
	toolManager, err := tools.NewManager(ws.Root)
	if err != nil {
		return nil, err
	}
	skills, err := extensions.LoadSkills(ws.Root)
	if err != nil {
		return nil, err
	}

	controller := &Controller{
		workspace:    ws,
		config:       cfg,
		store:        store,
		logger:       logger,
		provider:     providerClient,
		registry:     registry,
		tools:        toolManager,
		events:       make(chan history.EventEnvelope, 256),
		skills:       skills,
		loadedSkills: make(map[string]struct{}),
	}
	controller.version.Store(1)
	controller.systemPrompt = controller.loadSystemPrompt()

	return controller, nil
}

func (c *Controller) Workspace() workspace.Info {
	return c.workspace
}

func (c *Controller) Config() config.Config {
	return c.config
}

func (c *Controller) Session() history.SessionMeta {
	return c.session
}

func (c *Controller) SessionSaved() bool {
	return c.sessionSaved
}

func (c *Controller) Sessions() ([]history.SessionMeta, error) {
	return c.store.List(c.workspace.ProjectID)
}

func (c *Controller) Events() <-chan history.EventEnvelope {
	return c.events
}

func (c *Controller) InitialEvents() []history.EventEnvelope {
	out := make([]history.EventEnvelope, len(c.initial))
	copy(out, c.initial)
	return out
}

func (c *Controller) LogEntries() []logging.Entry {
	return c.logger.Ring.Snapshot()
}

// Registry returns the provider registry that populates the model-selection
// modal. Exposed so the TUI can read available models without importing the
// package-level default directly.
func (c *Controller) Registry() *provider.Registry {
	if c.registry != nil {
		return c.registry
	}
	return provider.DefaultRegistry()
}

// SwitchModel hot-swaps the active model. If the model belongs to a
// different provider (or no provider is currently bound), it also rebuilds
// the provider client. The session remains intact — subsequent turns use
// the new model.
func (c *Controller) SwitchModel(modelID string) error {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()

	reg := c.Registry()
	model, providerDef, ok := reg.FindModel(modelID)
	if !ok {
		// Allow arbitrary model IDs (e.g. fine-tuned names) not in the
		// registry — just set the name and keep the current provider.
		c.config.Provider.Model = modelID
		c.session.Model = modelID
		c.saveSessionMeta()
		c.emit("system.note", history.MessagePayload{
			ID:      nextID("note"),
			Content: fmt.Sprintf("switched model to %s", modelID),
		})
		return nil
	}

	// Same provider, same kind: just update the model string.
	if c.config.Provider.Kind == providerDef.ID || c.config.Provider.Kind == "openai-compatible" && providerDef.ID == "openai" {
		c.config.Provider.Model = model.ID
		c.session.Model = model.ID
		c.session.Provider = c.config.Provider.Kind
		c.saveSessionMeta()
		c.emit("system.note", history.MessagePayload{
			ID:      nextID("note"),
			Content: fmt.Sprintf("switched model to %s (%s)", model.Name, providerDef.Name),
		})
		return nil
	}

	// Different provider: rebuild the client.
	newCfg := c.config.Provider
	newCfg.Kind = providerDef.ID
	newCfg.Model = model.ID
	client, err := providerDef.Factory(newCfg)
	if err != nil {
		c.emit("system.error", history.MessagePayload{
			ID:      nextID("error"),
			Content: fmt.Sprintf("switch model failed: %v", err),
		})
		return err
	}
	c.provider = client
	c.config.Provider = newCfg
	c.session.Model = newCfg.Model
	c.session.Provider = newCfg.Kind
	c.saveSessionMeta()
	c.emit("system.note", history.MessagePayload{
		ID:      nextID("note"),
		Content: fmt.Sprintf("switched to %s / %s", providerDef.Name, model.Name),
	})
	return nil
}

// SetTheme updates the in-memory UI theme name. The TUI is responsible for
// reloading theme styles and rebuilding its views; this method only mutates
// configuration state so that subsequent reads see the new value. The change
// is not persisted to disk.
func (c *Controller) SetTheme(name string) {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()

	name = strings.TrimSpace(name)
	if c.config.UI.Theme == name {
		return
	}
	c.config.UI.Theme = name

	displayed := name
	if displayed == "" {
		displayed = "default"
	}
	c.emit("system.note", history.MessagePayload{
		ID:      nextID("note"),
		Content: fmt.Sprintf("switched theme to %s", displayed),
	})
}

func (c *Controller) Reload(ctx context.Context) error {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()

	cfg, err := config.Load(c.workspace.Root)
	if err != nil {
		c.emit("reload.failed", history.ReloadPayload{Version: c.version.Load(), Error: err.Error()})
		return err
	}

	systemPrompt := c.loadSystemPrompt()
	skills, err := extensions.LoadSkills(c.workspace.Root)
	if err != nil {
		c.emit("reload.failed", history.ReloadPayload{Version: c.version.Load(), Error: err.Error()})
		return err
	}
	toolManager, err := tools.NewManager(c.workspace.Root)
	if err != nil {
		c.emit("reload.failed", history.ReloadPayload{Version: c.version.Load(), Error: err.Error()})
		return err
	}
	registry, err := loadProviderRegistry(c.workspace.Root)
	if err != nil {
		c.emit("reload.failed", history.ReloadPayload{Version: c.version.Load(), Error: err.Error()})
		return err
	}
	provider.SetDefaultRegistry(registry)

	client, err := newProvider(cfg.Provider)
	if err != nil {
		c.emit("reload.failed", history.ReloadPayload{Version: c.version.Load(), Error: err.Error()})
		return err
	}

	c.config = cfg
	c.systemPrompt = systemPrompt
	c.skills = skills
	c.tools = toolManager
	c.registry = registry
	c.provider = client
	version := c.version.Add(1)
	c.emit("reload.finished", history.ReloadPayload{Version: version})
	c.logger.Ring.Add("info", fmt.Sprintf("reload finished: runtime version %d", version))
	return nil
}

func (c *Controller) NewSession() error {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	return c.startNewSession()
}

func (c *Controller) OpenSession(sessionID string) error {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	return c.loadSessionByID(sessionID)
}

func (c *Controller) Submit(ctx context.Context, input string) error {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()

	text := strings.TrimSpace(input)
	if text == "" {
		return nil
	}

	if strings.HasPrefix(text, "/") {
		return c.handleCommand(ctx, text)
	}

	if c.provider == nil {
		err := errors.New("provider is not ready; check API key configuration")
		c.emit("system.error", history.MessagePayload{ID: nextID("error"), Content: err.Error()})
		return err
	}

	userID := nextID("user")
	c.emit("message.user", history.MessagePayload{ID: userID, Content: text})
	c.appendMessage(provider.Message{Role: "user", Content: text})
	c.updateTitle(text)

	systemPrompt, err := c.composeSystemPrompt(text)
	if err != nil {
		c.emit("system.error", history.MessagePayload{ID: nextID("error"), Content: err.Error()})
		return err
	}

	for range 8 {
		stream, err := c.provider.Start(ctx, provider.Request{
			Model:       c.config.Provider.Model,
			System:      systemPrompt,
			Messages:    c.snapshotConversation(),
			Tools:       c.toolSpecs(),
			Temperature: c.config.Provider.Temperature,
			MaxTokens:   c.config.Provider.MaxTokens,
		})
		if err != nil {
			c.emit("system.error", history.MessagePayload{ID: nextID("error"), Content: err.Error()})
			return err
		}

		assistantID := nextID("assistant")
		var builder strings.Builder
		var calls []provider.ToolCall

		for {
			ev, err := stream.Recv()
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				_ = stream.Close()
				return err
			}
			if errors.Is(err, os.ErrClosed) || errors.Is(err, context.Canceled) {
				_ = stream.Close()
				return err
			}
			if err != nil {
				if errors.Is(err, os.ErrClosed) || errors.Is(err, context.Canceled) {
					_ = stream.Close()
					return err
				}
				if errors.Is(err, io.EOF) {
					break
				}
				_ = stream.Close()
				if errors.Is(err, context.Canceled) {
					return err
				}
				return err
			}

			switch ev.Type {
			case "thinking":
				text := strings.TrimSpace(ev.Text)
				if text == "" {
					text = "Thinking..."
				}
				c.emit("status.thinking", history.StatusPayload{Text: text})
			case "text_delta":
				builder.WriteString(ev.Text)
				c.emit("message.assistant.delta", history.MessageDeltaPayload{ID: assistantID, Delta: ev.Text})
			case "tool_call":
				calls = append(calls, ev.ToolCall)
			case "done":
				goto streamDone
			}
		}
	streamDone:
		_ = stream.Close()

		if len(calls) > 0 {
			payload := history.ToolCallBatchPayload{ID: assistantID}
			assistantMsg := provider.Message{Role: "assistant", ToolCalls: calls}
			for _, call := range calls {
				payload.Calls = append(payload.Calls, history.ToolCallPayload{
					ID:        call.ID,
					Name:      call.Name,
					Arguments: call.Arguments,
				})
			}
			c.emit("message.assistant.tool_calls", payload)
			c.appendMessage(assistantMsg)

			for _, call := range calls {
				c.emit("tool.requested", history.ToolCallPayload{
					ID:        call.ID,
					Name:      call.Name,
					Arguments: call.Arguments,
				})
				result, err := c.runToolCall(ctx, call)
				payload := history.ToolResultPayload{
					ID:       call.ID,
					Name:     call.Name,
					Content:  result.Content,
					Metadata: result.Metadata,
				}
				if err != nil {
					payload.Error = err.Error()
				}
				c.emit("tool.finished", payload)
				c.appendMessage(provider.Message{
					Role:       "tool",
					ToolCallID: call.ID,
					Name:       call.Name,
					Content:    toolResponseContent(payload),
				})
			}
			continue
		}

		final := strings.TrimSpace(builder.String())
		if final == "" {
			final = "No response."
		}
		c.emit("message.assistant.final", history.MessagePayload{ID: assistantID, Content: final})
		c.appendMessage(provider.Message{Role: "assistant", Content: final})
		return nil
	}

	return errors.New("exceeded tool loop limit")
}

func (c *Controller) handleCommand(ctx context.Context, text string) error {
	switch strings.TrimSpace(text) {
	case "/reload":
		c.emit("reload.started", history.ReloadPayload{Version: c.version.Load()})
		return c.Reload(ctx)
	case "/help":
		c.emit("system.note", history.MessagePayload{
			ID:      nextID("help"),
			Content: "Commands: /reload, /help",
		})
		return nil
	default:
		c.emit("system.error", history.MessagePayload{
			ID:      nextID("error"),
			Content: fmt.Sprintf("unknown command: %s", text),
		})
		return nil
	}
}

func (c *Controller) emit(kind string, payload any) {
	ev := history.EventEnvelope{
		Seq:       c.seq.Add(1),
		At:        time.Now().UTC(),
		SessionID: c.session.SessionID,
		AgentID:   "root",
		Kind:      kind,
		Payload:   payload,
	}
	c.persistSessionForEvent(kind)
	if c.sessionSaved {
		_ = c.store.Append(ev)
		c.session.UpdatedAt = ev.At
		c.saveSessionMeta()
	}

	select {
	case c.events <- ev:
	default:
		c.logger.Ring.Add("warn", "dropping UI event because channel is full")
	}
}

func (c *Controller) startNewSession() error {
	now := time.Now().UTC()
	meta := history.SessionMeta{
		SessionID: nextID("sess"),
		ProjectID: c.workspace.ProjectID,
		CreatedAt: now,
		UpdatedAt: now,
		Provider:  c.config.Provider.Kind,
		Model:     c.config.Provider.Model,
		Title:     defaultSessionTitle(c.workspace.Root),
	}
	return c.applySession(meta, nil)
}

func (c *Controller) loadSessionByID(sessionID string) error {
	meta, ok, err := c.store.Meta(sessionID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	if meta.ProjectID != c.workspace.ProjectID {
		return fmt.Errorf("session %s does not belong to this project", sessionID)
	}

	events, err := c.store.Load(meta.SessionID)
	if err != nil {
		return err
	}
	return c.applySession(meta, events)
}

func (c *Controller) loadLatestSession() error {
	meta, ok, err := c.store.Latest(c.workspace.ProjectID)
	if err != nil {
		return err
	}
	if !ok {
		return c.startNewSession()
	}
	return c.loadSessionByID(meta.SessionID)
}

func (c *Controller) applySession(meta history.SessionMeta, events []history.EventEnvelope) error {
	if err := c.configureSessionProvider(meta); err != nil {
		return err
	}

	c.session = meta
	c.sessionSaved = len(events) > 0
	c.seq.Store(0)
	c.initial = append([]history.EventEnvelope(nil), events...)
	c.loadedSkills = make(map[string]struct{})
	c.mu.Lock()
	c.conversation = nil
	c.mu.Unlock()
	for _, ev := range events {
		if ev.Seq > c.seq.Load() {
			c.seq.Store(ev.Seq)
		}
		c.replay(ev)
	}

	return nil
}

func (c *Controller) configureSessionProvider(meta history.SessionMeta) error {
	if strings.TrimSpace(meta.Provider) != "" {
		c.config.Provider.Kind = meta.Provider
	}
	if strings.TrimSpace(meta.Model) != "" {
		c.config.Provider.Model = meta.Model
	}

	client, err := newProvider(c.config.Provider)
	if err != nil {
		c.provider = nil
		c.logger.Ring.Add("error", err.Error())
		return nil
	}
	c.provider = client
	return nil
}

func loadProviderRegistry(workspaceRoot string) (*provider.Registry, error) {
	reg := seedDefaultRegistry()
	defs, err := extensions.LoadProviderDefs(workspaceRoot)
	if err != nil {
		return nil, err
	}
	for _, def := range defs {
		reg.Register(runtimeProviderDef(def))
	}
	return reg, nil
}

func runtimeProviderDef(def extensions.ProviderDef) provider.ProviderDef {
	runtimeDef := def
	models := make([]provider.ModelDef, 0, len(runtimeDef.Models))
	for _, model := range runtimeDef.Models {
		models = append(models, provider.ModelDef{
			ID:          model.ID,
			Name:        model.Name,
			Description: model.Description,
			ContextK:    model.ContextK,
			Provider:    runtimeDef.ID,
			Reasoning:   model.Reasoning,
		})
	}

	return provider.ProviderDef{
		ID:   runtimeDef.ID,
		Name: runtimeDef.Name,
		Factory: func(cfg config.ProviderConfig) (provider.Provider, error) {
			switch runtimeDef.Type {
			case "exec":
				return execprovider.New(cfg, execprovider.Spec{
					Name:    runtimeDef.Name,
					Command: runtimeDef.Command,
					Args:    runtimeDef.Args,
					Env:     runtimeDef.Env,
					Dir:     filepath.Dir(runtimeDef.SourcePath),
				})
			default:
				runtimeCfg := cfg
				runtimeCfg.Kind = runtimeDef.ID
				runtimeCfg.BaseURL = runtimeDef.BaseURL
				runtimeCfg.APIKeyEnv = runtimeDef.APIKeyEnv
				return openai.New(runtimeCfg)
			}
		},
		Models: models,
	}
}

func (c *Controller) replay(ev history.EventEnvelope) {
	switch ev.Kind {
	case "message.user":
		payload := decode[history.MessagePayload](ev.Payload)
		c.conversation = append(c.conversation, provider.Message{Role: "user", Content: payload.Content})
	case "message.assistant.final":
		payload := decode[history.MessagePayload](ev.Payload)
		c.conversation = append(c.conversation, provider.Message{Role: "assistant", Content: payload.Content})
	case "message.assistant.tool_calls":
		payload := decode[history.ToolCallBatchPayload](ev.Payload)
		msg := provider.Message{Role: "assistant"}
		for _, call := range payload.Calls {
			msg.ToolCalls = append(msg.ToolCalls, provider.ToolCall{
				ID:        call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
			})
		}
		c.conversation = append(c.conversation, msg)
	case "tool.finished":
		payload := decode[history.ToolResultPayload](ev.Payload)
		if payload.Name == "load_skill" {
			if skillName, _ := payload.Metadata["skill_name"].(string); strings.TrimSpace(skillName) != "" {
				c.loadedSkills[strings.ToLower(skillName)] = struct{}{}
			}
		}
		c.conversation = append(c.conversation, provider.Message{
			Role:       "tool",
			ToolCallID: payload.ID,
			Name:       payload.Name,
			Content:    toolResponseContent(payload),
		})
	}
}

func (c *Controller) appendMessage(msg provider.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conversation = append(c.conversation, msg)
}

func (c *Controller) snapshotConversation() []provider.Message {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]provider.Message, len(c.conversation))
	copy(out, c.conversation)
	return out
}

func (c *Controller) loadSystemPrompt() string {
	base := "You are luc, a local coding agent. Work inside the workspace, explain actions clearly, and prefer the smallest correct change."
	paths := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".luc", "prompts", "system.md"))
	}
	paths = append(paths, filepath.Join(c.workspace.StateDir, "prompts", "system.md"))

	content := base
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if candidate := strings.TrimSpace(string(data)); candidate != "" {
			content = candidate
		}
	}
	return content
}

func (c *Controller) composeSystemPrompt(input string) (string, error) {
	_ = input
	var builder strings.Builder
	builder.WriteString(c.systemPrompt)
	if catalog := c.skillCatalog(); strings.TrimSpace(catalog) != "" {
		builder.WriteString("\n\nAvailable skills:\n")
		builder.WriteString("Use the `load_skill` tool when a task matches a skill's description or the user explicitly names one.\n")
		builder.WriteString("After loading a skill, follow its instructions and use `read_skill_resource` for referenced bundled files when needed.\n\n")
		builder.WriteString(catalog)
	}
	return strings.TrimSpace(builder.String()), nil
}

func (c *Controller) skillCatalog() string {
	if len(c.skills) == 0 {
		return ""
	}
	var builder strings.Builder
	for _, skill := range c.skills {
		label := strings.TrimSpace(skill.DisplayName)
		if label == "" {
			label = skill.Name
		}
		builder.WriteString("- ")
		builder.WriteString(skill.Name)
		if label != "" && label != skill.Name {
			builder.WriteString(" (")
			builder.WriteString(label)
			builder.WriteString(")")
		}
		if desc := strings.TrimSpace(skill.Description); desc != "" {
			builder.WriteString(": ")
			builder.WriteString(desc)
		}
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func (c *Controller) toolSpecs() []provider.ToolSpec {
	specs := append([]provider.ToolSpec(nil), c.tools.Specs()...)
	if spec := c.skillToolSpec(); spec.Name != "" {
		specs = append(specs, spec)
	}
	if spec := c.skillResourceToolSpec(); spec.Name != "" {
		specs = append(specs, spec)
	}
	return specs
}

func (c *Controller) skillToolSpec() provider.ToolSpec {
	if len(c.skills) == 0 {
		return provider.ToolSpec{}
	}

	enum := make([]string, 0, len(c.skills))
	for _, skill := range c.skills {
		enum = append(enum, fmt.Sprintf("%q", skill.Name))
	}

	return provider.ToolSpec{
		Name: skillToolName,
		Description: "Load the full instructions for an available skill by name. " +
			"Use this when a task matches a skill's description or the user explicitly names a skill.",
		Schema: json.RawMessage(fmt.Sprintf(`{
			"type":"object",
			"properties":{
				"name":{"type":"string","enum":[%s]}
			},
			"required":["name"]
		}`, strings.Join(enum, ","))),
	}
}

func (c *Controller) skillResourceToolSpec() provider.ToolSpec {
	if len(c.skills) == 0 {
		return provider.ToolSpec{}
	}

	enum := make([]string, 0, len(c.skills))
	for _, skill := range c.skills {
		if strings.TrimSpace(skill.BaseDir) == "" {
			continue
		}
		enum = append(enum, fmt.Sprintf("%q", skill.Name))
	}
	if len(enum) == 0 {
		return provider.ToolSpec{}
	}

	return provider.ToolSpec{
		Name: skillResourceToolName,
		Description: "Read a bundled file referenced by a previously loaded skill. " +
			"Paths are relative to the skill directory.",
		Schema: json.RawMessage(fmt.Sprintf(`{
			"type":"object",
			"properties":{
				"name":{"type":"string","enum":[%s]},
				"path":{"type":"string"}
			},
			"required":["name","path"]
		}`, strings.Join(enum, ","))),
	}
}

func (c *Controller) runToolCall(ctx context.Context, call provider.ToolCall) (tools.Result, error) {
	switch call.Name {
	case skillToolName:
		return c.runLoadSkillTool(call.Arguments)
	case skillResourceToolName:
		return c.runReadSkillResourceTool(call.Arguments)
	default:
		return c.tools.Run(ctx, tools.Request{
			Name:      call.Name,
			Arguments: call.Arguments,
			Workspace: c.workspace.Root,
			SessionID: c.session.SessionID,
			AgentID:   "root",
		})
	}
}

func (c *Controller) runLoadSkillTool(raw string) (tools.Result, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return tools.Result{}, err
	}
	skill, ok := c.skillByName(args.Name)
	if !ok {
		return tools.Result{}, fmt.Errorf("unknown skill %q", args.Name)
	}
	if _, loaded := c.loadedSkills[strings.ToLower(skill.Name)]; loaded {
		return normalizeCustomToolResult(tools.Result{
			Content: fmt.Sprintf("skill %s is already loaded in this session", skill.Name),
			Metadata: map[string]any{
				"skill_name":                skill.Name,
				"already_loaded":            true,
				tools.MetadataUIHideContent: true,
				tools.MetadataUILabel:       fmt.Sprintf("skill loaded %s", skill.Name),
			},
		}), nil
	}
	prompt, err := extensions.ResolveSkillPrompt(skill)
	if err != nil {
		return tools.Result{}, err
	}
	c.loadedSkills[strings.ToLower(skill.Name)] = struct{}{}

	label := strings.TrimSpace(skill.DisplayName)
	if label == "" {
		label = skill.Name
	}
	content := renderSkillContent(skill, label, prompt)
	return normalizeCustomToolResult(tools.Result{
		Content: content,
		Metadata: map[string]any{
			"skill_name":                skill.Name,
			"skill_path":                skill.BodyPath,
			"skill_dir":                 skill.BaseDir,
			"resources":                 skillResources(skill),
			tools.MetadataUIHideContent: true,
			tools.MetadataUILabel:       fmt.Sprintf("skill loaded %s", skill.Name),
		},
	}), nil
}

func (c *Controller) runReadSkillResourceTool(raw string) (tools.Result, error) {
	var args struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return tools.Result{}, err
	}
	skill, ok := c.skillByName(args.Name)
	if !ok {
		return tools.Result{}, fmt.Errorf("unknown skill %q", args.Name)
	}
	if strings.TrimSpace(skill.BaseDir) == "" {
		return tools.Result{}, fmt.Errorf("skill %s has no bundled resources", skill.Name)
	}
	path, err := safeSkillPath(skill.BaseDir, args.Path)
	if err != nil {
		return tools.Result{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tools.Result{}, err
	}
	return normalizeCustomToolResult(tools.Result{
		Content:          string(data),
		DefaultCollapsed: true,
		CollapsedSummary: fmt.Sprintf("Read %s from skill %s.", args.Path, skill.Name),
		Metadata: map[string]any{
			"skill_name": skill.Name,
			"path":       path,
		},
	}), nil
}

func (c *Controller) skillByName(name string) (extensions.Skill, bool) {
	target := strings.ToLower(strings.TrimSpace(name))
	for _, skill := range c.skills {
		if strings.ToLower(skill.Name) == target {
			return skill, true
		}
	}
	return extensions.Skill{}, false
}

func renderSkillContent(skill extensions.Skill, label, prompt string) string {
	var builder strings.Builder
	builder.WriteString("<skill_content name=\"")
	builder.WriteString(skill.Name)
	builder.WriteString("\">\n")
	builder.WriteString("# ")
	builder.WriteString(label)
	builder.WriteString("\n\n")
	if desc := strings.TrimSpace(skill.Description); desc != "" {
		builder.WriteString(desc)
		builder.WriteString("\n\n")
	}
	builder.WriteString(prompt)
	if dir := strings.TrimSpace(skill.BaseDir); dir != "" {
		builder.WriteString("\n\nSkill directory: ")
		builder.WriteString(dir)
		builder.WriteString("\nRelative paths in this skill are relative to the skill directory.")
		if resources := skillResources(skill); len(resources) > 0 {
			builder.WriteString("\n\n<skill_resources>\n")
			for _, resource := range resources {
				builder.WriteString("<file>")
				builder.WriteString(resource)
				builder.WriteString("</file>\n")
			}
			builder.WriteString("</skill_resources>")
		}
	}
	builder.WriteString("\n</skill_content>")
	return builder.String()
}

func skillResources(skill extensions.Skill) []string {
	if strings.TrimSpace(skill.BaseDir) == "" {
		return nil
	}
	var out []string
	_ = filepath.WalkDir(skill.BaseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if samePath(path, skill.SourcePath) || samePath(path, skill.BodyPath) {
			return nil
		}
		rel, err := filepath.Rel(skill.BaseDir, path)
		if err != nil {
			return nil
		}
		out = append(out, rel)
		if len(out) >= 64 {
			return fs.SkipAll
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func safeSkillPath(root, target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", errors.New("path is required")
	}
	path := target
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes skill directory", target)
	}
	return path, nil
}

func samePath(a, b string) bool {
	return strings.TrimSpace(a) != "" && strings.TrimSpace(a) == strings.TrimSpace(b)
}

func normalizeCustomToolResult(result tools.Result) tools.Result {
	if result.Metadata == nil {
		result.Metadata = map[string]any{}
	}
	if hidden, _ := result.Metadata[tools.MetadataUIHideContent].(bool); hidden {
		delete(result.Metadata, tools.MetadataUIDefaultCollapsed)
		delete(result.Metadata, tools.MetadataUICollapsedSummary)
		return result
	}
	if result.DefaultCollapsed {
		result.Metadata[tools.MetadataUIDefaultCollapsed] = true
	}
	if summary := strings.TrimSpace(result.CollapsedSummary); summary != "" {
		result.Metadata[tools.MetadataUICollapsedSummary] = summary
	}
	return result
}

func (c *Controller) updateTitle(input string) {
	if c.session.Title != defaultSessionTitle(c.workspace.Root) {
		return
	}
	title := strings.Join(strings.Fields(strings.TrimSpace(input)), " ")
	if len(title) > 72 {
		title = title[:72]
	}
	c.session.Title = title
	c.saveSessionMeta()
}

func defaultSessionTitle(workspaceRoot string) string {
	return filepath.Base(workspaceRoot)
}

func (c *Controller) persistSessionForEvent(kind string) {
	if c.sessionSaved || kind != "message.user" {
		return
	}
	c.sessionSaved = true
	c.saveSessionMeta()
}

func (c *Controller) saveSessionMeta() {
	if !c.sessionSaved {
		return
	}
	_ = c.store.SaveMeta(c.session)
}

func toolResponseContent(result history.ToolResultPayload) string {
	if result.Error == "" {
		return result.Content
	}
	return fmt.Sprintf("%s\nerror: %s", result.Content, result.Error)
}

func nextID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func decode[T any](payload any) T {
	var out T
	data, _ := jsonMarshal(payload)
	_ = jsonUnmarshal(data, &out)
	return out
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
