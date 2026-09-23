//go:build windows

package agentsession

import "os/exec"

// prepareCmd is a no-op on Windows: ConPTY attaches the console itself, and
// the job object (kill_windows.go) groups the process tree.
func prepareCmd(*exec.Cmd) {}
