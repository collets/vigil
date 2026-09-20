//go:build linux || darwin

package harness

import (
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func signalGroup(cmd *exec.Cmd, kill bool) {
	if cmd.Process != nil {
		signal := syscall.SIGTERM
		if kill {
			signal = syscall.SIGKILL
		}
		_ = syscall.Kill(-cmd.Process.Pid, signal)
	}
}
