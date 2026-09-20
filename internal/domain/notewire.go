package domain

import (
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/markdown"
	"github.com/homeend/gigagit/internal/model"
)

// WireNote is the JSON projection of one resolved note thread, shared by every
// frontend that speaks JSON: the web handlers, `gg note list --json` and the
// MCP note tools. It lives in domain because internal/cli and internal/mcp
// cannot import internal/web, and three hand-rolled shapes would drift.
type WireNote struct {
	ID        string     `json:"id"`
	ParentID  string     `json:"parent_id,omitempty"`
	Source    string     `json:"source"`
	Author    string     `json:"author,omitempty"`
	Path      string     `json:"path,omitempty"`
	Rev       string     `json:"rev,omitempty"`
	Side      string     `json:"side"`
	Line      int        `json:"line"`
	Range     [2]int     `json:"range"`
	Summary   string     `json:"summary"`
	Rationale string     `json:"rationale,omitempty"`
	Status    string     `json:"status"`
	Replies   []WireNote `json:"replies,omitempty"`
	// ReadOnly marks a forge review comment: gg shows it and never edits,
	// answers or removes it. Resolved is the forge's own thread flag; FileLevel
	// a comment on the whole file (no line — it renders above the file).
	ReadOnly  bool   `json:"read_only,omitempty"`
	Resolved  bool   `json:"resolved,omitempty"`
	FileLevel bool   `json:"file_level,omitempty"`
	Created   string `json:"created,omitempty"` // RFC 3339; empty when unknown
	// MD is a FORGE note's rationale parsed as markdown and SummaryMD its
	// summary line's inline tree; a page paints them instead of the plain
	// strings above. Only ToWireNoteRendered fills them, and never for a
	// stored note: that is shown as typed.
	MD        *MarkdownDoc      `json:"md,omitempty"`
	SummaryMD []markdown.Inline `json:"summary_md,omitempty"`
}

// ToWireNote flattens one resolved thread. Line and Range are the RESOLVED
// anchor, not the stored one: a note that moved must render where its text is
// now. Line stays the range END for the web page that already reads it; Range
// carries both ends, which a multi-line hunk anchor needs.
func ToWireNote(r ResolvedNote) WireNote {
	w := WireNote{
		ID: r.Note.ID, ParentID: r.Note.ParentID, Source: string(r.Note.Source),
		Author: r.Note.Author, Path: r.Note.Address.Path, Rev: r.Note.Address.Commit,
		Side: string(r.Note.Side), Line: r.Range[1], Range: r.Range,
		Summary: r.Note.Summary, Rationale: r.Note.Rationale, Status: string(r.Status),
	}
	if r.Note.Source == model.NoteSourceForge {
		w.ReadOnly = true
		w.Resolved = model.NoteHasTag(r.Note, model.NoteTagResolved)
		w.FileLevel = r.Range == [2]int{}
	}
	if !r.Note.Created.IsZero() {
		w.Created = r.Note.Created.UTC().Format(time.RFC3339)
	}
	for _, rep := range r.Replies {
		w.Replies = append(w.Replies, ToWireNote(rep))
	}
	return w
}

// ToWireNotePreview is ToWireNote with the preview's status word. The wire
// shape is unchanged: only the `status` string differs, and only when the
// caller is rendering a merge preview (spec §1.2 — a preview calls stale
// "outdated", because there it is the expected case).
// It recurses into the replies: a reply inherits its root's anchor, so one
// thread must never report two words for the same state.
func ToWireNotePreview(r ResolvedNote, preview bool) WireNote {
	w := ToWireNote(r)
	if !preview {
		return w
	}
	w.Status = PreviewStatus(r.Status)
	for i, rep := range r.Replies {
		w.Replies[i] = ToWireNotePreview(rep, true)
	}
	return w
}

// ToWireNoteRendered is ToWireNotePreview for a frontend that RENDERS notes
// (the web page): forge notes also carry their parsed markdown. The JSON the
// CLI and MCP hand to agents goes through ToWireNote/ToWireNotePreview and
// stays free of the trees — an agent reads the raw markdown.
func ToWireNoteRendered(r ResolvedNote, preview bool) WireNote {
	w := ToWireNotePreview(r, preview)
	attachMarkdown(&w, r)
	return w
}

func attachMarkdown(w *WireNote, r ResolvedNote) {
	if r.Note.Source == model.NoteSourceForge {
		w.MD = ParseMarkdown(r.Note.Rationale)
		w.SummaryMD = markdown.ParseInline(r.SummarySrc)
	}
	for i := range r.Replies {
		attachMarkdown(&w.Replies[i], r.Replies[i])
	}
}

// MarkdownDoc is a parsed forge text. The alias lets a frontend hold one
// without importing the parser.
type MarkdownDoc = markdown.Doc

// ParseMarkdown parses forge text (a PR description, a comment); nil for
// blank text, so a wire field built from it is omitted.
func ParseMarkdown(src string) *MarkdownDoc {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	d := markdown.Parse(src)
	return &d
}
