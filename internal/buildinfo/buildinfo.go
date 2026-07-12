// Package buildinfo exposes version and build metadata, set via -ldflags at build time.
package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Version is overridden at build time with -ldflags "-X .../buildinfo.Version=...".
var Version = "dev"

// Commit is overridden at build time.
var Commit = "none"

func init() {
	if bi, ok := debug.ReadBuildInfo(); ok {
		Version, Commit = resolve(Version, Commit, bi)
	}
}

// resolve fills in Version/Commit from Go's embedded build info when the
// -ldflags defaults are still in place: `go install module@version` stamps
// the module version (but no vcs settings), while `go build` in a checkout
// stamps vcs.revision/vcs.modified (but Main.Version is "(devel)").
func resolve(version, commit string, bi *debug.BuildInfo) (string, string) {
	if bi == nil {
		return version, commit
	}
	if version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		version = bi.Main.Version
	}
	if commit == "none" {
		var rev, modified string
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				modified = s.Value
			}
		}
		if rev != "" {
			if len(rev) > 7 {
				rev = rev[:7]
			}
			if modified == "true" {
				rev += "-dirty"
			}
			commit = rev
		}
	}
	return version, commit
}

// String returns a one-line human-readable build identifier.
func String() string {
	return fmt.Sprintf("gg %s (%s) %s/%s", Version, Commit, runtime.GOOS, runtime.GOARCH)
}
