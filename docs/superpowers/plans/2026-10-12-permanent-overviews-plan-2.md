# Permanent Overviews — Plan 2 (TUI) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (this repo forbids implementer subagents — CLAUDE.md). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make permanent overviews visible and openable in the TUI — a `≡` marker cell on commit rows, a Branches sub-row, a commit `.` menu entry, View all notes, the PR view — opening as the overview split on the files at the overview's commit; and switch the gg-overview skill to permanent-by-default.

**Architecture:** Domain gains the read helpers the split needs (resolved anchors over the whole tree, remarks on a file, the file's diff, PR overviews, View-all-notes entries, HTML export) by presenting an overview as a review-shaped value to the existing review readers. The TUI adds one open-file source kind, `srcCommitOverview`, beside `srcReviewOverview`; every place that means "a stored overview" asks `kind.storedOverview()`, and the three domain calls (remarks, diff, export) switch on the kind.

**Tech Stack:** Go 1.26, Bubble Tea TUI (`internal/tui`), `internal/domain`, the e2e harness with `.screens` golden files.

**Spec:** `docs/superpowers/specs/2026-10-12-permanent-overviews-design.md` (§6, §9, §9a; §11 plan 2). Plan 1 (merged `17ddc122`) built storage + CLI.

## Global Constraints

- `internal/tui` never imports `internal/git`; it reaches git through `domain` (archtest).
- Every user-visible TUI string goes through `i18n.T` with a literal key present in ja/ko/zh/ru (`internal/i18n/lang/*.toml`); the AST gates in `internal/tui` fail otherwise.
- A permanent overview is shown only as itself (R9): its remarks appear only on the right side of ITS split.
- A commit overview shows from any branch the commit is on (R5); the Branches sub-row only while the branch's tip IS the overview's commit and its recorded branch is that branch; a PR overview shows only in its PR's view (R10).
- **User ruling (2026-10-12): the Commits row's marker area grows from 4 to 5 cells — `[↓][↑/badge][✎][≡][space]`.** Every commit row shifts one column; the `.screens` goldens change by exactly that shift.
- Row menus on an overview row (Branches sub-row, View all notes, PR view) = **Open + Delete** (spec §9; the "note rows = Open + Delete only" ruling).
- New tests call `t.Parallel()`; TUI tests build repos with the existing helpers (`testRepo`, see `internal/tui/*_test.go`).
- After every TUI change that adds a key or a menu row: help (`help.go`) and footer advertise it (memory: advertise features in help AND footer).
- `y` on a permanent overview keeps copying its TEXT, as on every overview (spec §9 said "copies its gg:// link" — ruling: one meaning for `y` across overviews; the link is ctrl+l in View all notes and `gg overview show`). Recorded as a spec amendment in Task 8.

## Review Focus

1. An overview whose commit is gone (rewritten history) — expected: the `.` menu / View all notes row still opens it; anchors draw plain, the right pane says the file is not there; never a crash. Pinned in Task 3 (`TestCommitOverviewMissingCommitOpens`).
2. Two overviews on one commit — expected: two `.` menu entries and two Branches sub-rows, each opening its own; the marker is still one `≡`. Pinned in Task 4 (`TestTwoOverviewsTwoRows`).
3. An overview deleted (from the CLI, or another window) while its split is open — expected (spec §9): when the notes counts reload without it, the open overview closes with the status line "the overview was deleted"; its rows vanish. Pinned in Task 4 (`TestOverviewRowsFollowReload`, `TestGoneOverviewCloses`).
4. A narrow terminal (< 60 cols) — expected: the overview opens full-screen (the existing `showOverview` door). Pinned in Task 3 (`TestCommitOverviewNarrowOpensFullScreen`).
5. `D` on a file the overview's commit does not change — expected: the existing "not changed" status line, the file stays. Pinned in Task 3 (`TestCommitOverviewDiffUntouchedFile`).

---

### Task 1: Domain — the split's reads for a permanent overview

**Files:**
- Create: `internal/domain/overview_view.go`
- Modify: `internal/domain/review_overview.go` (extract the anchor resolver), `internal/domain/review_file_diff.go` (extract `reviewFileDiffOf`), `internal/domain/review_doc.go` (extract `reviewNotesOf`), `internal/domain/overview_read.go` (`OverviewHead.Older`), `internal/domain/notes_overview.go` (`NoteCommitNotes.Overviews`), `internal/domain/overview_export.go` (`OverviewSource.OverviewID`)
- Test: `internal/domain/overview_view_test.go`

**Interfaces:**
- Consumes: plan 1's `Overview`, `OverviewHead`, `(*Service).Overview`, `Overviews`, `OverviewsForCommit`, `NoteCounts.PreviewOverviews`, `OverviewDoc`/`OverviewAnchor` (review_overview.go), `ReviewFileDiff` (type), `reviewDocNotes`, `SameNoteScope`.
- Produces:
  ```go
  func (s *Service) OverviewDocAt(ctx context.Context, id string) (OverviewDoc, error)            // anchors OK = path in the commit's tree + lines in the file
  func (s *Service) OverviewFileNotes(ctx context.Context, id, path string, lines []string) ([]ResolvedNote, error) // remarks on path (ids "review:<id>:<n>", read-only)
  func (s *Service) OverviewFileDiff(ctx context.Context, id, path string) (ReviewFileDiff, error) // commit^..commit (a PR: merge-base..head)
  func (s *Service) PROverviews(ctx context.Context, set PreviewNoteSet) ([]OverviewHead, error)  // a PR's overviews, Older when its head moved on
  // OverviewHead gains: Older bool
  // NoteCommitNotes gains: Overviews []Overview  (commit overviews only; never a PR's)
  // OverviewSource gains: OverviewID string
  ```

- [ ] **Step 1: Write the failing tests** — `internal/domain/overview_view_test.go`:

```go
package domain

import (
	"context"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// a.go (5 lines) and b.go (3 lines) on main (permOvRepo), then c.go added on
// HEAD; the overview links a.go (untouched by HEAD) and c.go (added by HEAD).
func viewRepo(t *testing.T) (string, *Service, string) {
	t.Helper()
	dir, svc := permOvRepo(t)
	commitFile(t, dir, "c.go", "c1\nc2\n", "add c")
	got, err := saveOv(t, svc, ovDoc("Tour", "[a](a.go:2) [c](c.go:1-2) [file](b.go)",
		`[{"path":"a.go","annotations":[{"newRange":[2,2],"summary":"two"}]},{"path":"c.go","annotations":[{"newRange":[1,1],"summary":"c one"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	return dir, svc, got.ID
}

func TestOverviewDocAtResolvesTheWholeTree(t *testing.T) {
	t.Parallel()
	_, svc, id := viewRepo(t)
	od, err := svc.OverviewDocAt(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var ok []string
	for _, a := range od.Anchors {
		if a.OK {
			ok = append(ok, a.Dest)
		}
	}
	// a.go is NOT changed by the commit, yet its anchor resolves: the whole tree counts.
	if !slices.Equal(ok, []string{"a.go:2", "c.go:1-2", "b.go"}) {
		t.Fatalf("resolved = %v", ok)
	}
}

func TestOverviewFileNotesAndDiff(t *testing.T) {
	t.Parallel()
	_, svc, id := viewRepo(t)
	ctx := context.Background()
	ns, err := svc.OverviewFileNotes(ctx, id, "a.go", []string{"1", "2", "3", "4", "5"})
	if err != nil || len(ns) != 1 || ns[0].Note.Summary != "two" || ns[0].Range != [2]int{2, 2} || !model.IsReviewNoteID(ns[0].Note.ID) {
		t.Fatalf("notes = %+v %v", ns, err)
	}
	d, err := svc.OverviewFileDiff(ctx, id, "c.go")
	if err != nil || !d.Changed || d.Status != "A" {
		t.Fatalf("c.go diff = %+v %v", d, err)
	}
	if len(d.Notes) != 1 || d.Notes[0].Note.Summary != "c one" {
		t.Fatalf("c.go diff notes = %+v", d.Notes)
	}
	u, err := svc.OverviewFileDiff(ctx, id, "a.go")
	if err != nil || u.Changed {
		t.Fatalf("a.go (untouched by the commit) = %+v %v", u, err)
	}
}

func TestOverviewsInViewAllNotes(t *testing.T) {
	t.Parallel()
	dir, svc, id := viewRepo(t)
	ov, err := svc.NotesOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	head := revParse(t, dir, "HEAD")
	var found bool
	for _, c := range ov.Commits {
		if c.Hash == head && len(c.Overviews) == 1 && c.Overviews[0].ID == id && len(c.Files) == 0 && len(c.Reviews) == 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("commits = %+v", ov.Commits)
	}
	if shown := ov.ShownOn([]string{"some-other-branch"}); len(shown.Commits) != 1 {
		t.Fatalf("a commit overview shows from any branch (R5): %+v", shown.Commits)
	}
}

func TestPROverviewsOlderAfterAPush(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	ctx := context.Background()
	set, err := svc.PreviewNotes(ctx, git.PRRef(7), "main")
	if err != nil {
		t.Fatal(err)
	}
	tg, _ := svc.LinkOverviewTarget(ctx, Resolved{Preview: &set, Commit: set.Tip, Addr: model.FileAddress{State: model.StateCommitted, Commit: set.Tip}})
	if _, _, err := svc.SaveOverview(ctx, SaveOverview{Target: tg, Agent: "c", Text: ovDoc("How #7 works", "[c](big.go:5)", "")}); err != nil {
		t.Fatal(err)
	}
	hs, err := svc.PROverviews(ctx, set)
	if err != nil || len(hs) != 1 || hs[0].Older {
		t.Fatalf("PROverviews = %+v %v", hs, err)
	}
	moved := set
	moved.Tip = set.Base // the head moved somewhere else
	if hs, _ = svc.PROverviews(ctx, moved); len(hs) != 1 || !hs[0].Older {
		t.Fatalf("after a push = %+v", hs)
	}
}

func TestExportPermanentOverview(t *testing.T) {
	t.Parallel()
	_, svc, id := viewRepo(t)
	page, name, err := svc.ExportOverviewHTML(context.Background(), OverviewSource{OverviewID: id})
	if err != nil || len(page) == 0 || name == "" {
		t.Fatalf("export = %d bytes %q %v", len(page), name, err)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/domain/ -run 'OverviewDocAt|OverviewFileNotes|OverviewsInViewAllNotes|PROverviewsOlder|ExportPermanentOverview' -count=1`
Expected: FAIL — `svc.OverviewDocAt undefined` (and the others).

- [ ] **Step 3: Implement**

(a) `review_overview.go` — extract the anchor loop so both kinds share it. Replace the body of `ReviewOverview` after `files, err := s.ReviewFiles(ctx, r)` with a call to a new helper, and add the helper:

```go
// resolveOverviewDoc parses text and judges each anchor: OK when inScope
// says its path counts and its lines exist (lines < 0 = unreadable). A note
// anchor (note:t<n>) is never OK in a stored overview.
func resolveOverviewDoc(text string, inScope func(path string) bool, lines func(path string) int) OverviewDoc {
	doc, anchors := agentdocs.ParseOverview(text)
	out := OverviewDoc{Text: text, Doc: doc}
	count := map[string]int{}
	for _, a := range anchors {
		oa := OverviewAnchor{Anchor: a}
		switch {
		case a.Note != "" || a.Path == "" || !inScope(a.Path):
		case a.Start == 0:
			oa.OK = true
		default:
			n, seen := count[a.Path]
			if !seen {
				n = lines(a.Path)
				count[a.Path] = n
			}
			oa.OK = n >= 0 && max(a.End, a.Start) <= n
		}
		out.Anchors = append(out.Anchors, oa)
	}
	return out
}
```

`ReviewOverview` keeps its `inReview` map and `overviewFileLines` and ends with `return resolveOverviewDoc(r.Doc.Overview, func(p string) bool { return inReview[p] }, func(p string) int { if ls, ok := s.overviewFileLines(ctx, r, p); ok { return len(ls) }; return -1 }), nil`. Run `go test ./internal/domain/ -run ReviewOverview -count=1` — must stay green.

(b) `review_doc.go` — extract `ReviewNotesFor`'s body after the review read:

```go
func (s *Service) ReviewNotesFor(ctx context.Context, reviewID, path string, d Diff) ([]ResolvedNote, error) {
	r, err := s.Review(ctx, reviewID)
	if err != nil || r.Doc == nil {
		return nil, err
	}
	return reviewNotesOf(r, path, d), nil
}

// reviewNotesOf is review r's remarks on both sides of d (ReviewNotesFor).
func reviewNotesOf(r Review, path string, d Diff) []ResolvedNote {
	addr := model.FileAddress{State: model.StateCommitted, Commit: r.Commit, Path: path}
	if r.Kind == ReviewOnWorktree {
		if WorkingReviewState(r.Worktree, r.Files).States[reviewPath(path)] != WorkingFileMatches {
			return nil
		}
		addr = model.FileAddress{State: model.StateUnstaged, Worktree: r.Worktree, Path: path}
	}
	oldLines, newLines := diffSideLines(d)
	return reviewDocNotes(r, path, addr, oldLines, newLines, false)
}
```

(c) `review_file_diff.go` — `ReviewFileDiff(ctx, id, path)` becomes `r, err := s.Review(ctx, reviewID); if err != nil { return ReviewFileDiff{}, err }; return s.reviewFileDiffOf(ctx, r, path)`, and `reviewFileDiffOf(ctx, r Review, path string) (ReviewFileDiff, error)` holds today's body from `path = reviewPath(path)` on, with its last notes line changed to `out.Notes = reviewNotesOf(r, path, d)`. Run `go test ./internal/domain/ -run 'ReviewFileDiff|SplitDiff|Review' -count=1` — green.

(d) `overview_read.go`: add `Older bool` to `OverviewHead` with the comment `// Older: a PR overview whose PR head has moved past its commit (PROverviews sets it).`

(e) `internal/domain/overview_view.go`:

```go
package domain

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

// The overview split's reads for a permanent overview (spec 2026-10-12
// §6): the overview is presented to the review readers as a review of its
// commit's own change (a PR's: merge base..head), so remarks, diffs and the
// export are the review code's, never a copy.

// overviewReview is overview o as a review-shaped value: its commit, the
// range its D diff shows in Scope, its remarks as the document's files.
func (s *Service) overviewReview(ctx context.Context, o Overview) Review {
	r := Review{ID: o.ID, Kind: ReviewOnCommit, Commit: o.Commit, Agent: o.Agent, Summary: o.Title,
		Created: o.Created, Updated: o.Updated}
	if o.Doc != nil {
		r.Doc = &notebatch.ReviewDoc{Overview: o.Doc.Overview, Files: o.Doc.Files}
	}
	if target, _, ok := strings.Cut(o.Scope, "..."); ok && strings.TrimSpace(target) != "" {
		if base, err := s.repo.MergeBase(ctx, strings.TrimSpace(target), o.Commit); err == nil && isFullSHA(strings.TrimSpace(base)) {
			r.Scope = strings.TrimSpace(base) + ".." + o.Commit
		}
	}
	return r
}

// OverviewDocAt is overview id's document with each anchor judged against
// the WHOLE tree at its commit: any path the commit holds, lines within the
// file. One ls-tree for the anchors' paths, one read per file.
func (s *Service) OverviewDocAt(ctx context.Context, id string) (OverviewDoc, error) {
	o, err := s.Overview(ctx, id)
	if err != nil {
		return OverviewDoc{}, err
	}
	if o.Doc == nil {
		return OverviewDoc{}, ErrNoOverview
	}
	_, anchors := agentdocs.ParseOverview(o.Doc.Overview)
	var paths []string
	seen := map[string]bool{}
	for _, a := range anchors {
		if a.Path != "" && !seen[a.Path] && cleanRepoPath(a.Path) {
			seen[a.Path] = true
			paths = append(paths, a.Path)
		}
	}
	inTree := map[string]bool{}
	if found, err := s.TreePaths(ctx, o.Commit, paths); err == nil {
		for _, p := range found {
			inTree[p] = true
		}
	}
	return resolveOverviewDoc(o.Doc.Overview, func(p string) bool { return inTree[p] }, func(p string) int {
		data, err := s.ShowFile(ctx, o.Commit, p)
		if err != nil {
			return -1
		}
		ls, ok := splitTextLines(data)
		if !ok {
			return -1
		}
		return len(ls)
	}), nil
}

// OverviewFileNotes are overview id's remarks on path, placed on the file at
// its commit (whose text is lines): read-only boxes, ids "review:<id>:<n>".
func (s *Service) OverviewFileNotes(ctx context.Context, id, path string, lines []string) ([]ResolvedNote, error) {
	o, err := s.Overview(ctx, id)
	if err != nil || o.Doc == nil {
		return nil, err
	}
	r := s.overviewReview(ctx, o)
	addr := model.FileAddress{State: model.StateCommitted, Commit: o.Commit, Path: path}
	return reviewDocNotes(r, path, addr, nil, lines, true), nil
}

// OverviewFileDiff is path's change in overview id's commit (a PR
// overview's: merge base..head) with the remarks on its new side — what D
// shows in the split. Changed is false when the change leaves path alone.
func (s *Service) OverviewFileDiff(ctx context.Context, id, path string) (ReviewFileDiff, error) {
	o, err := s.Overview(ctx, id)
	if err != nil {
		return ReviewFileDiff{}, err
	}
	return s.reviewFileDiffOf(ctx, s.overviewReview(ctx, o), path)
}

// PROverviews are the overviews stored on set's pull request (matched by
// number), newest first; Older when the PR's head moved past one's commit.
func (s *Service) PROverviews(ctx context.Context, set PreviewNoteSet) ([]OverviewHead, error) {
	c, err := s.NoteCounts(ctx)
	if err != nil {
		return nil, err
	}
	sc := set.scope()
	var out []OverviewHead
	for scope, hs := range c.PreviewOverviews {
		if !SameNoteScope(scope, sc) {
			continue
		}
		for _, h := range hs {
			h.Older = set.Tip != "" && h.Commit != set.Tip
			out = append(out, h)
		}
	}
	sortHeadsNewest(out)
	return out, nil
}

func sortHeadsNewest(hs []OverviewHead) {
	for i := 1; i < len(hs); i++ {
		for j := i; j > 0 && hs[j].Created.After(hs[j-1].Created); j-- {
			hs[j], hs[j-1] = hs[j-1], hs[j]
		}
	}
}
```

`s.TreePaths` is the gated domain query (query.go:714) — use it, not `s.repo.TreePaths`.

(f) `notes_overview.go`: add to `NoteCommitNotes` after `Reviews`:

```go
	Overviews []Overview // permanent overviews stored on the commit (newest first); a PR's never
```

In `NotesOverview`, after the reviews loop (`c.Reviews = append(c.Reviews, r)` … `}`):

```go
	ovs, _ := s.Overviews(ctx) // newest first
	for _, o := range ovs {
		if o.Scope != "" {
			continue // a PR's overview lives in its PR's view only (R10)
		}
		c := commits[o.Commit]
		if c == nil {
			c = s.overviewCommit(ctx, o.Commit)
			commits[o.Commit] = c
		}
		c.Overviews = append(c.Overviews, o)
	}
```

`Count()`: add `n += len(c.Overviews)` beside `n += len(c.Reviews)`. `ShownOn`: keep `kept.Overviews = c.Overviews` (R5 — from any branch) and keep the commit when `len(kept.Files) > 0 || len(kept.Reviews) > 0 || len(kept.Overviews) > 0`.

(g) `overview_export.go`: `OverviewSource` gains `OverviewID string // a permanent overview (gg overview)`. In `ExportOverviewHTML`, before `if src.ReviewID != "" {`:

```go
	if src.OverviewID != "" {
		o, err := s.Overview(ctx, src.OverviewID)
		if err != nil {
			return nil, "", err
		}
		ov, err := s.OverviewDocAt(ctx, src.OverviewID)
		if err != nil {
			return nil, "", err
		}
		doc = ov.Doc
		for _, a := range ov.Anchors {
			anchors, ok = append(anchors, a.Anchor), append(ok, a.OK)
		}
		e.Kind, e.Title = "review", "Overview: "+o.Title
		commit := o.Commit
		rd = exportReader{
			read: func(p string) ([]byte, error) { return s.ShowFile(ctx, commit, p) },
			diff: func(p string) (ReviewFileDiff, error) { return s.OverviewFileDiff(ctx, src.OverviewID, p) },
		}
	} else if src.ReviewID != "" {
```

(the existing `if src.ReviewID != "" {` becomes `} else if …` — keep its body; the `else` agent branch stays last). If the function names its output file from the title, check that a permanent overview gets a sane name (`overview-<slug>.html`); add `TestExportPermanentOverview`'s `name` assertion accordingly.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/domain/ -count=1`
Expected: PASS (all of domain — the three extractions must keep every review test green).

- [ ] **Step 5: Commit**

```bash
gg add internal/domain/overview_view.go internal/domain/overview_view_test.go internal/domain/review_overview.go internal/domain/review_file_diff.go internal/domain/review_doc.go internal/domain/overview_read.go internal/domain/notes_overview.go internal/domain/overview_export.go
git commit -F <msgfile>   # "domain: a permanent overview's split reads — anchors over the whole tree, remarks, diff, PR overviews, View all notes, export"
```

---

### Task 2: The `≡` marker cell on commit rows

**Files:**
- Modify: `internal/tui/commit_ident.go:27-34,70-82,134` , `internal/tui/view.go:1570` , `internal/tui/branch_reviews.go` (add `commitHasOverview`)
- Test: `internal/tui/commit_overview_mark_test.go`; update `internal/tui/commit_ident_decorations_test.go`, `commit_ident_test.go` expectations; regenerate `e2e/scenarios/*.screens`

**Interfaces:**
- Consumes: `m.noteCounts.Overviews` (plan 1).
- Produces: `const markerOverview = "≡"`, `commitIdent.overview bool`, `commitMarkerW = 5`, `(m Model) commitHasOverview(hash string) bool`.

- [ ] **Step 1: Write the failing test** — `internal/tui/commit_overview_mark_test.go`:

```go
package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
)

func TestMarkerFieldOverviewCell(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		id   commitIdent
		want string
	}{
		{commitIdent{}, "     "},
		{commitIdent{reviewed: true}, "  ✎  "},
		{commitIdent{overview: true}, "   ≡ "},
		{commitIdent{tip: true, reviewed: true, overview: true}, "↓ ✎≡ "},
	} {
		got := c.id.markerField()
		if got != c.want || lipgloss.Width(got) != commitMarkerW {
			t.Errorf("markerField(%+v) = %q, want %q (width %d)", c.id, got, c.want, commitMarkerW)
		}
	}
}

func TestCommitHasOverviewFromAnyBranch(t *testing.T) {
	t.Parallel()
	m := Model{}
	m.noteCounts.Overviews = []domain.OverviewHead{{ID: "o1", Commit: "abc123", Branch: "feat"}}
	m.status.Branch = "main" // viewing another branch: still marked (R5)
	if !m.commitHasOverview("abc123") || m.commitHasOverview("def456") {
		t.Fatal("commitHasOverview")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run 'MarkerFieldOverviewCell|CommitHasOverview' -count=1`
Expected: FAIL — `unknown field overview`.

- [ ] **Step 3: Implement**

`commit_ident.go`:

```go
// commitMarkerW is the display width of the tip-marker prefix on a commit
// identity token: two glyph cells (local ↓, remote ↑), the review cell (✎),
// the overview cell (≡) and one separator space.
const commitMarkerW = 5

// markerOverview marks a commit that holds a permanent overview (spec
// 2026-10-12-permanent-overviews; user ruling: its own cell right of ✎).
const markerOverview = "≡"
```

`markerField`:

```go
func (id commitIdent) markerField() string {
	rv, ov := " ", " "
	if id.reviewed {
		rv = markerReview
	}
	if id.overview {
		ov = markerOverview
	}
	return id.tipCells() + rv + ov + " "
}
```

Update its doc comment ("[marker1][marker2-or-badge][review][overview][separator] … Always exactly commitMarkerW (5) display cells") and add the field to `commitIdent`: `overview  bool     // the commit holds a permanent overview (≡)`.

`view.go:1570`: after `id.reviewed = m.commitReviewed(c.Hash)` add `id.overview = m.commitHasOverview(c.Hash)`.

`branch_reviews.go`:

```go
// commitHasOverview reports whether commit hash holds a permanent overview —
// the Commits list's ≡. Shown from ANY branch (R5): an overview explains the
// code at its commit, the same wherever the commit is. A PR's overview is
// its PR's (never in noteCounts.Overviews).
func (m Model) commitHasOverview(hash string) bool {
	if hash == "" {
		return false
	}
	for _, o := range m.noteCounts.Overviews {
		if o.Commit == hash {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run, then fix the one-column shift everywhere**

Run: `go test ./internal/tui/ -run 'MarkerFieldOverviewCell|CommitHasOverview' -count=1` → PASS.
Run: `go test ./internal/tui/ -count=1 2>&1 | tail -40` — every failure must be a commit-row expectation off by one column (a test that hard-codes a 4-cell marker string or a column number). Update each expectation by the shift only; a failure that is anything else is a real regression — stop and debug.
Run: `go test ./e2e/ -count=1 -update` then `git diff --stat -- e2e/scenarios/` and `git diff --word-diff=porcelain -- e2e/scenarios/*.screens | grep -E '^[-+][^-+]' | sort | uniq -c | sort -rn | head -30` — the changed tokens must be the extra space after the marker and the subject's last character / `…` it pushed out. Read at least five changed screens whole. Then `go test ./e2e/ -count=1` → PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui/commit_ident.go internal/tui/view.go internal/tui/branch_reviews.go internal/tui/commit_overview_mark_test.go internal/tui/*_test.go e2e/scenarios/*.screens
git commit -F <msgfile>   # "tui: ≡ marks a commit with a permanent overview — the marker area grows to 5 cells (user ruling)"
```

(`gg add` with a glob: list the changed test files by name from `git status --short` rather than the glob if gg refuses the pattern.)

---

### Task 3: Opening a permanent overview — the `srcCommitOverview` kind

**Files:**
- Create: `internal/tui/commit_overview.go`
- Modify: `internal/tui/open_file.go:17-25,139` , `internal/tui/open_files.go:365` , `internal/tui/overview_keys.go:38,163,360,400` , `internal/tui/split_overview.go:73` , `internal/tui/split_stack_pane.go:233` , `internal/tui/split_diff.go:536-545` , `internal/tui/review_remarks.go:42-57,106-125` , `internal/tui/split_overview.go:253` , `internal/tui/overview_export.go:20`
- Test: `internal/tui/commit_overview_test.go`

**Interfaces:**
- Consumes: Task 1's `OverviewDocAt`, `OverviewFileNotes`, `OverviewFileDiff`, `OverviewSource.OverviewID`; existing `showOverview`, `layOut`, `stepAnchor`, `registerDoc`, `openFileAtCommitLine`.
- Produces:
  ```go
  srcCommitOverview fileSourceKind // a PERMANENT overview (openFile.ov, ov.tip = its commit); rev = overview id, path = "overview-<id>.md"
  func (k fileSourceKind) storedOverview() bool // srcReviewOverview or srcCommitOverview
  func (m Model) openCommitOverview(id string) (Model, tea.Cmd) // reads off-thread, then showOverview
  type commitOverviewMsg struct{ slotStamp; id string; ov domain.Overview; doc domain.OverviewDoc; err error }
  func (d *openFile) storedFrom() (fileSourceKind, string) // the stored overview d was opened from (kind, id); (0, "") otherwise — replaces reviewOfFrom
  ```

- [ ] **Step 1: Write the failing tests** — `internal/tui/commit_overview_test.go`. The setup reuses `stackRepoModel` (diff_stack_load_test.go: a.go/b.go/c.go, tip `c1` adds a func to each, 140×40, a commit's Files view open) and `drainCmds`/`pressDrain`; one more commit `c2` changes only a.go, so b.go is a file the overview's commit leaves alone:

```go
package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
)

func gitInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// commitOverviewModel: stackRepoModel plus commit c2 changing only a.go, and
// a permanent overview on c2 linking a.go:3 (remark "the func") and b.go:3.
func commitOverviewModel(t *testing.T) (Model, string) {
	t.Helper()
	m := stackRepoModel(t)
	m.svc.UseNotesDir(t.TempDir())
	dir := m.currentWorktree
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() { _ = 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInDir(t, dir, "commit", "-qam", "c2")
	ctx := context.Background()
	tg, err := m.svc.HeadOverviewTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := m.svc.SaveOverview(ctx, domain.SaveOverview{Target: tg, Agent: "Claude Code", Text: []byte(
		`{"version":1,"title":"Tour of a","overview":"Start at [the func](a.go:3), then [b](b.go:3).","files":[{"path":"a.go","annotations":[{"newRange":[3,3],"summary":"the func"}]}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	return m, got.ID
}

func openCommitOverviewT(t *testing.T, m Model, id string) Model {
	t.Helper()
	m, cmd := m.openCommitOverview(id)
	return drainCmds(t, m, cmd)
}

func TestOpenCommitOverviewShowsTheSplit(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m = openCommitOverviewT(t, m, id)
	sv := m.splitLayer()
	if sv == nil || m.topLayer() != sv {
		t.Fatalf("top = %T, want the split", m.topLayer())
	}
	ov := sv.left.doc()
	if ov == nil || ov.src.kind != srcCommitOverview || ov.src.rev != id || ov.ov.tip == "" {
		t.Fatalf("left = %+v", ov)
	}
	screen := ansi.Strip(m.View())
	for _, want := range []string{"Overview: Tour of a", "View a.go @", "func A()", "the func"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
}

func TestCommitOverviewNarrowOpensFullScreen(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m.width = 50
	m = openCommitOverviewT(t, m, id)
	if m.splitLayer() != nil || layerOf[*fileViewer](m) == nil {
		t.Fatalf("below 60 columns the overview opens full-screen; top = %T", m.topLayer())
	}
}

func TestCommitOverviewDiffUntouchedFile(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m = openCommitOverviewT(t, m, id)
	m = pressDrain(t, m, "down") // step to the b.go anchor
	m = pressDrain(t, m, "D")
	if !strings.Contains(m.statusMsg, "b.go") {
		t.Fatalf("D on a file the commit does not change: status %q", m.statusMsg)
	}
}

func TestCommitOverviewMissingCommitOpens(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	dir := m.currentWorktree
	gitInDir(t, dir, "commit", "--amend", "-qm", "rewritten")
	gitInDir(t, dir, "reflog", "expire", "--expire=now", "--all")
	gitInDir(t, dir, "gc", "-q", "--prune=now")
	m = openCommitOverviewT(t, m, id)
	if m.splitLayer() == nil && layerOf[*fileViewer](m) == nil {
		t.Fatalf("a gone commit still opens its overview (anchors plain); status %q", m.statusMsg)
	}
}

func TestStoredOverviewKinds(t *testing.T) {
	t.Parallel()
	if !srcReviewOverview.storedOverview() || !srcCommitOverview.storedOverview() || srcOverview.storedOverview() {
		t.Fatal("storedOverview")
	}
}
```

(`ansi` is `github.com/charmbracelet/x/ansi`, as all_notes_popup_test.go imports it. If the D key's untouched-file status line names the file differently, assert on the status the existing split-diff test for an unchanged file asserts — grep `Changed` in split_diff_keys_test.go.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'CommitOverview|StoredOverviewKinds' -count=1`
Expected: FAIL — `undefined: srcCommitOverview`.

- [ ] **Step 3: Implement**

`open_file.go` — add the kind after `srcReviewOverview` and the predicate:

```go
	srcCommitOverview                       // a PERMANENT overview (openFile.ov, ov.tip = its commit); rev = overview id, path = "overview-<id>.md"
```

```go
// storedOverview reports an overview kept in gg's notes — a review's stored
// overview or a permanent one: anchors open at ov.tip, no agent-store
// reference (A9), remarks and diffs read from the domain by the kind.
func (k fileSourceKind) storedOverview() bool { return k == srcReviewOverview || k == srcCommitOverview }
```

`frameTitle` (`case srcExternal, srcNote, srcOverview, srcReviewOverview, srcReviewStack:`) gains `srcCommitOverview`. `open_files.go:365` `case srcOverview, srcReviewOverview:` gains `srcCommitOverview`.

Every "a stored overview" test becomes the predicate — `overview_keys.go:38,360,400`, `split_overview.go:73`: `d.src.kind == srcReviewOverview` → `d.src.kind.storedOverview()` and `d.src.kind != srcReviewOverview` → `!d.src.kind.storedOverview()`. `overview_keys.go:163` (`openAnchor`'s stored branch): `if ov.src.kind.storedOverview() {` — its body already opens at `ov.ov.tip` and sends `plain` anchors to the status line; for a permanent overview the plain message reads `i18n.T("anchor %s does not resolve at the overview's commit", a.dest)` (new key) — branch on the kind for the message only.

`review_remarks.go` — generalise `reviewOfFrom`:

```go
// storedFrom is the stored overview d was opened from (its way back still
// open): its kind and id; (0, "") otherwise. Its remarks are d's.
func (d *openFile) storedFrom() (fileSourceKind, string) {
	if f := d.from; f != nil && f.ov != nil && !f.ov.closed && f.src.kind.storedOverview() {
		return f.src.kind, f.src.rev
	}
	return 0, ""
}

// reviewOfFrom is storedFrom's id (kept for its callers).
func (d *openFile) reviewOfFrom() string { _, id := d.storedFrom(); return id }
```

In the remark fetch (line ~106–113) replace `id := d.reviewOfFrom()` … `svc.ReviewFileNotes(...)` with:

```go
	kind, id := d.storedFrom()
	...
		var ns []domain.ResolvedNote
		var err error
		if kind == srcCommitOverview {
			ns, err = svc.OverviewFileNotes(context.Background(), id, path, lines)
		} else {
			ns, err = svc.ReviewFileNotes(context.Background(), id, path, lines)
		}
```

`split_stack_pane.go:233` — `if ov.src.kind.storedOverview() {` and inside the command the same two-way call on `ov.src.kind`.

`split_diff.go:536-545`:

```go
	agent, kind, id := !d.from.src.kind.storedOverview(), d.from.src.kind, d.from.src.rev
	return func() tea.Msg {
		ctx := context.Background()
		var res domain.ReviewFileDiff
		var err error
		switch {
		case agent:
			res, err = svc.WorkingFileDiff(ctx, path)
		case kind == srcCommitOverview:
			res, err = svc.OverviewFileDiff(ctx, id, path)
		default:
			res, err = svc.ReviewFileDiff(ctx, id, path)
		}
		return reviewDiffMsg{slotStamp: slot, tag: tag, seq: seq, agent: agent, res: res, err: err}
	}
```

`overview_export.go:20`:

```go
		src := domain.OverviewSource{ReviewID: d.src.rev}
		switch {
		case d.src.kind == srcCommitOverview:
			src = domain.OverviewSource{OverviewID: d.src.rev}
		case d.src.kind != srcReviewOverview:
			// (the existing agent-overview body)
		}
```

`internal/tui/commit_overview.go`:

```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// Permanent overviews in the TUI (spec 2026-10-12-permanent-overviews §9):
// an overview stored on a commit opens as the overview split — the document
// on the left, the file AT ITS COMMIT on the right with the overview's
// remarks — through the same door every overview uses (showOverview).

// commitOverviewMsg is openCommitOverview's read.
type commitOverviewMsg struct {
	slotStamp
	id  string
	ov  domain.Overview
	doc domain.OverviewDoc
	err error
}

// openCommitOverview reads overview id off the UI thread and shows it. An
// overview already open comes to the front instead.
func (m Model) openCommitOverview(id string) (Model, tea.Cmd) {
	if d := m.openFiles.find(m.currentWorktree, docKey(fileSource{kind: srcCommitOverview, rev: id}, "overview-"+id+".md")); d != nil {
		return m.showOverview(d)
	}
	if m.svc == nil {
		return m, nil
	}
	svc, slot := m.svc, m.stamp()
	return m, func() tea.Msg {
		ctx := context.Background()
		msg := commitOverviewMsg{slotStamp: slot, id: id}
		if msg.ov, msg.err = svc.Overview(ctx, id); msg.err != nil {
			return msg
		}
		msg.doc, msg.err = svc.OverviewDocAt(ctx, id)
		return msg
	}
}

// onCommitOverview builds the open document and shows it.
func (m Model) onCommitOverview(msg commitOverviewMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("overview: %s", msg.err.Error())
		return m, nil
	}
	src := fileSource{kind: srcCommitOverview, rev: msg.id}
	d := m.newOpenFile(src, "overview-"+msg.id+".md")
	d.title = i18n.T("Overview: %s", msg.ov.Title)
	d.p.prose, d.p.mode = true, modeScroll
	d.ov = &overview{text: msg.doc.Text, sel: -1, tip: msg.ov.Commit}
	lw, _, bh := m.splitGeom()
	rows, inner := paneGeom(lw, bh)
	if w, _ := m.overlayDims(); w < splitMinWidth {
		rows, inner = m.viewerGeom()
		inner = m.overviewWidth(inner)
	}
	d.layOut(rows, inner)
	if len(d.ov.anchors) == len(msg.doc.Anchors) { // one parser, one order
		for i := range d.ov.anchors {
			d.ov.anchors[i].plain = !msg.doc.Anchors[i].OK
		}
		d.ov.paint(d.p.lines)
	}
	d.stepAnchor(1, rows) // the first resolved anchor is current on open
	return m.showOverview(d)
}
```

Route the message in `model.go`'s `Update` switch next to `reviewViewMsg` (`case commitOverviewMsg: if !m.slotCurrent(msg.slotStamp) { return m, nil }; return m.onCommitOverview(msg)` — use the stale-slot check the neighbouring async messages use; grep `slotStamp` handling for `splitStackNotesMsg`).

New i18n keys (all four bundles, `internal/i18n/lang/{ja,ko,zh,ru}.toml`, alphabetical position as the files keep it):

```toml
"anchor %s does not resolve at the overview's commit" = …  # ja: "アンカー %s は概要のコミットで解決できません" ko: "앵커 %s 은(는) 개요의 커밋에서 해석되지 않습니다" zh: "锚点 %s 在概览的提交中无法解析" ru: "якорь %s не находится в коммите обзора"
"overview: %s" = …                                          # ja: "概要: %s" ko: "개요: %s" zh: "概览：%s" ru: "обзор: %s"
```

(Check whether `"Overview: %s"` already exists — it does, from the review overview's title; reuse it.)

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'CommitOverview|StoredOverviewKinds|SplitOverview|SplitDiff|SplitStack|OverviewExport|Remark' -count=1` → PASS, then `go test ./internal/tui/ -count=1` → PASS (the i18n gates included).

- [ ] **Step 5: Commit**

```bash
gg add internal/tui/commit_overview.go internal/tui/commit_overview_test.go internal/tui/open_file.go internal/tui/open_files.go internal/tui/overview_keys.go internal/tui/split_overview.go internal/tui/split_stack_pane.go internal/tui/split_diff.go internal/tui/review_remarks.go internal/tui/overview_export.go internal/tui/model.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
git commit -F <msgfile>   # "tui: a permanent overview opens as the overview split on its commit's files (srcCommitOverview)"
```

---

### Task 4: Commit `.` menu entry and Branches sub-rows

**Files:**
- Modify: `internal/tui/action_menu.go` (`appendCommitContextRows`; the branch rows near `showBranchReviewRow`), `internal/tui/worktree_sessions.go:64-97` (`brEntry.overview`), `internal/tui/view.go:1128-1150` (`branchRowsFor`), `internal/tui/model.go:2922-2926` (enter), `internal/tui/review_delete.go` (overview delete + landing), `internal/tui/help.go`
- Create: `internal/tui/commit_overview_rows.go`
- Test: `internal/tui/commit_overview_rows_test.go`

**Interfaces:**
- Consumes: Task 3's `openCommitOverview`; `m.noteCounts.Overviews`; `sameCommit`, `reviewStampAt`, `confirmStoredDelete`/`storedDeletedMsg`.
- Produces: `brEntry.overview string`; `(m Model) commitOverviewRows() []actionRow` ("Open overview: <title>" per overview of the selected commit); `(m Model) branchOverviewHeads(b model.Branch) []domain.OverviewHead`; `branchOverviewRowBody(h domain.OverviewHead) string`; `(m Model) selectedBranchOverview() (domain.OverviewHead, bool)`; `(m Model) branchOverviewRows() []actionRow` (Open overview + Delete overview on the sub-row); `storedDeletedMsg.overview bool`.

- [ ] **Step 1: Write the failing tests** — `internal/tui/commit_overview_rows_test.go`:

```go
package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func ovHead(id, commit, branch, title string) domain.OverviewHead {
	return domain.OverviewHead{ID: id, Commit: commit, Branch: branch, Agent: "Claude Code", Title: title,
		Created: time.Date(2026, 10, 12, 9, 40, 0, 0, time.Local)}
}

func TestBranchOverviewRowsOnlyAtTheTip(t *testing.T) {
	t.Parallel()
	m := Model{}
	m.branches = []model.Branch{{Name: "feat", Hash: "abc1234"}, {Name: "main", Hash: "fff0000"}}
	m.noteCounts.Overviews = []domain.OverviewHead{
		ovHead("o1", "abc1234deadbeef", "feat", "How the gate works"),
		ovHead("o2", "0001111aaaaaaaa", "feat", "An older one"), // feat moved on: no row
	}
	ents := m.branchEntries()
	var subs []string
	for _, e := range ents {
		if e.overview != "" {
			subs = append(subs, e.overview)
		}
	}
	if len(subs) != 1 || subs[0] != "o1" {
		t.Fatalf("overview sub-rows = %v", subs)
	}
	rows := m.branchRowsFor(ents)
	if !strings.Contains(strings.Join(rows, "\n"), "└ Overview: How the gate works") {
		t.Fatalf("rows:\n%s", strings.Join(rows, "\n"))
	}
}

func TestTwoOverviewsTwoRows(t *testing.T) {
	t.Parallel()
	m := stackRepoModel(t)
	m.filesView, m.focus = nil, panelCommits // back on the Commits list (the helper opened a Files view)
	m.sel[panelCommits] = 0
	h := m.commits[0].Hash
	m.noteCounts.Overviews = []domain.OverviewHead{ovHead("o1", h, "", "Locks"), ovHead("o2", h, "", "Parsing")}
	var labels []string
	for _, r := range m.commitOverviewRows() {
		labels = append(labels, r.label)
	}
	if strings.Join(labels, "|") != "Open overview: Locks|Open overview: Parsing" {
		t.Fatalf("menu rows = %v", labels)
	}
}

func TestOverviewRowsFollowReload(t *testing.T) {
	t.Parallel()
	m := stackRepoModel(t)
	m.filesView, m.focus = nil, panelCommits
	m.sel[panelCommits] = 0
	m.noteCounts.Overviews = []domain.OverviewHead{ovHead("o1", m.commits[0].Hash, "", "Locks")}
	if len(m.commitOverviewRows()) != 1 {
		t.Fatal("one row")
	}
	m.noteCounts.Overviews = nil // the CLI removed it; the notes reload landed
	if len(m.commitOverviewRows()) != 0 {
		t.Fatal("rows follow the counts")
	}
}

func TestGoneOverviewCloses(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t) // Task 3's helper
	m.noteCounts.Overviews = []domain.OverviewHead{{ID: id}}
	m = openCommitOverviewT(t, m, id)
	if m.splitLayer() == nil {
		t.Fatal("setup: the split is open")
	}
	m = m.dropGoneOverviews(domain.NoteCounts{}) // the reload landed without it
	if m.splitLayer() != nil || !strings.Contains(m.statusMsg, "deleted") {
		t.Fatalf("a deleted overview closes; top %T status %q", m.topLayer(), m.statusMsg)
	}
}
```

(If clearing `m.filesView` is not how the Files view is left — `closeFilesView` — use that; the assertion is only about `commitOverviewRows`.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'BranchOverviewRows|TwoOverviewsTwoRows|OverviewRowsFollowReload|GoneOverviewCloses' -count=1`
Expected: FAIL — `e.overview undefined`, `m.commitOverviewRows undefined`.

- [ ] **Step 3: Implement**

`worktree_sessions.go`: `brEntry` gains `overview string // a permanent overview of the branch's current tip: its id`; `sub()` returns `e.sess != "" || e.review != "" || e.overview != ""`; in `branchEntries` after the reviews loop:

```go
		// The permanent overviews of the branch's current tip (commit_overview_rows.go).
		for _, o := range m.branchOverviewHeads(b) {
			out = append(out, brEntry{br: i, overview: o.ID})
		}
```

`internal/tui/commit_overview_rows.go`:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// branchOverviewHeads are b's permanent overviews of its CURRENT tip, newest
// first: made on b while its tip was that commit (spec R5).
func (m Model) branchOverviewHeads(b model.Branch) []domain.OverviewHead {
	var out []domain.OverviewHead
	for _, o := range m.noteCounts.Overviews {
		if o.Branch == b.Name && sameCommit(o.Commit, b.Hash) {
			out = append(out, o)
		}
	}
	return out
}

// branchOverviewRowBody is an overview sub-row without its indent:
// "└ Overview: <title> 10-12 09:40 Claude Code".
func branchOverviewRowBody(h domain.OverviewHead) string {
	parts := []string{i18n.T("Overview:"), sanitizeLine(h.Title)}
	if !h.Created.IsZero() {
		parts = append(parts, reviewStampAt(h.Created, clock.Now()))
	}
	if a := strings.TrimSpace(h.Agent); a != "" {
		parts = append(parts, sanitizeLine(a))
	}
	return "└ " + strings.Join(parts, " ")
}

func (m Model) overviewHead(id string) (domain.OverviewHead, bool) {
	for _, o := range m.noteCounts.Overviews {
		if o.ID == id {
			return o, true
		}
	}
	return domain.OverviewHead{}, false
}

// selectedBranchOverview resolves an overview sub-row under the Branches cursor.
func (m Model) selectedBranchOverview() (domain.OverviewHead, bool) {
	e, ok := m.selectedBranchEntry()
	if !ok || e.overview == "" {
		return domain.OverviewHead{}, false
	}
	return m.overviewHead(e.overview)
}

// commitOverviewRows are the Commits .-menu's "Open overview: <title>", one
// per permanent overview of the selected commit, newest first.
func (m Model) commitOverviewRows() []actionRow {
	if m.focus != panelCommits {
		return nil
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok || bi >= len(m.commits) {
		return nil
	}
	var out []actionRow
	for _, o := range m.noteCounts.Overviews {
		if o.Commit != m.commits[bi].Hash {
			continue
		}
		id := o.ID
		out = append(out, actionRow{id: "open-overview-" + id, label: i18n.T("Open overview: %s", sanitizeLine(o.Title)),
			run: func(m Model) (tea.Model, tea.Cmd) { return m.openCommitOverview(id) }})
	}
	return out
}

// branchOverviewRows are an overview sub-row's .-menu: Open and Delete.
func (m Model) branchOverviewRows() []actionRow {
	if m.focus != panelBranches || m.inContentWindow() {
		return nil
	}
	h, ok := m.selectedBranchOverview()
	if !ok {
		return nil
	}
	id, quote := h.ID, reviewQuote(h.Agent, h.Title)
	return []actionRow{
		{id: "open-overview", label: i18n.T("Open overview"), run: func(m Model) (tea.Model, tea.Cmd) { return m.openCommitOverview(id) }},
		{id: "delete-overview", label: i18n.T("Delete overview"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmOverviewDelete(id, quote)
		}},
	}
}
```

Check `backingIndex(panelCommits)`'s meaning (it returns an index into `m.commits` in `commitBookmarkRow` — same use). If a WIP row offsets it, mirror `commitBookmarkRow` exactly.

`action_menu.go`: in `appendCommitContextRows`, after `focusedCommitReviewRow`: `out = append(out, m.commitOverviewRows()...)`. Near the branch review rows (after `branchReviewCopyLinkRow`): `out = append(out, m.branchOverviewRows()...)`.

`view.go` `branchRowsFor` — before `if e.review != "" {`:

```go
		if e.overview != "" {
			if h, ok := m.overviewHead(e.overview); ok {
				out = append(out, indent+branchOverviewRowBody(h))
			} else {
				out = append(out, indent+"└ ?")
			}
			continue
		}
```

`model.go:2922` enter on Branches — before the review sub-row check:

```go
				if h, ok := m.selectedBranchOverview(); ok {
					return m.openCommitOverview(h.ID)
				}
```

`review_delete.go` — add `overview bool` to `storedDeletedMsg`, a sibling of `confirmStoredDelete`:

```go
// confirmOverviewDelete asks before a permanent overview goes (it is the
// agent's stored work; gg overview rm is the CLI twin).
func (m Model) confirmOverviewDelete(id, quote string) (tea.Model, tea.Cmd) {
	m.modal = &decisionState{
		req: engine.DecisionRequest{ID: "overview-remove", Prompt: i18n.T("Delete this overview?") + "\n" + quote, Options: []string{"Delete", "Cancel"}},
		sel: 1,
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt != "Delete" || m.svc == nil {
				return m, nil
			}
			svc := m.svc
			return m, func() tea.Msg {
				return storedDeletedMsg{id: id, overview: true, err: svc.RemoveOverview(context.Background(), id)}
			}
		},
	}
	return m, nil
}
```

and in `onStoredDeleted` after the review status line: `if msg.overview { m.statusMsg = i18n.T("deleted the overview") }`.

A gone overview closes (spec §9) — `commit_overview_rows.go`:

```go
// dropGoneOverviews closes every open permanent overview the reloaded
// counts c no longer hold (deleted here, by the CLI or another window).
func (m Model) dropGoneOverviews(c domain.NoteCounts) Model {
	have := map[string]bool{}
	for _, o := range c.Overviews {
		have[o.ID] = true
	}
	for _, hs := range c.PreviewOverviews {
		for _, o := range hs {
			have[o.ID] = true
		}
	}
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if d.src.kind != srcCommitOverview || have[d.src.rev] {
			continue
		}
		if sv := m.splitShowing(d); sv != nil && sv.left.doc() == d {
			m = m.closeSplit(sv)
		}
		m = m.closeDoc(d)
		m.statusMsg = i18n.T("the overview was deleted")
	}
	return m
}
```

Call it where the counts land (`model.go:2159`, `case srcNotes:`): right after `m.noteCounts = msg.value.(domain.NoteCounts)` add `m = m.dropGoneOverviews(m.noteCounts)`. New key `"the overview was deleted"` (ja: 概要は削除されました ko: 개요가 삭제되었습니다 zh: 概览已被删除 ru: обзор удалён). If the decision-id vocabulary gate (`options_vocab_test.go`) lists known decision ids, add `overview-remove` there with the same options as `review-remove`.

`help.go`: after the "Review (AI) …" row add
`r(".", i18n.T("Open overview: <title> (.-menu on a commit; enter on a Branches └ Overview row): an agent's permanent overview of the code at that commit — the overview on the left, the file at the commit on the right; ≡ marks such a commit")),`.

New i18n keys (four bundles): `"Overview:"`, `"Open overview: %s"`, `"Open overview"`, `"Delete overview"`, `"Delete this overview?"`, `"deleted the overview"`, and the help sentence. Translations — ja: 概要: / 概要を開く: %s / 概要を開く / 概要を削除 / この概要を削除しますか? / 概要を削除しました; ko: 개요: / 개요 열기: %s / 개요 열기 / 개요 삭제 / 이 개요를 삭제할까요? / 개요를 삭제했습니다; zh: 概览： / 打开概览：%s / 打开概览 / 删除概览 / 删除此概览？ / 已删除概览; ru: Обзор: / Открыть обзор: %s / Открыть обзор / Удалить обзор / Удалить этот обзор? / обзор удалён. The help sentence: translate it in each bundle in the style of its neighbouring "Review (AI)" sentence. The verb-agreement gate (`verbs.go`) may require the menu labels' verbs be listed — run the gates and follow their message.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'BranchOverviewRows|TwoOverviewsTwoRows|OverviewRowsFollowReload|GoneOverviewCloses' -count=1` → PASS; then `go test ./internal/tui/ -count=1` → PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui/commit_overview_rows.go internal/tui/commit_overview_rows_test.go internal/tui/action_menu.go internal/tui/worktree_sessions.go internal/tui/view.go internal/tui/model.go internal/tui/review_delete.go internal/tui/help.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
git commit -F <msgfile>   # "tui: Open overview in a commit's . menu; └ Overview rows under a branch at that tip; delete asks"
```

---

### Task 5: View all notes — an Overviews block per commit

**Files:**
- Modify: `internal/tui/all_notes_popup.go` (row kind, rows, enter / ctrl+d / ctrl+l, cells)
- Test: `internal/tui/all_notes_overview_test.go`

**Interfaces:**
- Consumes: Task 1's `NoteCommitNotes.Overviews`; Task 3's `openCommitOverview`; Task 4's `confirmOverviewDelete`; `domain.(*Service).OverviewLink` (plan 1).
- Produces: `anOverview anRowKind`, `anRow.overview *domain.Overview`.

- [ ] **Step 1: Write the failing test**

```go
package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestAllNotesListsOverviews(t *testing.T) {
	t.Parallel()
	ov := domain.NotesOverview{Commits: []domain.NoteCommitNotes{{Hash: "abc1234deadbeef", Subject: "gate",
		Overviews: []domain.Overview{{ID: "o1", Commit: "abc1234deadbeef", Agent: "Claude Code", Title: "How the gate works"}}}}}
	m := footerModel()
	m.currentWorktree = "/repo"
	m, _ = m.openAllNotes()
	u, _ := m.Update(allNotesMsg{ov: ov, gen: m.allNotesGen})
	m = u.(Model)
	p := layerOf[*allNotesPopup](m)
	var found bool
	for _, r := range p.rows {
		if r.kind == anOverview && r.overview != nil && r.overview.ID == "o1" {
			found = true
		}
	}
	if !found {
		t.Fatal("no overview row")
	}
	if s := strings.Join(allNotesScreen(m), "\n"); !strings.Contains(s, "Overviews") || !strings.Contains(s, "How the gate works") {
		t.Fatalf("screen:\n%s", s)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run AllNotesListsOverviews -count=1`
Expected: FAIL — `undefined: anOverview`.

- [ ] **Step 3: Implement**

`anRowKind`: add `anOverview // one permanent overview stored on a commit`. `anRow`: add `overview *domain.Overview // anOverview rows`. In the commits loop, after the Reviews block:

```go
			if len(c.Overviews) > 0 {
				rows = append(rows, anRow{kind: anDir, depth: 2, text: i18n.T("Overviews")})
				for i := range c.Overviews {
					o := &c.Overviews[i]
					rows = append(rows, anRow{kind: anOverview, depth: 3, overview: o,
						filter: strings.ToLower(o.Title + "\x00" + o.Agent + "\x00" + o.Branch)})
				}
			}
```

Rendering: every `switch r.kind` that handles `anReview` gets an `anOverview` arm laid out like a review row — status cell `i18n.T("overview")`, who = agent, where = `commit` / `branch %s` / `was tip %s` by `o.Kind` (reuse anReviewCells' wording keys), when = age, summary = title, no tally. Implement `anOverviewParts`/`anOverviewFull` mirroring `anReviewParts`/`anReviewFull` (copy them, replacing `r.review` with `r.overview`, `Summary` with `Title`, and dropping the tally).

Keys: enter `case anOverview: return m.openCommitOverview(r.overview.ID)`; ctrl+d → `m.confirmOverviewDelete(r.overview.ID, reviewQuote(r.overview.Agent, r.overview.Title))` (in `allNotesDelete`); ctrl+l → `asyncCopyLinkRow("copy-gg-link", i18n.T("Copy gg link"), func(ctx) { return svc.OverviewLink(ctx, id) })`. `onStoredDeleted` already re-reads an open popup.

New i18n keys: `"Overviews"`, `"overview"` (ja 概要一覧/概要, ko 개요 목록/개요, zh 概览列表/概览, ru Обзоры/обзор).

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'AllNotes' -count=1` → PASS; `go test ./internal/tui/ -count=1` → PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui/all_notes_popup.go internal/tui/all_notes_overview_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
git commit -F <msgfile>   # "tui: View all notes lists a commit's permanent overviews (enter opens, ctrl+d deletes, ctrl+l copies the link)"
```

---

### Task 6: The PR view's Overviews block

**Files:**
- Modify: `internal/tui/pr_actions.go:97` (read), `internal/tui/preview_open.go` (msg field + `setPreviewOverviews` call where `setPreviewReviews` is), `internal/tui/preview_reviews.go` (`previewOverviewLines`), `internal/tui/content_popup.go:65` (`contentLine.overviewID`), `internal/tui/files_view.go:1288` (enter), `internal/tui/note_row_menu.go:23-75` (row menu), `internal/tui/files_view.go:70` (reset)
- Test: `internal/tui/pr_overview_rows_test.go`

**Interfaces:**
- Consumes: Task 1's `PROverviews`; Task 3's `openCommitOverview`; Task 4's `confirmOverviewDelete`.
- Produces: `contentLine.overviewID string`; `previewOpenMsg.overviews []domain.OverviewHead`; `m.filesPreviewOverviews []domain.OverviewHead`; `previewOverviewLines(heads []domain.OverviewHead, lines []contentLine) []contentLine`.

- [ ] **Step 1: Write the failing test**

```go
package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestPreviewOverviewLines(t *testing.T) {
	t.Parallel()
	files := []contentLine{{text: "a.go", path: "a.go"}}
	heads := []domain.OverviewHead{{ID: "o1", Title: "How #7 works", Agent: "Claude Code", Created: time.Now()},
		{ID: "o2", Title: "Old one", Agent: "Claude Code", Older: true, Created: time.Now()}}
	got := previewOverviewLines(heads, files)
	if len(got) != 4 || !got[0].heading || got[0].text != "Overviews" || got[1].overviewID != "o1" || got[3].path != "a.go" {
		t.Fatalf("lines = %+v", got)
	}
	if !strings.Contains(got[2].text, "older tip") {
		t.Fatalf("an older overview says so: %q", got[2].text)
	}
	// idempotent: re-laying replaces the block, never stacks a second one
	if again := previewOverviewLines(heads, got); len(again) != 4 {
		t.Fatalf("re-laid = %d lines", len(again))
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run PreviewOverviewLines -count=1`
Expected: FAIL — `undefined: previewOverviewLines`.

- [ ] **Step 3: Implement**

`content_popup.go`: `contentLine` gains `overviewID string // a PR view's Overviews entry: the permanent overview it opens; "" = none`.

`preview_reviews.go`:

```go
// previewOverviewLines puts the PR's permanent overviews (R10) on top of
// lines as an "Overviews" block, replacing a block already there; the
// Reviews block (previewReviewLines) is laid over the result.
func previewOverviewLines(heads []domain.OverviewHead, lines []contentLine) []contentLine {
	rest := lines
	if len(rest) > 1 && rest[0].heading && rest[1].overviewID != "" {
		i := 1
		for i < len(rest) && rest[i].overviewID != "" {
			i++
		}
		rest = rest[i:]
	}
	if len(heads) == 0 {
		return rest
	}
	out := make([]contentLine, 0, len(heads)+1+len(rest))
	out = append(out, contentLine{text: i18n.T("Overviews"), heading: true})
	for _, h := range heads {
		body := branchOverviewRowBody(h)
		if h.Older {
			body += " · " + i18n.T("older tip")
		}
		out = append(out, contentLine{text: "  " + body, overviewID: h.ID})
	}
	return append(out, rest...)
}
```

Order on screen: Reviews block first, then Overviews, then files. Because `previewReviewLines` strips only a leading Reviews block, lay out as `previewReviewLines(reviews, previewOverviewLines(overviews, files))` everywhere the list is (re)laid — `model.go:1213-1214` and `setPreviewReviews` — and make `previewReviewLines`'s strip skip a following Overviews block correctly (it stops at the first non-noteID line: the Overviews heading — correct). Add `m.filesPreviewOverviews` (reset beside `m.filesPreviewReviews = nil` in `files_view.go:70`), fill it from `previewOpenMsg.overviews` where `msg.heads` is stored (`preview_open.go:231`), and read it in `openPRPreviewCmd` (`pr_actions.go:97`): `msg.overviews, _ = svc.PROverviews(ctx, r.Set)`.

`files_view.go:1288` — before the `l.noteID` arm: `if l.overviewID != "" { return m.openCommitOverview(l.overviewID) }`. Also `diff_stack_keys.go:29`'s "not a file" test gains `|| l.overviewID != ""`.

`note_row_menu.go` `filesNoteRow`: accept `l.overviewID != ""`; `noteRowMenu` gains:

```go
	case l.overviewID != "":
		id, quote := l.overviewID, reviewQuote("", "")
		for _, h := range m.filesPreviewOverviews {
			if h.ID == id {
				quote = reviewQuote(h.Agent, h.Title)
			}
		}
		return []actionRow{
			{id: "open-overview", label: i18n.T("Open overview"), run: func(m Model) (tea.Model, tea.Cmd) { return m.openCommitOverview(id) }},
			{id: "delete-overview", label: i18n.T("Delete overview"), run: func(m Model) (tea.Model, tea.Cmd) { return m.confirmOverviewDelete(id, quote) }},
		}, true
```

After a delete (`onStoredDeleted` with `msg.overview`), drop the row: `m = m.dropFilesRows(func(l contentLine) bool { return l.overviewID == msg.id })`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'PreviewOverviewLines|PreviewReview|PR' -count=1` → PASS; `go test ./internal/tui/ -count=1` → PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui/preview_reviews.go internal/tui/pr_overview_rows_test.go internal/tui/content_popup.go internal/tui/files_view.go internal/tui/note_row_menu.go internal/tui/pr_actions.go internal/tui/preview_open.go internal/tui/model.go internal/tui/diff_stack_keys.go
git commit -F <msgfile>   # "tui: a PR view lists its permanent overviews (R10) — enter opens, . menu Open / Delete"
```

---

### Task 7: `?overview=` links open the overview

**Files:**
- Modify: `internal/tui/steer_nav.go:186-200` (dispatch), `internal/tui/commit_overview.go` (the hint handler)
- Test: `internal/tui/overview_hint_test.go`

**Interfaces:**
- Consumes: `steer.Command{HintKind, HintID}` (linknav already copies the hint, `linknav.go:117`); Task 3's `openCommitOverview`; `m.answerSteer`, `steerFail`, `steerToPanels` (review_hint.go).
- Produces: `(m Model) steerNavigateOverview(c steer.Command) (Model, tea.Cmd)`.

- [ ] **Step 1: Write the failing test**

```go
package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

func TestSteerNavigateOpensAnOverview(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t) // Task 3's helper
	c := steer.Command{Cmd: "navigate", HintKind: model.OverviewHintKind, HintID: id}
	m.noteCounts.Overviews = []domain.OverviewHead{{ID: id}} // the counts a real reload would hold
	m, cmd := m.steerNavigate(c)
	m = drainCmds(t, m, cmd)
	if sv := m.splitLayer(); sv == nil || sv.left.doc() == nil || sv.left.doc().src.rev != id {
		t.Fatal("a ?overview= link opens that overview")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run SteerNavigateOpensAnOverview -count=1`
Expected: FAIL — no split (the hint is ignored today).

- [ ] **Step 3: Implement**

`steer_nav.go` `steerNavigate` switch, after the review case:

```go
	case c.HintKind == model.OverviewHintKind:
		// An overview link opens the overview itself (commit_overview.go).
		return m.steerNavigateOverview(c)
```

`commit_overview.go`:

```go
// steerNavigateOverview opens the permanent overview a ?overview= link
// names (gg session navigate, gg open, the # prompt). A gone overview opens
// the commit instead and says so.
func (m Model) steerNavigateOverview(c steer.Command) (Model, tea.Cmd) {
	if m.svc == nil {
		return m, m.answerSteer(c, steerFail(c, "no repository is open"))
	}
	if _, ok := m.overviewHead(c.HintID); !ok && !m.overviewInPRView(c.HintID) {
		plain := c
		plain.HintKind, plain.HintID = "", ""
		nm, cmd := m.steerNavigate(plain)
		nm.statusMsg = i18n.T("That overview no longer exists; opened its commit")
		return nm, cmd
	}
	nm := m.steerToPanels()
	nm, cmd := nm.openCommitOverview(c.HintID)
	return nm, tea.Batch(cmd, nm.answerSteer(c, steerOK(c)))
}

// overviewInPRView: id is among the open PR view's overviews (a PR overview
// is never in noteCounts.Overviews).
func (m Model) overviewInPRView(id string) bool {
	for _, h := range m.filesPreviewOverviews {
		if h.ID == id {
			return true
		}
	}
	return false
}
```

Check the real names of the success answer (`steerOK` or similar) in `review_hint.go`'s success path and use that; a PR overview link while its PR view is not open: `noteCounts.PreviewOverviews` holds it — extend `overviewInPRView` to scan `m.noteCounts.PreviewOverviews` too (every scope), so the link opens it from anywhere.

New i18n key: `"That overview no longer exists; opened its commit"` (ja: その概要はもうありません。コミットを開きました ko: 해당 개요가 더 이상 없습니다. 커밋을 열었습니다 zh: 该概览已不存在；已打开其提交 ru: этого обзора больше нет; открыт его коммит).

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'SteerNavigate|ReviewHint|Overview' -count=1` → PASS; `go test ./internal/tui/ -count=1` → PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui/steer_nav.go internal/tui/commit_overview.go internal/tui/overview_hint_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
git commit -F <msgfile>   # "tui: ?overview= links (gg session navigate, gg open, #) open the overview"
```

---

### Task 8: Skill switch, e2e scenario, docs, gate

**Files:**
- Modify: `internal/agentskill/gg-overview.md`, `internal/agentskill/using-gg.md` (one sentence), `internal/agentskill/agentskill.go` (`GGOverviewVersion` 6→7, `Version` +1), `.claude/skills/*/SKILL.md` (regenerated), `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, `docs/superpowers/specs/2026-10-12-permanent-overviews-design.md` (§9 amended: the marker cell ruling)
- Create: `e2e/scenarios/tui_permanent_overview.toml`

- [ ] **Step 1: e2e scenario** (load `writing-e2e-scenarios` first; shape follows `tui_stored_overview.toml`)

```toml
name = "tui: a permanent overview — ≡ on the commit, the . menu opens it as a split on the commit's files, the Branches row too"

# One commit adding a.txt (5 lines) and b.txt (3 lines); an agent stores a
# permanent overview on HEAD (main) linking a.txt:2 with a remark. The
# Commits row shows ≡; the . menu offers "Open overview: How a and b work";
# enter opens the split: the overview left, a.txt at the commit right with
# the remark; ↓ steers to b.txt; esc closes; the Branches tab lists the
# └ Overview row under main.
[input]
steps = [
  { write = "a.txt", content = "one\ntwo\nthree\nfour\nfive\n" },
  { write = "b.txt", content = "alpha\nbeta\ngamma\n" },
  { commit = "add a and b" },
]

[[run]]
cmd   = ["overview", "save", "--agent", "Claude Code", "--stdin"]
stdin = '{"version":1,"title":"How a and b work","overview":"Start at [two](a.txt:2), then [beta](b.txt:2).","files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"why two?"}]}]}'
exit  = 0

[tui]
size = "160x40"

[[tui.step]]
name = "commits"
keys = ["right"]
wait = true
screen_contains = ["≡", "add a and b"]

[[tui.step]]
name = "menu"
keys = ["."]
screen_contains = ["Open overview: How a and b work"]

[[tui.step]]
name = "split"
keys = ["enter"]
wait = true
screen_contains = ["Overview: How a and b work", "View a.txt @", "two", "why two?"]

[[tui.step]]
name = "steer"
keys = ["down"]
wait = true
screen_contains = ["View b.txt @", "beta"]

[[tui.step]]
name = "close"
keys = ["esc"]
screen_excludes = ["Overview: How a and b work"]

[expect]
branch = "main"
```

(The `.` menu step may need the cursor moved onto the menu row — the menu opens with its first row selected; if "Open overview" is not first, add the `down` presses the screen shows are needed, and say so in the scenario's comment. The Branches row is pinned by `TestBranchOverviewRowsOnlyAtTheTip`.)

Run: `go test ./e2e/ -run 'TestScenarios/tui_permanent_overview' -count=1 -update`, read the written `.screens`, then without `-update` → PASS.

- [ ] **Step 2: Skill switch (R1)** — rewrite `gg-overview.md`'s opening and Steps so the PERMANENT overview is the default:

- Opening paragraph: "An overview is PERMANENT by default: stored on the commit it explains, opened again any time from the commit (≡ in gg's Commits list, its `.` menu, the Branches row, View all notes). Make it TEMPORARY (in gg's memory only, gone when gg closes) only when the user says "in-memory", "temporary" or "don't keep it"."
- New steps for the permanent default: 1 finish first; 2 work in the worktree whose commit you explain (lines are the file AT that commit — `gg show <sha>:<path>` when the tree has uncommitted changes); 3 write the JSON document (`{"version":1,"title":…,"overview":…,"files":[{"path":…,"annotations":[{"newRange":[a,b],"summary":…,"rationale":…}]}]}` — your line remarks go in `annotations`, links are `path:N-M`, never `note:`); 4 `gg overview save [<gg-link>] --agent "<your name>" --stdin` (a pull request: the link from `gg link --pr <n>`); fix and re-save on a path/line problem; on `uncommitted changes` / `differs … on disk` STOP and ask the user: commit those files and save permanently, or keep it in memory only (then the temporary recipe); 5 `gg session navigate <the printed link>` when a gg window is open (`no gg session for this worktree` → skip; NEVER start gg web for a permanent overview); 6 reply with the link, the title and a three-line summary.
- Keep today's notes + `gg session overview add` steps under a heading "Temporary (in-memory) overview — only when asked", unchanged.
- The closing paragraph "Notes and overviews are TEMPORARY … If the user wants the result kept, store a review instead" is replaced by the permanent default above.

Bump `GGOverviewVersion` to 7 in `agentskill.go`. In `using-gg.md` add after the `gg overview rm` bullet: "An agent presenting an overview stores it this way by default (the gg-overview skill); a temporary `gg session overview` only when the user asks for one in memory." and bump `Version`. Regenerate the dogfood copies through the embedded renderer (a throwaway `GG_REGEN` test as in plan 1 — never `gg init --update` on the branch); `go test ./internal/agentskill/ -count=1` → PASS.

- [ ] **Step 3: Docs** — CHANGELOG entry "Permanent overviews in the TUI" (≡ cell — the marker area is 5 cells now, every commit row moved one column; `.` menu Open overview; Branches └ Overview rows; View all notes Overviews; PR view Overviews block; `?overview=` links open it; gg-overview skill v7: permanent by default). README: the TUI section's commit markers line gains ≡. `docs/CLAUDE-details.md`: append to "Permanent overviews (2026-10-12)" — `srcCommitOverview` + `storedOverview()`, `storedFrom()`, the three kind-switched calls (remarks/diff/export), `overviewReview` adapter, `commitMarkerW = 5`, rows and where each is built. Spec §9: replace "`≡` beside the commit (and beside `✎` when both)" with "`≡` in its own marker cell right of `✎` (user ruling 2026-10-12: the marker area is 5 cells)", and "`y` copies the overview link" with "`y` copies its text, as on every overview; the link is ctrl+l in View all notes and `gg overview show`".

- [ ] **Step 4: Gate**

Run: `env -u GG_INBOX -u GG_MCP_URL -u GG_SESSION_TOKEN -u GG_SESSION_ID ./test.sh race > <ledger dir>/race.log 2>&1` and read the log for `all green` (memory: `| tail` exits 0 on FAIL). Then drive the real binary: `go build -o bin/gg ./cmd/gg` and `./tui-capture.sh --gg <abs>/bin/gg --repo <a demo repo with a saved overview> --state <scratch state> "commits: right ; menu: . ; open: enter"` — read the snapshots: the ≡ cell, the menu row, the split.

- [ ] **Step 5: Commit**

```bash
gg add e2e/scenarios/tui_permanent_overview.toml e2e/scenarios/tui_permanent_overview.screens internal/agentskill/gg-overview.md internal/agentskill/using-gg.md internal/agentskill/agentskill.go .claude/skills/using-gg/SKILL.md .claude/skills/gg-overview/SKILL.md CHANGELOG.md README.md docs/CLAUDE-details.md docs/superpowers/specs/2026-10-12-permanent-overviews-design.md
git commit -F <msgfile>   # "skills, docs, e2e: permanent overviews are the default (gg-overview v7); the TUI scenario"
```

(If `.claude/skills/gg-overview/SKILL.md` is not a dogfood copy in this repo, `git status` shows what the regeneration touched — commit exactly those.)

Then the final whole-branch review (read-only subagent, most capable model), one fix pass, and ask the user for the merge OK. After the merge: `./build.sh install`, `gg init --update` (skills changed), remove the worktree and branch.
