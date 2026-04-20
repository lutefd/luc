package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/kernel"
	"github.com/lutefd/luc/internal/media"
	luruntime "github.com/lutefd/luc/internal/runtime"
	"github.com/lutefd/luc/internal/runtime/viewrender"
)

type Options struct {
	Controller *kernel.Controller
	Stdin      io.Reader
	Stdout     io.Writer
}

type Mode struct {
	controller *kernel.Controller
	writer     *jsonlWriter
	broker     *Broker
	busy       atomic.Bool
	promptWG   sync.WaitGroup
	reportErr  func(error)
}

type incomingRecord struct {
	command  *Command
	parseErr error
}

func Run(ctx context.Context, opts Options) error {
	if opts.Controller == nil {
		return errors.New("rpc controller is required")
	}
	if opts.Stdin == nil {
		return errors.New("rpc stdin is required")
	}
	if opts.Stdout == nil {
		return errors.New("rpc stdout is required")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	mode := &Mode{
		controller: opts.Controller,
		writer:     newJSONLWriter(opts.Stdout),
		broker:     NewBroker(),
	}
	mode.controller.SetUIBroker(mode.broker)

	records := make(chan incomingRecord, 32)

	var (
		wg     sync.WaitGroup
		errMu  sync.Mutex
		runErr error
	)
	recordErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if runErr == nil {
			runErr = err
		}
		errMu.Unlock()
		cancel()
	}
	mode.reportErr = recordErr

	wg.Add(3)

	go func() {
		defer wg.Done()
		defer close(records)

		scanner := newJSONLScanner(opts.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var command Command
			if err := decodeLine([]byte(scanner.Text()), &command); err != nil {
				select {
				case records <- incomingRecord{parseErr: err}:
				case <-runCtx.Done():
					return
				}
				continue
			}
			select {
			case records <- incomingRecord{command: &command}:
			case <-runCtx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
			recordErr(err)
		}
	}()

	go func() {
		defer wg.Done()
		defer cancel()

		for {
			select {
			case <-runCtx.Done():
				return
			case record, ok := <-records:
				if !ok {
					return
				}
				if record.parseErr != nil {
					if err := mode.writeError("", "parse", fmt.Sprintf("failed to parse command: %v", record.parseErr)); err != nil {
						recordErr(err)
						return
					}
					continue
				}
				if err := mode.handleCommand(runCtx, *record.command); err != nil {
					recordErr(err)
					return
				}
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case <-runCtx.Done():
				return
			case ev, ok := <-mode.controller.Events():
				if !ok {
					return
				}
				if err := mode.writer.WriteJSONLine(EventFrame{
					Type:  "event",
					Event: ev,
				}); err != nil {
					recordErr(err)
					return
				}
			}
		}
	}()

	wg.Wait()

	cancel()
	mode.controller.CancelTurn()
	mode.broker.Close()
	mode.promptWG.Wait()

	closeErr := mode.controller.Close()
	if runErr != nil {
		return errors.Join(runErr, closeErr)
	}
	return closeErr
}

func (m *Mode) handleCommand(ctx context.Context, command Command) error {
	commandType := strings.TrimSpace(command.Type)
	switch commandType {
	case "get_state":
		return m.writeSuccess(command.ID, commandType, m.state())
	case "get_events":
		data, err := m.getEvents(command.Scope, command.SinceSeq)
		if err != nil {
			return m.writeError(command.ID, commandType, err.Error())
		}
		return m.writeSuccess(command.ID, commandType, data)
	case "get_logs":
		return m.writeSuccess(command.ID, commandType, LogsResponseData{Entries: m.controller.LogEntries()})
	case "list_sessions":
		sessions, err := m.controller.Sessions()
		if err != nil {
			return m.writeError(command.ID, commandType, err.Error())
		}
		return m.writeSuccess(command.ID, commandType, map[string]any{"sessions": sessions})
	case "new_session":
		if err := m.ensureIdle(); err != nil {
			return m.writeError(command.ID, commandType, err.Error())
		}
		if err := m.controller.NewSession(); err != nil {
			return m.writeError(command.ID, commandType, err.Error())
		}
		return m.writeSuccess(command.ID, commandType, m.sessionSwitchState())
	case "open_session":
		if err := m.ensureIdle(); err != nil {
			return m.writeError(command.ID, commandType, err.Error())
		}
		if strings.TrimSpace(command.SessionID) == "" {
			return m.writeError(command.ID, commandType, "session_id is required")
		}
		if err := m.controller.OpenSession(command.SessionID); err != nil {
			return m.writeError(command.ID, commandType, err.Error())
		}
		return m.writeSuccess(command.ID, commandType, m.sessionSwitchState())
	case "list_models":
		return m.writeSuccess(command.ID, commandType, map[string]any{"models": m.controller.AvailableModels()})
	case "set_model":
		if err := m.ensureIdle(); err != nil {
			return m.writeError(command.ID, commandType, err.Error())
		}
		if strings.TrimSpace(command.ModelID) == "" {
			return m.writeError(command.ID, commandType, "model_id is required")
		}
		if err := m.controller.SwitchModelForProvider(command.ProviderID, command.ModelID); err != nil {
			return m.writeError(command.ID, commandType, err.Error())
		}
		return m.writeSuccess(command.ID, commandType, m.state())
	case "prompt":
		message := strings.TrimSpace(command.Message)
		if message == "" && len(command.Attachments) == 0 {
			return m.writeError(command.ID, commandType, "message or attachments are required")
		}
		if !m.busy.CompareAndSwap(false, true) {
			return m.writeError(command.ID, commandType, "agent is busy")
		}
		attachments, err := buildAttachments(command.Attachments)
		if err != nil {
			m.busy.Store(false)
			return m.writeError(command.ID, commandType, err.Error())
		}
		m.promptWG.Add(1)
		go func() {
			defer m.promptWG.Done()
			defer m.busy.Store(false)
			_ = m.controller.SubmitMessage(ctx, message, attachments)
		}()
		return m.writeSuccess(command.ID, commandType, nil)
	case "abort":
		m.controller.CancelTurn()
		return m.writeSuccess(command.ID, commandType, nil)
	case "reload":
		if err := m.ensureIdle(); err != nil {
			return m.writeError(command.ID, commandType, err.Error())
		}
		if err := m.controller.Reload(ctx); err != nil {
			return m.writeError(command.ID, commandType, err.Error())
		}
		return m.writeSuccess(command.ID, commandType, m.state())
	case "compact":
		if !m.busy.CompareAndSwap(false, true) {
			return m.writeError(command.ID, commandType, "agent is busy")
		}
		m.runExclusive(func() {
			if err := m.controller.Compact(ctx, command.Instructions); err != nil {
				m.reportAsyncWrite(m.writeError(command.ID, commandType, err.Error()))
				return
			}
			m.reportAsyncWrite(m.writeSuccess(command.ID, commandType, m.state()))
		})
		return nil
	case "get_runtime_ui":
		runtimeSet := m.controller.RuntimeContributions()
		return m.writeSuccess(command.ID, commandType, RuntimeUIResponseData{
			Commands:    runtimeSet.UI.Commands(),
			Views:       runtimeSet.UI.Views(),
			Diagnostics: m.controller.RuntimeDiagnostics(),
		})
	case "render_view":
		if !m.busy.CompareAndSwap(false, true) {
			return m.writeError(command.ID, commandType, "agent is busy")
		}
		if strings.TrimSpace(command.ViewID) == "" {
			m.busy.Store(false)
			return m.writeError(command.ID, commandType, "view_id is required")
		}
		m.runExclusive(func() {
			view, result, err := m.controller.RenderRuntimeView(ctx, command.ViewID)
			if err != nil {
				m.reportAsyncWrite(m.writeError(command.ID, commandType, err.Error()))
				return
			}
			rendered := viewrender.Render(m.controller.Config().UI.Theme, m.controller.Workspace().Root, view, result)
			m.reportAsyncWrite(m.writeSuccess(command.ID, commandType, RenderViewResponseData{
				View:         view,
				Result:       result,
				RenderedText: rendered,
			}))
		})
		return nil
	case "ui_response":
		result := luruntime.UIResult{
			ActionID: command.ActionID,
			Accepted: command.Accepted,
			ChoiceID: command.ChoiceID,
			Data:     cloneMap(command.Data),
		}
		if err := m.broker.Respond(command.ActionID, result); err != nil {
			return m.writeError(command.ID, commandType, err.Error())
		}
		return m.writeSuccess(command.ID, commandType, nil)
	default:
		return m.writeError(command.ID, commandType, fmt.Sprintf("unknown command %q", commandType))
	}
}

func (m *Mode) ensureIdle() error {
	if m.busy.Load() || m.controller.TurnActive() {
		return errors.New("agent is busy")
	}
	return nil
}

func (m *Mode) state() StateResponseData {
	visibleEvents := m.controller.VisibleEvents()
	rawEvents := m.controller.SessionEvents()
	cfg := m.controller.Config()
	return StateResponseData{
		ProtocolVersion: ProtocolVersion,
		Workspace:       buildWorkspaceState(m.controller.Workspace()),
		Session: SessionState{
			Meta:              m.controller.Session(),
			Saved:             m.controller.SessionSaved(),
			VisibleEventCount: len(visibleEvents),
			RawEventCount:     len(rawEvents),
		},
		Provider: ProviderState{
			Kind:  cfg.Provider.Kind,
			Model: cfg.Provider.Model,
		},
		TurnActive:       m.busy.Load() || m.controller.TurnActive(),
		ApprovalsMode:    cfg.UI.ApprovalsMode,
		HostCapabilities: m.controller.HostCapabilities(),
	}
}

func (m *Mode) sessionSwitchState() SessionSwitchResponseData {
	return SessionSwitchResponseData{
		State:  m.state(),
		Events: m.controller.VisibleEvents(),
	}
}

func (m *Mode) getEvents(scope string, sinceSeq uint64) (EventsResponseData, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "visible"
	}

	var events []history.EventEnvelope
	switch scope {
	case "visible":
		events = m.controller.VisibleEvents()
	case "raw":
		events = m.controller.SessionEvents()
	default:
		return EventsResponseData{}, fmt.Errorf("unsupported scope %q", scope)
	}

	lastSeq := lastSeq(events)
	events = filterEvents(events, sinceSeq)

	return EventsResponseData{
		Scope:   scope,
		Events:  events,
		LastSeq: lastSeq,
	}, nil
}

func (m *Mode) writeSuccess(id, command string, data any) error {
	return m.writer.WriteJSONLine(Response{
		ID:      id,
		Type:    "response",
		Command: command,
		Success: true,
		Data:    data,
	})
}

func (m *Mode) writeError(id, command, message string) error {
	return m.writer.WriteJSONLine(Response{
		ID:      id,
		Type:    "response",
		Command: command,
		Success: false,
		Error:   message,
	})
}

func buildAttachments(inputs []AttachmentInput) ([]media.Attachment, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	attachments := make([]media.Attachment, 0, len(inputs))
	for i, input := range inputs {
		if strings.TrimSpace(input.Type) != "image" {
			return nil, fmt.Errorf("unsupported attachment type %q", input.Type)
		}
		id := fmt.Sprintf("rpc_image_%d_%d", time.Now().UnixNano(), i)
		attachment, err := media.BuildImageAttachment(id, input.Name, input.MediaType, input.Data)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	return attachments, nil
}

func (m *Mode) runExclusive(fn func()) {
	m.promptWG.Add(1)
	go func() {
		defer m.promptWG.Done()
		defer m.busy.Store(false)
		fn()
	}()
}

func (m *Mode) reportAsyncWrite(err error) {
	if err != nil && m.reportErr != nil {
		m.reportErr(err)
	}
}

func filterEvents(events []history.EventEnvelope, sinceSeq uint64) []history.EventEnvelope {
	if sinceSeq == 0 {
		return events
	}
	out := make([]history.EventEnvelope, 0, len(events))
	for _, event := range events {
		if event.Seq > sinceSeq {
			out = append(out, event)
		}
	}
	return out
}

func lastSeq(events []history.EventEnvelope) uint64 {
	var out uint64
	for _, event := range events {
		if event.Seq > out {
			out = event.Seq
		}
	}
	return out
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}
