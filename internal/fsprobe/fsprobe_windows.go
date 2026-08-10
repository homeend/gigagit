package fsprobe

import "strings"

// Foreign reports whether path is a UNC network path (\\server\share). Mapped
// network drive letters are not resolved in this first cut — the trap this
// package exists for (WSL drvfs) is Linux-side, and a native local drive
// letter is the common fast case on Windows.
func Foreign(path string) bool {
	return strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//")
}
