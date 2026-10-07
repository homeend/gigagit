package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

const reviewSaveUsage = "usage: gg review save <gg-link> --agent <name> (--stdin | --file <path>) [--json]\n       gg review save <gg-link> --dry-run [--json]"

// reviewSave stores a review document an agent wrote itself as the review of
// the change the link names (domain.LinkReviewTarget) — the same note the
// review lane stores, so the TUI and the web show it the same way. --dry-run
// prints what would be reviewed, and the `gg diff` argument that shows it,
// without storing anything: a skill reads the change through it rather than
// guessing (a @ref: link is the tip's own change to `gg diff`, the branch
// against the trunk to a review).
func reviewSave(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review save", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agent := fs.String("agent", "", "the reviewer's name (shown on the review)")
	fromStdin := fs.Bool("stdin", false, "read the review document from stdin")
	file := fs.String("file", "", "read the review document from a file")
	dry := fs.Bool("dry-run", false, "print what the link would review; store nothing")
	asJSON := fs.Bool("json", false, "print JSON")
	// The link comes FIRST (`gg review save <link> --agent …`): flag.Parse
	// stops at the first positional, and --agent/--file take values, so
	// partitionFlags (bool-only) cannot be used.
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(stderr, reviewSaveUsage)
		return 2
	}
	link := args[0]
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(stderr, reviewSaveUsage)
		return 2
	}
	if !*dry && (strings.TrimSpace(*agent) == "" || *fromStdin == (*file != "")) {
		fmt.Fprintln(stderr, reviewSaveUsage)
		return 2
	}
	ctx := context.Background()
	res, err := resolveLinkArg(ctx, svc, link, linkShapes{Pair: true, Ref: true}, "review save")
	if err != nil {
		return linkExit("review save", err, stderr)
	}
	tsvc := openLinkTarget(res)
	target, err := tsvc.LinkReviewTarget(ctx, res)
	if err != nil {
		fmt.Fprintln(stderr, "review save:", err)
		return 1
	}
	if *dry {
		return printReviewTarget(stdout, target, res.Checkout, *asJSON)
	}
	var data []byte
	if *fromStdin {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(*file)
	}
	if err != nil {
		fmt.Fprintln(stderr, "review save:", err)
		return 1
	}
	if _, perr := notebatch.ParseReview(data); perr != nil {
		fmt.Fprintln(stderr, "review save: not a gg review document:", perr)
		return 1
	}
	// A working review carries the fingerprints of what it read, or it would
	// never be "current" (domain.WorkingReviewState): taken now, as the
	// lane takes them at its Prepare.
	var files []model.NoteFile
	if target.Kind == domain.ReviewWorking {
		if files, err = tsvc.WorkingReviewFiles(ctx); err != nil {
			fmt.Fprintln(stderr, "review save:", err)
			return 1
		}
	}
	id, warn, err := tsvc.SaveReview(ctx, domain.SaveReview{Target: target, Agent: strings.TrimSpace(*agent), Text: string(data), Files: files})
	if err != nil {
		fmt.Fprintln(stderr, "review save:", err)
		return 1
	}
	rl, lerr := tsvc.ReviewLink(ctx, id)
	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(map[string]string{"id": id, "link": rl, "warn": warn})
	} else {
		fmt.Fprintln(stdout, "review:", id)
		if lerr == nil {
			fmt.Fprintln(stdout, rl)
		}
	}
	if warn != "" && !*asJSON {
		fmt.Fprintln(stderr, "warning:", warn)
	}
	if lerr != nil && !errors.Is(lerr, domain.ErrReviewNotFound) {
		fmt.Fprintln(stderr, "warning: no link for the review:", lerr)
	}
	return 0
}

// reviewKind names a target for --dry-run.
func reviewKind(t domain.ReviewTarget) string {
	switch {
	case t.Kind == domain.ReviewWorking:
		return "working"
	case t.Kind == domain.ReviewBranch:
		return "branch"
	case strings.Contains(t.Preview, "..."):
		return "preview"
	case t.Preview != "":
		return "pair"
	}
	return "commit"
}

// printReviewTarget is --dry-run's answer: what is reviewed, and how to read
// exactly that. diff is the `gg diff` argument for the patch and the stat; a
// root commit spells its own change against the empty tree, since a bare sha
// would be the working tree vs that commit. hunks is the `gg diff --hunks`
// argument ("" for working changes: no hunk numbering covers HEAD → working
// tree, so read the patch, whose new-side lines are the working file's).
// checkout is where to run both: a working link names another checkout's
// changes.
func printReviewTarget(w io.Writer, t domain.ReviewTarget, checkout string, asJSON bool) int {
	diff, hunks := t.Range, t.Range
	switch {
	case t.Kind == domain.ReviewWorking:
		diff, hunks = "HEAD", "" // git diff HEAD: the working tree + the index vs HEAD
	case !strings.Contains(t.Range, ".."):
		diff = domain.EmptyTreeSHA1 + ".." + t.Range // a root commit (gg diff --hunks <sha> knows)
	}
	if asJSON {
		_ = json.NewEncoder(w).Encode(map[string]string{"kind": reviewKind(t), "label": t.DisplayLabel(),
			"range": t.Range, "diff": diff, "hunks": hunks, "checkout": checkout})
		return 0
	}
	fmt.Fprintf(w, "%s: %s\nin: %s\nread it with: gg diff %s\n", reviewKind(t), t.DisplayLabel(), checkout, diff)
	if hunks != "" {
		fmt.Fprintf(w, "numbered hunks: gg diff --hunks %s\n", hunks)
	}
	return 0
}
