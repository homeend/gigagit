package tui

import (
	"fmt"
	"sync/atomic"

	"github.com/homeend/gigagit/internal/i18n"
)

// fileSourceKind is where an open file's bytes come from.
type fileSourceKind int

const (
	srcWorktree fileSourceKind = iota // the file ON DISK in the current worktree
	srcCommit                         // the file at a commit (rev = sha)
	srcShelf                          // a shelf member's frozen bytes (rev = entry id)
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
	// tag is unique per document — two documents of one file never share it
	// — so a load result finds exactly the document that asked for it, and a
	// result for a document no frame shows any more finds nothing.
	tag string
	// pendingLine is the 1-based line a link asked for (0 = none), parked
	// until the async load fills the lines it indexes.
	pendingLine int
	// keep is the reader's place (cursor line, 1-based, and window top) a
	// reload restores; zero = none. Set by keepPlace, used by one fill.
	keep struct{ line, top int }
}

// keepPlace makes the next fill — a reload of a file the user is reading —
// restore the cursor and the window top instead of starting at the top.
func (d *openFile) keepPlace() {
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
	d.tag = fmt.Sprintf("%s#%d", d.key(), openFileSeq.Add(1))
	return d
}

// key is the document's identity without its instance number: the same
// version of the same file has the same key.
func (d *openFile) key() string {
	return fmt.Sprintf("%d:%s:%s", d.src.kind, d.src.rev, d.path)
}

// fill puts a load's lines into the document. The lines the cursor and the
// selection indexed are gone, so both reset; a line the link asked for then
// lands (centred, clamped to the last line); a live search — possibly typed
// while the placeholder showed — is re-run over the real lines and its hit
// scrolled into view. rows × innerW is the frame's content size. notice is a
// status line for the caller to show ("" = none).
func (d *openFile) fill(msg fileContentMsg, rows, innerW int) (notice string) {
	p := d.p
	if msg.err != nil {
		p.lines = []contentLine{{text: i18n.T("(load failed: %s)", msg.err.Error())}}
	} else {
		p.lines = msg.lines
	}
	p.cur, p.sel = 0, 0
	p.lsel.clear()
	keep := d.keep
	d.keep.line, d.keep.top = 0, 0
	if keep.line > 0 && d.pendingLine == 0 && len(p.lines) > 0 && p.lines[0].src {
		p.cur = min(keep.line, len(p.lines)) - 1
		p.sel = previewClamp(keep.top, len(p.lines), rows, p.mode)
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
