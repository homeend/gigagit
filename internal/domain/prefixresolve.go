package domain

import (
	"context"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/template"
	"github.com/homeend/gigagit/internal/worktree"
)

// PrefixUserLabels returns a prefix value's <user:…> labels in order.
// Frontends prompt for these before resolving; they route through domain
// because internal/template is a layering detail (the PrefixID pattern).
func PrefixUserLabels(value string) []string {
	return template.UserLabels(value)
}

// PrefixSeqNames returns a prefix value's <seq:…> counter names — the set to
// BumpPrefixSeqs after a successful create. A pure passthrough (PrefixID
// pattern): unlike ResolvePrefixValue it needs no inputs, so it cannot fail
// on an unfilled <user:…> label.
func PrefixSeqNames(value string) []string {
	return template.SeqNames(value)
}

// ResolvePrefixValue previews value against this repo: <parent-branch> is the
// current branch, <repo> the main worktree's directory name, <seq:…> counters
// are PEEKED (never consumed — a canceled prefill must not burn a number).
// Returns the resolved string plus the value's seq names so the caller can
// BumpPrefixSeqs once the branch is actually created (the TUI's
// pendingSeqBump contract).
func (s *Service) ResolvePrefixValue(ctx context.Context, value string, inputs map[string]string) (string, []string, error) {
	parent, err := s.CurrentBranch(ctx)
	if err != nil {
		parent = ""
	}
	repo := ""
	if wts, werr := s.Worktrees(ctx); werr == nil && len(wts) > 0 && wts[0].Path != "" {
		repo = worktree.RepoName(wts[0].Path)
	}
	gitDir := ""
	if cd, cerr := s.GitCommonDir(ctx); cerr == nil {
		gitDir = strings.TrimSpace(cd)
		if repo == "" {
			repo = filepath.Base(filepath.Dir(gitDir))
		}
	}
	names := template.SeqNames(value)
	tctx := template.Ctx{
		ParentBranch: parent,
		Repo:         repo,
		Seqs:         worktree.PeekSeqs(gitDir, names),
		Now:          time.Now,
		Rand:         rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
	}
	out, err := template.Resolve(value, inputs, tctx)
	if err != nil {
		return "", nil, err
	}
	return out, names, nil
}

// BumpPrefixSeqs consumes the named counters (call it only after the create
// that used the resolved value succeeded). Per-name bumps are best-effort —
// the branch already exists; a failed counter write must not fail the flow —
// but the first failure is still reported.
func (s *Service) BumpPrefixSeqs(ctx context.Context, names []string) error {
	gitDir, err := s.GitCommonDir(ctx)
	if err != nil {
		return err
	}
	var first error
	for _, n := range names {
		if _, berr := config.BumpSeq(strings.TrimSpace(gitDir), n); berr != nil && first == nil {
			first = berr
		}
	}
	return first
}
