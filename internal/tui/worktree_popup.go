package tui

import (
	"math/rand/v2"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/template"
	"github.com/gigagit/gg/internal/worktree"
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
	existing   bool   // checkout the startPoint branch itself; no new branch
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

// fixedBranch returns the verbatim branch when one is fixed: the selection in
// existing mode, the live buffer while editing, or a confirmed hand-edit.
func (p *worktreePopup) fixedBranch() string {
	if p.existing {
		return p.startPoint
	}
	if p.state == stEdit {
		return p.editBuf
	}
	return p.branchOverride
}

// recompute refreshes the preview from the current fields/state. A confirmed
// hand-edit (branchOverride) wins over the template; while actively editing, the
// live editBuf is shown.
func (p *worktreePopup) recompute() {
	fixed := p.fixedBranch()
	tm := worktree.Templates{Branch: p.branchTmpl, Path: p.pathTmpl}
	p.previewBranch, p.previewPath, p.previewErr = worktree.Resolve(tm, fixed, p.inputs, p.tctx())
}

// openWorktreePopup builds a popup for the currently-selected branch. In
// existing mode the popup checks out that branch itself (no new branch): the
// branch template is bypassed and only the path template's fields/counters
// apply. Returns (model, false) if there is no branch to act on.
func (m Model) openWorktreePopup(existing bool) (Model, bool) {
	if len(m.branches) == 0 {
		return m, false
	}
	bt := m.cfg.Worktree.DefaultBranchTemplate
	pt := m.cfg.Worktree.PathTemplate

	tm := worktree.Templates{Branch: bt, Path: pt}
	if existing {
		tm = worktree.Templates{Path: pt} // branch template bypassed entirely
	}
	labels := tm.Labels()
	seqNames := tm.SeqNames()

	bi, ok := m.backingIndex(panelBranches)
	if !ok {
		return m, false
	}
	p := &worktreePopup{
		startPoint: m.branches[bi].Name,
		existing:   existing,
		branchTmpl: bt,
		pathTmpl:   pt,
		repoName:   worktree.RepoName(m.currentWorktree),
		labels:     labels,
		inputs:     map[string]string{},
		seqNames:   seqNames,
		seqs:       worktree.PeekSeqs(m.gitCommonDir, seqNames),
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
	m = m.pushOverlay(p)
	return m, true
}

// update handles one key while the popup is open.
func (p *worktreePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	// ctrl+c always quits, even from inside the popup, so a user can never be
	// trapped (Esc also cancels from every state).
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch p.state {
	case stInput:
		switch msg.Type {
		case tea.KeyEsc:
			m = m.popOverlay()
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
			m = m.popOverlay()
		case "e":
			if p.existing {
				return m, nil // the branch IS the point of existing mode
			}
			p.editBuf = p.previewBranch
			p.state = stEdit
			p.recompute()
		case "w", "enter":
			return m.startCreateFromPopup(p, false)
		case "W":
			return m.startCreateFromPopup(p, true)
		}
		return m, nil
	}
}

// render composites the popup box over the layer beneath.
func (p *worktreePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// box draws the create-worktree dialog (fields, live preview, and
// state-specific key hints).
func (p *worktreePopup) box(m Model) string {
	var b strings.Builder
	title := "Create worktree from " + p.startPoint
	if p.existing {
		title = "Create worktree for " + p.startPoint
	}
	b.WriteString(title + "\n\n")

	for i, lbl := range p.labels {
		cursor := "  "
		if p.state == stInput && i == p.fieldIdx {
			cursor = "> "
		}
		b.WriteString(cursor + lbl + ": " + p.inputs[lbl] + "\n")
	}
	if len(p.labels) > 0 {
		b.WriteString("\n")
	}

	branch := p.previewBranch
	if p.state == stEdit {
		branch = p.editBuf
	}
	b.WriteString("branch: " + branch + "\n")
	b.WriteString("path:   " + p.previewPath + "\n")
	if p.previewErr != nil {
		b.WriteString("\n⚠ " + p.previewErr.Error() + "\n")
	}

	b.WriteString("\n")
	switch p.state {
	case stInput:
		b.WriteString("[type] value  [tab/enter] next field  [esc] cancel")
	case stEdit:
		b.WriteString("[type] edit name  [enter] done  [esc] discard")
	default:
		if p.existing {
			b.WriteString("[w] create  [W] create & switch  [esc] cancel")
		} else {
			b.WriteString("[w] create  [W] create & switch  [e] edit name  [esc] cancel")
		}
	}

	// Fixed, comfortably-wide content width so a long branch/path wraps (full name
	// stays visible) instead of stretching the box past the terminal edge. Capped
	// to leave a margin on each side for the centered overlay.
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	return modalStyle.Width(inner).Render(b.String()) + "\n"
}

// startCreateFromPopup launches the CreateWorktree op for the previewed names,
// closes the popup, and records which <seq> counters to bump on success. A
// preview error refuses to launch.
func (m Model) startCreateFromPopup(p *worktreePopup, switchAfter bool) (Model, tea.Cmd) {
	if p.previewErr != nil {
		m.statusMsg = "cannot create: " + p.previewErr.Error()
		return m, nil
	}
	m.pendingSeqBump = p.consumedSeqNames()
	m.pendingSwitch = switchAfter
	m = m.popOverlay()
	return m.startOp(p.createOp())
}

// createOp builds the engine operation from the (already-resolved) preview, so
// the worktree that gets created is exactly what the preview showed.
func (p *worktreePopup) createOp() engine.Operation {
	if p.existing {
		return engine.CreateWorktreeForBranch{Branch: p.previewBranch, Path: p.previewPath}
	}
	return engine.CreateWorktree{
		StartPoint: p.startPoint,
		Branch:     p.previewBranch,
		Path:       p.previewPath,
	}
}

// consumedSeqNames returns the <seq> counters the created names actually used.
// In existing mode and after a confirmed hand-edit the branch template is
// bypassed, so only the path template's <seq> tokens are consumed.
func (p *worktreePopup) consumedSeqNames() []string {
	if p.existing || p.branchOverride != "" {
		return worktree.Templates{Path: p.pathTmpl}.SeqNames()
	}
	return p.seqNames
}
