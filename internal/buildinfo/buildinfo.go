// Package buildinfo exposes version and build metadata, set via -ldflags at build time.
package buildinfo

import (
	"fmt"
	"runtime"
)

// Version is overridden at build time with -ldflags "-X .../buildinfo.Version=...".
var Version = "dev"

// Commit is overridden at build time.
var Commit = "none"

// String returns a one-line human-readable build identifier.
func String() string {
	return fmt.Sprintf("gg %s (%s) %s/%s", Version, Commit, runtime.GOOS, runtime.GOARCH)
}
