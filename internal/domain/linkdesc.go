package domain

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// DescMax caps the free-text portion of a stored Desc — a commit subject or
// a bookmark/shelf label is otherwise unbounded — so one row of `gg links`
// stays one line.
const DescMax = 60

// truncateDesc bounds s to DescMax runes, trimming surrounding whitespace
// first. Rune-safe: cutting mid-multibyte-character would corrupt the tail.
func truncateDesc(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= DescMax {
		return s
	}
	return string(r[:DescMax])
}

// LinkDesc is the human label stored with a copied link (ruling R7: captured
// at creation, never derived at read time — the describing context, which
// row the user was on, is gone by the time anything lists this). The forms
// are spec §4.3's table:
//
//	branch:   <name>
//	bookmark: <label>
//	shelf:    <label>
//	preview:  <target>...<source>
//	commit:   <short> <subject>
//	stash:    <subject>
//	file:     <path>
//
// commit and stash are the only kinds whose free text is a SUBJECT rather
// than the id itself (a stash's subject is the only thing that makes its
// row recognisable once stash@{N} is gone from the link); every other kind,
// including a caller-chosen fallback kind for a shape the table has no row
// for, prints "<kind>: <id>".
func LinkDesc(kind, id, subject string) string {
	switch kind {
	case "commit":
		return "commit: " + id + " " + truncateDesc(subject)
	case "stash":
		return "stash: " + truncateDesc(subject)
	default:
		return kind + ": " + truncateDesc(id)
	}
}

// linkDescFields decides what to pass linkDesc for l, the link a producer
// (`gg link`, `gg compare`) is about to record. Priority:
//
//  1. A copy-source HINT (bookmark/shelf/stash) names the surface the user
//     copied FROM, and wins over the link's own target — spec §4.3's own
//     bookmark/shelf example links both address a commit target, and their
//     Desc is still "bookmark: …" / "shelf: …", not "commit: …".
//  2. Otherwise a stash-shaped pair: "stash: <subject>" (spec §3.4).
//  3. Otherwise a PATH, read at any point or inside any pair or preview:
//     "file: <path>". The point is already in the link text; the path is
//     what makes the row recognisable.
//  4. Otherwise the link's own target: preview, branch (ref) or commit (rev).
//  5. Otherwise the shape has NO row in spec §4.3's table — a --pair
//     change-set link, a --cached link, or the bare working tree with no
//     path and no target flags. Rather than invent a false row (or leave
//     Desc blank), these fall back to kind "link" with id = the link's own
//     text, which linkDesc's default arm renders as "link: gg://…".
//
// Every lookup here is BEST-EFFORT: a bookmark/shelf that no longer exists,
// or a commit git can no longer show, falls back to the raw id rather than
// making the record — or the copy it describes — fail.
func (s *Service) linkDescFields(ctx context.Context, l model.Link) (kind, id, subject string) {
	switch l.Hint.Kind {
	case "bookmark":
		label := l.Hint.ID
		if b, err := s.BookmarkGet(ctx, l.Hint.ID); err == nil && b.Label != "" {
			label = b.Label
		}
		return "bookmark", label, ""
	case "shelf":
		label := l.Hint.ID
		if e, err := s.ShelfFind(ctx, l.Hint.ID); err == nil && e.Label != "" {
			label = e.Label
		}
		return "shelf", label, ""
	case "stash":
		// No producer wired in this task creates a stash-hinted link (`gg
		// link` has no --stash flag): the grammar and linkhist.Entry both
		// carry the shape, but nothing populates it yet. The hint's own id
		// (a stash index, e.g. "0") is the best available fallback until a
		// producer resolves it to the stash's actual subject.
		return "stash", "", l.Hint.ID
	}
	switch {
	case l.Target.Pair != nil:
		// A stash is copied as the PAIR it changes (spec §3.4), and once
		// stash@{N} is gone from the link its subject is the only thing that
		// makes the row recognisable. Best-effort: anything that fails here
		// falls to the "link:" fallback below.
		if pa, err := s.resolveHalf(ctx, l.Target.Pair.A); err == nil {
			if pb, err := s.resolveHalf(ctx, l.Target.Pair.B); err == nil {
				if subj, ok := s.looksLikeAStash(ctx, pa, pb); ok {
					return "stash", "", subj
				}
			}
		}
		if l.Path != "" {
			return "file", l.Path, ""
		}
		return "link", l.String(), ""
	case l.Path != "":
		// A path wins over the point it is read at, as the web's copy rows have
		// always recorded it ("file: <path>" at any rev). Tested the other way
		// round, one file at two refs titled a comparison "branch: feat/x ↔
		// branch: main".
		return "file", l.Path, ""
	case l.Target.Preview != nil:
		return "preview", l.Target.Preview.Target + "..." + l.Target.Preview.Source, ""
	case l.Target.Ref != "":
		return "branch", l.Target.Ref, ""
	case l.Target.Commit != "":
		short := l.Target.Commit
		if len(short) > 7 {
			short = short[:7]
		}
		subj := ""
		if line, found, err := s.CommitLookup(ctx, l.Target.Commit); err == nil && found {
			subj = line.Subject
		}
		return "commit", short, subj
	default:
		// A --pair change-set (no single name to show), --cached, or the
		// bare working tree: none has a row in spec §4.3's table.
		return "link", l.String(), ""
	}
}

// DescribeLink is the label a copied link is stored under — the ONE describer
// the CLI, the TUI and (through its gated JS twin) the web share. It lives in
// domain because domain owns the history store and every lookup a label
// needs; it used to live in internal/cli, where the TUI could not reach it.
//
// A link that arrives as TEXT and is then parsed may be a LOCAL-form file link,
// which carries the checkout and the file undivided in Repo.Abs with Path
// empty (model.LinkRepo; EvalLink's doc comment names the same trap). Left
// unsplit it would describe as the "link:" fallback while the very same text,
// built by `gg link <path>` from a structured link, describes as "file:
// <path>". So the path is located first — through LocateLink, the one splitter
// (it owns the path normalisation, Windows drive form included) — against THIS
// checkout only. Best-effort: a link into some other directory stays as it is.
func (s *Service) DescribeLink(ctx context.Context, l model.Link) string {
	if l.Repo.Abs != "" && l.Path == "" {
		if _, rel, err := LocateLink(ctx, l, ResolveOpts{Cwd: s}); err == nil && rel != "" {
			l.Path = rel
		}
	}
	return LinkDesc(s.linkDescFields(ctx, l))
}

// RecordCopiedLink records text in the copied-link history when — and only
// when — it IS a link. A frontend hands it whatever reached the clipboard;
// text that does not parse records nothing. Best-effort, like RecordLink.
func (s *Service) RecordCopiedLink(ctx context.Context, text string) {
	l, err := model.ParseLink(text)
	if err != nil {
		return
	}
	s.RecordLink(ctx, text, s.DescribeLink(ctx, l))
}
