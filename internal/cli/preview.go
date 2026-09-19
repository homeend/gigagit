package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// cmdPreview implements `gg preview <list|add|rm|rename|show|diff> …`: saved
// merge previews and saved commit pairs — "what would <source> bring into <target>", the GitHub PR
// files-changed diff (merge-base(target, source)..source). Records store
// branch NAMES; every show recomputes from the current tips.
func cmdPreview(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg preview <list|add|rm|rename|show|diff> ...")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return previewList(svc, rest, stdout, stderr)
	case "add":
		return previewAdd(svc, rest, stdout, stderr)
	case "rm":
		return previewRemove(svc, rest, stdout, stderr)
	case "rename":
		return previewRename(svc, rest, stdout, stderr)
	case "show":
		return previewShow(svc, rest, stdout, stderr)
	case "diff":
		return previewDiff(svc, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "preview: unknown subcommand %q (use list, add, rm, rename, show, or diff)\n", sub)
		return 2
	}
}

func previewList(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("preview list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: gg preview list")
		return 2
	}
	ctx := context.Background()
	ps, err := svc.PreviewList(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	// One pair's transient git failure must not blank the whole listing (the
	// web's /api/previews degrades the same way): print that row with state
	// "error" and zero counts, say why on stderr, and keep going. Exit 0 while
	// at least one row summarized normally; 1 only when every row failed, so a
	// script piping the list still learns that it learned nothing.
	ok, failed := 0, 0
	for _, p := range ps {
		sum, err := svc.PreviewSummary(ctx, p.Source, p.Target)
		if err != nil {
			failed++
			fmt.Fprintf(stderr, "preview list: %s: %v\n", p.ID, err)
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\terror\t0\t0\n", p.ID, p.Label, p.Source, p.Target)
			continue
		}
		ok++
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\t%d\t%d\n", p.ID, p.Label, p.Source, p.Target, sum.State, sum.Files, sum.Ahead)
	}
	// Saved commit pairs follow, in the SAME seven columns so one cut serves
	// both kinds: <a> and <b> are full shas where a preview prints its branch
	// names, the state reads "pair" (or "missing"), and ahead is always 0.
	pairs, err := svc.PairList(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, p := range pairs {
		sum, err := svc.PairSummary(ctx, p.A, p.B)
		if err != nil {
			failed++
			fmt.Fprintf(stderr, "preview list: %s: %v\n", p.ID, err)
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\terror\t0\t0\n", p.ID, p.Label, p.A, p.B)
			continue
		}
		ok++
		state := "pair"
		if sum.State != domain.PairOK {
			state = "missing"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\t%d\t0\n", p.ID, p.Label, p.A, p.B, state, sum.Files)
	}
	if ok == 0 && failed > 0 {
		return 1
	}
	return 0
}

func previewAdd(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("preview add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	label := fs.String("label", "", "human label (default: \"<source> → <target>\")")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 1 {
		if a, b, ok := pairSpec(fs.Arg(0)); ok {
			return previewAddPair(svc, a, b, *label, stdout, stderr)
		}
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: gg preview add [--label <text>] <source> <target> | <a>..<b>")
		return 2
	}
	p, err := svc.PreviewAdd(context.Background(), fs.Arg(0), fs.Arg(1), *label)
	if errors.Is(err, domain.ErrPreviewExists) {
		fmt.Fprintf(stderr, "preview add: %s → %s already saved as %s (%s)\n", p.Source, p.Target, p.ID, p.Label)
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintln(stdout, p.ID)
	return 0
}

func previewRemove(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("preview rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg preview rm <id|label>")
		return 2
	}
	ctx := context.Background()
	p, err := svc.PreviewGet(ctx, fs.Arg(0))
	if err == nil {
		err = svc.PreviewRemove(ctx, p.ID)
	} else if errors.Is(err, domain.ErrPreviewNotFound) {
		var pr domain.CommitPair
		if pr, err = svc.PairGet(ctx, fs.Arg(0)); err == nil {
			err = svc.PairRemove(ctx, pr.ID)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func previewRename(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("preview rename", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: gg preview rename <id|label> <text>")
		return 2
	}
	ctx := context.Background()
	p, err := svc.PreviewGet(ctx, fs.Arg(0))
	if err == nil {
		err = svc.PreviewRename(ctx, p.ID, fs.Arg(1))
	} else if errors.Is(err, domain.ErrPreviewNotFound) {
		var pr domain.CommitPair
		if pr, err = svc.PairGet(ctx, fs.Arg(0)); err == nil {
			err = svc.PairRename(ctx, pr.ID, fs.Arg(1))
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func previewShow(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("preview show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	patch := fs.Bool("patch", false, "print unified diffs instead of the changed-file list")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg preview show [--patch] <id|label>")
		return 2
	}
	p, err := svc.PreviewGet(context.Background(), fs.Arg(0))
	if errors.Is(err, domain.ErrPreviewNotFound) {
		if pr, perr := svc.PairGet(context.Background(), fs.Arg(0)); perr == nil {
			return printPair(svc, pr, *patch, stdout, stderr)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return printPreview(svc, p.Source, p.Target, *patch, stdout, stderr)
}

func previewDiff(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("preview diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	patch := fs.Bool("patch", false, "print unified diffs instead of the changed-file list")
	hunks := fs.Bool("hunks", false, "list each file's numbered git @@ hunks")
	asJSON := fs.Bool("json", false, "with --hunks: emit the hunk list as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *asJSON && !*hunks {
		fmt.Fprintln(stderr, "preview diff: --json requires --hunks")
		return 2
	}
	if *hunks && *patch {
		fmt.Fprintln(stderr, "preview diff: --hunks and --patch are mutually exclusive")
		return 2
	}
	if *hunks {
		// One id/label, or the pair, numbered over the SAME patch
		// `gg diff --preview --hunks` prints.
		if fs.NArg() < 1 || fs.NArg() > 2 {
			fmt.Fprintln(stderr, "usage: gg preview diff --hunks [--json] <id|label>|<source> <target>")
			return 2
		}
		spec := fs.Arg(0)
		if fs.NArg() == 2 {
			spec = fs.Arg(1) + "..." + fs.Arg(0) // <target>...<source>
		}
		ctx := context.Background()
		tgt, err := resolvePreviewTarget(ctx, svc, spec)
		if err != nil {
			// Every resolvePreviewTarget error is already a "preview: …"
			// message (ErrPreviewNotFound, the pair-shape error, the
			// missing-source/target/state messages below) — print it plain,
			// matching printPreview's shape for the same conditions, instead
			// of double-prefixing with "error:".
			fmt.Fprintln(stderr, err)
			return 1
		}
		return renderDiffSpec(ctx, svc, tgt.Spec, true, *asJSON, false, false, stdout, stderr)
	}
	if fs.NArg() == 1 {
		// One argument names a saved COMMIT PAIR (a merge preview is typed as
		// its two branches here); it prints exactly what `gg preview show` does.
		p, err := svc.PairGet(context.Background(), fs.Arg(0))
		if errors.Is(err, domain.ErrPairNotFound) {
			fmt.Fprintf(stderr, "preview diff: no saved pair %q (a merge preview is <source> <target>)\n", fs.Arg(0))
			return 1
		}
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return printPair(svc, p, *patch, stdout, stderr)
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: gg preview diff [--patch] <source> <target> | <pair id|label>")
		return 2
	}
	return printPreview(svc, fs.Arg(0), fs.Arg(1), *patch, stdout, stderr)
}

// printPreview resolves the pair and prints the three-dot file list or
// patch. A non-ok state is reported on stderr with exit 1 (stdout stays
// empty so a script never mistakes "merged" for "no changes").
func printPreview(svc *domain.Service, source, target string, patch bool, stdout, stderr io.Writer) int {
	ctx := context.Background()
	eps, err := svc.PreviewOpen(ctx, source, target)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	switch eps.Summary.State {
	case domain.PreviewOK:
	case domain.PreviewMissingSource:
		fmt.Fprintf(stderr, "preview: missing: %s\n", source)
		return 1
	case domain.PreviewMissingTarget:
		fmt.Fprintf(stderr, "preview: missing: %s\n", target)
		return 1
	default:
		fmt.Fprintf(stderr, "preview: %s → %s: %s\n", source, target, eps.Summary.State)
		return 1
	}
	if patch {
		diff, err := svc.ComparePatch(ctx, eps.Left, eps.Right)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprint(stdout, diff)
		return 0
	}
	files, err := svc.CompareFiles(ctx, eps.Left, eps.Right)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	printCompareFiles(stdout, files)
	return 0
}

// printCompareFiles is `gg compare`'s line format (extracted so both verbs
// stay byte-identical).
func printCompareFiles(stdout io.Writer, files []model.CommitFile) {
	for _, f := range files {
		if f.OldPath != "" {
			fmt.Fprintf(stdout, "%s\t%s -> %s\n", f.Status, f.OldPath, f.Path)
			continue
		}
		fmt.Fprintf(stdout, "%s\t%s\n", f.Status, f.Path)
	}
}

// pairSpec splits "<a>..<b>", the spelling of a saved commit pair. A
// three-dot text is a merge preview and is NOT a pair; a refname cannot hold
// "..", so the first ".." is the separator.
func pairSpec(s string) (a, b string, ok bool) {
	if strings.Contains(s, "...") {
		return "", "", false
	}
	a, b, ok = strings.Cut(s, "..")
	return a, b, ok && a != "" && b != ""
}

// previewAddPair saves the change-set a..b. Both revs FREEZE to full shas in
// domain.PairAdd, so `gg preview add main..feat/x` stores today's two tips
// and never follows the branches — a moving comparison is a merge preview.
func previewAddPair(svc *domain.Service, a, b, label string, stdout, stderr io.Writer) int {
	p, err := svc.PairAdd(context.Background(), a, b, label)
	if errors.Is(err, domain.ErrPairExists) {
		fmt.Fprintf(stderr, "preview add: %s already saved as %s (%s)\n", p.DefaultLabel(), p.ID, p.Label)
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "preview add:", err)
		return 2
	}
	fmt.Fprintln(stdout, p.ID)
	return 0
}

// printPair is printPreview for a saved commit pair: the TWO-dot file list
// or patch between its frozen commits.
func printPair(svc *domain.Service, p domain.CommitPair, patch bool, stdout, stderr io.Writer) int {
	ctx := context.Background()
	eps, err := svc.PairOpen(ctx, p.A, p.B)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	switch eps.Summary.State {
	case domain.PairOK:
	case domain.PairMissingA:
		fmt.Fprintf(stderr, "preview: commit %s is not in this repository\n", p.A)
		return 1
	default:
		fmt.Fprintf(stderr, "preview: commit %s is not in this repository\n", p.B)
		return 1
	}
	if patch {
		diff, err := svc.ComparePatch(ctx, eps.Left, eps.Right)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprint(stdout, diff)
		return 0
	}
	files, err := svc.CompareFiles(ctx, eps.Left, eps.Right)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	printCompareFiles(stdout, files)
	return 0
}
