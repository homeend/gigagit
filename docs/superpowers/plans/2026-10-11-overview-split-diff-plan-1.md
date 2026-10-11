# Overview split: file / review diff — plan 1 (gg web) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In gg web, D (and a `file | diff` switch in the right column's title) flips a file opened from an overview's link between the file and the change the review covered, drawn as one column; the choice holds for the overview's next links; two links in a row that open identical files say so.

**Architecture:** One domain query `Service.ReviewFileDiff` (stored range + the review's file list + the cached Differ + remarks on both sides) behind a new `review=` lane of `/api/diff`. A new pure module `static/splitdiff.js` turns /api/diff rows into one-column rows and answers every row ↔ line question; `viewer.js` keeps `view` as the one file (`view.cur` stays a NEW line number everywhere) and adds `view.diff` (rows, the cursor's row, notice, fold state) plus a page-local `diffPref` keyed by the overview (`from.id`).

**Tech Stack:** Go 1.26 (domain, web handler, tests with real git), plain ES modules (no build step), node for the guarded pure-JS tests, Playwright (scratch) for the visible checks.

**Spec:** `docs/superpowers/specs/2026-10-11-overview-split-diff-design.md` (rulings D1–D6; this plan is §8 plus the shared §3–§7, §10, §12).

## Global Constraints

- Notice texts, verbatim: `no change to this file in the review — D shows the file`, `no changes in the working tree — D shows the file`, `binary file — D shows the file`, `diff too large — D shows the file`, `the review is gone — D shows the file`; also `could not read the diff — D shows the file` (any other failed read).
- Status texts, verbatim: `same content as <path>` (path cut in the middle), `a deleted line has no link in the reviewed version`, `review remarks are read-only` (existing).
- Title: file `View <path> (<version>)` (today's); diff `Diff <path> (<left> → <right>)`, a rename `Diff <old> → <path> (…)`, a working tree diff `(HEAD → working tree)`. Paths cut in the MIDDLE (`elidePath`), never at the end.
- Rows: unchanged = new number, blank sign; deleted = OLD number dimmed, `−` (U+2212), deleted tint `var(--del)`; added = new number, `+`, `var(--add)`; changed words `mark.l` / `mark.r` (the diff view's).
- D acts only on a file an overview's link opened (`view.from` set, not an overview on screen); in the split from EITHER column. f in a diff toggles changed-lines-only; in file mode f keeps today's behaviour (falls through to the page).
- The choice (D and f) is held per overview (`diffPref.key === from.id`), survives stepping links, the narrow viewer's Backspace → Enter, `narrowSplit`, `backIntoSplit`; ends on `dropViewer` (every close), on a plain-file open (no `from`), and when the split's overview leaves the store.
- W5 / R2 hold: nothing in this plan moves the overview's selection from the right column.
- A new element with `class="hidden"` needs its own `#id.hidden { display: none; }` rule (gg web hides by id).
- Advertise in BOTH the footer and the `?` help.
- No implementer subagents; commit with `gg add <paths>` + `git commit -F <msgfile>` (never `git add -A`; `bin/` holds the verify binary); commit messages end with the session's attribution lines.
- The skill text (gg-overview) is updated in plan 2, once both frontends have the feature (spec §12) — not here.

## Review Focus

- A diff whose cursor sits on a DELETED row: every reader of `view.cur` (bands, the overview mark, `reportCursor`, L, the `.` menu, d / r) must act on the new line below it or refuse — never on a stale line. (Task 5 tests `onDeletedRow` paths in the wiring guard; Task 7 checks L visibly.)
- A file the review DELETED (file view = placeholder, diff = every row deleted, `view.cur` = 0): D must still flip, and the cursor, n / p, marking must not throw. (Task 3's node test pins `rowOfLine` / `rowNewLine` / `rowBand` on an all-deleted list; `canFlip` does not look at the placeholder, and the diff branch of `renderViewer` comes before the placeholder's.)
- Quick D presses and quick link steps while a diff read is out: a late answer must never paint over a newer file or a flipped-back choice. (Task 4: the `seq`/`id`/`from`/`diffWanted()` guard in `flipDiff` and `refreshDiff`, pinned by the wiring guard; Task 7 presses D twice fast.)
- f with the linked band or a note box in a folded run: those rows (and the cursor's) never fold. (Task 3 pins `rowBand` for deleted rows; Task 7 checks the banded rows stay visible under f.)
- A remark on a line the review deleted: absent in the file view (W6), present under its deleted row in the diff. (Task 2 server test + Task 7 visible check.)

---

### Task 1: Domain — `Service.ReviewFileDiff`

**Files:**
- Create: `internal/domain/review_file_diff.go`
- Test: `internal/domain/review_file_diff_test.go`

**Interfaces:**
- Consumes: `Service.Review`, `Service.reviewRevs`, `Service.ReviewFiles`, `Service.ShowFile`, `Service.ResolveBytes`, `Service.ResolveRev`, `Service.Differ`, `Service.ReviewNotesFor`, `reviewPath` (all existing).
- Produces:
  ```go
  type ReviewFileDiff struct {
      Diff            Diff
      Changed         bool
      Status, OldPath string
      Left, Right     string
      Notes           []ResolvedNote
  }
  func (s *Service) ReviewFileDiff(ctx context.Context, reviewID, path string) (ReviewFileDiff, error)
  ```

- [ ] **Step 1: Write the failing tests**

`internal/domain/review_file_diff_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// rowKinds is a diff's rows as "kind:left>right" — enough to read a change.
func rowKinds(d Diff) string {
	var parts []string
	for _, r := range d.Result.Rows {
		parts = append(parts, map[textdiff.Kind]string{textdiff.Same: "same", textdiff.Changed: "change", textdiff.Del: "del", textdiff.Add: "add"}[r.Kind]+":"+r.Left+">"+r.Right)
	}
	return strings.Join(parts, ",")
}

func saveReviewOn(t *testing.T, svc *Service, rng, doc string) string {
	t.Helper()
	id, _, err := svc.SaveReview(context.Background(), SaveReview{Target: ReviewTarget{Kind: ReviewRange, Range: rng, Label: "r"}, Agent: "Claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// Spec 2026-10-11-overview-split-diff §3: a branch review's diff is its
// stored range — the trunk's tip against the branch tip — and a file the
// range leaves alone is unchanged, with no diff read.
func TestReviewFileDiffOfABranchReviewIsTheRangeChange(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	commitFile(t, dir, "g.txt", "one\n", "add g")
	ctx := context.Background()
	tg, err := svc.BranchReviewTarget(ctx, "feature")
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Text: `{"version":1,"summary":"s","files":[]}`})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.ReviewFileDiff(ctx, id, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Changed || got.Status != "A" || rowKinds(got.Diff) != "add:>x" {
		t.Fatalf("f.txt = %+v rows %q, want an added x", got, rowKinds(got.Diff))
	}
	if main, tip := revParse(t, dir, "main"), revParse(t, dir, "feature"); got.Left != main[:7] || got.Right != tip[:7] {
		t.Fatalf("labels %q → %q, want %q → %q", got.Left, got.Right, main[:7], tip[:7])
	}
	same, err := svc.ReviewFileDiff(ctx, id, "README.md")
	if err != nil || same.Changed || len(same.Diff.Result.Rows) != 0 || same.Right == "" {
		t.Fatalf("README.md = %+v, %v; want unchanged, no rows, labels kept", same, err)
	}
}

// A single-commit review diffs the commit's parent against it, and the
// review's remarks come on BOTH sides (an old-side one on the deleted line).
func TestReviewFileDiffOfACommitCarriesBothSidesRemarks(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	commitFile(t, dir, "g.txt", "one\ntwo\nthree\n", "add g")
	commitFile(t, dir, "g.txt", "one\nTWO\nthree\nfour\n", "edit g")
	tip := revParse(t, dir, "HEAD")
	doc := `{"version":1,"summary":"s","files":[{"path":"g.txt","annotations":[` +
		`{"oldRange":[2,2],"summary":"old two"},{"newRange":[4,4],"summary":"new four"}]}]}`
	id := saveReviewOn(t, svc, tip+"^.."+tip, doc)
	got, err := svc.ReviewFileDiff(context.Background(), id, "g.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "M" || rowKinds(got.Diff) != "same:one>one,change:two>TWO,same:three>three,add:>four" {
		t.Fatalf("g.txt %q rows %q", got.Status, rowKinds(got.Diff))
	}
	if parent := revParse(t, dir, "HEAD^"); got.Left != parent[:7] || got.Right != tip[:7] {
		t.Fatalf("labels %q → %q", got.Left, got.Right)
	}
	sides := map[string]string{}
	for _, n := range got.Notes {
		sides[n.Note.Summary] = string(n.Note.Side)
	}
	if len(got.Notes) != 2 || sides["old two"] != string(model.NoteSideOld) || sides["new four"] != string(model.NoteSideNew) {
		t.Fatalf("notes %+v", got.Notes)
	}
}

// A root commit's review has no parent: every line is added, the left side
// labelled "empty".
func TestReviewFileDiffOfARootCommit(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	root := revParse(t, dir, "main")
	id := saveReviewOn(t, svc, root, `{"version":1,"summary":"s","files":[]}`)
	got, err := svc.ReviewFileDiff(context.Background(), id, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "A" || got.Left != "empty" || rowKinds(got.Diff) != "add:>hello" {
		t.Fatalf("root = %+v rows %q", got, rowKinds(got.Diff))
	}
}

// A working review diffs HEAD against the file on disk.
func TestReviewFileDiffOfAWorkingReview(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	files := []model.NoteFile{{Path: "f.txt", Blob: strings.Repeat("a", 40)}}
	id, _, err := svc.SaveReview(ctx, SaveReview{Target: WorkingReviewTarget(), Agent: "Claude", Text: "x", Files: files})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.ReviewFileDiff(ctx, id, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Left != "HEAD" || got.Right != "working tree" || rowKinds(got.Diff) != "change:x>changed" {
		t.Fatalf("working = %+v rows %q", got, rowKinds(got.Diff))
	}
}

// A renamed file diffs against its old path; a deleted one shows every
// line deleted.
func TestReviewFileDiffFollowsRenamesAndDeletes(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	runGitIn(t, dir, "mv", "f.txt", "h.txt")
	runGitIn(t, dir, "commit", "-qm", "rename")
	tip := revParse(t, dir, "HEAD")
	id := saveReviewOn(t, svc, tip+"^.."+tip, `{"version":1,"summary":"s","files":[]}`)
	got, err := svc.ReviewFileDiff(context.Background(), id, "h.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "R" || got.OldPath != "f.txt" || rowKinds(got.Diff) != "same:x>x" {
		t.Fatalf("rename = %+v rows %q", got, rowKinds(got.Diff))
	}
	runGitIn(t, dir, "rm", "-q", "h.txt")
	runGitIn(t, dir, "commit", "-qm", "delete")
	tip = revParse(t, dir, "HEAD")
	id = saveReviewOn(t, svc, tip+"^.."+tip, `{"version":1,"summary":"s","files":[]}`)
	got, err = svc.ReviewFileDiff(context.Background(), id, "h.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "D" || rowKinds(got.Diff) != "del:x>" {
		t.Fatalf("delete = %+v rows %q", got, rowKinds(got.Diff))
	}
}

func TestReviewFileDiffOfAGoneReview(t *testing.T) {
	t.Parallel()
	_, svc, _ := reviewRepo(t)
	if _, err := svc.ReviewFileDiff(context.Background(), "nope", "f.txt"); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("err = %v, want ErrReviewNotFound", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/overview-split-diff && go test ./internal/domain -run 'TestReviewFileDiff' 2>&1 | tail -5`
Expected: FAIL — `svc.ReviewFileDiff undefined` (build error).

- [ ] **Step 3: Write the implementation**

`internal/domain/review_file_diff.go`:

```go
package domain

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// ReviewFileDiff is one file of a review as the change the review covered:
// what the overview split's right column shows when D flips it (spec
// 2026-10-11-overview-split-diff §3).
type ReviewFileDiff struct {
	Diff Diff
	// Changed is false when the review's range leaves the file alone: no
	// diff was read and Diff is empty.
	Changed bool
	// Status is the file's letter in the review (A M D R …), OldPath a
	// rename's or copy's old path ("" otherwise).
	Status, OldPath string
	// Left and Right label the two sides: short hashes ("empty" for a root
	// commit's missing parent), or HEAD / working tree for a working review.
	Left, Right string
	// Notes are the review's remarks on both sides of Diff (an old-side one
	// on the line the review deleted); none for a binary or too-large diff.
	Notes []ResolvedNote
}

// ReviewFileDiff reads path's change in review reviewID: the left end of
// the review's STORED range (a single commit's parent) against its tip, or
// for a working review HEAD against the file on disk. The sides are
// reviewRevs', the status and old path ReviewFiles', so the diff is the one
// the review view lists for the file. ErrReviewNotFound when it is gone.
func (s *Service) ReviewFileDiff(ctx context.Context, reviewID, path string) (ReviewFileDiff, error) {
	r, err := s.Review(ctx, reviewID)
	if err != nil {
		return ReviewFileDiff{}, err
	}
	path = reviewPath(path)
	base, tip, _ := s.reviewRevs(ctx, r)
	working := r.Kind == ReviewOnWorktree
	out := ReviewFileDiff{Left: "HEAD", Right: "working tree"}
	if !working {
		out.Left, out.Right = "empty", reviewShort(tip)
		if h, ok, err := s.ResolveRev(ctx, base); err == nil && ok {
			base = strings.TrimSpace(h)
			out.Left = reviewShort(base)
		}
	}
	files, err := s.ReviewFiles(ctx, r)
	if err != nil {
		return ReviewFileDiff{}, err
	}
	var f *model.CommitFile
	for i := range files {
		if reviewPath(files[i].Path) == path {
			f = &files[i]
			break
		}
	}
	if f == nil {
		return out, nil
	}
	out.Changed, out.Status, out.OldPath = true, f.Status, f.OldPath
	oldPath := path
	if f.OldPath != "" {
		oldPath = reviewPath(f.OldPath)
	}
	var oldSrc, newSrc ByteSource
	if f.Status != "A" && out.Left != "empty" {
		oldSrc = absentOnError(func(ctx context.Context) ([]byte, error) { return s.ShowFile(ctx, base, oldPath) })
	}
	if f.Status != "D" {
		if working {
			newSrc = absentOnError(func(ctx context.Context) ([]byte, error) {
				return s.ResolveBytes(ctx, model.FileRef{Source: model.SourceUnstaged, Path: path})
			})
		} else {
			newSrc = absentOnError(func(ctx context.Context) ([]byte, error) { return s.ShowFile(ctx, tip, path) })
		}
	}
	key, rel := "", "" // the working tree moves under one key: never cached
	if !working {
		key = "review-diff:" + base + ".." + tip + ":" + oldPath + ">" + path
	}
	if oldPath != path {
		rel = oldPath
	}
	d, err := s.Differ().Diff(ctx, Request{Key: key, Path: path, OldPath: rel, Old: oldSrc, New: newSrc})
	if err != nil {
		return ReviewFileDiff{}, err
	}
	out.Diff = d
	if !d.Binary && !d.TooLarge {
		out.Notes, _ = s.ReviewNotesFor(ctx, reviewID, path, d)
	}
	return out, nil
}

// absentOnError reads a side that may not exist (an untracked file has no
// HEAD version, a listed file may be gone from disk) as absent instead of
// failing the diff; a cancelled read stays an error.
func absentOnError(read ByteSource) ByteSource {
	return func(ctx context.Context) ([]byte, error) {
		b, err := read(ctx)
		if err != nil && ctx.Err() == nil {
			return nil, nil
		}
		return b, err
	}
}

// reviewShort is a hash's seven-character form.
func reviewShort(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/overview-split-diff && go test ./internal/domain -run 'TestReviewFileDiff' -count=1 2>&1 | tail -5`
Expected: `ok  github.com/homeend/gigagit/internal/domain`

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/overview-split-diff
gg add internal/domain/review_file_diff.go internal/domain/review_file_diff_test.go
git commit -F $CLAUDE_JOB_DIR/tmp/msg.txt   # "domain: ReviewFileDiff — one file of a review as the change it covered" + attribution
```

---

### Task 2: Web server — the `review=` lane of `/api/diff`

**Files:**
- Create: `internal/web/reviewdiff.go`, `internal/web/reviewdiff_test.go`
- Modify: `internal/web/diff.go` (`handleDiff`, ~line 163), `internal/web/filecontent.go` (`noteRow`, `remarksFor`)

**Interfaces:**
- Consumes: `Service.ReviewFileDiff` (Task 1), `diffPayload`, `writeJSON`, `writeErr`, `isGitArgSafe`.
- Produces: `GET /api/diff?review=<id>&path=<p>` → /api/diff's body plus `changed` (bool), `status`, `old_path`, `left_label`, `right_label`, `remarks` ([]noteRow, `side` = `"old"` on an old-side remark). 404 for a gone review, 400 for a missing or option-like path. `noteRow.Side string json:"side,omitempty"`; `func remarkRows(ns []domain.ResolvedNote) []noteRow`.

- [ ] **Step 1: Write the failing tests**

`internal/web/reviewdiff_test.go`:

```go
package web

import (
	"net/http"
	"net/url"
	"testing"
)

type reviewDiffWire struct {
	Rows []struct {
		Kind  string `json:"kind"`
		Left  string `json:"left"`
		Right string `json:"right"`
	} `json:"rows"`
	Changed    bool   `json:"changed"`
	Status     string `json:"status"`
	LeftLabel  string `json:"left_label"`
	RightLabel string `json:"right_label"`
	Remarks    []struct {
		remarkWire
		Side string `json:"side"`
	} `json:"remarks"`
}

// Spec 2026-10-11-overview-split-diff §8: the review lane answers the
// review's change to one file, its side labels and the remarks on BOTH
// sides — the one on the deleted line marked old.
func TestReviewDiffLaneAnswersTheChangeAndBothSidesRemarks(t *testing.T) {
	t.Parallel()
	doc := `{"version":1,"summary":"s","files":[{"path":"f.txt","annotations":[` +
		`{"newRange":[1,1],"summary":"kept"},{"oldRange":[1,1],"summary":"on a deleted line"}]}]}`
	ts, _, sha, id := reviewServer(t, doc)
	var got reviewDiffWire
	q := "/api/diff?review=" + url.QueryEscape(id) + "&path=f.txt"
	if code := getJSON(t, ts, q, &got); code != http.StatusOK {
		t.Fatalf("GET %s = %d", q, code)
	}
	if !got.Changed || got.Status != "M" || len(got.Rows) != 1 || got.Rows[0].Kind != "change" || got.Rows[0].Left != "content 1" || got.Rows[0].Right != "content 2" {
		t.Fatalf("diff = %+v", got)
	}
	if got.RightLabel != sha[:7] || len(got.LeftLabel) != 7 || got.LeftLabel == got.RightLabel {
		t.Fatalf("labels %q → %q", got.LeftLabel, got.RightLabel)
	}
	sides := map[string]string{}
	for _, r := range got.Remarks {
		if !r.ReadOnly {
			t.Errorf("remark %+v is not read-only", r)
		}
		sides[r.Summary] = r.Side
	}
	if len(got.Remarks) != 2 || sides["kept"] != "" || sides["on a deleted line"] != "old" {
		t.Fatalf("remarks = %+v", got.Remarks)
	}
}

// A file the review leaves alone answers changed=false and no rows; a gone
// review is a 404 (the page says "the review is gone"); a bad path a 400.
func TestReviewDiffLaneRefusals(t *testing.T) {
	t.Parallel()
	ts, _, _, id := reviewServer(t, `{"version":1,"summary":"s","files":[]}`)
	var got reviewDiffWire
	if code := getJSON(t, ts, "/api/diff?review="+url.QueryEscape(id)+"&path=other.txt", &got); code != http.StatusOK || got.Changed || len(got.Rows) != 0 {
		t.Fatalf("unchanged file = %d %+v", code, got)
	}
	for q, want := range map[string]int{
		"/api/diff?review=nope&path=f.txt":                   http.StatusNotFound,
		"/api/diff?review=" + url.QueryEscape(id) + "&path=-x": http.StatusBadRequest,
		"/api/diff?review=" + url.QueryEscape(id):            http.StatusBadRequest,
	} {
		if code := getJSON(t, ts, q, nil); code != want {
			t.Errorf("GET %s = %d, want %d", q, code, want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/overview-split-diff && go test ./internal/web -run 'TestReviewDiffLane' -count=1 2>&1 | tail -8`
Expected: FAIL — the lane does not exist: `GET /api/diff?review=… = 400` ("sha and path are required").

- [ ] **Step 3: Write the implementation**

In `internal/web/filecontent.go` add `Side` to `noteRow` (after `ReadOnly`):

```go
	Side      string `json:"side,omitempty"`     // "old": a remark on a line the review deleted (the diff lays it there)
```

and make `remarksFor` build its rows through `remarkRows` (its body after the `ReviewFileNotes` call becomes):

```go
	if err != nil || len(ns) == 0 {
		return nil
	}
	return remarkRows(ns)
```

`internal/web/reviewdiff.go`:

```go
package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// handleReviewDiff serves /api/diff?review=<id>&path=<p>: one file of a
// review as the change the review covered (spec
// 2026-10-11-overview-split-diff §8) — /api/diff's rows plus whether the
// review changed the file, its status and old path, the sides' labels and
// the review's remarks on both sides (read-only).
func (s *Server) handleReviewDiff(w http.ResponseWriter, r *http.Request, id string) {
	path := r.URL.Query().Get("path")
	if !isGitArgSafe(path) {
		writeErr(w, http.StatusBadRequest, errors.New("a valid path is required"))
		return
	}
	rd, err := s.service().ReviewFileDiff(r.Context(), id, path)
	if errors.Is(err, domain.ErrReviewNotFound) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	p := diffPayload(rd.Diff, nil)
	p["changed"] = rd.Changed
	p["status"] = rd.Status
	p["old_path"] = rd.OldPath
	p["left_label"] = rd.Left
	p["right_label"] = rd.Right
	p["remarks"] = remarkRows(rd.Notes)
	writeJSON(w, p)
}

// remarkRows are a review's remarks on the wire: read-only boxes, an
// old-side one marked so the page lays it under the deleted line.
func remarkRows(ns []domain.ResolvedNote) []noteRow {
	out := make([]noteRow, 0, len(ns))
	for _, n := range ns {
		row := noteRow{ID: n.Note.ID, Start: n.Range[0], End: max(n.Range[1], n.Range[0]), Summary: n.Note.Summary,
			Rationale: n.Note.Rationale, Author: n.Note.Author, ReadOnly: true}
		if n.Note.Side == model.NoteSideOld {
			row.Side = "old"
		}
		out = append(out, row)
	}
	return out
}
```

In `internal/web/diff.go` `handleDiff`, first thing after `q := r.URL.Query()`:

```go
	if id := q.Get("review"); id != "" { // a review's change to one file (the overview split's diff)
		s.handleReviewDiff(w, r, id)
		return
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/overview-split-diff && go test ./internal/web -run 'TestReviewDiffLane|TestFileContent' -count=1 2>&1 | tail -5`
Expected: `ok  github.com/homeend/gigagit/internal/web`

- [ ] **Step 5: Commit**

```bash
gg add internal/web/reviewdiff.go internal/web/reviewdiff_test.go internal/web/diff.go internal/web/filecontent.go
git commit -F $CLAUDE_JOB_DIR/tmp/msg.txt   # "web: /api/diff review= lane — a review's change to one file with both sides' remarks"
```

---

### Task 3: The pure split diff model (`static/splitdiff.js`)

**Files:**
- Create: `internal/web/static/splitdiff.js`, `internal/web/splitdiffjs_test.go`

**Interfaces:**
- Consumes: nothing (pure, no imports).
- Produces (exported): a row = `{i, kind: "same"|"add"|"del", old, nu, text, tok, spans}`;
  `unifiedRows(d) → rows`; `lineRows(rows) → by` (`by[n]` = row of new line n);
  `rowOfLine(rows, by, line) → int` (-1 no rows, 0 when the line has none);
  `rowNewLine(rows, i) → int` (a deleted row: the next new line, else the last above, 0 none);
  `rowBand(rows, i, bands, cur) → "cur"|"other"|""`;
  `placeNotes(rows, notes) → [{n, first, last}]` (sorted by first; `n.side === "old"` places on old numbers);
  `nextNoteRow(placed, row, dir) → int` (-1 none); `noteAtRow(placed, row) → note|null`;
  `diffSource(from, path) → url|""`; `diffNotice(d, status, agent) → string`;
  `diffHead(d, path, agent) → {name, range}`; `sameContent(prev, cur) → bool` with `{key, path, lines: string[]}`.

- [ ] **Step 1: Write the failing test**

`internal/web/splitdiffjs_test.go`:

```go
package web

import "testing"

const sdPureStart = "// --- split diff model (pure; guarded against Go) ---"
const sdPureEnd = "// --- end split diff model ---"

// Spec 2026-10-11-overview-split-diff §4–§7: /api/diff's aligned pairs read
// as one column (a run's deleted lines, then its added ones), every row ↔
// new line question, bands on deleted rows, boxes on both sides, the
// notices, the title and the same-content rule.
func TestSplitDiffModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "splitdiff.js", sdPureStart, sdPureEnd, `
const d = { rows: [
  { kind: "same", left: "a", right: "a", left_no: 1, right_no: 1 },
  { kind: "change", left: "b", right: "B", left_no: 2, right_no: 2, left_spans: [[0, 1]], right_spans: [[0, 1]] },
  { kind: "del", left: "c", right: "", left_no: 3, right_no: 0 },
  { kind: "same", left: "d", right: "d", left_no: 4, right_no: 3 },
  { kind: "add", left: "", right: "e", left_no: 0, right_no: 4 },
] };
const rows = unifiedRows(d), by = lineRows(rows);
const tail = unifiedRows({ rows: [{ kind: "same", left: "a", right: "a", left_no: 1, right_no: 1 }, { kind: "del", left: "z", left_no: 2, right_no: 0 }] });
const lead = unifiedRows({ rows: [{ kind: "del", left: "x", left_no: 1, right_no: 0 }, { kind: "same", left: "a", right: "a", left_no: 2, right_no: 1 }] });
const gone = unifiedRows({ rows: [{ kind: "del", left: "x", left_no: 1, right_no: 0 }, { kind: "del", left: "y", left_no: 2, right_no: 0 }] });
const bands = [{ start: 1, end: 2, i: 0 }, { start: 3, end: 3, i: 1 }];
const placed = placeNotes(rows, [{ id: "n1", start: 2, end: 3 }, { id: "r1", side: "old", start: 3, end: 3 }, { id: "r2", side: "old", start: 9, end: 9 }, { id: "r3", start: 1, end: 1 }]);
console.log([
  rows.map((r) => r.kind + ":" + r.old + ":" + r.nu + ":" + r.text).join(","),
  JSON.stringify(rows[1].spans) + JSON.stringify(rows[2].spans) + JSON.stringify(rows[3].spans),
  [by[1], by[2], by[3], by[4]].join(","),
  rowOfLine(rows, by, 2), rowOfLine(rows, by, 9), rowOfLine([], [], 1),
  rowNewLine(rows, 1), rowNewLine(rows, 2), rowNewLine(rows, 4), rowNewLine(tail, 1), rowNewLine(rows, 99),
  rows.map((r) => rowBand(rows, r.i, bands, 1)).join(","),
  rowBand(rows, 1, bands, 0), rowBand(lead, 0, [{ start: 1, end: 1, i: 0 }], 0) || "none",
  placed.map((p) => p.n.id + ":" + p.first + "-" + p.last).join(","),
  nextNoteRow(placed, 0, 1), nextNoteRow(placed, 3, 1), nextNoteRow(placed, 3, -1), nextNoteRow(placed, 0, -1),
  noteAtRow(placed, 4).id, noteAtRow(placed, 1),
  rowOfLine(gone, lineRows(gone), 0), rowNewLine(gone, 1), rowBand(gone, 0, bands, 0) || "none",
].join("|"));
console.log([
  diffSource({ stored: { id: "review-overview:abc" } }, "a b.txt"), diffSource({ id: "f3" }, "x.go"),
  diffSource(null, "x") || "none", diffSource({ stored: { id: "" } }, "x") || "none",
].join("|"));
console.log([
  diffNotice(d, 0, false) || "rows",
  diffNotice({ rows: [{ kind: "same" }] }, 0, true),
  diffNotice({ changed: false, rows: [] }, 0, false),
  diffNotice(null, 404, false), diffNotice(null, 500, true),
  diffNotice({ binary: true }, 0, false), diffNotice({ too_large: true }, 0, false),
].join("|"));
const h1 = diffHead({ left_label: "3a4d954", right_label: "774ace5", old_path: "old.txt" }, "new.txt", false), h2 = diffHead({}, "a.txt", true);
const s = (key, path, lines) => ({ key, path, lines });
console.log([
  h1.name, h1.range, h2.name, h2.range,
  sameContent(s("o", "a", ["x"]), s("o", "b", ["x"])), sameContent(s("o", "a", ["x"]), s("o", "a", ["x"])),
  sameContent(s("o", "a", ["x"]), s("p", "b", ["x"])), sameContent(s("o", "a", []), s("o", "b", [])),
  sameContent(s("o", "a", ["x"]), s("o", "b", ["y"])), sameContent(null, s("o", "b", ["x"])),
].join("|"));
`)
	want := "same:1:1:a,del:2:0:b,del:3:0:c,add:0:2:B,same:4:3:d,add:0:4:e|[[0,1]]null[[0,1]]|0,3,4,5|3|0|-1|2|2|3|1|0|other,other,other,other,cur,|cur|none|r3:0-0,r1:2-2,n1:3-4|2|-1|2|-1|n1||0|0|none\n" +
		"/api/diff?review=abc&path=a+b.txt|/api/diff?wt=head&path=x.go|none|none\n" +
		"rows|no changes in the working tree — D shows the file|no change to this file in the review — D shows the file|the review is gone — D shows the file|could not read the diff — D shows the file|binary file — D shows the file|diff too large — D shows the file\n" +
		"old.txt → new.txt|3a4d954 → 774ace5|a.txt|HEAD → working tree|true|false|false|false|false|false"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/overview-split-diff && go test ./internal/web -run TestSplitDiffModelJS -count=1 2>&1 | tail -5`
Expected: FAIL — `open static/splitdiff.js: no such file or directory`.

- [ ] **Step 3: Write the module**

`internal/web/static/splitdiff.js`:

```js
// splitdiff.js — the overview split's right column as the review's diff
// (spec 2026-10-11-overview-split-diff): /api/diff's aligned pairs read as
// ONE column, and every row ↔ line question the viewer asks of it. The
// viewer keeps view.cur a NEW line number everywhere; these helpers map it.

// --- split diff model (pure; guarded against Go) ---
// A row: {i, kind: "same" | "add" | "del", old, nu, text, tok, spans} — old
// and nu the line's number on each side (0 = none), i its index.

// unifiedRows reads /api/diff's rows as one column: a run of changed rows
// gives its deleted lines first, then its added ones (a unified diff's
// order); an unchanged row is its new line. Changed words keep their spans.
function unifiedRows(d) {
  const src = (d && d.rows) || [], out = [];
  const push = (kind, old, nu, text, tok, spans) =>
    out.push({ i: out.length, kind, old: old || 0, nu: nu || 0, text: text || "", tok: tok || null, spans: spans && spans.length ? spans : null });
  for (let k = 0; k < src.length; ) {
    const r = src[k];
    if (r.kind === "same") {
      push("same", r.left_no, r.right_no, r.right, r.right_tok, null);
      k++;
      continue;
    }
    let j = k;
    while (j < src.length && src[j].kind !== "same") j++;
    for (let x = k; x < j; x++) if (src[x].kind !== "add") push("del", src[x].left_no, 0, src[x].left, src[x].left_tok, src[x].left_spans);
    for (let x = k; x < j; x++) if (src[x].kind !== "del") push("add", 0, src[x].right_no, src[x].right, src[x].right_tok, src[x].right_spans);
    k = j;
  }
  return out;
}

// lineRows maps each new line to its row: lineRows(rows)[n] = the row of
// new line n (undefined = none).
function lineRows(rows) {
  const by = [];
  for (const r of rows) if (r.nu) by[r.nu] = r.i;
  return by;
}

// rowOfLine is new line's row; the first row when the line has none (0, or
// past the end), -1 with no rows at all.
function rowOfLine(rows, by, line) {
  if (!rows.length) return -1;
  const r = by[line];
  return r === undefined ? 0 : r;
}

// rowNewLine is the new line row i stands for: its own; a deleted row's,
// the next new line below it, else the last one above (0 = none).
function rowNewLine(rows, i) {
  if (!rows[i]) return 0;
  for (let k = i; k < rows.length; k++) if (rows[k].nu) return rows[k].nu;
  for (let k = i - 1; k >= 0; k--) if (rows[k].nu) return rows[k].nu;
  return 0;
}

// rowBand is row i's band kind — "cur" (the current band wins an overlap),
// "other" or "" — over bands as the viewer's anchorBands makes them (new
// lines), cur the current band's index. A deleted row is in a band when the
// band holds the new lines on both sides of it.
function rowBand(rows, i, bands, cur) {
  const r = rows[i];
  if (!r || !bands.length) return "";
  const within = (b, n) => b.start <= n && n <= b.end;
  let test;
  if (r.nu) test = (b) => within(b, r.nu);
  else {
    let p = 0, q = 0;
    for (let k = i - 1; k >= 0 && !p; k--) p = rows[k].nu;
    for (let k = i + 1; k < rows.length && !q; k++) q = rows[k].nu;
    if (!p || !q) return "";
    test = (b) => within(b, p) && within(b, q);
  }
  let kind = "";
  for (let x = 0; x < bands.length; x++) {
    if (!test(bands[x])) continue;
    if (x === cur) return "cur";
    kind = "other";
  }
  return kind;
}

// placeNotes lays boxes on rows: a note covers the rows whose number on its
// side (side "old" = the old numbers, anything else the new) is in its
// lines, first..last; one with no such row is left out. Sorted by first
// row, the given order kept on a tie.
function placeNotes(rows, notes) {
  const out = [];
  for (const n of notes || []) {
    const old = n.side === "old";
    let first = -1, last = -1;
    for (const r of rows) {
      const no = old ? r.old : r.nu;
      if (!no || no < n.start || no > n.end) continue;
      if (first < 0) first = r.i;
      last = r.i;
    }
    if (first >= 0) out.push({ n, first, last });
  }
  return out.sort((a, b) => a.first - b.first);
}

// nextNoteRow is the first row of the next (dir > 0) or previous note
// strictly past row; -1 = none.
function nextNoteRow(placed, row, dir) {
  let hit = -1;
  for (const p of placed) {
    if (dir > 0 && p.first > row) return p.first;
    if (dir < 0 && p.first < row) hit = p.first;
  }
  return hit;
}

// noteAtRow is the first placed note covering row, or null.
function noteAtRow(placed, row) {
  const p = placed.find((x) => x.first <= row && row <= x.last);
  return p ? p.n : null;
}

// diffSource is the url a file's diff is read from (§3): a stored review
// overview's link through the review lane, an agent's HEAD → the working
// tree; "" = none.
function diffSource(from, path) {
  if (!from || !path) return "";
  if (from.stored) {
    const id = String(from.stored.id || "").replace(/^review-overview:/, "");
    return id ? "/api/diff?" + new URLSearchParams({ review: id, path }) : "";
  }
  return "/api/diff?" + new URLSearchParams({ wt: "head", path });
}

const D_SHOWS = " — D shows the file";

// diffNotice is what the column says instead of rows (§4); "" = draw the
// rows. status is a failed read's HTTP status (0 = it did not fail).
function diffNotice(d, status, agent) {
  if (status === 404 && !agent) return "the review is gone" + D_SHOWS;
  if (status) return "could not read the diff" + D_SHOWS;
  if (!d) return "no diff for this file" + D_SHOWS;
  if (d.binary) return "binary file" + D_SHOWS;
  if (d.too_large) return "diff too large" + D_SHOWS;
  if (d.changed === false || !(d.rows || []).some((r) => r.kind !== "same"))
    return (agent ? "no changes in the working tree" : "no change to this file in the review") + D_SHOWS;
  return "";
}

// diffHead is the diff title's parts: the shown name (a rename's old path
// first) and the "left → right" range.
function diffHead(d, path, agent) {
  const left = agent ? "HEAD" : (d && d.left_label) || "?";
  const right = agent ? "working tree" : (d && d.right_label) || "?";
  const old = d && d.old_path && d.old_path !== path ? d.old_path + " → " : "";
  return { name: old + path, range: left + " → " + right };
}

// sameContent: the file a link step opened ({key, path, lines}) holds the
// very text the previous step's did — the same overview, another path,
// something to read (§7).
function sameContent(prev, cur) {
  if (!prev || !cur || prev.key !== cur.key || prev.path === cur.path) return false;
  if (!cur.lines.length || prev.lines.length !== cur.lines.length) return false;
  for (let i = 0; i < cur.lines.length; i++) if (prev.lines[i] !== cur.lines[i]) return false;
  return true;
}
// --- end split diff model ---

export { diffHead, diffNotice, diffSource, lineRows, nextNoteRow, noteAtRow, placeNotes, rowBand, rowNewLine, rowOfLine, sameContent, unifiedRows };
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /work/gigagit/.claude/worktrees/overview-split-diff && go test ./internal/web -run TestSplitDiffModelJS -count=1 2>&1 | tail -5`
Expected: `ok  github.com/homeend/gigagit/internal/web`

- [ ] **Step 5: Commit**

```bash
gg add internal/web/static/splitdiff.js internal/web/splitdiffjs_test.go
git commit -F $CLAUDE_JOB_DIR/tmp/msg.txt   # "web: the split diff model — one-column rows and their line map (pure)"
```

---

### Task 4: The viewer draws the diff — D, the title switch, the sticky choice

**Files:**
- Modify: `internal/web/static/viewer.js`, `internal/web/static/files.js` (export `collapseDiffRows`), `internal/web/static/style.css`
- Test: `internal/web/splitdiffjs_test.go` (add `TestSplitDiffIsWired`)

**Interfaces:**
- Consumes: Task 3's exports; `/api/diff?review=` (Task 2) and `/api/diff?wt=head` (existing); `collapseDiffRows(rows, open, keep)` from files.js.
- Produces (viewer.js, used by Tasks 5–6): `view.diff` = `{rows, by, row, notice, name, range, remarks, partial, open, shown}` or null; `let diffPref = null` (`{key, on, partial}`); `canFlip()`, `diffWanted(from = view.from)`, `fetchDiff(from, path) → {d, status}`, `buildDiff(got, from)`, `flipDiff()`, `refreshDiff()`, `keepCursorPlace(paint)`, `syncDiffRow()`, `diffNotes()`, `diffRowsHTML()`, `paintModeSwitch()`, `toggleDiffFold()`.

- [ ] **Step 1: Write the failing wiring guard**

Append to `internal/web/splitdiffjs_test.go` (add `"strings"` to its imports):

```go
// Spec §6 / §8: the viewer reads a diff for a file an overview's link
// opened when that overview's choice says so, before the first paint; D and
// the title's switch flip it; every close forgets the choice; a late read
// never lands on a newer file or a flipped-back choice.
func TestSplitDiffIsWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`from "./splitdiff.js"`,
		`let diffPref = null;`,
		`function canFlip()`,
		`function diffWanted(from = view.from)`,
		`diffWanted(from) ? fetchDiff(from, reg.file.path) : null`,
		`view.diff = dres ? buildDiff(dres, from) : null;`,
		`if (!from) diffPref = null;`,
		`if (seq !== loadSeq || id !== view.id || view.from !== from || !diffWanted()) return;`,
		`} else if (view.diff) {`,
		`<div id="viewer-mode" class="hidden"><button data-mode="file">file</button><button data-mode="diff">diff</button></div>`,
		`case "D": if (!canFlip()) return false; flipDiff(); break;`,
		`diffPref = null;\n  lastStep = null;`,
		`if (view.diff) refreshDiff();`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	if !strings.Contains(readStatic(t, "style.css"), "#viewer-mode.hidden { display: none; }") {
		t.Error("style.css: #viewer-mode.hidden has no rule (gg web hides by id)")
	}
	if !strings.Contains(readStatic(t, "files.js"), "collapseDiffRows,") {
		t.Error("files.js does not export collapseDiffRows")
	}
}
```

Note: `lastStep` is declared in this task (next to `diffPref`) and used in Task 6.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/overview-split-diff && go test ./internal/web -run TestSplitDiffIsWired -count=1 2>&1 | tail -15`
Expected: FAIL — every `viewer.js lacks …` line, the style and the files.js lines.

- [ ] **Step 3: Implement**

**files.js** — add `collapseDiffRows` to the big `export { … }` list at the end (e.g. right after `clearRowSelection`: `…, clearRowSelection, collapseDiffRows };`; the guard looks for `collapseDiffRows,` so put it BEFORE the last name instead: `…, collapseDiffRows, clearRowSelection };`).

**viewer.js — imports** (after the files.js import, which gains `collapseDiffRows`):

```js
import { collapseDiffRows, cycleTextMode, openFile, openWorkingTree, renderCell } from "./files.js";
import { diffHead, diffNotice, diffSource, lineRows, nextNoteRow, noteAtRow, placeNotes, rowBand, rowNewLine, rowOfLine, sameContent, unifiedRows } from "./splitdiff.js";
```

**viewer.js — state.** The `view` initialiser gains `diff: null` (after `range: null`). Under `let split = null;` add:

```js
// diffPref is the D / f choice (spec 2026-10-11-overview-split-diff §6):
// {key, on, partial}, key the overview whose links it holds for (a file's
// from.id). Page-local; gone when that overview's split or viewer closes.
let diffPref = null;
// lastStep is the file the previous link step showed ({key, path, lines}):
// the next step that opens the same text says so (§7).
let lastStep = null;
```

**viewer.js — markup.** In the `viewerRoot.innerHTML` string, `<div id="viewer-file"><div id="viewer-title"></div>` becomes:

```js
  `<div id="viewer-file"><div id="viewer-mode" class="hidden"><button data-mode="file">file</button><button data-mode="diff">diff</button></div><div id="viewer-title"></div>` +
```

and after the markup's listeners add:

```js
$("viewer-mode").addEventListener("click", (e) => {
  const b = e.target.closest("button[data-mode]");
  if (b && (b.dataset.mode === "diff") !== diffWanted()) flipDiff();
});
```

**viewer.js — resets.** Add `diff: null` to the `Object.assign(view, {…})` field lists in `showOverview`, `showStoredOverview` and `openSplit` (the right-column reset), and to the file `Object.assign` in `openViewer` (`{ id: f.id, src: …, lines: body.lines || [], diff: null }`).

**viewer.js — openViewer.** Declare `let reg, body, ov, ovMine, dres;`. The content read becomes:

```js
    } else {
      [body, dres] = await Promise.all([
        fetchContent(reg.file.source, reg.file.rev || "", reg.file.path, remarksReview(from)),
        diffWanted(from) ? fetchDiff(from, reg.file.path) : null,
      ]);
    }
```

After `if (ov) return showOverview(f, ov, ovMine);` add:

```js
  if (!from) diffPref = null; // a file opened any other way: the overview's choice ends (§6)
```

After `view.cur = landed.line;`:

```js
  view.diff = dres ? buildDiff(dres, from) : null; // read with the file: no flash of it first
```

**viewer.js — dropViewer** gains, after `if (split) leaveSplit();`:

```js
  diffPref = null;
  lastStep = null;
  view.diff = null;
```

**viewer.js — splitOverviewGone** gains, after `leaveSplit();`: `diffPref = null;\n  view.diff = null;` (the overview went away: its choice too).

**viewer.js — viewerFileChanged** gains, after `renderViewer();` and the two scroll restores, before the `swapFoot` line:

```js
  if (view.diff) refreshDiff(); // the working tree moved: the diff with it
```

**viewer.js — the diff section** (new, after `rerenderKeepingScroll`):

```js
// ---- the right column's diff (spec 2026-10-11-overview-split-diff) --------
// canFlip: D acts on a file an overview's link opened (§6).
function canFlip() {
  return !!view.id && !view.ov && !!view.from;
}

// diffWanted: the overview that opened the file asks for its diff.
function diffWanted(from = view.from) {
  return !!from && !!diffPref && diffPref.on && diffPref.key === from.id;
}

// fetchDiff reads path's diff for from: {d, status} — status a failed
// read's HTTP status (500 when it had none), 0 when it answered.
async function fetchDiff(from, path) {
  const url = diffSource(from, path);
  if (!url) return { d: null, status: 0 };
  try {
    return { d: await getJSON(url), status: 0 };
  } catch (e) {
    return { d: null, status: e.status || 500 };
  }
}

// buildDiff is view.diff for one read; the cursor's row follows view.cur.
function buildDiff({ d, status }, from) {
  const agent = !from.stored;
  const notice = diffNotice(d, status, agent);
  const rows = notice ? [] : unifiedRows(d);
  const by = lineRows(rows), head = diffHead(d, view.path, agent);
  const partial = !!diffPref && diffPref.key === from.id && diffPref.partial;
  return { rows, by, row: Math.max(0, rowOfLine(rows, by, view.cur)), notice, name: head.name, range: head.range,
    remarks: (d && d.remarks) || [], partial, open: new Set(), shown: null };
}

// flipDiff is D and the title's switch: the file ⇄ its diff, the choice
// held for the overview's next links (§6); the cursor keeps its line and
// its place on screen.
async function flipDiff() {
  if (!canFlip()) return;
  const from = view.from, on = !diffWanted();
  diffPref = { key: from.id, on, partial: !!diffPref && diffPref.key === from.id && diffPref.partial };
  if (!on) {
    keepCursorPlace(() => {
      view.diff = null;
      paintTitle();
      renderViewer();
    });
    return swapFoot(true);
  }
  paintTitle(); // the switch shows the choice while the read is out
  const seq = loadSeq, id = view.id;
  const got = await fetchDiff(from, view.path);
  if (seq !== loadSeq || id !== view.id || view.from !== from || !diffWanted()) return;
  keepCursorPlace(() => {
    view.diff = buildDiff(got, from);
    paintTitle();
    renderViewer();
  });
  swapFoot(true);
}

// refreshDiff re-reads the shown diff (the file moved on disk), keeping the
// fold state; the cursor keeps its line.
async function refreshDiff() {
  const from = view.from, id = view.id, seq = loadSeq, was = view.diff;
  const got = await fetchDiff(from, view.path);
  if (seq !== loadSeq || id !== view.id || view.from !== from || !diffWanted()) return;
  view.diff = buildDiff(got, from);
  if (was) Object.assign(view.diff, { partial: was.partial, open: was.open });
  paintTitle();
  rerenderKeepingScroll();
}

// keepCursorPlace repaints with paint and puts the cursor's row back where
// it was on screen — centred when it was off screen.
function keepCursorPlace(paint) {
  const body = $("viewer-body"), top = body.getBoundingClientRect().top;
  const before = cursorRow(), off = before ? before.getBoundingClientRect().top - top : -1;
  paint();
  const after = cursorRow();
  if (!after) return;
  if (off < 0 || off > body.clientHeight - after.offsetHeight) after.scrollIntoView({ block: "center" });
  else body.scrollTop += after.getBoundingClientRect().top - top - off;
}

// syncDiffRow puts the diff's cursor row on view.cur when something moved
// the line (n / p, a hit, a note); a deleted row stands for the line below
// it, so a cursor there stays.
function syncDiffRow() {
  const dv = view.diff;
  if (!dv || dv.notice || !dv.rows.length) return;
  if (rowNewLine(dv.rows, dv.row) !== view.cur) dv.row = Math.max(0, rowOfLine(dv.rows, dv.by, view.cur));
}

// diffNotes is every box the diff draws: the agent's notes (new side), then
// the review's remarks on both sides.
function diffNotes() {
  return [...view.notes, ...view.diff.remarks];
}

// diffRowsHTML is the diff's rows (§4): one column, deleted lines red with
// −, added green with +, the bands, the marked range and the note boxes;
// with f the unchanged runs fold, never a banded or noted row or the
// cursor's.
function diffRowsHTML() {
  const dv = view.diff, rows = dv.rows, bands = viewBands(), f = view.from;
  const curB = f ? bandOf(bands, f.anchors || [], f.cur, view.lines.length) : -1;
  syncDiffRow();
  const noted = new Set(), after = new Map();
  for (const p of placeNotes(rows, diffNotes())) {
    for (let k = p.first; k <= p.last; k++) noted.add(k);
    if (!after.has(p.last)) after.set(p.last, []);
    after.get(p.last).push(p.n);
  }
  const keep = (r) => r.i === dv.row || noted.has(r.i) || !!rowBand(rows, r.i, bands, curB);
  const items = (dv.partial && collapseDiffRows(rows, dv.open, keep)) || rows;
  dv.shown = [];
  let html = "";
  for (const r of items) {
    if (r.fold) {
      html += `<div class="vfold" data-fold="${r.start}">⋯ ${r.fold} unchanged line${r.fold === 1 ? "" : "s"}</div>`;
      continue;
    }
    dv.shown.push(r.i);
    const bk = rowBand(rows, r.i, bands, curB);
    const ranged = view.range && r.nu && view.range.start <= r.nu && r.nu <= view.range.end ? " vrange" : "";
    const cls = "vline vd-" + r.kind + (r.i === dv.row ? " vcur" : "") + (noted.has(r.i) ? " vnoted" : "") + ranged + (bk ? " vanchor" + (bk === "cur" ? " acur" : "") : "");
    const sign = r.kind === "add" ? "+" : r.kind === "del" ? "−" : " ";
    html +=
      `<div class="${cls}" data-r="${r.i}"><span class="vno">${r.kind === "del" ? r.old : r.nu}</span><span class="vsign">${sign}</span>` +
      `<span class="vtext">${renderCell(r.text, r.spans, r.tok, r.kind === "del" ? "l" : "r", viewerSearch.query ? viewerSearch.hitsOn(r.i, 0) : null) || " "}</span></div>`;
    for (const n of after.get(r.i) || []) html += noteBoxHTML(n);
  }
  return html;
}

// paintModeSwitch shows the title's file | diff switch when D can act, the
// half in force lit (§4).
function paintModeSwitch() {
  const sw = $("viewer-mode"), on = canFlip();
  sw.classList.toggle("hidden", !on);
  if (!on) return;
  const diff = diffWanted();
  for (const b of sw.querySelectorAll("button")) b.classList.toggle("on", (b.dataset.mode === "diff") === diff);
}
```

**viewer.js — renderViewer.** The search line becomes:

```js
  if (viewerSearch.query) viewerSearch.refind(view.diff ? view.diff.rows.map((r) => ({ row: r.i, side: 0, text: r.text })) : view.lines.map((l, i) => ({ row: i, side: 0, text: l.text || "" })));
```

and between the `view.ov` branch and the `view.placeholder` branch:

```js
  } else if (view.diff) {
    body.innerHTML = view.diff.notice ? `<div class="notice">${esc(view.diff.notice)}</div>` : diffRowsHTML();
```

**viewer.js — paintTitle** becomes:

```js
function paintTitle() {
  const el = $("viewer-title");
  paintModeSwitch();
  if (view.ov) {
    el.title = view.ov.title;
    el.textContent = ovTitle(view.ov, view.id);
    return;
  }
  if (!view.id) { // the split's right column before a link opened a file
    el.title = el.textContent = "";
    return;
  }
  const dv = view.diff, name = dv ? dv.name : view.path;
  const lead = dv ? "Diff " : "View ", tail = " (" + (dv ? dv.range : versionLabel(view.src, view.rev)) + ")";
  const mode = $("viewer-mode"), sw = mode.classList.contains("hidden") ? 0 : mode.offsetWidth + 12;
  el.style.paddingRight = sw ? 14 + sw + "px" : "";
  const cols = Math.floor((el.clientWidth - 28 - sw) / charWidth()) - lead.length - tail.length;
  el.title = name;
  el.textContent = lead + (cols > 0 ? elidePath(name, cols) : name) + tail;
}
```

**viewer.js — keys.** In `fileKey`'s switch, before `default`:

```js
    case "D": if (!canFlip()) return false; flipDiff(); break;
```

In `splitOverviewKey`'s switch (D from the left column flips the right one — plan ruling, see below), before `default`:

```js
    case "D": if (!canFlip()) return false; flipDiff(); break;
```

**viewer.js — cursorRow** becomes:

```js
function cursorRow() {
  const dv = view.diff;
  if (dv) return dv.notice ? null : $("viewer-body").querySelector(`.vline[data-r="${dv.row}"]`);
  return $("viewer-body").querySelector(`.vline[data-i="${view.cur - 1}"]`);
}
```

**style.css** — after the `.vtext` rule (~line 1759):

```css
/* The overview split's diff (spec 2026-10-11-overview-split-diff §4). */
#viewer-file { position: relative; }
#viewer-mode { position: absolute; top: 7px; right: 10px; z-index: 1; display: flex; border: 1px solid var(--border); border-radius: 4px; overflow: hidden; }
#viewer-mode.hidden { display: none; }
#viewer-mode button { font: inherit; font-size: 12px; color: var(--dim); background: none; border: 0; padding: 2px 10px; cursor: pointer; }
#viewer-mode button + button { border-left: 1px solid var(--border); }
#viewer-mode button.on { color: var(--fg); background: var(--sel); }
.vsign { flex: none; width: 1.4em; text-align: center; color: var(--dim); }
.vline.vd-add .vtext, .vline.vd-add .vsign { background: var(--add); }
.vline.vd-del .vtext, .vline.vd-del .vsign { background: var(--del); }
.vline.vd-del .vno { opacity: .6; }
.vline.vd-add.vanchor .vtext, .vline.vd-del.vanchor .vtext { box-shadow: inset 0 0 0 999px color-mix(in srgb, #d4a72c 14%, transparent); }
.vline.vd-add.vcur .vtext, .vline.vd-del.vcur .vtext, .vline.vd-add.vcur .vsign, .vline.vd-del.vcur .vsign { background: var(--sel); }
.vfold { text-align: center; color: var(--dim); background: var(--bg-alt); border-top: 1px dashed var(--border); border-bottom: 1px dashed var(--border); cursor: pointer; user-select: none; }
.vfold:hover { color: var(--fg); }
```

(If `#viewer-file` already has a rule, add `position: relative;` to it rather than a second rule.)

**Plan ruling (ledger it):** D also works from the split's LEFT column (the reader steps links there and wants the diff without a Tab) — it always flips the right column. Spec §6 names the right column as what flips, not which column the key comes from.

- [ ] **Step 4: Run the guard and the web package**

Run: `cd /work/gigagit/.claude/worktrees/overview-split-diff && go test ./internal/web -count=1 2>&1 | tail -8`
Expected: `ok  github.com/homeend/gigagit/internal/web` (an older guard naming `paintTitle`'s old `const lead = "View "` text, if any, is updated to the new line in the same commit; ledger it).

- [ ] **Step 5: Smoke the page by hand-free check** — `node --check internal/web/static/viewer.js && node --check internal/web/static/splitdiff.js`
Expected: no output (both parse).

- [ ] **Step 6: Commit**

```bash
gg add internal/web/static/viewer.js internal/web/static/files.js internal/web/static/style.css internal/web/splitdiffjs_test.go
git commit -F $CLAUDE_JOB_DIR/tmp/msg.txt   # "web: D flips the overview split's file to its review diff, one column, the choice held per overview"
```

---

### Task 5: Keys and footer in the diff

**Files:**
- Modify: `internal/web/static/viewer.js`
- Test: `internal/web/splitdiffjs_test.go` (add `TestSplitDiffKeysAreWired`)

**Interfaces:**
- Consumes: Task 4's `view.diff`, `syncDiffRow`, `diffNotes`, `flipDiff`, `keepCursorPlace`; Task 3's `rowNewLine`, `placeNotes`, `nextNoteRow`, `noteAtRow`.
- Produces: `onDeletedRow()`, `noteAtCursor()`, `toggleDiffFold()`, `diffChips()`; the cursor / click / search / L / menu / } { / d r / f behaviour of spec §5.

- [ ] **Step 1: Write the failing guard**

Append to `internal/web/splitdiffjs_test.go`:

```go
// Spec §5: in a diff the cursor walks every shown row, a click or a hit
// lands on its row, } / { step the boxes by row (old-side remarks too),
// d / r and the footer act on the box at the cursor's row, L and the menu
// refuse a deleted line, f folds, and the footer and help advertise D / f.
func TestSplitDiffKeysAreWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`function onDeletedRow()`,
		`function noteAtCursor()`,
		`"a deleted line has no link in the reviewed version"`,
		`const here = onDeletedRow() && !view.range ? null : viewerLink`,
		`case "f": if (!view.diff) return false; if (!view.diff.notice) toggleDiffFold(); break;`,
		`const line = view.diff ? 0 : nextNoteLine(shownNotes(), view.cur, dir);`,
		`nextNoteRow(placeNotes(dv.rows, diffNotes()), dv.row, dir)`,
		`view.diff.row = h.row;`,
		`const fold = e.target.closest(".vfold");`,
		`e.target.closest(".vline[data-i], .vline[data-r]")`,
		`case "dflip": flipDiff(); break;`,
		`case "dfold": if (view.diff && !view.diff.notice) toggleDiffFold(); break;`,
		`<b>D</b>`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	if n := strings.Count(v, "noteAtLine(shownNotes(), view.cur)"); n != 1 { // noteAtCursor's own file branch only
		t.Errorf("noteAtLine(shownNotes(), view.cur) appears %d times: every d / r path must read noteAtCursor()", n)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web -run TestSplitDiffKeysAreWired -count=1 2>&1 | tail -15`
Expected: FAIL — the missing names, and the count line (`appears 4 times`).

- [ ] **Step 3: Implement**

Helpers (in the diff section):

```js
// onDeletedRow: the diff's cursor is on a line the review deleted — no
// line at the reviewed version to link or copy (§5).
function onDeletedRow() {
  const dv = view.diff;
  return !!dv && !dv.notice && dv.rows[dv.row]?.kind === "del";
}

// noteAtCursor is the box d / r act on: in a diff the one covering the
// cursor's row (an old-side remark included), else the one on the line.
function noteAtCursor() {
  const dv = view.diff;
  if (dv && !dv.notice) return noteAtRow(placeNotes(dv.rows, diffNotes()), dv.row);
  return noteAtLine(shownNotes(), view.cur);
}

// toggleDiffFold is f in a diff: the changed lines only ⇄ the whole file,
// held with D's choice (§4).
function toggleDiffFold() {
  const dv = view.diff;
  dv.partial = !dv.partial;
  dv.open = new Set();
  if (diffPref && view.from && diffPref.key === view.from.id) diffPref.partial = dv.partial;
  keepCursorPlace(renderViewer);
  swapFoot(true);
}

// diffChips are the footer's D / f chips: D wherever it acts, f in a diff.
function diffChips() {
  if (!canFlip()) return "";
  const dv = view.diff;
  return `<button data-vact="dflip">D file / diff</button>` +
    (dv && !dv.notice ? `<button data-vact="dfold">f ${dv.partial ? "whole file" : "changed only"}</button>` : "");
}
```

Replace every `noteAtLine(shownNotes(), view.cur)` (fileKey `d` / `r`, the footer's `dismiss` / `ref`) with `noteAtCursor()`.

**moveCursor** becomes:

```js
function moveCursor(delta) {
  const dv = view.diff;
  if (dv && !dv.notice) {
    const shown = dv.shown && dv.shown.length ? dv.shown : dv.rows.map((r) => r.i);
    if (!shown.length) return;
    const at = Math.max(0, shown.indexOf(dv.row));
    dv.row = shown[Math.min(Math.max(at + delta, 0), shown.length - 1)];
    view.cur = rowNewLine(dv.rows, dv.row);
    return paintCursor();
  }
  view.cur = clampLine(view.cur + delta, view.lines.length);
  paintCursor();
}
```

**fileKey** Home / End / f:

```js
    case "Home": case "g": if (view.diff) moveCursor(-Infinity); else { view.cur = clampLine(1, view.lines.length); paintCursor(); } break;
    case "End": case "G": if (view.diff) moveCursor(Infinity); else { view.cur = view.lines.length; paintCursor(); } break;
    case "f": if (!view.diff) return false; if (!view.diff.notice) toggleDiffFold(); break;
```

**paintCursor** begins with:

```js
  syncDiffRow();
  if (view.diff && !view.diff.notice && !cursorRow()) rerenderKeepingScroll(); // the row was folded: keep() shows it now
```

**stepNote** — the line lookup and a row branch before it:

```js
async function stepNote(dir) {
  const dv = view.diff;
  if (dv && !dv.notice) {
    const r = nextNoteRow(placeNotes(dv.rows, diffNotes()), dv.row, dir);
    if (r >= 0) {
      dv.row = r;
      view.cur = rowNewLine(dv.rows, r);
      paintCursor();
      const n = noteAtCursor();
      for (const b of $("viewer-body").querySelectorAll(".vnote")) if (n && b.dataset.note === n.id) b.scrollIntoView({ block: "nearest" });
      cursorRow()?.scrollIntoView({ block: "nearest" });
      return;
    }
  }
  const line = view.diff ? 0 : nextNoteLine(shownNotes(), view.cur, dir);
  // … the rest as today (the line branch, then the next noted file)
```

**Body click** — at the top of the `$("viewer-body")` click listener, after the split focus line:

```js
  const fold = e.target.closest(".vfold");
  if (fold && view.diff) {
    view.diff.open.add(Number(fold.dataset.fold));
    return rerenderKeepingScroll();
  }
```

and the row handling reads either kind of row:

```js
  const row = e.target.closest(".vline[data-i], .vline[data-r]");
  if (!row) return;
  const dv = view.diff, hit = row.dataset.r !== undefined ? rowNewLine(dv.rows, Number(row.dataset.r)) : Number(row.dataset.i) + 1;
  if (e.shiftKey && e.target.closest(".vno")) {
    getSelection().removeAllRanges();
    view.rangeOwn = true;
    view.range = viewerRange(Math.min(view.cur || hit, hit), Math.max(view.cur || hit, hit), view.lines.length);
    rerenderKeepingScroll();
    return;
  }
  const picked = getSelection();
  if (!picked.isCollapsed && $("viewer-body").contains(picked.anchorNode)) return;
  if (row.dataset.r !== undefined) dv.row = Number(row.dataset.r);
  view.cur = hit;
  if (clearViewerRange()) return;
  paintCursor();
```

The `contextmenu` listener likewise:

```js
  const row = e.target.closest(".vline[data-i], .vline[data-r]");
  if (row && row.dataset.r !== undefined) {
    view.diff.row = Number(row.dataset.r);
    view.cur = rowNewLine(view.diff.rows, view.diff.row);
  } else if (row) view.cur = Number(row.dataset.i) + 1;
```

**Search** — `goToViewerHit`'s cursor lines:

```js
  if (h) {
    if (view.diff) {
      view.diff.row = h.row;
      view.cur = rowNewLine(view.diff.rows, h.row);
    } else view.cur = h.row + 1;
    paintCursor();
  }
```

`viewerSearchBar`: `here: () => ({ row: view.diff ? view.diff.row : Math.max(0, view.cur - 1), side: 0, col: -1 }),`, `origin` adds `row: view.diff ? view.diff.row : -1`, `restore` adds `if (view.diff && o.row >= 0) view.diff.row = o.row;` after `view.cur = o.cur;`.

**Marking** — `paintViewerBand`'s guard gains `|| view.diff` (a diff repaints); `stepViewerRange`'s scroll target:

```js
  const row = view.diff
    ? $("viewer-body").querySelector(`.vline[data-r="${view.diff.by[got.end]}"]`)
    : $("viewer-body").querySelector(`.vline[data-i="${got.end - 1}"]`);
```

**L and the menu** — `copyViewerLinkHere`:

```js
function copyViewerLinkHere() {
  if (onDeletedRow() && !view.range) return opLine("a deleted line has no link in the reviewed version", false);
  const got = viewerLinkHere();
  if (got) copyViewerLink(got.link);
}
```

`openViewerMenu`: `const here = onDeletedRow() && !view.range ? null : viewerLinkHere();` and the copy-line row gains `&& !onDeletedRow()`.

**Footer** — `viewerFoot`: the split-left branch ends its chips with `+ diffChips()` before `<button data-vact="close">`; the split-right branch and the one-document file branch insert `diffChips()` right after their `bandKeys` span. The `#foot` click switch gains:

```js
    case "dflip": flipDiff(); break;
    case "dfold": if (view.diff && !view.diff.notice) toggleDiffFold(); break;
```

**Help** — `registerHelp({ key: "view file", … })`'s html gains, before the final sentence about the overview's bands:

```js
    "In a file an overview's link opened, <b>D</b> (or the <b>file | diff</b> switch in its title) flips to the change the review made to it — an agent's overview: HEAD → the working tree — " +
    "drawn as one column, the whole file with the deleted lines red with − and the added green with +, the link's lines banded and the review's remarks under their lines (one on a deleted line too); " +
    "the choice holds while you step the overview's links until <b>D</b> again or <b>esc</b>; <b>f</b> in the diff shows the changed lines only. " +
```

- [ ] **Step 4: Run the web package**

Run: `cd /work/gigagit/.claude/worktrees/overview-split-diff && go test ./internal/web -count=1 2>&1 | tail -5 && node --check internal/web/static/viewer.js`
Expected: `ok  github.com/homeend/gigagit/internal/web`, no node output.

- [ ] **Step 5: Commit**

```bash
gg add internal/web/static/viewer.js internal/web/splitdiffjs_test.go
git commit -F $CLAUDE_JOB_DIR/tmp/msg.txt   # "web: the split diff's keys — cursor over rows, notes by row, deleted lines refuse a link, f folds, footer and help"
```

---

### Task 6: `same content as <path>`

**Files:**
- Modify: `internal/web/static/viewer.js` (`openViewer`'s tail)
- Test: `internal/web/splitdiffjs_test.go` (add `TestSameContentLineIsWired`)

**Interfaces:**
- Consumes: `sameContent` (Task 3), `lastStep` (Task 4).
- Produces: the §7 status line on a link step.

- [ ] **Step 1: Write the failing guard**

```go
// Spec §7: a link step whose file holds the previous step's very text, at
// another path, says so — after the open's own notices, which win.
func TestSameContentLineIsWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`const step = { key: from.id, path: f.path, lines: view.placeholder || view.image ? [] : view.lines.map((l) => l.text || "") };`,
		`if (!landed.notice && !reg.evicted && sameContent(lastStep, step)) opLine("same content as " + elidePath(lastStep.path, 60), false);`,
		`lastStep = step;`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web -run TestSameContentLineIsWired -count=1 2>&1 | tail -6`
Expected: FAIL — three `lacks` lines.

- [ ] **Step 3: Implement** — at the end of `openViewer`, after the `reg.evicted` line and before `return { ok: true, … }`:

```js
  if (from) { // a link step (§7): the same text as the previous step's file says so
    const step = { key: from.id, path: f.path, lines: view.placeholder || view.image ? [] : view.lines.map((l) => l.text || "") };
    if (!landed.notice && !reg.evicted && sameContent(lastStep, step)) opLine("same content as " + elidePath(lastStep.path, 60), false);
    lastStep = step;
  }
```

- [ ] **Step 4: Run the web package**

Run: `go test ./internal/web -count=1 2>&1 | tail -3`
Expected: `ok  github.com/homeend/gigagit/internal/web`

- [ ] **Step 5: Commit**

```bash
gg add internal/web/static/viewer.js internal/web/splitdiffjs_test.go
git commit -F $CLAUDE_JOB_DIR/tmp/msg.txt   # "web: a link step to a file with the previous step's text says same content as …"
```

---

### Task 7: Visible checks (Playwright) and docs

**Files:**
- Create (scratch, not committed): `$CLAUDE_JOB_DIR/tmp/websplitdiff/check.mjs` (copy `$CLAUDE_JOB_DIR/tmp/websplit/` with its `package.json` and `node_modules`, then replace the scenarios)
- Modify: `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`

- [ ] **Step 1: Build the verify binary**

Run: `cd /work/gigagit/.claude/worktrees/overview-split-diff && go build -o bin/gg ./cmd/gg && md5sum bin/gg`
Expected: a hash (record it in the ledger).

- [ ] **Step 2: Write the checks**

`cp -r $CLAUDE_JOB_DIR/tmp/websplit $CLAUDE_JOB_DIR/tmp/websplitdiff`, then in the copy's `check.mjs` keep the harness (`page`, `openStored`, `vis`, `hidden`, `text`, `live`, `addOverview`, `ok`, `fail`, the run loop) and replace the fixture and scenarios with:

```js
const lines = (n) => Array.from({ length: n }, (_, i) => "line " + (i + 6)).join("\n") + "\n";
writeFileSync(join(repo, "a.txt"), "old\none\ntwo\nthree\nfour\nfive\n" + lines(7));
writeFileSync(join(repo, "b.txt"), "alpha\nbeta\ngamma\n");
writeFileSync(join(repo, "r.txt"), "readme\n");
writeFileSync(join(repo, "gone.txt"), "bye\n");
git("add", "-A"); git("commit", "-qm", "seed");
writeFileSync(join(repo, "a.txt"), "one\ntwo\nthree\nfour\nfive\n" + lines(7)); // deletes "old"
writeFileSync(join(repo, "b.txt"), "alpha\nbeta\ngamma\ndelta\n");
mkdirSync(join(repo, "c1")); mkdirSync(join(repo, "c2"));
writeFileSync(join(repo, "c1", "x.txt"), "same\n"); writeFileSync(join(repo, "c2", "x.txt"), "same\n");
git("rm", "-q", "gone.txt"); git("add", "-A"); git("commit", "-qm", "change");
const sha = git("rev-parse", "HEAD");
const doc = { version: 1, summary: "## Summary\nfine",
  overview: "Start at [line two](a.txt:2), then [four](a.txt:4-5), then [beta](b.txt:2), [x1](c1/x.txt:1), [x2](c2/x.txt:1).",
  files: [{ path: "a.txt", annotations: [{ newRange: [2, 2], summary: "why two?" }, { oldRange: [1, 1], summary: "why drop old?" }] }] };
```

(import `mkdirSync` from `node:fs`). Scenarios:

```js
const title = (p) => text(p, "#viewer-title");
const titled = (p, s) => p.waitForFunction((s) => document.querySelector("#viewer-title").innerText.startsWith(s), s, { timeout: 3000 }).then(() => true, () => false);

scenarios.flip = async () => {
  const p = await page(1400);
  await openStored(p);
  ok(await titled(p, "View a.txt"), "the split opens on the file");
  ok(await vis(p, '#viewer-mode button.on:has-text("file")'), "the switch is visible with file lit");
  ok(!(await p.locator('#viewer-body .vnote:has-text("why drop old?")').count()), "the file view leaves out the deleted line's remark");
  await p.keyboard.press("D"); // from the left column
  ok(await titled(p, "Diff a.txt ("), "D: the title reads Diff a.txt (…)");
  ok((await title(p)).includes("→"), "the title names both sides");
  ok(await vis(p, '#viewer-mode button.on:has-text("diff")'), "the switch lights diff");
  ok(await vis(p, '#viewer-body .vline.vd-del:has-text("old")'), "the deleted line is visible");
  ok((await text(p, '#viewer-body .vline.vd-del:has-text("old") .vsign')) === "−", "it carries −");
  ok(await vis(p, '#viewer-body .vline.vanchor.acur:has-text("two")'), "the link's line is banded current");
  ok(await vis(p, '#viewer-body .vnote:has-text("why drop old?")'), "the deleted line's remark shows in the diff");
  ok(await vis(p, '#viewer-body .vnote:has-text("why two?")'), "the new line's remark shows too");
  await p.keyboard.press("D"); await p.keyboard.press("D"); // fast off-on: the last wins
  ok(await titled(p, "Diff a.txt ("), "two quick D presses settle on the diff");
  await p.close();
};

scenarios.sticky = async () => {
  const p = await page(1400);
  await openStored(p);
  await p.keyboard.press("D");
  await titled(p, "Diff a.txt (");
  await p.keyboard.press("ArrowDown"); await p.keyboard.press("ArrowDown"); // to beta
  ok(await titled(p, "Diff b.txt ("), "the next links open as diffs");
  ok(await vis(p, '#viewer-body .vline.vd-add:has-text("delta")'), "b.txt's added line is visible");
  await p.click('#viewer-mode button[data-mode="file"]');
  ok(await titled(p, "View b.txt"), "a click on file flips back");
  ok(!(await p.locator("#viewer-body .vline.vd-add").count()), "no diff rows remain");
  await p.keyboard.press("D");
  await titled(p, "Diff b.txt (");
  await p.keyboard.press("Escape");
  await hidden(p, "#viewer");
  await openStored(p);
  ok(await titled(p, "View a.txt"), "after esc the next split starts on the file");
  await p.close();
};

scenarios.fold = async () => {
  const p = await page(1400);
  await openStored(p);
  await p.keyboard.press("D");
  await titled(p, "Diff a.txt (");
  await p.keyboard.press("Tab");
  await p.keyboard.press("f");
  ok(await vis(p, '#viewer-body .vfold:has-text("7 unchanged lines")'), "f folds the unchanged tail");
  ok(await vis(p, '#viewer-body .vline.vanchor:has-text("four")'), "the banded lines are never folded");
  ok(await vis(p, '#foot button[data-vact="dfold"]:has-text("whole file")'), "the footer offers the whole file back");
  await p.keyboard.press("f");
  ok(await hidden(p, "#viewer-body .vfold"), "f again shows the whole file");
  await p.keyboard.press("Home");
  await p.keyboard.press("L"); // the cursor on the deleted "old" row
  ok((await text(p, "#op-text")).includes("a deleted line has no link in the reviewed version"), "L on a deleted line refuses");
  await p.close();
};

scenarios.same = async () => {
  const p = await page(1400);
  await openStored(p);
  for (let i = 0; i < 3; i++) await p.keyboard.press("ArrowDown"); // to x1
  await titled(p, "View c1/x.txt");
  await p.keyboard.press("ArrowDown"); // to x2
  await titled(p, "View c2/x.txt");
  ok(await p.waitForFunction(() => document.querySelector("#op-text").innerText.includes("same content as c1/x.txt"), null, { timeout: 3000 }).then(() => true, () => false),
    "the identical file says same content as c1/x.txt");
  await p.close();
};

scenarios.agent = async () => {
  const p = await page(1400);
  await live(p);
  logged(addOverview("Tour", "See [readme](r.txt:1)."));
  ok(await titled(p, "View r.txt"), "the agent overview opens r.txt");
  await p.keyboard.press("D");
  ok(await vis(p, '#viewer-body .notice:has-text("no changes in the working tree — D shows the file")'), "an unchanged file says so");
  writeFileSync(join(repo, "r.txt"), "readme\nmore\n");
  ok(await vis(p, '#viewer-body .vline.vd-add:has-text("more")'), "an edit on disk shows in the diff");
  ok(await titled(p, "Diff r.txt (HEAD → working tree)"), "the title names HEAD → working tree");
  await p.close();
};

scenarios.narrow = async () => {
  const p = await page(800);
  await openStored(p);
  await p.keyboard.press("Enter");
  await titled(p, "View a.txt");
  await p.keyboard.press("D");
  ok(await titled(p, "Diff a.txt ("), "the narrow viewer flips too");
  await p.keyboard.press("Backspace");
  await p.waitForSelector("#viewer-body .vdoc");
  await p.keyboard.press("Tab"); await p.keyboard.press("Tab"); // to beta
  await p.keyboard.press("Enter");
  ok(await titled(p, "Diff b.txt ("), "back and another link keeps the diff");
  await p.close();
};
```

(If `live`, `addOverview` or `logged` sit below the old scenarios in the copied file, keep their definitions above the new scenarios.)

- [ ] **Step 3: Run the checks against the fresh binary**

Run: `cd $CLAUDE_JOB_DIR/tmp/websplitdiff && node check.mjs /work/gigagit/.claude/worktrees/overview-split-diff/bin/gg 2>&1 | tail -40`
Expected: every line `ok …`, then `all checks passed`. A failure is a finding: fix the code (systematic-debugging), rebuild (Step 1), rerun.

- [ ] **Step 4: Watch the checks fail on the unfixed binary**

Run: `cd /work/gigagit && go build -o $CLAUDE_JOB_DIR/tmp/websplitdiff/gg-main ./cmd/gg && cd $CLAUDE_JOB_DIR/tmp/websplitdiff && node check.mjs ./gg-main flip 2>&1 | tail -3`
Expected: `FAIL: D: the title reads Diff a.txt (…)` — proof the checks see the feature (the main checkout is main's code).

- [ ] **Step 5: Docs**

`CHANGELOG.md` — a new top entry in the file's style:

```markdown
- gg web: in an overview's split, **D** (or the **file | diff** switch in the right column's title) flips the file a link opened to the change the review made to it — the review's stored range (a PR: merge base → head), a commit's own change, or HEAD → the working tree for a working review or an agent's overview — drawn as one column with the whole file, deleted lines red with −, added green with +, the link's lines banded and the review's remarks under their lines (one on a deleted line too). The choice holds while you step the overview's links until D again or esc; **f** shows the changed lines only. Two links in a row that open identical files say `same content as <path>`.
```

`README.md` — in the gg web overview split paragraph, one sentence: `In the file a link opened, D (or the file | diff switch in its title) shows the review's change to that file instead, and keeps doing so for the next links.`

`docs/CLAUDE-details.md` — in "Review overview split", after the gg web paragraph:

```markdown
**gg web: file ⇄ review diff (2026-10-11, spec `2026-10-11-overview-split-diff-design.md`, plan 1).**
`domain.ReviewFileDiff(id, path)` = the review's stored range (`reviewRevs`; a single commit's parent, "empty" for a root) or HEAD → disk for a working review, status / old path from `ReviewFiles` (not listed = `Changed` false, no read), the cached Differ, `ReviewNotesFor` remarks on BOTH sides; `/api/diff?review=` serves it (+ `changed`, `status`, `old_path`, `left_label`, `right_label`, `remarks` with `side: "old"`). An agent overview reads `wt=head`. Page: `static/splitdiff.js` (pure, node-guarded) makes one-column rows (`unifiedRows`: a run's deletions then additions) and every row ↔ new-line map; `view.cur` stays a NEW line everywhere, `view.diff.row` is the cursor's row (`syncDiffRow` re-finds it when something moved the line; a deleted row stands for the line below). The choice lives in `diffPref` keyed by `from.id` (cleared by `dropViewer`, a plain-file open, the overview leaving the store). Gotchas: D works from either split column; f in file mode still falls through to the page's diff toggle; `openViewer` reads the diff WITH the file (no flash).
```

- [ ] **Step 6: Commit the docs**

```bash
gg add CHANGELOG.md README.md docs/CLAUDE-details.md
git commit -F $CLAUDE_JOB_DIR/tmp/msg.txt   # "docs: gg web overview split file / review diff"
```

- [ ] **Step 7: Race gate**

Run (background, then read the log's end): `cd /work/gigagit/.claude/worktrees/overview-split-diff && env -u GG_INBOX -u GG_MCP_URL -u GG_SESSION_TOKEN -u GG_SESSION_ID ./test.sh race > $CLAUDE_JOB_DIR/tmp/race-diff.log 2>&1; tail -5 $CLAUDE_JOB_DIR/tmp/race-diff.log`
Expected: the log ends with `all green`.
