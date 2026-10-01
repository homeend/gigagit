// Package worktree holds the frontend-agnostic logic for resolving a new
// worktree's branch and path from configured templates, shared by the TUI popup
// and the CLI so neither duplicates it.
package worktree

import (
	"path/filepath"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/template"
)

// Templates is the branch + path template pair for worktree creation.
type Templates struct {
	Branch string
	Path   string
}

// Labels returns the distinct <user:LABEL> labels across both templates, in
// order of first appearance (branch first). A frontend collects a value for each.
func (t Templates) Labels() []string {
	var out []string
	for _, l := range template.UserLabels(t.Branch) {
		out = appendDistinct(out, l)
	}
	for _, l := range template.UserLabels(t.Path) {
		out = appendDistinct(out, l)
	}
	return out
}

// SeqNames returns the distinct <seq:NAME> names across both templates.
func (t Templates) SeqNames() []string {
	var out []string
	for _, n := range template.SeqNames(t.Branch) {
		out = appendDistinct(out, n)
	}
	for _, n := range template.SeqNames(t.Path) {
		out = appendDistinct(out, n)
	}
	return out
}

// Resolve resolves the branch then the path (two-phase: the path template is
// resolved with ctx.Branch set, so template's "<branch> is path-only" rule
// holds). When fixedBranch != "" it is used verbatim as the branch (edit mode).
func Resolve(t Templates, fixedBranch string, inputs map[string]string, ctx template.Ctx) (branch, path string, err error) {
	if fixedBranch != "" {
		branch = fixedBranch
	} else {
		branch, err = template.Resolve(t.Branch, inputs, ctx)
		if err != nil {
			return "", "", err
		}
	}
	ctx.Branch = branch
	path, err = template.Resolve(t.Path, inputs, ctx)
	if err != nil {
		return branch, "", err
	}
	return branch, path, nil
}

// RepoName returns the <repo> token value for a worktree root path.
func RepoName(worktreeRoot string) string {
	return filepath.Base(worktreeRoot)
}

// ResolvePrefix resolves a branch-prefix template against ctx — the one
// resolver behind the TUI's prefix picker and gg prefix resolve. A <seq>
// counter ctx does not carry yet is peeked from gitCommonDir (never into the
// caller's map). It returns the name and the prefix's counter names, which
// the caller bumps once the name is used.
func ResolvePrefix(value string, inputs map[string]string, ctx template.Ctx, gitCommonDir string) (string, []string, error) {
	names := Templates{Branch: value}.SeqNames()
	seqs := make(map[string]int, len(ctx.Seqs)+len(names))
	for k, v := range ctx.Seqs {
		seqs[k] = v
	}
	for _, n := range names {
		if _, ok := seqs[n]; !ok {
			seqs[n] = config.PeekSeq(gitCommonDir, n)
		}
	}
	ctx.Seqs = seqs
	out, err := template.Resolve(value, inputs, ctx)
	return out, names, err
}

// PeekSeqs reads the next value of each named counter (no mutation).
func PeekSeqs(gitCommonDir string, names []string) map[string]int {
	out := make(map[string]int, len(names))
	for _, n := range names {
		out[n] = config.PeekSeq(gitCommonDir, n)
	}
	return out
}

func appendDistinct(dst []string, s string) []string {
	for _, x := range dst {
		if x == s {
			return dst
		}
	}
	return append(dst, s)
}
