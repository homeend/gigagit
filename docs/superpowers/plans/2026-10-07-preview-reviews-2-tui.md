# Preview reviews — plan 2: TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. (gigagit: executing-plans, inline — NO implementer subagents.)

**Goal:** The TUI shows a merge preview's / commit pair's AI reviews where the preview lives — sub-rows on the Previews list, a "Reviews" block in the opened preview, a "Review (AI)" + "Show review" row on the preview's `.` menu — and the review view names the preview and says "older tip".

**Architecture:** `readPreviews` (srcPreviews, off-thread) attaches each row's classified review heads (`svc.PreviewReviews` / a new by-scope twin for a non-previewable merge row). The Previews list becomes an entry layer (`pvEntry`, the Branches `brEntry` pattern): `backingIndex` refuses a sub-row, so every preview-row action self-gates unchanged. The opened preview carries its heads on the same off-thread resolve that arms its note scope and puts them on top of its file list; the review view gets a "back to the preview" return.

**Tech Stack:** Go 1.26, Bubble Tea, real `git` in temp repos for tests, the e2e TOML harness with golden screens.

**Spec:** `docs/superpowers/specs/2026-10-07-preview-reviews-design.md` (§6 TUI; R2, R3, R5, R6; §8 errors; §9 tui + e2e testing). Plan 1 (merged `20b2fc2b`): `docs/superpowers/plans/2026-10-07-preview-reviews-1-domain-cli-skills.md`.

## Global Constraints

- NO implementer subagents; work in `.claude/worktrees/preview-reviews-tui` on `feat/preview-reviews-tui`; commit with `gg add <files>` + `git commit -F <msgfile>` (gg commit has no -F); never `git add -A`.
- `internal/tui` never imports `internal/git`; reads go through `domain`; nothing git-touching runs on the Update thread.
- Every user-visible TUI string goes through `i18n.T` with a literal key present in ALL FOUR bundles (`internal/i18n/lang/{ja,ko,ru,zh}.toml`, appended at the end of `[strings]`); the AST gates in `internal/tui` fail otherwise. Engine/CLI prose stays English.
- R2: a preview review shows only while BOTH commits it read exist; a moved tip shows "older tip"; a gone one is hidden (still in View all notes). Never synthesised from current files.
- R5: a preview review never marks a commit (already enforced by plan 1 — do not regress).
- R6: a review row's `.` menu is Open + Copy gg link + Delete — the existing review row menu (`noteRowMenu`'s `noteID` arm); nothing else.
- New ops need no `opAffectedSources` entry (the review lane is mapped); new tests call `t.Parallel()` unless they touch globals (`domain.Tasks()`, `i18n.SetLanguage`, `t.Setenv`).
- Window-return convention: a window opened from the preview returns to the preview on esc.
- Race gate: `./test.sh race` is green ONLY when its log says "all green".

## Rulings made while planning (spec gaps)

- **P2-R1 (spec §8 vs code):** `PreviewReviews` needs an OK set (`set.scope()` is "" otherwise), so a merge row that is merged / missing a side / has no base gets its reviews from a new `Service.PreviewReviewsByScope(ctx, scope)`: every review tagged `scope` whose two commits still exist, each `Older` (there is no current tip to match). That keeps §8 ("a deleted source branch's reviews show as older tip while the commits exist"). Cost if wrong: one extra domain function.
- **P2-R2 (header wording):** spec §6 writes the preview as `main ... feat/x`; the TUI names a preview `feat/x → main` everywhere (Previews subject column, Range review rows via `domain.NoteScopeLabel`). The review view header uses `NoteScopeLabel` — the TUI's own wording. Cost if wrong: one string format.
- **P2-R3 ("older tip" in the header):** `Review` has no `Older` and the view opens from many entry points (sub-row, Reviews block, link, View all notes, task result), so `openReviewFrom`'s off-thread read asks a new `Service.ReviewOlder(ctx, r)`; the head's `Older` is never threaded through `openReview`'s signature.
- **P2-R4 (sub-row menu):** R6's "Open + Delete" is the existing review-row menu, which also carries "Copy gg link" (the Branches sub-row and a commit's Reviews row both do). The preview sub-row gets exactly Open review / Copy gg link / Delete review.
- **P2-R5 (footer):** Review (AI) has no key of its own. The footer advertises it with footer-only bindings (id "", exempt from `TestActionMenuLabelCoverage`): `[.] review (AI)` on a reviewable preview row and `[enter] open review` on a review sub-row.

## Review Focus

1. **Index confusion after the entry layer.** Any site still treating a Previews display/backing index as an `m.previews` index panics or acts on the wrong row once sub-rows interleave (marks, steer landing, selection keys). Expect: every preview action on a sub-row is refused; on a preview row it acts on that row. Pinned by Task 2's `TestPreviewSubRowsDoNotShiftRowActions` and `TestPreviewSteerLandingSkipsSubRows`.
2. **A review saved while the preview is open / the tab is active.** Expect: the sub-row and the opened preview's Reviews block both appear after the note reload chain, without reopening anything. Pinned by Task 5's `TestOpenPreviewReviewsBlockFollowsARefresh` and Task 6's steer test.
3. **Esc from a review opened inside a preview.** Expect: back to the SAME preview (merge or pair), the cursor on the review's row — never a commit's file list, never a closed view. Pinned by Task 5's `TestReviewFromPreviewEscReturnsToThePreview` (merge) and `…Pair`.
4. **A preview that stops being previewable (merged / source deleted).** Expect: Review (AI) hidden; its reviews still listed as "older tip" while the commits exist. Pinned by Task 1's `TestPreviewReviewsByScopeKeepsExistingCommits` and Task 4's gating test.
5. **The filter.** Expect: a `/` query matching a preview keeps its sub-rows with it; a query matching only a sub-row's agent keeps its preview row; the ◆N badge never matches. Pinned by Task 2's `TestPreviewSubRowsFollowTheirRowUnderAFilter`.

---

### Task 1: Domain — reviews by scope name, and "is this review of an older tip"

**Files:**
- Modify: `internal/domain/preview_reviews.go`
- Test: `internal/domain/preview_review_test.go`

**Interfaces:**
- Consumes (plan 1): `Service.PreviewReviews(ctx, set) ([]ReviewHead, error)`, `NoteCounts.PreviewReviews map[string][]ReviewHead`, `Service.NoteScopeResolve(ctx, spec) (PreviewNoteSet, error)`, `Service.ResolveRev`.
- Produces:
  - `func (s *Service) PreviewReviewsByScope(ctx context.Context, scope string) ([]ReviewHead, error)` — every head tagged `scope` whose tip AND base still exist, each `Older = true`, newest first.
  - `func (s *Service) ReviewOlder(ctx context.Context, r Review) bool` — true when `r.Preview != ""` and the scope's tip today is not `r.Commit` (a scope that no longer resolves counts as older); false for every non-preview review.

- [ ] **Step 1: Write the failing tests** (append to `internal/domain/preview_review_test.go`)

```go
func TestPreviewReviewsByScopeKeepsExistingCommits(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	id := savePreviewReview(t, svc, set)
	// The preview stops being previewable: the source is merged into main.
	runGitIn(t, dir, "checkout", "-q", "main")
	runGitIn(t, dir, "merge", "-q", "--ff-only", "feat/x")
	got, err := svc.PreviewReviewsByScope(ctx, "main...feat/x")
	if err != nil || len(got) != 1 || got[0].ID != id || !got[0].Older {
		t.Fatalf("merged preview's review = %+v %v, want it listed as older", got, err)
	}
	if got, _ := svc.PreviewReviewsByScope(ctx, "main...other"); len(got) != 0 {
		t.Fatalf("another scope shows %+v", got)
	}
	if got, _ := svc.PreviewReviewsByScope(ctx, ""); len(got) != 0 {
		t.Fatalf("the empty scope shows %+v", got)
	}
}

func TestPreviewReviewsByScopeHidesAGoneTip(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	savePreviewReview(t, svc, set)
	runGitIn(t, dir, "checkout", "-q", "main")
	runGitIn(t, dir, "branch", "-D", "feat/x")
	runGitIn(t, dir, "reflog", "expire", "--expire=now", "--all")
	runGitIn(t, dir, "gc", "--prune=now", "--quiet")
	if got, err := svc.PreviewReviewsByScope(ctx, "main...feat/x"); err != nil || len(got) != 0 {
		t.Fatalf("a deleted, pruned source's review = %+v %v, want hidden", got, err)
	}
}

func TestReviewOlder(t *testing.T) {
	t.Parallel()
	dir, svc, set := previewReviewRepo(t)
	ctx := context.Background()
	id := savePreviewReview(t, svc, set)
	r, err := svc.Review(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if svc.ReviewOlder(ctx, r) {
		t.Fatal("a review of the current tip reads as older")
	}
	commitFile(t, dir, "g.txt", "y\n", "second") // the source moves on
	if !svc.ReviewOlder(ctx, r) {
		t.Fatal("a review of a moved tip does not read as older")
	}
	runGitIn(t, dir, "checkout", "-q", "main")
	runGitIn(t, dir, "branch", "-D", "feat/x")
	if !svc.ReviewOlder(ctx, r) {
		t.Fatal("a review whose scope no longer resolves does not read as older")
	}
	if svc.ReviewOlder(ctx, Review{Commit: set.Tip}) {
		t.Fatal("a commit review reads as older")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/preview-reviews-tui && go test ./internal/domain -run 'TestPreviewReviewsByScope|TestReviewOlder' -count=1`
Expected: FAIL to compile — `svc.PreviewReviewsByScope undefined`, `svc.ReviewOlder undefined`.

- [ ] **Step 3: Implement** — refactor the classification into one helper both entry points use (`internal/domain/preview_reviews.go`):

```go
func (s *Service) PreviewReviews(ctx context.Context, set PreviewNoteSet) ([]ReviewHead, error) {
	sc := set.scope()
	if sc == "" {
		return nil, nil
	}
	return s.classifyScopeReviews(ctx, sc, set.Tip, set.commitSet())
}

// PreviewReviewsByScope is PreviewReviews for a scope that cannot be built
// today — a merged preview, a deleted source (spec §8): with no current tip,
// every review whose two commits still exist is an older one (R2).
func (s *Service) PreviewReviewsByScope(ctx context.Context, scope string) ([]ReviewHead, error) {
	if strings.TrimSpace(scope) == "" {
		return nil, nil
	}
	return s.classifyScopeReviews(ctx, scope, "", nil)
}

// classifyScopeReviews keeps scope's reviews gg can still show exactly
// (R2): tip == the scope's tip is current; else both commits must exist
// (Older). in names commits known to exist without a git call.
func (s *Service) classifyScopeReviews(ctx context.Context, scope, tip string, in map[string]bool) ([]ReviewHead, error) {
	c, err := s.NoteCounts(ctx)
	if err != nil {
		return nil, err
	}
	heads := c.PreviewReviews[scope]
	if len(heads) == 0 {
		return nil, nil
	}
	exists := func(sha string) bool {
		if sha == "" {
			return false
		}
		if in[sha] {
			return true
		}
		_, found, err := s.ResolveRev(ctx, sha+"^{commit}")
		return err == nil && found
	}
	out := make([]ReviewHead, 0, len(heads))
	for _, h := range heads {
		if tip != "" && h.Commit == tip {
			out = append(out, h)
			continue
		}
		base, _, _ := strings.Cut(h.Scope, "..")
		if !exists(h.Commit) || !exists(base) {
			continue
		}
		h.Older = true
		out = append(out, h)
	}
	return out, nil
}

// ReviewOlder reports whether r is a preview review of a tip its preview has
// since moved past — the review view's "older tip". A scope that no longer
// resolves (merged, a side deleted) counts as older: its tip is not current.
func (s *Service) ReviewOlder(ctx context.Context, r Review) bool {
	if r.Preview == "" {
		return false
	}
	set, err := s.NoteScopeResolve(ctx, r.Preview)
	if err != nil || !set.OK() {
		return true
	}
	return set.Tip != r.Commit
}
```

(Keep the existing doc comment above `PreviewReviews`.)

- [ ] **Step 4: Run them to verify they pass, plus the plan-1 preview review tests**

Run: `go test ./internal/domain -run 'PreviewReview|ReviewOlder|ScopeReviewTarget' -count=1`
Expected: PASS (the three new tests and every existing `TestPreviewReviews*`).

- [ ] **Step 5: Commit**

```bash
gg add internal/domain/preview_reviews.go internal/domain/preview_review_test.go
git commit -F <msgfile>   # "feat(domain): preview reviews by scope name; ReviewOlder"
```

---

### Task 2: Previews list — review sub-rows (entry layer)

**Files:**
- Modify: `internal/tui/preview_panel.go` (row field, `readPreviews`, entries, rows, list type, `selectedPreview`)
- Modify: `internal/tui/viewstate.go` (`listFor` panelPreviews arm, `backingIndex` panelPreviews arm)
- Modify: `internal/tui/selection.go:71-72` (rowKeyAt → entry-aware Key)
- Modify: `internal/tui/preview_marks.go:38-60, 179-187` (display positions through entries)
- Modify: `internal/tui/steer_nav.go:351-356, 1005-1010` (landing through `previewDisplayIndex`)
- Modify: `internal/i18n/lang/{ja,ko,ru,zh}.toml` (`"older tip"`)
- Create: `internal/tui/preview_reviews.go` (sub-row helpers)
- Test: `internal/tui/preview_reviews_test.go`

**Interfaces:**
- Consumes: Task 1's `PreviewReviewsByScope`; plan 1's `PreviewReviews`; `branchReviewRowBody(domain.ReviewHead) string` (branch_reviews.go).
- Produces:
  - `previewRow.reviews []domain.ReviewHead` (newest first, classified).
  - `type pvEntry struct{ pv int; review string }` + `func (e pvEntry) sub() bool`.
  - `func (m Model) previewEntries() []pvEntry`.
  - `func (m Model) previewRowsFor(ents []pvEntry) []string` (replaces `previewRows`).
  - `func previewReviewRowBody(h domain.ReviewHead) string` — `"└ Review: <stamp> <agent> · <tally>"` + `" · older tip"` when `Older`.
  - `func (m Model) selectedPreviewEntry() (pvEntry, bool)`, `func (m Model) selectedPreviewReview() (domain.ReviewHead, bool)`.
  - `func (m Model) previewDisplayIndex(bi int) (int, bool)` — the display row of preview row `bi` (never a sub-row).
  - `func (m Model) previewRowOf(id string) (previewRow, bool)` is NOT needed; do not add it.

- [ ] **Step 1: Write the failing tests** (`internal/tui/preview_reviews_test.go`)

```go
package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// reviewPreviewsModel: two saved rows — a merge preview "login" with two
// reviews (a current one and one of an older tip) and a comparison row,
// focused on the Previews tab.
func reviewPreviewsModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t)
	m.loading = false
	now := time.Now()
	m.previews = []previewRow{
		{rec: model.MergePreview{ID: "p1", Label: "login", Source: "feat/x", Target: "main", Created: now},
			sum: domain.PreviewSummary{State: domain.PreviewOK, Files: 1, Ahead: 1},
			reviews: []domain.ReviewHead{
				{ID: "r2", Agent: "Claude Code", Created: now.Add(-time.Hour), Remarks: 3, Resolved: 1, Preview: "main...feat/x"},
				{ID: "r1", Agent: "Codex", Created: now.Add(-48 * time.Hour), Preview: "main...feat/x", Older: true},
			}},
		{kind: rowCompare, cmp: domain.SavedCompare{ID: "c1", Label: "cmp", Created: now.Add(-time.Minute)},
			cmpDesc: [2]string{"a", "b"}},
	}
	m = m.activateTab(panelPreviews)
	return m
}

func TestPreviewRowsShowReviewSubRows(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	rows, _ := m.panelView(panelPreviews)
	if len(rows) != 4 {
		t.Fatalf("rows %q, want login, its two reviews, cmp", rows)
	}
	if !strings.Contains(rows[0], "login") || !strings.Contains(rows[3], "cmp") {
		t.Fatalf("parent rows out of place: %q", rows)
	}
	cur := reviewStampAt(m.previews[0].reviews[0].Created, time.Now())
	if !strings.Contains(rows[1], "└ Review: "+cur+" Claude Code") || strings.Contains(rows[1], "older tip") {
		t.Fatalf("current review row %q", rows[1])
	}
	if !strings.Contains(rows[2], "Codex") || !strings.HasSuffix(strings.TrimRight(rows[2], " "), "· older tip") {
		t.Fatalf("older review row %q, want it to end with · older tip", rows[2])
	}
}

func TestPreviewSubRowsDoNotShiftRowActions(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	m.sel[panelPreviews] = 1 // a review sub-row
	if r, ok := m.selectedPreview(); ok {
		t.Fatalf("a sub-row resolved to preview %q", r.id())
	}
	if h, ok := m.selectedPreviewReview(); !ok || h.ID != "r2" {
		t.Fatalf("selectedPreviewReview = %+v %v", h, ok)
	}
	m.sel[panelPreviews] = 3 // the comparison row, after two sub-rows
	if r, ok := m.selectedPreview(); !ok || r.id() != "c1" {
		t.Fatalf("row after the sub-rows = %+v %v, want c1", r.id(), ok)
	}
	if a, b := m.rowKeyAt(panelPreviews, 0), m.rowKeyAt(panelPreviews, 1); a != "p1" || a == b {
		t.Fatalf("keys %q %q: a preview row keys by its id, a sub-row distinctly", a, b)
	}
	// Marks address preview rows only, by id, in display order.
	m.previewCompareSet = map[string]bool{"c1": true}
	if got := m.previewMarkedSubjects(); got != "cmp" {
		t.Fatalf("marked subjects %q", got)
	}
}

func TestPreviewSubRowsFollowTheirRowUnderAFilter(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	m.filterQuery, m.filterPanel = "login", panelPreviews
	rows, _ := m.panelView(panelPreviews)
	if len(rows) != 3 || !strings.Contains(rows[1], "└ Review:") {
		t.Fatalf("filter on the preview: %q, want it and both reviews", rows)
	}
	m.filterQuery = "Codex"
	rows, _ = m.panelView(panelPreviews)
	if len(rows) == 0 || !strings.Contains(rows[0], "login") {
		t.Fatalf("filter on a review's agent: %q, want its preview kept", rows)
	}
}

func TestPreviewSteerLandingSkipsSubRows(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	if di, ok := m.previewDisplayIndex(1); !ok || di != 3 {
		t.Fatalf("display index of the comparison row = %d %v, want 3", di, ok)
	}
}

// A real store: a review saved on the preview shows as its sub-row after
// a previews read; once the source moves on it is an older tip.
func TestPreviewSubRowFromTheStore(t *testing.T) {
	t.Parallel()
	m, dir := storedPreviewReviewModel(t)
	rows, _ := m.panelView(panelPreviews)
	if len(rows) != 2 || !strings.Contains(rows[1], "└ Review:") || strings.Contains(rows[1], "older tip") {
		t.Fatalf("rows %q, want the preview and its current review", rows)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "checkout", "-q", "feat/x")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "more")
	runGit(t, dir, "checkout", "-q", "main")
	m = readPreviewsNow(t, m)
	rows, _ = m.panelView(panelPreviews)
	if len(rows) != 2 || !strings.Contains(rows[1], "older tip") {
		t.Fatalf("rows %q, want the review marked older tip", rows)
	}
}

// storedPreviewReviewModel: mergePreviewModel's repo with a note store and
// one review saved on the "login" preview (main...feat/x).
func storedPreviewReviewModel(t *testing.T) (Model, string) {
	t.Helper()
	dir, repo := newRepoDir(t)
	runGit(t, dir, "checkout", "-q", "-b", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "add a")
	runGit(t, dir, "checkout", "-q", "main")
	svc := domain.New(repo)
	svc.UsePreviewsDir(t.TempDir())
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	if _, err := svc.PreviewAdd(ctx, "feat/x", "main", "login"); err != nil {
		t.Fatal(err)
	}
	set, err := svc.PreviewNotes(ctx, "feat/x", "main")
	if err != nil || !set.OK() {
		t.Fatalf("PreviewNotes: %+v %v", set, err)
	}
	doc := `{"version":1,"summary":"## Summary\nok","files":[{"path":"a.txt","annotations":[{"newRange":[1,1],"summary":"one"}]}]}`
	if _, _, err := svc.SaveReview(ctx, domain.SaveReview{Target: domain.ScopeReviewTarget(set), Agent: "Claude Code", Text: doc}); err != nil {
		t.Fatal(err)
	}
	m := New(svc)
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)
	m = readPreviewsNow(t, m)
	return m.activateTab(panelPreviews), dir
}

// readPreviewsNow runs one srcPreviews read to completion.
func readPreviewsNow(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ := m.Update(cmd())
	return updated.(Model)
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run 'TestPreviewRowsShowReviewSubRows|TestPreviewSubRows|TestPreviewSteerLanding|TestPreviewSubRowFromTheStore' -count=1`
Expected: FAIL to compile — `unknown field reviews in struct literal of type previewRow`, `m.selectedPreviewReview undefined`, `m.previewDisplayIndex undefined`.

- [ ] **Step 3: Implement**

`internal/tui/preview_panel.go`:
- Add to `previewRow`: `reviews []domain.ReviewHead // the row's AI reviews (spec R2: current, then older tips), newest first`.
- In `readPreviews`, merge rows: inside the `PreviewOK` block, after the counts, `row.reviews, _ = svc.PreviewReviews(ctx, set)` (best-effort, like the counts). For a merge row with `err == nil && sum.State != domain.PreviewOK`: `row.reviews, _ = svc.PreviewReviewsByScope(ctx, p.Target+"..."+p.Source)` (P2-R1). Pair rows: inside the `PairOK` block, `row.reviews, _ = svc.PreviewReviews(ctx, set)`. Comparison rows: none.
- Replace `previewList` and `previewRows`:

```go
// previewList is entry-based like branchList: each saved row is followed by
// one sub-row per AI review of it (previewEntries). A sub-row's Name and Date
// are its row's, so the stable sort keeps it under its parent; backingIndex
// refuses it, so every preview action self-gates on a sub-row.
type previewList struct {
	rows []previewRow
	ents []pvEntry
	text []string // one per entry
}

func (l previewList) Len() int          { return len(l.ents) }
func (l previewList) Row(i int) string  { return l.text[i] }
func (l previewList) Name(i int) string { return l.rows[l.ents[i].pv].label() }
func (l previewList) Date(i int) int64  { return l.rows[l.ents[i].pv].created().Unix() }

// Key is the record id for a preview row (selection, marks and steer
// landings key on it) and id\x00review for a sub-row.
func (l previewList) Key(i int) string {
	k := l.rows[l.ents[i].pv].id()
	if r := l.ents[i].review; r != "" {
		k += "\x00" + r
	}
	return k
}

// Haystack: a row and its sub-rows match as one unit (branchList's rule) —
// the row WITHOUT its ◆N badge (a digit never matches a note count) plus
// every sub-row's text.
func (l previewList) Haystack(i int) string {
	p := i
	for p > 0 && l.ents[p].sub() {
		p--
	}
	var sb strings.Builder
	sb.WriteString(strings.TrimSuffix(l.text[p], noteBadge(l.rows[l.ents[p].pv].notes)))
	for q := p + 1; q < len(l.ents) && l.ents[q].sub(); q++ {
		sb.WriteByte(' ')
		sb.WriteString(l.text[q])
	}
	return sb.String()
}
```

`previewRows()` becomes `previewRowsFor(ents []pvEntry) []string`: compute the parent texts exactly as today into `parent[i]`, then for each entry append `parent[e.pv]` or, for a sub-row, `"  " + previewReviewRowBody(h)` (look the head up in `m.previews[e.pv].reviews` by id; a missing one renders `"  └ ?"`).

`selectedPreview` keeps its body (backingIndex now refuses sub-rows).

`internal/tui/preview_reviews.go` (new):

```go
package tui

// The Previews list shows a preview's (or commit pair's) AI reviews as
// sub-rows under it (spec R3), the Branches tab's review sub-rows: pvEntry
// rows, so sorting, the filter and backingIndex treat them like a branch's.

type pvEntry struct {
	pv     int    // index into m.previews
	review string // a review sub-row: its note id ("" = the saved row itself)
}

func (e pvEntry) sub() bool { return e.review != "" }

// previewEntries is the Previews list in backing order: each row followed by
// its reviews (readPreviews classified them: current, then older tips).
func (m Model) previewEntries() []pvEntry {
	out := make([]pvEntry, 0, len(m.previews))
	for i, r := range m.previews {
		out = append(out, pvEntry{pv: i})
		for _, h := range r.reviews {
			out = append(out, pvEntry{pv: i, review: h.ID})
		}
	}
	return out
}

// previewReviewRowBody is a sub-row without its indent: the Branches
// sub-row's text, plus " · older tip" for a review of a tip the preview has
// moved past (R2: it still opens exactly what was reviewed).
func previewReviewRowBody(h domain.ReviewHead) string {
	body := branchReviewRowBody(h)
	if h.Older {
		body += " · " + i18n.T("older tip")
	}
	return body
}

func (m Model) previewReviewHead(pv int, id string) (domain.ReviewHead, bool) {
	if pv < 0 || pv >= len(m.previews) {
		return domain.ReviewHead{}, false
	}
	for _, h := range m.previews[pv].reviews {
		if h.ID == id {
			return h, true
		}
	}
	return domain.ReviewHead{}, false
}

func (m Model) selectedPreviewEntry() (pvEntry, bool) {
	idx := m.displayIndices(panelPreviews)
	sel := m.sel[panelPreviews]
	if sel < 0 || sel >= len(idx) {
		return pvEntry{}, false
	}
	ents := m.previewEntries()
	if idx[sel] >= len(ents) {
		return pvEntry{}, false
	}
	return ents[idx[sel]], true
}

// selectedPreviewReview resolves a review sub-row under the Previews cursor.
func (m Model) selectedPreviewReview() (domain.ReviewHead, bool) {
	if m.focus != panelPreviews {
		return domain.ReviewHead{}, false
	}
	e, ok := m.selectedPreviewEntry()
	if !ok || !e.sub() {
		return domain.ReviewHead{}, false
	}
	return m.previewReviewHead(e.pv, e.review)
}

// previewDisplayIndex is the display row of saved row bi — a steer landing's
// target, never one of its sub-rows.
func (m Model) previewDisplayIndex(bi int) (int, bool) {
	ents := m.previewEntries()
	for di, u := range m.displayIndices(panelPreviews) {
		if u < len(ents) && !ents[u].sub() && ents[u].pv == bi {
			return di, true
		}
	}
	return 0, false
}
```

(imports: `domain`, `i18n`.)

`internal/tui/viewstate.go`:
- `listFor` panelPreviews: `ents := m.previewEntries(); return previewList{rows: m.previews, ents: ents, text: m.previewRowsFor(ents)}`.
- `backingIndex`, next to the panelBranches arm:

```go
	if p == panelPreviews {
		// A review sub-row is not a saved row.
		ents := m.previewEntries()
		if u >= len(ents) || ents[u].sub() {
			return 0, false
		}
		return ents[u].pv, true
	}
```

`internal/tui/selection.go:71-72`: `case panelPreviews: return m.listFor(p).Key(u) // entry-aware: a sub-row keys as id\x00review` (and update the doc bullet above `rowKeyAt`).

`internal/tui/preview_marks.go`:
- `previewMarkedLinks`: build `pos` through entries — `ents := m.previewEntries(); for n, u := range m.displayIndices(panelPreviews) { if u < len(ents) && !ents[u].sub() { pos[ents[u].pv] = n } }`.
- `previewMarkedSubjects`: iterate `displayIndices`, map each `u` through `ents`, skip sub-rows, then test `m.previews[ents[u].pv]`.

`internal/tui/steer_nav.go` (both landings, ~351 and ~1005): replace the `for di, b := range m.displayIndices(panelPreviews) { if b == bi …` loops with `if di, ok := m.previewDisplayIndex(bi); ok { m.sel[panelPreviews] = di }`.

Grep once more before running: `grep -n 'displayIndices(panelPreviews)\|m\.previews\[' internal/tui/*.go | grep -v _test` — every remaining hit must go through entries or `backingIndex`.

i18n: append to all four bundles' `[strings]`:
- ja: `"older tip" = "古い先端"`
- ko: `"older tip" = "이전 끝 커밋"`
- ru: `"older tip" = "старая вершина"`
- zh: `"older tip" = "旧的末端"`

- [ ] **Step 4: Run the new tests, then the whole Previews / i18n test set**

Run: `go test ./internal/tui -run 'Preview|Review|I18n|Steer' -count=1`
Expected: PASS — the five new tests and every existing Previews / steer-preview / i18n-gate test.

- [ ] **Step 5: Watch a guard fail.** Temporarily delete the `if p == panelPreviews` arm in `backingIndex`; `TestPreviewSubRowsDoNotShiftRowActions` must FAIL; restore it by re-applying the edit (never `git checkout --`). Re-run: PASS.

- [ ] **Step 6: Commit**

```bash
gg add internal/tui/preview_panel.go internal/tui/preview_reviews.go internal/tui/preview_reviews_test.go internal/tui/viewstate.go internal/tui/selection.go internal/tui/preview_marks.go internal/tui/steer_nav.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/ru.toml internal/i18n/lang/zh.toml
git commit -F <msgfile>   # "feat(tui): Previews list shows a preview's reviews as sub-rows"
```

---

### Task 3: Sub-row actions — enter opens, `.` = Open / Copy gg link / Delete, footer hint

**Files:**
- Modify: `internal/tui/model.go` (the `enter` arm for `panelPreviews`, ~2862)
- Modify: `internal/tui/preview_reviews.go` (menu rows)
- Modify: `internal/tui/review_delete.go` (`deleteReviewRow` panelPreviews case)
- Modify: `internal/tui/action_menu.go` (append the sub-row rows after the branch review rows, ~324)
- Modify: `internal/tui/footer.go` (footer-only `[enter] open review`)
- Modify: `internal/i18n/lang/*.toml` (`"[enter] open review"`)
- Test: `internal/tui/preview_reviews_test.go`

**Interfaces:**
- Consumes: Task 2's `selectedPreviewReview()`; `openReview(id, title string) (Model, tea.Cmd)`; `asyncCopyLinkRow`; `confirmStoredDelete(id, prompt string, review bool)`; `reviewQuote`.
- Produces: `func (m Model) previewReviewRowMenu() []actionRow` (ids `open-review`, `copy-gg-link`, `delete-review`).

- [ ] **Step 1: Write the failing tests** (append)

```go
func TestPreviewReviewSubRowEnterOpensTheReview(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	m.sel[panelPreviews] = 1
	_, cmd := updateKey(m, "enter")
	if cmd == nil {
		t.Fatal("enter on a review sub-row started nothing")
	}
	if msg, ok := cmd().(reviewViewMsg); !ok || msg.id != "r2" {
		t.Fatalf("enter read %#v, want the review r2", msg)
	}
}

// R6: Open + Copy gg link + Delete, and none of the preview row's actions.
func TestPreviewReviewSubRowMenu(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	m.sel[panelPreviews] = 2
	ids := map[string]bool{}
	for _, r := range m.actionMenuRows() {
		ids[r.id] = true
	}
	for _, want := range []string{"open-review", "copy-gg-link", "delete-review"} {
		if !ids[want] {
			t.Errorf("sub-row menu lacks %q (%v)", want, ids)
		}
	}
	for _, not := range []string{"preview-open", "preview-rename", "preview-delete", "preview-swap", "preview-review", "preview-show-review", "copy-link"} {
		if ids[not] {
			t.Errorf("sub-row menu offers the preview's %q", not)
		}
	}
	if !strings.Contains(m.footerLine(), "open review") {
		t.Errorf("footer %q does not advertise enter on a review row", m.footerLine())
	}
}
```

(If the menu builder is not named `actionMenuRows`, use the name `TestActionMenuRenders` in `action_menu_test.go` calls — check with `grep -n 'func (m Model) .*\[\]actionRow' internal/tui/action_menu.go`; same for `footerLine`.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run 'TestPreviewReviewSubRow' -count=1`
Expected: FAIL — enter on the sub-row does nothing (the row is refused by `selectedPreview`), the menu lacks `open-review`.

- [ ] **Step 3: Implement**

`model.go`, the `enter` arm for Previews (before `selectedPreview()`):

```go
			if m.focus == panelPreviews {
				// A review sub-row opens the review, never its preview.
				if h, ok := m.selectedPreviewReview(); ok {
					return m.openReview(h.ID, h.Summary)
				}
				if r, ok := m.selectedPreview(); ok && m.opsIdle() {
```

`preview_reviews.go`:

```go
// previewReviewRowMenu is a review sub-row's . menu (R6): the existing
// review row menu — open, copy its link, delete.
func (m Model) previewReviewRowMenu() []actionRow {
	h, ok := m.selectedPreviewReview()
	if !ok || m.inContentWindow() || m.svc == nil {
		return nil
	}
	svc, id := m.svc, h.ID
	return []actionRow{
		{id: "open-review", label: i18n.T("Open review"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openReview(h.ID, h.Summary)
		}},
		m.asyncCopyLinkRow("copy-gg-link", i18n.T("Copy gg link"), func(ctx context.Context) (string, error) {
			return svc.ReviewLink(ctx, id)
		}),
	}
}
```

`review_delete.go` `deleteReviewRow`: add a case before `default`:

```go
	case m.focus == panelPreviews && !m.inContentWindow():
		h, ok := m.selectedPreviewReview()
		if !ok {
			return actionRow{}, false
		}
		id, quote = h.ID, reviewQuote(h.Agent, h.Summary)
```

`action_menu.go`, right after the `deleteReviewRow` append in the panel menu (~324): insert the sub-row's open + copy rows BEFORE that delete row so the order reads Open, Copy, Delete:

```go
	out = append(out, m.previewReviewRowMenu()...)
	if r, ok := m.deleteReviewRow(); ok {
		out = append(out, r)
	}
```

(i.e. add the one `previewReviewRowMenu` line above the existing `deleteReviewRow` append.)

`footer.go`, after the `preview-swap` binding:

```go
		{"", "enter", i18n.T("[enter] open review"), func(m Model) bool {
			_, ok := m.selectedPreviewReview()
			return ok
		}, scopeRow},
```

i18n (all four): ja `"[enter] open review" = "[enter] レビューを開く"`, ko `"[enter] 리뷰 열기"`, ru `"[enter] открыть ревью"`, zh `"[enter] 打开审查"`.

- [ ] **Step 4: Run them to verify they pass, plus the menu/footer gates**

Run: `go test ./internal/tui -run 'PreviewReview|ActionMenu|Footer|I18n|MenuLabel' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui/model.go internal/tui/preview_reviews.go internal/tui/preview_reviews_test.go internal/tui/review_delete.go internal/tui/action_menu.go internal/tui/footer.go internal/i18n/lang/*.toml
git commit -F <msgfile>   # "feat(tui): a preview review sub-row opens, copies its link, deletes"
```

---

### Task 4: Preview row `.` menu — Review (AI) and Show review

**Files:**
- Modify: `internal/tui/review.go` (rows + async target)
- Modify: `internal/tui/action_menu.go` (append after the sub-row rows)
- Modify: `internal/tui/footer.go` (footer-only `[.] review (AI)`)
- Modify: `internal/tui/review.go` `applyReviewResult` (also chain a previews read)
- Modify: `internal/i18n/lang/*.toml` (`"Review (AI)"`, `"[.] review (AI)"`)
- Test: `internal/tui/preview_reviews_test.go`

**Interfaces:**
- Consumes: `domain.ScopeReviewTarget(set)`; `svc.PreviewNotes(ctx, source, target)`; `svc.PairNotes(ctx, a, b)`; `reviewTargetReadyMsg{svc, target, err}` (handled at model.go:3935 → `startReview`); `hasReviewTool()`; `chainPreviewsRead()`.
- Produces:
  - `func (m Model) previewReviewRow() (actionRow, bool)` — id `preview-review`, label `Review (AI)`.
  - `func (m Model) previewShowReviewRow() (actionRow, bool)` — id `preview-show-review`, label `Show review`.
  - `func (m Model) previewReviewTargetCmd(r previewRow) tea.Cmd`.

- [ ] **Step 1: Write the failing tests** (append; `reviewToolModel` sets a capture review tool the way `TestReviewRowsAbsentWithoutTool`'s neighbours do — `m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "echo hi")}`)

```go
func TestPreviewReviewRowGating(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "echo hi")}
	m.sel[panelPreviews] = 0
	if r, ok := m.previewReviewRow(); !ok || r.id != "preview-review" || r.label != "Review (AI)" {
		t.Fatalf("an ok preview: %+v %v", r, ok)
	}
	m.sel[panelPreviews] = 1 // a sub-row
	if _, ok := m.previewReviewRow(); ok {
		t.Fatal("offered on a review sub-row")
	}
	m.sel[panelPreviews] = 3 // a saved comparison
	if _, ok := m.previewReviewRow(); ok {
		t.Fatal("offered on a comparison row")
	}
	m.sel[panelPreviews] = 0
	m.previews[0].sum.State = domain.PreviewMerged
	if _, ok := m.previewReviewRow(); ok {
		t.Fatal("offered on a merged preview")
	}
	m.previews[0].sum.State = domain.PreviewOK
	m.cfg.Tools.Command = nil
	if _, ok := m.previewReviewRow(); ok {
		t.Fatal("offered with no review tool")
	}
}

func TestPreviewReviewRowTargetsTheScope(t *testing.T) {
	t.Parallel()
	m, _ := storedPreviewReviewModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "echo hi")}
	m.sel[panelPreviews] = 0
	r, ok := m.previewReviewRow()
	if !ok {
		t.Fatal("row not offered")
	}
	_, cmd := r.run(m)
	msg, ok := cmd().(reviewTargetReadyMsg)
	if !ok || msg.err != nil {
		t.Fatalf("target hop = %#v", msg)
	}
	if msg.target.Preview != "main...feat/x" || !strings.Contains(msg.target.Range, "..") || msg.target.Kind != domain.ReviewRange {
		t.Fatalf("target %+v, want the preview's scope", msg.target)
	}
}

func TestPreviewShowReviewIsTheNewestCurrent(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	m.sel[panelPreviews] = 0
	r, ok := m.previewShowReviewRow()
	if !ok || r.id != "preview-show-review" {
		t.Fatalf("row %+v %v", r, ok)
	}
	_, cmd := r.run(m)
	if msg, ok := cmd().(reviewViewMsg); !ok || msg.id != "r2" {
		t.Fatalf("Show review opened %#v, want r2", msg)
	}
	m.previews[0].reviews = m.previews[0].reviews[1:] // only the older one left
	if _, ok := m.previewShowReviewRow(); ok {
		t.Fatal("Show review offered with no current review")
	}
}
```

(imports: `config`, `exttool`.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run 'TestPreviewReviewRow|TestPreviewShowReview' -count=1`
Expected: FAIL to compile — `m.previewReviewRow undefined`, `m.previewShowReviewRow undefined`.

- [ ] **Step 3: Implement** (`internal/tui/review.go`, next to `branchReviewRow`)

```go
// previewReviewRow offers "Review (AI)" on a Previews row: a merge preview
// that is previewable now, or a commit pair whose two commits both exist —
// never a comparison or a sub-row. The target needs the scope's merge base,
// so the row resolves it off the UI thread (reviewTargetReadyMsg).
func (m Model) previewReviewRow() (actionRow, bool) {
	if m.focus != panelPreviews || m.inContentWindow() || !m.opsIdle() || !m.hasReviewTool() {
		return actionRow{}, false
	}
	r, ok := m.selectedPreview()
	if !ok || r.err != nil {
		return actionRow{}, false
	}
	switch r.kind {
	case rowMerge:
		if r.sum.State != domain.PreviewOK {
			return actionRow{}, false
		}
	case rowPair:
		if r.psum.State != domain.PairOK {
			return actionRow{}, false
		}
	default:
		return actionRow{}, false
	}
	return actionRow{
		id:    "preview-review",
		label: i18n.T("Review (AI)"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.previewReviewTargetCmd(r)
		},
	}, true
}

// previewReviewTargetCmd builds the scope's review target off the UI thread.
// A scope that no longer resolves (the row went stale) is an error, never a
// review of nothing.
func (m Model) previewReviewTargetCmd(r previewRow) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ctx := context.Background()
		var set domain.PreviewNoteSet
		var err error
		if rec, isMerge := r.merge(); isMerge {
			set, err = svc.PreviewNotes(ctx, rec.Source, rec.Target)
		} else {
			set, err = svc.PairNotes(ctx, r.pair.A, r.pair.B)
		}
		if err == nil && !set.OK() {
			err = errors.New("nothing to review: the preview has no changes against its base")
		}
		if err != nil {
			return reviewTargetReadyMsg{svc: svc, err: err}
		}
		return reviewTargetReadyMsg{svc: svc, target: domain.ScopeReviewTarget(set)}
	}
}

// previewShowReviewRow is the preview row's "Show review": its newest review
// of the current tip (R2 — an older tip's is on its sub-row).
func (m Model) previewShowReviewRow() (actionRow, bool) {
	if m.focus != panelPreviews || m.inContentWindow() {
		return actionRow{}, false
	}
	r, ok := m.selectedPreview()
	if !ok {
		return actionRow{}, false
	}
	for _, h := range r.reviews {
		if h.Older {
			continue
		}
		return actionRow{
			id:    "preview-show-review",
			label: i18n.T("Show review"),
			run: func(m Model) (tea.Model, tea.Cmd) {
				return m.openReview(h.ID, h.Summary)
			},
		}, true
	}
	return actionRow{}, false
}
```

(The error string is a status-line detail shown through `i18n.T("review: %s", …)` like every lane error; add `"errors"` to the imports.)

`action_menu.go`, next to `branchReviewRow` / `showBranchReviewRow`:

```go
	if r, ok := m.previewReviewRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.previewShowReviewRow(); ok {
		out = append(out, r)
	}
```

`footer.go`, after the Task 3 binding:

```go
		{"", ".", i18n.T("[.] review (AI)"), func(m Model) bool {
			_, ok := m.previewReviewRow()
			return ok
		}, scopeRow},
```

`applyReviewResult` (review.go): after the `srcNotes` reload, also refresh the Previews rows — a review run takes minutes and the user may have left the tab, which the srcNotes→previews chain (gated on the tab being active) would miss:

```go
	m, counts = m.reloadSourcesCmd([]sourceKey{srcNotes}, reloadOpts{})
	var rows tea.Cmd
	if len(m.previews) > 0 {
		m, rows = m.chainPreviewsRead() // a preview review's sub-row (spec §6 Refresh)
	}
```

and add `rows` to the final `tea.Batch`.

i18n (all four):
- `"Review (AI)"`: ja `"レビュー (AI)"`, ko `"리뷰 (AI)"`, ru `"Ревью (AI)"`, zh `"审查 (AI)"`.
- `"[.] review (AI)"`: ja `"[.] レビュー (AI)"`, ko `"[.] 리뷰 (AI)"`, ru `"[.] ревью (AI)"`, zh `"[.] 审查 (AI)"`.

- [ ] **Step 4: Run them to verify they pass, plus the review lane tests**

Run: `go test ./internal/tui -run 'PreviewReview|PreviewShowReview|ReviewRow|ReviewResult|ActionMenu|Footer|I18n' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui/review.go internal/tui/action_menu.go internal/tui/footer.go internal/tui/preview_reviews_test.go internal/i18n/lang/*.toml
git commit -F <msgfile>   # "feat(tui): Review (AI) and Show review on a preview row"
```

---

### Task 5: The opened preview's "Reviews" block, and esc back to the preview

**Files:**
- Modify: `internal/tui/preview_open.go` (`previewOpenMsg.heads`, `previewOpenMsg.landNote`, resolve, both arm paths, `afterPreviewsRefresh`)
- Modify: `internal/tui/saved_pair_open.go` (`pairOpenMsg.landNote`) and `internal/tui/saved_pair_notes.go` (`pairNotesMsg.heads`)
- Modify: `internal/tui/model.go` (field `filesPreviewReviews`; the `compareFilesMsg` arm)
- Modify: `internal/tui/files_view.go` (`closeFilesView` clears the field; enter on a `noteID` line inside a preview)
- Modify: `internal/tui/review_view.go` (`reviewViewState.backPreview`, `openReviewFrom` → `openReviewWith`)
- Modify: `internal/tui/review_delete.go` (`leaveReviewView` preview branch)
- Modify: `internal/tui/preview_reviews.go` (block helpers)
- Test: `internal/tui/preview_reviews_test.go`

**Interfaces:**
- Consumes: Task 2's `previewReviewRowBody`; `svc.PreviewReviews(ctx, set)`; `openPreviewCmd(id, source, target, keepPath)`; `svc.PairOpen`.
- Produces:
  - `Model.filesPreviewReviews []domain.ReviewHead` — the opened scope's heads (cleared by `closeFilesView`).
  - `func previewReviewLines(heads []domain.ReviewHead, lines []contentLine) []contentLine` — replaces a leading Reviews block (a heading followed by `noteID` lines) with the new one; no heads = no block.
  - `func (m Model) setPreviewReviews(heads []domain.ReviewHead) Model` — store + apply to the list on screen (skipped while a review view or a loading placeholder is shown), keeping the cursor on the same row, landing `filesLandNote`.
  - `type previewReturn struct{ id, source, target string; pair *domain.CommitPair; title string }` and `reviewViewState.backPreview *previewReturn`.
  - `func (m Model) openReviewWith(id, title string, back model.Commit, bp *previewReturn) (Model, tea.Cmd)` — `openReviewFrom` delegates to it with `bp == nil`.
  - `func (m Model) previewReturnHere() *previewReturn` — what the view on screen re-opens as (merge: from `previewOpen`; pair: from `filesPreviewSet` + `filesTitle`); nil otherwise.

- [ ] **Step 1: Write the failing tests** (append)

```go
// openStoredPreview opens the "login" preview of storedPreviewReviewModel and
// lands its file list (the resolve, then the compare files).
func openStoredPreview(t *testing.T, m Model) Model {
	t.Helper()
	m.sel[panelPreviews] = 0
	m, cmd := updateKey(m, "enter")
	m = drainUntilFilesListed(t, m, cmd)
	return m
}

func TestOpenPreviewShowsItsReviewsOnTop(t *testing.T) {
	t.Parallel()
	m, _ := storedPreviewReviewModel(t)
	m = openStoredPreview(t, m)
	lines := m.filesView.lines
	if len(lines) < 3 || !lines[0].heading || lines[0].text != "Reviews" || lines[1].noteID == "" || !strings.Contains(lines[1].text, "└ Review:") {
		t.Fatalf("file list %+v, want a Reviews block on top", lines)
	}
	if lines[len(lines)-1].path != "a.txt" {
		t.Fatalf("the files follow the block: %+v", lines)
	}
}

func TestReviewFromPreviewEscReturnsToThePreview(t *testing.T) {
	t.Parallel()
	m, _ := storedPreviewReviewModel(t)
	m = openStoredPreview(t, m)
	id := m.filesView.lines[1].noteID
	m.filesView.sel = 1
	m, cmd := updateKey(m, "enter")
	m = drainUntil(t, m, cmd, func(m Model) bool { return m.filesReview != nil })
	if st := m.filesReview; st.backPreview == nil || st.backPreview.source != "feat/x" {
		t.Fatalf("review opened without a way back to its preview: %+v", st)
	}
	m, cmd = updateKey(m, "esc")
	m = drainUntilFilesListed(t, m, cmd)
	if m.filesReview != nil || m.previewOpen == nil || m.previewOpen.source != "feat/x" {
		t.Fatalf("esc did not return to the preview (review %v, preview %+v)", m.filesReview != nil, m.previewOpen)
	}
	vis := m.filesView.visible()
	if m.filesView.sel < 0 || m.filesView.sel >= len(vis) || vis[m.filesView.sel].noteID != id {
		t.Fatalf("cursor not back on the review's row (sel %d)", m.filesView.sel)
	}
}

func TestPreviewReviewLinesReplaceInPlace(t *testing.T) {
	t.Parallel()
	files := []contentLine{{text: "M  a.txt", path: "a.txt"}}
	h := domain.ReviewHead{ID: "r1", Agent: "A"}
	once := previewReviewLines([]domain.ReviewHead{h}, files)
	twice := previewReviewLines([]domain.ReviewHead{h, {ID: "r2", Agent: "B"}}, once)
	if len(once) != 3 || len(twice) != 4 || twice[3].path != "a.txt" {
		t.Fatalf("once %d lines, twice %d: %+v", len(once), len(twice), twice)
	}
	if none := previewReviewLines(nil, twice); len(none) != 1 || none[0].path != "a.txt" {
		t.Fatalf("no heads left a block: %+v", none)
	}
}

func TestOpenPreviewReviewsBlockFollowsARefresh(t *testing.T) {
	t.Parallel()
	m, _ := storedPreviewReviewModel(t)
	m = openStoredPreview(t, m)
	m = m.setPreviewReviews(nil)
	if m.filesView.lines[0].heading {
		t.Fatalf("an emptied scope kept its block: %+v", m.filesView.lines)
	}
	// A previews read whose tips did not move re-applies the row's heads.
	m = readPreviewsNow(t, m)
	if l := m.filesView.lines; !l[0].heading || l[1].noteID == "" {
		t.Fatalf("a refresh did not restore the block: %+v", l)
	}
}
```

Helpers (add to the test file if no equivalent exists — check `grep -n 'func drain' internal/tui/*_test.go` first and reuse one):

```go
// drainUntil runs cmd and every command its messages return, depth-first,
// until done(m) holds or the queue is empty (tea.BatchMsg unwrapped). Ticks
// and never-ending commands are skipped by a 2 s per-command timeout.
func drainUntil(t *testing.T, m Model, cmd tea.Cmd, done func(Model) bool) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 && !done(m) {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		ch := make(chan tea.Msg, 1)
		go func() { ch <- c() }()
		var msg tea.Msg
		select {
		case msg = <-ch:
		case <-time.After(2 * time.Second):
			continue
		}
		if b, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, b...)
			continue
		}
		if msg == nil {
			continue
		}
		updated, next := m.Update(msg)
		m = updated.(Model)
		queue = append(queue, next)
	}
	if !done(m) {
		t.Fatalf("drainUntil: condition never held (status %q)", m.statusMsg)
	}
	return m
}

func drainUntilFilesListed(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	return drainUntil(t, m, cmd, func(m Model) bool {
		return m.filesView != nil && m.filesReview == nil && len(m.filesView.lines) > 0 &&
			!isLoadingPlaceholder(m.filesView.lines[0].text) && m.filesView.lines[0].heading
	})
}
```

Pair twin (same file):

```go
// storedPairReviewModel: a saved commit pair main..feat/x ("pair one") with
// one review saved on it, the Previews tab read.
func storedPairReviewModel(t *testing.T) Model {
	t.Helper()
	dir, repo := newRepoDir(t)
	base := strings.TrimSpace(runGitOut(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "-q", "-b", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "add a")
	tip := strings.TrimSpace(runGitOut(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "-q", "main")
	svc := domain.New(repo)
	svc.UsePreviewsDir(t.TempDir())
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	if _, err := svc.PairAdd(ctx, base, tip, "pair one"); err != nil {
		t.Fatal(err)
	}
	set, err := svc.PairNotes(ctx, base, tip)
	if err != nil || !set.OK() {
		t.Fatalf("PairNotes: %+v %v", set, err)
	}
	doc := `{"version":1,"summary":"## Summary\nok","files":[{"path":"a.txt","annotations":[{"newRange":[1,1],"summary":"one"}]}]}`
	if _, _, err := svc.SaveReview(ctx, domain.SaveReview{Target: domain.ScopeReviewTarget(set), Agent: "Claude Code", Text: doc}); err != nil {
		t.Fatal(err)
	}
	m := New(svc)
	updated, _ := m.Update(m.loadCmd()())
	m = readPreviewsNow(t, updated.(Model))
	return m.activateTab(panelPreviews)
}

func TestReviewFromPairEscReturnsToThePair(t *testing.T) {
	t.Parallel()
	m := storedPairReviewModel(t)
	rows, _ := m.panelView(panelPreviews)
	if len(rows) != 2 || !strings.Contains(rows[1], "└ Review:") {
		t.Fatalf("rows %q, want the pair and its review", rows)
	}
	m = openStoredPreview(t, m) // enter on row 0: the pair
	id := m.filesView.lines[1].noteID
	m.filesView.sel = 1
	m, cmd := updateKey(m, "enter")
	m = drainUntil(t, m, cmd, func(m Model) bool { return m.filesReview != nil })
	if bp := m.filesReview.backPreview; bp == nil || bp.pair == nil || bp.pair.Label != "pair one" {
		t.Fatalf("review opened without a way back to its pair: %+v", m.filesReview.backPreview)
	}
	m, cmd = updateKey(m, "esc")
	m = drainUntilFilesListed(t, m, cmd)
	if m.filesReview != nil || m.filesPreviewSet == nil || !m.filesPreviewSet.IsPair() || !strings.Contains(m.filesTitle, "pair one") {
		t.Fatalf("esc did not return to the pair (title %q)", m.filesTitle)
	}
	vis := m.filesView.visible()
	if m.filesView.sel < 0 || m.filesView.sel >= len(vis) || vis[m.filesView.sel].noteID != id {
		t.Fatalf("cursor not back on the review's row (sel %d)", m.filesView.sel)
	}
}
```

(`runGitOut` — check `grep -n 'func runGit' internal/tui/*_test.go`; if only `runGit` exists, add `runGitOut(t, dir, args...) string` beside it using `exec.Command("git", args...)` with `Dir = dir` and `Output()`.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run 'TestOpenPreview|TestReviewFromP|TestPreviewReviewLines' -count=1`
Expected: FAIL to compile — `previewReviewLines undefined`, `m.setPreviewReviews undefined`, `st.backPreview undefined`.

- [ ] **Step 3: Implement**

Model field (model.go, next to `filesPreviewCounts`):
`filesPreviewReviews []domain.ReviewHead // the open preview's AI reviews: the Reviews block on top of its file list`
`closeFilesView`: `m.filesPreviewReviews = nil`.

`preview_reviews.go`:

```go
// previewReviewLines puts scope's reviews on top of an opened preview's file
// list under a "Reviews" heading, replacing a block already there (a refresh
// re-applies it). Each row is a noteID row: enter opens the review view.
func previewReviewLines(heads []domain.ReviewHead, lines []contentLine) []contentLine {
	rest := lines
	if len(rest) > 1 && rest[0].heading && rest[1].noteID != "" {
		i := 1
		for i < len(rest) && rest[i].noteID != "" {
			i++
		}
		rest = rest[i:]
	}
	if len(heads) == 0 {
		return rest
	}
	out := make([]contentLine, 0, len(heads)+1+len(rest))
	out = append(out, contentLine{text: i18n.T("Reviews"), heading: true})
	for _, h := range heads {
		out = append(out, contentLine{text: "  " + previewReviewRowBody(h), noteID: h.ID})
	}
	return append(out, rest...)
}

// setPreviewReviews stores the open scope's heads and re-lays the list on
// screen, keeping the cursor on the row it was on; a pending landing
// (filesLandNote: back from a review) puts it on that review's row.
func (m Model) setPreviewReviews(heads []domain.ReviewHead) Model {
	m.filesPreviewReviews = heads
	p := m.filesView
	if p == nil || m.filesReview != nil || !m.inCompareMode() ||
		(len(p.lines) == 1 && isLoadingPlaceholder(p.lines[0].text)) {
		return m
	}
	var keep contentLine
	if vis := p.visible(); p.sel >= 0 && p.sel < len(vis) {
		keep = vis[p.sel]
	}
	p.lines = previewReviewLines(heads, p.lines)
	return m.landPreviewCursor(keep)
}

// landPreviewCursor puts the tree cursor on filesLandNote's row when it is
// listed (consuming it), else back on keep's row (same path or review).
func (m Model) landPreviewCursor(keep contentLine) Model {
	p := m.filesView
	vis := p.visible()
	if id := m.filesLandNote; id != "" {
		for i, l := range vis {
			if l.noteID == id {
				m.filesLandNote = ""
				p.sel = i
				return m
			}
		}
	}
	for i, l := range vis {
		if (keep.path != "" && l.path == keep.path) || (keep.noteID != "" && l.noteID == keep.noteID) {
			p.sel = i
			return m
		}
	}
	if p.sel >= len(vis) {
		p.sel = max(len(vis)-1, 0)
	}
	return m
}
```

`model.go` `compareFilesMsg` arm, after the `filesReview` block and the `filesView.sel = 0` line (before the keepPath block):

```go
		if m.filesReview == nil && m.filesPreviewSet != nil && len(m.filesPreviewReviews) > 0 {
			m.filesView.lines = previewReviewLines(m.filesPreviewReviews, m.filesView.lines)
			m = m.landPreviewCursor(contentLine{})
		}
```

Merge preview (`preview_open.go`):
- `previewOpenMsg` gains `heads []domain.ReviewHead` and `landNote string`; `reopenPreviewCmd` fills `msg.heads, _ = svc.PreviewReviews(ctx, set)` inside the same `PreviewOK` block that fills `msg.set` (skip when `set.Only != ""`).
- Add `openPreviewLandingCmd(id, source, target, landNote string) tea.Cmd` = `reopenPreviewCmd` with `landNote` stamped on the message (thread it as a trailing parameter of an unexported `resolvePreviewCmd` both call; keep `openPreviewCmd`/`reopenPreviewCmd` signatures).
- `handlePreviewOpenMsg`, the same-tag branch: after `m.filesPreviewSet, m.filesPreviewCounts = &set, msg.counts` add `m = m.setPreviewReviews(msg.heads)`.
- The full-open branch: after the `filesPreviewSet` arm add `m.filesPreviewReviews = msg.heads` and `m.filesLandNote = msg.landNote` (both AFTER `openCompareFiles`, which ran `closeFilesView`).
- `afterPreviewsRefresh`, the "did not move" branch: after the `byPath` update add `m = m.setPreviewReviews(r.reviews)`.

Pair (`saved_pair_open.go`, `saved_pair_notes.go`):
- `pairOpenMsg` gains `landNote string`; `handlePairOpenMsg` sets `m.filesLandNote = msg.landNote` after `openCompareFiles`.
- `pairNotesMsg` gains `heads []domain.ReviewHead`; `pairNotesCmd` fills `msg.heads, _ = svc.PreviewReviews(ctx, set)` when `only == ""`; `handlePairNotesMsg` after arming the set: `m = m.setPreviewReviews(msg.heads)`.

Review view return (`review_view.go`):

```go
// previewReturn is the preview a review view was opened from (its Reviews
// block): esc re-opens it, the cursor on the review's row.
type previewReturn struct {
	id, source, target string             // a merge preview (id "" = a one-off)
	pair               *domain.CommitPair // a commit pair (A, B, Label)
	title              string
}
```

- `reviewViewState` and `reviewViewMsg` gain `backPreview *previewReturn`.
- Rename the body of `openReviewFrom` to `openReviewWith(id, title string, back model.Commit, bp *previewReturn)` (setting `out.backPreview = bp`); `openReviewFrom(id, title, back)` = `openReviewWith(id, title, back, nil)`. `handleReviewViewMsg` copies `msg.backPreview` into the state.
- `previewReturnHere`:

```go
func (m Model) previewReturnHere() *previewReturn {
	if po := m.previewOpen; po != nil {
		return &previewReturn{id: po.id, source: po.source, target: po.target, title: po.title}
	}
	if s := m.filesPreviewSet; s != nil && s.IsPair() {
		return &previewReturn{pair: &domain.CommitPair{A: s.Base, B: s.Tip, Label: m.filesPairLabel}, title: m.filesTitle}
	}
	return nil
}
```

`filesPairLabel string` is a new Model field (next to `filesPreviewReviews`): `handlePairOpenMsg` sets it to `msg.pair.Label` after `openCompareFiles`; `closeFilesView` clears it. A pair opened from a link (no saved row) has an empty label: `pairTitle("")` then re-titles it on return — acceptable, and `pairOpenMsg`'s handler falls back to `msg.pair.DefaultLabel()` when `Label == ""`.

`files_view.go`, the `l.noteID != ""` enter branch:

```go
	if l.noteID != "" { // an @notes/ entry: the review opens as the review view, esc comes back here
		if bp := m.previewReturnHere(); bp != nil && m.inCompareMode() {
			return m.openReviewWith(l.noteID, reviewTitle(bp.title), model.Commit{}, bp)
		}
		back := m.filesCommit
		…unchanged…
```

`review_delete.go` `leaveReviewView`, first branch:

```go
	if st := m.filesReview; st != nil && st.backPreview != nil {
		// Opened from a preview's Reviews block: re-open that preview, the
		// cursor on the review's row. One-shot — a preview that no longer
		// opens leaves the review on screen, and the next esc closes it.
		bp, id := st.backPreview, st.id
		cp := *st
		cp.backPreview = nil
		m.filesReview = &cp
		if bp.pair != nil {
			svc, gen, p := m.svc, m.previewGen, *bp.pair
			return m, func() tea.Msg {
				eps, err := svc.PairOpen(context.Background(), p.A, p.B)
				return pairOpenMsg{pair: p, eps: eps, gen: gen, err: err, landNote: id}
			}
		}
		return m, m.openPreviewLandingCmd(bp.id, bp.source, bp.target, id)
	}
```

Note the pair title: `handlePairOpenMsg` sets `m.filesTitle = pairTitle(msg.pair.Label)` — the Label carried in `previewReturn.pair` must be the saved label (see the field note above).

- [ ] **Step 4: Run them to verify they pass, then the preview/review/steer suites**

Run: `go test ./internal/tui -run 'Preview|Review|Pair|Steer|Files' -count=1`
Expected: PASS.

- [ ] **Step 5: Watch a guard fail.** Remove the `cp.backPreview = nil` one-shot line → no test fails? Then the guard is untested: add `TestReviewFromPreviewEscTwiceWhenThePreviewIsGone` — open the review from the block, delete `feat/x` (`runGit … branch -D`, after switching to main), esc → status names the missing branch and the review stays; esc again → the view closes (`m.filesView == nil`). Watch it fail with the line removed, pass with it restored.

- [ ] **Step 6: Commit**

```bash
gg add internal/tui/preview_open.go internal/tui/saved_pair_open.go internal/tui/saved_pair_notes.go internal/tui/model.go internal/tui/files_view.go internal/tui/review_view.go internal/tui/review_delete.go internal/tui/preview_reviews.go internal/tui/preview_reviews_test.go
git commit -F <msgfile>   # "feat(tui): an opened preview lists its reviews; esc from one returns to the preview"
```

---

### Task 6: Review view header names the preview and says "older tip"; steer navigate refreshes

**Files:**
- Modify: `internal/tui/review_view.go` (`reviewLabel`, `reviewViewMsg.older`, the read, the title)
- Modify: `internal/tui/review_hint.go` (`reviewHintMsg.preview`, reload sources, no commit back for a preview review)
- Modify: `internal/i18n/lang/*.toml` (`"%s · older tip"`)
- Test: `internal/tui/preview_reviews_test.go`

**Interfaces:**
- Consumes: Task 1's `svc.ReviewOlder(ctx, r)`; `domain.NoteScopeLabel(scope)`; `reloadSourcesCmd`, `chainPreviewsRead`.
- Produces: `reviewViewMsg.older bool`, `reviewViewState.older bool`; `reviewLabel(r)` returns `NoteScopeLabel(r.Preview)` for a preview review (P2-R2).

- [ ] **Step 1: Write the failing tests** (append)

```go
func TestReviewViewHeaderNamesThePreview(t *testing.T) {
	t.Parallel()
	if got := reviewLabel(domain.Review{Preview: "main...feat/x", Commit: strings.Repeat("a", 40)}); got != "feat/x → main" {
		t.Fatalf("reviewLabel = %q", got)
	}
	m, dir := storedPreviewReviewModel(t)
	id := m.previews[0].reviews[0].ID
	m, cmd := m.openReview(id, "x")
	m = drainUntil(t, m, cmd, func(m Model) bool { return m.filesReview != nil })
	if m.filesTitle != "Review: feat/x → main" {
		t.Fatalf("title %q", m.filesTitle)
	}
	runGit(t, dir, "checkout", "-q", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "more")
	runGit(t, dir, "checkout", "-q", "main")
	m, cmd = m.openReview(id, "x")
	m = drainUntil(t, m, cmd, func(m Model) bool { return m.filesReview != nil && m.filesReview.older })
	if m.filesTitle != "Review: feat/x → main · older tip" {
		t.Fatalf("title %q, want it to say older tip", m.filesTitle)
	}
}

// An agent saves a review out of band and navigates to it: the review opens,
// and the note counts and the Previews rows are re-read on the way, so its
// sub-row is there when the user looks.
func TestSteerToAFreshPreviewReviewReloadsTheRows(t *testing.T) {
	t.Parallel()
	m, _ := storedPreviewReviewModel(t)
	id := m.previews[0].reviews[0].ID
	m.previews[0].reviews = nil // the TUI has not seen it yet
	nm, cmd := m.onReviewHint(reviewHintMsg{svc: m.svc, cmd: steer.Command{Verb: "navigate", HintKind: "review", HintID: id},
		commit: strings.Repeat("0", 40), preview: "main...feat/x", found: true})
	if !nm.srcInflight[srcNotes] {
		t.Fatal("a review hint did not re-read the note counts")
	}
	nm = drainUntil(t, nm, cmd, func(m Model) bool { return len(m.previews) > 0 && len(m.previews[0].reviews) == 1 })
	if st := nm.filesReview; st == nil || st.back.Hash != "" {
		t.Fatalf("a preview review opened with a commit to return to: %+v", st)
	}
}
```

(Check the steer `Command` field names with `grep -n 'type Command struct' -A 20 internal/steer/*.go` and adjust the literal.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run 'TestReviewViewHeaderNamesThePreview|TestSteerToAFreshPreviewReview' -count=1`
Expected: FAIL — `reviewLabel` returns the short sha; `reviewHintMsg` has no field `preview`.

- [ ] **Step 3: Implement**

`review_view.go`:

```go
// reviewLabel is the preview a preview review belongs to (the Previews
// panel's "source → target", P2-R2), else the branch a review was of, else
// its commit's short sha.
func reviewLabel(r domain.Review) string {
	if r.Kind == domain.ReviewOnWorktree {
		return i18n.T("working changes")
	}
	if r.Preview != "" {
		return domain.NoteScopeLabel(r.Preview)
	}
	…unchanged…
}
```

- `reviewViewMsg` / `reviewViewState` gain `older bool`; in `openReviewWith`'s goroutine, after `ReviewRevs`: `out.older = svc.ReviewOlder(ctx, out.review)`; the handler copies it into the state.
- The title: `label := reviewLabel(msg.review); if msg.older { label = i18n.T("%s · older tip", label) }; m.filesTitle = i18n.T("Review: %s", label)`.

`review_hint.go`:
- `reviewHintMsg` gains `preview string`; the lookup fills `preview: r.Preview`.
- `onReviewHint`, found branch:

```go
	nm := m.steerToPanels()
	back := model.Commit{Hash: msg.commit}
	if msg.preview != "" {
		back = model.Commit{} // a preview's review returns to the panels, never a commit (R5)
	}
	nm, open := nm.openReviewFrom(c.HintID, reviewTitle(shortHash(msg.commit)), back)
	// An agent that just saved this review (gg review save, /gg-review) wrote
	// the store behind this process: re-read the counts — and the Previews
	// rows, a preview review's sub-row — so it shows on arrival (spec §6).
	var counts, rows tea.Cmd
	nm, counts = nm.reloadSourcesCmd([]sourceKey{srcNotes}, reloadOpts{})
	if msg.preview != "" {
		nm, rows = nm.chainPreviewsRead()
	}
	return nm, tea.Batch(open, counts, rows, nm.answerSteer(c, steerOK(c, "opened review "+c.HintID)))
```

i18n (all four): `"%s · older tip"`: ja `"%s · 古い先端"`, ko `"%s · 이전 끝 커밋"`, ru `"%s · старая вершина"`, zh `"%s · 旧的末端"`.

- [ ] **Step 4: Run them to verify they pass, plus the review-hint suite**

Run: `go test ./internal/tui -run 'ReviewView|ReviewHint|Steer|PreviewReview|I18n' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui/review_view.go internal/tui/review_hint.go internal/tui/preview_reviews_test.go internal/i18n/lang/*.toml
git commit -F <msgfile>   # "feat(tui): the review view names its preview, says older tip; a review link re-reads the rows"
```

---

### Task 7: Help, e2e golden screen, docs

**Files:**
- Modify: `internal/tui/help.go` (Previews panel rows)
- Modify: `internal/i18n/lang/*.toml` (the help strings)
- Create: `e2e/scenarios/tui_preview_review.toml` (+ `tui_preview_review.screens/` via `-update`)
- Modify: `CHANGELOG.md`, `README.md` (Previews TUI bullet), `docs/CLAUDE-details.md` (`## Preview reviews (2026-10-07)` — the TUI half)

**Interfaces:**
- Consumes: everything above.
- Produces: user-facing docs; a golden screen.

- [ ] **Step 1: Write the e2e scenario** (`e2e/scenarios/tui_preview_review.toml`)

```toml
name = "tui: a review saved on a merge preview shows as its sub-row and on top of the opened preview"

# feature changes a.txt. An agent stores a review document on the preview
# link (gg review save — what /gg-review does); the Previews tab lists it as
# a sub-row under the preview, and the opened preview lists it under Reviews
# above its files. The commit it reviewed shows nothing of it (R5).
[input]
steps = [
  { write = "a.txt", content = "a\n" },
  { commit = "add a" },
  { branch = "feature" }, { switch = "feature" },
  { write = "a.txt", content = "a\nmore a\n" },
  { commit = "grow a" },
  { switch = "main" },
]

[[run]]
cmd = ["preview", "add", "--label", "feature into main", "feature", "main"]
exit = 0

[[run]]
cmd   = ["review", "save", "gg://{{cwd}}@main...feature", "--agent", "Claude Code", "--stdin"]
stdin = '{"version":1,"summary":"## Summary\nfine","files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"why more a?"}]}]}'
exit  = 0

[tui]
size = "160x40"

[[tui.step]]
name = "previews"
keys = ["C-right", "C-right", "C-right"]
screen_contains = ["feature into main", "└ Review:", "Claude Code"]
screen_excludes = ["older tip"]

[[tui.step]]
name = "preview-reviews-block"
keys = ["enter"]
screen_contains = ["Reviews", "└ Review:", "a.txt"]

[[tui.step]]
name = "review-view"
keys = ["down", "enter"]
screen_contains = ["Review: feature → main", "Overview"]

[expect]
branch = "main"
```

(Verify the link grammar first: `bin/gg review save 'gg://<abs>@main...feature' --dry-run` in a scratch repo; `s92_preview_links.toml` uses `gg://{{cwd}}/a.txt@main...feat/x:4` for a file — the change link drops the path. Adjust the down-count to land on the review row if the cursor starts on the heading.)

- [ ] **Step 2: Run it to verify it fails, then record the goldens**

Run: `go test ./e2e -run 'TestScenarios/tui_preview_review' -count=1`
Expected: FAIL (no golden yet). Then `go test ./e2e -run 'TestScenarios/tui_preview_review' -count=1 -update`, read every file under `e2e/scenarios/tui_preview_review.screens/`, confirm the sub-row and the block are on screen, run `-update` a SECOND time and `git diff --stat` the screens dir — no change means the stamp is deterministic under the frozen clock. Re-run without `-update`: PASS. (Check the exact test name with `grep -n 'func Test' e2e/*_test.go`.)

- [ ] **Step 3: Help rows** (`help.go`, the "Previews panel" section, after the `enter` row "open the preview…")

```go
		r(".", i18n.T("Review (AI) (.-menu) on a merge preview or commit pair: an AI agent writes ONE review document — an overview, then remarks per file — stored with the preview (needs a review tool in Settings); /gg-review <gg link> in an agent session does the same. Show review opens the newest review of the current tip")),
		r("enter", i18n.T("a preview's reviews list as sub-rows under it (└ Review: date agent tally); one of a tip the preview has since moved past says older tip and still opens exactly what was reviewed, one whose commits are gone is hidden. enter opens the review view, . offers Open review / Copy gg link / Delete review; the opened preview lists them under Reviews above its files, and esc from a review returns to the preview")),
```

i18n: add both keys to all four bundles (translate faithfully; keep "Review (AI)", "Show review", "older tip", "/gg-review", "gg link", "└ Review:" as the same terms the bundles already use).

- [ ] **Step 4: Run the i18n gates and the help test**

Run: `go test ./internal/tui -run 'I18n|Help|Vocab|MenuLabels' -count=1 && go test ./internal/i18n -count=1`
Expected: PASS.

- [ ] **Step 5: Docs**
- `CHANGELOG.md` (top entry): "Preview reviews in the TUI: the Previews list shows a preview's (or commit pair's) AI reviews as sub-rows (older tip marked, gone hidden); `.` → Review (AI) / Show review on a preview row; the opened preview lists its reviews under Reviews; esc from one returns to the preview; the review view names the preview; a review link re-reads the rows."
- `README.md`: in the Previews / reviews section, one bullet for Review (AI) on a preview row and the sub-rows.
- `docs/CLAUDE-details.md`, `## Preview reviews (2026-10-07)`: a "TUI (plan 2)" paragraph — `previewRow.reviews` from `readPreviews` (`PreviewReviews` / `PreviewReviewsByScope`), `pvEntry` entry layer (backingIndex refuses sub-rows; Key = id / id\x00review), `filesPreviewReviews` + `previewReviewLines` (re-applied in place), `previewReturn` (esc back, one-shot), `ReviewOlder` for the header, the review-hint reload, P2-R1..R5.

- [ ] **Step 6: Full gate**

Run: `./test.sh race > /tmp/claude-1000/-work-gigagit/8d4423ea-2419-4d1a-bd9c-7cf69aaa99fc/scratchpad/race-p2.log 2>&1; tail -5 …/race-p2.log`
Expected: the log ends with "all green".

- [ ] **Step 7: Commit**

```bash
gg add internal/tui/help.go internal/i18n/lang/*.toml e2e/scenarios/tui_preview_review.toml e2e/scenarios/tui_preview_review.screens CHANGELOG.md README.md docs/CLAUDE-details.md
git commit -F <msgfile>   # "docs+e2e: preview reviews in the TUI"
```

- [ ] **Step 8: Manual check with the binary.** `./build.sh` in the worktree (`bin/gg`), a scratch repo with a preview and a `gg review save` on its link, `./tui-capture.sh` (driving-tui-headless skill): Previews tab shows the sub-row; `.` on the preview row lists Review (AI) and Show review; enter on the sub-row opens the review whose title names the preview; esc returns to the Previews tab.
