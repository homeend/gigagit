package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// versionsPopup lists a branch's recorded versions (operations history), or —
// in branch mode, opened from the command palette — every branch that has
// versions, including deleted ones. Layer-stack popup, popupMax-embedding.
// Navigation-first like the repo switcher and the git-config explorer: plain
// keys navigate, enter drills in (branches mode) or opens a compare (versions
// mode), esc backs out (to branches mode when drilled in, else closes).
type versionsPopup struct {
	popupMax
	mode       int
	fromList   bool // versions mode entered by drilling from branch mode: esc goes back
	branch     string
	deleted    bool
	branchRows []model.VersionedBranch
	rows       []model.BranchVersion
	sel        int
	loading    bool
	err        string
}

const (
	versionsModeBranches = iota
	versionsModeVersions
)

// versionsLoadedMsg carries a branch's recorded versions (both the initial
// load and the post-delete re-read).
type versionsLoadedMsg struct {
	gen    int
	branch string
	rows   []model.BranchVersion
	err    error
}

// versionBranchesLoadedMsg carries the branch-mode branch list.
type versionBranchesLoadedMsg struct {
	gen  int
	rows []model.VersionedBranch
	err  error
}

// loadBranchVersionsCmd fetches one branch's recorded versions off the UI
// thread.
func (m Model) loadBranchVersionsCmd(gen int, branch string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		rows, err := svc.BranchVersions(context.Background(), branch)
		return versionsLoadedMsg{gen: gen, branch: branch, rows: rows, err: err}
	}
}

// loadVersionBranchesCmd fetches every branch that has recorded versions off
// the UI thread.
func (m Model) loadVersionBranchesCmd(gen int) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		rows, err := svc.AllVersionBranches(context.Background())
		return versionBranchesLoadedMsg{gen: gen, rows: rows, err: err}
	}
}

// openBranchVersions pushes the popup straight into versions mode for branch
// (the Branches-panel . row: the branch is already known, no need to browse
// the full list first).
func (m Model) openBranchVersions(branch string, deleted, fromList bool) (Model, tea.Cmd) {
	m.versionsGen++
	p := &versionsPopup{mode: versionsModeVersions, branch: branch, deleted: deleted, fromList: fromList, loading: true}
	m = m.pushLayer(p)
	return m, m.loadBranchVersionsCmd(m.versionsGen, branch)
}

// openVersionBranchList pushes the popup in branch mode (the command-palette
// entry: browse every branch that has recorded versions, including deleted
// ones, then drill into one).
func (m Model) openVersionBranchList() (Model, tea.Cmd) {
	m.versionsGen++
	p := &versionsPopup{mode: versionsModeBranches, loading: true}
	m = m.pushLayer(p)
	return m, m.loadVersionBranchesCmd(m.versionsGen)
}

// rowCount is the number of rows in the popup's CURRENT mode — what moveSel
// clamps against.
func (p *versionsPopup) rowCount() int {
	if p.mode == versionsModeBranches {
		return len(p.branchRows)
	}
	return len(p.rows)
}

// moveSel moves the cursor by d, clamped to the current mode's row count.
func (p *versionsPopup) moveSel(d int) {
	n := p.sel + d
	if hi := p.rowCount() - 1; n > hi {
		n = hi
	}
	if n < 0 {
		n = 0
	}
	p.sel = n
}

// update handles all keys while the popup is open. It swallows everything (no
// fallthrough to global handlers), mirroring the other navigation-first
// popups (repoPopup, gitConfigPopup).
func (p *versionsPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		if p.mode == versionsModeVersions && p.fromList {
			return p.backToList(m)
		}
		return m.popLayer(), nil
	case tea.KeyUp:
		p.moveSel(-1)
		return m, nil
	case tea.KeyDown:
		p.moveSel(1)
		return m, nil
	case tea.KeyEnter:
		return p.onEnter(m)
	case tea.KeyRunes:
		switch msg.String() {
		case "j":
			p.moveSel(1)
		case "k":
			p.moveSel(-1)
		case "r":
			if p.mode == versionsModeVersions {
				return p.onRestore(m)
			}
		case "d":
			if p.mode == versionsModeVersions {
				return p.onDelete(m)
			}
		case "y":
			if p.mode == versionsModeVersions {
				return p.onCopy(m)
			}
		}
		return m, nil
	}
	return m, nil
}

// backToList switches a drilled-in versions-mode popup back to branch mode,
// re-triggering the branch-list load. Bumps versionsGen so a still-in-flight
// versions load for the branch being left can't land after the mode switch.
func (p *versionsPopup) backToList(m Model) (Model, tea.Cmd) {
	m.versionsGen++
	p.mode = versionsModeBranches
	p.fromList = false
	p.branch = ""
	p.deleted = false
	p.rows = nil
	p.err = ""
	p.sel = 0
	p.loading = true
	return m, m.loadVersionBranchesCmd(m.versionsGen)
}

// onEnter drills from branch mode into versions mode, or — in versions mode —
// opens the version↔current-tip compare. A deleted branch has no live tip to
// compare against, so it sets a status hint instead of opening a compare.
func (p *versionsPopup) onEnter(m Model) (Model, tea.Cmd) {
	switch p.mode {
	case versionsModeBranches:
		if p.sel < 0 || p.sel >= len(p.branchRows) {
			return m, nil
		}
		row := p.branchRows[p.sel]
		m.versionsGen++
		p.mode = versionsModeVersions
		p.branch = row.Branch
		p.deleted = row.Deleted
		p.fromList = true
		p.rows = nil
		p.err = ""
		p.sel = 0
		p.loading = true
		return m, m.loadBranchVersionsCmd(m.versionsGen, row.Branch)
	case versionsModeVersions:
		if p.sel < 0 || p.sel >= len(p.rows) {
			return m, nil
		}
		if p.deleted {
			m.statusMsg = i18n.T("branch no longer exists — restore it to compare")
			return m, nil
		}
		v := p.rows[p.sel]
		// Both endpoints must be HASHES, never the branch name — a name in
		// Endpoint.Hash would poison the session-lived diff cache, which
		// treats commit↔commit endpoints as immutable (branch_compare.go's
		// openBranchCompare doc comment has the full rationale). branchTipHash
		// falls back to the branch NAME itself when the branch is absent from
		// m.branches (see branch_compare.go's doc comment) — indistinguishable
		// here from "deleted" (no live tip to compare against), so treat it
		// the same way rather than letting a name slip into Endpoint.Hash.
		tip := m.branchTipHash(p.branch)
		if tip == p.branch {
			m.statusMsg = i18n.T("branch no longer exists — restore it to compare")
			return m, nil
		}
		m = m.clearLayers()
		return m.openCompareFiles(
			model.Endpoint{Kind: model.EndpointCommit, Hash: v.Hash},
			model.Endpoint{Kind: model.EndpointCommit, Hash: tip},
		)
	}
	return m, nil
}

// versionShortHash truncates a version's hash to 8 chars for display and for
// the restore-prompt/copy-status text (the row-rendering convention this
// popup uses, 1 char longer than the shortHash 7-char convention elsewhere).
func versionShortHash(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

// onRestore offers the restore choice: reset the branch in place, or create a
// new branch at the version instead (a frontend-only decision — no engine
// Decider round-trip, the tagDeleteRow/copyFilePrompt pattern).
func (p *versionsPopup) onRestore(m Model) (Model, tea.Cmd) {
	if p.sel < 0 || p.sel >= len(p.rows) {
		return m, nil
	}
	v := p.rows[p.sel]
	short := versionShortHash(v.Hash)
	branch, ref, hash := p.branch, v.Ref, v.Hash
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "restore-version-choice",
			Prompt:  i18n.T("Restore %s to %s?", branch, short),
			Options: []string{"Reset branch", "New branch at version", "Cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			switch opt {
			case "Reset branch":
				m = m.clearLayers()
				return m.startOp(engine.RestoreBranchVersion{Branch: branch, Ref: ref})
			case "New branch at version":
				return m.openVersionBranchNamePopup(hash)
			}
			return m, nil
		},
	}
	return m, nil
}

// onDelete confirms then removes one recorded version ref; the popup stays
// open (on the layer stack beneath the modal) and its rows reload once the
// write lands (the gitConfigWriteCmd stageCmd pattern).
func (p *versionsPopup) onDelete(m Model) (Model, tea.Cmd) {
	if p.sel < 0 || p.sel >= len(p.rows) {
		return m, nil
	}
	branch, ref := p.branch, p.rows[p.sel].Ref
	gen := m.versionsGen
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "delete-branch-version",
			Prompt:  i18n.T("Delete this version of %s?", branch),
			Options: []string{"Delete", "Cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt == "Delete" {
				return m, m.versionDeleteCmd(gen, branch, ref)
			}
			return m, nil
		},
	}
	return m, nil
}

// versionDeleteCmd runs the delete synchronously (the gitConfigWriteCmd
// stageCmd pattern) and re-reads the branch's versions in the same message,
// reusing gen so a stale reopen/repo-switch drops the result.
func (m Model) versionDeleteCmd(gen int, branch, ref string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		if _, err := svc.Execute(context.Background(), engine.DeleteBranchVersion{Ref: ref}, nil, nil); err != nil {
			return versionsLoadedMsg{gen: gen, branch: branch, err: err}
		}
		rows, err := svc.BranchVersions(context.Background(), branch)
		return versionsLoadedMsg{gen: gen, branch: branch, rows: rows, err: err}
	}
}

// onCopy copies the FULL sha to the clipboard via the shared seam every other
// copy action uses (copyToClipboardCmd — clipboard.Copy under the hood).
func (p *versionsPopup) onCopy(m Model) (Model, tea.Cmd) {
	if p.sel < 0 || p.sel >= len(p.rows) {
		return m, nil
	}
	full := p.rows[p.sel].Hash
	short := versionShortHash(full)
	return m, m.copyToClipboardCmd(i18n.T("copied %s", short), full)
}

// versionRowText renders one versions-mode row:
// "2026-07-21 14:03 · rebase · a1b2c3d4 <subject>".
func versionRowText(v model.BranchVersion) string {
	ts := time.Unix(v.Unix, 0).Format("2006-01-02 15:04")
	return ts + " · " + opDisplayName(v.Op) + " · " + versionShortHash(v.Hash) + " " + v.Subject
}

// branchVersionRowText renders one branches-mode row: "<branch>  <count>
// versions", with a suffix when the branch no longer exists. The count uses
// the two-key singular/plural convention (the push-tip-tags pattern): the
// English source string "1 versions" would otherwise be grammatically wrong.
func branchVersionRowText(b model.VersionedBranch) string {
	countText := i18n.T("%d versions", b.Count)
	if b.Count == 1 {
		countText = i18n.T("%d version", b.Count)
	}
	row := b.Branch + "  " + countText
	if b.Deleted {
		row += " " + i18n.T("(deleted)")
	}
	return row
}

// render composites the popup over the layer beneath.
func (p *versionsPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// box draws the popup box (modal box only).
func (p *versionsPopup) box(m Model) string {
	w, termH := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupInnerWidth(w))
	textW := popupTextWidth(inner)

	title := i18n.T("Branch versions")
	if p.mode == versionsModeVersions {
		title = i18n.T("Versions of %s", p.branch)
	}

	var bodyLines []string
	switch {
	case p.loading:
		bodyLines = []string{padRight("  "+i18n.T("loading…"), textW)}
	case p.err != "":
		bodyLines = []string{padRight("  "+p.err, textW)}
	case p.mode == versionsModeBranches:
		bodyLines = p.branchBodyLines(termH, textW)
	default:
		bodyLines = p.versionsBodyLines(termH, textW)
	}

	hint := i18n.T("[enter] versions")
	if p.mode == versionsModeVersions {
		hint = i18n.T("[enter] compare  [r] restore  [d] delete  [y] copy sha")
	}

	parts := []string{title, ""}
	parts = append(parts, bodyLines...)
	parts = append(parts, "", hint)
	return popupBox(inner, strings.Join(parts, "\n"))
}

// branchBodyLines renders the branch-mode row list (or the empty-state line).
func (p *versionsPopup) branchBodyLines(termH, textW int) []string {
	if len(p.branchRows) == 0 {
		return []string{padRight("  "+i18n.T("no versions recorded"), textW)}
	}
	wr := make([]winRow, len(p.branchRows))
	for i, b := range p.branchRows {
		prefix := "  "
		var st lipgloss.Style
		if i == p.sel {
			prefix, st = "> ", selectedRow
		}
		wr[i] = winRow{text: prefix + branchVersionRowText(b), style: st}
	}
	capRows := popupResolveRowCap(p.maximized, termH, 12)
	h := len(p.branchRows)
	if h > capRows {
		h = capRows
	}
	return renderWindow(wr, winOpts{w: textW, h: h, mode: modeCutoff, anchor: p.sel})
}

// versionsBodyLines renders the versions-mode row list (or the empty-state
// line).
func (p *versionsPopup) versionsBodyLines(termH, textW int) []string {
	if len(p.rows) == 0 {
		return []string{padRight("  "+i18n.T("no versions recorded"), textW)}
	}
	wr := make([]winRow, len(p.rows))
	for i, v := range p.rows {
		prefix := "  "
		var st lipgloss.Style
		if i == p.sel {
			prefix, st = "> ", selectedRow
		}
		wr[i] = winRow{text: prefix + versionRowText(v), style: st}
	}
	capRows := popupResolveRowCap(p.maximized, termH, 12)
	h := len(p.rows)
	if h > capRows {
		h = capRows
	}
	return renderWindow(wr, winOpts{w: textW, h: h, mode: modeCutoff, anchor: p.sel})
}

// versionBranchNamePopup collects the name for "New branch at version": a
// one-line textfield, mirroring commitNamePopup's shape. enter creates the
// branch at startHash and clears the WHOLE layer stack (both this popup and
// the versionsPopup beneath it — the branchPopup/commitNamePopup convention
// of landing a dispatched op on the base panels); esc pops just this popup,
// revealing the versionsPopup beneath.
type versionBranchNamePopup struct {
	popupMax
	startHash string
	name      textfield
}

// openVersionBranchNamePopup pushes the name popup on top of the current
// layer stack (the versionsPopup stays beneath it).
func (m Model) openVersionBranchNamePopup(startHash string) (Model, tea.Cmd) {
	m = m.pushLayer(&versionBranchNamePopup{startHash: startHash, name: newTextField("")})
	return m, nil
}

func (p *versionBranchNamePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		name := strings.TrimSpace(p.name.Value())
		if name == "" {
			return m, nil
		}
		start := p.startHash
		m = m.clearLayers()
		return m.startOp(engine.CreateBranch{Name: name, StartPoint: start})
	case tea.KeySpace:
		// Branch names cannot contain spaces; dropping it avoids a guaranteed
		// validation error on create (the branchPopup convention).
	default:
		p.name.HandleEditKey(msg)
	}
	return m, nil
}

func (p *versionBranchNamePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	var b strings.Builder
	b.WriteString(i18n.T("New branch at version") + "\n\n")
	b.WriteString(viewField(i18n.T("name: "), p.name, true, popupContentWidth(w)) + "\n\n")
	b.WriteString(i18n.T("[enter] create  [esc] cancel"))
	box := modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
