package tui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/i18n"
)

// repoCfgAction is one whole-file relocation of the per-repo config between the
// committed .gg.toml and the private user-dir file.
type repoCfgAction int

const (
	actCopyToPrivate repoCfgAction = iota
	actMoveToPrivate
	actCopyToCommitted
	actMoveToCommitted
)

func repoCfgActionLabel(a repoCfgAction) string {
	switch a {
	case actCopyToPrivate:
		return i18n.T("Copy to private (user dir)")
	case actMoveToPrivate:
		return i18n.T("Move to private (user dir)")
	case actCopyToCommitted:
		return i18n.T("Copy to committed (.gg.toml)")
	case actMoveToCommitted:
		return i18n.T("Move to committed (.gg.toml)")
	}
	return "?"
}

// repoConfigActions lists the applicable relocation actions given which files
// exist and which paths are available. To-private needs the committed file to
// exist and a private path to write to; to-committed is the mirror.
func repoConfigActions(committedExists, privateExists, haveCommitted, havePrivate bool) []repoCfgAction {
	var a []repoCfgAction
	if committedExists && havePrivate {
		a = append(a, actCopyToPrivate, actMoveToPrivate)
	}
	if privateExists && haveCommitted {
		a = append(a, actCopyToCommitted, actMoveToCommitted)
	}
	return a
}

// repoCfgEndpoints maps an action to its (source, destination, isMove).
func repoCfgEndpoints(act repoCfgAction, committed, private string) (src, dst string, isMove bool) {
	switch act {
	case actCopyToPrivate:
		return committed, private, false
	case actMoveToPrivate:
		return committed, private, true
	case actCopyToCommitted:
		return private, committed, false
	case actMoveToCommitted:
		return private, committed, true
	}
	return "", "", false
}

// repoConfigPopup lets the user copy/move the whole per-repo config between the
// committed .gg.toml and the private user-dir file.
type repoConfigPopup struct {
	popupMax
	committedPath string
	privatePath   string
	committedEx   bool
	privateEx     bool
	actions       []repoCfgAction
	sel           int
	confirm       bool          // overwrite confirmation is showing
	pending       repoCfgAction // action awaiting confirmation
}

// openRepoConfigLocation builds the popup from current model state. The
// committed path is anchored on the CURRENT worktree; the private path on the
// MAIN worktree (worktrees[0]) so all worktrees of the repo share it.
func (m Model) openRepoConfigLocation() Model {
	committed := ""
	if m.currentWorktree != "" {
		committed = filepath.Join(m.currentWorktree, ".gg.toml")
	}
	private := ""
	if len(m.worktrees) > 0 && m.worktrees[0].Path != "" {
		private = config.PrivateRepoPath(m.worktrees[0].Path)
	}
	p := &repoConfigPopup{committedPath: committed, privatePath: private}
	p.refresh()
	return m.pushLayer(p)
}

func repoCfgFileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func repoCfgFileNonEmpty(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

// refresh recomputes existence + the applicable action list.
func (p *repoConfigPopup) refresh() {
	p.committedEx = repoCfgFileExists(p.committedPath)
	p.privateEx = repoCfgFileExists(p.privatePath)
	p.actions = repoConfigActions(p.committedEx, p.privateEx, p.committedPath != "", p.privatePath != "")
	if p.sel >= len(p.actions) {
		p.sel = 0
	}
}

func (p *repoConfigPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if p.confirm {
		switch msg.String() {
		case "y", "Y", "enter":
			p.confirm = false
			return p.run(m, p.pending)
		default: // n / esc / anything else cancels
			p.confirm = false
			return m, nil
		}
	}
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyUp:
		if p.sel > 0 {
			p.sel--
		}
		return m, nil
	case tea.KeyDown:
		if p.sel < len(p.actions)-1 {
			p.sel++
		}
		return m, nil
	case tea.KeyEnter:
		if len(p.actions) == 0 {
			return m, nil
		}
		act := p.actions[p.sel]
		_, dst, _ := repoCfgEndpoints(act, p.committedPath, p.privatePath)
		if repoCfgFileNonEmpty(dst) { // overwriting real content → confirm first
			p.confirm = true
			p.pending = act
			return m, nil
		}
		return p.run(m, act)
	}
	return m, nil
}

// run performs the file op then closes the popup and triggers a full reload so
// the new file layout re-applies (config re-read, write target rebound via
// ActiveRepoConfigPath, feed/status re-walked in any new sort).
func (p *repoConfigPopup) run(m Model, act repoCfgAction) (Model, tea.Cmd) {
	src, dst, isMove := repoCfgEndpoints(act, p.committedPath, p.privatePath)
	if err := config.CopyRepoConfig(src, dst); err != nil {
		m.statusMsg = i18n.T("repo settings: %s", err.Error())
		return m, nil
	}
	m.statusMsg = i18n.T("repo settings: %s done", repoCfgActionLabel(act))
	if isMove {
		if err := config.RemoveRepoConfig(src); err != nil {
			m.statusMsg = i18n.T("repo settings: copied but source not removed: %s", err.Error())
		}
	}
	// The op is now fully complete (copy landed, and — for a move — the remove
	// was attempted). Rebind the write target SYNCHRONOUSLY here, before the
	// (async) reload lands. This must run AFTER the remove attempt:
	// ActiveRepoConfigPath re-stats the filesystem, and a move-to-committed only resolves to
	// committed once the private source is actually gone (computing it right
	// after the copy, while the source still exists, would wrongly resolve back
	// to the source). Without this rebind, m.repoConfigPath keeps pointing at the
	// pre-relocation file for the whole reload window; any per-repo Settings
	// write in that window (Show graph, Commit sort, a refresh rate, the hook)
	// would go to the stale path — after a move-to-private that's the
	// just-deleted committed file, which setScalarLine tolerantly recreates
	// (os.IsNotExist is not an error), silently reintroducing the file the user
	// just relocated away from.
	m.repoConfigPath = config.ActiveRepoConfigPath(p.committedPath, p.privatePath)
	m = m.popLayer()
	m.loadGen++
	return m, m.loadCmd()
}

func (p *repoConfigPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func slotDisplay(path string, exists bool) string {
	if path == "" {
		return i18n.T("(unavailable)")
	}
	status := i18n.T("absent")
	if exists {
		status = i18n.T("present")
	}
	return padCell(status, maxLabelWidth(9, i18n.T("absent"), i18n.T("present"))) + path
}

func (p *repoConfigPopup) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w)) // paths are long
	textW := popupTextWidth(inner)
	var b strings.Builder
	cw := maxLabelWidth(11, i18n.T("committed"), i18n.T("private"))
	b.WriteString(i18n.T("Repo settings location") + "\n\n")
	b.WriteString("  " + padCell(i18n.T("committed"), cw) + slotDisplay(p.committedPath, p.committedEx) + "\n")
	b.WriteString("  " + padCell(i18n.T("private"), cw) + slotDisplay(p.privatePath, p.privateEx) + "\n\n")

	if p.confirm {
		_, dst, _ := repoCfgEndpoints(p.pending, p.committedPath, p.privatePath)
		for _, seg := range wrapWidth(i18n.T("Overwrite %s ?", dst), textW, 1<<20) {
			b.WriteString(seg + "\n")
		}
		b.WriteString("\n" + i18n.T("[y] overwrite  [n/esc] cancel"))
		return popupBox(inner, strings.TrimRight(b.String(), "\n"))
	}

	if len(p.actions) == 0 {
		b.WriteString("  " + i18n.T("(nothing to move — no per-repo config here, or not in a repo)") + "\n")
		b.WriteString("\n" + i18n.T("[esc] close"))
		return popupBox(inner, strings.TrimRight(b.String(), "\n"))
	}

	wr := make([]winRow, len(p.actions))
	for i, a := range p.actions {
		prefix := "  "
		var st lipgloss.Style
		if i == p.sel {
			prefix, st = "> ", selectedRow
		}
		wr[i] = winRow{text: prefix + repoCfgActionLabel(a), style: st}
	}
	for _, line := range renderWindow(wr, winOpts{w: textW, h: len(p.actions), mode: modeCutoff, anchor: p.sel}) {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + i18n.T("active file = private if present; move deletes the source (may dirty a tracked .gg.toml)"))
	b.WriteString("\n" + i18n.T("[↑/↓] select  [enter] do  [esc] close"))
	return popupBox(inner, strings.TrimRight(b.String(), "\n"))
}
