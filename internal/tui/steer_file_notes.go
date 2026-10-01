package tui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// The temporary-note steer verbs (gg session note add|list|show|rm). Like
// the open-files list verbs they never move the screen, so applySteer runs
// them past steerRefusal: a note appears under its line wherever the user
// is. Reply prose is English protocol text.

// noteLandedMsg carries a note_add that had to open (or re-read) its file
// first: the note can only be checked against loaded lines. tag names the
// document, which may have been closed by the time the load lands.
type noteLandedMsg struct {
	load fileContentMsg
	cmd  steer.Command
	tag  string
	path string
	lead string // "; closed <path> (20 files open)" when the open pushed a file out
}

func (m Model) steerFileNote(c steer.Command) (Model, tea.Cmd) {
	switch c.Cmd {
	case "note_add":
		return m.steerNoteAdd(c)
	case "note_list":
		return m.steerNoteList(c)
	case "note_show":
		return m.steerNoteShow(c)
	case "note_rm":
		return m.steerNoteRm(c)
	}
	return m, m.answerSteer(c, steerFail(c, "unknown command "+strconv.Quote(c.Cmd)))
}

// fileNoteProto is a note in its wire form; text adds the lines it sits on.
func fileNoteProto(d *openFile, n agentdocs.Note, text bool) steer.FileNote {
	var lines []string
	if text && docLoaded(d) {
		lines = d.rawLines(n.Start, n.End)
	}
	return agentdocs.NoteWire(n, d.id(), lines)
}

// steerNoteAdd puts a note on an open working-tree file, opening the file
// in the background first when it is not open (or not loaded yet).
func (m Model) steerNoteAdd(c steer.Command) (Model, tea.Cmd) {
	// Another worktree is refused, not asked about: the switch notice would
	// be the very screen change a note promises not to make.
	if c.Worktree != "" && !domain.SameCheckout(c.Worktree, m.snapshotWorktree) {
		return m, m.answerSteer(c, steerFail(c, "gg is showing worktree "+m.snapshotWorktree+", not "+c.Worktree))
	}
	src := fileSource{kind: srcWorktree}
	var d *openFile
	if c.FileID != "" {
		if d = m.findOpenFile(c.FileID, ""); d == nil || d.id() != c.FileID {
			return m, m.answerSteer(c, steerFail(c, "no open file "+c.FileID))
		}
	} else {
		d = m.openFiles.find(m.currentWorktree, docKey(src, c.File))
	}
	// The note goes on the file AS IT IS ON DISK: a loaded copy is good only
	// while the disk still matches what was read (the agent may have edited
	// the file a moment ago, ahead of the watcher); otherwise re-read first.
	if d != nil && docLoaded(d) && !d.loading && m.docCurrent(d) {
		return m.finishNoteAdd(c, d, "")
	}
	path := c.File
	if d != nil {
		path = d.path
	}
	isNew := d == nil
	if isNew {
		ctx, cancel := updateThreadCtx(updateThreadGitTimeout)
		defer cancel()
		present, err := m.svc.WorktreeFilesPresent(ctx, []string{path})
		if err := busyOr(err); err != nil {
			return m, m.answerSteer(c, steerFail(c, "checking "+path+": "+err.Error()))
		}
		if !present[path] {
			return m, m.answerSteer(c, steerFail(c, path+" is not in the working tree"))
		}
		d = newOpenFile(src, path)
	} else {
		d.keepPlace()
	}
	lead := ""
	if isNew {
		status := m.statusMsg
		var ev *openFile
		m, ev = m.registerDocEv(d)
		m.statusMsg = status // an agent's note never takes over the status line
		if p := evictedPath(ev); p != "" {
			lead = "; closed " + p + " (" + strconv.Itoa(maxOpenFiles) + " files open)"
		}
	}
	load, tag := m.loadDoc(d), d.tag
	return m, func() tea.Msg {
		return noteLandedMsg{load: load().(fileContentMsg), cmd: c, tag: tag, path: path, lead: lead}
	}
}

// finishNoteAdd adds the note to a loaded document and answers.
func (m Model) finishNoteAdd(c steer.Command, d *openFile, lead string) (Model, tea.Cmd) {
	n, err := d.addNote(c.Start, c.End, c.Summary, c.Rationale, c.Author)
	if err != nil {
		return m, m.answerSteer(c, steerFail(c, err.Error()))
	}
	r := steerOK(c, agentdocs.NotedDetail(*n, lead))
	r.Notes = []steer.FileNote{fileNoteProto(d, *n, false)}
	return m, m.answerSteer(c, r)
}

// noteLanded finishes a note_add whose file load just arrived.
func (m Model) noteLanded(msg noteLandedMsg) (Model, tea.Cmd) {
	tm, fill := m.Update(msg.load)
	m = tm.(Model)
	d := m.openFiles.findTag(msg.tag)
	switch {
	case d == nil:
		return m, tea.Batch(fill, m.answerSteer(msg.cmd, steerFail(msg.cmd, msg.path+" was closed before the note landed")))
	case msg.load.err != nil:
		return m, tea.Batch(fill, m.answerSteer(msg.cmd, steerFail(msg.cmd, "reading "+msg.path+": "+msg.load.err.Error())))
	}
	m, reply := m.finishNoteAdd(msg.cmd, d, msg.lead)
	return m, tea.Batch(fill, reply)
}

// steerNoteList answers with every note of the worktree's open files, or of
// the one file named.
func (m Model) steerNoteList(c steer.Command) (Model, tea.Cmd) {
	docs := m.openFiles.list(m.currentWorktree)
	if c.FileID != "" || c.File != "" {
		d := m.findOpenFile(c.FileID, c.File)
		if d == nil {
			name := c.FileID
			if name == "" {
				name = c.File
			}
			return m, m.answerSteer(c, steerFail(c, "no open file "+name))
		}
		docs = []*openFile{d}
	}
	r := steerOK(c, "")
	for _, d := range docs {
		for _, n := range d.notes {
			r.Notes = append(r.Notes, fileNoteProto(d, n, false))
		}
	}
	if len(r.Notes) == 0 {
		r.Detail = "no notes"
	}
	return m, m.answerSteer(c, r)
}

// steerNoteShow answers with one note and the lines it sits on now.
func (m Model) steerNoteShow(c steer.Command) (Model, tea.Cmd) {
	d, n := m.findFileNote(c.NoteID)
	if n == nil {
		return m, m.answerSteer(c, steerFail(c, "no note "+c.NoteID))
	}
	r := steerOK(c, "")
	r.Notes = []steer.FileNote{fileNoteProto(d, *n, true)}
	return m, m.answerSteer(c, r)
}

// steerNoteRm removes one note by id, or every note of a file.
func (m Model) steerNoteRm(c steer.Command) (Model, tea.Cmd) {
	if c.NoteID != "" {
		d, n := m.findFileNote(c.NoteID)
		if n == nil {
			return m, m.answerSteer(c, steerFail(c, "no note "+c.NoteID))
		}
		id := n.ID
		d.removeNote(id)
		return m, m.answerSteer(c, steerOK(c, "removed "+id))
	}
	d := m.findOpenFile(c.FileID, c.File)
	if d == nil {
		name := c.FileID
		if name == "" {
			name = c.File
		}
		return m, m.answerSteer(c, steerFail(c, "no open file "+name))
	}
	return m, m.answerSteer(c, steerOK(c, "removed "+strconv.Itoa(d.clearNotes())+" notes from "+d.path))
}

// docCurrent reports whether a working-tree document's lines are what the
// disk holds right now: one stat against the state its bytes were read at.
func (m Model) docCurrent(d *openFile) bool {
	abs := m.docAbs(d)
	return d.src.kind == srcWorktree && abs != "" && d.disk.known && statDisk(abs).same(d.disk)
}
