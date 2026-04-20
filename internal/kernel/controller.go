package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		if shouldSaveMetaForEvent(kind) {
			c.saveSessionMeta()
		}
	}
	c.mu.Lock()
	c.rawEvents = append(c.rawEvents, ev)
	c.eventLog = append(c.eventLog, ev)
	c.mu.Unlock()
	c.mirrorEventToLogs(kind, payload)

	select {
	case c.events <- ev:
	default:
		c.logger.Ring.Add("warn", "dropping UI event because channel is full")
	}
	c.dispatchHooks(ev)
	c.dispatchExtensionObserveEvents(ev)
}

func (c *Controller) mirrorEventToLogs(kind string, payload any) {
	if c.logger == nil || c.logger.Ring == nil {
		return
	}

	level, message, ok := eventLogEntry(kind, payload)
	if !ok {
		return
	}
	c.logger.Ring.Add(level, message)
}

func eventLogEntry(kind string, payload any) (level, message string, ok bool) {
	switch kind {
	case "system.error":
		data := decode[history.MessagePayload](payload)
		if strings.TrimSpace(data.Content) == "" {
			return "", "", false
		}
		return "error", data.Content, true
	case "reload.failed":
		data := decode[history.ReloadPayload](payload)
		message := strings.TrimSpace(data.Error)
		if message == "" {
			message = "reload failed"
		} else {
			message = "reload failed: " + message
		}
		return "error", message, true
	case "tool.finished":
		data := decode[history.ToolResultPayload](payload)
		if strings.TrimSpace(data.Error) == "" {
			return "", "", false
		}
		name := strings.TrimSpace(data.Name)
		if name == "" {
			name = "unknown"
		}
		return "error", fmt.Sprintf("tool %s failed: %s", name, data.Error), true
	default:
		return "", "", false
	}
}

func isNoResponsePlaceholder(text string) bool {
	return strings.EqualFold(strings.TrimSpace(text), noResponseText)
}

func (c *Controller) loadSystemPrompt() string {
	base := "You are luc, the local coding agent running inside luc for this workspace. Use luc tools to inspect files, edit code, and run commands instead of guessing. Be concise, prefer the smallest correct change, and verify important changes with targeted tool calls."
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
	var builder strings.Builder
	builder.WriteString(c.systemPrompt)
	if summary := strings.TrimSpace(c.compactionSummary); summary != "" {
		builder.WriteString("\n\nSession summary from earlier compacted context:\n")
		builder.WriteString(summary)
	}
	if loaded := strings.TrimSpace(c.loadedSkillPromptBlock()); loaded != "" {
		builder.WriteString("\n\n")
		builder.WriteString(loaded)
	}
	for _, ext := range c.matchingPromptExtensions() {
		builder.WriteString("\n\n")
		builder.WriteString(ext.Prompt)
	}
	if relevant := c.relevantSkills(input); len(relevant) > 0 {
		builder.WriteString("\n\nLikely relevant skills for this request:\n")
		builder.WriteString("Before editing luc core code or this repo for luc itself, load the most relevant skill first when the task is about extending luc or adding a runtime capability.\n")
		if c.hasRelevantSkill(relevant, "runtime-extension-authoring") {
			builder.WriteString("luc does support runtime UI manifests via `luc.ui/v1`. New runtime `inspector_tab` and `page` views are supported; only the built-in `Overview` tab remains core-owned.\n")
		}
		for _, skill := range relevant {
			builder.WriteString("- ")
			builder.WriteString(skill.Name)
			label := strings.TrimSpace(skill.DisplayName)
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
		builder.WriteString("Prefer implementing supported capabilities under `~/.luc` or `<workspace>/.luc` before changing core code.\n")
	}
	if catalog := c.skillCatalog(); strings.TrimSpace(catalog) != "" {
		builder.WriteString("\n\nAvailable skills:\n")
		builder.WriteString("Use the `load_skill` tool when a task matches a skill's description or the user explicitly names one.\n")
		builder.WriteString("After loading a skill, follow its instructions and use `read_skill_resource` for referenced bundled files when needed.\n\n")
		builder.WriteString(catalog)
	}
	return strings.TrimSpace(builder.String()), nil
}

func (c *Controller) matchingPromptExtensions() []extensions.PromptExtension {
	if len(c.promptExts) == 0 {
		return nil
	}

	out := make([]extensions.PromptExtension, 0, len(c.promptExts))
	for _, ext := range c.promptExts {
		if ext.Matches(c.config.Provider.Kind, c.config.Provider.Model) {
			out = append(out, ext)
		}
	}
	return out
}

func (c *Controller) hasRelevantSkill(skills []extensions.Skill, name string) bool {
	target := strings.ToLower(strings.TrimSpace(name))
	for _, skill := range skills {
		if strings.ToLower(strings.TrimSpace(skill.Name)) == target {
			return true
		}
	}
	return false
}

func (c *Controller) relevantSkills(input string) []extensions.Skill {
	text := strings.ToLower(strings.TrimSpace(input))
	if text == "" {
		return nil
	}
	var out []extensions.Skill
	for _, skill := range c.skills {
		if skill.Always {
			out = append(out, skill)
			continue
		}
		for _, trigger := range skill.Triggers {
			trigger = strings.ToLower(strings.TrimSpace(trigger))
			if trigger == "" {
				continue
			}
			if strings.Contains(text, trigger) {
				out = append(out, skill)
				break
			}
		}
	}
	return out
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
	prepared, err := c.prepareToolCall(ctx, call)
	if err != nil {
		return tools.Result{}, err
	}
	return c.runPreparedToolCall(ctx, prepared)
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

func toolResponseContent(result history.ToolResultPayload) string {
	if result.Error == "" {
		return result.Content
	}
	return fmt.Sprintf("%s\nerror: %s", result.Content, result.Error)
}

func shouldSaveMetaForEvent(kind string) bool {
	switch kind {
	case "message.assistant.delta", "status.thinking":
		return false
	default:
		return true
	}
}

func nextID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func decode[T any](payload any) T {
	return history.DecodePayload[T](payload)
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
