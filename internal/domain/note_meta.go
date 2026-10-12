package domain

import (
	"context"
	"errors"
	"fmt"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/notemeta"
	"github.com/homeend/gigagit/internal/notes"
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

// ErrRemarkDisplay refuses a declaration on one remark: a declaration
// belongs to whoever wrote the meta it describes — the review's agent.
var ErrRemarkDisplay = errors.New("a remark's display is its review's")

// SetNoteMeta writes kv onto note id (spec §2.4, §5.1). On a review remark
// ("review:<id>:<n>") every entry is added meta on its review note, keyed
// by the remark's fingerprint; on a stored note it rewrites the author's
// meta when by is the note's author, else it is added meta. Over-limit
// entries are dropped and reported.
func (s *Service) SetNoteMeta(ctx context.Context, id, by string, kv []notemeta.KV) ([]notemeta.Problem, error) {
	kv, probs := notemeta.CheckMeta(kv)
	err := s.editMeta(ctx, id, func(n *model.Note, fp string) {
		for _, e := range kv {
			if fp == "" && by == n.Author {
				n.Meta = upsertKV(n.Meta, e)
				continue
			}
			n.Added = upsertAdded(n.Added, notemeta.Added{RemarkFP: fp, Key: e.Key, Value: e.Value, By: by, At: notes.Now().UTC()})
		}
		if len(n.Meta) > notemeta.MaxMetaKeys {
			n.Meta = n.Meta[:notemeta.MaxMetaKeys]
		}
	})
	return probs, err
}

// UnsetNoteMeta removes keys: from the author's meta when by is the author
// of a stored note, else from the added meta.
func (s *Service) UnsetNoteMeta(ctx context.Context, id, by string, keys []string) error {
	drop := map[string]bool{}
	for _, k := range keys {
		drop[k] = true
	}
	return s.editMeta(ctx, id, func(n *model.Note, fp string) {
		if fp == "" && by == n.Author {
			kept := n.Meta[:0:0]
			for _, e := range n.Meta {
				if !drop[e.Key] {
					kept = append(kept, e)
				}
			}
			n.Meta = kept
			return
		}
		kept := n.Added[:0:0]
		for _, a := range n.Added {
			if a.RemarkFP != fp || !drop[a.Key] {
				kept = append(kept, a)
			}
		}
		n.Added = kept
	})
}

// SetNoteDisplay replaces a stored note's own declaration (empty clears).
func (s *Service) SetNoteDisplay(ctx context.Context, id string, d notemeta.Display) error {
	if model.IsReviewNoteID(id) {
		return ErrRemarkDisplay
	}
	return s.editMeta(ctx, id, func(n *model.Note, _ string) {
		if len(d) == 0 {
			n.Display = nil
			return
		}
		n.Display = d
	})
}

// editMeta runs fn on the stored note that holds id's meta under its part
// lock: the review note for a remark (fp = the remark's fingerprint), the
// note itself otherwise (fp = "").
func (s *Service) editMeta(ctx context.Context, id string, fn func(n *model.Note, fp string)) error {
	if model.IsForgeNoteID(id) {
		return ErrReadOnlyNote
	}
	st := s.notesStore(ctx)
	if st == nil {
		return ErrNotesDisabled
	}
	target, fp := id, ""
	if rid, idx, ok := model.ParseReviewNoteID(id); ok {
		r, err := s.Review(ctx, rid)
		if err != nil {
			return err
		}
		fps := r.remarkFPs()
		if idx >= len(fps) {
			return fmt.Errorf("%w: review %s has no remark %d", ErrNoSuchRemark, rid, idx)
		}
		target, fp = rid, fps[idx]
	}
	err := st.Edit(target, func(n *model.Note) error {
		fn(n, fp)
		return nil
	})
	if errors.Is(err, notes.ErrNotFound) {
		return ErrNoteNotFound
	}
	if err == nil {
		s.invalidateNoteCounts()
	}
	return err
}

func upsertKV(kv []notemeta.KV, e notemeta.KV) []notemeta.KV {
	for i := range kv {
		if kv[i].Key == e.Key {
			kv[i].Value = e.Value
			return kv
		}
	}
	return append(kv, e)
}

func upsertAdded(as []notemeta.Added, e notemeta.Added) []notemeta.Added {
	for i := range as {
		if as[i].RemarkFP == e.RemarkFP && as[i].Key == e.Key {
			as[i] = e
			return as
		}
	}
	return append(as, e)
}
