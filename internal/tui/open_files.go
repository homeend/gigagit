package tui

import (
	"context"
	"errors"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// maxOpenFiles is how many files one worktree keeps open (spec ruling 4).
const maxOpenFiles = 100

// openFilesReg is the open-files list: per worktree, the documents the user
// or an agent opened, most recently shown first. A document leaves it only
// when it is closed on purpose (esc on it, x in the switcher) or evicted over
// the cap — every other teardown leaves it in the background.
type openFilesReg struct {
	byWT map[string][]*openFile
	// open is what the ctrl+\ tree shows expanded, per worktree: an
	// overview's or a group's expand key (doc_tree.go). Kept while gg runs.
	open map[string]bool
}

// expanded says whether the tree node keyed key of wt is expanded.
func (r *openFilesReg) expanded(wt, key string) bool {
	if r == nil {
		return false
	}
	return r.open[string(model.KeyOf(wt))+"\x00"+key]
}

// setExpanded records the tree node keyed key of wt as expanded (or not).
func (r *openFilesReg) setExpanded(wt, key string, on bool) {
	if r == nil {
		return
	}
	k := string(model.KeyOf(wt)) + "\x00" + key
	if !on {
		delete(r.open, k)
		return
	}
	if r.open == nil {
		r.open = map[string]bool{}
	}
	r.open[k] = true
}

// list is wt's open files, most recently shown first. A nil registry is empty.
func (r *openFilesReg) list(wt string) []*openFile {
	if r == nil {
		return nil
	}
	return r.byWT[string(model.KeyOf(wt))]
}

// all is every worktree's open documents, by checkout key.
func (r *openFilesReg) all() map[model.CheckoutKey][]*openFile {
	out := map[model.CheckoutKey][]*openFile{}
	if r == nil {
		return out
	}
	for k, l := range r.byWT {
		out[model.CheckoutKey(k)] = l
	}
	return out
}

// find is wt's open document with key, or nil.
func (r *openFilesReg) find(wt, key string) *openFile {
	for _, d := range r.list(wt) {
		if d.key() == key {
			return d
		}
	}
	return nil
}

// touch puts d first in wt's list, adding it when new. Over the cap it drops
// the least recently shown document that is not on screen (shown reports
// that), carries no notes and is no overview — an agent's remarks are never
// pushed out — and
// returns it; nil when nothing was dropped (the list then grows past the cap).
func (r *openFilesReg) touch(wt string, d *openFile, shown func(*openFile) bool) (evicted *openFile) {
	if r.byWT == nil {
		r.byWT = map[string][]*openFile{}
	}
	wt = string(model.KeyOf(wt)) // one list per checkout, whatever its spelling
	l := []*openFile{d}
	for _, e := range r.byWT[wt] {
		if e != d {
			l = append(l, e)
		}
	}
	if len(l) > maxOpenFiles {
		for i := len(l) - 1; i > 0; i-- {
			if !shown(l[i]) && !l[i].hasNotes() && l[i].ov == nil {
				evicted = l[i]
				evicted.closeTok() // out of the list, off screen: its colours are moot
				l = append(l[:i], l[i+1:]...)
				break
			}
		}
	}
	r.byWT[wt] = l
	return evicted
}

// has reports whether d is in wt's open files.
func (r *openFilesReg) has(wt string, d *openFile) bool {
	for _, e := range r.list(wt) {
		if e == d {
			return true
		}
	}
	return false
}

// findTag is the open document tagged tag in any worktree's list, or nil.
// Tags are unique, so a load result finds its document even after a
// worktree switch (its loading flag must still clear).
func (r *openFilesReg) findTag(tag string) *openFile {
	if r == nil {
		return nil
	}
	for _, l := range r.byWT {
		for _, d := range l {
			if d.tag == tag {
				return d
			}
		}
	}
	return nil
}

// remove drops d from wt's list.
func (r *openFilesReg) remove(wt string, d *openFile) {
	if r == nil {
		return
	}
	wt = string(model.KeyOf(wt))
	l := r.byWT[wt][:0:0]
	d.closeTok() // closed: nobody will see its colours
	for _, e := range r.byWT[wt] {
		if e == d && d.ov != nil {
			d.ov.closed = true // a file it opened stops drawing its anchors
		}
		if e != d {
			l = append(l, e)
		}
	}
	r.byWT[wt] = l
}

// drop releases wt's whole list: the worktree is gone (pruneViews), so its
// documents cannot be reloaded and nothing will show them again. An
// overview among them stops drawing its anchors, as remove does.
func (r *openFilesReg) drop(wt string) {
	if r == nil {
		return
	}
	wt = string(model.KeyOf(wt))
	for _, d := range r.byWT[wt] {
		d.closeTok()
		if d.ov != nil {
			d.ov.closed = true
		}
	}
	delete(r.byWT, wt)
}

// docShown reports whether d is in a frame: the files view's preview or a
// full-screen viewer anywhere on the stack (a covered one is still shown —
// it comes back when the window above it closes).
func (m Model) docShown(d *openFile) bool {
	if m.filesPreview == d {
		return true
	}
	if m.layers != nil {
		for _, l := range m.layers.entries {
			if fv, ok := l.(*fileViewer); ok && fv.openFile == d {
				return true
			}
			if sv, ok := l.(*splitView); ok && sv.holds(d) {
				return true
			}
		}
	}
	return false
}

// detachDoc takes d out of whatever frame shows it, so it can be shown in
// another: a document is in one frame at a time.
func (m Model) detachDoc(d *openFile) Model {
	d.dropDiff(m.docRows(d)) // the diff shows only where an overview's step put it (spec §6)
	if d.p.cs.on {
		d.p.previewCharLeave() // put aside: it never comes back already in the copy mode
	}
	if m.filesPreview == d {
		m.filesPreview = nil
		m = m.focusTree()
	}
	// A document shown in a split pane leaves for another frame (a document
	// is in one frame at a time): the file pane is emptied and the split
	// stays; the overview leaving takes the split down, as esc does — there
	// is nothing left to steer.
	if sv := m.splitShowing(d); sv != nil {
		if sv.left.doc() == d {
			if o := sv.right.doc(); o != nil {
				o.backgrounded = true
			}
			return m.closeSplit(sv)
		}
		if dp, ok := sv.right.(*docPane); ok && dp.d == d {
			dp.d = nil
		}
		return m
	}
	if m.layers != nil {
		for _, l := range m.layers.entries {
			if fv, ok := l.(*fileViewer); ok && fv.openFile == d {
				return m.removeLayer(fv)
			}
		}
	}
	return m
}

// registerDoc puts d first in the current worktree's open files — call it
// once d is in its frame. A file dropped over the cap is named in the status.
func (m Model) registerDoc(d *openFile) Model {
	m, _ = m.registerDocEv(d)
	return m
}

// registerDocEv is registerDoc that also returns the file dropped over the
// cap (nil when none), so a steer reply can name it.
func (m Model) registerDocEv(d *openFile) (Model, *openFile) {
	if m.openFiles == nil {
		return m, nil
	}
	m.adoptDoc(d)
	ev := m.openFiles.touch(m.currentWorktree, d, m.docShown)
	if ev != nil {
		m.statusMsg = i18n.T("closed %s (%d files open)", ev.path, maxOpenFiles)
	}
	return m, ev
}

// evictedPath is ev's path, or "" when nothing was dropped.
func evictedPath(ev *openFile) string {
	if ev == nil {
		return ""
	}
	return ev.path
}

// docLoaded reports whether d holds its file's lines (not a placeholder).
func docLoaded(d *openFile) bool {
	if d.stream != nil { // a split stack: its files load one by one, the stream is always the document
		return len(d.p.lines) > 0
	}
	return len(d.p.lines) > 0 && d.p.lines[0].src
}

// backgroundDoc takes d off the screen and keeps it open (ctrl+]): the screen
// returns to whatever was beneath it.
func (m Model) backgroundDoc(d *openFile) Model {
	d.backgrounded = true
	if d.ov != nil { // the overview leaves the screen: its D / f choice ends
		d.ov.forgetDiff()
	}
	m = m.detachDoc(d)
	if d.ov != nil {
		m.statusMsg = i18n.T("overview %s is in the background — ctrl+\\ lists open files", d.overviewLabel())
	} else {
		m.statusMsg = i18n.T("%s is in the background — ctrl+\\ lists open files", d.path)
	}
	return m
}

// escDoc is esc on an open file's frame: a file that has been in the
// background goes back there, any other one closes.
func (m Model) escDoc(d *openFile) Model {
	if d.backgrounded {
		return m.backgroundDoc(d)
	}
	return m.closeDoc(d)
}

// closeDoc closes d (X or esc on it, x in the switcher). A FILE leaves the
// screen and the list; its notes stay in the store — a session document is
// deleted only on purpose (deleteDoc, confirmFileNotesDelete). An agent's
// OVERVIEW the store still holds closes LOCALLY: it leaves the screen and
// stays in the list (spec 2026-10-18-gg-session-docs §4.4); one the store
// no longer holds leaves the list too.
func (m Model) closeDoc(d *openFile) Model {
	if m.heldOverview(d) {
		return m.backgroundDoc(d)
	}
	if d.ov != nil {
		d.ov.forgetDiff()
	}
	m = m.detachDoc(d)
	m.openFiles.remove(m.currentWorktree, d)
	return m
}

// focusedDoc is the open file the keyboard is on: a full-screen viewer on
// top, else the files view's focused preview.
func (m Model) focusedDoc() (*openFile, bool) {
	if _, d, _, _, ok := m.splitFocus(); ok {
		return d, true
	}
	if fv, ok := m.topLayer().(*fileViewer); ok {
		return fv.openFile, true
	}
	return m.focusedFilesPreview()
}

// backgroundRow is the . menu's "Send to background" for the focused open
// file — ctrl+] for the mouse.
func (m Model) backgroundRow() (actionRow, bool) {
	if _, _, _, _, inSplit := m.splitFocus(); inSplit {
		// The overview drives the split: esc puts it in the background;
		// its file pane holds whatever the overview's place shows.
		return actionRow{}, false
	}
	d, ok := m.focusedDoc()
	if !ok || d.stream != nil { // a split's stacked stream is no open file
		return actionRow{}, false
	}
	return actionRow{
		id:    "file-background",
		label: i18n.T("Send to background"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.backgroundDoc(d), nil
		},
	}, true
}

// docLoader is how d's bytes are read again: the disk, the commit, the shelf.
func (m Model) docLoader(d *openFile) func(context.Context) ([]byte, error) {
	svc, src, path := m.svc, d.src, d.path
	switch src.kind {
	case srcCommit:
		return func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, src.rev, path) }
	case srcShelf:
		ref := model.FileRef{Source: model.SourceShelf, Locator: src.rev, Path: path}
		return func(ctx context.Context) ([]byte, error) { return svc.ResolveBytes(ctx, ref) }
	case srcExternal:
		return func(context.Context) ([]byte, error) { return os.ReadFile(path) }
	case srcNote:
		return func(ctx context.Context) ([]byte, error) {
			r, err := svc.Review(ctx, src.rev)
			if errors.Is(err, domain.ErrReviewNotFound) {
				return []byte(i18n.T("review deleted") + "\n"), nil
			}
			if err != nil {
				return nil, err
			}
			return []byte(r.Text), nil
		}
	}
	return func(ctx context.Context) ([]byte, error) { return svc.WorktreeFile(ctx, path) }
}

// bringToFront shows open file d (the switcher's enter): the files view's
// preview when it is the preview (focus moves there), else a full-screen
// viewer on top — a covered viewer's frame is moved up, a background file
// gets a new one (spec ruling 9). A working-tree file, or one whose load
// never landed, is read again at the reader's place.
func (m Model) bringToFront(d *openFile) (Model, tea.Cmd) {
	if d.ov != nil { // every overview comes up beside its code when it fits
		if w, _ := m.overlayDims(); w >= splitMinWidth {
			return m.openOverviewSplit(d)
		}
	}
	if m.filesPreview == d && m.topLayer() == nil {
		m.filesTreeFocused = false
		return m.registerDoc(d), nil
	}
	m = m.detachDoc(d)
	m = m.pushLayer(&fileViewer{openFile: d})
	m = m.registerDoc(d)
	if !d.onDisk() && docLoaded(d) {
		return m, nil
	}
	d.keepPlace()
	return m, m.loadDoc(d)
}

// openFilesProto is the current worktree's open files in their wire form,
// most recently shown first — `gg session files` and the snapshot.
func (m Model) openFilesProto() []steer.OpenFile {
	var out []steer.OpenFile
	for _, d := range m.openFiles.list(m.currentWorktree) {
		f := steer.OpenFile{ID: d.id(), Path: d.path, Source: "worktree", State: "background"}
		switch d.src.kind {
		case srcCommit:
			f.Source, f.Rev = "commit", d.src.rev
		case srcShelf:
			f.Source, f.Rev = "shelf", d.src.rev
		case srcExternal:
			f.Source, f.Title = "result", d.title
		case srcNote:
			f.Source, f.Rev, f.Title = "review", d.src.rev, d.title
		case srcOverview, srcReviewOverview, srcCommitOverview:
			f.Source, f.Title = "overview", d.title
			// A session document's front matter (as the web's rows carry it).
			if d.ov != nil && m.docs != nil {
				if o, ok := m.docs.OverviewIn(d.root, d.id()); ok {
					f.Author, f.Anchors = o.Author, len(o.Anchors)
					if !o.Created.IsZero() {
						f.Created = o.Created.UTC().Format(time.RFC3339)
					}
				}
			}
		}
		if docLoaded(d) {
			f.Line = d.p.cur + 1
		}
		if m.docShown(d) {
			f.State = "shown"
		}
		f.Notes = len(d.notes)
		out = append(out, f)
	}
	return out
}

// findOverview is the current worktree's overview with id, or nil and why.
func (m Model) findOverview(id string) (*openFile, string) {
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if d.ov != nil && d.src.kind == srcOverview && d.id() == id {
			return d, ""
		}
	}
	return nil, "no overview " + id
}

// findOpenFile is the current worktree's open file an agent named: by id,
// else — or when no id matches — by path, the most recently shown version;
// a path that matches nothing is tried as an id (`gg session files focus
// <document id>` sends a document's id as a path: only f<n> reads as an id
// to the CLI).
func (m Model) findOpenFile(id, path string) *openFile {
	l := m.openFiles.list(m.currentWorktree)
	if id != "" {
		for _, d := range l {
			if d.id() == id {
				return d
			}
		}
		path = id
	}
	for _, d := range l {
		if d.path == path {
			return d
		}
	}
	if id == "" && path != "" {
		for _, d := range l {
			if d.id() == path {
				return d
			}
		}
	}
	return nil
}

// onDisk reports a document read from a file on disk (a working-tree file,
// an AI task's result): a reload rereads it.
func (d *openFile) onDisk() bool { return d.src.kind == srcWorktree || d.src.kind == srcExternal }
