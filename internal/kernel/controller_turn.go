package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/media"
	"github.com/lutefd/luc/internal/provider"
	"github.com/lutefd/luc/internal/tools"
)

const (
	noResponseText       = "No response."
	noUsableResponseText = "Provider returned no usable response."
	autoContinueText     = "continue"
	maxToolLoopRounds    = 8
	maxAutoContinues     = 4
)

var errExceededToolLoopLimit = errors.New("exceeded tool loop limit")

func (c *Controller) Submit(ctx context.Context, input string) error {
	return c.SubmitMessage(ctx, input, nil)
}

func (c *Controller) SubmitMessage(ctx context.Context, input string, attachments []media.Attachment) error {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()

	text := strings.TrimSpace(input)
	if text == "" && len(attachments) == 0 {
		return nil
	}

	if strings.HasPrefix(text, "/") {
		return c.handleCommand(ctx, text)
	}

	transformedText, handled, handledMessage, err := c.applyInputTransforms(ctx, text, attachments)
	if err != nil {
		c.emit("system.error", history.MessagePayload{ID: nextID("error"), Content: err.Error()})
		return err
	}
	if handled {
		if message := strings.TrimSpace(handledMessage); message != "" {
			c.emit("system.note", history.MessagePayload{ID: nextID("note"), Content: message})
		}
		return nil
	}
	text = transformedText

	if c.provider == nil {
		err := errors.New("provider is not ready; check API key configuration")
		c.emit("system.error", history.MessagePayload{ID: nextID("error"), Content: err.Error()})
		return err
	}

	userID := nextID("user")
	c.emit("message.user", history.MessagePayload{
		ID:          userID,
		Content:     text,
		Attachments: media.ToHistoryPayloads(attachments),
	})
	userMessage := provider.Message{Role: "user"}
	if parts := media.MessageParts(text, attachments); len(parts) > 0 {
		userMessage.Parts = parts
	} else {
		userMessage.Content = text
	}
	c.appendMessage(userMessage)
	if text != "" {
		c.updateTitle(text)
	}

	turnCtx, cancel := context.WithCancel(ctx)
	c.beginTurn(cancel)
	defer c.endTurn()

	autoContinues := 0

	for {
	toolLoop:
		for range maxToolLoopRounds {
			systemPrompt, err := c.composeTurnSystemPrompt(turnCtx, text, attachments)
			if err != nil {
				c.emit("system.error", history.MessagePayload{ID: nextID("error"), Content: err.Error()})
				return err
			}
			stream, err := c.provider.Start(turnCtx, provider.Request{
				Model:       c.config.Provider.Model,
				System:      systemPrompt,
				Messages:    c.snapshotConversation(),
				Tools:       c.toolSpecs(),
				Temperature: c.config.Provider.Temperature,
				MaxTokens:   c.config.Provider.MaxTokens,
			})
			if err != nil {
				if turnCtx.Err() != nil {
					return c.handleTurnCanceled()
				}
				c.emit("system.error", history.MessagePayload{ID: nextID("error"), Content: err.Error()})
				return err
			}

			assistantID := nextID("assistant")
			var builder strings.Builder
			var calls []provider.ToolCall

			for {
				ev, err := stream.Recv()
				if turnCtx.Err() != nil {
					_ = stream.Close()
					return c.handleTurnCanceled()
				}
				if errors.Is(err, context.Canceled) {
					_ = stream.Close()
					return c.handleTurnCanceled()
				}
				if errors.Is(err, context.DeadlineExceeded) {
					_ = stream.Close()
					return err
				}
				if errors.Is(err, os.ErrClosed) || errors.Is(err, context.Canceled) {
					_ = stream.Close()
					return c.handleTurnCanceled()
				}
				if err != nil {
					if errors.Is(err, os.ErrClosed) || errors.Is(err, context.Canceled) {
						_ = stream.Close()
						return c.handleTurnCanceled()
					}
					if errors.Is(err, io.EOF) {
						break
					}
					_ = stream.Close()
					if errors.Is(err, context.Canceled) {
						return c.handleTurnCanceled()
					}
					if shouldAutoContinueToolLimit(err) {
						break toolLoop
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

				completedCalls := make([]provider.ToolCall, 0, len(calls))
				completedResults := make([]provider.Message, 0, len(calls))
				appendCompletedToolMessages := func() {
					if len(completedCalls) == 0 {
						return
					}
					if len(completedCalls) == len(calls) {
						c.appendMessage(assistantMsg)
					} else {
						c.appendMessage(provider.Message{Role: "assistant", ToolCalls: completedCalls})
					}
					for _, msg := range completedResults {
						c.appendMessage(msg)
					}
				}

				for i, call := range calls {
					preparedCall, preflightErr := c.prepareToolCall(turnCtx, call)
					c.emit("tool.requested", history.ToolCallPayload{
						ID:        preparedCall.ID,
						Name:      preparedCall.Name,
						Arguments: preparedCall.Arguments,
					})
					var (
						result tools.Result
						err    error
					)
					if preflightErr != nil {
						err = preflightErr
					} else {
						result, err = c.runPreparedToolCall(turnCtx, preparedCall)
					}
					if turnCtx.Err() != nil || errors.Is(err, context.Canceled) {
						if len(completedCalls) > 0 || i == 0 {
							c.appendInterruptedToolMessages(calls, completedCalls, completedResults, i)
						}
						return c.handleTurnCanceled()
					}
					payload := history.ToolResultPayload{
						ID:       preparedCall.ID,
						Name:     preparedCall.Name,
						Content:  result.Content,
						Metadata: result.Metadata,
					}
					if err != nil {
						payload.Error = err.Error()
					}
					if preflightErr == nil {
						payload = c.applyToolResultPatches(turnCtx, preparedCall, payload)
					}
					c.emit("tool.finished", payload)
					completedCalls = append(completedCalls, preparedCall)
					completedResults = append(completedResults, provider.Message{
						Role:       "tool",
						ToolCallID: preparedCall.ID,
						Name:       preparedCall.Name,
						Content:    toolResponseContent(payload),
					})
				}
				appendCompletedToolMessages()
				continue
			}

			final := strings.TrimSpace(builder.String())
			if final == "" || isNoResponsePlaceholder(final) {
				c.emit("message.assistant.final", history.MessagePayload{
					ID:        assistantID,
					Content:   noUsableResponseText,
					Synthetic: true,
				})
				errMsg := "provider returned an empty response"
				if isNoResponsePlaceholder(final) {
					errMsg = fmt.Sprintf("provider returned placeholder response %q", noResponseText)
				}
				c.emit("system.error", history.MessagePayload{ID: nextID("error"), Content: errMsg})
				return nil
			}
			c.emit("message.assistant.final", history.MessagePayload{ID: assistantID, Content: final})
			c.appendMessage(provider.Message{Role: "assistant", Content: final})
			if err := c.maybeAutoCompact(turnCtx); err != nil {
				c.emit("system.error", history.MessagePayload{
					ID:      nextID("error"),
					Content: "auto-compaction failed: " + err.Error(),
				})
			}
			return nil
		}

		if autoContinues >= maxAutoContinues {
			return errExceededToolLoopLimit
		}
		c.appendSyntheticUserMessage(autoContinueText)
		autoContinues++
	}
}

func (c *Controller) beginTurn(cancel context.CancelFunc) {
	c.mu.Lock()
	c.turnCancel = cancel
	c.mu.Unlock()
	c.turnActive.Store(true)
}

func (c *Controller) endTurn() {
	c.turnActive.Store(false)
	c.mu.Lock()
	c.turnCancel = nil
	c.mu.Unlock()
}

func (c *Controller) appendInterruptedToolMessages(calls []provider.ToolCall, completedCalls []provider.ToolCall, completedResults []provider.Message, current int) {
	if len(calls) == 0 || current < 0 || current >= len(calls) {
		return
	}
	toAppend := append([]provider.ToolCall(nil), completedCalls...)
	toAppend = append(toAppend, calls[current])
	c.appendMessage(provider.Message{Role: "assistant", ToolCalls: toAppend})
	for _, msg := range completedResults {
		c.appendMessage(msg)
	}
	call := calls[current]
	payload := history.ToolResultPayload{
		ID:      call.ID,
		Name:    call.Name,
		Content: "Tool execution was interrupted before luc recorded a result.",
		Error:   "tool execution interrupted",
	}
	c.emit("tool.finished", payload)
	c.appendMessage(provider.Message{
		Role:       "tool",
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    toolResponseContent(payload),
	})
}

func (c *Controller) handleTurnCanceled() error {
	c.emit("system.note", history.MessagePayload{
		ID:      nextID("stop"),
		Content: "Stopped current turn.",
	})
	return nil
}

func shouldAutoContinueToolLimit(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, provider.ErrExceededToolLimits) || errors.Is(err, errExceededToolLoopLimit) {
		return true
	}
	return provider.IsToolLimitReason(err.Error())
}

func (c *Controller) appendSyntheticUserMessage(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	c.emit("message.user", history.MessagePayload{
		ID:        nextID("user"),
		Content:   text,
		Synthetic: true,
	})
	c.appendMessage(provider.Message{Role: "user", Content: text})
}

func (c *Controller) handleCommand(ctx context.Context, text string) error {
	trimmed := strings.TrimSpace(text)
	switch {
	case trimmed == "/reload":
		c.emit("reload.started", history.ReloadPayload{Version: c.version.Load()})
		return c.Reload(ctx)
	case trimmed == "/help":
		c.emit("system.note", history.MessagePayload{
			ID:      nextID("help"),
			Content: "Commands: /reload, /help, /compact [instructions]",
		})
		return nil
	case strings.HasPrefix(trimmed, "/compact"):
		instructions := strings.TrimSpace(strings.TrimPrefix(trimmed, "/compact"))
		_, err := c.runCompaction(ctx, instructions, "manual", estimateRequestTokens(c.mustComposeSystemPrompt(), c.snapshotConversation()))
		if err != nil {
			c.emit("system.error", history.MessagePayload{
				ID:      nextID("error"),
				Content: "compaction failed: " + err.Error(),
			})
			return err
		}
		return nil
	default:
		c.emit("system.error", history.MessagePayload{
			ID:      nextID("error"),
			Content: fmt.Sprintf("unknown command: %s", text),
		})
		return nil
	}
}
