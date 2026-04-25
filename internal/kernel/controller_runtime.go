package kernel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lutefd/luc/internal/auth"
	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/extensions"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/logging"
	"github.com/lutefd/luc/internal/provider"
	execprovider "github.com/lutefd/luc/internal/provider/exec"
	"github.com/lutefd/luc/internal/provider/openai"
	luruntime "github.com/lutefd/luc/internal/runtime"
	luctstate "github.com/lutefd/luc/internal/state"
	"github.com/lutefd/luc/internal/tools"
	"github.com/lutefd/luc/internal/workspace"
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

type runtimeConfigurableProvider interface {
	SetRuntimeOptions(broker luruntime.UIBroker, hostCapabilities []string)
}

func init() {
	// Seed the default provider registry at package load so the TUI
	// model-selection modal can enumerate built-ins immediately.
	provider.SetDefaultRegistry(seedDefaultRegistry())
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

	// Overlay user-level state (last theme/model/provider) onto config so
	// new sessions start from the user's most recent choice rather than
	// reverting to the config-file defaults on every restart.
	applyUserState(&cfg, registry, logger)

	store := history.NewStore(ws.StateDir)
	providerClient, err := newProvider(cfg.Provider)
	if err != nil {
		logger.Ring.Add("error", err.Error())
	} else {
		configureRuntimeProvider(providerClient, controllerUIBroker(cfg, logger), luruntime.DefaultHostCapabilities())
	}
	toolManager, err := tools.NewManager(ws.Root)
	if err != nil {
		return nil, err
	}
	skills, err := extensions.LoadSkills(ws.Root)
	if err != nil {
		return nil, err
	}
	promptExts, err := extensions.LoadPromptExtensions(ws.Root)
	if err != nil {
		return nil, err
	}
	runtimeSet, err := extensions.LoadRuntimeContributions(ws.Root, luruntime.DefaultHostCapabilities())
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
		promptExts:   promptExts,
		loadedSkills: make(map[string]struct{}),
		hostCaps:     luruntime.DefaultHostCapabilities(),
		runtime:      runtimeSet,
		hookSeen:     map[string]struct{}{},
	}
	toolManager.SetHostedToolInvoker(controller)
	controller.uiBroker = controllerUIBroker(cfg, logger)
	configureRuntimeProvider(controller.provider, controller.recordingUIBroker(), controller.hostCaps)
	for _, diagnostic := range runtimeSet.Diagnostics {
		controller.logger.Ring.Add("warn", diagnostic.Message)
	}
	controller.version.Store(1)
	controller.toolSpecsVersion = toolManager.Version()
	controller.systemPrompt = controller.loadSystemPrompt()

	return controller, nil
}

// SwitchModel hot-swaps the active model. If the model belongs to a
// different provider (or no provider is currently bound), it also rebuilds
// the provider client. The session remains intact — subsequent turns use
// the new model.
func (c *Controller) SwitchModel(modelID string) error {
	return c.SwitchModelForProvider("", modelID)
}

func (c *Controller) SwitchModelForProvider(providerID, modelID string) error {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()

	reg := c.Registry()
	model, providerDef, ok := reg.FindModel(providerID, modelID)
	if !ok {
		// Allow arbitrary model IDs (e.g. fine-tuned names) not in the
		// registry — just set the name and keep the current provider.
		c.config.Provider.Model = modelID
		c.session.Model = modelID
		c.saveSessionMeta()
		c.persistProviderState()
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
		c.persistProviderState()
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
	configureRuntimeProvider(c.provider, c.recordingUIBroker(), c.HostCapabilities())
	c.config.Provider = newCfg
	c.session.Model = newCfg.Model
	c.session.Provider = newCfg.Kind
	c.saveSessionMeta()
	c.persistProviderState()
	c.emit("system.note", history.MessagePayload{
		ID:      nextID("note"),
		Content: fmt.Sprintf("switched to %s / %s", providerDef.Name, model.Name),
	})
	return nil
}

// applyUserState overlays ~/.luc/state.yaml values onto the config so the
// runtime starts from (or returns to) the user's last-used theme/model.
// Called at controller creation AND after Reload — Reload re-reads config
// from disk and would otherwise wipe out any in-memory runtime switches.
// State errors are non-fatal; the function logs and falls through.
func applyUserState(cfg *config.Config, registry *provider.Registry, logger *logging.Manager) {
	st, err := luctstate.Load()
	if err != nil {
		logger.Ring.Add("error", "state: "+err.Error())
		return
	}
	if st.Theme != "" {
		cfg.UI.Theme = st.Theme
	}
	// Only adopt the persisted provider kind if it's still known. A deleted
	// extension would leave state.yaml pointing at a dead provider; falling
	// back to the config default is safer than wedging on an unknown kind.
	// The built-in "openai-compatible" fallback is always accepted because
	// it's not registered under that name but IS honored by newProvider.
	if st.ProviderKind != "" {
		if _, ok := registry.Provider(st.ProviderKind); ok || st.ProviderKind == "openai-compatible" {
			cfg.Provider.Kind = st.ProviderKind
		}
	}
	if st.Model != "" {
		cfg.Provider.Model = st.Model
	}
}

// persistProviderState writes the current provider kind + model to the
// user-level state file. Errors are logged but not returned — persistence
// is a best-effort optimization, not a correctness requirement.
func (c *Controller) persistProviderState() {
	kind := c.config.Provider.Kind
	model := c.config.Provider.Model
	if err := luctstate.Update(func(s *luctstate.State) {
		s.ProviderKind = kind
		s.Model = model
	}); err != nil {
		c.logger.Ring.Add("error", "state: "+err.Error())
	}
}

// SetTheme updates the in-memory UI theme name and persists the choice to
// ~/.luc/state.yaml so subsequent luc launches (new or reopened sessions)
// default to this theme. The TUI is responsible for reloading theme styles
// and rebuilding its views.
func (c *Controller) SetTheme(name string) {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()

	name = strings.TrimSpace(name)
	if c.config.UI.Theme == name {
		return
	}
	c.config.UI.Theme = name

	if err := luctstate.Update(func(s *luctstate.State) {
		s.Theme = name
	}); err != nil {
		c.logger.Ring.Add("error", "state: "+err.Error())
	}

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

	c.shutdownExtensionHosts(ctx, "reload")

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
	promptExts, err := extensions.LoadPromptExtensions(c.workspace.Root)
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
	runtimeSet, err := extensions.LoadRuntimeContributions(c.workspace.Root, c.HostCapabilities())
	if err != nil {
		c.emit("reload.failed", history.ReloadPayload{Version: c.version.Load(), Error: err.Error()})
		return err
	}
	provider.SetDefaultRegistry(registry)

	// Re-apply the user-state overlay so reload doesn't revert the runtime
	// theme/model switches. Without this, ctrl+r reloads config.yaml and
	// the in-memory provider/model regresses to the on-disk default — the
	// model picker then highlights the startup model rather than the one
	// the user actively switched to. Must run before newProvider so the
	// rebuilt client uses the correct model.
	applyUserState(&cfg, registry, c.logger)

	client, err := newProvider(cfg.Provider)
	if err != nil {
		c.emit("reload.failed", history.ReloadPayload{Version: c.version.Load(), Error: err.Error()})
		return err
	}

	// Reload doesn't touch the current session meta (the user is still in
	// the same session), but they may have runtime-switched to a model that
	// now differs from the one stored in session meta after reload. Keep
	// session.Model/Provider in sync with the resolved config so the
	// inspector and picker agree.
	c.session.Model = cfg.Provider.Model
	c.session.Provider = cfg.Provider.Kind

	c.config = cfg
	c.systemPrompt = systemPrompt
	c.skills = skills
	c.promptExts = promptExts
	c.tools = toolManager
	c.tools.SetHostedToolInvoker(c)
	c.registry = registry
	c.provider = client
	configureRuntimeProvider(c.provider, c.recordingUIBroker(), c.HostCapabilities())
	c.runtime = runtimeSet
	if _, ok := c.uiBroker.(*luruntime.DefaultBroker); ok || c.uiBroker == nil {
		c.uiBroker = luruntime.NewDefaultBroker(cfg.UI.ApprovalsMode, func(format string, args ...any) {
			c.logger.Ring.Add("info", fmt.Sprintf(format, args...))
		})
	}
	for _, diagnostic := range runtimeSet.Diagnostics {
		c.logger.Ring.Add("warn", diagnostic.Message)
	}
	c.restartExtensionHosts(ctx, "")
	version := c.version.Add(1)
	c.emit("reload.finished", history.ReloadPayload{Version: version})
	c.logger.Ring.Add("info", fmt.Sprintf("reload finished: runtime version %d", version))
	return nil
}

func configureRuntimeProvider(client provider.Provider, broker luruntime.UIBroker, hostCapabilities []string) {
	if configurable, ok := client.(runtimeConfigurableProvider); ok {
		configurable.SetRuntimeOptions(broker, hostCapabilities)
	}
}

func controllerUIBroker(cfg config.Config, logger *logging.Manager) luruntime.UIBroker {
	return luruntime.NewDefaultBroker(cfg.UI.ApprovalsMode, func(format string, args ...any) {
		logger.Ring.Add("info", fmt.Sprintf(format, args...))
	})
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
				env := cloneStringMap(runtimeDef.Env)
				if keyEnv := strings.TrimSpace(runtimeDef.APIKeyEnv); keyEnv != "" {
					if os.Getenv(keyEnv) == "" {
						if stored, err := auth.Get(runtimeDef.ID); err == nil {
							env[keyEnv] = stored
						}
					}
				}
				return execprovider.New(cfg, execprovider.Spec{
					Name:         runtimeDef.Name,
					Command:      runtimeDef.Command,
					Args:         runtimeDef.Args,
					Env:          env,
					Dir:          filepath.Dir(runtimeDef.SourcePath),
					Capabilities: runtimeDef.Capabilities,
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

func cloneStringMap(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
