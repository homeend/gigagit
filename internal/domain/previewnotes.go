package domain

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// errPreviewNotesNeedPath guards PreviewNotesFor/PreviewNotesAt: the
// counts-only, no-path gather lives in loadPreviewNotes(ctx, set, ""), called
// only from PreviewNoteCounts. A caller resolving notes for display always
// has a path in hand (a file view), so an empty one here is a caller bug.
var errPreviewNotesNeedPath = errors.New("preview notes: path is required")

// Notes in merge previews (spec §1).
//
// A preview of source → target shows `git diff target...source`: the merge
// base on the old side, the source TIP on the new side. The new side is byte
// for byte the file at the tip, so a note on it is an ordinary committed note
// — FileAddress{StateCommitted, <tip>, Path}, Side new. Nothing new is stored.
//
// What IS new is the READ: when the agent pushes more commits the tip moves,
// and notes written against the old tip would vanish from the preview. So the
// preview gathers notes along the whole branch (merge-base..source) and
// resolves each one against the tip's content through the SAME resolver the
// ordinary note path uses. A note whose anchored text survived is active; one
// whose lines a later commit changed is stale — which the preview surfaces as
// "outdated", because there it is the expected case rather than an edge one.

// PreviewNoteSet is one preview's note scope: the write target (the source
// tip) and every commit whose notes the preview gathers.
//
// The zero value means "not previewable" — a merged pair, a missing name, no
// common base. That is NOT an error (spec ruling 6): callers test OK() and
// simply show no notes and no badge.
type PreviewNoteSet struct {
	Source, Target string   // the pair's NAMES, as saved/typed
	Tip            string   // full sha of the source tip: the write target
	Base           string   // merge-base(target, source)
	Commits        []string // merge-base..source, newest first (includes Tip)
}

// OK reports whether the pair resolved to a previewable range.
func (set PreviewNoteSet) OK() bool { return set.Tip != "" }

// DiffSpec is THE preview's patch: merge-base → source tip. Every surface that
// numbers hunks under --preview (the CLI's `gg diff --preview --hunks`, the
// note verbs' --hunk N, the batch planner, MCP) builds it from here and
// nowhere else, so they cannot drift apart (ruling 1). `<base>..<tip>` and
// `<targetHash>...<sourceHash>` are the same patch by construction; the base
// is already resolved here, so the two-dot form is the cheaper spelling.
func (set PreviewNoteSet) DiffSpec() model.DiffSpec {
	if !set.OK() {
		return model.DiffSpec{}
	}
	return model.DiffSpec{Rev: set.Base + ".." + set.Tip}
}

// commitSet reports whether a commit belongs to this preview. The membership
// map is built ONCE per query (ruling 3): the store is iterated a single time
// and every note tested against this set, never one store query per commit.
func (set PreviewNoteSet) commitSet() map[string]bool {
	m := make(map[string]bool, len(set.Commits))
	for _, c := range set.Commits {
		m[c] = true
	}
	return m
}

// PreviewNotes resolves a pair into its note set. It reuses PreviewSummary's
// cached (srcHash, tgtHash) entry for the tip and the base, so an unchanged
// pair costs the two rev-parse calls the summary already makes plus one
// rev-list; a moved tip is a new cache key and recomputes everything.
func (s *Service) PreviewNotes(ctx context.Context, source, target string) (PreviewNoteSet, error) {
	sum, err := s.PreviewSummary(ctx, source, target)
	if err != nil {
		return PreviewNoteSet{}, err
	}
	if sum.State != PreviewOK {
		return PreviewNoteSet{}, nil // ruling 6: no set, no error
	}
	key := "preview-revlist:" + sum.SourceHash + ":" + sum.TargetHash
	v, err := s.factory.Cache("preview").GetOrLoad(key, func() (any, error) {
		return query(ctx, s, key, func(ctx context.Context) ([]string, error) {
			return s.repo.RevListRange(ctx, sum.Base(), sum.SourceHash)
		})
	})
	if err != nil {
		return PreviewNoteSet{}, err
	}
	return PreviewNoteSet{
		Source: source, Target: target,
		Tip: sum.SourceHash, Base: sum.Base(),
		Commits: v.([]string),
	}, nil
}

// PreviewResolve turns one --preview argument into a (source, target) pair:
// a saved record's id or label, or git's three-dot form `<target>...<source>`
// (the same order `git diff target...source` reads, and the order Feature B's
// link grammar will use). The three-dot form needs no saved record.
//
// It lives in domain, not in the CLI, because MCP resolves the same string.
func (s *Service) PreviewResolve(ctx context.Context, spec string) (source, target string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", ErrPreviewNotFound
	}
	if i := strings.Index(spec, "..."); i >= 0 {
		target, source = strings.TrimSpace(spec[:i]), strings.TrimSpace(spec[i+3:])
		if target == "" || source == "" {
			return "", "", errPreviewPairShape
		}
		return source, target, nil
	}
	p, err := s.PreviewGet(ctx, spec)
	if err != nil {
		return "", "", err
	}
	return p.Source, p.Target, nil
}

// PreviewStatus is the word a preview uses for a resolved note's status.
// "Outdated" is the preview's name for stale (spec §1.2): in a preview a note
// whose lines a later commit changed is the EXPECTED case, not an edge case,
// so the surfaces say so. model.NoteStatus gains no value — this maps at
// render/wire time only, so the store and the resolver stay untouched.
func PreviewStatus(st model.NoteStatus) string {
	if st == model.NoteStale {
		return "outdated"
	}
	return string(st)
}

// ErrPreviewOldSide is a preview anchor that would land on the old side: the
// preview's old side is the merge base, which no stored address names, so
// there is nothing to anchor to. The only way to reach it is a hunk that
// ONLY deletes lines (no new side at all).
var ErrPreviewOldSide = errors.New("notes in a preview anchor on the new side (that hunk only deletes lines)")

// PreviewHunkAnchor resolves `--hunk N` inside a preview: the side and range a
// note takes, numbered over the PREVIEW's patch (merge-base → tip, ruling 1),
// never over the tip commit's own parent→tip diff. It is the ONE place that
// rule and the old-side refusal live, shared by the CLI's `gg note add
// --preview --hunk N` and the MCP note tools, so the two cannot drift.
func (s *Service) PreviewHunkAnchor(ctx context.Context, set PreviewNoteSet, path string, n int) (model.NoteSide, [2]int, error) {
	if !set.OK() {
		return "", [2]int{}, ErrPreviewNotFound
	}
	// The 1-based check lives HERE, not in each frontend: a 0 or negative hunk
	// is a caller usage error, never "that hunk does not exist in the patch".
	if n < 1 {
		return "", [2]int{}, fmt.Errorf("%w: a hunk number is 1-based", ErrNoteTargetUsage)
	}
	spec := set.DiffSpec()
	spec.Paths = []string{path}
	side, rng, err := s.HunkRange(ctx, spec, path, n)
	if err != nil {
		return "", [2]int{}, err
	}
	if side == model.NoteSideOld {
		return "", [2]int{}, ErrPreviewOldSide
	}
	return side, rng, nil
}

// loadPreviewNotes gathers every stored note the preview covers for one path:
// a committed, NEW-side note whose commit is in the set. The store is iterated
// ONCE against a membership map (ruling 3) — never one query per commit.
//
// Old-side notes on those commits are ignored on purpose (spec §1.2): they
// belong to that commit's own parent→commit picture, not to the
// merge-base → tip one the preview draws. Replies inherit their root's side,
// so this one test covers them too.
func (s *Service) loadPreviewNotes(ctx context.Context, set PreviewNoteSet, path string) ([]model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return nil, err
	}
	in := set.commitSet()
	mine := make([]model.Note, 0, 8)
	for _, n := range all {
		if n.Address.State != model.StateCommitted || n.Side != model.NoteSideNew {
			continue
		}
		if path != "" && n.Address.Path != path {
			continue
		}
		if in[n.Address.Commit] {
			mine = append(mine, n)
		}
	}
	return mine, nil
}

// PreviewNotesFor resolves the preview's notes for one path against a diff the
// caller already holds (the TUI's open compare view). Only the NEW side is
// used: the preview's old side is the merge base, which no stored address
// names, so nothing can anchor there.
//
// path == "" is refused: the counts-only, every-path gather lives in
// loadPreviewNotes(ctx, set, "") behind PreviewNoteCounts, not here.
func (s *Service) PreviewNotesFor(ctx context.Context, set PreviewNoteSet, path string, d Diff) ([]ResolvedNote, error) {
	if !set.OK() {
		return nil, nil
	}
	if path == "" {
		return nil, errPreviewNotesNeedPath
	}
	// A pull request's review threads ride along, appended AFTER the store
	// notes are resolved: they are active by construction and must not pass
	// through resolveNotes (no old side here — a LEFT comment would be dropped).
	forge := s.forgeNotesFor(set, path)
	mine, err := s.loadPreviewNotes(ctx, set, path)
	if err != nil {
		if len(forge) > 0 && errors.Is(err, ErrNotesDisabled) {
			return forge, nil // no note store on this machine: the forge's threads still show
		}
		return nil, err
	}
	if len(mine) == 0 {
		return forge, nil
	}
	_, newLines := diffSideLines(d)
	return append(keepResolved(resolveNotes(mine, nil, newLines)), forge...), nil
}

// PreviewNotesAt is PreviewNotesFor for a caller with no diff in hand (the web
// handler, the CLI, MCP). The preview's new side is byte for byte the file at
// the tip, so the tip's own content is the resolution text — the same bytes
// noteSideLines would read for a committed address.
func (s *Service) PreviewNotesAt(ctx context.Context, set PreviewNoteSet, path string) ([]ResolvedNote, error) {
	if !set.OK() {
		return nil, nil
	}
	// The same guard PreviewNotesFor has, and the one errPreviewNotesNeedPath
	// promises: with no path loadPreviewNotes gathers EVERY file's notes and
	// resolves them against one file's content, which is nobody's contract.
	// The counts-only gather is PreviewNoteCounts' own call.
	if path == "" {
		return nil, errPreviewNotesNeedPath
	}
	mine, err := s.loadPreviewNotes(ctx, set, path)
	if err != nil {
		return nil, err
	}
	if len(mine) == 0 {
		return nil, nil
	}
	var newLines []string
	if b, ferr := s.ShowFile(ctx, set.Tip, path); ferr == nil {
		newLines = splitLines(b)
	}
	// newLines stays nil when the path is gone from the tip: resolveOne then
	// reports orphaned, and keepResolved hides those — exactly the rule the
	// ordinary note path follows for a deleted file.
	return keepResolved(resolveNotes(mine, nil, newLines)), nil
}

// PreviewNotesAll is PreviewNotesAt for EVERY path the preview carries notes
// on: `gg note list --preview P` with no --file, and the MCP read of a whole
// preview. It loads the store ONCE (ruling 3) and groups by path, rather than
// asking PreviewNotesAt per path — that would re-read and re-scan the whole
// store for each file.
//
// Each path is resolved against its own content at the tip, exactly as
// PreviewNotesAt does, so orphans (the path is gone from the tip) stay hidden.
// A path whose notes all resolve away is left OUT of the map, so a caller can
// range over it without testing for empties.
func (s *Service) PreviewNotesAll(ctx context.Context, set PreviewNoteSet) (map[string][]ResolvedNote, error) {
	if !set.OK() {
		return map[string][]ResolvedNote{}, nil
	}
	forge := s.forgeNotesFor(set, "")
	mine, err := s.loadPreviewNotes(ctx, set, "")
	if err != nil {
		if len(forge) == 0 || !errors.Is(err, ErrNotesDisabled) {
			return nil, err
		}
		mine = nil // no store here; the forge's threads still list
	}
	byPath := map[string][]model.Note{}
	for _, n := range mine {
		byPath[n.Address.Path] = append(byPath[n.Address.Path], n)
	}
	paths := make([]string, 0, len(byPath))
	for p := range byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths) // stable output order for the CLI's rows
	out := make(map[string][]ResolvedNote, len(paths))
	for _, p := range paths {
		var newLines []string
		if b, ferr := s.ShowFile(ctx, set.Tip, p); ferr == nil {
			newLines = splitLines(b)
		}
		// newLines nil (the path is gone from the tip) → resolveOne reports
		// orphaned and keepResolved drops it, the ordinary note-path rule.
		if got := keepResolved(resolveNotes(byPath[p], nil, newLines)); len(got) > 0 {
			out[p] = got
		}
	}
	for _, r := range forge { // active by construction: appended, never re-resolved
		out[r.Note.Address.Path] = append(out[r.Note.Address.Path], r)
	}
	return out, nil
}

// PreviewNotePaths are PreviewNotesAll's keys in the order it resolved them —
// sorted, so a caller rendering rows has one stable order to follow.
func PreviewNotePaths(byPath map[string][]ResolvedNote) []string {
	paths := make([]string, 0, len(byPath))
	for p := range byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// previewCountEntry is one cached count result.
type previewCountEntry struct {
	byPath map[string]int
	total  int
}

// PreviewNoteCounts are the preview's badges: root notes per path and in
// total, counted from the STORE without resolving anything. A path-gone
// orphan — its commit stays on the branch, but a later commit removed the
// path from the tip — never draws a row, yet still counts (spec §1.2, the
// "retired file" rule): the Previews panel still says it is there.
//
// A note whose commit is no longer in base..tip at all (the branch was
// rebased) is a different case: it cannot be attributed to this preview, so
// loadPreviewNotes's commitSet membership test excludes it before it ever
// reaches this count (controller ruling; spec amended).
//
// The maps are the cached instance, shared by every caller: READ-ONLY.
func (s *Service) PreviewNoteCounts(ctx context.Context, set PreviewNoteSet) (map[string]int, int, error) {
	byPath, total, err := s.previewStoreCounts(ctx, set)
	fp, ft := s.forgeNoteCounts(set)
	if ft == 0 {
		return byPath, total, err
	}
	if err != nil && !errors.Is(err, ErrNotesDisabled) {
		return byPath, total, err
	}
	// The store's map is the shared cached instance (read-only): merge into a
	// fresh one. The forge half is never cached here — it changes on a refresh
	// the notes generation knows nothing about.
	merged := make(map[string]int, len(byPath)+len(fp))
	for p, n := range byPath {
		merged[p] = n
	}
	for p, n := range fp {
		merged[p] += n
	}
	return merged, total + ft, nil
}

// previewStoreCounts is the stored-notes half of PreviewNoteCounts.
func (s *Service) previewStoreCounts(ctx context.Context, set PreviewNoteSet) (map[string]int, int, error) {
	if !set.OK() {
		return map[string]int{}, 0, nil
	}
	key := set.Tip + ":" + set.Base
	s.mu.Lock()
	if e, ok := s.previewCounts[key]; ok {
		s.mu.Unlock()
		return e.byPath, e.total, nil
	}
	gen := s.notesGen
	s.mu.Unlock()

	mine, err := s.loadPreviewNotes(ctx, set, "")
	if err != nil {
		return map[string]int{}, 0, err
	}
	e := previewCountEntry{byPath: map[string]int{}}
	for _, n := range mine {
		if n.IsReply() { // a badge counts THREADS
			continue
		}
		e.total++
		if n.Address.Path != "" {
			e.byPath[n.Address.Path]++
		}
	}
	s.mu.Lock()
	if s.notesGen == gen { // a mutation raced this computation: drop it
		if s.previewCounts == nil {
			s.previewCounts = map[string]previewCountEntry{}
		}
		s.previewCounts[key] = e
	}
	s.mu.Unlock()
	return e.byPath, e.total, nil
}
