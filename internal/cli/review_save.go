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
		return printReviewTarget(stdout, target, *asJSON)
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
	id, warn, err := tsvc.SaveReview(ctx, domain.SaveReview{Target: target, Agent: strings.TrimSpace(*agent), Text: string(data)})
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

func printReviewTarget(w io.Writer, t domain.ReviewTarget, asJSON bool) int {
	diff := t.Range
	if t.Kind == domain.ReviewWorking {
		diff = "HEAD" // git diff HEAD: the working tree + the index vs HEAD
	}
	if asJSON {
		_ = json.NewEncoder(w).Encode(map[string]string{"kind": reviewKind(t), "label": t.DisplayLabel(), "range": t.Range, "diff": diff})
		return 0
	}
	fmt.Fprintf(w, "%s: %s\nread it with: gg diff %s\n", reviewKind(t), t.DisplayLabel(), diff)
	return 0
}
