package domain

// The shared agent-batch import planner/applier — moved here from
// internal/cli/noteapply.go so `gg note apply --stdin`, `gg review --notes`
// and the MCP gg_notes_apply tool validate and write batches through the ONE
// path (spec §4.5: MCP must use "the same validator as the CLI,
// all-or-nothing"). internal/cli and internal/mcp cannot import each other,
// so the shared code lives in domain, which both already depend on.
//
// The contract is ALL-OR-NOTHING: every item is parsed, its address resolved
// and its anchor computed AND VALIDATED before the first write. A batch with
// one bad item stores nothing. Range validation happens in PlanNoteBatch
// itself (via NoteRangeCheck, a read-only precheck) — not by writing and
// rolling back — so a batch whose third item names a line past the end of
// the file never stores items one and two either.

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

// NoteSideRule says which diff sides a batch import target can address. A
// single commit has both (parent → commit); a range review or a working
// review has an old side that is not a note-addressable base (§4.4's old-side
// table), so old-side items are skipped with one warning.
type NoteSideRule int

const (
	NoteSideBoth    NoteSideRule = iota // a commit target: both sides are addressable
	NoteSideNewOnly                     // a range or working review: only the new side is
)

// PlannedNote is one validated, fully anchored note waiting to be written.
type PlannedNote struct {
	Note    model.Note // ready to store; zero ParentID for a root
	ReplyTo string     // non-empty makes it a reply
}

// PlanNoteBatch resolves every item to a storable note. cached/rev choose the
// target diff for the WHOLE batch; author fills in for items that carry none.
// Every batch note is Source: agent.
//
// skipped counts old-side items dropped by NoteSideNewOnly — the caller warns
// once rather than per item.
//
// Every item's range is validated against its side's real length (via
// NoteRangeCheck) BEFORE it is appended to planned, so a bad range anywhere
// in the batch fails here — before ApplyNoteBatch performs its first write.
func (s *Service) PlanNoteBatch(ctx context.Context, b notebatch.Batch, cached bool, rev, author string, rule NoteSideRule) (planned []PlannedNote, skipped int, err error) {
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
			if _, gerr := s.NoteGet(ctx, it.ReplyTo); gerr != nil {
				return nil, 0, fmt.Errorf("item %d: replyTo %s: %w", i, it.ReplyTo, gerr)
			}
			planned = append(planned, PlannedNote{Note: n, ReplyTo: it.ReplyTo})
			continue
		}
		addr, ok := addrCache[it.Path]
		if !ok {
			addr, err = s.NoteTarget(ctx, it.Path, cached, rev)
			if err != nil {
				return nil, 0, fmt.Errorf("item %d: %w", i, err)
			}
			addrCache[it.Path] = addr
		}
		side, rng, aerr := s.planNoteBatchAnchor(ctx, addr, cached, rev, it.Target)
		if aerr != nil {
			return nil, 0, fmt.Errorf("item %d: %w", i, aerr)
		}
		if rule == NoteSideNewOnly && side == model.NoteSideOld {
			skipped++
			continue
		}
		if rerr := s.NoteRangeCheck(ctx, addr, side, rng); rerr != nil {
			return nil, 0, fmt.Errorf("item %d: %w", i, rerr)
		}
		n.Address, n.Side, n.Range = addr, side, rng
		planned = append(planned, PlannedNote{Note: n})
	}
	return planned, skipped, nil
}

// planNoteBatchAnchor turns one parsed target into a side and range: an
// explicit range is used as-is, a hunk number resolves through the same
// patch `gg diff --hunks` numbers for this target.
func (s *Service) planNoteBatchAnchor(ctx context.Context, addr model.FileAddress, cached bool, rev string, t notebatch.Target) (model.NoteSide, [2]int, error) {
	switch {
	case t.NewLine != [2]int{0, 0}:
		return model.NoteSideNew, t.NewLine, nil
	case t.OldLine != [2]int{0, 0}:
		return model.NoteSideOld, t.OldLine, nil
	case t.Hunk != 0:
		spec, err := s.HunkDiffSpec(ctx, cached, rev, []string{addr.Path})
		if err != nil {
			return "", [2]int{}, err
		}
		return s.HunkRange(ctx, spec, addr.Path, t.Hunk)
	}
	return "", [2]int{}, fmt.Errorf("no anchor")
}

// ApplyNoteBatch writes a planned batch in order and returns the stored notes.
//
// PlanNoteBatch validates up front, but a write can still fail mid-batch — the
// classic case is a replyTo target removed by someone else between planning
// and applying. On that failure ApplyNoteBatch rolls back everything IT
// already stored (best-effort: an individual removal failure is counted, not
// fatal) so the batch stays all-or-nothing even when the "nothing" only
// becomes true after a brief moment where it wasn't.
func (s *Service) ApplyNoteBatch(ctx context.Context, planned []PlannedNote) ([]model.Note, error) {
	out := make([]model.Note, 0, len(planned))
	for i, p := range planned {
		var (
			stored model.Note
			err    error
		)
		if p.ReplyTo != "" {
			stored, err = s.NoteReply(ctx, p.ReplyTo, p.Note)
		} else {
			stored, err = s.NoteAdd(ctx, p.Note)
		}
		if err != nil {
			removed, failed := s.rollbackNoteBatch(ctx, out)
			if failed > 0 {
				return nil, fmt.Errorf("item %d: %w (rollback incomplete: %d could not be removed)", i, err, failed)
			}
			return nil, fmt.Errorf("item %d: %w (rolled back %d notes)", i, err, removed)
		}
		out = append(out, stored)
	}
	return out, nil
}

// rollbackNoteBatch is ApplyNoteBatch's best-effort undo: it removes what THIS
// call already stored, most-recent first (a reply before its root, so a root
// is never asked to remove a batch reply that has already been removed
// separately). An individual NoteRemove failure is counted, not fatal to the
// rest of the rollback.
func (s *Service) rollbackNoteBatch(ctx context.Context, stored []model.Note) (removed, failed int) {
	for i := len(stored) - 1; i >= 0; i-- {
		if err := s.NoteRemove(ctx, stored[i].ID); err != nil {
			failed++
			continue
		}
		removed++
	}
	return removed, failed
}
