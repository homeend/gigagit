package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

const unlockUsage = `usage: gg unlock [--yes]

Lists git lock files (.git/index.lock and friends) left behind in this
repository. A lock file only outlives its git process when that process was
killed before it could clean up, and until it is removed git refuses to run:
"Another git process seems to be running in this repository".

  --yes   remove the lock files that were found

Without --yes nothing is removed: gg cannot see git processes it did not
start, and deleting a lock a LIVE git is holding would corrupt that write.
Check that no other git is running, then re-run with --yes.

Exit: 0 clean or removed, 1 locks present without --yes (or a failure), 2 usage.`

// cmdUnlock is the CLI half of the stale-lock recovery the TUI offers through
// its notification center. Read-only by default and explicitly opt-in to
// delete — the destructive direction is never the default in a scriptable
// frontend, where nobody is watching the output.
func cmdUnlock(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("unlock", flag.ContinueOnError)
	fs.SetOutput(stderr)
	yes := fs.Bool("yes", false, "remove the lock files that were found")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, unlockUsage)
		return 2
	}

	ctx := context.Background()
	locks, err := svc.StaleLocks(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if len(locks) == 0 {
		fmt.Fprintln(stdout, "(no git locks)")
		return 0
	}

	now := time.Now()
	for _, l := range locks {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", l.Name, cliLockAge(now.Sub(l.ModTime)), l.Path)
	}
	if !*yes {
		// Exit 1, not 0: a script that runs `gg unlock` as a precondition
		// check must be able to tell "repo is usable" from "repo is locked".
		fmt.Fprintln(stderr, "error: locks present; make sure no other git is running, then re-run with --yes to remove them")
		return 1
	}

	paths := make([]string, 0, len(locks))
	for _, l := range locks {
		paths = append(paths, l.Path)
	}
	res, err := runOperation(ctx, svc, engine.RemoveGitLocks{Paths: paths}, engine.MapDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// cliLockAge is the terse, script-friendly age rendering (the TUI has its own
// translated one). Fixed units so a caller can cut on them.
func cliLockAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
