package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/lutefd/luc/internal/config"
	"github.com/lutefd/luc/internal/extensions"
	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/media"
	"github.com/lutefd/luc/internal/provider"
)

const (
	defaultContextWindowTokens = 128000
	toolResultMaxChars         = 2000
)

const compactionSystemPrompt = `You are a context summarization assistant. Read the serialized luc session below and produce a structured checkpoint summary.

Do not continue the conversation. Do not answer any user request from the transcript. Only return the summary in the requested format.`

const compactionPrompt = `Create a structured context checkpoint summary that luc can inject into future prompts.

Use this exact format:

## Goal
[What the user is trying to accomplish]

## Constraints & Preferences
- [Requirement or "(none)"]

## Progress
### Done
- [x] [Completed work]

### In Progress
- [ ] [Current work]

### Blocked
- [Issue or "(none)"]

## Key Decisions
- **[Decision]**: [Rationale]

## Next Steps
1. [Next action]

## Critical Context
- [Facts, references, paths, or "(none)"]

Keep the summary concise. Preserve exact file paths, function names, error messages, and commands when they matter.`

const compactionUpdatePrompt = `Update the existing summary using the new serialized luc session events below.

Rules:
- Preserve important information from the existing summary.
- Move completed work from "In Progress" to "Done".
- Keep exact file paths, function names, error messages, and commands.
- Remove items that are no longer relevant only when they have clearly been superseded.

Use the exact same format as the existing summary.`

type compactionPlan struct {
	firstKeptSeq    uint64
	tokensBefore    int
	previousSummary string
	conversation    string
	details         history.CompactionDetails
}

func (c *Controller) maybeAutoCompact(ctx context.Context) error {
	settings := normalizeCompactionSettings(c.config.Compaction)
	if !settings.Enabled {
		return nil
	}
	systemPrompt, err := c.composeSystemPrompt("")
	if err != nil {
		return err
	}
	contextTokens := estimateRequestTokens(systemPrompt, c.snapshotConversation())
	if contextTokens <= c.currentContextWindow()-settings.ReserveTokens {
		return nil
	}
	_, err = c.runCompaction(ctx, "", "auto", contextTokens)
	return err
}

func (c *Controller) runCompaction(ctx context.Context, instructions, reason string, tokensBefore int) (bool, error) {
	if c.provider == nil {
		return false, errors.New("provider is not ready; check API key configuration")
	}

	settings := normalizeCompactionSettings(c.config.Compaction)
	plan, ok := c.prepareCompactionPlan(tokensBefore, settings)
	if !ok {
		c.emit("system.note", history.MessagePayload{
			ID:      nextID("note"),
			Content: "Nothing to compact.",
		})
		return false, nil
	}

	c.emit("status.thinking", history.StatusPayload{Text: "Compacting context..."})
	summary, err := c.generateCompactionSummary(ctx, plan, instructions)
	if err != nil {
		return false, err
	}

	c.emit("session.compaction", history.CompactionPayload{
		Summary:      summary,
		FirstKeptSeq: plan.firstKeptSeq,
		TokensBefore: plan.tokensBefore,
		Reason:       strings.TrimSpace(reason),
		Details:      plan.details,
	})
	c.rebuildReplayState()
	return true, nil
}

func (c *Controller) prepareCompactionPlan(tokensBefore int, settings normalizedCompactionSettings) (compactionPlan, bool) {
	raw := c.rawSessionEvents()
	active := history.ActiveEvents(raw)
	if len(active) == 0 {
		return compactionPlan{}, false
	}

	firstKeptIndex := findCompactionCutIndex(active, settings.KeepRecentTokens)
	if firstKeptIndex <= 0 || firstKeptIndex >= len(active) {
		return compactionPlan{}, false
	}

	conversation := serializeCompactionEvents(active[:firstKeptIndex])
	if strings.TrimSpace(conversation) == "" {
		return compactionPlan{}, false
	}

	var previousSummary string
	var previousDetails history.CompactionDetails
	if state, ok := history.LatestCompaction(raw); ok {
		previousSummary = strings.TrimSpace(state.Payload.Summary)
		previousDetails = state.Payload.Details
	}

	return compactionPlan{
		firstKeptSeq:    active[firstKeptIndex].Seq,
		tokensBefore:    tokensBefore,
		previousSummary: previousSummary,
		conversation:    conversation,
		details:         collectCompactionDetails(previousDetails, active[:firstKeptIndex]),
	}, true
}

func (c *Controller) generateCompactionSummary(ctx context.Context, plan compactionPlan, instructions string) (string, error) {
	var prompt strings.Builder
	if prev := strings.TrimSpace(plan.previousSummary); prev != "" {
		prompt.WriteString(compactionUpdatePrompt)
		prompt.WriteString("\n\n<previous-summary>\n")
		prompt.WriteString(prev)
		prompt.WriteString("\n</previous-summary>")
	} else {
		prompt.WriteString(compactionPrompt)
	}
	if extra := strings.TrimSpace(instructions); extra != "" {
		prompt.WriteString("\n\nFocus instructions:\n")
		prompt.WriteString(extra)
	}
	prompt.WriteString("\n\n<conversation>\n")
	prompt.WriteString(plan.conversation)
	prompt.WriteString("\n</conversation>")

	text, err := c.completeText(ctx, compactionSystemPrompt, prompt.String())
	if err != nil {
		return "", err
	}
	return rewriteCompactionFileSections(text, plan.details), nil
}

func (c *Controller) completeText(ctx context.Context, systemPrompt, prompt string) (string, error) {
	stream, err := c.provider.Start(ctx, provider.Request{
		Model:       c.config.Provider.Model,
		System:      systemPrompt,
		Messages:    []provider.Message{{Role: "user", Content: prompt}},
		Temperature: c.config.Provider.Temperature,
		MaxTokens:   c.config.Provider.MaxTokens,
	})
	if err != nil {
		return "", err
	}
	defer stream.Close()

	var builder strings.Builder
	for {
		ev, err := stream.Recv()
		switch {
		case err == nil:
		case errors.Is(err, context.Canceled):
			return "", err
		case errors.Is(err, io.EOF):
			return strings.TrimSpace(builder.String()), nil
		default:
			return "", err
		}

		switch ev.Type {
		case "thinking":
			continue
		case "text_delta":
			builder.WriteString(ev.Text)
		case "tool_call":
			return "", fmt.Errorf("provider requested tool %s during compaction", ev.ToolCall.Name)
		case "done":
			return strings.TrimSpace(builder.String()), nil
		}
	}
}

func (c *Controller) rebuildReplayState() {
	raw := c.rawSessionEvents()
	active := history.ActiveEvents(raw)
	loaded := loadedSkillsFromEvents(raw)

	var summary string
	if state, ok := history.LatestCompaction(raw); ok {
		summary = strings.TrimSpace(state.Payload.Summary)
	}

	conversation := replayConversation(active)

	c.mu.Lock()
	c.conversation = conversation
	c.loadedSkills = loaded
	c.compactionSummary = summary
	c.mu.Unlock()
}

func (c *Controller) mustComposeSystemPrompt() string {
	prompt, err := c.composeSystemPrompt("")
	if err != nil {
		return c.systemPrompt
	}
	return prompt
}

func (c *Controller) rawSessionEvents() []history.EventEnvelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]history.EventEnvelope, len(c.rawEvents))
	copy(out, c.rawEvents)
	return out
}

func (c *Controller) currentContextWindow() int {
	model, _, ok := c.Registry().FindModel(c.config.Provider.Model)
	if ok && model.ContextK > 0 {
		return model.ContextK * 1000
	}
	return defaultContextWindowTokens
}

type normalizedCompactionSettings struct {
	Enabled          bool
	ReserveTokens    int
	KeepRecentTokens int
}

func normalizeCompactionSettings(cfg config.CompactionConfig) normalizedCompactionSettings {
	settings := normalizedCompactionSettings{
		Enabled:          cfg.Enabled,
		ReserveTokens:    cfg.ReserveTokens,
		KeepRecentTokens: cfg.KeepRecentTokens,
	}
	if settings.ReserveTokens <= 0 {
		settings.ReserveTokens = 16384
	}
	if settings.KeepRecentTokens <= 0 {
		settings.KeepRecentTokens = 20000
	}
	return settings
}

func loadedSkillsFromEvents(events []history.EventEnvelope) map[string]struct{} {
	out := make(map[string]struct{})
	for _, ev := range events {
		if strings.TrimSpace(ev.Kind) != "tool.finished" {
			continue
		}
		payload := decode[history.ToolResultPayload](ev.Payload)
		if strings.TrimSpace(payload.Name) != skillToolName {
			continue
		}
		if skillName, _ := payload.Metadata["skill_name"].(string); strings.TrimSpace(skillName) != "" {
			out[strings.ToLower(skillName)] = struct{}{}
		}
	}
	return out
}

func replayConversation(events []history.EventEnvelope) []provider.Message {
	out := make([]provider.Message, 0, len(events))
	for _, ev := range events {
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
			out = append(out, msg)
		case "message.assistant.final":
			payload := decode[history.MessagePayload](ev.Payload)
			if payload.Synthetic || isNoResponsePlaceholder(payload.Content) {
				continue
			}
			out = append(out, provider.Message{Role: "assistant", Content: payload.Content})
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
			out = append(out, msg)
		case "tool.finished":
			payload := decode[history.ToolResultPayload](ev.Payload)
			out = append(out, provider.Message{
				Role:       "tool",
				ToolCallID: payload.ID,
				Name:       payload.Name,
				Content:    toolResponseContent(payload),
			})
		}
	}
	return out
}

func findCompactionCutIndex(events []history.EventEnvelope, keepRecentTokens int) int {
	if keepRecentTokens <= 0 || len(events) == 0 {
		return 0
	}

	validCuts := make([]int, 0, len(events))
	for i, ev := range events {
		if isCompactionBoundary(ev.Kind) {
			validCuts = append(validCuts, i)
		}
	}
	if len(validCuts) == 0 {
		return 0
	}

	accumulated := 0
	cut := -1
	for i := len(events) - 1; i >= 0; i-- {
		accumulated += estimateCompactionEventTokens(events[i])
		if accumulated < keepRecentTokens {
			continue
		}
		for _, idx := range validCuts {
			if idx >= i {
				cut = idx
				break
			}
		}
		break
	}
	return cut
}

func isCompactionBoundary(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "message.user", "message.assistant.final", "message.assistant.tool_calls":
		return true
	default:
		return false
	}
}

func estimateRequestTokens(systemPrompt string, messages []provider.Message) int {
	tokens := estimateCharsTokens(systemPrompt)
	for _, msg := range messages {
		tokens += estimateMessageTokens(msg)
	}
	return tokens
}

func estimateMessageTokens(msg provider.Message) int {
	chars := 0
	switch {
	case len(msg.ToolCalls) > 0:
		for _, call := range msg.ToolCalls {
			chars += len(call.Name) + len(call.Arguments) + len(call.ID)
		}
	case msg.Role == "tool":
		chars += len(msg.Content) + len(msg.Name)
	default:
		for _, part := range msg.ContentParts() {
			switch strings.TrimSpace(part.Type) {
			case "", "text":
				chars += len(part.Text)
			case "image":
				chars += 4800
			}
		}
		chars += len(msg.Name)
	}
	return estimateCharsTokensCount(chars)
}

func estimateCompactionEventTokens(ev history.EventEnvelope) int {
	switch strings.TrimSpace(ev.Kind) {
	case "message.user":
		payload := decode[history.MessagePayload](ev.Payload)
		chars := len(payload.Content)
		for _, attachment := range payload.Attachments {
			if strings.TrimSpace(attachment.Type) == "image" {
				chars += 4800
			}
		}
		return estimateCharsTokensCount(chars)
	case "message.assistant.final":
		payload := decode[history.MessagePayload](ev.Payload)
		return estimateCharsTokens(payload.Content)
	case "message.assistant.tool_calls":
		payload := decode[history.ToolCallBatchPayload](ev.Payload)
		chars := 0
		for _, call := range payload.Calls {
			chars += len(call.ID) + len(call.Name) + len(call.Arguments)
		}
		return estimateCharsTokensCount(chars)
	case "tool.finished":
		payload := decode[history.ToolResultPayload](ev.Payload)
		return estimateCharsTokens(payload.Content + payload.Error + payload.Name)
	default:
		return 0
	}
}

func estimateCharsTokens(text string) int {
	return estimateCharsTokensCount(len(text))
}

func estimateCharsTokensCount(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 3) / 4
}

func serializeCompactionEvents(events []history.EventEnvelope) string {
	parts := make([]string, 0, len(events))
	for _, ev := range events {
		switch strings.TrimSpace(ev.Kind) {
		case "message.user":
			payload := decode[history.MessagePayload](ev.Payload)
			if payload.Synthetic {
				continue
			}
			text := strings.TrimSpace(payload.Content)
			if summary := media.AttachmentsSummary(media.FromHistoryPayloads(payload.Attachments)); summary != "" {
				if text == "" {
					text = "[" + summary + "]"
				} else {
					text += "\n[" + summary + "]"
				}
			}
			if text != "" {
				parts = append(parts, "[User]: "+text)
			}
		case "message.assistant.final":
			payload := decode[history.MessagePayload](ev.Payload)
			if payload.Synthetic || isNoResponsePlaceholder(payload.Content) {
				continue
			}
			if text := strings.TrimSpace(payload.Content); text != "" {
				parts = append(parts, "[Assistant]: "+text)
			}
		case "message.assistant.tool_calls":
			payload := decode[history.ToolCallBatchPayload](ev.Payload)
			calls := make([]string, 0, len(payload.Calls))
			for _, call := range payload.Calls {
				calls = append(calls, fmt.Sprintf("%s(%s)", call.Name, compactJSONString(call.Arguments)))
			}
			if len(calls) > 0 {
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(calls, "; "))
			}
		case "tool.finished":
			payload := decode[history.ToolResultPayload](ev.Payload)
			content := truncateForCompaction(toolResponseContent(payload), toolResultMaxChars)
			if strings.TrimSpace(content) != "" {
				parts = append(parts, "[Tool result]: "+content)
			}
		case "system.note":
			payload := decode[history.MessagePayload](ev.Payload)
			if text := strings.TrimSpace(payload.Content); text != "" {
				parts = append(parts, "[System note]: "+text)
			}
		case "system.error":
			payload := decode[history.MessagePayload](ev.Payload)
			if text := strings.TrimSpace(payload.Content); text != "" {
				parts = append(parts, "[System error]: "+text)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func truncateForCompaction(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	remaining := len(text) - maxChars
	return strings.TrimSpace(text[:maxChars]) + fmt.Sprintf("\n\n[... %d more characters truncated]", remaining)
}

func compactJSONString(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return raw
	}
	data, err := json.Marshal(decoded)
	if err != nil {
		return raw
	}
	return string(data)
}

func collectCompactionDetails(previous history.CompactionDetails, events []history.EventEnvelope) history.CompactionDetails {
	readFiles := make(map[string]struct{})
	modifiedFiles := make(map[string]struct{})

	for _, path := range previous.ReadFiles {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			readFiles[trimmed] = struct{}{}
		}
	}
	for _, path := range previous.ModifiedFiles {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			modifiedFiles[trimmed] = struct{}{}
		}
	}

	for _, ev := range events {
		if strings.TrimSpace(ev.Kind) != "tool.finished" {
			continue
		}
		payload := decode[history.ToolResultPayload](ev.Payload)
		path, _ := payload.Metadata["path"].(string)
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		switch strings.TrimSpace(payload.Name) {
		case "read":
			if _, modified := modifiedFiles[path]; !modified {
				readFiles[path] = struct{}{}
			}
		case "write", "edit":
			modifiedFiles[path] = struct{}{}
			delete(readFiles, path)
		}
	}

	return history.CompactionDetails{
		ReadFiles:     sortedSet(readFiles),
		ModifiedFiles: sortedSet(modifiedFiles),
	}
}

func rewriteCompactionFileSections(summary string, details history.CompactionDetails) string {
	summary = strings.TrimSpace(stripCompactionTag(stripCompactionTag(summary, "read-files"), "modified-files"))
	sections := []string{}
	if len(details.ReadFiles) > 0 {
		sections = append(sections, "<read-files>\n"+strings.Join(details.ReadFiles, "\n")+"\n</read-files>")
	}
	if len(details.ModifiedFiles) > 0 {
		sections = append(sections, "<modified-files>\n"+strings.Join(details.ModifiedFiles, "\n")+"\n</modified-files>")
	}
	if len(sections) == 0 {
		return summary
	}
	if summary == "" {
		return strings.Join(sections, "\n\n")
	}
	return summary + "\n\n" + strings.Join(sections, "\n\n")
}

func stripCompactionTag(text, tag string) string {
	startTag := "<" + tag + ">"
	endTag := "</" + tag + ">"
	for {
		start := strings.Index(text, startTag)
		if start < 0 {
			return strings.TrimSpace(text)
		}
		end := strings.Index(text[start:], endTag)
		if end < 0 {
			return strings.TrimSpace(text[:start])
		}
		end += start + len(endTag)
		text = strings.TrimSpace(text[:start] + "\n\n" + text[end:])
	}
}

func sortedSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func (c *Controller) loadedSkillPromptBlock() string {
	if strings.TrimSpace(c.compactionSummary) == "" || len(c.loadedSkills) == 0 {
		return ""
	}
	names := make([]string, 0, len(c.loadedSkills))
	for name := range c.loadedSkills {
		names = append(names, name)
	}
	sort.Strings(names)

	var sections []string
	for _, name := range names {
		skill, ok := c.skillByName(name)
		if !ok {
			continue
		}
		prompt, err := extensions.ResolveSkillPrompt(skill)
		if err != nil {
			continue
		}
		label := strings.TrimSpace(skill.DisplayName)
		if label == "" {
			label = skill.Name
		}
		var block strings.Builder
		block.WriteString("## ")
		block.WriteString(label)
		if desc := strings.TrimSpace(skill.Description); desc != "" {
			block.WriteString("\n")
			block.WriteString(desc)
		}
		block.WriteString("\n")
		block.WriteString(strings.TrimSpace(prompt))
		sections = append(sections, strings.TrimSpace(block.String()))
	}
	if len(sections) == 0 {
		return ""
	}
	return "Previously loaded skills still in force for this session:\n\n" + strings.Join(sections, "\n\n")
}
