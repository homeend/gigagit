package tui

import (
	"github.com/homeend/gigagit/internal/agentdocs"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/i18n"
)

// The stacked file pane's STREAM (spec 2026-10-10-review-overview-split
// §4.2): every review file, one after another — a blank line (not on the
// first), a header, a rule, then the file's lines or one placeholder. Pure:
// no loading, no keys. Every (file, line) ↔ stream mapping goes through this
// table, never through a line number alone — line numbers repeat across
// files.

// streamFile is one file of the stream and where its block sits.
type streamFile struct {
	path, status string
	notes        int // the review's notes on it (◆n on the header)
	lines        []contentLine
	folded       bool
	// autoFold: folded by the stream itself (past streamFoldOver), not by
	// the reader — it unfolds once the reader brings it into view
	// (stackPane.unfoldInView). A fold by hand clears it.
	autoFold bool
	loaded   bool  // lines (or err) are final
	loading  bool  // a load is out for it
	err      error // the load failed, or the loader named a placeholder
	// boxes are the file's notes (a review's remarks, or the agent's notes
	// on a working-tree file), at its own 1-based lines; rebuild maps them
	// to stream lines.
	boxes []agentdocs.Note
	start int // the block's first stream line (the blank; the header on the first file)
	hdr   int // the header line
	body  int // the first content line, or the placeholder
}

// fileStream is the table the pane rebuilds the document's lines from.
type fileStream struct{ files []streamFile }

// streamFoldOver is the file count above which a stream opens folded
// (every file but the target), as the stacked diff does.
const streamFoldOver = 100

// headerText is a file's header row: path, status, note count.
func headerText(f streamFile) string {
	t := "━━ " + f.path
	if f.status != "" {
		t += "  (" + f.status + ")"
	}
	if f.notes > 0 {
		t += "  ◆" + strconv.Itoa(f.notes)
	}
	return t
}

// shows reports whether the file's own lines are in the stream.
func (f streamFile) shows() bool { return !f.folded && f.loaded && f.err == nil && len(f.lines) > 0 }

// build lays the stream out and stamps each file's start/hdr/body.
func (s *fileStream) build() []contentLine {
	var out []contentLine
	for i := range s.files {
		f := &s.files[i]
		f.start = len(out)
		if i > 0 {
			out = append(out, contentLine{})
		}
		f.hdr = len(out)
		h := headerText(*f)
		out = append(out, contentLine{text: h, raw: h, elide: true, stackHeader: true})
		out = append(out, contentLine{text: strings.Repeat("─", 400), dim: true, noWrap: true})
		f.body = len(out)
		switch {
		case f.folded:
			out = append(out, contentLine{text: "  " + i18n.T("(folded)"), dim: true})
		case !f.loaded:
			out = append(out, contentLine{text: "  " + i18n.T("(loading…)"), dim: true})
		case f.err != nil:
			out = append(out, contentLine{text: "  " + f.err.Error(), dim: true})
		case len(f.lines) == 0:
			out = append(out, contentLine{text: "  " + i18n.T("(empty file)"), dim: true})
		default:
			out = append(out, f.lines...)
		}
	}
	return out
}

// fileAt is the file whose block holds stream line line; -1 before the first.
func (s *fileStream) fileAt(line int) int {
	at := -1
	for i, f := range s.files {
		if line >= f.start {
			at = i
		}
	}
	return at
}

// fileAtOr is fileAt on a stream, -1 with no stream.
func (s *fileStream) fileAtOr(line int) int {
	if s == nil {
		return -1
	}
	return s.fileAt(line)
}

// streamLine is file's 1-based line as a stream index: clamped to the
// file's last line; the header when the file shows no lines (unloaded,
// folded, failed, empty).
func (s *fileStream) streamLine(file, line int) int {
	f := s.files[file]
	if !f.shows() {
		return f.hdr
	}
	line = min(max(line, 1), len(f.lines))
	return f.body + line - 1
}

// lineOf maps (file, offset from its body's first line) back to a stream
// line: an offset above the body (its blank, header, rule) as it was, one in
// a body that no longer shows its lines onto the body's first line.
func (s *fileStream) lineOf(file, off int) int {
	f := s.files[file]
	if off >= 0 && (!f.shows() || off >= len(f.lines)) {
		off = 0
	}
	return max(f.body+off, f.start)
}

// nextHeader is the header after (dir 1) / before (dir -1) stream line from,
// or -1 at the ends. Going back from inside a file stops on its own header
// first.
func (s *fileStream) nextHeader(from, dir int) int {
	at := s.fileAt(from)
	if dir > 0 {
		if at+1 < len(s.files) {
			return s.files[at+1].hdr
		}
		return -1
	}
	switch {
	case at < 0:
		return -1
	case from > s.files[at].hdr:
		return s.files[at].hdr
	case at > 0:
		return s.files[at-1].hdr
	}
	return -1
}
