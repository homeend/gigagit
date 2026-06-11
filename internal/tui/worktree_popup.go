package tui

import (
	"math/rand/v2"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/engine"
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

// openWorktreePopup builds a popup for the currently-selected branch. Returns
// (model, false) if there is no branch to base it on.
func (m Model) openWorktreePopup() (Model, bool) {
	if len(m.branches) == 0 {
		return m, false
	}
	bt := m.cfg.Worktree.DefaultBranchTemplate
	pt := m.cfg.Worktree.PathTemplate

	var labels []string
	for _, l := range template.UserLabels(bt) {
		labels = distinctAppend(labels, l)
	}
	for _, l := range template.UserLabels(pt) {
		labels = distinctAppend(labels, l)
	}
	var seqNames []string
	for _, n := range template.SeqNames(bt) {
		seqNames = distinctAppend(seqNames, n)
	}
	for _, n := range template.SeqNames(pt) {
		seqNames = distinctAppend(seqNames, n)
	}

	p := &worktreePopup{
		startPoint: m.branches[m.sel[panelBranches]].Name,
		branchTmpl: bt,
		pathTmpl:   pt,
		repoName:   repoNameFrom(m.currentWorktree),
		labels:     labels,
		inputs:     map[string]string{},
		seqNames:   seqNames,
		seqs:       peekSeqs(m.gitCommonDir, seqNames),
		seed:       rand.Uint64(),
		now:        time.Now(),
	}
	for _, l := range labels {
		p.inputs[l] = ""
	}
	if len(labels) > 0 {
		p.state = stInput
	} else {
		p.state = stAction
	}
	p.recompute()
	m.popup = p
	return m, true
}

// updatePopupKey handles one key while the popup is open.
func (m Model) updatePopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.popup
	switch p.state {
	case stInput:
		switch msg.Type {
		case tea.KeyEsc:
			m.popup = nil
		case tea.KeyEnter, tea.KeyTab:
			p.fieldIdx++
			if p.fieldIdx >= len(p.labels) {
				p.fieldIdx = len(p.labels) - 1
				p.state = stAction
			}
			p.recompute()
		case tea.KeyBackspace:
			lbl := p.labels[p.fieldIdx]
			if r := []rune(p.inputs[lbl]); len(r) > 0 {
				p.inputs[lbl] = string(r[:len(r)-1])
			}
			p.recompute()
		case tea.KeyRunes:
			p.inputs[p.labels[p.fieldIdx]] += string(msg.Runes)
			p.recompute()
		case tea.KeySpace:
			p.inputs[p.labels[p.fieldIdx]] += " "
			p.recompute()
		}
		return m, nil
	case stEdit:
		switch msg.Type {
		case tea.KeyEnter:
			p.branchOverride = p.editBuf
			p.state = stAction
			p.recompute()
		case tea.KeyEsc:
			p.state = stAction
			p.recompute()
		case tea.KeyBackspace:
			if r := []rune(p.editBuf); len(r) > 0 {
				p.editBuf = string(r[:len(r)-1])
			}
			p.recompute()
		case tea.KeyRunes:
			p.editBuf += string(msg.Runes)
			p.recompute()
		case tea.KeySpace:
			p.editBuf += " "
			p.recompute()
		}
		return m, nil
	default: // stAction
		switch msg.String() {
		case "esc":
			m.popup = nil
		case "e":
			p.editBuf = p.previewBranch
			p.state = stEdit
			p.recompute()
		case "w", "enter":
			return m.startCreateFromPopup()
		}
		return m, nil
	}
}

// startCreateFromPopup launches the CreateWorktree op for the previewed names,
// closes the popup, and records which <seq> counters to bump on success. A
// preview error refuses to launch.
func (m Model) startCreateFromPopup() (tea.Model, tea.Cmd) {
	p := m.popup
	if p.previewErr != nil {
		m.statusMsg = "cannot create: " + p.previewErr.Error()
		return m, nil
	}
	op := engine.CreateWorktree{
		StartPoint: p.startPoint,
		Branch:     p.previewBranch,
		Path:       p.previewPath,
	}
	m.pendingSeqBump = p.seqNames
	m.popup = nil
	return m.startOp(op)
}

// renderWorktreePopup gets its real body in Task 8.
func (m Model) renderWorktreePopup() string {
	return "create worktree…\n"
}
