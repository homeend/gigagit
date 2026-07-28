package model

import "time"

// GitLock is one git lockfile found on disk (`.git/index.lock` and friends).
// Git creates these while rewriting the file they shadow and removes them on
// exit — including on SIGTERM, via its own signal handler — so one lingering
// after no git is running means a git process died hard. Until it is removed,
// every operation touching that file fails with "Another git process seems to
// be running in this repository".
type GitLock struct {
	Path    string    // absolute path to the .lock file
	Name    string    // base name, e.g. "index.lock" — the display label
	ModTime time.Time // when git created it; the age is the staleness hint
}

// RepoHealth is the cheap repo-health snapshot behind the notification
// center: stat-level filesystem facts plus one config lookup — no expensive
// git walks, safe to run in the background on every repo load.
type RepoHealth struct {
	GitCommonDir          string // absolute git common dir (doubles as the per-repo dismissal key)
	PackBytes             int64  // total size of *.pack under objects/pack
	HasCommitGraph        bool   // objects/info/commit-graph file OR commit-graphs/ chain dir present
	WriteCommitGraphSet   bool   // fetch.writeCommitGraph set in local or global scope
	WriteCommitGraphValue string // the set value ("" when unset)
	// UnmappedBranches lists local branches whose upstream is configured
	// (branch.<n>.remote + .merge) but unresolvable because the remote's
	// fetch refspec does not map them (single-branch/shallow clones) — the
	// state where a push never moves the remote-tracking ref. Sorted.
	UnmappedBranches []string
	// StaleLocks lists git lockfiles present in the worktree's git dir or the
	// common dir. Feeds the stale_git_lock notice, which offers to remove
	// them — the recovery path for a git that was killed mid-write (always on
	// Windows, where cancellation cannot be graceful).
	StaleLocks []GitLock
}
