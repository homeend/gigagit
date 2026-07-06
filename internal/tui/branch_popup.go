package tui

import (
	"math/rand/v2"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/template"
	"github.com/homeend/gigagit/internal/worktree"
)

// branchPopup holds the in-flight create-branch dialog.
type branchPopup struct {
	popupMax
	startPoint  string    // selected branch the new one is based on
	name        textfield // typed branch name
	switchAfter bool      // B: smart-switch to the branch after creating it

	prefixSeqNames []string // <seq> counters from a chosen prefix, bumped on create
}

// openBranchPopup builds the popup for the currently-selected branch. Returns
// (model, false) when no branch row is selected.
func (m Model) openBranchPopup(switchAfter bool) (Model, bool) {
	bi, ok := m.backingIndex(panelBranches)
	if !ok {
		return m, false
	}
	m = m.pushLayer(&branchPopup{startPoint: m.branches[bi].Name, switchAfter: switchAfter})
	return m, true
}

// update handles one key while the popup is open. The popup swallows every
// key; ctrl+c still quits so the user is never trapped.
func (p *branchPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
	case tea.KeyEnter:
		if p.name.Value() == "" {
			return m, nil
		}
		op := engine.CreateBranch{Name: p.name.Value(), StartPoint: p.startPoint}
		if p.switchAfter {
			m.pendingSwitchBranch = p.name.Value()
		}
		m.pendingSeqBump = p.prefixSeqNames
		m = m.popLayer()
		return m.startOp(op)
	case tea.KeyCtrlP:
		// The whole popup is a text field, so a plain `p` is name text; ctrl+p
		// (never a branch-name char) opens the prefix picker.
		return m, m.openPrefixPicker(p.resolvePrefix(m), p.onPrefixPicked())
	case tea.KeySpace:
		// Branch names cannot contain spaces; dropping it avoids a guaranteed
		// validation error on create.
	default:
		p.name.HandleEditKey(msg)
	}
	return m, nil
}

// resolvePrefix returns a closure resolving a prefix value against a ctx seeded
// from this popup (parent branch + now + a fresh rand + peeked seqs). It returns
// the resolved string plus the prefix's <seq> names for the create-time bump.
func (p *branchPopup) resolvePrefix(m Model) func(string, map[string]string) (string, []string, error) {
	gitDir := m.gitCommonDir
	parent := p.startPoint
	repo := worktree.RepoName(m.mainWorktreeRoot())
	now := time.Now()
	seed := rand.Uint64()
	return func(value string, inputs map[string]string) (string, []string, error) {
		names := worktree.Templates{Branch: value}.SeqNames()
		ctx := template.Ctx{
			ParentBranch: parent,
			Repo:         repo,
			Seqs:         worktree.PeekSeqs(gitDir, names),
			Now:          func() time.Time { return now },
			Rand:         rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
		}
		out, err := template.Resolve(value, inputs, ctx)
		return out, names, err
	}
}

// onPrefixPicked seeds the name field with the resolved prefix (cursor at end)
// and records the prefix's <seq> names to bump on create.
func (p *branchPopup) onPrefixPicked() func(Model, string, []string) (Model, tea.Cmd) {
	return func(m Model, resolved string, seqNames []string) (Model, tea.Cmd) {
		p.name = newTextField(resolved)
		p.prefixSeqNames = seqNames
		return m.popLayer(), nil
	}
}

// render composites the popup box over the layer beneath.
func (p *branchPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// displayStart shortens a full 40-char hex SHA to 7 chars for display (the op
// still receives the full, unambiguous start-point). Branch names pass through.
func displayStart(s string) string {
	if len(s) != 40 {
		return s
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return s
		}
	}
	return s[:7]
}

// box draws the create-branch dialog (modal box only).
func (p *branchPopup) box(m Model) string {
	var b strings.Builder
	start := displayStart(p.startPoint)
	title := "Create branch from " + start
	if p.switchAfter {
		title = "Create + switch branch from " + start
	}
	b.WriteString(title + "\n\n")
	w, _ := m.overlayDims()
	b.WriteString(viewField("name: ", p.name, true, popupContentWidth(w)) + "\n\n")
	b.WriteString("[type] name  [ctrl+p] use prefix  [enter] create  [esc] cancel")
	return modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}
