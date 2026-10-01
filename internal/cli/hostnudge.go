package cli

import (
	"context"
	"os"
	"slices"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// Source sets a successful verb changes, in the TUI's reload names.
var (
	srcTree     = []string{"status"}
	srcHistory  = []string{"status", "commits", "branches"}
	srcHead     = []string{"status", "commits", "branches", "worktrees"}
	srcBranches = []string{"branches", "worktrees"} // a worktree row shows its branch
	srcWts      = []string{"worktrees", "branches"} // a branch row shows its worktree
	srcRemote   = []string{"remotes", "branches", "commits"}
)

// hostReloadSources names the TUI sources a SUCCESSFUL verb changed; nil
// for a read (and for notes, which post their own reload). The table is the
// whole policy: an agent working inside a gg console changes git under a TUI
// whose watchers are off by default, and this is how that TUI hears of it.
func hostReloadSources(cmd string, rest []string) []string {
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}
	switch cmd {
	case "commit", "merge", "rebase", "cherry-pick", "revert", "reset", "fast-forward", "undo":
		return srcHistory
	case "pull":
		return []string{"status", "commits", "branches", "remotes"}
	case "push":
		return []string{"branches", "remotes", "commits"}
	case "switch", "checkout":
		return srcHead
	case "add", "unstage", "discard", "apply":
		return srcTree
	case "branch":
		if sub == "create" || sub == "delete" || sub == "rename" {
			return srcBranches
		}
	case "worktree":
		if sub != "" && sub != "list" {
			return srcWts
		}
	case "stash":
		if sub != "list" {
			return srcTree
		}
	case "shelf":
		if sub == "add" || sub == "restore" || sub == "cherry-pick" {
			return srcTree
		}
	case "tag":
		switch sub {
		case "create", "annotate", "delete", "push":
			return []string{"tags"}
		case "checkout":
			return srcHead
		}
	case "remote":
		if sub == "fetch" || sub == "prune" || sub == "rm" {
			return srcRemote
		}
	case "versions":
		if sub == "restore" {
			return []string{"branches", "commits"}
		}
	case "compare":
		if slices.ContainsFunc(rest, func(a string) bool {
			return a == "--save" || a == "--remove" || a == "--rename" ||
				strings.HasPrefix(a, "--save=") || strings.HasPrefix(a, "--remove=") || strings.HasPrefix(a, "--rename=")
		}) {
			return []string{"previews"}
		}
	case "preview":
		if sub == "add" || sub == "rm" || sub == "rename" {
			return []string{"previews"}
		}
	}
	return nil
}

// nudgeHostTUI tells the TUI hosting this console (GG_INBOX) what a
// successful verb changed, naming the worktree it acted in. Best effort and
// never waited on: outside gg there is no inbox and nothing happens.
func nudgeHostTUI(svc *domain.Service, cmd string, rest []string) {
	inbox := os.Getenv("GG_INBOX")
	if inbox == "" {
		return
	}
	srcs := hostReloadSources(cmd, rest)
	if srcs == nil {
		return
	}
	dir, _ := svc.TopLevel(context.Background())
	_, _ = steer.Post(inbox, steer.Command{Cmd: "reload", Sources: srcs, Dir: dir})
}
