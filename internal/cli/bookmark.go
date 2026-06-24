package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// cmdBookmark implements `gg bookmark <add|list|rm|paste> ...`: a persistent
// registry of richly-addressed file references. add stores a pointer; paste
// resolves its (live or frozen) bytes into the working tree as unstaged.
func cmdBookmark(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg bookmark <add|list|rm|paste> ...")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return bookmarkAdd(svc, rest, stdout, stderr)
	case "list":
		return bookmarkList(svc, rest, stdout, stderr)
	case "rm":
		return bookmarkRemove(svc, rest, stdout, stderr)
	case "paste":
		return bookmarkPaste(svc, rest, stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "bookmark: unknown subcommand %q\n", sub)
		return 2
	}
}

func bookmarkAdd(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bookmark add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rev := fs.String("rev", "", "bookmark a committed file at this commit/branch")
	staged := fs.Bool("staged", false, "bookmark the index (staged) side")
	wt := fs.String("worktree", "", "worktree top-level to target (default: this repo)")
	label := fs.String("label", "", "human label (default: the display string)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	paths := fs.Args()
	if len(paths) == 0 {
		fmt.Fprintln(stderr, "usage: gg bookmark add [--rev <commit>] [--staged] [--worktree <path>] [--label <l>] <path>...")
		return 2
	}
	if *rev != "" && (*staged || *wt != "") {
		fmt.Fprintln(stderr, "bookmark add: --rev is mutually exclusive with --staged/--worktree")
		return 2
	}
	ctx := context.Background()
	worktree := *wt
	if *rev == "" && worktree == "" {
		top, err := svc.TopLevel(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		worktree = top
	}
	for _, p := range paths {
		b := model.Bookmark{Path: p, Label: *label}
		switch {
		case *rev != "":
			b.State, b.Commit = model.StateCommitted, *rev
		case *staged:
			b.State, b.Worktree = model.StateStaged, worktree
		default:
			b.State, b.Worktree = model.StateUnstaged, worktree
		}
		stored, err := svc.BookmarkAdd(ctx, b)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprintln(stdout, stored.ID)
	}
	return 0
}

func bookmarkList(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if err := flag.NewFlagSet("bookmark list", flag.ContinueOnError).Parse(args); err != nil {
		return 2
	}
	bs, err := svc.BookmarkList(context.Background(), 0, 0)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, b := range bs {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", b.ID, b.State.String(), b.Path)
	}
	return 0
}

func bookmarkRemove(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bookmark rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg bookmark rm <id>")
		return 2
	}
	if err := svc.BookmarkRemove(context.Background(), fs.Arg(0)); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func bookmarkPaste(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bookmark paste", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "overwrite an existing destination")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: gg bookmark paste [--force] <id> <dest>")
		return 2
	}
	id, dest := fs.Arg(0), fs.Arg(1)
	bm, err := svc.BookmarkGet(context.Background(), id)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	blob, err := svc.BookmarkBytes(context.Background(), bm)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	policy := map[string]string{"overwrite": "cancel"}
	if *force {
		policy["overwrite"] = "overwrite"
	}
	res, err := runOperation(context.Background(), svc,
		engine.WriteFile{Path: dest, Data: blob}, cliDecider{policy: policy}, stderr)
	if errors.Is(err, engine.ErrWriteCancelled) {
		fmt.Fprintf(stderr, "bookmark paste: %s already exists; pass --force to overwrite\n", dest)
		return 2
	}
	return finish(res, err, stdout, stderr)
}
