package domain

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/notes"
	"github.com/homeend/gigagit/internal/prcache"
)

const remarkKeyPrefix = "remark:"

// RemarkKey names one review remark as a send item (and in its marker).
func RemarkKey(reviewID, fp string) string { return remarkKeyPrefix + reviewID + ":" + fp }

func parseRemarkKey(key string) (string, string, bool) {
	rest, ok := strings.CutPrefix(key, remarkKeyPrefix)
	if !ok {
		return "", "", false
	}
	i := strings.LastIndexByte(rest, ':')
	if i <= 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

type prLedger struct {
	s  *Service
	pr int
}

func (s *Service) sendLedger(pr int) engine.SendLedger { return prLedger{s: s, pr: pr} }

// editKey applies fn to the stamp of one item: a stored note's Send, or a
// remark's entry on its review note (created when missing).
func (l prLedger) editKey(ctx context.Context, key string, fn func(*model.NoteSend) bool) error {
	st := l.s.notesStore(ctx)
	if st == nil {
		return ErrNotesDisabled
	}
	defer l.s.invalidateNoteCounts()
	if rid, fp, ok := parseRemarkKey(key); ok {
		return st.Edit(rid, func(n *model.Note) error {
			out := slices.Clone(n.RemarkSends)
			i := slices.IndexFunc(out, func(r model.RemarkSend) bool { return r.RemarkFP == fp })
			if i < 0 {
				out, i = append(out, model.RemarkSend{RemarkFP: fp}), len(out)
			}
			if !fn(&out[i].Send) { // false = drop the entry
				out = slices.Delete(out, i, i+1)
			}
			n.RemarkSends = out
			return nil
		})
	}
	return st.Edit(key, func(n *model.Note) error {
		s := model.NoteSend{}
		if n.Send != nil {
			s = *n.Send
		}
		if fn(&s) {
			n.Send = &s
		} else {
			n.Send = nil
		}
		return nil
	})
}

func (l prLedger) Stamp(ctx context.Context, key string, s model.NoteSend) error {
	return l.editKey(ctx, key, func(cur *model.NoteSend) bool {
		// Ids only grow during one send: a later stamp never forgets one.
		if s.Thread == "" {
			s.Thread = cur.Thread
		}
		if s.Comment == "" {
			s.Comment = cur.Comment
		}
		if s.URL == "" {
			s.URL = cur.URL
		}
		s.Err = ""
		*cur = s
		return true
	})
}

func (l prLedger) Fail(ctx context.Context, keys []string, review string, err error) {
	for _, k := range keys {
		_ = l.editKey(ctx, k, func(cur *model.NoteSend) bool {
			joined := review != "" && cur.Review == review && cur.Joined
			*cur = model.NoteSend{PR: l.pr, Review: review, Joined: joined, Err: err.Error(), At: l.s.forgeClock()}
			return true
		})
	}
}

// Settle is write-through and crash recovery at once: one read of the PR,
// whose snapshot store runs the settle pass.
func (l prLedger) Settle(ctx context.Context) error {
	_, err := l.s.PRRevalidate(ctx, l.pr)
	return err
}

// settleSends applies the settle table (plan Task 5) to PR pr's stamped
// items, judging only stamps older than readStart. It runs on EVERY
// PRRevalidate (the TUI heartbeat, the web poll), so the common case —
// nothing of this PR is stamped and no echoed marker names a local note —
// costs no store read: sendIndex answers it from a per-notes-generation memo.
func (s *Service) settleSends(ctx context.Context, pr model.PullRequest, cs []model.ForgeComment, readStart time.Time) {
	idx := s.sendIndex(ctx)
	if !idx.prs[pr.Number] && !idx.echoed(cs) {
		return
	}
	st := s.notesStore(ctx)
	if st == nil {
		return
	}
	all, err := st.LoadAll()
	if err != nil {
		return
	}
	pending := pr.ViewerPendingReview
	submitted := func(c model.ForgeComment) bool { return c.ReviewID == "" || c.ReviewID != pending }
	byComment, byThread, byMarker := map[string]bool{}, map[string]bool{}, map[string]bool{}
	reviewDone := map[string]bool{}
	for _, c := range cs {
		if !submitted(c) {
			continue
		}
		byComment[c.ID] = true
		if c.ThreadID != "" {
			byThread[c.ThreadID] = true
		}
		if c.ReviewID != "" {
			reviewDone[c.ReviewID] = true
		}
		if c.Kind == model.ForgeCommentReview {
			reviewDone[c.ID] = true
		}
		if k, ok := forge.SendMarkerKey(c.Body); ok {
			byMarker[k] = true
		}
	}
	judge := func(key string, sd *model.NoteSend) (sent, keep bool) {
		switch {
		// A thread proves a send only for a REVIEW item (gg created that
		// thread in that review); a standalone reply's thread existed before
		// it, so only its own comment id or marker does.
		case byMarker[key] || (sd != nil && (byComment[sd.Comment] || (sd.Review != "" && byThread[sd.Thread]))):
			return true, false
		case sd == nil:
			return false, true // unstamped and not echoed: plain local
		case sd.Err != "" || sd.PR != pr.Number || !sd.At.Before(readStart):
			return false, true
		case sd.Review != "" && sd.Review == pending:
			return false, true
		}
		return false, false // gone: clear
	}
	changed := false
	groups := map[string]string{} // forge review id → the local group it came from (§1.3)
	for _, n := range all {
		if n.IsReviewNote() || n.IsWorkingReview() {
			for _, r := range n.RemarkSends {
				if r.Send.PR == pr.Number && r.Send.Review != "" {
					groups[r.Send.Review] = "review:" + n.ID
				}
			}
			if n.Send != nil && n.Send.PR == pr.Number && n.Send.Review != "" {
				groups[n.Send.Review] = "review:" + n.ID
			}
			if s.settleReview(ctx, st, n, pr.Number, pending, judge, reviewDone, readStart) {
				changed = true
			}
			continue
		}
		if n.Send == nil && !byMarker[n.ID] {
			continue
		}
		if n.Send != nil && n.Send.PR != pr.Number && !byMarker[n.ID] {
			continue
		}
		sent, keep := judge(n.ID, n.Send)
		switch {
		case sent:
			if n.Send != nil && n.Send.Review != "" {
				groups[n.Send.Review] = GroupMine
			}
			if err := st.Remove(n.ID); err == nil || errors.Is(err, notes.ErrNotFound) {
				changed = true
			}
		case !keep:
			// Judged again on the live record: another process may have
			// stamped it since LoadAll.
			err := st.Edit(n.ID, func(x *model.Note) error {
				if _, keep := judge(x.ID, x.Send); keep || x.Send == nil {
					return errSettleNoChange
				}
				x.Send = nil
				return nil
			})
			changed = changed || err == nil
		}
	}
	if changed {
		s.invalidateNoteCounts()
	}
	s.rememberGroups(ctx, pr.Number, groups, reviewDone)
}

// rememberGroups keeps, for PR n, which local group each SUBMITTED forge
// review came from, so a sent group keeps its colour on GitHub's side
// (spec 2026-10-07 §1.3). Memory + the PR's cache entry.
func (s *Service) rememberGroups(ctx context.Context, n int, groups map[string]string, done map[string]bool) {
	add := map[string]string{}
	for rev, g := range groups {
		if done[rev] {
			add[rev] = g
		}
	}
	if len(add) == 0 {
		return
	}
	s.forgeMu.Lock()
	if s.forgeGroups == nil {
		s.forgeGroups = map[int]map[string]string{}
	}
	if s.forgeGroups[n] == nil {
		s.forgeGroups[n] = map[string]string{}
	}
	for k, v := range add {
		s.forgeGroups[n][k] = v
	}
	s.forgeMu.Unlock()
	s.persistPR(ctx, n, func(e *prcache.Entry) { // takes forgeMu itself: called unlocked
		if e.Groups == nil {
			e.Groups = map[string]string{}
		}
		for k, v := range add {
			e.Groups[k] = v
		}
	})
}

// prGroups is PR n's forge review → local group map (memory, else the disk
// entry, loaded once).
func (s *Service) prGroups(ctx context.Context, n int) map[string]string {
	s.forgeMu.Lock()
	g, ok := s.forgeGroups[n]
	s.forgeMu.Unlock()
	if ok {
		return g
	}
	g = map[string]string{}
	if e, found := s.prCacheEntry(ctx, n); found {
		for k, v := range e.Groups {
			g[k] = v
		}
	}
	s.forgeMu.Lock()
	if s.forgeGroups == nil {
		s.forgeGroups = map[int]map[string]string{}
	}
	if cur, raced := s.forgeGroups[n]; raced {
		g = cur
	} else {
		s.forgeGroups[n] = g
	}
	s.forgeMu.Unlock()
	return g
}

// errSettleNoChange aborts a settle edit with nothing written.
var errSettleNoChange = errors.New("settle: no change")

// settleReview settles one review note: each remark entry, then the note's
// own whole-review stamp (R4: deleted only when every remark has moved). n is
// the LoadAll snapshot that said there is something to settle; the judgment
// itself runs on the live record inside its edit, since another process may
// have stamped it since.
func (s *Service) settleReview(ctx context.Context, st notes.Store, n model.Note, pr int, pending string,
	judge func(string, *model.NoteSend) (bool, bool), reviewDone map[string]bool, readStart time.Time) bool {
	if len(n.RemarkSends) == 0 && n.Send == nil {
		return false
	}
	remove := false
	err := st.Edit(n.ID, func(x *model.Note) error {
		out := make([]model.RemarkSend, 0, len(x.RemarkSends))
		changed := false
		for _, r := range x.RemarkSends {
			if r.Moved || r.Send.PR != pr {
				out = append(out, r)
				continue
			}
			sd := r.Send
			sent, keep := judge(RemarkKey(x.ID, r.RemarkFP), &sd)
			switch {
			case sent:
				r.Moved, changed = true, true
				out = append(out, r)
			case keep:
				out = append(out, r)
			default:
				changed = true // gone: the remark is local again
			}
		}
		whole := x.Send != nil && x.Send.PR == pr && x.Send.Err == "" && x.Send.At.Before(readStart)
		switch {
		case whole && reviewDone[x.Send.Review]:
			// reviewDocOf parses the stored document only: no branch-tip
			// lookup, so the settle pass never takes the repo gate (R12).
			r := reviewDocOf(*x)
			r.RemarkSends = out
			if allRemarksMoved(r) {
				remove = true
				return errSettleNoChange
			}
			x.Send, x.RemarkSends = nil, out
		case whole && x.Send.Review != pending:
			// Its review is neither submitted nor pending: gone. Local again.
			x.Send, x.RemarkSends = nil, out
		case changed:
			x.RemarkSends = out
		default:
			return errSettleNoChange
		}
		return nil
	})
	if remove {
		return st.Remove(n.ID) == nil
	}
	return err == nil
}

// reviewDocOf is a review note's document and send entries, nothing more:
// enough for remark fingerprints, with no git call.
func reviewDocOf(n model.Note) Review {
	r := Review{ID: n.ID, RemarkSends: n.RemarkSends}
	if doc, err := notebatch.ParseReview([]byte(n.Rationale)); err == nil {
		r.Doc = &doc
	}
	return r
}

func allRemarksMoved(r Review) bool {
	for _, fp := range r.remarkFPs() {
		if !r.remarkMoved(fp) {
			return false
		}
	}
	return true
}

// sendIndexT is what settleSends needs to know cheaply: which PRs have a
// stamped item, and which local ids (notes, reviews) exist to be echoed.
type sendIndexT struct {
	prs map[int]bool
	ids map[string]bool
}

// echoed reports a comment whose marker names a local note or a remark of
// a local review.
func (x sendIndexT) echoed(cs []model.ForgeComment) bool {
	for _, c := range cs {
		k, ok := forge.SendMarkerKey(c.Body)
		if !ok {
			continue
		}
		if rid, _, isRemark := parseRemarkKey(k); isRemark {
			k = rid
		}
		if x.ids[k] {
			return true
		}
	}
	return false
}

// sendIndex is built once per notes generation (one LoadAll per notes
// change, never per poll); Stamp/Fail invalidate it through
// invalidateNoteCounts.
func (s *Service) sendIndex(ctx context.Context) sendIndexT {
	s.mu.Lock()
	if s.sendIdx != nil && s.sendIdxGen == s.notesGen {
		x := *s.sendIdx
		s.mu.Unlock()
		return x
	}
	gen := s.notesGen
	s.mu.Unlock()
	x := sendIndexT{prs: map[int]bool{}, ids: map[string]bool{}}
	if st := s.notesStore(ctx); st != nil {
		if all, err := st.LoadAll(); err == nil {
			for _, n := range all {
				x.ids[n.ID] = true
				if n.Send != nil && n.Send.Err == "" {
					x.prs[n.Send.PR] = true
				}
				for _, r := range n.RemarkSends {
					if !r.Moved && r.Send.Err == "" {
						x.prs[r.Send.PR] = true
					}
				}
			}
		}
	}
	s.mu.Lock()
	if s.notesGen == gen {
		s.sendIdx, s.sendIdxGen = &x, gen
	}
	s.mu.Unlock()
	return x
}

// PRInterrupted is what an interrupted send left on GitHub: the pending
// review gg's stamps name (it must be the PR's ViewerPendingReview), the
// keys waiting in it, and whether gg JOINED it (then it is the user's own
// draft: finish only, never discard); "" when nothing of gg's is pending.
func (s *Service) PRInterrupted(ctx context.Context, n int) (string, []string, bool) {
	p, _, ok := s.PRDetailsCached(n)
	if !ok || p.ViewerPendingReview == "" {
		return "", nil, false
	}
	st := s.notesStore(ctx)
	if st == nil {
		return "", nil, false
	}
	all, err := st.LoadAll()
	if err != nil {
		return "", nil, false
	}
	// Failed stamps count too: a failure whose delete also failed keeps the
	// review's id (the review may still hold gg's threads).
	var keys []string
	joined := false
	for _, x := range all {
		if x.Send != nil && x.Send.PR == n && x.Send.Review == p.ViewerPendingReview {
			keys = append(keys, x.ID)
			joined = joined || x.Send.Joined
		}
		for _, r := range x.RemarkSends {
			if !r.Moved && r.Send.PR == n && r.Send.Review == p.ViewerPendingReview {
				keys = append(keys, RemarkKey(x.ID, r.RemarkFP))
				joined = joined || r.Send.Joined
			}
		}
	}
	if len(keys) == 0 {
		return "", nil, false
	}
	return p.ViewerPendingReview, keys, joined
}
