//go:build !windows

package agentsession

import (
	"os/exec"
	"syscall"
)

// prepareCmd makes the child a session leader with the PTY as its
// controlling terminal, so job control works and a kill can target the
// whole process group (-pid).
func prepareCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
}
