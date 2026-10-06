package tui

import (
	"path/filepath"

	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/repos"
)

// defaultErrLogPath puts the always-on error log beside operations.log in the
// gg state dir, reusing the repo registry's platform-appropriate resolution.
// "" when no home/state dir exists.
func defaultErrLogPath() string {
	sp := repos.DefaultStatePath()
	if sp == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sp), "errors.log")
}

// OpenErrorLog opens (creating as needed) the always-on error log for
// appending and returns the handle plus its path. Unlike the operation log it
// has no on/off toggle: every genuine failure is recorded for the whole
// session. The log rolls over weekly (observ.WeeklyLog). Returns (nil, "", nil)
// when there is no state dir — nothing to open, and not an error worth
// blocking TUI launch.
func OpenErrorLog() (*observ.WeeklyLog, string, error) {
	path := defaultErrLogPath()
	if path == "" {
		return nil, "", nil
	}
	l, err := observ.OpenWeeklyLog(path)
	if err != nil {
		return nil, path, err
	}
	return l, path, nil
}
