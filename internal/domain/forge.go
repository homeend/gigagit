package domain

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/preflight"
)

// ErrForgeUnavailable: detection found no usable forge CLI for this repo.
var ErrForgeUnavailable = errors.New("no forge CLI can read this repository's pull requests")

// ForgeStatus is the once-per-session detection verdict.
type ForgeStatus struct {
	Provider string // "" = none usable
	Err      error  // why not (diagnostics; the TUI stays silent about it)
}

// Available reports whether a forge provider can be used.
func (st ForgeStatus) Available() bool { return st.Provider != "" }

// SetForgeProviders replaces the provider list. Test seam; call before the
// first ForgeStatus.
func (s *Service) SetForgeProviders(ps []forge.Provider) {
	s.forgeMu.Lock()
	defer s.forgeMu.Unlock()
	s.forgeProviders = ps
}

// ForgeDisabled is a TEST seam (the NotesDisabled precedent): a frontend test
// binary sets it in TestMain so no Service ever shells out to the real forge
// CLI — the probe is a network call. A Service with injected providers
// (SetForgeProviders) ignores it.
var ForgeDisabled bool

// ForgeStatus probes the forge providers on its FIRST call and answers from
// that verdict for the rest of the session — there is no re-detection: a user
// who installs or logs into gh restarts gg. The first call makes a network
// round trip, so frontends call it off their UI thread.
//
// The probe runs OUTSIDE forgeMu: it can take its whole timeout, and
// Preflight (notices, FeatureEnabled, the web gate) reads forgeProbe under
// that lock. Concurrent first callers wait on forgeProbing, so Detect still
// runs once; forgeProbe reports "unprobed" while it is in flight.
func (s *Service) ForgeStatus(ctx context.Context) ForgeStatus {
	s.forgeMu.Lock()
	for !s.forgeProbed {
		if wait := s.forgeProbing; wait != nil {
			s.forgeMu.Unlock()
			select {
			case <-wait:
			case <-ctx.Done():
				return ForgeStatus{Err: errors.Join(ErrForgeUnavailable, ctx.Err())}
			}
			s.forgeMu.Lock()
			continue
		}
		done := make(chan struct{})
		s.forgeProbing = done
		ps, rec := s.forgeProviders, s.forgeRec
		s.forgeMu.Unlock()

		if ps == nil && !ForgeDisabled {
			ps = forge.Default(s.workdir, rec)
		}
		var active forge.Provider
		probeErr := ErrForgeUnavailable
		for _, p := range ps {
			err := p.Detect(ctx)
			if err == nil {
				active, probeErr = p, nil
				break
			}
			probeErr = errors.Join(ErrForgeUnavailable, err)
		}

		if active == nil && ctx.Err() != nil {
			// The CALLER gave up (a cancelled startup, a closed request) — that
			// says nothing about gh. Do not burn the session's one verdict on it.
			s.forgeMu.Lock()
			s.forgeProbing = nil
			s.forgeMu.Unlock()
			close(done)
			return ForgeStatus{Err: errors.Join(ErrForgeUnavailable, ctx.Err())}
		}
		s.forgeMu.Lock()
		s.forgeActive, s.forgeErr = active, probeErr
		s.forgeProbed, s.forgeProbing = true, nil
		s.forgeMu.Unlock()
		close(done)
		s.invalidatePreflight() // the forge verdict just changed; never under forgeMu
		s.forgeMu.Lock()
	}
	st := ForgeStatus{Err: s.forgeErr}
	if s.forgeActive != nil {
		st = ForgeStatus{Provider: s.forgeActive.Name()}
	}
	s.forgeMu.Unlock()
	return st
}

// forgeProbe snapshots detection for the preflight resolver WITHOUT probing.
func (s *Service) forgeProbe() *preflight.ForgeProbe {
	s.forgeMu.Lock()
	defer s.forgeMu.Unlock()
	switch {
	case !s.forgeProbed:
		return nil
	case s.forgeActive == nil:
		return &preflight.ForgeProbe{Err: s.forgeErr.Error()}
	}
	return &preflight.ForgeProbe{Provider: s.forgeActive.Name()}
}

// provider returns the active provider, probing if nobody has yet.
func (s *Service) provider(ctx context.Context) (forge.Provider, error) {
	if st := s.ForgeStatus(ctx); !st.Available() {
		return nil, st.Err
	}
	s.forgeMu.Lock()
	defer s.forgeMu.Unlock()
	return s.forgeActive, nil
}

// PullRequests lists the open pull requests plus every KNOWN one that is no
// longer open: a PR never disappears from gg because it was closed or merged.
// "Known" = listed open earlier this session, or has a fetched
// refs/gg/pr/<n> (the user opened it, in any session — the ref is the durable
// record; there is no state file). Open ones come first; each group is
// newest-updated first.
func (s *Service) PullRequests(ctx context.Context) ([]model.PullRequest, error) {
	p, err := s.provider(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.flight.Do("forge-prs", func() (any, error) { return s.pullRequests(ctx, p) })
	if err != nil {
		return nil, err
	}
	return slices.Clone(v.([]model.PullRequest)), nil
}

func (s *Service) pullRequests(ctx context.Context, p forge.Provider) ([]model.PullRequest, error) {
	open, err := p.ListOpen(ctx)
	if err != nil {
		return nil, err
	}
	isOpen := make(map[int]bool, len(open))
	known := map[int]bool{}
	s.forgeMu.Lock()
	if s.forgeSeen == nil {
		s.forgeSeen = map[int]bool{}
	}
	for _, pr := range open {
		isOpen[pr.Number] = true
		s.forgeSeen[pr.Number] = true
		delete(s.forgeTerminal, pr.Number) // reopened
	}
	for n := range s.forgeSeen {
		known[n] = true
	}
	s.forgeMu.Unlock()
	if refs, err := s.repo.ForEachRef(ctx, git.PRRefPrefix); err == nil { // fail open
		for _, r := range refs {
			if n, ok := git.ParsePRRef(r.Ref); ok {
				known[n] = true
			}
		}
	}
	var rest []model.PullRequest
	for n := range known {
		if !isOpen[n] {
			rest = append(rest, s.terminalPR(ctx, p, n))
		}
	}
	byUpdated := func(a, b model.PullRequest) int {
		if c := b.Updated.Compare(a.Updated); c != 0 {
			return c
		}
		return b.Number - a.Number
	}
	slices.SortFunc(open, byUpdated)
	slices.SortFunc(rest, byUpdated)
	return append(open, rest...), nil
}

// terminalPR reads a no-longer-open PR once and caches the answer: a
// closed/merged state is re-read only after Forget or a reopen.
func (s *Service) terminalPR(ctx context.Context, p forge.Provider, n int) model.PullRequest {
	s.forgeMu.Lock()
	cached, ok := s.forgeTerminal[n]
	s.forgeMu.Unlock()
	if ok {
		return cached
	}
	pr, err := p.PR(ctx, n)
	switch {
	case errors.Is(err, forge.ErrNotFound):
		pr = model.PullRequest{Number: n, State: model.PRStateUnavailable}
	case err != nil:
		// Transient: show it as unavailable now, do NOT cache, retry next read.
		return model.PullRequest{Number: n, State: model.PRStateUnavailable}
	case pr.IsOpen():
		return pr // raced with a reopen; the next list carries it
	}
	s.forgeMu.Lock()
	if s.forgeTerminal == nil {
		s.forgeTerminal = map[int]model.PullRequest{}
	}
	s.forgeTerminal[n] = pr
	s.forgeMu.Unlock()
	return pr
}

// PullRequest reads one PR with its body.
func (s *Service) PullRequest(ctx context.Context, n int) (model.PullRequest, error) {
	p, err := s.provider(ctx)
	if err != nil {
		return model.PullRequest{}, err
	}
	return p.PR(ctx, n)
}

// PRComments is one PR's comments, bucketed by where a frontend shows them.
type PRComments struct {
	Inline    []model.ForgeComment `json:"inline"`   // anchored at a current position (line or whole file), thread order
	Hub       []model.ForgeComment `json:"hub"`      // conversation + review verdicts, chronological
	Outdated  []model.ForgeComment `json:"outdated"` // inline threads with no current position
	Truncated bool                 `json:"truncated"`
}

// PRComments reads PR n's comments. Read-only and uncached: the forge owns
// this data, gg only shows it.
func (s *Service) PRComments(ctx context.Context, n int) (PRComments, error) {
	p, err := s.provider(ctx)
	if err != nil {
		return PRComments{}, err
	}
	cs, truncated, err := p.Comments(ctx, n)
	if err != nil {
		return PRComments{}, err
	}
	// Empty buckets are [] on the wire, never null: agents index into them.
	out := PRComments{Inline: []model.ForgeComment{}, Hub: []model.ForgeComment{},
		Outdated: []model.ForgeComment{}, Truncated: truncated}
	for _, c := range cs {
		switch {
		case c.Kind == model.ForgeCommentGeneral || c.Kind == model.ForgeCommentReview:
			out.Hub = append(out.Hub, c)
		case c.Outdated:
			out.Outdated = append(out.Outdated, c)
		case c.Kind == model.ForgeCommentFile:
			out.Inline = append(out.Inline, c)
		case c.Line <= 0: // a line comment the forge gave no position
			out.Outdated = append(out.Outdated, c)
		default:
			out.Inline = append(out.Inline, c)
		}
	}
	slices.SortStableFunc(out.Hub, func(a, b model.ForgeComment) int { return a.Created.Compare(b.Created) })
	return out, nil
}

// PRFetchOp builds the op that brings PR n's head into refs/gg/pr/<n>. It
// fetches through the configured remote naming the base repository — the
// user's own transport and credentials — and only falls back to the
// provider's URL when no remote matches.
func (s *Service) PRFetchOp(ctx context.Context, n int) (engine.FetchPRHead, error) {
	p, err := s.provider(ctx)
	if err != nil {
		return engine.FetchPRHead{}, err
	}
	pr, err := p.PR(ctx, n)
	if err != nil {
		return engine.FetchPRHead{}, err
	}
	slug, fallback, err := p.BaseRepo(ctx)
	if err != nil {
		return engine.FetchPRHead{}, err
	}
	remote := fallback
	if names, err := s.repo.RemoteNames(ctx); err == nil && slug != "" {
		for _, name := range names {
			if u, err := s.repo.RemoteURL(ctx, name); err == nil && forge.RepoSlug(u) == slug {
				remote = name
				break
			}
		}
	}
	if remote == "" {
		return engine.FetchPRHead{}, fmt.Errorf("pull request #%d: no remote or URL for the base repository", n)
	}
	return engine.FetchPRHead{Remote: remote, Refspec: p.HeadRefspec(n), Number: n, HeadSHA: pr.HeadSHA}, nil
}

// PRForgetOp builds the op that drops PR n's ref, and forgets n for this
// session so a closed row leaves the list.
func (s *Service) PRForgetOp(n int) engine.ForgetPR {
	s.forgeMu.Lock()
	delete(s.forgeSeen, n)
	delete(s.forgeTerminal, n)
	s.forgeMu.Unlock()
	return engine.ForgetPR{Number: n}
}

// PRPair is the rev pair a PR's diff opens on: Base...Head.
type PRPair struct{ Base, Head string }

// PRPair picks the pair for p. An open PR diffs against its moving target
// branch (what merging would do today); a closed/merged one against the base
// commit the forge recorded — after a merge the target tip already contains
// the head, so the three-dot diff would be empty. Each choice falls through
// to the next when it does not resolve here: target branch, recorded base
// sha, then a remote-tracking <remote>/<target>.
func (s *Service) PRPair(ctx context.Context, p model.PullRequest) PRPair {
	pair := PRPair{Base: p.BaseSHA, Head: git.PRRef(p.Number)}
	resolves := func(rev string) bool {
		if rev == "" {
			return false
		}
		_, err := s.repo.ResolveCommit(ctx, rev)
		return err == nil
	}
	if p.IsOpen() && resolves(p.Target) {
		pair.Base = p.Target
		return pair
	}
	if resolves(p.BaseSHA) {
		return pair
	}
	if names, err := s.repo.RemoteNames(ctx); err == nil && p.Target != "" {
		for _, name := range names {
			if rev := name + "/" + p.Target; resolves(rev) {
				pair.Base = rev
				return pair
			}
		}
	}
	return pair
}
