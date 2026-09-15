package domain

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
	for _, rep := range r.Replies {
		w.Replies = append(w.Replies, ToWireNote(rep))
	}
	return w
}

// ToWireNotePreview is ToWireNote with the preview's status word. The wire
// shape is unchanged: only the `status` string differs, and only when the
// caller is rendering a merge preview (spec §1.2 — a preview calls stale
// "outdated", because there it is the expected case).
func ToWireNotePreview(r ResolvedNote, preview bool) WireNote {
	w := ToWireNote(r)
	if preview {
		w.Status = PreviewStatus(r.Status)
	}
	return w
}
