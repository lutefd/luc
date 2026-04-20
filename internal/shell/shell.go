package shell

import (
	"context"
	"os/exec"
	"runtime"
)

// Command returns an exec.Cmd that runs script through the platform shell.
// On Windows this is cmd /C; on all other platforms it is sh -c.
func Command(ctx context.Context, script string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", script)
	}
	return exec.CommandContext(ctx, "sh", "-c", script)
}
