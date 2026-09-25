package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/steer"
)

// The open-files steer verbs (gg session files, files focus, open
// --background). files and a background open never move the screen, so
// applySteer runs them past steerRefusal; file_focus brings a frame up and
// keeps every refusal.

// steerFiles answers `gg session files`: the current worktree's open files,
// most recently shown first. Read-only — it never touches the screen.
func (m Model) steerFiles(c steer.Command) (Model, tea.Cmd) {
	r := steerOK(c, "")
	r.Files = m.openFilesProto()
	if len(r.Files) == 0 {
		r.Detail = "no open files"
	}
	return m, m.answerSteer(c, r)
}

// steerNavigateBackground loads a content link into the open-files list
// without showing it (gg open --background): no frame, no panel move, no
// parked navigate — so a navigate still loading never refuses it. The reply
// rides the load: only the lines say where the cursor landed. A file already
// on screen is left exactly as the user has it.
func (m Model) steerNavigateBackground(c steer.Command) (Model, tea.Cmd) {
	// Another worktree is refused, not asked about: the switch notice would
	// be the very screen change a background open promises not to make.
	if c.Worktree != "" && !domain.SameCheckout(c.Worktree, m.snapshotWorktree) {
		return m, m.answerSteer(c, steerFail(c, "gg is showing worktree "+m.snapshotWorktree+", not "+c.Worktree))
	}
	ctx, cancel := updateThreadCtx(updateThreadGitTimeout)
	defer cancel()
	present, err := m.svc.WorktreeFilesPresent(ctx, []string{c.File})
	if err := busyOr(err); err != nil {
		return m, m.answerSteer(c, steerFail(c, "checking "+c.File+": "+err.Error()))
	}
	if !present[c.File] {
		return m, m.answerSteer(c, steerFail(c, c.File+" is not in the working tree"))
	}
	src := fileSource{kind: srcWorktree}
	d := m.openFiles.find(m.currentWorktree, docKey(src, c.File))
	onScreen := d != nil && m.docShown(d) ||
		d == nil && m.filesPreview != nil && m.filesPreview.src == src && m.filesPreview.path == c.File
	if onScreen {
		return m, m.answerSteer(c, steerOK(c, c.File+" is already open on screen"))
	}
	line := 0
	if c.Line != nil {
		line = c.Line.No
	}
	if d == nil {
		d = newOpenFile(src, c.File)
	} else if line == 0 {
		d.keepPlace()
	}
	d.pendingLine = line
	m.statusMsg = ""
	m, ev := m.registerDocEv(d)
	if m.statusMsg == "" {
		m.statusMsg = i18n.T("%s is in the background — ctrl+\\ lists open files", d.path)
	}
	load := m.loadDoc(d)
	lead, evicted := "opened "+c.File+" in the background", evictedPath(ev)
	return m, func() tea.Msg {
		return contentLandedMsg{load: load().(fileContentMsg), cmd: c, line: line, lead: lead, evicted: evicted}
	}
}

// steerFileFocus brings an open file to the front (gg session files focus):
// the switcher's enter, optionally at a line. A load it starts carries the
// reply; a loaded commit/shelf version lands the line at once.
func (m Model) steerFileFocus(c steer.Command) (Model, tea.Cmd) {
	d := m.findOpenFile(c.FileID, c.File)
	if d == nil {
		name := c.FileID
		if name == "" {
			name = c.File
		}
		return m, m.answerSteer(c, steerFail(c, "no open file "+name))
	}
	m = m.steerToPanels()
	line := 0
	if c.Line != nil {
		line = c.Line.No
	}
	d.pendingLine = line
	m, load := m.bringToFront(d)
	lead := "focused " + d.path
	if load == nil {
		rows, _ := m.viewerGeom()
		if m.filesPreview == d {
			rows = m.filePreviewRowsCap()
		}
		if n := d.landPendingLine(rows); n != "" {
			m.statusMsg = n
		}
		return m, m.answerSteer(c, steerOK(c, landedDetail(lead, line, d.p.lines, "")))
	}
	return m, func() tea.Msg {
		return contentLandedMsg{load: load().(fileContentMsg), cmd: c, line: line, lead: lead}
	}
}
