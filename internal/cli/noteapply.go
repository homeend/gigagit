package cli

// `gg note apply --stdin` — the batch import lane. The actual planner and
// applier (domain.PlanNoteBatch / domain.ApplyNoteBatch) live in internal/domain,
// shared with `gg review --notes` and the MCP gg_notes_apply tool (spec §4.5:
// every batch-importing caller validates and writes through the SAME
// all-or-nothing path). This file is just the `--stdin` flag surface plus the
// stored-notes printer.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

// printStoredNotes emits the ids (one per line) or the wire notes as JSON.
func printStoredNotes(w io.Writer, notes []model.Note, asJSON bool) error {
	if !asJSON {
		for _, n := range notes {
			if _, err := fmt.Fprintln(w, n.ID); err != nil {
				return err
			}
		}
		return nil
	}
	wires := make([]domain.WireNote, 0, len(notes))
	for _, n := range notes {
		wires = append(wires, domain.ToWireNote(domain.ResolvedNote{Note: n, Status: model.NoteActive, Range: n.Range}))
	}
	return json.NewEncoder(w).Encode(wires)
}

// warnSkippedOldSide prints the ONE warning for the old-side items a batch
// import dropped, naming the reason the target that dropped them has: a merge
// preview's old side is the merge base, a review's is its own base. Shared by
// `gg note apply` and `gg review --notes` so the same condition never gets two
// different explanations.
func warnSkippedOldSide(stderr io.Writer, skipped int, preview bool) {
	if skipped <= 0 {
		return
	}
	why := "this review's base is not a note-addressable side"
	if preview {
		why = "notes in a preview anchor on the new side"
	}
	fmt.Fprintf(stderr, "note: skipped %d old-side annotation(s) — %s\n", skipped, why)
}

func noteApply(svc *domain.Service, link *domain.Resolved, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("note apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	pf := addPreviewFlag(fs)
	useStdin := fs.Bool("stdin", false, "read the JSON batch from stdin (required)")
	author := fs.String("author", "", "author for items that carry none (default: $GG_AGENT, else agent)")
	asJSON := fs.Bool("json", false, "print the stored notes as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "note apply: unexpected argument %q; a gg:// link must be the first argument\n", fs.Arg(0))
		return 2
	}
	if !*useStdin {
		fmt.Fprintln(stderr, "usage: gg note apply [<repo-link>] --stdin [--cached | --rev <commit>] [--author <name>] [--json]")
		return 2
	}
	if strings.TrimSpace(*tf.file) != "" {
		fmt.Fprintln(stderr, "note apply: --file is not used; each batch item names its own path")
		return 2
	}
	// A link's target (`@staged`, `@<sha>`, or neither = the working tree)
	// stands in for --cached/--rev; both at once is a usage error, not an
	// override. cmdNote already refused a link carrying a path.
	cached, rev := *tf.cached, *tf.rev
	if link != nil {
		// Ruling 9: a link and a preview both name a target (see noteAdd).
		if pf.set() {
			return previewUsageErr("note apply", stderr)
		}
		if cached || rev != "" {
			fmt.Fprintln(stderr, "note apply: a gg:// link already names the target (drop --cached and --rev)")
			return 2
		}
		cached, rev = link.Addr.State == model.StateStaged, link.Addr.Commit
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintln(stderr, "error: reading stdin:", err)
		return 1
	}
	batch, err := notebatch.Parse(data)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	// Unanchored prose has nowhere to live in gg's model: echo it so the human
	// still sees it, and store only what is anchored.
	for _, c := range batch.Contexts {
		fmt.Fprintln(stderr, "context:", c)
	}
	ctx := context.Background()
	target := domain.NoteBatchTarget{Cached: cached, Rev: rev}
	rule := domain.NoteSideBoth
	previewed := false
	if link != nil {
		if pv, ok := previewTargetFromLink(*link); ok {
			// The same three facts --preview sets: stored on the tip, hunk
			// numbers from the PREVIEW's patch, new side only.
			spec := pv.Spec
			target = domain.NoteBatchTarget{Rev: pv.Set.Tip, Hunks: &spec}
			rule = domain.NoteSideNewOnly
			previewed = true
		}
	}
	if pf.set() {
		if cached || rev != "" {
			return previewUsageErr("note apply", stderr)
		}
		tgt, terr := resolvePreviewTarget(ctx, svc, *pf.spec)
		if terr != nil {
			fmt.Fprintln(stderr, "error:", terr)
			return 1
		}
		spec := tgt.Spec
		// Stored on the tip, numbered over the preview's own patch, new side
		// only — old-side items are SKIPPED with one warning (the --working rule).
		target = domain.NoteBatchTarget{Rev: tgt.Set.Tip, Hunks: &spec}
		rule = domain.NoteSideNewOnly
		previewed = true
	}
	planned, skipped, err := svc.PlanNoteBatchIn(ctx, batch, target, noteAuthorDefault(*author), rule)
	if err != nil {
		return noteExit(err, stderr)
	}
	warnSkippedOldSide(stderr, skipped, previewed)
	stored, err := svc.ApplyNoteBatch(ctx, planned)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if err := printStoredNotes(stdout, stored, *asJSON); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}
