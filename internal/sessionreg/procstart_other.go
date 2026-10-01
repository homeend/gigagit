//go:build !linux && !windows

package sessionreg

import "time"

// procStart is unknown here (no /proc): liveness falls back to the pid alone.
func procStart(int) (time.Time, bool) { return time.Time{}, false }

func procStartKnown() bool { return false }
