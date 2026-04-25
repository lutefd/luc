package kernel

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/extensions"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/logging"
	"github.com/lutefd/luc/internal/provider"
	luruntime "github.com/lutefd/luc/internal/runtime"
	"github.com/lutefd/luc/internal/tools"
	"github.com/lutefd/luc/internal/workspace"
)

const (
	skillToolName         = "load_skill"
	skillResourceToolName = "read_skill_resource"
)

type Controller struct {
	workspace      workspace.Info
	config         config.Config
	store          *history.Store
	logger         *logging.Manager
	provider       provider.Provider
	registry       *provider.Registry
	tools          *tools.Manager
	uiBroker       luruntime.UIBroker
	hostCaps       []string
	runtime        luruntime.ContributionSet
	extensionHosts *extensionSupervisor

	session  history.SessionMeta
	events   chan history.EventEnvelope
	initial  []history.EventEnvelope
	eventLog []history.EventEnvelope

	seq        atomic.Uint64
	version    atomic.Uint64
	turnActive atomic.Bool

	mu                sync.Mutex
	turnMu            sync.Mutex
	turnCancel        context.CancelFunc
	sessionSaved      bool
	rawEvents         []history.EventEnvelope
	conversation      []provider.Message
	compactionSummary string
	systemPrompt      string
	toolSpecsVersion  uint64
	skills            []extensions.Skill
	promptExts        []extensions.PromptExtension
	loadedSkills      map[string]struct{}
	hookSeen          map[string]struct{}
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

func (c *Controller) TurnActive() bool {
	return c.turnActive.Load()
}

func (c *Controller) CancelTurn() bool {
	c.mu.Lock()
	cancel := c.turnCancel
	active := c.turnActive.Load()
	c.mu.Unlock()
	if !active || cancel == nil {
		return false
	}
	cancel()
	return true
}

func (c *Controller) Close() error {
	c.shutdownExtensionHosts(context.Background(), "close")
	if c.store == nil {
		return nil
	}
	return c.store.Close()
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

func (c *Controller) SessionEvents() []history.EventEnvelope {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]history.EventEnvelope, len(c.eventLog))
	copy(out, c.eventLog)
	return out
}

func (c *Controller) VisibleEvents() []history.EventEnvelope {
	return history.VisibleEvents(c.SessionEvents())
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

func (c *Controller) AvailableModels() []provider.ModelDef {
	return c.Registry().AllModels()
}

func (c *Controller) HostCapabilities() []string {
	out := make([]string, len(c.hostCaps))
	copy(out, c.hostCaps)
	return out
}

func (c *Controller) SetUIBroker(broker luruntime.UIBroker) {
	if broker == nil {
		broker = luruntime.NewDefaultBroker(c.config.UI.ApprovalsMode, func(format string, args ...any) {
			c.logger.Ring.Add("info", fmt.Sprintf(format, args...))
		})
	}
	c.uiBroker = broker
	configureRuntimeProvider(c.provider, c.recordingUIBroker(), c.hostCaps)
}

func (c *Controller) UIBroker() luruntime.UIBroker {
	if c.uiBroker == nil {
		c.uiBroker = luruntime.NewDefaultBroker(c.config.UI.ApprovalsMode, func(format string, args ...any) {
			c.logger.Ring.Add("info", fmt.Sprintf(format, args...))
		})
	}
	return c.uiBroker
}

func (c *Controller) RuntimeContributions() luruntime.ContributionSet {
	return c.runtime
}

func (c *Controller) RuntimeDiagnostics() []luruntime.Diagnostic {
	out := make([]luruntime.Diagnostic, 0, len(c.runtime.Diagnostics)+4)
	out = append(out, c.runtime.Diagnostics...)
	if c.extensionHosts != nil {
		out = append(out, c.extensionHosts.Diagnostics()...)
	}
	return out
}

func (c *Controller) RenderRuntimeView(ctx context.Context, viewID string) (luruntime.RuntimeView, tools.Result, error) {
	view, ok := c.runtime.UI.View(viewID)
	if !ok {
		return luruntime.RuntimeView{}, tools.Result{}, fmt.Errorf("runtime view %q not found", viewID)
	}
	if err := c.maybeAuthorizeTool(ctx, view.SourceTool, `{}`); err != nil {
		return luruntime.RuntimeView{}, tools.Result{}, err
	}
	result, err := c.tools.Run(ctx, tools.Request{
		Name:             view.SourceTool,
		Arguments:        `{}`,
		Workspace:        c.workspace.Root,
		SessionID:        c.session.SessionID,
		AgentID:          "root",
		HostCapabilities: c.HostCapabilities(),
		ViewContext: &luruntime.ViewContext{
			ViewID:    view.ID,
			Placement: view.Placement,
		},
		UIBroker: c.recordingUIBroker(),
	})
	return view, result, err
}

func isNoResponsePlaceholder(text string) bool {
	return strings.EqualFold(strings.TrimSpace(text), noResponseText)
}
