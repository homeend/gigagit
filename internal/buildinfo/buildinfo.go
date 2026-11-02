// Package buildinfo exposes version and build metadata, set via -ldflags at build time.
package buildinfo

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"time"
)

// Version is overridden at build time with -ldflags "-X .../buildinfo.Version=...".
var Version = "dev"

// Commit is overridden at build time.
var Commit = "none"

// BuildTime is overridden at build time: RFC 3339 (build.sh, build.cmd,
// goreleaser). A plain `go build` leaves it empty.
var BuildTime = ""

// Read once at start: a binary rebuilt while it runs must keep describing
// the program that is running, not the file now at its path.
var (
	exePath       string
	builtAt       time.Time
	builtFromFile bool
)

func init() {
	if bi, ok := debug.ReadBuildInfo(); ok {
		Version, Commit = resolve(Version, Commit, bi)
	}
	exePath, _ = os.Executable()
	builtAt, builtFromFile = built(BuildTime, os.Executable)
}

// built is when this binary was made: the stamp, else the executable file's
// modification time (fromFile), else zero.
func built(stamp string, exe func() (string, error)) (at time.Time, fromFile bool) {
	if at, err := time.Parse(time.RFC3339, stamp); err == nil {
		return at, false
	}
	path, err := exe()
	if err != nil {
		return time.Time{}, false
	}
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}, false
	}
	return st.ModTime(), true
}

// Info identifies the running binary, for a person checking which one it is.
type Info struct {
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	Built         string `json:"built"`           // local time; "" when unknown
	BuiltFromFile bool   `json:"built_from_file"` // no stamp: Built is the file's modification time
	Path          string `json:"path"`
	Platform      string `json:"platform"`
}

// Current describes the running binary.
func Current() Info {
	in := Info{Version: Version, Commit: Commit, BuiltFromFile: builtFromFile, Path: exePath, Platform: runtime.GOOS + "/" + runtime.GOARCH}
	if !builtAt.IsZero() {
		in.Built = builtAt.Local().Format("2006-01-02 15:04:05 -0700")
	}
	return in
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
