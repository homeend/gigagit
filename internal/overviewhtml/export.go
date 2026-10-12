// Package overviewhtml renders an overview as ONE self-contained HTML page
// (spec 2026-10-12-overview-html-export): the overview's markdown tree, its
// anchors, and every anchored file's frozen lines and diff, inlined beside
// gg web's own painters (markdown.js, splitdiff.js, style.css) and a small
// split-view script — a page that works from disk with no gg and no network.
//
// It renders plain data: no git, no domain, no frontend. The data's file and
// diff rows are gg web's wire shapes, so the reused painters read them
// unchanged. DAG leaf (markdown, syntax, textdiff, web/static).
package overviewhtml

import (
	"strings"

	"github.com/homeend/gigagit/internal/markdown"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

// Export is the page's data — the JSON blob the page script reads.
type Export struct {
	Title      string           `json:"title"`
	Repo       string           `json:"repo"`
	Kind       string           `json:"kind"` // "agent" | "review"
	Left       string           `json:"left"`
	Right      string           `json:"right"`
	ExportedAt string           `json:"exported_at"`
	Version    string           `json:"version"`
	Blocks     []markdown.Block `json:"blocks"`
	Anchors    []Anchor         `json:"anchors"`
	Files      []File           `json:"files"`
}

// Anchor is one overview anchor, index-aligned with the markdown tree's
// anchor numbers. A Missing anchor draws as plain text.
type Anchor struct {
	Dest    string `json:"dest"`
	Path    string `json:"path,omitempty"`
	Start   int    `json:"start,omitempty"`
	End     int    `json:"end,omitempty"`
	Label   string `json:"label,omitempty"`
	Missing bool   `json:"missing"`
}

// File is one anchored file: its lines at the frozen version and its diff
// (none when the range leaves it unchanged). A Placeholder (binary, too
// large) stands in for both.
type File struct {
	Path        string    `json:"path"`
	Status      string    `json:"status,omitempty"`
	OldPath     string    `json:"old_path,omitempty"`
	Lines       []Line    `json:"lines"`
	Changed     bool      `json:"changed"`
	Diff        []DiffRow `json:"diff,omitempty"`
	Placeholder string    `json:"placeholder,omitempty"`
}

// Line is one source line with its syntax runs.
type Line struct {
	Text string `json:"text"`
	Tok  []Tok  `json:"tok,omitempty"`
}

// Tok is one syntax run on the wire: [start, end, class] — the triple
// gg web's /api/file-content and /api/diff send.
type Tok [3]any

// DiffRow is /api/diff's row shape: the fields splitdiff.js reads.
type DiffRow struct {
	Kind       string   `json:"kind"`
	Left       string   `json:"left"`
	Right      string   `json:"right"`
	LeftNo     int      `json:"left_no"`
	RightNo    int      `json:"right_no"`
	LeftSpans  [][2]int `json:"left_spans,omitempty"`
	RightSpans [][2]int `json:"right_spans,omitempty"`
	LeftTok    []Tok    `json:"left_tok,omitempty"`
	RightTok   []Tok    `json:"right_tok,omitempty"`
}

// unstyled are classes style.css paints nothing for (internal/web's
// unstyledOnWire — keep the two in step): sending them costs bytes only.
var unstyled = map[syntax.Class]bool{syntax.Name: true}

// Toks is the wire form of side's runs for source line no (1-based); nil
// when there is no such line or it has no styled run.
func Toks(side [][]syntax.Tok, no int) []Tok {
	if no <= 0 || no > len(side) || len(side[no-1]) == 0 {
		return nil
	}
	var out []Tok
	for _, tk := range side[no-1] {
		if !unstyled[tk.Class] {
			out = append(out, Tok{tk.Start, tk.End, tk.Class.String()})
		}
	}
	return out
}

// Lines pairs each line of text with its runs (tok may be nil: unlexed).
func Lines(text []string, tok [][]syntax.Tok) []Line {
	out := make([]Line, len(text))
	for i, l := range text {
		out[i] = Line{Text: l, Tok: Toks(tok, i+1)}
	}
	return out
}

func kindOf(k textdiff.Kind) string {
	switch k {
	case textdiff.Changed:
		return "change"
	case textdiff.Del:
		return "del"
	case textdiff.Add:
		return "add"
	}
	return "same"
}

func spans(s []textdiff.Span) [][2]int {
	if len(s) == 0 {
		return nil
	}
	out := make([][2]int, len(s))
	for i, sp := range s {
		out[i] = [2]int{sp.Start, sp.End}
	}
	return out
}

// Rows is an aligned diff in /api/diff's row shape, with each side's runs.
func Rows(rows []textdiff.Row, oldTok, newTok [][]syntax.Tok) []DiffRow {
	out := make([]DiffRow, len(rows))
	for i, r := range rows {
		out[i] = DiffRow{Kind: kindOf(r.Kind), Left: r.Left, Right: r.Right, LeftNo: r.LeftNo, RightNo: r.RightNo,
			LeftSpans: spans(r.LeftSpans), RightSpans: spans(r.RightSpans),
			LeftTok: Toks(oldTok, r.LeftNo), RightTok: Toks(newTok, r.RightNo)}
	}
	return out
}

// maxSlug caps the file name stem.
const maxSlug = 60

// Slug is a file name stem for title: ASCII letters and digits, every other
// run one dash, at most 60 bytes; "overview" when nothing is left.
func Slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	s := b.String()
	if len(s) > maxSlug {
		s = strings.TrimRight(s[:maxSlug], "-")
	}
	if s == "" {
		return "overview"
	}
	return s
}
