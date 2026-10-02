package domain

import (
	"context"
	"errors"
	"fmt"
	"github.com/homeend/gigagit/internal/model"
	"slices"
	"strings"
)

// Notes on a commit pair.
//
// A pair a..b shows `git diff a b`: commit a on the old side, commit b on the
// new side. The new side is byte for byte the file at b, so a note on it is an
// ordinary committed note on b — the very rule a merge preview follows for its
// source tip. Nothing new is stored, and nothing here forks the preview lane:
// a pair is one more way to BUILD a PreviewNoteSet, and the resolver, the
// loader, the counts and the hunk anchor then serve it unchanged.

// IsPair reports a scope built from two COMMITS (PairNotes) rather than from a
// branch pair (PreviewNotes). The names are what differ: a pair has none, so
// every surface that would print or link them must ask this first.
func (set PreviewNoteSet) IsPair() bool { return set.OK() && set.Source == "" }

// PairNotes is a commit pair's note scope: notes are written to b (new side)
// and gathered along a..b. It reads no saved entry, so an unsaved pair link
// gets the very same scope as a saved one. A commit that is not here yields
// the ZERO set and no error (the preview lane's ruling 6: no set, no badge).
func (s *Service) PairNotes(ctx context.Context, a, b string) (PreviewNoteSet, error) {
	fa, aok, err := s.ResolveRev(ctx, a)
	if err != nil {
		return PreviewNoteSet{}, err
	}
	fb, bok, err := s.ResolveRev(ctx, b)
	if err != nil {
		return PreviewNoteSet{}, err
	}
	if !aok || !bok {
		return PreviewNoteSet{}, nil
	}
	fa, fb = strings.TrimSpace(fa), strings.TrimSpace(fb)
	key := "pair-revlist:" + fa + ":" + fb
	v, err := s.factory.Cache("preview").GetOrLoad(key, func() (any, error) {
		return query(ctx, s, key, func(ctx context.Context) ([]string, error) {
			return s.repo.RevListRange(ctx, fa, fb)
		})
	})
	if err != nil {
		return PreviewNoteSet{}, err
	}
	commits := v.([]string)
	// a..b is EMPTY when b is an ancestor of a (a reversed save). b is still
	// the write target, so it is always a member — or a note just written to
	// it would not show in its own pair. A fresh slice: the cached one is
	// shared.
	if !slices.Contains(commits, fb) {
		commits = append([]string{fb}, commits...)
	}
	return PreviewNoteSet{Tip: fb, Base: fa, Commits: commits}, nil
}

// NoteScopeResolve is THE parser of a `--preview` argument and of MCP's
// `preview` arg: it turns the text into a usable note scope or says why not.
// It lives in domain so the CLI and MCP cannot disagree about a word of it.
//
//	<target>...<source>   a merge preview, saved or not
//	<a>..<b>              a commit pair, saved or not
//	<id> | <label>        a saved entry of either kind (a preview wins a
//	                      shared label — the order `gg preview show` follows)
//
// Unlike PreviewNotes/PairNotes (whose zero set means "nothing to show"), a
// scope asked for BY NAME that cannot be built is an error: the caller must
// not be handed an empty diff.
func (s *Service) NoteScopeResolve(ctx context.Context, spec string) (PreviewNoteSet, error) {
	spec = strings.TrimSpace(spec)
	if !strings.Contains(spec, "...") {
		if i := strings.Index(spec, ".."); i >= 0 {
			a, b := strings.TrimSpace(spec[:i]), strings.TrimSpace(spec[i+2:])
			if a == "" || b == "" {
				return PreviewNoteSet{}, fmt.Errorf("%w (a commit pair is <a>..<b>)", errPreviewPairShape)
			}
			return s.pairScope(ctx, a, b)
		}
	}
	source, target, err := s.PreviewResolve(ctx, spec)
	if errors.Is(err, ErrPreviewNotFound) && spec != "" {
		p, perr := s.PairGet(ctx, spec)
		if perr == nil {
			return s.pairScope(ctx, p.A, p.B)
		}
		if !errors.Is(perr, ErrPairNotFound) {
			return PreviewNoteSet{}, perr
		}
	}
	if err != nil {
		return PreviewNoteSet{}, err
	}
	sum, err := s.PreviewSummary(ctx, source, target)
	if err != nil {
		return PreviewNoteSet{}, err
	}
	switch sum.State {
	case PreviewOK:
	case PreviewMissingSource:
		return PreviewNoteSet{}, fmt.Errorf("preview: missing: %s", source)
	case PreviewMissingTarget:
		return PreviewNoteSet{}, fmt.Errorf("preview: missing: %s", target)
	default:
		return PreviewNoteSet{}, fmt.Errorf("preview: %s → %s: %s", source, target, sum.State)
	}
	set, err := s.PreviewNotes(ctx, source, target)
	if err != nil {
		return PreviewNoteSet{}, err
	}
	if !set.OK() {
		return PreviewNoteSet{}, fmt.Errorf("preview %s → %s is not previewable", source, target)
	}
	return set, nil
}

// pairScope is PairNotes for a caller that named the pair: a commit that is
// not in this repository is an error naming it.
func (s *Service) pairScope(ctx context.Context, a, b string) (PreviewNoteSet, error) {
	set, err := s.PairNotes(ctx, a, b)
	if err != nil {
		return PreviewNoteSet{}, err
	}
	if set.OK() {
		return set, nil
	}
	missing := b
	if _, ok, _ := s.ResolveRev(ctx, a); !ok {
		missing = a
	}
	return PreviewNoteSet{}, fmt.Errorf("preview: missing commit: %s", missing)
}

// NoteScopeLabel names a note scope (model.Note.Preview) the Previews panel's
// way round: a merge preview as "source → target", a commit pair as its
// "a..b". One wording for every frontend's Range review row.
func NoteScopeLabel(scope string) string {
	if target, source, ok := strings.Cut(scope, "..."); ok {
		return source + " → " + target
	}
	return scope
}

// ScopeAtCommit turns a scope a note names (Note.Preview) into the frozen
// commit range it opens as from commit, the commit holding the note:
//
//	<a>..<b>              the pair itself (stored as short shas)
//	<target>...<source>   where commit left the target .. commit
//
// A merge preview's names move on; the range read from the commit does not,
// so the review opens as it was written however far the branch went since.
// A commit already in the target (the branch was merged) has no such range
// left: that is an error, never an empty diff.
func (s *Service) ScopeAtCommit(ctx context.Context, scope, commit string) (a, b string, err error) {
	full := func(rev string) (string, error) { return s.fullRev(ctx, rev) }
	scope = strings.TrimSpace(scope)
	// The start the notes recorded when they were written wins: git can no
	// longer work it out once the branch was merged into its target.
	if c, cerr := s.NoteCounts(ctx); cerr == nil {
		if full, ferr := full(commit); ferr == nil {
			for _, sc := range c.ScopesByCommit[full] {
				if sc.Scope != scope || sc.Base == "" {
					continue
				}
				if base, berr := s.fullRev(ctx, sc.Base); berr == nil && base != full {
					return base, full, nil
				}
			}
		}
	}
	if target, _, ok := strings.Cut(scope, "..."); ok {
		target = strings.TrimSpace(target)
		if target == "" {
			return "", "", errPreviewPairShape
		}
		if b, err = full(commit); err != nil {
			return "", "", err
		}
		if _, err = full(target); err != nil {
			return "", "", err
		}
		base, err := s.repo.MergeBase(ctx, target, b)
		if err != nil {
			return "", "", err
		}
		if base == b {
			return "", "", fmt.Errorf("%s is already in %s", shortSHA(b), target)
		}
		return base, b, nil
	}
	l, r, ok := strings.Cut(scope, "..")
	if !ok || strings.TrimSpace(l) == "" || strings.TrimSpace(r) == "" {
		return "", "", errPreviewPairShape
	}
	if a, err = full(strings.TrimSpace(l)); err != nil {
		return "", "", err
	}
	if b, err = full(strings.TrimSpace(r)); err != nil {
		return "", "", err
	}
	return a, b, nil
}

// fullRev resolves rev to its full commit id; a rev that is not here is an
// error naming it.
func (s *Service) fullRev(ctx context.Context, rev string) (string, error) {
	sha, ok, err := s.ResolveRev(ctx, rev)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("missing: %s", rev)
	}
	return strings.TrimSpace(sha), nil
}

// stampReview records, on a note written in a scope, what later reads cannot
// work out any more:
//
//   - a commit PAIR's note (a range review) gets the branch it was written on
//     — the checked-out one — because a range review is shown on that branch
//     and no other (ReviewShownOn);
//   - a merge PREVIEW's note gets where its range began (the merge base): the
//     review still opens from it once the branch was merged and git can no
//     longer tell. It gets no branch: a preview review is the preview's, not a
//     branch's.
//
// Best-effort: a caller that already set them, a runner that cannot resolve,
// a detached HEAD just leave the fields as they are.
func (s *Service) stampReview(ctx context.Context, n *model.Note) {
	if n.Preview == "" || n.IsReply() || n.Address.State != model.StateCommitted {
		return
	}
	target, _, merge := strings.Cut(n.Preview, "...")
	if !merge {
		if n.PreviewBranch == "" {
			if b, err := s.CurrentBranch(ctx); err == nil {
				n.PreviewBranch = strings.TrimSpace(b)
			}
		}
		return
	}
	if n.PreviewBase == "" && isFullSHA(n.Address.Commit) {
		if base, err := s.repo.MergeBase(ctx, strings.TrimSpace(target), n.Address.Commit); err == nil && isFullSHA(base) && base != n.Address.Commit {
			n.PreviewBase = base
		}
	}
}

// shortBranch drops a refs/heads/ prefix.
func shortBranch(name string) string { return strings.TrimPrefix(name, "refs/heads/") }

// IsPreviewScope reports a merge preview's scope name ("<target>...<source>")
// as opposed to a commit pair's ("<a>..<b>"). A preview review belongs to its
// preview: it is shown there and never on a commit.
func IsPreviewScope(scope string) bool { return strings.Contains(scope, "...") }

// NoteReviewBranch is the branch a note's RANGE review was written on (the
// one a commit pair's note recorded). "" for a plain note, for a preview's
// note — a preview review is not a branch's — and for a pair note older than
// the record.
func NoteReviewBranch(n model.Note) string {
	if IsPreviewScope(n.Preview) {
		return ""
	}
	return n.PreviewBranch
}

// ReviewShownOn reports whether a review created on reviewBranch shows while
// the reader is on the viewing branches (the checked-out branch, or the ones
// the commit list is narrowed to). What was created on a branch is shown on
// that branch and on no other — not on the target it was merged into. It also
// shows when either side is unknown: a review with no branch recorded, or no
// branch being viewed (a detached HEAD).
//
// The names match exactly, or as a branch and its remote-tracking spelling
// (feat ~ origin/feat).
func ReviewShownOn(reviewBranch string, viewing []string) bool {
	if reviewBranch == "" || len(viewing) == 0 {
		return true
	}
	for _, v := range viewing {
		v = shortBranch(v)
		if v == reviewBranch || strings.HasSuffix(reviewBranch, "/"+v) || strings.HasSuffix(v, "/"+reviewBranch) {
			return true
		}
	}
	return false
}
