package domain

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// ErrReadOnlyNote is returned by every note mutation handed a forge comment's
// id: gg reads the forge and never writes to it.
var ErrReadOnlyNote = errors.New("forge comments are read-only")

// PRCommentsRefresh re-reads PR n's comments from the forge and caches them
// for the note readers below. It is the ONLY network call on this path —
// every read (PreviewNotesFor, PreviewNoteCounts, …) consults the cache alone,
// so opening a PR's diff never waits on the forge CLI. changed reports whether
// the comments differ from what was cached. A failed read keeps the previous
// cache.
func (s *Service) PRCommentsRefresh(ctx context.Context, n int) (changed bool, err error) {
	c, err := s.PRComments(ctx, n) // provider call: never under forgeMu
	if err != nil {
		return false, err
	}
	sig := prCommentsSig(c)
	s.forgeMu.Lock()
	defer s.forgeMu.Unlock()
	if s.forgeComments == nil {
		s.forgeComments = map[int]forgeCommentsEntry{}
	}
	prev, had := s.forgeComments[n]
	s.forgeComments[n] = forgeCommentsEntry{c: c, sig: sig}
	return !had || prev.sig != sig, nil
}

type forgeCommentsEntry struct {
	c   PRComments
	sig string
}

// prCommentsSig is a stable fingerprint of what a reader would draw: ids,
// bodies, positions, the resolved flag and the update stamp. Never a
// reflect.DeepEqual over the structs — time.Time's internals differ between
// two parses of the same instant.
func prCommentsSig(c PRComments) string {
	var b strings.Builder
	for _, bucket := range [][]model.ForgeComment{c.Inline, c.Hub, c.Outdated} {
		for _, m := range bucket {
			b.WriteString(m.ID + "\x00" + m.ParentID + "\x00" + m.Path + "\x00" + string(m.Side) + "\x00" +
				strconv.Itoa(m.StartLine) + ":" + strconv.Itoa(m.Line) + "\x00" + strconv.FormatBool(m.Resolved) + "\x00" +
				strconv.FormatInt(m.Updated.Unix(), 10) + "\x00" + m.Verdict + "\x00" + m.Body + "\x01")
		}
		b.WriteString("\x02")
	}
	return b.String()
}

// dropForgeComments forgets PR n's cached comments (ForgetPR).
func (s *Service) dropForgeComments(n int) {
	s.forgeMu.Lock()
	delete(s.forgeComments, n)
	s.forgeMu.Unlock()
}

// forgeInline is the cached inline + file-level threads of the pull request a
// preview set shows, or nil when the set is not a PR (its source is not gg's
// private refs/gg/pr/<n>) or nothing has been fetched yet.
func (s *Service) forgeInline(set PreviewNoteSet) []model.ForgeComment {
	n, ok := git.ParsePRRef(set.Source)
	if !ok {
		return nil
	}
	s.forgeMu.Lock()
	defer s.forgeMu.Unlock()
	return s.forgeComments[n].c.Inline
}

// forgeNotesFor converts the PR's cached threads on path ("" = every path)
// into resolved notes: one root per thread, replies nested, in the forge's
// thread order. They are ACTIVE by construction — the forge already vouched
// for the position — so they never pass through resolveNotes (which, on a
// preview, has no old side and would drop every LEFT-side comment as stale).
func (s *Service) forgeNotesFor(set PreviewNoteSet, path string) []ResolvedNote {
	inline := s.forgeInline(set)
	if len(inline) == 0 {
		return nil
	}
	var roots []ResolvedNote
	rootOf := map[string]int{} // comment id → index into roots
	for _, c := range inline {
		if path != "" && c.Path != path {
			continue
		}
		rn := forgeNote(c, set.Tip)
		if c.ParentID != "" {
			if i, ok := rootOf[c.ParentID]; ok {
				// A reply inherits its thread's anchor, like a stored reply.
				rn.Note.ParentID = roots[i].Note.ID
				rn.Note.Side, rn.Note.Range, rn.Range = roots[i].Note.Side, roots[i].Note.Range, roots[i].Range
				roots[i].Replies = append(roots[i].Replies, rn)
				rootOf[c.ID] = i // a reply to a reply still hangs off the root
				continue
			}
			rn.Note.ParentID = "" // its parent is not here: show it on its own
		}
		rootOf[c.ID] = len(roots)
		roots = append(roots, rn)
	}
	return roots
}

// forgeNote is one comment as a note. The first body line is the summary (the
// bold row), the rest the rationale; a file-level comment has Range{0,0}, the
// renderer's "top of the file" sentinel.
func forgeNote(c model.ForgeComment, tip string) ResolvedNote {
	body := strings.TrimSpace(strings.ReplaceAll(c.Body, "\r\n", "\n"))
	summary, rest, _ := strings.Cut(body, "\n")
	side := c.Side
	if side == "" {
		side = model.NoteSideNew
	}
	var rng [2]int
	if c.Kind != model.ForgeCommentFile && c.Line > 0 {
		rng = [2]int{c.Line, c.Line}
		if c.StartLine > 0 && c.StartLine < c.Line {
			rng[0] = c.StartLine
		}
	}
	n := model.Note{
		ID: model.ForgeNoteIDPrefix + c.ID, Source: model.NoteSourceForge, Author: c.Author,
		Address: model.FileAddress{State: model.StateCommitted, Commit: tip, Path: c.Path},
		Side:    side, Range: rng,
		Summary: strings.TrimSpace(summary), Rationale: strings.TrimSpace(rest),
		Created: c.Created, Updated: c.Updated,
	}
	if c.ParentID != "" {
		n.ParentID = model.ForgeNoteIDPrefix + c.ParentID
	}
	if c.Resolved {
		n.Tags = []string{model.NoteTagResolved}
	}
	return ResolvedNote{Note: n, Status: model.NoteActive, Range: rng}
}

// forgeNoteCounts counts the PR's cached threads per path (roots only — a
// badge counts threads).
func (s *Service) forgeNoteCounts(set PreviewNoteSet) (map[string]int, int) {
	byPath, total := map[string]int{}, 0
	for _, r := range s.forgeNotesFor(set, "") {
		byPath[r.Note.Address.Path]++
		total++
	}
	return byPath, total
}
