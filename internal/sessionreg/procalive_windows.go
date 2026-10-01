//go:build windows

package sessionreg

import (
	"errors"

	"golang.org/x/sys/windows"
)

const stillActive = 259 // STILL_ACTIVE

// ProcAlive reports whether a process with pid exists and has not exited.
// Access denied means it exists but belongs to someone else.
func ProcAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil {
		return true
	}
	return code == stillActive
}
