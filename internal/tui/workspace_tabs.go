package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// The grouped tabs' interactions across members (spec §5): a key or a
// click that ACTS on a row of another member first shows that member (a
// look into the worktree the row names), then runs there — the rule the
// consoles follow. Navigation keys never switch. A heading click adopts
// into the member's last-viewed worktree, else its main checkout.

// cursorRowMember is the member of the cursor's row in panel p, whether it
// is a heading, and the worktree path the row names ("" when none).
func (m Model) cursorRowMember(p panel) (member int, heading bool, path string, ok bool) {
	if !m.groupedPanel(p) {
		return 0, false, "", false
	}
	idx := m.displayIndices(p)
	sel := m.sel[p]
	if sel < 0 || sel >= len(idx) {
		return 0, false, "", false
	}
	switch p {
	case panelBranches:
		ents := m.branchEntries()
		if idx[sel] >= len(ents) {
			return 0, false, "", false
		}
		e := ents[idx[sel]]
		if e.heading {
			return e.member, true, "", true
		}
		mm := m.asMember(e.member)
		if e.br >= 0 && e.br < len(mm.branches) {
			if wt, has := mm.worktreePathOf(mm.branches[e.br].Name); has {
				path = wt
			}
		}
		return e.member, false, path, true
	case panelWorktrees:
		ents := m.worktreeEntries()
		if idx[sel] >= len(ents) {
			return 0, false, "", false
		}
		e := ents[idx[sel]]
		if e.heading {
			return e.member, true, "", true
		}
		mm := m.asMember(e.member)
		if e.wt >= 0 && e.wt < len(mm.worktrees) {
			path = mm.worktrees[e.wt].Path
		}
		return e.member, false, path, true
	}
	return 0, false, "", false
}

// memberSwitchKey reports a key that ACTS ON THE ROW under the cursor
// (the Branches/Worktrees row keys: enter, switch, branch, worktree,
// delete, mark, rename, the session row keys, the . menu, go-to-worktree,
// solo, the per-group branch-filter slots) and so switches to the row's
// member first. Every other key — navigation, focus moves, global actions
// such as undo, push, commit, find current — acts on the repository on
// screen and never switches.
func memberSwitchKey(key string) bool {
	switch key {
	case "enter", ".", "s", "b", "B", "w", "W", "d", "D", "m", "e", "x", "X", "p", "ctrl+s", "ctrl+g",
		"alt+1", "alt+2", "alt+3", "alt+4", "alt+5":
		return true
	}
	return false
}

// switchForCursorRow runs before an action key on a grouped tab: with the
// cursor on another member's row, the panels switch to that member (a
// look, landing on the worktree the row names) and the key then acts
// there. stop = the key is consumed here: a heading row, a refused switch
// (said on the status line), or `m` (pair) with a mark in the live member
// (a compare never crosses repositories).
func (m Model) switchForCursorRow(key string) (Model, bool) {
	p := m.focus
	member, heading, path, ok := m.cursorRowMember(p)
	if !ok || heading || member == m.liveMember() {
		return m, heading && ok
	}
	if key == "m" && m.mark != nil {
		// Pairing the live member's mark with another member's row would be
		// a compare across repositories: refused in place (spec §5).
		m.statusMsg = i18n.T("compare works within one repository")
		return m, true
	}
	r := m.ws.members[member]
	if r.missing || r.svc == nil {
		m.statusMsg = i18n.T("%s (missing)", r.name)
		return m, true
	}
	rowKey := m.panelSelKey(p) // the row acted on, by its member-qualified key
	nm, swapped := m.switchRepo(r, path, true)
	if !swapped {
		return nm, true
	}
	nm = nm.restorePanelSel(p, rowKey) // the groups may have changed size (a sleeping member shows no note sub-rows)
	return nm, false
}

// clickHeading is a click on member heading row `row` of panel p: an adopt
// into that member's last-viewed worktree, else its main checkout; the
// cursor moves to the member's first row.
func (m Model) clickHeading(p panel, row int) (Model, tea.Cmd) {
	m.sel[p] = row
	member, heading, _, ok := m.cursorRowMember(p)
	if !ok || !heading {
		return m, nil
	}
	m.sel[p] = min(row+1, max(0, m.panelLen(p)-1))
	r := m.ws.members[member]
	if r.missing || r.svc == nil {
		m.statusMsg = i18n.T("%s (missing)", r.name)
		return m, nil
	}
	if r == m.liveRepo() {
		return m, nil
	}
	tm, cmd := m.guardedReRoot(r.defaultWorktree(), false, true)
	return tm.(Model), cmd
}
