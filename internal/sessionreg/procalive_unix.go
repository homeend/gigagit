//go:build !windows

package sessionreg

import (
	"errors"
	"syscall"
)

// ProcAlive reports whether a process with pid exists (signal 0; EPERM means
// it exists but belongs to someone else).
func ProcAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
