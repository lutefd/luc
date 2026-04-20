package kernel

import (
	"fmt"
	"strings"
	"time"

	"github.com/lutefd/luc/internal/history"
)

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
