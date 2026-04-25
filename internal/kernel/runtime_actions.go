package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lutefd/luc/internal/history"
	"github.com/lutefd/luc/internal/provider"
	"github.com/lutefd/luc/internal/tools"
)

// RunRuntimeToolAction executes a declarative runtime tool.run action through
// the same host pipeline as model-requested tool calls: extension preflight,
// approval policy checks, tool execution, result patching, events, and hooks.
func (c *Controller) RunRuntimeToolAction(ctx context.Context, toolName string, arguments map[string]any) (tools.Result, error) {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return tools.Result{}, fmt.Errorf("tool.run requires tool_name")
	}
	rawArguments, err := json.Marshal(arguments)
	if err != nil {
		return tools.Result{}, fmt.Errorf("marshal tool.run arguments: %w", err)
	}
	if len(rawArguments) == 0 || string(rawArguments) == "null" {
		rawArguments = []byte(`{}`)
	}

	call := provider.ToolCall{
		ID:        nextID("runtime_tool"),
		Name:      toolName,
		Arguments: string(rawArguments),
	}
	preparedCall, preflightErr := c.prepareToolCall(ctx, call)
	c.emit("tool.requested", history.ToolCallPayload{
		ID:        preparedCall.ID,
		Name:      preparedCall.Name,
		Arguments: preparedCall.Arguments,
	})

	var result tools.Result
	if preflightErr != nil {
		err = preflightErr
	} else {
		result, err = c.runPreparedToolCall(ctx, preparedCall)
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
		payload = c.applyToolResultPatches(ctx, preparedCall, payload)
	}
	c.emit("tool.finished", payload)
	result.Content = payload.Content
	result.Metadata = payload.Metadata
	return result, err
}
