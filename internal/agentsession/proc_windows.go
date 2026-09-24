//go:build windows

package agentsession

import (
	"os/exec"
	"syscall"
)

// prepareCmd hands a raw command line through verbatim when the caller built
// one. ConPTY attaches the console itself, and the job object
// (kill_windows.go) groups the process tree.
func prepareCmd(cmd *exec.Cmd, cmdline string) {
	if cmdline != "" {
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdline}
	}
}
