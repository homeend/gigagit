package domain

import (
	"context"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/template"
	"github.com/homeend/gigagit/internal/worktree"
)

// NewWorktreeSpec is what a create-worktree dialog has filled in: the TUI
// worktree popup's fields (internal/tui/worktree_popup.go). Existing is the
// branch entrance (the selection is checked out, the branch template does
// not apply); Seed and Now fix <random> and <date> for the dialog's life.
type NewWorktreeSpec struct {
	Start          string
	Existing       bool
	BranchTemplate string
	PathTemplate   string
	FixedBranch    string // "" = the selection (Existing) or the branch template
	FixedPath      string // "" = the path template
	Inputs         map[string]string
	Seed           uint64
	Now            time.Time
}

// NewWorktreePreview is the dialog's live preview: the <user:…> labels to ask
// for, the names a create would use, and why they cannot resolve.
type NewWorktreePreview struct {
	Labels []string
	Branch string
	Path   string
	Err    error
}

// PreviewNewWorktree is the TUI popup's recompute over a spec: <repo> is the
// main worktree's directory name, <parent-branch> the start point, <seq>
// counters are PEEKED (the create bumps them, never the preview).
func (s *Service) PreviewNewWorktree(ctx context.Context, sp NewWorktreeSpec) NewWorktreePreview {
	tm := worktree.Templates{Path: sp.PathTemplate}
	if !sp.Existing {
		tm.Branch = sp.BranchTemplate
	}
	out := NewWorktreePreview{Labels: tm.Labels()}
	repo, gitDir := "", ""
	if wts, err := s.Worktrees(ctx); err == nil && len(wts) > 0 && wts[0].Path != "" {
		repo = worktree.RepoName(wts[0].Path)
	}
	if cd, err := s.GitCommonDir(ctx); err == nil {
		gitDir = strings.TrimSpace(cd)
		if repo == "" {
			repo = filepath.Base(filepath.Dir(gitDir))
		}
	}
	fixed := sp.FixedBranch
	if fixed == "" && sp.Existing {
		fixed = sp.Start
	}
	if sp.FixedPath != "" {
		tm.Path = "" // a hand-edited path bypasses the template and its counters
	}
	tctx := template.Ctx{
		ParentBranch: sp.Start,
		Repo:         repo,
		Seqs:         worktree.PeekSeqs(gitDir, tm.SeqNames()),
		Now:          func() time.Time { return sp.Now },
		Rand:         rand.New(rand.NewPCG(sp.Seed, sp.Seed^0x9e3779b97f4a7c15)),
	}
	out.Branch, out.Path, out.Err = worktree.Resolve(tm, fixed, sp.Inputs, tctx)
	if sp.FixedPath != "" {
		out.Path = sp.FixedPath
	}
	return out
}

// WorktreeSeqNames is a path template's <seq:…> counters — what a create
// that used the template bumps once it succeeded (PrefixSeqNames' pattern).
func WorktreeSeqNames(pathTmpl string) []string {
	return worktree.Templates{Path: pathTmpl}.SeqNames()
}
