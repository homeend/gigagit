package domain

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

// Sending to a forge (spec 2026-10-07 §3): the frontend collects a
// PRSendRequest, PRSendOp turns it into a SendPlan — re-read, settled,
// re-anchored, rendered — and hands it to engine.SendToForge, which
// confirms and writes.

var (
	ErrPRHeadMoved       = errors.New("the pull request has new commits on GitHub: fetch them first (gg pr fetch)")
	ErrForgeReadOnly     = errors.New("this forge cannot be written to from gg")
	ErrNoInterruptedSend = errors.New("no interrupted send: gg has nothing waiting in a pending review")
	ErrMixedSend         = errors.New("send new comments and replies / resolves separately")
	ErrSendRequest       = errors.New("send request")
	// ErrInterruptedPending: a send of gg's is still pending on the forge
	// (interrupted, or its delete failed) — a fresh review would add its
	// threads to that one.
	ErrInterruptedPending = errors.New("an earlier send is still pending on GitHub: finish it (gg pr send <n> --finish) or discard it (--discard) first")
	// ErrDiscardJoined: the pending review is the user's own draft gg added
	// to; gg never deletes it.
	ErrDiscardJoined = errors.New("the pending review is your own (gg added to it): finish it (--finish), or discard it on GitHub")
	// ErrNoteOffPR: a note written in a PR's view on a commit the PR no
	// longer holds (its head was force-pushed under the open view).
	ErrNoteOffPR = errors.New("the pull request no longer holds this commit")
)

// PRSendRequest is everything a frontend collects before a send starts
// (decisions are option lists only: no free text mid-flight).
type PRSendRequest struct {
	PR        int      `toml:"pr" json:"pr"`
	Review    string   `toml:"review,omitempty" json:"review,omitempty"`       // a stored review: every unsent remark + its summary
	Mine      bool     `toml:"mine,omitempty" json:"mine,omitempty"`           // every local root note the PR shows
	Notes     []string `toml:"notes,omitempty" json:"notes,omitempty"`         // note ids, remark ids (review:<id>:<n>), draft replies
	Resolve   []string `toml:"resolve,omitempty" json:"resolve,omitempty"`     // thread ids or forge comment ids
	Unresolve []string `toml:"unresolve,omitempty" json:"unresolve,omitempty"` // thread ids or forge comment ids
	Verdict   bool     `toml:"verdict,omitempty" json:"verdict,omitempty"`     // a verdict with no comments
	Body      string   `toml:"body,omitempty" json:"body,omitempty"`
	// BodySet: the user answered the body box, so Body is used as is — an
	// emptied box posts no body (user ruling 2026-10-08), never the stored
	// summary.
	BodySet bool `toml:"body_set,omitempty" json:"body_set,omitempty"`
	Finish  bool `toml:"finish,omitempty" json:"finish,omitempty"`
	Discard bool `toml:"discard,omitempty" json:"discard,omitempty"`
}

// PRSendOp builds the one op that writes to a forge. The frontend collected
// every input already; building the op re-reads the PR, settles, re-anchors
// and renders the bodies (R12: outside the gate, right before Execute); the
// op confirms and writes.
func (s *Service) PRSendOp(ctx context.Context, req PRSendRequest) (engine.SendToForge, error) {
	p, err := s.provider(ctx)
	if err != nil {
		return engine.SendToForge{}, err
	}
	w, ok := p.(forge.Writer)
	if !ok {
		return engine.SendToForge{}, ErrForgeReadOnly
	}
	plan, err := s.planSend(ctx, req) // outside the gate (R12): every read here reserves it
	if err != nil {
		return engine.SendToForge{}, err
	}
	return engine.SendToForge{
		Plan:   plan,
		Writer: w,
		Ledger: s.sendLedger(req.PR),
		Now:    s.forgeClock,
	}, nil
}

func (s *Service) planSend(ctx context.Context, req PRSendRequest) (engine.SendPlan, error) {
	req.Notes, req.Resolve, req.Unresolve = uniqIDs(req.Notes), uniqIDs(req.Resolve), uniqIDs(req.Unresolve)
	rv, err := s.PRRevalidate(ctx, req.PR) // also settles what earlier sends left
	if err != nil {
		return engine.SendPlan{}, err
	}
	if rv.Moved {
		return engine.SendPlan{}, ErrPRHeadMoved
	}
	pr := rv.PR
	plan := engine.SendPlan{Target: s.prTarget(ctx, pr.Number), PR: pr.Number, PRID: pr.NodeID, Head: pr.HeadSHA,
		OwnPR: pr.ViewerDidAuthor, Pending: pr.ViewerPendingReview, Body: req.Body}
	switch {
	case req.Finish || req.Discard:
		rev, keys, joined := s.PRInterrupted(ctx, pr.Number)
		if rev == "" {
			return engine.SendPlan{}, ErrNoInterruptedSend
		}
		if req.Discard && joined {
			return engine.SendPlan{}, ErrDiscardJoined
		}
		plan.Mode, plan.Pending, plan.Body = engine.SendFinish, rev, ""
		if req.Discard {
			plan.Mode = engine.SendDiscard
		}
		names := s.interruptedNames(ctx, keys)
		for _, k := range keys {
			plan.Items = append(plan.Items, engine.SendItem{Key: k, Summary: names[k],
				Label: names[k] + " (waiting in the pending review)"})
		}
		return plan, nil
	}
	drafts, other, err := s.noteKinds(ctx, req.Notes)
	if err != nil {
		return engine.SendPlan{}, err
	}
	if len(req.Resolve)+len(req.Unresolve) > 0 || drafts > 0 {
		if req.Review != "" || req.Mine || req.Verdict || other > 0 {
			return engine.SendPlan{}, ErrMixedSend
		}
		return s.planActions(ctx, plan, req)
	}
	if rev, _, _ := s.PRInterrupted(ctx, pr.Number); rev != "" {
		return engine.SendPlan{}, ErrInterruptedPending
	}
	plan, err = s.planReview(ctx, plan, pr, req)
	if err == nil && len(plan.Items) == 0 && !req.Verdict {
		// Nothing sendable: posting an empty review would be a public write
		// nobody asked for.
		var why []string
		for _, sk := range plan.Skipped {
			why = append(why, sk.Label+": "+sk.Reason)
		}
		return engine.SendPlan{}, fmt.Errorf("%w (%s)", engine.ErrNothingToSend, strings.Join(why, "; "))
	}
	return plan, err
}

// uniqIDs drops repeated ids, keeping each first occurrence in place: a
// note named twice is one thread.
func uniqIDs(ids []string) []string {
	if len(ids) < 2 {
		return ids
	}
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// interruptedNames is what each ledger key of an interrupted send says to a
// person: a note's summary or a remark's summary; the key itself when
// nothing better is stored. (A summary mark is moved, so PRInterrupted
// never lists one.)
func (s *Service) interruptedNames(ctx context.Context, keys []string) map[string]string {
	out := make(map[string]string, len(keys))
	byID, _ := s.storedNotes(ctx) // nil on error: every key falls back to itself
	for _, k := range keys {
		out[k] = k
		if rid, fp, ok := parseRemarkKey(k); ok {
			if r, err := s.Review(ctx, rid); err == nil {
				for _, d := range r.docRemarks() {
					if d.fp == fp {
						out[k] = cutLabel(d.summary)
					}
				}
			}
			continue
		}
		if n, ok := byID[k]; ok && n.Summary != "" {
			out[k] = cutLabel(n.Summary)
		}
	}
	return out
}

// prTarget names the PR for the confirm: "owner/repo #n".
func (s *Service) prTarget(ctx context.Context, n int) string {
	p, err := s.provider(ctx)
	if err != nil {
		return fmt.Sprintf("#%d", n)
	}
	slug, _, err := s.baseRepo(ctx, p)
	if err != nil || slug == "" {
		return fmt.Sprintf("#%d", n)
	}
	return fmt.Sprintf("%s #%d", strings.TrimPrefix(slug, "github.com/"), n)
}

// storedNotes is every stored note by id (one store read).
func (s *Service) storedNotes(ctx context.Context) (map[string]model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, ErrNotesDisabled
	}
	all, err := st.LoadAll()
	if err != nil {
		return nil, err
	}
	out := make(map[string]model.Note, len(all))
	for _, n := range all {
		out[n.ID] = n
	}
	return out, nil
}

// noteKinds counts a --note list's draft replies to GitHub threads and its
// other items (local notes, remarks, GitHub ids); an id no longer stored is
// neither — a reply/resolve send skips it, any other send names it. A store
// that cannot be read is an error, never a guess.
func (s *Service) noteKinds(ctx context.Context, ids []string) (drafts, other int, err error) {
	if len(ids) == 0 {
		return 0, 0, nil
	}
	byID, err := s.storedNotes(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, id := range ids {
		n, ok := byID[id]
		switch {
		case ok && n.IsForgeReply():
			drafts++
		case ok || model.IsReviewNoteID(id) || model.IsForgeNoteID(id):
			other++
		}
	}
	return drafts, other, nil
}

// planActions is replies and resolves: each its own call.
func (s *Service) planActions(ctx context.Context, plan engine.SendPlan, req PRSendRequest) (engine.SendPlan, error) {
	plan.Mode = engine.SendActions
	if len(req.Notes) > 0 {
		byID, err := s.storedNotes(ctx)
		if err != nil {
			return engine.SendPlan{}, err
		}
		for _, id := range req.Notes {
			d, ok := byID[id]
			if !ok { // deleted since the request was made (a queued send)
				plan.Skipped = append(plan.Skipped, engine.SendSkip{Label: "reply: " + id, Reason: SkipGone, Summary: id})
				continue
			}
			label := "reply: " + cutLabel(d.Summary)
			_, root, ok := s.forgeCommentByID(strings.TrimPrefix(d.ParentID, model.ForgeNoteIDPrefix))
			switch {
			case !ok || root.ThreadID == "":
				plan.Skipped = append(plan.Skipped, engine.SendSkip{Label: label, Reason: SkipThreadNotInPR, Summary: cutLabel(d.Summary)})
				continue
			case d.Send.State() == model.SyncSending:
				plan.Skipped = append(plan.Skipped, engine.SendSkip{Label: label, Reason: SkipBeingSent, Summary: cutLabel(d.Summary)})
				continue
			}
			plan.Items = append(plan.Items, engine.SendItem{Key: d.ID, Label: label, Kind: engine.SendReply,
				ThreadID: root.ThreadID, Body: sendBody(d, d.ID, ""), Summary: cutLabel(d.Summary)})
		}
	}
	for _, x := range []struct {
		ids  []string
		kind engine.SendKind
		verb string
	}{{req.Resolve, engine.SendResolve, "resolve "}, {req.Unresolve, engine.SendUnresolve, "unresolve "}} {
		done := map[string]bool{}
		for _, id := range x.ids {
			th, err := s.threadIDFor(ctx, plan.PR, id)
			if err != nil {
				return engine.SendPlan{}, err
			}
			if done[th] { // a thread named by its id and by one of its comments
				continue
			}
			done[th] = true
			plan.Items = append(plan.Items, engine.SendItem{Label: x.verb + th, Kind: x.kind, ThreadID: th})
		}
	}
	return plan, nil
}

// threadIDFor maps a thread id, one of its comment ids, or "forge:<id>" to
// PR n's thread id.
func (s *Service) threadIDFor(ctx context.Context, n int, id string) (string, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), model.ForgeNoteIDPrefix)
	cs, err := s.prCommentsNow(ctx, n)
	if err != nil {
		return "", err
	}
	for _, bucket := range [][]model.ForgeComment{cs.Inline, cs.Outdated} {
		for _, c := range bucket {
			if c.ThreadID != "" && (c.ThreadID == id || c.ID == id) {
				return c.ThreadID, nil
			}
		}
	}
	return "", fmt.Errorf("%w: %s is not a thread of #%d", ErrSendRequest, id, n)
}

// sendPlace is what anchoring needs about the PR's diff: its hunks at
// GitHub's 3-line context, the paths it changes, the lines at its head and
// at its merge base (read once per path).
type sendPlace struct {
	hunks      map[string][]model.Hunk
	changed    map[string]bool
	head, base map[string][]string
	read       func(rev, path string) []string
	tip, mbase string
}

func (pl *sendPlace) lines(old bool, path string) []string {
	m, rev := pl.head, pl.tip
	if old {
		m, rev = pl.base, pl.mbase
	}
	l, ok := m[path]
	if !ok {
		l = pl.read(rev, path)
		m[path] = l
	}
	return l
}

func (s *Service) sendPlacer(ctx context.Context, set PreviewNoteSet, files []model.CommitFile) (*sendPlace, error) {
	fh, err := s.DiffHunks(ctx, model.DiffSpec{Rev: set.Base + ".." + set.Tip, Unified: 3})
	if err != nil {
		return nil, err
	}
	pl := &sendPlace{hunks: map[string][]model.Hunk{}, changed: map[string]bool{},
		head: map[string][]string{}, base: map[string][]string{}, tip: set.Tip, mbase: set.Base,
		read: func(rev, path string) []string { return s.revLines(ctx, rev, path) }}
	for _, f := range fh {
		pl.hunks[f.Path], pl.changed[f.Path] = f.Hunks, true
	}
	for _, f := range files {
		if f.Status != "D" {
			pl.changed[f.Path] = true
		}
	}
	return pl, nil
}

// planReview is one review: single notes / remarks, a whole stored review,
// "my draft review", or a verdict alone.
func (s *Service) planReview(ctx context.Context, plan engine.SendPlan, pr model.PullRequest, req PRSendRequest) (engine.SendPlan, error) {
	plan.Mode = engine.SendReview
	if len(req.Notes) == 0 && req.Review == "" && !req.Mine {
		// A verdict alone needs no diff here.
		plan.Verdict, plan.Body = true, typedBody(req)
		return plan, nil
	}
	prev, err := s.PRPreview(ctx, pr)
	if err != nil {
		return engine.SendPlan{}, err
	}
	if prev.Endpoints.Summary.State != PreviewOK {
		return engine.SendPlan{}, fmt.Errorf("%w: #%d's diff is not available here (gg pr fetch %d)", ErrSendRequest, pr.Number, pr.Number)
	}
	pl, err := s.sendPlacer(ctx, prev.Set, prev.Files)
	if err != nil {
		return engine.SendPlan{}, err
	}
	shown, err := s.PreviewNotesAll(ctx, prev.Set)
	if err != nil {
		return engine.SendPlan{}, err
	}
	roots := map[string]ResolvedNote{}
	for _, rs := range shown {
		for _, r := range rs {
			roots[r.Note.ID] = r
		}
	}
	switch {
	case req.Review != "":
		r, err := s.Review(ctx, req.Review)
		if err != nil {
			return engine.SendPlan{}, err
		}
		if !s.prOwnsReview(ctx, prev.Set, r.ID) {
			return engine.SendPlan{}, fmt.Errorf("%w: review %s is not in this PR", ErrSendRequest, r.ID)
		}
		plan.Key, plan.Body, plan.Verdict = r.ID, reviewSendBody(r), true
		edited := strings.TrimSpace(req.Body)
		cleared := req.BodySet && edited == ""
		switch {
		case cleared:
			plan.Body = "" // the user cleared it: no body, no trailer, no marker
		case edited != "": // the user edited the summary (plan 3, T7)
			plan.Body = sendBody(model.Note{Source: model.NoteSourceAgent, Author: r.Agent, Summary: edited}, r.ID, "")
		}
		// The stored summary is on GitHub already: skip it — unless the user
		// typed a body of their own, which is new text and goes.
		if !cleared && r.summarySent(pr.Number) && (edited == "" || edited == r.summaryText()) {
			plan.Body = ""
			plan.Skipped = append(plan.Skipped, engine.SendSkip{Label: "review summary", Reason: SkipOnGitHub})
		}
		for i := range r.docRemarks() {
			s.remarkItem(ctx, &plan, r, i, pl)
		}
	case req.Mine:
		plan.Verdict, plan.Body = true, typedBody(req)
		for _, p := range PreviewNotePaths(shown) {
			for _, r := range shown[p] {
				if r.Group == GroupMine && r.Note.Source != model.NoteSourceForge && !r.Note.IsForgeReply() {
					noteItem(&plan, r, pl)
				}
			}
		}
	default:
		var byID map[string]model.Note
		for _, id := range req.Notes {
			switch {
			case model.IsForgeNoteID(id):
				return engine.SendPlan{}, fmt.Errorf("%w: %s is a GitHub comment: answer it with gg pr reply", ErrSendRequest, id)
			case model.IsReviewNoteID(id):
				rid, n, _ := model.ParseReviewNoteID(id)
				r, err := s.Review(ctx, rid)
				if err != nil {
					return engine.SendPlan{}, err
				}
				if !s.prOwnsReview(ctx, prev.Set, r.ID) {
					return engine.SendPlan{}, fmt.Errorf("%w: review %s is not in this PR", ErrSendRequest, r.ID)
				}
				if n >= len(r.docRemarks()) {
					return engine.SendPlan{}, fmt.Errorf("%w: %s", ErrNoSuchRemark, id)
				}
				s.remarkItem(ctx, &plan, r, n, pl)
			default:
				r, ok := roots[id]
				if !ok {
					if byID == nil {
						if byID, err = s.storedNotes(ctx); err != nil {
							return engine.SendPlan{}, err
						}
					}
					n, stored := byID[id]
					if !stored {
						return engine.SendPlan{}, fmt.Errorf("%w: no local note %s", ErrSendRequest, id)
					}
					if n.IsReply() && !n.IsForgeReply() {
						return engine.SendPlan{}, fmt.Errorf("%w: %s is a reply: send its thread's root", ErrSendRequest, id)
					}
					plan.Skipped = append(plan.Skipped, engine.SendSkip{Label: noteLabel(n.Address.Path, n.Range[0], n.Summary), Reason: SkipNotInPR,
						Path: n.Address.Path, Line: n.Range[0], Summary: cutLabel(n.Summary)})
					continue
				}
				noteItem(&plan, r, pl)
			}
		}
	}
	return plan, nil
}

// noteItem turns one stored root the PR shows into a thread (or a skip).
func noteItem(plan *engine.SendPlan, r ResolvedNote, pl *sendPlace) {
	n := r.Note
	path := n.Address.Path
	skip := func(reason string) {
		plan.Skipped = append(plan.Skipped, engine.SendSkip{Label: noteLabel(path, r.Range[0], n.Summary), Reason: reason,
			Path: path, Line: r.Range[0], Summary: cutLabel(n.Summary)})
	}
	switch {
	case r.Sync == model.SyncSending:
		skip(SkipBeingSent)
		return
	case !pl.changed[path]:
		skip(SkipNotInPR)
		return
	case r.Status != model.NoteActive:
		skip(SkipLinesChanged)
		return
	}
	text := anchorLines(pl.lines(false, path), r.Range)
	th := threadFor(path, model.NoteSideNew, r.Range, text, pl.hunks[path],
		func(q string) string { return sendBody(n, n.ID, q) })
	it := engine.SendItem{Key: n.ID, Label: threadLabel(th, r.Range[0], n.Summary), Kind: engine.SendThread,
		Thread: th, Resolve: r.Resolution != nil, Summary: cutLabel(n.Summary)}
	for _, rep := range r.Replies {
		if rep.Note.Source == model.NoteSourceForge {
			continue
		}
		it.Replies = append(it.Replies, engine.SendReplyBody{Key: rep.Note.ID, Body: sendBody(rep.Note, rep.Note.ID, "")})
	}
	plan.Items = append(plan.Items, it)
}

// remarkItem turns remark i of review r into a thread (or a skip): its
// lines' text is re-found by hash in the PR head (new side) or the merge
// base (old side). A moved remark is skipped silently — GitHub has it.
func (s *Service) remarkItem(ctx context.Context, plan *engine.SendPlan, r Review, i int, pl *sendPlace) {
	if r.Doc == nil {
		return
	}
	var file notebatch.ReviewFile
	var dn notebatch.ReviewNote
	k, found := 0, false
	for _, f := range r.Doc.Files {
		for _, d := range f.Notes {
			if k == i {
				file, dn, found = f, d, true
			}
			k++
		}
	}
	if !found {
		return
	}
	side := reviewSide(dn.Side)
	path := reviewPath(file.Path)
	fp := remarkFP(file.Path, side, dn.Range, dn.Summary)
	if r.remarkMoved(fp) {
		return
	}
	skip := func(reason string) {
		plan.Skipped = append(plan.Skipped, engine.SendSkip{Label: noteLabel(path, dn.Range[0], dn.Summary), Reason: reason,
			Path: path, Line: dn.Range[0], Summary: cutLabel(dn.Summary)})
	}
	if r.remarkSend(fp).State() == model.SyncSending {
		skip(SkipBeingSent)
		return
	}
	if !pl.changed[path] {
		skip(SkipNotInPR)
		return
	}
	rng, want, ok := s.remarkPlace(ctx, r, path, side, dn.Range, pl.lines(side == model.NoteSideOld, path))
	if !ok {
		skip(SkipLinesChanged)
		return
	}
	note := model.Note{Source: model.NoteSourceAgent, Author: r.Agent, Summary: dn.Summary, Rationale: dn.Rationale}
	for _, kv := range dn.Meta {
		note.Tags = append(note.Tags, kv.Key+": "+kv.Value)
	}
	key := RemarkKey(r.ID, fp)
	th := threadFor(path, side, rng, want, pl.hunks[path], func(q string) string { return sendBody(note, key, q) })
	it := engine.SendItem{Key: key, Label: threadLabel(th, rng[0], dn.Summary), Kind: engine.SendThread, Thread: th,
		Summary: cutLabel(dn.Summary)}
	if threads, _ := r.RemarkThreads(); i < len(threads) {
		it.Resolve = threads[i].Resolution != nil
		for _, rep := range threads[i].Replies {
			it.Replies = append(it.Replies, engine.SendReplyBody{Key: rep.ID, Body: sendBody(rep, rep.ID, "")})
		}
	}
	plan.Items = append(plan.Items, it)
}

// remarkPlace re-finds a remark's lines in target (§3.2): the text is read
// where the review read it (the merge base for the old side, the review's
// worktree or tip for the new) and found by its hash. ok false = its lines
// changed. Shared by the send planner and the PR view (plan 3, T1).
func (s *Service) remarkPlace(ctx context.Context, r Review, path string, side model.NoteSide, rng [2]int, target []string) ([2]int, []string, bool) {
	base, tip, _ := s.reviewRevs(ctx, r)
	var src []string
	switch {
	case side == model.NoteSideOld:
		src = s.revLines(ctx, base, path)
	case r.Kind == ReviewOnWorktree:
		src = s.worktreeLines(ctx, r.Worktree, path)
	default:
		src = s.revLines(ctx, tip, path)
	}
	span := rng[1] - rng[0] + 1
	want := anchorLines(src, rng)
	if span < 1 || len(want) != span {
		return [2]int{}, nil, false
	}
	start := findAnchor(target, model.NoteContextHash(want), span, rng[0])
	if start == 0 {
		return [2]int{}, nil, false
	}
	return [2]int{start, start + span - 1}, want, true
}

// placeThread reports whether rng lies inside ONE hunk on side — the lines
// GitHub accepts a line comment on.
func placeThread(hunks []model.Hunk, side model.NoteSide, rng [2]int) bool {
	for _, h := range hunks {
		r := h.New
		if side == model.NoteSideOld {
			r = h.Old
		}
		if r != [2]int{0, 0} && rng[0] >= r[0] && rng[1] <= r[1] {
			return true
		}
	}
	return false
}

// threadFor turns an anchored range into a forge thread: a line thread in
// the diff, a file-level one quoting the lines outside it.
func threadFor(path string, side model.NoteSide, rng [2]int, text []string, hunks []model.Hunk,
	body func(quote string) string) forge.Thread {
	if placeThread(hunks, side, rng) {
		return forge.Thread{Path: path, Line: rng[1], StartLine: rng[0], Side: side, Body: body("")}
	}
	return forge.Thread{Path: path, Body: body(quoteLines(rng[0], text))}
}

func threadLabel(th forge.Thread, line int, summary string) string {
	if th.Line == 0 {
		return fmt.Sprintf("%s (file) %s", th.Path, cutLabel(summary))
	}
	return noteLabel(th.Path, line, summary)
}

func noteLabel(path string, line int, summary string) string {
	return fmt.Sprintf("%s:%d %s", path, line, cutLabel(summary))
}

// cutLabel keeps a confirm line short: the first 60 runes of a summary.
func cutLabel(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > 60 {
		return string(r[:59]) + "…"
	}
	return string(r)
}

// typedBody is the review body the user typed (agents never send, so it
// is never signed).
func typedBody(req PRSendRequest) string { return strings.TrimSpace(req.Body) }

// PRThreadRoot names a thread of PR n by any of its handles — a thread id,
// one of its comment ids, or "forge:<comment id>" — reading the PR's
// comments when they are not cached yet.
func (s *Service) PRThreadRoot(ctx context.Context, n int, id string) (string, string, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), model.ForgeNoteIDPrefix)
	// The PR too, not only its comments: a draft reply is addressed at its
	// head (forgeReplyDraft reads PRDetailsCached).
	if _, err := s.PullRequest(ctx, n); err != nil {
		return "", "", err
	}
	cs, err := s.prCommentsNow(ctx, n)
	if err != nil {
		return "", "", err
	}
	for _, bucket := range [][]model.ForgeComment{cs.Inline, cs.Outdated} {
		for _, c := range bucket {
			if (c.ID == id || c.ThreadID == id) && c.ParentID == "" {
				return c.ID, c.ThreadID, nil
			}
			if c.ID == id { // a reply: its root shares its thread
				for _, r := range bucket {
					if r.ThreadID == c.ThreadID && r.ParentID == "" {
						return r.ID, r.ThreadID, nil
					}
				}
			}
		}
	}
	return "", "", fmt.Errorf("%w: %s is not a thread of #%d", ErrSendRequest, id, n)
}

// PRNotes is everything PR n's view holds, by path: the local notes written
// for it, its AI reviews' remarks, GitHub threads, draft replies — each with
// its sync state. The PR's diff must be available here (gg pr fetch).
func (s *Service) PRNotes(ctx context.Context, n int) (map[string][]ResolvedNote, error) {
	pr, err := s.PullRequest(ctx, n)
	if err != nil {
		return nil, err
	}
	prev, err := s.PRPreview(ctx, pr)
	if err != nil {
		return nil, err
	}
	if !prev.Set.OK() {
		return nil, fmt.Errorf("%w: #%d's diff is not available here (gg pr fetch %d)", ErrSendRequest, n, n)
	}
	if _, err := s.prCommentsNow(ctx, n); err != nil {
		return nil, err
	}
	return s.PreviewNotesAll(ctx, prev.Set)
}

// PRNoteScope is the scope a note written in PR pr's view records
// ("<base>...refs/gg/pr/<n>", spec 2026-10-08 §3): any commit of the PR's
// range takes it (the view may show an older tip than the forge's); "" when
// the PR's diff is not available here. A commit the PR no longer holds (its
// head was force-pushed under the open view) is ErrNoteOffPR — never a plain
// note, which would silently leave the PR. pr is the caller's cached row:
// no forge read.
func (s *Service) PRNoteScope(ctx context.Context, pr model.PullRequest, commit string) (string, error) {
	prev, err := s.PRPreview(ctx, pr)
	if err != nil || !prev.Set.OK() {
		return "", nil
	}
	if !slices.Contains(prev.Set.Commits, commit) {
		return "", fmt.Errorf("%w (%s) — reopen pull request #%d", ErrNoteOffPR, shortSHA(commit), pr.Number)
	}
	return prev.Set.Pair(), nil
}

// prCommentsNow is PR n's comments from the cache, else from ONE snapshot
// read (PRRevalidate also caches the PR and settles any stamps).
func (s *Service) prCommentsNow(ctx context.Context, n int) (PRComments, error) {
	if cs, ok := s.PRCommentsCached(n); ok {
		return cs, nil
	}
	if _, err := s.PRRevalidate(ctx, n); err != nil {
		return PRComments{}, err
	}
	cs, _ := s.PRCommentsCached(n)
	return cs, nil
}
