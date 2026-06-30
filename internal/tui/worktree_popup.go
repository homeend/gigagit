package tui

import (
	"math/rand/v2"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/template"
	"github.com/homeend/gigagit/internal/worktree"
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
	startPoint string // selected branch = <parent-branch>; a commit SHA in fromCommit mode
	existing   bool   // checkout the startPoint branch itself; no new branch
	fromCommit bool   // start point is a commit; the branch name is user-typed (no template)
	branchTmpl string
	pathTmpl   string
	repoName   string

	labels   []string             // distinct <user:> labels, in order
	inputs   map[string]textfield // label -> value
	fieldIdx int                  // focused label (stInput)

	seqNames []string       // distinct <seq> names referenced by the templates
	seqs     map[string]int // peeked counter values (reused for preview + create)

	gitCommonDir   string   // for peeking a chosen prefix's own <seq> counters
	prefixSeqNames []string // <seq> counters from a chosen prefix, bumped on create

	seed uint64    // fixes <random> so the preview does not jitter
	now  time.Time // fixes <date>

	state          popupState
	editBuf        textfield // stEdit working buffer
	branchOverride string    // a confirmed hand-edited branch name; "" = use the template

	previewBranch string
	previewPath   string
	previewErr    error

	runHook bool // run the configured post-create hook on create (default true)
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
		return p.editBuf.Value()
	}
	return p.branchOverride
}

// recompute refreshes the preview from the current fields/state. A confirmed
// hand-edit (branchOverride) wins over the template; while actively editing, the
// live editBuf is shown.
func (p *worktreePopup) recompute() {
	fixed := p.fixedBranch()
	tm := worktree.Templates{Branch: p.branchTmpl, Path: p.pathTmpl}
	vals := make(map[string]string, len(p.inputs))
	for l, f := range p.inputs {
		vals[l] = f.Value()
	}
	p.previewBranch, p.previewPath, p.previewErr = worktree.Resolve(tm, fixed, vals, p.tctx())
}

// openWorktreePopup builds a popup for the currently-selected branch. In
// existing mode the popup checks out that branch itself (no new branch): the
// branch template is bypassed and only the path template's fields/counters
// apply. Returns (model, false) if there is no branch to act on.
// mainWorktreeRoot returns the main worktree's root for resolving the <repo>
// token and the relative-path base (git lists the main worktree first), falling
// back to the current worktree when the list isn't loaded yet. This must match
// engine.resolveNewWorktreePath's anchor so the popup preview reflects where the
// worktree actually lands — anchoring on the current (linked) worktree would
// double the ".worktrees" segment.
func (m Model) mainWorktreeRoot() string {
	if len(m.worktrees) > 0 && m.worktrees[0].Path != "" {
		return m.worktrees[0].Path
	}
	return m.currentWorktree
}

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
		startPoint:   m.branches[bi].Name,
		existing:     existing,
		branchTmpl:   bt,
		pathTmpl:     pt,
		repoName:     worktree.RepoName(m.mainWorktreeRoot()),
		labels:       labels,
		inputs:       map[string]textfield{},
		seqNames:     seqNames,
		seqs:         worktree.PeekSeqs(m.gitCommonDir, seqNames),
		gitCommonDir: m.gitCommonDir,
		seed:         rand.Uint64(),
		now:          time.Now(),
		runHook:      true,
	}
	for _, l := range labels {
		p.inputs[l] = textfield{}
	}
	if len(labels) > 0 {
		p.state = stInput
	} else {
		p.state = stAction
	}
	p.recompute()
	m = m.pushLayer(p)
	return m, true
}

// openWorktreeAt opens the create-worktree dialog based at startPoint (a commit
// SHA or a tag/ref). The new branch name is NOT templated — the popup starts in
// branch-edit mode seeded with prefillBranch ("" = empty, user types it). The
// path resolves from that branch (sanitized per-OS into a single segment).
// engine.CreateWorktree creates the branch at startPoint and the worktree in one
// step.
func (m Model) openWorktreeAt(startPoint, prefillBranch string) Model {
	bt := m.cfg.Worktree.DefaultBranchTemplate
	pt := m.cfg.Worktree.PathTemplate
	tm := worktree.Templates{Branch: bt, Path: pt}
	labels := tm.Labels()
	seqNames := tm.SeqNames()
	p := &worktreePopup{
		startPoint:   startPoint,
		fromCommit:   true,
		branchTmpl:   bt,
		pathTmpl:     pt,
		repoName:     worktree.RepoName(m.mainWorktreeRoot()),
		labels:       labels,
		inputs:       map[string]textfield{},
		seqNames:     seqNames,
		seqs:         worktree.PeekSeqs(m.gitCommonDir, seqNames),
		gitCommonDir: m.gitCommonDir,
		seed:         rand.Uint64(),
		now:          time.Now(),
		state:        stEdit,                      // user edits the branch name immediately
		editBuf:      newTextField(prefillBranch), // seeded default (e.g. the tag name)
		runHook:      true,
	}
	for _, l := range labels {
		p.inputs[l] = textfield{}
	}
	p.recompute()
	return m.pushLayer(p)
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
			m = m.popLayer()
		case tea.KeyEnter, tea.KeyTab:
			p.fieldIdx++
			if p.fieldIdx >= len(p.labels) {
				p.fieldIdx = len(p.labels) - 1
				p.state = stAction
			}
			p.recompute()
		default:
			lbl := p.labels[p.fieldIdx]
			f := p.inputs[lbl]
			if f.HandleEditKey(msg) {
				p.inputs[lbl] = f
				p.recompute()
			}
		}
		return m, nil
	case stEdit:
		switch msg.Type {
		case tea.KeyEnter:
			p.branchOverride = p.editBuf.Value()
			p.state = stAction
			p.recompute()
		case tea.KeyEsc:
			p.state = stAction
			p.recompute()
		default:
			if p.editBuf.HandleEditKey(msg) {
				p.recompute()
			}
		}
		return m, nil
	default: // stAction
		switch msg.String() {
		case "esc":
			m = m.popLayer()
		case "e":
			if p.existing {
				return m, nil // the branch IS the point of existing mode
			}
			p.editBuf = newTextField(p.previewBranch)
			p.state = stEdit
			p.recompute()
		case "p":
			if p.existing {
				return m, nil // existing mode checks out the branch as-is; no new name
			}
			return m, m.openPrefixPicker(p.resolvePrefix(), p.onPrefixPicked())
		case "w", "enter":
			return m.startCreateFromPopup(p, false)
		case "W":
			return m.startCreateFromPopup(p, true)
		case "h":
			if m.cfg.Worktree.PostCreateHook != "" {
				p.runHook = !p.runHook
			}
			return m, nil
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
	w, _ := m.overlayDims()
	cw := popupContentWidth(w)
	var b strings.Builder
	title := "Create worktree from " + displayStart(p.startPoint)
	if p.existing {
		title = "Create worktree for " + p.startPoint
	}
	b.WriteString(title + "\n\n")

	for i, lbl := range p.labels {
		focused := p.state == stInput && i == p.fieldIdx
		cursor := "  "
		if focused {
			cursor = "> "
		}
		f := p.inputs[lbl]
		b.WriteString(viewField(cursor+lbl+": ", f, focused, cw) + "\n")
	}
	if len(p.labels) > 0 {
		b.WriteString("\n")
	}

	if p.state == stEdit {
		b.WriteString(viewField("branch: ", p.editBuf, true, cw) + "\n")
	} else {
		b.WriteString("branch: " + p.previewBranch + "\n")
	}
	b.WriteString("path:   " + p.previewPath + "\n")
	if p.previewErr != nil {
		b.WriteString("\n⚠ " + p.previewErr.Error() + "\n")
	}

	b.WriteString("\n")
	if p.state == stAction && m.cfg.Worktree.PostCreateHook != "" {
		mark := "[x]"
		if !p.runHook {
			mark = "[ ]"
		}
		b.WriteString(mark + " run post-create hook  ([h] toggle)\n")
	}
	switch p.state {
	case stInput:
		b.WriteString("[type] value  [tab/enter] next field  [esc] cancel")
	case stEdit:
		b.WriteString("[type] edit name  [enter] done  [esc] discard")
	default:
		if p.existing {
			b.WriteString("[w] create  [W] create & switch  [h] hook  [esc] cancel")
		} else {
			b.WriteString("[w] create  [W] create & switch  [e] edit name  [p] use a prefix  [h] hook  [esc] cancel")
		}
	}

	// Fixed, comfortably-wide content width so a long branch/path wraps (full name
	// stays visible) instead of stretching the box past the terminal edge. Capped
	// to leave a margin on each side for the centered overlay.
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}

// startCreateFromPopup launches the CreateWorktree op for the previewed names,
// closes the popup, and records which <seq> counters to bump on success. A
// preview error refuses to launch.
func (m Model) startCreateFromPopup(p *worktreePopup, switchAfter bool) (Model, tea.Cmd) {
	if p.fromCommit && strings.TrimSpace(p.branchOverride) == "" {
		m.statusMsg = "branch name required"
		return m, nil
	}
	if p.previewErr != nil {
		m.statusMsg = "cannot create: " + p.previewErr.Error()
		return m, nil
	}
	m.pendingSeqBump = p.consumedSeqNames()
	m.pendingSwitch = switchAfter
	m = m.popLayer()
	hook := ""
	if p.runHook {
		hook = m.cfg.Worktree.PostCreateHook
	}
	return m.startOp(p.createOp(hook))
}

// createOp builds the engine operation from the (already-resolved) preview, so
// the worktree that gets created is exactly what the preview showed. hook is the
// post-create hook script to pass through ("" = skip).
func (p *worktreePopup) createOp(hook string) engine.Operation {
	if p.existing {
		return engine.CreateWorktreeForBranch{Branch: p.previewBranch, Path: p.previewPath, PostCreateHook: hook}
	}
	return engine.CreateWorktree{
		StartPoint:     p.startPoint,
		Branch:         p.previewBranch,
		Path:           p.previewPath,
		PostCreateHook: hook,
	}
}

// consumedSeqNames returns the <seq> counters the created names actually used.
// In existing mode and after a confirmed hand-edit the branch template is
// bypassed, so only the path template's <seq> tokens are consumed. A chosen
// prefix's own <seq> names are always unioned in.
func (p *worktreePopup) consumedSeqNames() []string {
	var base []string
	if p.existing || p.branchOverride != "" {
		base = worktree.Templates{Path: p.pathTmpl}.SeqNames()
	} else {
		base = p.seqNames
	}
	return appendDistinctAll(base, p.prefixSeqNames)
}

// resolvePrefix resolves a prefix value against this popup's fixed ctx, peeking
// any prefix-only <seq> counters into the ctx snapshot so the result is stable.
func (p *worktreePopup) resolvePrefix() func(string, map[string]string) (string, []string, error) {
	return func(value string, inputs map[string]string) (string, []string, error) {
		names := worktree.Templates{Branch: value}.SeqNames()
		ctx := p.tctx()
		for _, n := range names {
			if _, ok := ctx.Seqs[n]; !ok {
				if ctx.Seqs == nil {
					ctx.Seqs = map[string]int{}
				}
				ctx.Seqs[n] = config.PeekSeq(p.gitCommonDir, n)
			}
		}
		out, err := template.Resolve(value, inputs, ctx)
		return out, names, err
	}
}

// onPrefixPicked seeds stEdit with the resolved prefix so the user appends the
// tail, and records the prefix's <seq> names for the create-time bump.
func (p *worktreePopup) onPrefixPicked() func(Model, string, []string) (Model, tea.Cmd) {
	return func(m Model, resolved string, seqNames []string) (Model, tea.Cmd) {
		p.editBuf = newTextField(resolved)
		p.state = stEdit
		p.prefixSeqNames = seqNames
		p.recompute()
		return m.popLayer(), nil
	}
}

// appendDistinctAll appends each of extra not already present in dst.
func appendDistinctAll(dst, extra []string) []string {
	seen := map[string]bool{}
	for _, x := range dst {
		seen[x] = true
	}
	for _, x := range extra {
		if !seen[x] {
			seen[x] = true
			dst = append(dst, x)
		}
	}
	return dst
}
