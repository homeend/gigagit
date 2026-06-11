package tui

import (
	"math/rand/v2"
	"path/filepath"
	"time"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/template"
)

// popupState is the worktree-create popup's mode.
type popupState int

const (
	stInput  popupState = iota // collecting <user:LABEL> field values
	stAction                   // preview shown; choose create / edit / cancel
	stEdit                     // free-editing the resolved branch name
)

// worktreePopup holds the in-flight create-worktree dialog. The <random>/<date>
// sources are fixed at open (seed/now) so the preview is stable across keystrokes
// and the created branch matches what was shown.
type worktreePopup struct {
	startPoint string // selected branch = <parent-branch>
	branchTmpl string
	pathTmpl   string
	repoName   string

	labels   []string          // distinct <user:> labels, in order
	inputs   map[string]string // label -> value
	fieldIdx int               // focused label (stInput)

	seqNames []string       // distinct <seq> names referenced by the templates
	seqs     map[string]int // peeked counter values (reused for preview + create)

	seed uint64    // fixes <random> so the preview does not jitter
	now  time.Time // fixes <date>

	state          popupState
	editBuf        string // stEdit working buffer
	branchOverride string // a confirmed hand-edited branch name; "" = use the template

	previewBranch string
	previewPath   string
	previewErr    error
}

// tctx builds a fresh template.Ctx. A new Rand is created from the fixed seed on
// every call, so repeated resolves of the same fields yield identical output.
func (p *worktreePopup) tctx() template.Ctx {
	return template.Ctx{
		ParentBranch: p.startPoint,
		Repo:         p.repoName,
		Seqs:         p.seqs,
		Now:          func() time.Time { return p.now },
		Rand:         rand.New(rand.NewPCG(p.seed, p.seed^0x9e3779b97f4a7c15)),
	}
}

// resolveWorktreeNames resolves the branch then the path. The path template is
// resolved with ctx.Branch set, so A1's "<branch> is path-template-only" rule is
// satisfied. When fixedBranch != "" (edit mode) it is used verbatim as the
// branch instead of resolving branchTmpl.
func resolveWorktreeNames(branchTmpl, pathTmpl, fixedBranch string, inputs map[string]string, ctx template.Ctx) (branch, path string, err error) {
	if fixedBranch != "" {
		branch = fixedBranch
	} else {
		branch, err = template.Resolve(branchTmpl, inputs, ctx)
		if err != nil {
			return "", "", err
		}
	}
	ctx.Branch = branch
	path, err = template.Resolve(pathTmpl, inputs, ctx)
	if err != nil {
		return branch, "", err
	}
	return branch, path, nil
}

// recompute refreshes the preview from the current fields/state. A confirmed
// hand-edit (branchOverride) wins over the template; while actively editing, the
// live editBuf is shown.
func (p *worktreePopup) recompute() {
	fixed := p.branchOverride
	if p.state == stEdit {
		fixed = p.editBuf
	}
	p.previewBranch, p.previewPath, p.previewErr = resolveWorktreeNames(p.branchTmpl, p.pathTmpl, fixed, p.inputs, p.tctx())
}

// distinctAppend appends s to dst if not already present.
func distinctAppend(dst []string, s string) []string {
	for _, x := range dst {
		if x == s {
			return dst
		}
	}
	return append(dst, s)
}

// peekSeqs reads the next value of each named counter (no mutation).
func peekSeqs(gitCommonDir string, names []string) map[string]int {
	out := make(map[string]int, len(names))
	for _, n := range names {
		out[n] = config.PeekSeq(gitCommonDir, n)
	}
	return out
}

// repoNameFrom returns the <repo> token value for a worktree root path.
func repoNameFrom(root string) string {
	return filepath.Base(root)
}
