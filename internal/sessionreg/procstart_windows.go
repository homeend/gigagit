//go:build windows

package sessionreg

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// procStart is pid's creation time (GetProcessTimes).
func procStart(pid int) (time.Time, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return time.Time{}, false
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil {
		return time.Time{}, false
	}
	return time.Unix(0, created.Nanoseconds()), true
}

func procStartKnown() bool { _, ok := procStart(os.Getpid()); return ok }
