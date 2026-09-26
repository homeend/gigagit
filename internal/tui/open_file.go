package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/homeend/gigagit/internal/i18n"
)

// fileSourceKind is where an open file's bytes come from.
type fileSourceKind int

const (
	srcWorktree fileSourceKind = iota // the file ON DISK in the current worktree
	srcCommit                         // the file at a commit (rev = sha)
	srcShelf                          // a shelf member's frozen bytes (rev = entry id)
	srcExternal                       // a file outside the repository (an AI task's result); path is absolute
)

// fileSource names one version of a file: the working tree, a commit or a
// shelf entry. Together with the path it is an open file's identity.
type fileSource struct {
	kind fileSourceKind
	rev  string
}

// openFile is ONE viewed file: its version, its lines and the viewing state
// over them (cursor, scroll, selection, search, display mode). It is the
// DOCUMENT; the files view's right-column preview and the full-screen
// fileViewer are two frames that show one. Keeping the state here, not in
// the frame, is what lets a file leave the screen and come back as it was.
type openFile struct {
	src  fileSource
	path string
	p    *contentPopup
	// An AI task's result (srcExternal): title names it in the viewer and
	// the switcher; result adds y (copy it all); apply, when set, adds a
	// (use it — a commit message fills the commit box).
	title  string
	result bool
	apply  func(Model) (Model, tea.Cmd)
	// tag is unique per document — two documents of one file never share it
	// — so a load result finds exactly the document that asked for it, and a
	// result for a document no frame shows any more finds nothing.
	tag string
	// seq is the document's instance number: its tag's suffix and, as
	// f<seq>, the id an agent names it by (gg session files).
	seq int64
	// pendingLine is the 1-based line a link asked for (0 = none), parked
	// until the async load fills the lines it indexes.
	pendingLine int
	// keep is the reader's place (cursor line, 1-based, and window top) a
	// reload restores; zero = none. Set by keepPlace, used by one fill.
	keep struct{ line, top int }
	// disk is the file's state on disk when its shown bytes were read (a
	// working-tree document only; zero = never read). A poll that finds the
	// disk different reloads the document.
	disk diskStat
	// checked is when a poll last looked at the disk (background documents
	// are looked at every backgroundPollEvery).
	checked time.Time
	// loading is true while a load is in flight: a poll leaves the document
	// alone, so a second load can never race the first (whose fill would
	// land a link's line, the second's reset it).
	loading bool
}

// keepPlace makes the next fill — a reload of a file the user is reading —
// restore the cursor and the window top instead of starting at the top.
// A placeholder is no place: the one saved before it (a file deleted on
// disk) is kept for when the file comes back.
func (d *openFile) keepPlace() {
	if !docLoaded(d) {
		return
	}
	d.keep.line, d.keep.top = d.p.cur+1, d.p.sel
}

// openFileSeq numbers documents for their tags. Process-global: a tag only
// has to be unique among documents alive at once.
var openFileSeq atomic.Int64

// newOpenFile is a document for path at src, showing the loading placeholder
// until its load arrives.
func newOpenFile(src fileSource, path string) *openFile {
	d := &openFile{
		src:  src,
		path: path,
		p:    &contentPopup{title: path, lines: []contentLine{{text: i18n.T("(loading…)")}}},
	}
	d.seq = openFileSeq.Add(1)
	d.tag = fmt.Sprintf("%s#%d", d.key(), d.seq)
	return d
}

// id is the document's agent-facing id (gg session files focus <id>).
func (d *openFile) id() string { return "f" + strconv.FormatInt(d.seq, 10) }

// key is the document's identity without its instance number: the same
// version of the same file has the same key.
func (d *openFile) key() string { return docKey(d.src, d.path) }

// docKey is the key of path at src — what an open looks an open file up by.
func docKey(src fileSource, path string) string {
	return fmt.Sprintf("%d:%s:%s", src.kind, src.rev, path)
}

// fill puts a load's lines into the document. The lines the cursor and the
// selection indexed are gone, so both reset; a line the link asked for then
// lands (centred, clamped to the last line); a live search — possibly typed
// while the placeholder showed — is re-run over the real lines and its hit
// scrolled into view. rows × innerW is the frame's content size. notice is a
// status line for the caller to show ("" = none).
//
// A reload (msg.reload) keeps the reader's place as it is NOW, when the new
// lines arrive — not when the reload was sent, so a cursor moved during a
// slow read is not snapped back. A placeholder does not use the saved place:
// it waits for the next real fill.
func (d *openFile) fill(msg fileContentMsg, rows, innerW int) (notice string) {
	d.loading = false
	if msg.disk.known {
		d.disk = msg.disk
	}
	if msg.reload && d.pendingLine == 0 {
		d.keepPlace()
	}
	p := d.p
	if msg.err != nil {
		p.lines = []contentLine{{text: i18n.T("(load failed: %s)", msg.err.Error())}}
	} else {
		p.lines = msg.lines
	}
	p.cur, p.sel = 0, 0
	p.lsel.clear()
	if docLoaded(d) {
		keep := d.keep
		d.keep.line, d.keep.top = 0, 0
		if keep.line > 0 && d.pendingLine == 0 {
			p.cur = min(keep.line, len(p.lines)) - 1
			p.sel = previewClamp(keep.top, len(p.lines), rows, p.mode)
		}
	}
	notice = d.landPendingLine(rows)
	if p.search.active() {
		p.search.refindFrom(previewSearchLines(p), p.searchPos(rows))
		p.snapHit(rows, innerW)
	}
	return notice
}

// landPendingLine puts the cursor on the line the link asked for, centred in
// a rows-row window — clamped to the last line (the file may have shrunk since
// the link was copied), which it reports as a notice. A placeholder (empty,
// too large, load failed) is not a line of the file: the request is dropped.
// Either way it is consumed.
func (d *openFile) landPendingLine(rows int) (notice string) {
	line := d.pendingLine
	d.pendingLine = 0
	p := d.p
	if line <= 0 || len(p.lines) == 0 || !p.lines[0].src {
		return ""
	}
	n := len(p.lines)
	if line > n {
		notice = i18n.T("line %d is past the end of %s (%d lines)", line, d.path, n)
		line = n
	}
	p.cur = line - 1
	p.sel = previewClamp(p.cur-rows/2, n, rows, p.mode)
	return notice
}
