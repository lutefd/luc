package execprovider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/provider"
	luruntime "github.com/lutefd/luc/internal/runtime"
)

type Spec struct {
	Name         string
	Command      string
	Args         []string
	Env          map[string]string
	Dir          string
	Capabilities []string
}

type Client struct {
	name             string
	spec             Spec
	broker           luruntime.UIBroker
	hostCapabilities []string
}

func New(cfg config.ProviderConfig, spec Spec) (*Client, error) {
	_ = cfg
	command := strings.TrimSpace(spec.Command)
	if command == "" {
		return nil, errors.New("exec provider command is required")
	}
	spec.Command = command
	spec.Args = append([]string(nil), spec.Args...)
	spec.Env = cloneEnv(spec.Env)
	spec.Dir = strings.TrimSpace(spec.Dir)
	spec.Capabilities = append([]string(nil), spec.Capabilities...)
	if spec.Dir == "" {
		spec.Dir = "."
	}
	return &Client{
		name: firstNonEmpty(spec.Name, "exec"),
		spec: spec,
	}, nil
}

func (c *Client) Name() string {
	return c.name
}

func (c *Client) SetRuntimeOptions(broker luruntime.UIBroker, hostCapabilities []string) {
	c.broker = broker
	c.hostCapabilities = append([]string(nil), hostCapabilities...)
}

func (c *Client) Start(ctx context.Context, req provider.Request) (provider.Stream, error) {
	commandPath := resolveCommand(c.spec.Command, c.spec.Dir)
	cmd := osexec.CommandContext(ctx, commandPath, c.spec.Args...)
	configureCommandProcess(cmd)
	cmd.Dir = c.spec.Dir
	cmd.Env = mergeEnv(os.Environ(), c.spec.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}

	if err := json.NewEncoder(stdin).Encode(execRequest{Request: req, HostCapabilities: append([]string(nil), c.hostCapabilities...)}); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = cmd.Wait()
		return nil, err
	}
	if !luruntime.HasCapability(c.spec.Capabilities, luruntime.CapabilityClientAction) {
		_ = stdin.Close()
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	return &stream{
		cmd:                cmd,
		stdout:             stdout,
		stdin:              stdin,
		scanner:            scanner,
		stderr:             &stderr,
		broker:             c.broker,
		allowClientActions: luruntime.HasCapability(c.spec.Capabilities, luruntime.CapabilityClientAction),
	}, nil
}

type stream struct {
	cmd     *osexec.Cmd
	stdout  io.Closer
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  *bytes.Buffer
	broker  luruntime.UIBroker

	allowClientActions bool

	once    sync.Once
	waitErr error
}

func (s *stream) Recv() (provider.Event, error) {
	for s.scanner.Scan() {
		line := strings.TrimSpace(s.scanner.Text())
		if line == "" {
			continue
		}

		var ev execEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			_ = s.Close()
			return provider.Event{}, err
		}
		if err := ev.validate(); err != nil {
			_ = s.Close()
			return provider.Event{}, err
		}
		if ev.Type == "client_action" {
			if !s.allowClientActions {
				_ = s.Close()
				return provider.Event{}, errors.New("exec provider emitted client_action without client_actions capability")
			}
			if ev.Action == nil {
				_ = s.Close()
				return provider.Event{}, errors.New("exec provider client_action is missing action payload")
			}
			if err := s.handleClientAction(*ev.Action); err != nil {
				_ = s.Close()
				return provider.Event{}, err
			}
			continue
		}
		if ev.Error != "" {
			_ = s.Close()
			return provider.Event{}, errors.New(ev.Error)
		}

		out := provider.Event{
			Type:      ev.Type,
			Text:      ev.Text,
			Usage:     ev.Usage,
			Completed: ev.Completed,
		}
		if ev.ToolCall != nil {
			out.ToolCall = *ev.ToolCall
		}
		return out, nil
	}

	if err := s.scanner.Err(); err != nil {
		_ = s.Close()
		return provider.Event{}, err
	}

	if err := s.wait(); err != nil {
		return provider.Event{}, err
	}
	return provider.Event{}, io.EOF
}

func (s *stream) Close() error {
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.stdout != nil {
		_ = s.stdout.Close()
	}
	_ = killCommandProcess(s.cmd)
	return s.wait()
}

func (s *stream) wait() error {
	s.once.Do(func() {
		if s.cmd == nil {
			return
		}
		err := s.cmd.Wait()
		if err == nil {
			return
		}
		stderr := strings.TrimSpace(s.stderr.String())
		if stderr != "" {
			s.waitErr = fmt.Errorf("%w: %s", err, stderr)
			return
		}
		s.waitErr = err
	})
	return s.waitErr
}

type execRequest struct {
	Request          provider.Request `json:"request"`
	HostCapabilities []string         `json:"host_capabilities,omitempty"`
}

type execEvent struct {
	Type      string              `json:"type"`
	Text      string              `json:"text,omitempty"`
	ToolCall  *provider.ToolCall  `json:"tool_call,omitempty"`
	Action    *luruntime.UIAction `json:"action,omitempty"`
	Usage     map[string]any      `json:"usage,omitempty"`
	Completed bool                `json:"completed,omitempty"`
	Error     string              `json:"error,omitempty"`
}

func (e execEvent) validate() error {
	switch strings.TrimSpace(e.Type) {
	case "thinking", "text_delta", "done":
		return nil
	case "tool_call":
		if e.ToolCall == nil {
			return errors.New("exec provider event tool_call is missing tool_call payload")
		}
		if strings.TrimSpace(e.ToolCall.Name) == "" {
			return errors.New("exec provider event tool_call is missing tool name")
		}
		if strings.TrimSpace(e.ToolCall.ID) == "" {
			return errors.New("exec provider event tool_call is missing tool id")
		}
		return nil
	case "client_action":
		if e.Action == nil {
			return errors.New("exec provider event client_action is missing action payload")
		}
		return nil
	default:
		if e.Error != "" && e.Type == "" {
			return nil
		}
		return fmt.Errorf("unsupported exec provider event type %q", e.Type)
	}
}

func (s *stream) handleClientAction(action luruntime.UIAction) error {
	if strings.TrimSpace(action.ID) == "" {
		action.ID = "provider_action"
	}
	broker := s.broker
	if broker == nil {
		broker = luruntime.NewDefaultBroker("trusted", nil)
	}
	var (
		result luruntime.UIResult
		err    error
	)
	if action.Blocking {
		result, err = broker.Request(context.Background(), action)
	} else {
		err = broker.Publish(action)
		result = luruntime.UIResult{ActionID: action.ID, Accepted: err == nil}
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(s.stdin).Encode(luruntime.ClientResultEnvelope{
		Type:   "client_result",
		Result: result,
	})
}

func resolveCommand(command, dir string) string {
	if filepath.IsAbs(command) {
		return command
	}
	if strings.Contains(command, string(filepath.Separator)) {
		return filepath.Join(dir, command)
	}
	return command
}

func mergeEnv(base []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return base
	}
	merged := make(map[string]string, len(base)+len(extra))
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	out := make([]string, 0, len(merged))
	for key, value := range merged {
		out = append(out, key+"="+value)
	}
	return out
}

func cloneEnv(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var _ provider.Provider = (*Client)(nil)
