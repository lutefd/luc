package kernel

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/media"
	"github.com/lutefd/luc/internal/provider"
	luruntime "github.com/lutefd/luc/internal/runtime"
)

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
	c.shutdownExtensionHosts(context.Background(), "session_switch")

	if err := c.configureSessionProvider(meta); err != nil {
		return err
	}

	if prev := strings.TrimSpace(c.session.SessionID); prev != "" && prev != meta.SessionID {
		_ = c.store.CloseSession(prev)
	}
	c.session = meta
	c.sessionSaved = len(events) > 0
	c.seq.Store(0)
	c.initial = append([]history.EventEnvelope(nil), events...)
	c.mu.Lock()
	c.rawEvents = append([]history.EventEnvelope(nil), events...)
	c.eventLog = append([]history.EventEnvelope(nil), events...)
	c.mu.Unlock()
	for _, ev := range events {
		if ev.Seq > c.seq.Load() {
			c.seq.Store(ev.Seq)
		}
	}
	c.rebuildReplayState()
	c.restartExtensionHosts(context.Background(), luruntime.ExtensionEventSessionStart)

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
	configureRuntimeProvider(c.provider, c.recordingUIBroker(), c.HostCapabilities())
	return nil
}

func (c *Controller) replay(ev history.EventEnvelope) {
	switch ev.Kind {
	case "message.user":
		payload := decode[history.MessagePayload](ev.Payload)
		msg := provider.Message{Role: "user"}
		attachments := media.FromHistoryPayloads(payload.Attachments)
		if parts := media.MessageParts(payload.Content, attachments); len(parts) > 0 {
			msg.Parts = parts
		} else {
			msg.Content = payload.Content
		}
		c.conversation = append(c.conversation, msg)
	case "message.assistant.final":
		payload := decode[history.MessagePayload](ev.Payload)
		if payload.Synthetic || isNoResponsePlaceholder(payload.Content) {
			return
		}
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

	out := make([]provider.Message, 0, len(c.conversation))
	for _, msg := range c.conversation {
		if msg.Role == "assistant" && isNoResponsePlaceholder(msg.Content) {
			continue
		}
		out = append(out, msg)
	}
	return out
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
