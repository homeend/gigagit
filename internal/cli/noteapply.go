package cli

// `gg note apply --stdin` — the batch import lane, and the shared planner the
// `gg review --notes` importer and the MCP gg_notes_apply tool reuse.
//
// The contract is ALL-OR-NOTHING: every item is parsed, its address resolved
// and its anchor computed AND VALIDATED before the first write. A batch with
// one bad item exits 1 having stored nothing. Range validation happens in
// planNoteBatch itself (via domain.NoteRangeCheck, a read-only precheck) —
// not by writing and rolling back — so a batch whose third item names a line
// past the end of the file never stores items one and two either.

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

// noteSideRule says which diff sides the import target can address. A single
// commit has both (parent → commit); a range review or a working review has an
// old side that is not a note-addressable base (§4.4's old-side table), so
// old-side items are skipped with one warning.
type noteSideRule int

const (
	sideRuleBoth    noteSideRule = iota // a commit target: both sides are addressable
	sideRuleNewOnly                     // a range or working review: only the new side is
)

// plannedNote is one validated, fully anchored note waiting to be written.
type plannedNote struct {
	Note    model.Note // ready to store; zero ParentID for a root
	ReplyTo string     // non-empty makes it a reply
}

// planNoteBatch resolves every item to a storable note. cached/rev choose the
// target diff for the WHOLE batch; author fills in for items that carry none.
// Every batch note is Source: agent.
//
// skipped counts old-side items dropped by sideRuleNewOnly — the caller warns
// once rather than per item.
//
// Every item's range is validated against its side's real length (via
// domain.NoteRangeCheck) BEFORE it is appended to planned, so a bad range
// anywhere in the batch fails here — before applyNoteBatch performs its first
// write.
func planNoteBatch(ctx context.Context, svc *domain.Service, b notebatch.Batch, cached bool, rev, author string, sideRule noteSideRule) (planned []plannedNote, skipped int, err error) {
	addrCache := map[string]model.FileAddress{}
	for i, it := range b.Items {
		who := it.Author
		if who == "" {
			who = author
		}
		n := model.Note{
			Source: model.NoteSourceAgent, Author: who,
			Summary: it.Summary, Rationale: it.Rationale,
			Tags: it.Tags, Confidence: it.Confidence,
		}
		if it.ReplyTo != "" {
			// Validate the parent NOW: an unknown id must reject the batch
			// before anything is stored.
			if _, gerr := svc.NoteGet(ctx, it.ReplyTo); gerr != nil {
				return nil, 0, fmt.Errorf("item %d: replyTo %s: %w", i, it.ReplyTo, gerr)
			}
			planned = append(planned, plannedNote{Note: n, ReplyTo: it.ReplyTo})
			continue
		}
		addr, ok := addrCache[it.Path]
		if !ok {
			addr, err = svc.NoteTarget(ctx, it.Path, cached, rev)
			if err != nil {
				return nil, 0, fmt.Errorf("item %d: %w", i, err)
			}
			addrCache[it.Path] = addr
		}
		side, rng, aerr := planAnchor(ctx, svc, addr, cached, rev, it.Target)
		if aerr != nil {
			return nil, 0, fmt.Errorf("item %d: %w", i, aerr)
		}
		if sideRule == sideRuleNewOnly && side == model.NoteSideOld {
			skipped++
			continue
		}
		if rerr := svc.NoteRangeCheck(ctx, addr, side, rng); rerr != nil {
			return nil, 0, fmt.Errorf("item %d: %w", i, rerr)
		}
		n.Address, n.Side, n.Range = addr, side, rng
		planned = append(planned, plannedNote{Note: n})
	}
	return planned, skipped, nil
}

// planAnchor turns one parsed target into a side and range: an explicit range
// is used as-is, a hunk number resolves through the same patch
// `gg diff --hunks` numbers for this target.
func planAnchor(ctx context.Context, svc *domain.Service, addr model.FileAddress, cached bool, rev string, t notebatch.Target) (model.NoteSide, [2]int, error) {
	switch {
	case t.NewLine != [2]int{0, 0}:
		return model.NoteSideNew, t.NewLine, nil
	case t.OldLine != [2]int{0, 0}:
		return model.NoteSideOld, t.OldLine, nil
	case t.Hunk != 0:
		return svc.HunkRange(ctx, domain.HunkDiffSpec(cached, rev, []string{addr.Path}), addr.Path, t.Hunk)
	}
	return "", [2]int{}, fmt.Errorf("no anchor")
}

// applyNoteBatch writes a planned batch in order and returns the stored notes.
func applyNoteBatch(ctx context.Context, svc *domain.Service, planned []plannedNote) ([]model.Note, error) {
	out := make([]model.Note, 0, len(planned))
	for i, p := range planned {
		var (
			stored model.Note
			err    error
		)
		if p.ReplyTo != "" {
			stored, err = svc.NoteReply(ctx, p.ReplyTo, p.Note)
		} else {
			stored, err = svc.NoteAdd(ctx, p.Note)
		}
		if err != nil {
			return out, fmt.Errorf("item %d: %w", i, err)
		}
		out = append(out, stored)
	}
	return out, nil
}

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

func noteApply(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("note apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	useStdin := fs.Bool("stdin", false, "read the JSON batch from stdin (required)")
	author := fs.String("author", "", "author for items that carry none (default: $GG_AGENT, else agent)")
	asJSON := fs.Bool("json", false, "print the stored notes as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*useStdin {
		fmt.Fprintln(stderr, "usage: gg note apply --stdin [--cached | --rev <commit>] [--author <name>] [--json]")
		return 2
	}
	if strings.TrimSpace(*tf.file) != "" {
		fmt.Fprintln(stderr, "note apply: --file is not used; each batch item names its own path")
		return 2
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
	planned, _, err := planNoteBatch(ctx, svc, batch, *tf.cached, *tf.rev, noteAuthorDefault(*author), sideRuleBoth)
	if err != nil {
		return noteExit(err, stderr)
	}
	stored, err := applyNoteBatch(ctx, svc, planned)
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
