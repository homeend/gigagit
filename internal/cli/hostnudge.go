package cli

import (
	"context"
	"os"
	"slices"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// Source sets in the TUI's reload names, copied from the TUI's own
// opAffectedSources for the op each verb runs (srcFeed = "commits").
var (
	srcTree    = []string{"status"}
	srcHistory = []string{"status", "commits", "branches", "reflog"}              // Commit, SmartMerge, CherryPick, ApplyPatch, …
	srcHead    = []string{"status", "commits", "branches", "reflog", "worktrees"} // a checkout: the worktree rows show branches
	srcWts     = []string{"worktrees", "branches"}                                // Create/Remove/MoveWorktree
)

// hostReloadSources names the TUI sources a verb changed — nil for a read
// (and for notes, which post their own reload). afterFailure: the verb can
// exit 1 with the tree changed (a merge stopped on conflicts), so it is
// reported then too. The table is the whole policy: an agent working inside
// a gg console changes git under a TUI whose watchers are off by default,
// and this is how that TUI hears of it.
func hostReloadSources(cmd string, rest []string) (sources []string, afterFailure bool) {
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}
	switch cmd {
	case "commit", "merge", "rebase", "cherry-pick", "revert", "reset", "fast-forward", "undo", "apply":
		return srcHistory, true
	case "pull":
		return []string{"status", "commits", "branches", "reflog", "remotes"}, true
	case "push":
		return []string{"branches", "remotes", "commits"}, false
	case "switch", "checkout":
		return srcHead, true
	case "add", "unstage", "discard", "unlock":
		return srcTree, false
	case "branch":
		switch sub {
		case "create":
			return []string{"branches"}, false
		case "delete":
			return []string{"branches", "commits", "notes"}, false
		case "rename":
			return []string{"status", "branches", "commits", "worktrees", "notes", "reflog"}, false
		}
	case "worktree":
		switch sub {
		case "", "list":
		case "recycle":
			return []string{"worktrees", "branches", "commits", "notes"}, true
		default:
			return srcWts, false
		}
	case "stash":
		if sub != "list" {
			return srcTree, true
		}
	case "shelf":
		switch sub {
		case "restore":
			return srcTree, false
		case "cherry-pick":
			return srcHistory, true
		}
	case "bookmark":
		if sub == "paste" {
			return srcTree, false
		}
	case "tag":
		switch sub {
		case "create", "annotate", "rm", "delete", "push":
			return []string{"tags", "commits"}, false
		case "checkout", "co":
			return srcHead, true
		}
	case "remote":
		switch sub {
		case "fetch", "prune":
			return []string{"remotes"}, false
		case "rm", "remove":
			return []string{"branches", "remotes", "commits", "notes"}, false
		}
	case "versions":
		if sub == "restore" {
			return []string{"status", "branches", "commits", "worktrees", "reflog"}, true
		}
	case "compare":
		if slices.ContainsFunc(rest, func(a string) bool {
			return a == "--save" || a == "--remove" || a == "--rename" ||
				strings.HasPrefix(a, "--save=") || strings.HasPrefix(a, "--remove=") || strings.HasPrefix(a, "--rename=")
		}) {
			return []string{"previews"}, false
		}
	case "preview":
		if sub == "add" || sub == "rm" || sub == "rename" {
			return []string{"previews"}, false
		}
	}
	return nil, false
}

// nudgeHostTUI tells the TUI hosting this console (GG_INBOX) what a verb
// that exited with code changed, naming the worktree it acted in. Best
// effort and never waited on: outside gg there is no inbox and nothing
// happens. Exit 2 (usage, refusal) changed nothing.
func nudgeHostTUI(svc *domain.Service, cmd string, rest []string, code int) {
	inbox := os.Getenv("GG_INBOX")
	if inbox == "" {
		return
	}
	srcs, afterFailure := hostReloadSources(cmd, rest)
	if srcs == nil || !(code == 0 || code == 1 && afterFailure) {
		return
	}
	dir, _ := svc.TopLevel(context.Background())
	_, _ = steer.Post(inbox, steer.Command{Cmd: "reload", Sources: srcs, Dir: dir})
}
