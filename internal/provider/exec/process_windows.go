//go:build windows

package execprovider

import osexec "os/exec"

func configureCommandProcess(cmd *osexec.Cmd) {
	_ = cmd
}

func killCommandProcess(cmd *osexec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
