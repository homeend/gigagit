package domain

import (
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/notemeta"
)

// NoteMeta resolves what to show for note n (spec 2026-10-12-note-meta §4):
// its meta, its declaration — a reply with none uses its root's — and the
// meta added to it later.
func NoteMeta(n model.Note, root *model.Note) notemeta.Resolved {
	d := n.Display
	if len(d) == 0 && root != nil {
		d = root.Display
	}
	return notemeta.Resolve(n.Meta, d, ownAdded(n.Added))
}

// ownAdded is the added meta of the note itself (a review note's per-remark
// entries are its remarks', never its own).
func ownAdded(as []notemeta.Added) []notemeta.Added {
	var out []notemeta.Added
	for _, a := range as {
		if a.RemarkFP == "" {
			out = append(out, a)
		}
	}
	return out
}

// remarkMeta gives a remark's note its meta, its effective declaration
// (document → file → remark) and the meta added to it on its review note.
func (r Review) remarkMeta(n *model.Note, f notebatch.ReviewFile, dn notebatch.ReviewNote, fp string) {
	n.Meta = append([]notemeta.KV(nil), dn.Meta...)
	var doc notemeta.Display
	if r.Doc != nil {
		doc = r.Doc.Display
	}
	n.Display = notemeta.Effective(doc, f.Display, dn.Display)
	n.Added = nil
	for _, a := range r.Added {
		if a.RemarkFP == fp {
			a.RemarkFP = ""
			n.Added = append(n.Added, a)
		}
	}
}

// docRemarkAt is review r's remark number i: its file and its note.
func (r Review) docRemarkAt(i int) (notebatch.ReviewFile, notebatch.ReviewNote, bool) {
	if r.Doc == nil || i < 0 {
		return notebatch.ReviewFile{}, notebatch.ReviewNote{}, false
	}
	for _, f := range r.Doc.Files {
		if i < len(f.Notes) {
			return f, f.Notes[i], true
		}
		i -= len(f.Notes)
	}
	return notebatch.ReviewFile{}, notebatch.ReviewNote{}, false
}
