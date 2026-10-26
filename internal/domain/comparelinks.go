package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// LinkSide names one half of a two-link comparison.
type LinkSide int

const (
	LinkSideLeft LinkSide = iota
	LinkSideRight
)

func (s LinkSide) String() string {
	if s == LinkSideRight {
		return "right"
	}
	return "left"
}

// LinkSideError is a failure that belongs to ONE side of a comparison, so a
// dialog can put the message under the right field. It unwraps: the CLI's
// exit codes key on the cause (model.ErrLink, ErrLinkCrossRepo), not on which
// side it came from.
type LinkSideError struct {
	Side LinkSide
	Err  error
}

func (e *LinkSideError) Error() string { return e.Side.String() + ": " + e.Err.Error() }
func (e *LinkSideError) Unwrap() error { return e.Err }

// ErrLinkCrossRepo refuses a link that names a checkout other than the
// service's own (spec §9). It is its own sentinel rather than a wrapped
// model.ErrLink: the link is well-formed, and "bad gg link" would say
// otherwise. A caller mistake all the same — the CLI exits 2 on it.
var ErrLinkCrossRepo = errors.New("cross-repository compare is not supported yet")

// ErrLinkOtherWorktree refuses a link to ANOTHER worktree's working tree or
// index: same repository, but that content is the other checkout's, and every
// byte read here would come from this one. A caller mistake like
// ErrLinkCrossRepo — the link is well-formed, it is just read in the wrong
// place.
var ErrLinkOtherWorktree = errors.New("names another worktree's uncommitted state; run it in that worktree")

// LinkComparison is what comparing two links yields: the two resolved sets
// (Source(path) is each member's byte source), the texts exactly as given —
// the comparison's identity, which a view tags itself with and a saved row
// stores — and the changed-file list.
type LinkComparison struct {
	Left, Right         FileSet
	LeftText, RightText string
	Files               []model.CommitFile
}

// EvalLinkText turns ONE link text into its file set in THIS checkout. It is
// the only place outside this package's own tests that a link becomes a set
// (internal/archtest holds frontends to it).
//
// THE LINK IS LOCATED BEFORE IT IS EVALUATED, and that order is a correctness
// requirement rather than a formality. A PARSED local-form link
// (gg:///abs/checkout/dir/f.go) carries the checkout AND the file path
// undivided in Repo.Abs with Link.Path == "" — the grammar puts no delimiter
// between them, and only this machine's repository registry can split them.
// Hand an unresolved local link to EvalLink and a FILE link silently evaluates
// as a WHOLE-TREE link: a wrong answer with no error, which is the one class
// this feature refuses to ship. LocateLink performs the split (and refuses a
// path that escapes the checkout); the located path is then the link's Path,
// while the TARGET stays exactly as parsed — a `@<a>..<b>` change-set must not
// be flattened into an address, which is why this goes through LocateLink and
// not ResolveLink.
//
// Cross-repository compare is deferred (spec §9). Both sides would evaluate to
// file sets happily, but every read goes through ONE Service under ONE
// repogate reservation, so two repositories need two reservations taken in a
// fixed global order to avoid deadlock. Refuse explicitly rather than silently
// compare against the wrong checkout.
//
// o.Cwd defaults to s: the caller is asking about this checkout.
func (s *Service) EvalLinkText(ctx context.Context, text string, o ResolveOpts) (FileSet, error) {
	text = strings.TrimSpace(text)
	l, err := model.ParseLink(text)
	if err != nil {
		return FileSet{}, err
	}
	top, err := s.TopLevel(ctx)
	if err != nil {
		return FileSet{}, err
	}
	if o.Cwd == nil {
		o.Cwd = s
	}
	if o.OpenFn == nil {
		o.OpenFn = Open
	}
	checkout, rel, err := LocateLink(ctx, l, o)
	if err != nil {
		return FileSet{}, err
	}
	l.Path = rel
	if SameCheckout(checkout, top) {
		return s.EvalLink(ctx, l)
	}
	return s.evalSiblingLink(ctx, text, checkout, l, o)
}

// evalSiblingLink evaluates a link that names ANOTHER checkout: allowed when
// that checkout is a worktree of THIS repository, refused otherwise.
//
// A worktree is the same repository — one object store, one ref store, one
// repogate reservation (keyed by the common dir) — and the saved-comparison
// store is keyed the same way, so every worktree lists rows that name
// whichever checkout saved them. Refusing those as cross-repository made a
// row the list showed impossible to open.
//
// The link is evaluated by the checkout it NAMES, not by this one: a ref such
// as HEAD is per-worktree. Its set is then readable here only when it is made
// of objects; a working tree or index endpoint means the other checkout's
// files, which every byte read through this Service would take from its own.
func (s *Service) evalSiblingLink(ctx context.Context, text, checkout string, l model.Link, o ResolveOpts) (FileSet, error) {
	other := o.OpenFn(checkout)
	ours, err := s.GitCommonDir(ctx)
	if err != nil {
		return FileSet{}, err
	}
	theirs, err := other.GitCommonDir(ctx)
	if err != nil {
		return FileSet{}, err
	}
	if !SamePath(ours, theirs) {
		return FileSet{}, fmt.Errorf("%s names a different repository (%s): %w", text, checkout, ErrLinkCrossRepo)
	}
	fs, err := other.EvalLink(ctx, l)
	if err != nil {
		return FileSet{}, err
	}
	if fs.Endpoint().IsLive() {
		return FileSet{}, fmt.Errorf("%s %w (%s)", text, ErrLinkOtherWorktree, checkout)
	}
	return fs, nil
}

// CompareLinks is the one door from two link texts to a comparison: every
// frontend that compares links calls it, so none of them can disagree about
// what a link means (spec D6). A failure that belongs to one side comes back
// as a *LinkSideError; a failure of the comparison itself comes back bare.
func (s *Service) CompareLinks(ctx context.Context, leftText, rightText string, o ResolveOpts) (LinkComparison, error) {
	c := LinkComparison{LeftText: strings.TrimSpace(leftText), RightText: strings.TrimSpace(rightText)}
	var err error
	if c.Left, err = s.EvalLinkText(ctx, c.LeftText, o); err != nil {
		return LinkComparison{}, &LinkSideError{Side: LinkSideLeft, Err: err}
	}
	if c.Right, err = s.EvalLinkText(ctx, c.RightText, o); err != nil {
		return LinkComparison{}, &LinkSideError{Side: LinkSideRight, Err: err}
	}
	if c.Files, err = s.CompareSets(ctx, c.Left, c.Right); err != nil {
		return LinkComparison{}, err
	}
	return c, nil
}
