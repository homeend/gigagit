package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// cmdShelf implements `gg shelf <add|commit|restore|export|list|rm> ...`: a
// per-file, non-git content store. add/commit freeze a copy; restore writes a
// file entry back to the working tree as an unstaged change; export writes any
// entry's files to a directory outside the working tree; list/rm manage
// entries.
func cmdShelf(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg shelf <add|commit|restore|export|list|rm> ...")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return shelfAdd(svc, rest, stdout, stderr)
	case "commit":
		return shelfCommit(svc, rest, stdout, stderr)
	case "list":
		return shelfList(svc, rest, stdout, stderr)
	case "rm":
		return shelfRemove(svc, rest, stdout, stderr)
	case "restore":
		return shelfRestore(svc, rest, stdin, stdout, stderr)
	case "export":
		return shelfExport(svc, rest, stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "shelf: unknown subcommand %q\n", sub)
		return 2
	}
}

func shelfAdd(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("shelf add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	staged := fs.Bool("staged", false, "shelve the index (staged) version")
	rev := fs.String("rev", "", "shelve the version at this commit/branch")
	bucket := fs.String("bucket", "", "target bucket (default: default)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	paths := fs.Args()
	if len(paths) == 0 {
		fmt.Fprintln(stderr, "usage: gg shelf add [--staged|--rev <commit>] [--bucket <name>] <path>...")
		return 2
	}
	ctx := context.Background()
	var worktree, branch string
	if *rev == "" { // working/index origin: capture worktree + branch for display
		top, err := svc.TopLevel(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		worktree = top
		if br, err := svc.CurrentBranch(ctx); err == nil {
			branch = br
		}
	}
	for _, p := range paths {
		var addr model.FileAddress
		switch {
		case *rev != "":
			addr = model.FileAddress{State: model.StateCommitted, Commit: *rev, Path: p}
		case *staged:
			addr = model.FileAddress{State: model.StateStaged, Worktree: worktree, Branch: branch, Path: p}
		default:
			addr = model.FileAddress{State: model.StateUnstaged, Worktree: worktree, Branch: branch, Path: p}
		}
		e, err := svc.ShelfAdd(ctx, addr, *bucket)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprintln(stdout, e.ID)
	}
	return 0
}

func shelfList(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("shelf list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bucket := fs.String("bucket", "", "bucket to list")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	es, err := svc.ShelfList(context.Background(), *bucket, 0, 0) // 0 limit = all
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, e := range es {
		disp := e.Origin.Display()
		if e.Label != "" {
			disp += " — " + e.Label
		}
		fmt.Fprintf(stdout, "%s\t%s\t%dB\n", e.ID, disp, e.Size)
	}
	return 0
}

func shelfRemove(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("shelf rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg shelf rm <entry>")
		return 2
	}
	if err := svc.ShelfRemove(context.Background(), fs.Arg(0)); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func shelfRestore(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("shelf restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "overwrite an existing destination")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: gg shelf restore [--force] <entry> <dest>")
		return 2
	}
	entry, dest := fs.Arg(0), fs.Arg(1)
	blob, err := svc.ShelfBlob(context.Background(), entry)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	// Answer the engine's Overwrite/Cancel fork from policy only: --force =
	// overwrite, otherwise cancel (an existing differing dest then refuses).
	policy := map[string]string{"overwrite": "cancel"}
	if *force {
		policy["overwrite"] = "overwrite"
	}
	dec := cliDecider{policy: policy}
	res, err := runOperation(context.Background(), svc,
		engine.WriteFile{Path: dest, Data: blob}, dec, stderr)
	if errors.Is(err, engine.ErrWriteCancelled) {
		fmt.Fprintf(stderr, "shelf restore: %s already exists; pass --force to overwrite\n", dest)
		return 2
	}
	return finish(res, err, stdout, stderr)
}

// shelfCommit freezes a commit's changed files (content at that sha) into a
// durable, path-less shelf entry.
func shelfCommit(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("shelf commit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "human name for the shelved commit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg shelf commit [--name <name>] <sha>")
		return 2
	}
	e, err := svc.ShelfAddCommit(context.Background(), fs.Arg(0), *name)
	if err != nil {
		fmt.Fprintf(stderr, "shelf commit: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "shelved commit as %s\n", e.ID)
	return 0
}

// shelfExport writes a shelf entry's files to a directory outside the working
// tree, defaulting the target to <main-worktree>.tmp/<name>.
func shelfExport(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("shelf export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "target directory (default: <repo>.tmp/<name>)")
	force := fs.Bool("force", false, "overwrite an existing target directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg shelf export [--dir <path>] [--force] <entry-id>")
		return 2
	}
	ctx := context.Background()
	e, ok := shelfEntryByID(svc, ctx, fs.Arg(0))
	if !ok {
		fmt.Fprintf(stderr, "shelf export: no entry %q\n", fs.Arg(0))
		return 1
	}
	files, name, err := svc.ExportShelfEntry(ctx, e)
	if err != nil {
		fmt.Fprintf(stderr, "shelf export: %v\n", err)
		return 1
	}
	target := *dir
	if target == "" {
		base, err := svc.TempExportBase(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "shelf export: %v\n", err)
			return 1
		}
		target = filepath.Join(base, name)
	}
	// Answer the engine's Overwrite/Cancel fork from policy only: --force =
	// overwrite, otherwise cancel (an existing target then refuses).
	policy := map[string]string{"overwrite": "cancel"}
	if *force {
		policy["overwrite"] = "overwrite"
	}
	dec := cliDecider{policy: policy}
	res, err := runOperation(ctx, svc, engine.ExportToDir{Dir: target, Files: files}, dec, stderr)
	if errors.Is(err, engine.ErrExportCancelled) {
		fmt.Fprintf(stderr, "shelf export: %s already exists; pass --force to overwrite\n", target)
		return 2
	}
	return finish(res, err, stdout, stderr)
}

// shelfEntryByID scans shelf pages (default bucket) for an entry id.
func shelfEntryByID(svc *domain.Service, ctx context.Context, id string) (model.ShelfEntry, bool) {
	for skip := 0; ; skip += 100 {
		page, err := svc.ShelfList(ctx, "", skip, 100)
		if err != nil || len(page) == 0 {
			return model.ShelfEntry{}, false
		}
		for _, e := range page {
			if e.ID == id {
				return e, true
			}
		}
		if len(page) < 100 {
			return model.ShelfEntry{}, false
		}
	}
}
