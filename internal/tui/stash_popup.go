package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// stashFileItem is one candidate file in the stash-create popup.
type stashFileItem struct {
	path      string
	included  bool
	untracked bool
}

// stashPopup is the create-stash dialog: a name field plus a checklist of the
// working tree's unstaged/untracked files.
type stashPopup struct {
	name  textfield
	files []stashFileItem
	field int // 0 = name, 1 = file list
	sel   int // cursor in the file list
}

// op assembles the engine.Stash for the currently-checked files. ok is false
// when nothing is checked (caller refuses to submit).
func (p *stashPopup) op() (engine.Stash, bool) {
	var paths []string
	untracked := false
	for _, f := range p.files {
		if f.included {
			paths = append(paths, f.path)
			untracked = untracked || f.untracked
		}
	}
	if len(paths) == 0 {
		return engine.Stash{}, false
	}
	return engine.Stash{Message: p.name.Value(), Paths: paths, IncludeUntracked: untracked}, true
}

// stashCandidates returns the files eligible for stashing: untracked files and
// files with unstaged content (a fully-staged file is excluded). Order follows
// the Status list.
func stashCandidates(st model.WorkingTreeStatus) []stashFileItem {
	var out []stashFileItem
	for _, f := range st.Files {
		untracked := f.Kind == model.KindUntracked
		hasUnstaged := untracked || (f.Unstaged != '.' && f.Unstaged != 0)
		if f.Kind == model.KindUnmerged || !hasUnstaged {
			continue
		}
		out = append(out, stashFileItem{path: f.Path, untracked: untracked})
	}
	return out
}

// openStashPopup builds the popup. Returns (model, false) when nothing is
// eligible to stash.
func (m Model) openStashPopup() (Model, bool) {
	cand := stashCandidates(m.status)
	if len(cand) == 0 {
		return m, false
	}
	anyMarked := len(m.fileMarks) > 0
	for i := range cand {
		cand[i].included = !anyMarked || m.fileMarks[cand[i].path]
	}
	m = m.pushLayer(&stashPopup{name: newTextField("WIP on " + m.status.Branch), files: cand, field: 1})
	return m, true
}

// update handles one key while the popup is open. It swallows every key
// (no fallthrough), per the popup checklist.
func (p *stashPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case "esc":
		m = m.popLayer()
		return m, nil
	case "ctrl+s":
		op, ok := p.op()
		if !ok {
			m.statusMsg = "select at least one file"
			return m, nil
		}
		if strings.TrimSpace(op.Message) == "" {
			op.Message = "WIP on " + m.status.Branch
		}
		for _, path := range op.Paths { // clear marks we just stashed
			delete(m.fileMarks, path)
		}
		m = m.popLayer()
		return m.startOp(op)
	case "tab", "shift+tab":
		p.field = 1 - p.field
		return m, nil
	}
	if p.field == 1 { // file list
		switch msg.String() {
		case "up", "k":
			if p.sel > 0 {
				p.sel--
			}
		case "down", "j":
			if p.sel < len(p.files)-1 {
				p.sel++
			}
		case " ", "space":
			if p.sel >= 0 && p.sel < len(p.files) {
				p.files[p.sel].included = !p.files[p.sel].included
			}
		}
		return m, nil
	}
	// name field
	p.name.HandleEditKey(msg)
	return m, nil
}

// render composites the stash dialog over the layer beneath.
func (p *stashPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// box draws the stash name field and file checklist (modal box only).
func (p *stashPopup) box(m Model) string {
	w, _ := m.overlayDims()
	var b strings.Builder
	b.WriteString("Stash changes\n\n")
	b.WriteString(viewField("name: ", p.name, p.field == 0, popupContentWidth(w)) + "\n\n")
	for i, f := range p.files {
		box := "[ ]"
		if f.included {
			box = "[x]"
		}
		row := box + " " + f.path
		if p.field == 1 && i == p.sel {
			b.WriteString(selectedRow.Render("> "+row) + "\n")
		} else {
			b.WriteString("  " + row + "\n")
		}
	}
	b.WriteString("\n[space] toggle  [tab] name/files  [ctrl+s] stash  [esc] cancel")
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
