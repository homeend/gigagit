package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// parseEndpoint maps a CLI token to a comparison endpoint: "@worktree" (the
// working tree), "@staged"/"@index" (the index), or any other token as a
// commit-ish (git resolves HEAD, branch names, abc123, HEAD~2, …).
func parseEndpoint(s string) model.Endpoint {
	switch s {
	case "@worktree":
		return model.Endpoint{Kind: model.EndpointWorkTree}
	case "@staged", "@index":
		return model.Endpoint{Kind: model.EndpointIndex}
	default:
		return model.Endpoint{Kind: model.EndpointCommit, Hash: s}
	}
}

// validComparePair reports whether (left, right) is one of the four forward
// forms DiffTreeFiles supports: ordered oldest→newest as commit → @staged →
// @worktree, plus commit↔commit. It lets cmdCompare give a friendly message
// instead of leaking the verb's internal "unsupported endpoint pair" error.
func validComparePair(left, right model.Endpoint) bool {
	// A frozen shelf side pairs with a commit or another shelf endpoint only:
	// the tar snapshots a commit's changes, so diffing it against the live
	// index/worktree would mix a frozen past with a moving target.
	if left.Kind == model.EndpointShelf || right.Kind == model.EndpointShelf {
		pairable := func(e model.Endpoint) bool {
			return e.Kind == model.EndpointCommit || e.Kind == model.EndpointShelf
		}
		return pairable(left) && pairable(right)
	}

	rank := func(e model.Endpoint) int {
		switch e.Kind {
		case model.EndpointCommit:
			return 0
		case model.EndpointIndex:
			return 1
		default: // worktree
			return 2
		}
	}
	if left.Kind == model.EndpointCommit && right.Kind == model.EndpointCommit {
		return true
	}
	return rank(left) < rank(right)
}

// cmdCompare prints the changed-file list (or, with --patch, unified diffs)
// between two endpoints:
//
//	gg compare [--patch] <left> [<right>]
//
// where each endpoint is a commit-ish, @staged, @worktree, or a stored commit
// entry: bookmark:<id> / shelf:<id> (hybrid — the live sha while it exists, a
// shelved entry's frozen tar after a gc; the fallback is noted on stderr).
// <right> defaults to @worktree. List output is one "<status>\t<path>" line
// per changed file.
func cmdCompare(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	patch := fs.Bool("patch", false, "print unified diffs instead of the changed-file list")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	args = fs.Args()
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg compare [--patch] <left> [<right>]   (endpoints: a commit, @staged, @worktree, bookmark:<id>, shelf:<id>; right defaults to @worktree)")
		return 2
	}
	left, code := resolveCompareSpec(svc, args[0], stderr)
	if code != 0 {
		return code
	}
	right := model.Endpoint{Kind: model.EndpointWorkTree}
	if len(args) > 1 {
		if right, code = resolveCompareSpec(svc, args[1], stderr); code != 0 {
			return code
		}
	}
	if !validComparePair(left, right) {
		if left.Kind == model.EndpointShelf || right.Kind == model.EndpointShelf {
			fmt.Fprintln(stderr, "compare: a frozen shelf entry pairs only with a commit or another shelf entry (never @staged/@worktree)")
		} else {
			fmt.Fprintln(stderr, "compare: order endpoints oldest→newest (a commit, then @staged, then @worktree); e.g. `gg compare main @worktree`, not the reverse")
		}
		return 2
	}
	if *patch {
		diff, err := svc.ComparePatch(context.Background(), left, right)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprint(stdout, diff)
		return 0
	}
	files, err := svc.CompareFiles(context.Background(), left, right)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, f := range files {
		if f.OldPath != "" {
			fmt.Fprintf(stdout, "%s\t%s -> %s\n", f.Status, f.OldPath, f.Path)
			continue
		}
		fmt.Fprintf(stdout, "%s\t%s\n", f.Status, f.Path)
	}
	return 0
}

// resolveCompareSpec turns one CLI token into an endpoint. bookmark:<id> and
// shelf:<id> address a stored commit entry and resolve hybrid (live sha while
// it exists, frozen tar for a gc'd shelved commit — noted on stderr so stdout
// stays parseable); anything else is the existing vocabulary
// (@worktree/@staged/commit-ish). The int is an exit code: 0 = resolved,
// 1 = failure (gone bookmark), 2 = usage (unknown id / not a commit entry).
func resolveCompareSpec(svc *domain.Service, tok string, stderr io.Writer) (model.Endpoint, int) {
	ctx := context.Background()
	switch {
	case strings.HasPrefix(tok, "bookmark:"):
		id := strings.TrimPrefix(tok, "bookmark:")
		b, err := svc.BookmarkGet(ctx, id)
		if err != nil {
			fmt.Fprintf(stderr, "compare: bookmark %q: %v\n", id, err)
			return model.Endpoint{}, 2
		}
		if !b.IsCommit() {
			fmt.Fprintf(stderr, "compare: bookmark %q is a file bookmark, not a commit\n", id)
			return model.Endpoint{}, 2
		}
		ep, err := svc.ResolveCommitEntryEndpoint(ctx, b.Commit, "")
		if err != nil {
			fmt.Fprintln(stderr, "compare:", err)
			return model.Endpoint{}, 1
		}
		return ep, 0
	case strings.HasPrefix(tok, "shelf:"):
		id := strings.TrimPrefix(tok, "shelf:")
		e, err := svc.ShelfFind(ctx, id)
		if err != nil {
			fmt.Fprintf(stderr, "compare: shelf %q: %v\n", id, err)
			return model.Endpoint{}, 2
		}
		if !e.IsCommit() {
			fmt.Fprintf(stderr, "compare: shelf entry %q is a file entry, not a commit\n", id)
			return model.Endpoint{}, 2
		}
		ep, err := svc.ResolveCommitEntryEndpoint(ctx, e.Origin.Commit, e.ID)
		if err != nil {
			fmt.Fprintln(stderr, "compare:", err)
			return model.Endpoint{}, 1
		}
		if ep.Kind == model.EndpointShelf {
			sha := e.Origin.Commit
			if len(sha) > 7 {
				sha = sha[:7]
			}
			fmt.Fprintf(stderr, "# frozen compare: commit %s no longer exists\n", sha)
		}
		return ep, 0
	default:
		return parseEndpoint(tok), 0
	}
}
