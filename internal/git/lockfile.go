package git

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// lockCandidates are the lockfile names gg probes for. Git creates a `<file>.lock`
// beside whatever it is about to rewrite; these are the ones a killed git
// realistically strands and that block later operations.
//
// This is a fixed stat-level list on purpose — the PausedOpIn precedent. The
// alternative, walking refs/ for *.lock, is unbounded work on a repo with
// hundreds of thousands of refs, and this probe runs on every repo load.
var lockCandidates = []string{
	"index.lock",       // git add / commit / status writeback — the common one
	"HEAD.lock",        // any HEAD move: checkout, reset, commit
	"FETCH_HEAD.lock",  // fetch
	"ORIG_HEAD.lock",   // merge, rebase, reset
	"shallow.lock",     // fetch in a shallow clone
	"packed-refs.lock", // ref packing, fetch, gc
	"config.lock",      // git config writes
}

// LockFiles reports the git lockfiles currently present in any of dirs,
// newest first is NOT imposed — order follows lockCandidates so the display is
// stable. Pass this worktree's git dir AND the common dir: index.lock and
// HEAD.lock are per-worktree, while packed-refs.lock, config.lock and
// shallow.lock live in the common dir. A repeated or empty dir is skipped, and
// a path already reported is never listed twice (in the main worktree the two
// dirs are the same directory).
//
// Presence alone does NOT prove a lock is stale — a git process running right
// now legitimately holds one. gg surfaces these for a human to judge and only
// ever removes them while holding an exclusive repo reservation, so no gg
// operation can be mid-flight.
func LockFiles(dirs ...string) []model.GitLock {
	var out []model.GitLock
	seen := map[string]bool{}
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		for _, name := range lockCandidates {
			p := filepath.Join(dir, name)
			if seen[p] {
				continue
			}
			info, err := os.Stat(p)
			if err != nil || info.IsDir() {
				continue
			}
			seen[p] = true
			out = append(out, model.GitLock{Path: p, Name: name, ModTime: info.ModTime()})
		}
	}
	return out
}

// IsLockFilePath reports whether p names a git lockfile gg is willing to
// remove: it must be one of the known candidate names. The engine op pairs
// this with a check that the path really sits inside the repository's git
// dirs, so a frontend bug can never delete an arbitrary file (the
// DeleteBranchVersion refuse-outside-the-namespace precedent).
func IsLockFilePath(p string) bool {
	base := filepath.Base(p)
	for _, name := range lockCandidates {
		if base == name {
			return true
		}
	}
	return false
}

// IsLockError reports whether err is git refusing to run because a lockfile
// already exists — the "Another git process seems to be running in this
// repository" failure. Matched on the message because it arrives as plain
// stderr text wrapped by the runner and then by the engine; there is no exit
// code that distinguishes it (git exits 128 for many fatals).
//
// Both fragments are matched independently: git prints the "Unable to create
// … .lock: File exists" line for every lockfile, while the longer
// "Another git process" advisory only accompanies index.lock.
func IsLockError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "Another git process seems to be running") {
		return true
	}
	return strings.Contains(msg, ".lock") &&
		strings.Contains(msg, "File exists") &&
		strings.Contains(msg, "Unable to create")
}
