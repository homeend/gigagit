package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// An AI task's result opens in the file viewer (file_viewer.go) like any
// file: line cursor, selection copy, / search, ctrl+] keeps it open in the
// background and ctrl+\ lists it under the open files. The bytes are a file
// on disk, written by domain.SaveTaskResult, so the viewer reloads it when it
// changes. A commit/range/branch review is the exception: it lives in its
// note and opens through openReviewNote (srcNote), never as a file.

// openResultViewer writes text as task id's result file (ext names its
// kind: .md, .txt, .log) and opens it. apply, when set, is the viewer's a.
func (m Model) openResultViewer(id domain.TaskID, ext, title, text string, apply func(Model) (Model, tea.Cmd)) (Model, tea.Cmd) {
	path, err := domain.SaveTaskResult(id, ext, text)
	if err != nil {
		m.statusMsg = i18n.T("could not open the result: %s", err.Error())
		return m, nil
	}
	return m.openResultFile(path, title, apply)
}

// openReviewNote opens an AI review, stored as a note, in the viewer (y
// copies it all). The text is read from the note on every load.
func (m Model) openReviewNote(id, title string) (Model, tea.Cmd) {
	src := fileSource{kind: srcNote, rev: id}
	path := "review-" + id + ".md"
	d := m.openFiles.find(m.currentWorktree, docKey(src, path))
	if d == nil {
		d = newOpenFile(src, path)
	} else {
		m = m.detachDoc(d)
	}
	d.title, d.result = title, true
	d.p.extraHint = i18n.T("[y] copy")
	m = m.pushLayer(&fileViewer{d})
	m = m.registerDoc(d)
	return m, m.loadDoc(d)
}

// openResultFile opens the result file at path (absolute) in the viewer.
func (m Model) openResultFile(path, title string, apply func(Model) (Model, tea.Cmd)) (Model, tea.Cmd) {
	src := fileSource{kind: srcExternal}
	d := m.openFiles.find(m.currentWorktree, docKey(src, path))
	if d == nil {
		d = newOpenFile(src, path)
	} else {
		m = m.detachDoc(d)
	}
	d.title, d.result, d.apply = title, true, apply
	d.p.extraHint = i18n.T("[y] copy")
	if apply != nil {
		d.p.extraHint = i18n.T("[a] apply") + "  " + d.p.extraHint
	}
	m = m.pushLayer(&fileViewer{d})
	m = m.registerDoc(d)
	return m, m.loadDoc(d)
}

// docText is a document's whole text as loaded.
func docText(d *openFile) string {
	var b strings.Builder
	for _, l := range d.p.lines {
		b.WriteString(l.text)
		b.WriteByte('\n')
	}
	return b.String()
}
