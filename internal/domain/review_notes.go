package domain

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/filelock"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/notes"
)

// AI reviews live in the note store as commit-level notes tagged "review"
// (docs/superpowers/specs/2026-09-27-review-notes-design.md). This file is
// the ONE writer and the read model every frontend uses; nothing outside
// domain knows a review is a note.

// ReviewNoteKind is how a stored review relates to its commit and branch.
// It is computed on every read, never stored.
type ReviewNoteKind int

const (
	ReviewOnCommit   ReviewNoteKind = iota // a commit or range review
	ReviewOnBranch                         // a branch review whose commit is still the branch's tip
	ReviewWasTip                           // a branch review whose branch moved on or is gone
	ReviewOnWorktree                       // a review of a worktree's uncommitted changes (spec 2026-10-04 working reviews)
)

var (
	ErrNoReviewCommit = errors.New("review: working changes have no commit to attach a review to")
	ErrReviewNotFound = errors.New("review not found")
	// ErrNoReviewWorktree: a working-changes review needs this checkout's
	// top level to know which worktree's notes it belongs to.
	ErrNoReviewWorktree = errors.New("review: no worktree to attach a working review to")
)

// SaveReview is the one command every review path issues.
type SaveReview struct {
	Target ReviewTarget
	Agent  string           // the tool's name → Note.Author
	Text   string           // the review, markdown
	NoteID string           // non-empty: rewrite this review in place
	Files  []model.NoteFile // a working review's fingerprints (engine Prepare)
}

// Review is one stored AI review, as every frontend sees it.
type Review struct {
	ID             string
	Kind           ReviewNoteKind
	Commit, Branch string
	Scope          string // the reviewed hex range
	Worktree       string // a working review's checkout ("" otherwise)
	// Files is a working review's fingerprints: what it read, by blob id.
	Files            []model.NoteFile
	Agent, Summary   string
	Text             string
	Created, Updated time.Time
	// Doc is Text parsed as a review document; nil when the review is prose
	// (a reply that was not the document is kept as text).
	Doc *notebatch.ReviewDoc
}

// ReviewHead is a review without its text: what a list row needs.
type ReviewHead struct {
	ID, Commit, Branch, Agent, Summary string
	Created                            time.Time
}

// A held lock is retried within reviewRetryBudget, sleeping a random
// 500 ms–2 s between attempts so two gg processes retrying at once drift
// apart instead of colliding again.
const reviewRetryBudget = 15 * time.Second

// Retry seams. Tests that swap them run serially.
var (
	reviewSleep   = time.Sleep
	reviewJitter  = func() time.Duration { return 500*time.Millisecond + rand.N(1500*time.Millisecond+1) }
	reviewClock   func() time.Duration // time since the save began; nil = the real clock
	reviewPutHook func() error         // fails an attempt before the store is touched
)

// setReviewRetrySeams swaps the sleep for a test and returns the restore
// func, which also resets the clock and the put hook.
func setReviewRetrySeams(sleep func(time.Duration)) func() {
	s := reviewSleep
	reviewSleep = sleep
	return func() { reviewSleep, reviewClock, reviewPutHook = s, nil, nil }
}

// FillReviewCommit resolves t.Commit from the right side of t.Range when a
// constructor left it empty (a typed range, a commit's sha^..sha).
func (s *Service) FillReviewCommit(ctx context.Context, t ReviewTarget) ReviewTarget {
	if t.Commit != "" || t.Kind == ReviewWorking || strings.TrimSpace(t.Range) == "" {
		return t
	}
	right := strings.TrimSpace(t.Range)
	if _, r, ok := strings.Cut(right, ".."); ok {
		right = strings.TrimPrefix(r, ".")
	}
	if sha, found, err := s.ResolveRev(ctx, right); err == nil && found {
		t.Commit = strings.TrimSpace(sha)
	}
	return t
}

// reviewBranchName is tip when it names a local or remote-tracking branch
// (not a sha, not a tag): the branch a branch review's note carries.
func (s *Service) reviewBranchName(ctx context.Context, tip, tipSHA string) string {
	tip = strings.TrimSpace(tip)
	if tip == "HEAD" {
		// "Review the current branch" is asked as HEAD: the note carries the
		// checked-out branch's name (none when HEAD is detached).
		if b, err := s.CurrentBranch(ctx); err == nil && strings.TrimSpace(b) != "" && s.branchTip(ctx, strings.TrimSpace(b)) == tipSHA {
			return strings.TrimSpace(b)
		}
		return ""
	}
	if tip == "" || tip == tipSHA || strings.HasPrefix(tipSHA, tip) {
		return ""
	}
	if s.branchTip(ctx, tip) == "" {
		return ""
	}
	return tip
}

// SaveReview writes (or, with NoteID, rewrites) a review note and returns its
// id. warn is set when a corrupt store had to be moved aside first. A held
// lock is retried within reviewRetryBudget; nothing is ever written to a
// file outside the note store.
func (s *Service) SaveReview(ctx context.Context, cmd SaveReview) (string, string, error) {
	t := s.FillReviewCommit(ctx, cmd.Target)
	wt := ""
	if t.Kind == ReviewWorking {
		top, err := s.TopLevel(ctx)
		if err != nil || strings.TrimSpace(top) == "" {
			return "", "", ErrNoReviewWorktree
		}
		wt = filepath.Clean(strings.TrimSpace(top))
	} else if t.Commit == "" {
		return "", "", ErrNoReviewCommit
	}
	// A review document is stored canonical — unwrapped from any fence or
	// envelope, in its documented shape — so every reader parses one form.
	cmd.Text = canonicalReview(cmd.Text)
	st := s.notesStore(ctx)
	if st == nil {
		return "", "", ErrNotesDisabled
	}
	start := time.Now()
	elapsed := func() time.Duration {
		if reviewClock != nil {
			return reviewClock()
		}
		return time.Since(start)
	}
	warn := ""
	for {
		id, err := s.putReview(st, t, wt, cmd)
		if err == nil {
			s.invalidateNoteCounts()
			return id, warn, nil
		}
		switch {
		case errors.Is(err, notes.ErrCorrupt) && warn == "":
			q, ok := st.(interface{ Quarantine() (string, error) })
			if !ok {
				return "", "", err
			}
			moved, qerr := q.Quarantine()
			if qerr != nil {
				return "", "", errors.Join(err, qerr)
			}
			warn = fmt.Sprintf("the note store was unreadable and was moved to %s", moved)
			continue
		case errors.Is(err, filelock.ErrHeld):
			d := reviewJitter()
			if elapsed()+d > reviewRetryBudget {
				return "", "", err
			}
			if cerr := ctx.Err(); cerr != nil {
				return "", "", cerr
			}
			reviewSleep(d)
			continue
		}
		return "", "", err
	}
}

// putReview is one save attempt: read, build the note, Put.
func (s *Service) putReview(st notes.Store, t ReviewTarget, wt string, cmd SaveReview) (string, error) {
	if reviewPutHook != nil {
		if err := reviewPutHook(); err != nil {
			return "", err
		}
	}
	all, err := st.LoadAll()
	if err != nil {
		return "", err
	}
	now := notes.Now().UTC()
	addr := model.FileAddress{State: model.StateCommitted, Commit: t.Commit, Branch: t.Branch}
	scope := t.Range
	if t.Kind == ReviewWorking {
		// One note per review in this worktree's part file (spec §4): the
		// cleaned top level is what PartOf and the readers key it by.
		addr, scope = model.FileAddress{State: model.StateUnstaged, Worktree: wt}, ""
	}
	n := model.Note{ID: cmd.NoteID, Source: model.NoteSourceAgent, Author: cmd.Agent,
		Address: addr, Side: model.NoteSideNew, Tags: []string{model.ReviewTag}, Scope: scope,
		Summary: reviewSummary(t), Rationale: cmd.Text, Files: cmd.Files, Created: now, Updated: now}
	if n.ID == "" {
		n.ID = notes.NewID(all)
	} else {
		for _, old := range all {
			if old.ID == n.ID {
				n.Created = old.Created
			}
		}
	}
	return n.ID, st.Put(n)
}

// reviewSummary is "Review: <branch or label> (<a7>..<b7>)".
func reviewSummary(t ReviewTarget) string {
	label := strings.TrimSpace(t.Branch)
	if label == "" {
		label = t.DisplayLabel()
	}
	if r := longHex.ReplaceAllStringFunc(t.Range, sha7); r != "" && r != label {
		return "Review: " + label + " (" + r + ")"
	}
	return "Review: " + label
}

// ReviewKindOf computes a review's kind from its stored branch and commit and
// the branch's current tip ("" = the branch is gone).
func ReviewKindOf(branch, commit, branchTip string) ReviewNoteKind {
	switch {
	case branch == "":
		return ReviewOnCommit
	case branchTip == commit:
		return ReviewOnBranch
	}
	return ReviewWasTip
}

// branchTip is a local or remote-tracking branch's full sha; "" when gone.
func (s *Service) branchTip(ctx context.Context, name string) string {
	for _, ref := range []string{"refs/heads/" + name, "refs/remotes/" + name} {
		if sha, found, err := s.ResolveRev(ctx, ref); err == nil && found {
			return strings.TrimSpace(sha)
		}
	}
	return ""
}

func (s *Service) reviewOf(ctx context.Context, n model.Note, tips map[string]string) Review {
	if n.IsWorkingReview() {
		r := Review{ID: n.ID, Kind: ReviewOnWorktree, Worktree: n.Address.Worktree, Files: n.Files,
			Agent: n.Author, Summary: n.Summary, Text: n.Rationale, Created: n.Created, Updated: n.Updated}
		if doc, err := notebatch.ParseReview([]byte(n.Rationale)); err == nil {
			r.Doc = &doc
		}
		return r
	}
	b := n.Address.Branch
	tip, ok := tips[b]
	if !ok && b != "" {
		tip = s.branchTip(ctx, b)
		tips[b] = tip
	}
	r := Review{ID: n.ID, Kind: ReviewKindOf(b, n.Address.Commit, tip),
		Commit: n.Address.Commit, Branch: b, Scope: n.Scope, Agent: n.Author,
		Summary: n.Summary, Text: n.Rationale, Created: n.Created, Updated: n.Updated}
	if doc, err := notebatch.ParseReview([]byte(n.Rationale)); err == nil {
		r.Doc = &doc
	}
	return r
}

// reviewNotes is every review note (roots only), newest first: the commit
// reviews and THIS worktree's working reviews — never a sibling's.
func (s *Service) reviewNotes(ctx context.Context) ([]model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, ErrNotesDisabled
	}
	parts := []notes.Part{notes.PartCommits}
	top, _ := s.TopLevel(ctx)
	top = strings.TrimSpace(top)
	if top != "" {
		parts = append(parts, notes.WorktreePart(top))
	}
	all, err := loadParts(st, parts...)
	if err != nil {
		return nil, err
	}
	var out []model.Note
	for _, n := range all {
		if n.IsReply() {
			continue
		}
		if n.IsReviewNote() || (n.IsWorkingReview() && sameWorktreePath(n.Address.Worktree, top)) {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Created.After(out[b].Created) })
	return out, nil
}

// Reviews is every stored review, newest first.
func (s *Service) Reviews(ctx context.Context) ([]Review, error) {
	ns, err := s.reviewNotes(ctx)
	tips := map[string]string{}
	out := make([]Review, 0, len(ns))
	for _, n := range ns {
		out = append(out, s.reviewOf(ctx, n, tips))
	}
	return out, err
}

// Review is one stored review by note id; ErrReviewNotFound when gone.
func (s *Service) Review(ctx context.Context, id string) (Review, error) {
	ns, err := s.reviewNotes(ctx)
	if err != nil {
		return Review{}, err
	}
	for _, n := range ns {
		if n.ID == id {
			return s.reviewOf(ctx, n, map[string]string{}), nil
		}
	}
	return Review{}, ErrReviewNotFound
}

// ReviewsForCommit is every review stored on commit sha, newest first.
func (s *Service) ReviewsForCommit(ctx context.Context, sha string) ([]Review, error) {
	// Filter the notes first: building a Review parses its document and may
	// resolve its branch tip (a git call), which only this commit's need.
	ns, err := s.reviewNotes(ctx)
	tips := map[string]string{}
	var out []Review
	for _, n := range ns {
		if n.Address.Commit == sha {
			out = append(out, s.reviewOf(ctx, n, tips))
		}
	}
	return out, err
}

// ReviewsForBranch is the reviews of the branch's CURRENT tip; older ones
// stay on their commits as ReviewWasTip.
func (s *Service) ReviewsForBranch(ctx context.Context, name string) ([]Review, error) {
	all, err := s.Reviews(ctx)
	var out []Review
	for _, r := range all {
		if r.Branch == name && r.Kind == ReviewOnBranch {
			out = append(out, r)
		}
	}
	return out, err
}

// reviewsFollowBranchOp keeps review notes in step with a branch op that
// just succeeded: a deleted branch takes its reviews, a renamed one keeps
// them under the new name — and so do a range review's notes (their
// PreviewBranch). Otherwise only review notes are touched: a line note's
// Address.Branch is left alone. Best-effort: the op already happened.
func (s *Service) reviewsFollowBranchOp(ctx context.Context, op engine.Operation) {
	var drop, from, to string
	switch o := op.(type) {
	case engine.DeleteBranch:
		drop = o.Name
	case engine.DeleteRemoteBranch:
		drop = o.Remote + "/" + o.Branch
	case engine.RenameBranch:
		from, to = o.Old, o.New
	default:
		return
	}
	st := s.notesStore(ctx)
	if st == nil {
		return
	}
	all, err := st.Load(notes.PartCommits)
	if err != nil {
		return
	}
	changed := false
	for _, n := range all {
		// A range review's notes are shown on the branch they were written on
		// (Note.PreviewBranch): they keep that under the branch's new name. A
		// deleted branch's are left in the store, as every line note is.
		if from != "" && n.PreviewBranch == from {
			n.PreviewBranch = to
			changed = st.Put(n) == nil || changed
			continue
		}
		if n.IsReply() || !n.IsReviewNote() {
			continue
		}
		switch {
		case drop != "" && n.Address.Branch == drop:
			changed = st.Remove(n.ID) == nil || changed
		case from != "" && n.Address.Branch == from:
			n.Address.Branch = to
			changed = st.Put(n) == nil || changed
		}
	}
	if changed {
		s.invalidateNoteCounts()
	}
}
