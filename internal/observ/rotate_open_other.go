//go:build !windows

package observ

import "os"

// openAppend opens path for appending, creating it as needed.
func openAppend(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}
