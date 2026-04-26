package kernel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lutefd/luc/internal/extensions"
	"github.com/lutefd/luc/internal/history"
	luruntime "github.com/lutefd/luc/internal/runtime"
	"github.com/lutefd/luc/internal/tools"
)

var (
	extensionReadyTimeout     = 3 * time.Second
	extensionShutdownTimeout  = 750 * time.Millisecond
	extensionRestartBaseDelay = 250 * time.Millisecond
	extensionRestartMaxDelay  = 2 * time.Second
	extensionRestartAttempts  = 4
)

const extensionStorageMaxBytes = 64 * 1024

type extensionSupervisor struct {
	controller *Controller

	mu         sync.Mutex
	hosts      map[string]*managedExtensionHost
	desired    map[string]luruntime.ExtensionHost
	generation uint64

	diagMu      sync.Mutex
	diagnostics map[string]luruntime.Diagnostic
}

type managedExtensionHost struct {
	supervisor *extensionSupervisor
	controller *Controller
	def        luruntime.ExtensionHost
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	encoder    *json.Encoder

	encMu      sync.Mutex
	pendingMu  sync.Mutex
	readyOnce  sync.Once
	failOnce   sync.Once
	stopOnce   sync.Once
	requestSeq atomic.Uint64

	readyCh      chan error
	exitCh       chan error
	pending      map[string]chan extensionDecisionResult
	pendingTools map[string]chan hostedToolResult
	stopping     atomic.Bool
	unhealthy    atomic.Bool

	generation     uint64
	restartAttempt int
}

type extensionDecisionResult struct {
	decision luruntime.ExtensionDecisionEnvelope
	err      error
}

type hostedToolResult struct {
	result tools.Result
	err    error
}

type reportedExtensionStartError struct {
	err error
}

func (e reportedExtensionStartError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e reportedExtensionStartError) Unwrap() error {
	return e.err
}

func newExtensionSupervisor(controller *Controller) *extensionSupervisor {
	return &extensionSupervisor{
		controller:  controller,
		hosts:       map[string]*managedExtensionHost{},
		desired:     map[string]luruntime.ExtensionHost{},
		diagnostics: map[string]luruntime.Diagnostic{},
	}
}

func (c *Controller) restartExtensionHosts(ctx context.Context, startupEvent string) {
	if c.extensionHosts == nil {
		c.extensionHosts = newExtensionSupervisor(c)
	}
	c.extensionHosts.Replace(ctx, c.runtime.Extensions.Hosts())
	if strings.TrimSpace(startupEvent) != "" {
		c.extensionHosts.Dispatch(startupEvent, nil, 0, time.Now().UTC())
	}
}

func (c *Controller) shutdownExtensionHosts(ctx context.Context, reason string) {
	if c.extensionHosts == nil {
		return
	}
	c.extensionHosts.Shutdown(ctx, reason)
}

func (c *Controller) dispatchExtensionObserveEvents(ev history.EventEnvelope) {
	if c.extensionHosts == nil {
		return
	}
	switch strings.TrimSpace(ev.Kind) {
	case "message.assistant.final":
		c.extensionHosts.Dispatch(luruntime.ExtensionEventMessageFinal, ev.Payload, ev.Seq, ev.At)
	case "tool.finished":
		c.extensionHosts.Dispatch(luruntime.ExtensionEventToolFinished, ev.Payload, ev.Seq, ev.At)
		payload := history.DecodePayload[history.ToolResultPayload](ev.Payload)
		if strings.TrimSpace(payload.Error) != "" {
			c.extensionHosts.Dispatch(luruntime.ExtensionEventToolError, ev.Payload, ev.Seq, ev.At)
		}
	case "session.compaction":
		c.extensionHosts.Dispatch(luruntime.ExtensionEventCompactionDone, ev.Payload, ev.Seq, ev.At)
	case "reload.finished":
		c.extensionHosts.Dispatch(luruntime.ExtensionEventSessionReload, ev.Payload, ev.Seq, ev.At)
	}
}

func (s *extensionSupervisor) Replace(ctx context.Context, defs []luruntime.ExtensionHost) {
	s.Shutdown(ctx, "reload")
	s.mu.Lock()
	s.desired = make(map[string]luruntime.ExtensionHost, len(defs))
	generation := s.generation
	for _, def := range defs {
		s.desired[strings.ToLower(def.ID)] = def
	}
	s.mu.Unlock()
	s.clearDiagnostics()
	for _, def := range defs {
		s.start(ctx, def, generation, 0)
	}
}

func (s *extensionSupervisor) Shutdown(ctx context.Context, reason string) {
	s.mu.Lock()
	hosts := make([]*managedExtensionHost, 0, len(s.hosts))
	for _, host := range s.hosts {
		hosts = append(hosts, host)
	}
	s.hosts = map[string]*managedExtensionHost{}
	s.desired = map[string]luruntime.ExtensionHost{}
	s.generation++
	s.mu.Unlock()
	s.clearDiagnostics()

	for _, host := range hosts {
		host.shutdown(ctx, reason)
	}
}

func (s *extensionSupervisor) Dispatch(event string, payload any, seq uint64, at time.Time) {
	subscribers := s.controller.runtime.Extensions.ObserveSubscribers(event)
	if len(subscribers) == 0 {
		return
	}

	s.mu.Lock()
	hosts := make([]*managedExtensionHost, 0, len(subscribers))
	for _, subscriber := range subscribers {
		host := s.hosts[strings.ToLower(subscriber.ID)]
		if host != nil {
			hosts = append(hosts, host)
		}
	}
	s.mu.Unlock()

	for _, host := range hosts {
		go host.dispatchEvent(event, payload, seq, at)
	}
}

func (s *extensionSupervisor) RequestDecision(ctx context.Context, binding luruntime.ExtensionBinding, event string, payload any) (luruntime.ExtensionDecisionEnvelope, error) {
	timeout := binding.Subscription.TimeoutMS
	if timeout <= 0 {
		timeout = luruntime.DefaultSyncTimeoutMS(event)
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	s.mu.Lock()
	host := s.hosts[strings.ToLower(binding.Host.ID)]
	s.mu.Unlock()
	if host == nil {
		return luruntime.ExtensionDecisionEnvelope{}, fmt.Errorf("extension %s is not running", binding.Host.ID)
	}
	return host.requestDecision(requestCtx, event, payload)
}

func (s *extensionSupervisor) InvokeHostedTool(ctx context.Context, extensionID, handler string, req luruntime.ToolRequestEnvelope, timeout time.Duration) (tools.Result, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	s.mu.Lock()
	host := s.hosts[strings.ToLower(strings.TrimSpace(extensionID))]
	s.mu.Unlock()
	if host == nil {
		return tools.Result{}, fmt.Errorf("extension %s is not running", extensionID)
	}
	return host.invokeHostedTool(requestCtx, handler, req)
}

func (s *extensionSupervisor) Diagnostics() []luruntime.Diagnostic {
	s.diagMu.Lock()
	defer s.diagMu.Unlock()

	if len(s.diagnostics) == 0 {
		return nil
	}
	keys := make([]string, 0, len(s.diagnostics))
	for key := range s.diagnostics {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]luruntime.Diagnostic, 0, len(keys))
	for _, key := range keys {
		out = append(out, s.diagnostics[key])
	}
	return out
}

func (s *extensionSupervisor) start(ctx context.Context, def luruntime.ExtensionHost, generation uint64, restartAttempt int) {
	if !s.shouldRun(def.ID, generation) {
		return
	}
	host, err := startManagedExtensionHost(ctx, s, s.controller, def, generation, restartAttempt)
	if err != nil {
		var reported reportedExtensionStartError
		if errors.As(err, &reported) {
			return
		}
		s.controller.logger.Ring.Add("error", fmt.Sprintf("extension %s failed to start: %v", def.ID, err))
		s.scheduleRestart(def, generation, restartAttempt+1, err)
		return
	}
	if !s.shouldRun(def.ID, generation) {
		host.shutdown(context.Background(), "superseded")
		return
	}

	s.mu.Lock()
	s.hosts[strings.ToLower(def.ID)] = host
	s.mu.Unlock()
	s.clearDiagnostic(def.ID)
	if restartAttempt > 0 {
		s.controller.logger.Ring.Add("info", fmt.Sprintf("extension %s restarted after failure", def.ID))
	}
}

func (s *extensionSupervisor) scheduleRestart(def luruntime.ExtensionHost, generation uint64, restartAttempt int, err error) {
	if !s.shouldRun(def.ID, generation) {
		return
	}
	if restartAttempt > extensionRestartAttempts {
		message := fmt.Sprintf("extension %s disabled for this session after %d restart attempts: %v", def.ID, extensionRestartAttempts, err)
		s.setDiagnostic(def, message)
		s.controller.logger.Ring.Add("warn", message)
		return
	}

	delay := extensionRestartDelay(restartAttempt)
	message := fmt.Sprintf("extension %s unavailable; retrying in %s (%d/%d): %v", def.ID, delay.Round(time.Millisecond), restartAttempt, extensionRestartAttempts, err)
	s.setDiagnostic(def, message)
	s.controller.logger.Ring.Add("info", message)
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		<-timer.C
		s.start(context.Background(), def, generation, restartAttempt)
	}()
}

func (s *extensionSupervisor) handleRuntimeFailure(host *managedExtensionHost, err error) {
	if host == nil || err == nil || host.stopping.Load() {
		return
	}
	if !s.shouldRun(host.def.ID, host.generation) {
		return
	}

	s.mu.Lock()
	key := strings.ToLower(host.def.ID)
	if current := s.hosts[key]; current == host {
		delete(s.hosts, key)
	}
	s.mu.Unlock()
	s.scheduleRestart(host.def, host.generation, host.restartAttempt+1, err)
}

func (s *extensionSupervisor) shouldRun(extensionID string, generation uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != generation {
		return false
	}
	_, ok := s.desired[strings.ToLower(strings.TrimSpace(extensionID))]
	return ok
}

func (s *extensionSupervisor) clearDiagnostics() {
	s.diagMu.Lock()
	defer s.diagMu.Unlock()
	s.diagnostics = map[string]luruntime.Diagnostic{}
}

func (s *extensionSupervisor) setDiagnostic(def luruntime.ExtensionHost, message string) {
	s.diagMu.Lock()
	defer s.diagMu.Unlock()
	s.diagnostics[strings.ToLower(def.ID)] = luruntime.Diagnostic{
		SourcePath: def.SourcePath,
		Kind:       "extension",
		Message:    message,
	}
}

func (s *extensionSupervisor) clearDiagnostic(extensionID string) {
	s.diagMu.Lock()
	defer s.diagMu.Unlock()
	delete(s.diagnostics, strings.ToLower(strings.TrimSpace(extensionID)))
}

func extensionRestartDelay(restartAttempt int) time.Duration {
	delay := extensionRestartBaseDelay
	for i := 1; i < restartAttempt; i++ {
		delay *= 2
		if delay >= extensionRestartMaxDelay {
			return extensionRestartMaxDelay
		}
	}
	if delay <= 0 {
		return extensionRestartBaseDelay
	}
	return delay
}

func startManagedExtensionHost(ctx context.Context, supervisor *extensionSupervisor, controller *Controller, def luruntime.ExtensionHost, generation uint64, restartAttempt int) (*managedExtensionHost, error) {
	cmd, err := buildExtensionCommand(ctx, def)
	if err != nil {
		return nil, err
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, err
	}

	host := &managedExtensionHost{
		supervisor:     supervisor,
		controller:     controller,
		def:            def,
		cmd:            cmd,
		stdin:          stdin,
		encoder:        json.NewEncoder(stdin),
		readyCh:        make(chan error, 1),
		exitCh:         make(chan error, 1),
		pending:        map[string]chan extensionDecisionResult{},
		pendingTools:   map[string]chan hostedToolResult{},
		generation:     generation,
		restartAttempt: restartAttempt,
	}

	controller.emit("extension.started", history.ExtensionPayload{
		ExtensionID: def.ID,
		SourcePath:  def.SourcePath,
	})

	var stderrBuf bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderrBuf, stderr)
		close(stderrDone)
	}()
	go host.readLoop(stdout)
	go host.waitLoop(&stderrBuf, stderrDone)

	if err := host.send(luruntime.ExtensionHelloEnvelope{
		Type:             "hello",
		ProtocolVersion:  def.ProtocolVersion,
		ExtensionID:      def.ID,
		HostCapabilities: controller.HostCapabilities(),
	}); err != nil {
		host.reportFailure(err)
		return nil, reportedExtensionStartError{err: err}
	}

	readyCtx, cancel := context.WithTimeout(ctx, extensionReadyTimeout)
	defer cancel()
	select {
	case err := <-host.readyCh:
		if err != nil {
			return nil, reportedExtensionStartError{err: err}
		}
	case <-readyCtx.Done():
		host.reportFailure(readyCtx.Err())
		return nil, reportedExtensionStartError{err: readyCtx.Err()}
	}

	var sessionStore, workspaceStore any
	if luruntime.ExtensionHostHasCapability(def, luruntime.HostCapabilityExtensionSessionStorage) || luruntime.ExtensionHostHasCapability(def, luruntime.HostCapabilityExtensionWorkspaceStore) {
		loadedSessionStore, loadedWorkspaceStore, err := loadExtensionStorageSnapshot(controller.workspace.Root, controller.session.SessionID, def.ID)
		if err != nil {
			host.reportFailure(err)
			return nil, reportedExtensionStartError{err: err}
		}
		if luruntime.ExtensionHostHasCapability(def, luruntime.HostCapabilityExtensionSessionStorage) {
			sessionStore = loadedSessionStore
		}
		if luruntime.ExtensionHostHasCapability(def, luruntime.HostCapabilityExtensionWorkspaceStore) {
			workspaceStore = loadedWorkspaceStore
		}
	}
	if err := host.send(luruntime.ExtensionStorageSnapshotEnvelope{
		Type:      "storage_snapshot",
		Session:   sessionStore,
		Workspace: workspaceStore,
	}); err != nil {
		host.reportFailure(err)
		return nil, reportedExtensionStartError{err: err}
	}
	if err := host.send(luruntime.ExtensionSessionEnvelope{
		Type:      "session_start",
		Session:   extensionSessionContext(controller),
		Workspace: extensionWorkspaceContext(controller),
	}); err != nil {
		host.reportFailure(err)
		return nil, reportedExtensionStartError{err: err}
	}
	return host, nil
}

func buildExtensionCommand(ctx context.Context, def luruntime.ExtensionHost) (*exec.Cmd, error) {
	command := strings.TrimSpace(def.Runtime.Command)
	if command == "" {
		return nil, errors.New("runtime.command is required")
	}
	if strings.Contains(command, string(os.PathSeparator)) {
		command = filepath.Clean(filepath.Join(filepath.Dir(def.SourcePath), command))
	}
	cmd := exec.CommandContext(ctx, command, def.Runtime.Args...)
	cmd.Dir = filepath.Dir(def.SourcePath)
	cmd.Env = os.Environ()
	for key, value := range def.Runtime.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	return cmd, nil
}

func (h *managedExtensionHost) dispatchEvent(event string, payload any, seq uint64, at time.Time) {
	if h.unhealthy.Load() || h.stopping.Load() {
		return
	}
	if err := h.send(luruntime.ExtensionEventEnvelope{
		Type:      "event",
		Event:     event,
		Sequence:  seq,
		At:        at.UTC().Format(time.RFC3339Nano),
		Payload:   payload,
		Session:   extensionSessionContext(h.controller),
		Workspace: extensionWorkspaceContext(h.controller),
	}); err != nil {
		h.reportFailure(err)
	}
}

func (h *managedExtensionHost) requestDecision(ctx context.Context, event string, payload any) (luruntime.ExtensionDecisionEnvelope, error) {
	if h.unhealthy.Load() {
		return luruntime.ExtensionDecisionEnvelope{}, fmt.Errorf("extension %s is unhealthy", h.def.ID)
	}
	if h.stopping.Load() {
		return luruntime.ExtensionDecisionEnvelope{}, fmt.Errorf("extension %s is stopping", h.def.ID)
	}

	requestID := fmt.Sprintf("%s_req_%d", safeExtensionPathPart(h.def.ID), h.requestSeq.Add(1))
	resultCh := make(chan extensionDecisionResult, 1)

	h.pendingMu.Lock()
	h.pending[requestID] = resultCh
	h.pendingMu.Unlock()

	err := h.send(luruntime.ExtensionEventEnvelope{
		Type:      "event",
		Event:     event,
		RequestID: requestID,
		At:        time.Now().UTC().Format(time.RFC3339Nano),
		Payload:   payload,
		Session:   extensionSessionContext(h.controller),
		Workspace: extensionWorkspaceContext(h.controller),
	})
	if err != nil {
		h.pendingMu.Lock()
		delete(h.pending, requestID)
		h.pendingMu.Unlock()
		return luruntime.ExtensionDecisionEnvelope{}, err
	}

	select {
	case result := <-resultCh:
		return result.decision, result.err
	case <-ctx.Done():
		h.pendingMu.Lock()
		delete(h.pending, requestID)
		h.pendingMu.Unlock()
		h.reportFailure(ctx.Err())
		return luruntime.ExtensionDecisionEnvelope{}, ctx.Err()
	}
}

func (h *managedExtensionHost) invokeHostedTool(ctx context.Context, handler string, req luruntime.ToolRequestEnvelope) (tools.Result, error) {
	if h.unhealthy.Load() {
		return tools.Result{}, fmt.Errorf("extension %s is unhealthy", h.def.ID)
	}
	if h.stopping.Load() {
		return tools.Result{}, fmt.Errorf("extension %s is stopping", h.def.ID)
	}

	requestID := fmt.Sprintf("%s_tool_%d", safeExtensionPathPart(h.def.ID), h.requestSeq.Add(1))
	resultCh := make(chan hostedToolResult, 1)

	h.pendingMu.Lock()
	h.pendingTools[requestID] = resultCh
	h.pendingMu.Unlock()

	err := h.send(luruntime.HostedToolInvokeEnvelope{
		Type:      "tool_invoke",
		RequestID: requestID,
		Handler:   strings.TrimSpace(handler),
		Tool:      req,
	})
	if err != nil {
		h.pendingMu.Lock()
		delete(h.pendingTools, requestID)
		h.pendingMu.Unlock()
		return tools.Result{}, err
	}

	select {
	case result := <-resultCh:
		return result.result, result.err
	case <-ctx.Done():
		h.pendingMu.Lock()
		delete(h.pendingTools, requestID)
		h.pendingMu.Unlock()
		h.reportFailure(ctx.Err())
		return tools.Result{}, ctx.Err()
	}
}

func (h *managedExtensionHost) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event luruntime.ExtensionHostEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			h.reportFailure(fmt.Errorf("extension emitted invalid JSON: %w", err))
			return
		}
		h.handleEvent(event)
		if h.unhealthy.Load() {
			return
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		h.reportFailure(err)
	}
}

func (h *managedExtensionHost) handleEvent(event luruntime.ExtensionHostEvent) {
	switch strings.TrimSpace(event.Type) {
	case "ready":
		if event.ProtocolVersion != 1 {
			h.reportFailure(fmt.Errorf("extension %s negotiated unsupported protocol version %d", h.def.ID, event.ProtocolVersion))
			return
		}
		h.readyOnce.Do(func() {
			h.controller.emit("extension.ready", history.ExtensionPayload{
				ExtensionID: h.def.ID,
				SourcePath:  h.def.SourcePath,
			})
			h.readyCh <- nil
		})
	case "log":
		if text := kernelFirstNonEmpty(event.Text, event.Message); text != "" {
			h.controller.logger.Ring.Add("info", fmt.Sprintf("extension %s: %s", h.def.ID, text))
		}
	case "progress":
		if text := kernelFirstNonEmpty(event.Progress, event.Message); text != "" {
			h.controller.logger.Ring.Add("info", fmt.Sprintf("extension %s: %s", h.def.ID, text))
		}
	case "client_action":
		if !h.hasCapability(luruntime.CapabilityClientAction) {
			h.reportFailure(errors.New("extension emitted client_action without client_actions capability"))
			return
		}
		if event.Action == nil {
			h.reportFailure(errors.New("extension client_action is missing action payload"))
			return
		}
		action := *event.Action
		if strings.TrimSpace(action.ID) == "" {
			action.ID = nextID("extension_action")
		}
		actionCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		var (
			result luruntime.UIResult
			err    error
		)
		if action.Blocking {
			result, err = h.controller.recordingUIBroker().Request(actionCtx, action)
		} else {
			err = h.controller.recordingUIBroker().Publish(action)
			result = luruntime.UIResult{ActionID: action.ID, Accepted: err == nil}
		}
		if err != nil {
			h.reportFailure(err)
			return
		}
		if err := h.send(luruntime.ClientResultEnvelope{Type: "client_result", Result: result}); err != nil {
			h.reportFailure(err)
		}
	case "storage_update":
		if !h.canUpdateStorage(event.Scope) {
			h.reportFailure(fmt.Errorf("extension emitted storage_update for scope %q without declared storage capability", event.Scope))
			return
		}
		if err := persistExtensionStorageUpdate(h.controller.workspace.Root, h.controller.session.SessionID, h.def.ID, event.Scope, event.Value); err != nil {
			h.reportFailure(err)
		}
	case "decision":
		h.resolveDecision(luruntime.ExtensionDecisionEnvelope{
			Type:                event.Type,
			RequestID:           event.RequestID,
			Decision:            event.Decision,
			Message:             event.Message,
			Text:                event.Text,
			SystemAppend:        append([]string(nil), event.SystemAppend...),
			HiddenContext:       append([]string(nil), event.HiddenContext...),
			Arguments:           cloneAnyMap(event.Arguments),
			Content:             event.Content,
			Metadata:            cloneAnyMap(event.Metadata),
			Error:               event.Error,
			CollapsedSummary:    event.CollapsedSummary,
			ErrorClassification: event.ErrorClassification,
		})
	case "tool_result":
		h.resolveHostedToolResult(event)
	case "tools.register":
		if !h.hasCapability(luruntime.HostCapabilityDynamicTools) {
			h.reportFailure(errors.New("extension emitted tools.register without tools.dynamic capability"))
			return
		}
		h.registerDynamicTools(event)
	case "error":
		h.reportFailure(errors.New(kernelFirstNonEmpty(event.Error, event.Message, "extension failed")))
	case "done":
		return
	default:
		h.reportFailure(fmt.Errorf("unsupported extension message type %q", event.Type))
	}
}

func (h *managedExtensionHost) hasCapability(capability string) bool {
	return luruntime.ExtensionHostHasCapability(h.def, capability)
}

func (h *managedExtensionHost) canUpdateStorage(scope string) bool {
	switch strings.TrimSpace(scope) {
	case "session":
		return h.hasCapability(luruntime.HostCapabilityExtensionSessionStorage)
	case "workspace":
		return h.hasCapability(luruntime.HostCapabilityExtensionWorkspaceStore)
	default:
		return false
	}
}

func (h *managedExtensionHost) registerDynamicTools(event luruntime.ExtensionHostEvent) {
	defs := make([]extensions.ToolDef, 0, len(event.Tools))
	for _, dynamic := range event.Tools {
		def, err := extensions.DynamicToolDef(h.def.ID, dynamic)
		if err != nil {
			h.reportFailure(err)
			return
		}
		defs = append(defs, def)
	}
	if h.controller.tools == nil {
		h.reportFailure(errors.New("dynamic tool registration requires a tool manager"))
		return
	}
	if err := h.controller.tools.RegisterDynamicTools(h.def.ID, defs); err != nil {
		h.reportFailure(err)
		return
	}
	h.controller.emit("tools.registered", history.ExtensionPayload{ExtensionID: h.def.ID, SourcePath: h.def.SourcePath})
}

func (h *managedExtensionHost) resolveDecision(decision luruntime.ExtensionDecisionEnvelope) {
	requestID := strings.TrimSpace(decision.RequestID)
	if requestID == "" {
		h.reportFailure(errors.New("extension decision is missing request_id"))
		return
	}

	h.pendingMu.Lock()
	resultCh := h.pending[requestID]
	if resultCh != nil {
		delete(h.pending, requestID)
	}
	h.pendingMu.Unlock()
	if resultCh == nil {
		return
	}
	resultCh <- extensionDecisionResult{decision: decision}
}

func (h *managedExtensionHost) resolveHostedToolResult(event luruntime.ExtensionHostEvent) {
	requestID := strings.TrimSpace(event.RequestID)
	if requestID == "" {
		h.reportFailure(errors.New("extension tool_result is missing request_id"))
		return
	}

	h.pendingMu.Lock()
	resultCh := h.pendingTools[requestID]
	if resultCh != nil {
		delete(h.pendingTools, requestID)
	}
	h.pendingMu.Unlock()
	if resultCh == nil {
		return
	}

	if event.Result == nil && strings.TrimSpace(event.Error) == "" {
		resultCh <- hostedToolResult{err: errors.New("extension tool_result is missing result payload")}
		return
	}

	result := tools.Result{}
	if event.Result != nil {
		result.Content = event.Result.Content
		result.Metadata = cloneAnyMap(event.Result.Metadata)
		result.DefaultCollapsed = event.Result.DefaultCollapsed
		result.CollapsedSummary = event.Result.CollapsedSummary
	}
	if strings.TrimSpace(event.Error) != "" {
		resultCh <- hostedToolResult{result: result, err: errors.New(strings.TrimSpace(event.Error))}
		return
	}
	resultCh <- hostedToolResult{result: result}
}

func (h *managedExtensionHost) waitLoop(stderrBuf *bytes.Buffer, stderrDone <-chan struct{}) {
	err := h.cmd.Wait()
	<-stderrDone

	if stderrText := strings.TrimSpace(stderrBuf.String()); stderrText != "" {
		h.controller.logger.Ring.Add("warn", fmt.Sprintf("extension %s stderr: %s", h.def.ID, stderrText))
	}

	if h.stopping.Load() {
		h.exitCh <- nil
		return
	}
	if err == nil {
		err = errors.New("extension host exited unexpectedly")
	}
	if stderrText := strings.TrimSpace(stderrBuf.String()); stderrText != "" && err != nil {
		err = fmt.Errorf("%w: %s", err, stderrText)
	}
	h.reportFailure(err)
	h.exitCh <- err
}

func (h *managedExtensionHost) shutdown(ctx context.Context, reason string) {
	h.stopOnce.Do(func() {
		h.stopping.Store(true)
		_ = h.send(luruntime.ExtensionShutdownEnvelope{
			Type:   "session_shutdown",
			Reason: strings.TrimSpace(reason),
		})

		waitCtx, cancel := context.WithTimeout(ctx, extensionShutdownTimeout)
		defer cancel()
		select {
		case <-waitCtx.Done():
			if h.cmd.Process != nil {
				_ = h.cmd.Process.Kill()
			}
			select {
			case <-h.exitCh:
			case <-time.After(250 * time.Millisecond):
			}
		case <-h.exitCh:
		}

		h.controller.emit("extension.stopped", history.ExtensionPayload{
			ExtensionID: h.def.ID,
			EventKind:   luruntime.ExtensionEventSessionShutdown,
			SourcePath:  h.def.SourcePath,
		})
	})
}

func (h *managedExtensionHost) send(message any) error {
	if h.unhealthy.Load() || h.stopping.Load() && !isShutdownEnvelope(message) {
		return nil
	}
	h.encMu.Lock()
	defer h.encMu.Unlock()
	return h.encoder.Encode(message)
}

func (h *managedExtensionHost) reportFailure(err error) {
	if err == nil {
		return
	}
	h.failOnce.Do(func() {
		h.unhealthy.Store(true)
		h.pendingMu.Lock()
		pending := make([]chan extensionDecisionResult, 0, len(h.pending))
		pendingTools := make([]chan hostedToolResult, 0, len(h.pendingTools))
		for id, ch := range h.pending {
			delete(h.pending, id)
			pending = append(pending, ch)
		}
		for id, ch := range h.pendingTools {
			delete(h.pendingTools, id)
			pendingTools = append(pendingTools, ch)
		}
		h.pendingMu.Unlock()
		h.readyOnce.Do(func() {
			h.readyCh <- err
		})
		for _, ch := range pending {
			ch <- extensionDecisionResult{err: err}
		}
		for _, ch := range pendingTools {
			ch <- hostedToolResult{err: err}
		}
		h.controller.emit("extension.failed", history.ExtensionPayload{
			ExtensionID: h.def.ID,
			SourcePath:  h.def.SourcePath,
			Error:       err.Error(),
		})
		h.controller.logger.Ring.Add("error", fmt.Sprintf("extension %s failed: %v", h.def.ID, err))
		if h.supervisor != nil {
			h.supervisor.handleRuntimeFailure(h, err)
		}
		if !h.stopping.Load() && h.cmd.Process != nil {
			_ = h.cmd.Process.Kill()
		}
	})
}

func isShutdownEnvelope(message any) bool {
	switch msg := message.(type) {
	case luruntime.ExtensionShutdownEnvelope:
		return msg.Type == "session_shutdown"
	default:
		return false
	}
}

func extensionSessionContext(controller *Controller) map[string]any {
	return map[string]any{
		"session_id": controller.session.SessionID,
		"provider":   controller.session.Provider,
		"model":      controller.session.Model,
	}
}

func extensionWorkspaceContext(controller *Controller) map[string]any {
	return map[string]any{
		"root":       controller.workspace.Root,
		"project_id": controller.workspace.ProjectID,
		"branch":     controller.workspace.Branch,
	}
}

func loadExtensionStorageSnapshot(workspaceRoot, sessionID, extensionID string) (any, any, error) {
	sessionValue, err := loadExtensionStorageValue(extensionSessionStoragePath(workspaceRoot, sessionID, extensionID))
	if err != nil {
		return nil, nil, err
	}
	workspaceValue, err := loadExtensionStorageValue(extensionWorkspaceStoragePath(workspaceRoot, extensionID))
	if err != nil {
		return nil, nil, err
	}
	return sessionValue, workspaceValue, nil
}

func persistExtensionStorageUpdate(workspaceRoot, sessionID, extensionID, scope string, value any) error {
	scope = strings.TrimSpace(scope)
	switch scope {
	case "session":
		return writeExtensionStorageValue(extensionSessionStoragePath(workspaceRoot, sessionID, extensionID), value)
	case "workspace":
		return writeExtensionStorageValue(extensionWorkspaceStoragePath(workspaceRoot, extensionID), value)
	default:
		return fmt.Errorf("unsupported extension storage scope %q", scope)
	}
}

func loadExtensionStorageValue(path string) (any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func writeExtensionStorageValue(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > extensionStorageMaxBytes {
		return fmt.Errorf("extension storage value exceeds %d bytes", extensionStorageMaxBytes)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func extensionSessionStoragePath(workspaceRoot, sessionID, extensionID string) string {
	return filepath.Join(workspaceRoot, ".luc", "extensions", "sessions", safeExtensionPathPart(sessionID), safeExtensionPathPart(extensionID)+".json")
}

func extensionWorkspaceStoragePath(workspaceRoot, extensionID string) string {
	return filepath.Join(workspaceRoot, ".luc", "extensions", "workspace", safeExtensionPathPart(extensionID)+".json")
}

func safeExtensionPathPart(value string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_")
	value = replacer.Replace(strings.TrimSpace(value))
	if value == "" {
		return "default"
	}
	return value
}

func cloneAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}
