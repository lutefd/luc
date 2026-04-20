package kernel

import (
	"context"
	"errors"
	"time"

	"github.com/lutefd/luc/internal/extensions"
	luruntime "github.com/lutefd/luc/internal/runtime"
	"github.com/lutefd/luc/internal/tools"
)

func (c *Controller) InvokeHostedTool(ctx context.Context, def extensions.ToolDef, req tools.Request) (tools.Result, error) {
	if c.extensionHosts == nil {
		return tools.Result{}, errors.New("extension hosts are not available")
	}

	timeout := 30 * time.Second
	if def.TimeoutSeconds > 0 {
		timeout = time.Duration(def.TimeoutSeconds) * time.Second
	}

	return c.extensionHosts.InvokeHostedTool(ctx, def.ExtensionID, def.Handler, luruntime.ToolRequestEnvelope{
		ToolName:         req.Name,
		Arguments:        parseToolArgsOrEmpty(req.Arguments),
		Workspace:        req.Workspace,
		SessionID:        req.SessionID,
		AgentID:          req.AgentID,
		HostCapabilities: append([]string(nil), req.HostCapabilities...),
		ViewContext:      req.ViewContext,
	}, timeout)
}
