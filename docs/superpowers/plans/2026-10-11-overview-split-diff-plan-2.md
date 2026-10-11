# Overview split file / review diff — plan 2 (TUI) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (native, no implementer subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In the TUI's overview split (and its narrow full-screen fallback), D flips the right column between the file at the reviewed tip and that file's review diff, drawn as one column of rows over which the cursor, search, v, marking, bands, remark boxes and the overview's mark all work.

**Architecture:** The open file keeps its FILE popup in `d.p` (every line-number consumer stays right); a new `d.diff` holds a second `contentPopup` whose lines are the diff rows plus the row map. Every frame and key path asks `d.shownP()` for the popup to draw / move, and after every key `d.p.cur` mirrors the new line of the diff cursor (the web's `view.cur` rule), so consumers that read `d.p.cur` as a file line keep working. The choice (D, f) is held on the overview (`ov.ov.diff`), so it survives link steps and the narrow viewer and is forgotten when the overview's split or viewer closes.

**Tech Stack:** Go 1.26, Bubble Tea, `internal/domain` (`ReviewFileDiff`, new `WorkingFileDiff`), `internal/textdiff` rows, the TUI's `renderWindow` (`winRow.prefix` frozen column, `emph`, `cls`).

**Spec:** `docs/superpowers/specs/2026-10-11-overview-split-diff-design.md` (rulings D1–D6; this plan is §9 plus the shared §3–§7, §10, §12). Plan 1 (web) is merged (`e3643ce2`); its model is `internal/web/static/splitdiff.js`.

## Global Constraints

- Every user-visible TUI string goes through `i18n.T` with a literal key present in ja/ko/zh/ru (`internal/i18n/lang/*.toml`); the AST gates (`i18n_scan_test.go`, …) must stay green.
- `internal/tui` never imports `internal/git`; reads go through `internal/domain`.
- Notice texts (spec §4): `no change to this file in the review — D shows the file`, `no changes in the working tree — D shows the file`, `binary file — D shows the file`, `diff too large — D shows the file`, `the review is gone — D shows the file` (+ `could not read the diff — D shows the file` for any other failure, plan 1's).
- Title: file `View <path> @ <hash>` (unchanged); diff `Diff <path> (<left> → <right>)`, a rename `Diff <old> → <path> (…)`; working tree `(HEAD → working tree)`.
- Switch text `file | diff`, the half in force in reverse video, shown only when D can act.
- Deleted-row refusal: `a deleted line has no link in the reviewed version`.
- D6 status: `same content as <previous path>` (path cut in the middle).
- Tests call `t.Parallel()`; repos via the existing helpers (`stackRepoModel`/`testRepo`), never the machine's stores (`tempPromptStore`).
- Run `./test.sh race` with `GG_INBOX`, `GG_MCP_URL`, `GG_SESSION_TOKEN`, `GG_SESSION_ID` unset; green only on "all green" in the log.

## Pre-check (spec §9: "first confirm D reaches nothing there today")

Done while writing this plan: `grep -n '"D"' internal/tui/*.go` lists only `blame_view.go` (its own layer), `footer.go`/`help.go`/`model.go:2877` (the Files panel's discard-all — the main screen, never reached while a layer is on top: `fileViewer.update` swallows every key, `splitView.updateKey` hands unknown keys to the focused pane), `theme_editor_popup.go` (its own popup). `docPane.update`, `overviewPane.update`, `splitView.updateKey`, `fileViewer.update`, `overviewKey`, `bandKey`, `previewNoteKey`, `previewSelectKey`, `previewSearchKey`, `previewCharKey` have no `D` case: D does nothing in the split and the viewer today. `f` likewise has no case there.

## Review Focus

1. A D (or a click) while the step's file is still loading, then the load lands: the diff must show (the choice wins at landing time, never at press time).
2. A working-tree file that changes on disk while its diff shows: the diff is read again, the cursor keeps its new line, no flash of the file.
3. A file the review deleted (the file view is the `(load failed …)` placeholder): D still shows every line as deleted; nothing indexes the placeholder as a line.
4. Leaving the overview's context — esc on the split, X/ctrl+] on the right file, the switcher bringing the file back alone, a narrow resize: the diff never shows outside an overview step, except the resize, which keeps the choice.
5. Old-side remarks: a remark on a deleted line shows under its deleted row and d / r / enter on it say read-only / open it — never a crash on a row with no new line.

---

### Task 1: Domain — `WorkingFileDiff`

**Files:**
- Modify: `internal/domain/review_file_diff.go`
- Test: `internal/domain/review_file_diff_test.go`

**Interfaces:**
- Produces: `func (s *Service) WorkingFileDiff(ctx context.Context, path string) (ReviewFileDiff, error)` — Left `HEAD`, Right `working tree`, `Changed` false when no row changed (and not binary / too large), no `Notes`, `Status` `A` when HEAD has no such file.

- [ ] **Step 1: Write the failing test** (append to `review_file_diff_test.go`, reusing that file's repo helper — read its top for the helper's name and a Service constructor; the shape below assumes `reviewDiffRepo(t)` returning `(*Service, dir)`; adapt the two names to what the file defines):

```go
func TestWorkingFileDiffHeadToDisk(t *testing.T) {
	t.Parallel()
	s, dir := reviewDiffRepo(t) // a.txt committed
	ctx := context.Background()
	same, err := s.WorkingFileDiff(ctx, "a.txt")
	if err != nil || same.Changed || same.Left != "HEAD" || same.Right != "working tree" {
		t.Fatalf("unchanged: %+v %v", same, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.WorkingFileDiff(ctx, "a.txt")
	if err != nil || !got.Changed || len(got.Diff.Result.Rows) == 0 || got.Notes != nil {
		t.Fatalf("changed: %+v %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := s.WorkingFileDiff(ctx, "new.txt")
	if err != nil || !added.Changed || added.Status != "A" {
		t.Fatalf("untracked: %+v %v", added, err)
	}
}
```

- [ ] **Step 2: Run it** — `go test ./internal/domain -run TestWorkingFileDiffHeadToDisk` → FAIL: `s.WorkingFileDiff undefined`.

- [ ] **Step 3: Implement** (append to `review_file_diff.go`):

```go
// WorkingFileDiff is path's change from HEAD to the file on disk: what an
// agent overview's file shows when D flips it (spec 2026-10-11-overview-
// split-diff §3, D4). Changed is false when no row changed; a file HEAD does
// not have is an add. No remarks: an agent's notes are the TUI's own.
func (s *Service) WorkingFileDiff(ctx context.Context, path string) (ReviewFileDiff, error) {
	path = reviewPath(path)
	out := ReviewFileDiff{Left: "HEAD", Right: "working tree"}
	head := false
	oldSrc := func(ctx context.Context) ([]byte, error) {
		b, err := s.ShowFile(ctx, "HEAD", path)
		if err != nil && ctx.Err() == nil {
			return nil, nil
		}
		head = err == nil
		return b, err
	}
	newSrc := absentOnError(func(ctx context.Context) ([]byte, error) {
		return s.ResolveBytes(ctx, model.FileRef{Source: model.SourceUnstaged, Path: path})
	})
	d, err := s.Differ().Diff(ctx, Request{Path: path, Old: oldSrc, New: newSrc})
	if err != nil {
		return ReviewFileDiff{}, err
	}
	out.Diff = d
	if !head {
		out.Status = "A"
	}
	out.Changed = d.Binary || d.TooLarge || rowsChange(d.Result.Rows)
	return out, nil
}

// rowsChange reports whether any aligned row is not an unchanged one.
func rowsChange(rows []textdiff.Row) bool {
	for _, r := range rows {
		if r.Kind != textdiff.Same {
			return true
		}
	}
	return false
}
```

(Import `internal/textdiff`; if the Differ caches by an empty key — check `cachedDiffer` skips `Key == ""` — keep `Key` empty so the moving working tree is never cached, as `worktreeDiff` does.)

- [ ] **Step 4: Run it** — same command → PASS. Then `go test ./internal/domain` → PASS.

- [ ] **Step 5: Commit** — `gg add internal/domain/review_file_diff.go internal/domain/review_file_diff_test.go` + `git commit -F <msg>` ("domain: WorkingFileDiff — HEAD → disk for one file").

---

### Task 2: The pure row model

**Files:**
- Create: `internal/tui/split_diff_rows.go`
- Test: `internal/tui/split_diff_rows_test.go`

**Interfaces:**
- Consumes: `domain.Diff` (Rows, OldTok/NewTok), `sanitizeCell` (diff_syntax.go), `tokAt`.
- Produces:

```go
type diffRowKind uint8 // diffSame, diffDel, diffAdd, diffFold
type diffRow struct {
	kind    diffRowKind
	old, nu int            // 1-based numbers (0 = none)
	text    string         // display text (sanitized)
	raw     string         // source text (copy)
	cls     []syntax.Class // per display rune
	words   []emphLevel    // emphWord over changed words, per display rune (nil = none)
	hidden  int            // a fold: how many unchanged rows it stands for
	at      int            // index of this row in the unfolded list (a fold: its first hidden row)
}
func unifiedDiffRows(d domain.Diff) []diffRow
func rowOfNew(rows []diffRow, line int) int   // -1 none
func rowOfOld(rows []diffRow, line int) int   // -1 none
func rowNewLine(rows []diffRow, i int) int    // own; deleted/fold → next new below, else last above; 0 none
func foldDiffRows(all []diffRow, keep func(i int) bool, open map[int]bool) []diffRow // 3 context rows, ≥1 hidden row folds
func diffNoticeText(res domain.ReviewFileDiff, err error, agent bool) string
func diffTitle(path string, res domain.ReviewFileDiff) string
func sameText(a, b []string) bool
```

- [ ] **Step 1: Write the failing tests**

```go
package tui

import (
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/textdiff"
)

func rowsDiff(rows ...textdiff.Row) domain.Diff {
	return domain.Diff{Result: textdiff.Result{Rows: rows}}
}

func TestUnifiedDiffRowsPutsARunsDeletionsFirst(t *testing.T) {
	t.Parallel()
	d := rowsDiff(
		textdiff.Row{Kind: textdiff.Same, Left: "a", Right: "a", LeftNo: 1, RightNo: 1},
		textdiff.Row{Kind: textdiff.Changed, Left: "b", Right: "B", LeftNo: 2, RightNo: 2, LeftSpans: []textdiff.Span{{Start: 0, End: 1}}, RightSpans: []textdiff.Span{{Start: 0, End: 1}}},
		textdiff.Row{Kind: textdiff.Del, Left: "c", LeftNo: 3},
		textdiff.Row{Kind: textdiff.Add, Right: "x", RightNo: 3},
		textdiff.Row{Kind: textdiff.Same, Left: "d", Right: "d", LeftNo: 4, RightNo: 4},
	)
	got := unifiedDiffRows(d)
	want := []struct {
		k       diffRowKind
		old, nu int
		text    string
	}{{diffSame, 1, 1, "a"}, {diffDel, 2, 0, "b"}, {diffDel, 3, 0, "c"}, {diffAdd, 0, 2, "B"}, {diffAdd, 0, 3, "x"}, {diffSame, 4, 4, "d"}}
	if len(got) != len(want) {
		t.Fatalf("rows %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if g := got[i]; g.kind != w.k || g.old != w.old || g.nu != w.nu || g.text != w.text || g.at != i {
			t.Fatalf("row %d = %+v, want %+v", i, g, w)
		}
	}
	if got[1].words == nil || got[1].words[0] != emphWord || got[3].words == nil {
		t.Fatal("a changed pair keeps its changed words on both rows")
	}
}

func TestRowMapsAndTheDeletedRowRule(t *testing.T) {
	t.Parallel()
	rows := unifiedDiffRows(rowsDiff(
		textdiff.Row{Kind: textdiff.Same, Left: "a", Right: "a", LeftNo: 1, RightNo: 1},
		textdiff.Row{Kind: textdiff.Del, Left: "b", LeftNo: 2},
		textdiff.Row{Kind: textdiff.Same, Left: "c", Right: "c", LeftNo: 3, RightNo: 2},
		textdiff.Row{Kind: textdiff.Del, Left: "z", LeftNo: 4},
	))
	if rowOfNew(rows, 2) != 2 || rowOfOld(rows, 2) != 1 || rowOfNew(rows, 9) != -1 {
		t.Fatal("line ↔ row maps")
	}
	if rowNewLine(rows, 1) != 2 { // a deleted row: the next new line below
		t.Fatalf("deleted row → %d, want 2", rowNewLine(rows, 1))
	}
	if rowNewLine(rows, 3) != 2 { // at the end: the last one above
		t.Fatalf("last deleted row → %d, want 2", rowNewLine(rows, 3))
	}
}

func TestFoldDiffRowsKeepsContextAndKeptRows(t *testing.T) {
	t.Parallel()
	var src []textdiff.Row
	for i := 1; i <= 20; i++ {
		src = append(src, textdiff.Row{Kind: textdiff.Same, Left: "s", Right: "s", LeftNo: i, RightNo: i})
	}
	src[10] = textdiff.Row{Kind: textdiff.Changed, Left: "o", Right: "n", LeftNo: 11, RightNo: 11}
	all := unifiedDiffRows(rowsDiff(src...))
	keep := func(i int) bool { return all[i].nu == 2 } // a banded / noted line
	got := foldDiffRows(all, keep, nil)
	// row 0 (line 1) folds alone? no: 1 hidden row folds, but line 2 is kept,
	// so: fold(1) · 2 · fold(5: lines 3..7) · 8 9 10 · del 11 · add 11 · 12 13 14 · fold(6)
	var kinds []diffRowKind
	for _, r := range got {
		kinds = append(kinds, r.kind)
	}
	if got[0].kind != diffFold || got[0].hidden != 1 || got[1].nu != 2 || got[2].kind != diffFold || got[2].hidden != 5 {
		t.Fatalf("head = %+v %+v %+v", got[0], got[1], got[2])
	}
	if last := got[len(got)-1]; last.kind != diffFold || last.hidden != 6 {
		t.Fatalf("tail = %+v", last)
	}
	open := map[int]bool{got[2].at: true}
	if again := foldDiffRows(all, keep, open); len(again) != len(got)+4 {
		t.Fatalf("an opened fold shows its 5 rows: %d rows, want %d", len(again), len(got)+4)
	}
}

func TestDiffNoticeTexts(t *testing.T) {
	t.Parallel()
	changed := domain.ReviewFileDiff{Changed: true, Diff: rowsDiff(textdiff.Row{Kind: textdiff.Add, Right: "x", RightNo: 1})}
	for _, c := range []struct {
		res   domain.ReviewFileDiff
		err   error
		agent bool
		want  string
	}{
		{changed, nil, false, ""},
		{domain.ReviewFileDiff{}, nil, false, "no change to this file in the review — D shows the file"},
		{domain.ReviewFileDiff{}, nil, true, "no changes in the working tree — D shows the file"},
		{domain.ReviewFileDiff{Changed: true, Diff: domain.Diff{Binary: true}}, nil, false, "binary file — D shows the file"},
		{domain.ReviewFileDiff{Changed: true, Diff: domain.Diff{TooLarge: true}}, nil, false, "diff too large — D shows the file"},
		{domain.ReviewFileDiff{}, domain.ErrReviewNotFound, false, "the review is gone — D shows the file"},
		{domain.ReviewFileDiff{}, errors.New("boom"), false, "could not read the diff — D shows the file"},
	} {
		if got := diffNoticeText(c.res, c.err, c.agent); got != c.want {
			t.Fatalf("notice(%+v, %v) = %q, want %q", c.res.Changed, c.err, got, c.want)
		}
	}
}

func TestDiffTitleNamesTheRange(t *testing.T) {
	t.Parallel()
	res := domain.ReviewFileDiff{Left: "3a4d954", Right: "774ace5"}
	if got := diffTitle("a.go", res); got != "Diff a.go (3a4d954 → 774ace5)" {
		t.Fatalf("title %q", got)
	}
	res.OldPath = "old.go"
	if got := diffTitle("a.go", res); got != "Diff old.go → a.go (3a4d954 → 774ace5)" {
		t.Fatalf("rename title %q", got)
	}
}
```

(Check the sentinel's name: `grep -n "ErrReviewNotFound" internal/domain/*.go`; use whatever `ReviewFileDiff` returns for a gone review.)

- [ ] **Step 2: Run** — `go test ./internal/tui -run 'TestUnifiedDiffRows|TestRowMaps|TestFoldDiffRows|TestDiffNotice|TestDiffTitle'` → FAIL: undefined symbols.

- [ ] **Step 3: Implement** `split_diff_rows.go`:

```go
package tui

import (
	"errors"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

// The overview split's diff rows (spec 2026-10-11-overview-split-diff §4):
// a file's whole aligned diff as ONE column — a run's deleted lines, then
// its added ones, an unchanged line once. Pure: no Model, no keys. The
// cached diff's rows are shared read-only (domain.Diff), so every row here
// is a copy.

type diffRowKind uint8

const (
	diffSame diffRowKind = iota
	diffDel
	diffAdd
	diffFold // ⋯ N unchanged lines (f's changed-only view)
)

// diffRow is one row of the column; at is its index in the unfolded list
// (a fold's: its first hidden row), the key folds are opened by.
type diffRow struct {
	kind    diffRowKind
	old, nu int
	text    string
	raw     string
	cls     []syntax.Class
	words   []emphLevel
	hidden  int
	at      int
}

// unifiedDiffRows is d's rows as one column (splitdiff.js unifiedRows).
func unifiedDiffRows(d domain.Diff) []diffRow {
	src := d.Result.Rows
	var out []diffRow
	push := func(kind diffRowKind, old, nu int, s string, spans []textdiff.Span, tok [][]syntax.Tok, no int) {
		disp, emph, cls := sanitizeCell(s, spans, tokAt(tok, no))
		var words []emphLevel
		if len(spans) > 0 {
			words = emph
		}
		out = append(out, diffRow{kind: kind, old: old, nu: nu, text: string(disp), raw: s, cls: cls, words: words, at: len(out)})
	}
	for k := 0; k < len(src); {
		if r := src[k]; r.Kind == textdiff.Same {
			push(diffSame, r.LeftNo, r.RightNo, r.Right, nil, d.NewTok, r.RightNo)
			k++
			continue
		}
		j := k
		for j < len(src) && src[j].Kind != textdiff.Same {
			j++
		}
		for x := k; x < j; x++ {
			if r := src[x]; r.Kind != textdiff.Add {
				push(diffDel, r.LeftNo, 0, r.Left, r.LeftSpans, d.OldTok, r.LeftNo)
			}
		}
		for x := k; x < j; x++ {
			if r := src[x]; r.Kind != textdiff.Del {
				push(diffAdd, 0, r.RightNo, r.Right, r.RightSpans, d.NewTok, r.RightNo)
			}
		}
		k = j
	}
	return out
}

// rowOfNew is the row holding new line line, -1 when none does.
func rowOfNew(rows []diffRow, line int) int {
	for i, r := range rows {
		if r.nu == line && r.kind != diffFold {
			return i
		}
	}
	return -1
}

// rowOfOld is the row holding old line line, -1 when none does.
func rowOfOld(rows []diffRow, line int) int {
	for i, r := range rows {
		if r.old == line && r.kind != diffFold {
			return i
		}
	}
	return -1
}

// rowNewLine is the new line row i stands for: its own; a deleted row's or
// a fold's, the next new line below it, else the last one above; 0 = none.
func rowNewLine(rows []diffRow, i int) int {
	if i < 0 || i >= len(rows) {
		return 0
	}
	for k := i; k < len(rows); k++ {
		if rows[k].nu > 0 && rows[k].kind != diffFold {
			return rows[k].nu
		}
	}
	for k := i - 1; k >= 0; k-- {
		if rows[k].nu > 0 && rows[k].kind != diffFold {
			return rows[k].nu
		}
	}
	return 0
}

// diffContext is how many unchanged rows f keeps on each side of a change.
const diffContext = 3

// foldDiffRows is f's changed-only view of all: a change keeps diffContext
// unchanged rows on each side, keep(i) rows (a band's, a remark's) stay,
// every other run of unchanged rows becomes one fold row — unless open
// names it (by its first row's at).
func foldDiffRows(all []diffRow, keep func(i int) bool, open map[int]bool) []diffRow {
	show := make([]bool, len(all))
	for i, r := range all {
		if r.kind != diffSame || (keep != nil && keep(i)) {
			show[i] = true
			for k := max(i-diffContext, 0); k <= min(i+diffContext, len(all)-1); k++ {
				if r.kind != diffSame {
					show[k] = true
				}
			}
		}
	}
	var out []diffRow
	for i := 0; i < len(all); {
		if show[i] {
			out = append(out, all[i])
			i++
			continue
		}
		j := i
		for j < len(all) && !show[j] {
			j++
		}
		if open[all[i].at] {
			out = append(out, all[i:j]...)
		} else {
			out = append(out, diffRow{kind: diffFold, hidden: j - i, at: all[i].at, nu: all[i].nu, old: all[i].old})
		}
		i = j
	}
	return out
}

// diffShows is the suffix every notice ends in.
func diffShows(s string) string { return s + i18n.T(" — D shows the file") }

// diffNoticeText is what the column says instead of rows ("" = the rows).
func diffNoticeText(res domain.ReviewFileDiff, err error, agent bool) string {
	switch {
	case errors.Is(err, domain.ErrReviewNotFound):
		return i18n.T("the review is gone — D shows the file")
	case err != nil:
		return i18n.T("could not read the diff — D shows the file")
	case res.Diff.Binary:
		return i18n.T("binary file — D shows the file")
	case res.Diff.TooLarge:
		return i18n.T("diff too large — D shows the file")
	case !res.Changed || !rowsChangeTUI(res.Diff.Result.Rows):
		if agent {
			return i18n.T("no changes in the working tree — D shows the file")
		}
		return i18n.T("no change to this file in the review — D shows the file")
	}
	return ""
}

// rowsChangeTUI reports whether any aligned row changed.
func rowsChangeTUI(rows []textdiff.Row) bool {
	for _, r := range rows {
		if r.Kind != textdiff.Same {
			return true
		}
	}
	return false
}

// diffTitle is the column's title in diff mode.
func diffTitle(path string, res domain.ReviewFileDiff) string {
	name := path
	if res.OldPath != "" && res.OldPath != path {
		name = res.OldPath + " → " + path
	}
	return i18n.T("Diff %s (%s → %s)", name, res.Left, res.Right)
}

// sameText reports whether two files' lines are the same text.
func sameText(a, b []string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

Fix while implementing: `foldDiffRows`'s inner context loop must mark the neighbours of a CHANGE only (a kept unchanged row shows itself, no context): write it as `if r.kind != diffSame { mark i-3..i+3 } else if keep(i) { show[i] = true }`. Drop `diffShows` if unused (vet). The notice strings stay whole literals (i18n gate).

- [ ] **Step 4: Run** — the step-2 command → PASS. Fix the fold test's expectations against the rule if the comment's arithmetic is off by one (the rule is the authority: 3 context rows each side of a change, a kept row shows alone, ≥1 hidden row folds).

- [ ] **Step 5: Commit** — "tui: the overview split's diff rows — one column, row maps, f's folds, notices".

---

### Task 3: `openFile.diff` — the second popup, drawing it, the cursor mirror

**Files:**
- Create: `internal/tui/split_diff.go`
- Modify: `open_file.go` (field + `frameTitle`), `open_file_notes.go` (`gutterW`, `previewDoc`, `syncNoteRows`), `review_remarks.go` (`boxNote`), `anchor_bands.go` (none: row bands are a new method), `file_preview.go` (`renderPreviewBoxOpts` diff branch), `preview_select.go` (`activePreview`), `split_doc_pane.go` (`render`, `captures`, `update` scroll keys, `paneCharHint`), `split_view.go` (`focus`, `leaveChars`, mouse row click + wheel), `file_viewer.go` (`render`, `update` scroll keys), `open_files.go` (`detachDoc` drops the diff), `open_file.go` (`landPendingLine` follows into the diff)
- Test: `internal/tui/split_diff_test.go`

**Interfaces:**
- Consumes: Task 2's rows.
- Produces:

```go
type docDiff struct {
	p       *contentPopup // the rows as lines (cursor, search, v, marking)
	rows    []diffRow     // p.lines' rows (folded when partial)
	all     []diffRow     // unfolded
	res     domain.ReviewFileDiff
	notice  string
	agent   bool
	partial bool
	open    map[int]bool
	remarks []rowNote // the review's remarks, both sides
	numW    int       // the number column's width
}
type rowNote struct{ n agentdocs.Note; old bool }
func (d *openFile) shownP() *contentPopup
func (d *openFile) showDiff(res domain.ReviewFileDiff, err error, agent, partial bool, rows int)
func (d *openFile) dropDiff(rows int)
func (d *openFile) mirrorDiffCursor()           // d.p.cur ← the diff cursor's new line
func (d *openFile) followIntoDiff(rows int)     // the diff cursor ← d.p.cur's line, centred
func (d *openFile) rowBands() []anchorBand       // d.bands() on rows
func (d *openFile) rowNotes() []agentdocs.Note   // shown notes on rows (Start/End = row+1)
func (d *openFile) rowNoteOrigin(id string) (agentdocs.Note, bool)
func (d *openFile) refoldDiff(rows int)          // rebuild rows after f / a fold open, cursor line kept
```

Rules (write them as the file's top comment):
- `d.diff == nil` → the file shows. `d.p` is ALWAYS the file popup; `d.diff.p` is drawn instead while `d.diff != nil`.
- After every key / click the split or the viewer handles, `mirrorDiffCursor` puts `d.p.cur` on the cursor row's new line (a deleted row / fold: `rowNewLine`), `d.p.sel` so it stays visible — so `d.p.cur` is a file line everywhere.
- Anything that MOVES `d.p.cur` while the diff shows (`landPendingLine`: steer, n/p) calls `followIntoDiff`.
- `showDiff` with a notice builds one placeholder line (`src` false, `noCursor`): no cursor, nothing to mark.
- Flip keeps the screen row: file → diff puts the row of the cursor's line at the same distance from the top when it fits, else centred; diff → file the same with the mirrored line.

- [ ] **Step 1: Write the failing tests** (`split_diff_test.go`) — a repo with a change that deletes and adds, reviewed as one commit, opened as the split:

```go
package tui

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// diffSplitModel is a commit that edits a.txt (line 3 changed, line 5
// deleted, line 8 added at the end) and leaves b.txt alone, reviewed with a
// remark on new line 3 and one on old line 5, its overview anchoring
// a.txt:3 then b.txt:1 — opened as the split, focus on the file.
func diffSplitModel(t *testing.T) (Model, *splitView) {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-b", "main")
	write("a.txt", "l1\nl2\nl3\nl4\nl5\nl6\nl7\n")
	write("b.txt", "b1\nb2\n")
	write("c.txt", "b1\nb2\n") // the same text as b.txt
	git("add", ".")
	git("commit", "-m", "c0")
	write("a.txt", "l1\nl2\nL3\nl4\nl6\nl7\nl8\n")
	git("add", ".")
	git("commit", "-m", "c1")

	m := New(domain.New(testRepo(t, dir)))
	m.width, m.height = 160, 40
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	m.svc.UseNotesDir(t.TempDir())
	sha := m.commits[0].Hash
	ov, _ := json.Marshal("Look at [three](a.txt:3), then [b](b.txt:1), then [c](c.txt:1).")
	doc := `{"version":1,"summary":"## Summary\nok","overview":` + string(ov) + `,"files":[{"path":"a.txt","annotations":[{"newRange":[3,3],"summary":"why L3?"},{"oldRange":[5,5],"summary":"why drop l5?"}]}]}`
	tg := domain.ReviewTarget{Kind: domain.ReviewRange, Range: sha + "^.." + sha, Label: shortHash(sha)}
	id, _, err := m.svc.SaveReview(context.Background(), domain.SaveReview{Target: tg, Agent: "Claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	m.sel[panelCommits] = 0
	m, cmd := m.openChangedFiles(m.commits[0])
	m = drainCmds(t, m, cmd)
	m = tempPromptStore(t, m)
	m, cmd = m.openReview(id, "review")
	m = drainCmds(t, m, cmd)
	u2, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u2.(Model), cmd)
	sv := m.splitLayer()
	if sv == nil {
		t.Fatal("no split")
	}
	m = pressDrain(t, m, "enter") // focus the file
	return m, sv
}

func TestDiffShowsRowsAndKeepsTheCursorLineBothWays(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	d := sv.right.doc()
	if d.p.cur != 2 {
		t.Fatalf("file cursor %d, want line 3", d.p.cur+1)
	}
	m = pressDrain(t, m, "D")
	if d.diff == nil || d.diff.notice != "" {
		t.Fatalf("diff = %+v", d.diff)
	}
	r := d.diff.rows[d.diff.p.cur]
	if r.kind != diffAdd || r.nu != 3 {
		t.Fatalf("cursor row %+v, want the added line 3", r)
	}
	if !strings.Contains(d.frameTitle(), "Diff a.txt (") {
		t.Fatalf("title %q", d.frameTitle())
	}
	m = pressDrain(t, m, "alt+up") // onto the deleted l3 row
	if k := d.diff.rows[d.diff.p.cur].kind; k != diffDel || d.p.cur != 2 {
		t.Fatalf("on %v the mirrored file line is %d, want 3 (the next new line)", k, d.p.cur+1)
	}
	m = pressDrain(t, m, "D")
	if d.diff != nil || d.p.cur != 2 {
		t.Fatalf("back on the file: diff %v cursor line %d", d.diff, d.p.cur+1)
	}
	_ = m
}

func TestDiffDrawsSignsTintsBandAndBothSidesRemarks(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	d := sv.right.doc()
	m = pressDrain(t, m, "D")
	screen := plainScreen(m) // ansi-stripped View(); see split tests' helper
	for _, want := range []string{"− l3", "+ L3", "− l5", "+ l8", "why L3?", "why drop l5?", "file | diff"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("screen lacks %q:\n%s", want, screen)
		}
	}
	bs := d.rowBands()
	if len(bs) != 1 || d.diff.rows[bs[0].start-1].nu != 3 {
		t.Fatalf("row bands %+v: the link's line 3 is banded on its added row", bs)
	}
}
```

(`plainScreen`: use the split tests' existing way of reading the screen — `ansi.Strip(m.View())` as `split_view_test.go` does; name the helper after what exists.)

- [ ] **Step 2: Run** — `go test ./internal/tui -run 'TestDiffShowsRows|TestDiffDrawsSigns'` → FAIL (D does nothing; `rowBands` undefined).

These two tests need Task 4's D key to go green; in this task add a TEST-ONLY seam instead of D: in the tests replace `pressDrain(t, m, "D")` by `m = flipForTest(t, m, d)` where the helper (in the test file) calls `m.svc.ReviewFileDiff` and `d.showDiff(res, err, false, false, m.docRows(d))` — and the second `D` by `d.dropDiff(m.docRows(d))`. Task 4 swaps them back to the key.

- [ ] **Step 3: Implement `split_diff.go`** — `docDiff`, `rowNote`, `shownP`, `showDiff`, `dropDiff`, `mirrorDiffCursor`, `followIntoDiff`, `rowBands`, `rowNotes`, `rowNoteOrigin`, `refoldDiff`, plus `diffLines(rows) []contentLine` (one `contentLine{text, raw, src: true, cls}` per row; a fold `{text: i18n.T("⋯ %d unchanged lines", n), dim: true}` with `src` false) and `numW` = digits of the largest number + 2 (" −"). Key code:

```go
// shownP is the popup a frame draws and the keys move: the diff's rows
// while they show, the file otherwise.
func (d *openFile) shownP() *contentPopup {
	if d.diff != nil {
		return d.diff.p
	}
	return d.p
}

// showDiff puts res on screen (or its notice) in place of the file, the
// cursor on the row of the line it is on now, on the same screen row when
// that fits. A diff already showing is replaced the same way (a reload).
func (d *openFile) showDiff(res domain.ReviewFileDiff, err error, agent, partial bool, rows int) {
	line, off := d.p.cur+1, d.p.cur-d.p.sel
	var mode dispMode = d.p.mode
	var open map[int]bool
	if old := d.diff; old != nil {
		line, off, mode, open = rowNewLine(old.rows, old.p.cur), old.p.cur-old.p.sel, old.p.mode, old.open
		if old.p.cs.on {
			old.p.previewCharLeave()
		}
	}
	dd := &docDiff{res: res, agent: agent, partial: partial, notice: diffNoticeText(res, err, agent), open: open}
	if dd.open == nil {
		dd.open = map[int]bool{}
	}
	dd.p = &contentPopup{title: d.path, charWrap: true, mode: mode}
	if dd.notice != "" {
		dd.p.lines = []contentLine{{text: dd.notice}}
		dd.p.noCursor = true
		d.diff = dd
		return
	}
	dd.all = unifiedDiffRows(res.Diff)
	for _, n := range res.Notes {
		dd.remarks = append(dd.remarks, rowNote{n: remarkBoxes([]domain.ResolvedNote{n})[0], old: n.Note.Side == model.NoteSideOld})
	}
	d.diff = dd
	d.layDiff()
	r := max(rowOfNew(dd.rows, line), 0)
	dd.p.cur = r
	dd.p.sel = dd.p.clampTop(r-off, rows)
	if r < dd.p.sel || r > dd.p.lastVisible(rows) {
		dd.p.sel = dd.p.clampTop(r-rows/2, rows)
	}
}
```

(`layDiff` = rows from `all` (folded when `partial`, `keep` = a row inside a row band or under a note), lines, `numW`, `extraRows = d.diffNoteRowsUnder`. Check the remark's side field name: `grep -n "Side" internal/model/note*.go` — use the real one. A deleted row never matches `rowOfNew`: `max(…, 0)` lands on the top; use `rowOfNew` of the next line that has a row when `line` has none — write it as a small loop.)

```go
// dropDiff puts the file back, its cursor on the diff cursor's new line on
// the same screen row.
func (d *openFile) dropDiff(rows int) {
	dd := d.diff
	if dd == nil {
		return
	}
	if dd.p.cs.on {
		dd.p.previewCharLeave()
	}
	off := dd.p.cur - dd.p.sel
	d.mirrorDiffCursor()
	d.diff = nil
	d.p.mode = dd.p.mode
	d.p.sel = d.p.clampTop(d.p.cur-off, rows)
	d.syncNoteRows()
}

// mirrorDiffCursor keeps d.p.cur on the diff cursor's new line, so every
// reader of d.p.cur reads a file line (the web's view.cur rule).
func (d *openFile) mirrorDiffCursor() {
	dd := d.diff
	if dd == nil || dd.notice != "" {
		return
	}
	if line := rowNewLine(dd.rows, dd.p.cur); line > 0 && line <= len(d.p.lines) {
		d.p.cur = line - 1
	}
}

// followIntoDiff puts the diff cursor on d.p.cur's row, centred — after
// something moved the file cursor (a steer, n / p).
func (d *openFile) followIntoDiff(rows int) {
	dd := d.diff
	if dd == nil || dd.notice != "" {
		return
	}
	if r := rowOfNew(dd.rows, d.p.cur+1); r >= 0 {
		dd.p.cur = r
		dd.p.sel = dd.p.clampTop(r-rows/2, rows)
		dd.p.ensureCursorVisible(rows)
	}
}
```

`rowBands`: map `d.bands()` (file space; `d.bands()` needs the FILE loaded — a deleted file has no bands, fine) through `rowOfNew` for start and end (skip a band whose start or end has no row); same `i`. `rowNotes`: `d.notes` (agent, new side) + `dd.remarks` (each side) → `Start/End` = first/last row whose number on its side lies in `[Start, End]`, `+1`; a note with no row is left out; sorted by Start (stable). `rowNoteOrigin(id)` finds the note by ID among `d.notes` and `dd.remarks` (box titles keep the file's line numbers).

- [ ] **Step 4: Wire the frame and key paths through `shownP()`**:
  - `open_file.go`: field `diff *docDiff` (comment: "the review diff shown in place of the file (split_diff.go); nil = the file"); `frameTitle`: `if d.diff != nil { return diffTitle(d.path, d.diff.res) }` first (a notice keeps the diff title too).
  - `open_file_notes.go` `gutterW`: `if d.diff != nil { if d.diff.notice != "" { return 0 }; return noteGutterW + d.diff.numW }`. `previewDoc`: each `d.p == p` test also accepts `d.diff != nil && d.diff.p == p`. `syncNoteRows`: when `d.diff != nil` also set `d.diff.p.extraRows = d.diffNoteRowsUnder` (row-space boxes, `noteBoxLines(origin…)` heights).
  - `review_remarks.go` `boxNote`: `if d.diff != nil { if o, ok := d.rowNoteOrigin(n.ID); ok { return o } }` first.
  - `preview_select.go` `activePreview`: split branch returns `d.shownP()`; viewer branch `fv.shownP()`.
  - `split_doc_pane.go`: `captures` reads `p.d.shownP()`; `render` passes `p.d.shownP()`; `update`'s `pp := d.shownP()`; `paneCharHint` reads `d.shownP()`.
  - `split_view.go`: `focus`/`leaveChars` test `d.shownP().cs.on`; `splitMousePress` wheel `d.shownP().scrollBy`, row click `sp := d.shownP(); sp.lineAtRow(…); sp.cur = line`.
  - `file_viewer.go`: `render` passes `fv.shownP()`; `update`'s `p := fv.shownP()`.
  - `open_files.go` `detachDoc`: first line `d.dropDiff(m.docRows(d))` — a document leaving its frame shows the file (spec §6: a file opened any other way shows as the file); the char-mode check below reads `d.shownP()`.
  - `open_file.go` `landPendingLine`: after the cursor lands (`p.cur = line - 1` path), `d.followIntoDiff(rows)`.
  - `file_preview.go` `renderPreviewBoxOpts`: when `d := m.previewDoc(p); d != nil && d.diff != nil && d.diff.p == p` — `notes = d.rowNotes()`, `bands = d.rowBands()`, `curB = d.curBand(d.bands())`, `gut = d.gutterW()`, `boxNote = d.boxNote`; per row `i` (`dr := d.diff.rows[row]`): `r.prefix = padRight(gutterMark(kind, noted), noteGutterW) + diffNum(dr, d.diff.numW)` (`diffNum`: right-aligned number — new, or old for a deleted row — then " −" / " +" / "  "); base style `st().diffDelCell` / `st().diffAddCell` for del / add rows, the cursor row `st().diffDelCursor` / `st().diffAddCursor` (`previewRowMark` gains a `tint lipgloss.Style, tinted bool` argument used as the starting style; a band paints only an unchanged row — a changed row keeps its tint, its band shows in the gutter mark); `r.emph = dr.words` before the search overlay (`overlayHits(r.emph, …)`). The notice popup (`d.diff.notice != ""`) draws as today (one plain line, no gutter).

- [ ] **Step 5: Run** — step-2 command → PASS; then `go test ./internal/tui` → PASS (no regressions: every changed call site reads `d.p` when `d.diff == nil`).

- [ ] **Step 6: Commit** — "tui: an open file can show its review diff — a second popup over the same document, rows mapped to lines".

---

### Task 4: D, f, the read, the sticky choice, the title switch

**Files:**
- Modify: `split_diff.go` (keys + read), `overview.go` (`overview` struct fields), `split_view.go` (`updateKey` D, `update` mirror, `splitMouse` mirror + title click, `narrowSplits` keep), `split_overview.go` (`docSteer` old doc drops + diff on a loaded steer; `onClose` forgets; `retire`), `split_doc_pane.go` (`update` calls `diffKey`), `file_viewer.go` (`update` wrapper with the mirror, `diffKey`), `open_files.go` (`backgroundDoc`/`closeDoc` of an overview forget), `model.go` (`reviewDiffMsg` case; fill → diff read), `file_preview.go` (title switch via `previewBoxOpts`)
- Test: `split_diff_test.go`, `split_diff_keys_test.go`

**Interfaces:**
- Consumes: Task 3.
- Produces:

```go
// on the overview (overview.go):
//   diff, diffPartial bool  — the choice D / f made for this overview's files
func (ov *overview) forgetDiff()
func (m Model) canFlip(d *openFile) bool          // D acts on d here
func (m Model) diffWanted(d *openFile) bool       // canFlip && d.from.ov.diff
func (m Model) flipDiff(d *openFile) (Model, tea.Cmd)
func (m Model) diffKey(d *openFile, msg tea.KeyMsg) (Model, tea.Cmd, bool) // f, enter on a fold
func (m Model) diffReadCmd(d *openFile) tea.Cmd
type reviewDiffMsg struct{ slotStamp; tag string; seq int; agent bool; res domain.ReviewFileDiff; err error }
func (m Model) diffLanded(msg reviewDiffMsg) Model
func diffSwitch(on bool) string                    // "file | diff", the half in force reversed
func switchHalf(col, innerW int) int              // 0 none, 1 file, 2 diff — col 0-based inside the box text
```

Rules:
- `canFlip(d)`: `d.stream == nil`, `d.from != nil && d.from.ov != nil && !d.from.ov.closed`, and d is the right `*docPane`'s document of a split whose left document is `d.from` — or the document of a `fileViewer` on the stack (the narrow viewer).
- `flipDiff`: not `canFlip` → if `d.diff != nil` drop it (no stuck diff once the overview is gone), else nothing. Otherwise toggle `ov.diff`; off → `dropDiff`; on → `d.loading` ? nothing (the fill reads it) : `diffReadCmd(d)`. A loaded placeholder (a file the review deleted) reads too — never gate on `docLoaded`.
- `diffReadCmd`: `d.diffSeq++`; stored overview (`d.from.src.kind == srcReviewOverview`) → `svc.ReviewFileDiff(ctx, d.from.src.rev, d.path)`; agent → `svc.WorkingFileDiff(ctx, d.path)`.
- `diffLanded`: `d := m.openFiles.findTag(msg.tag)`; drop when nil, `msg.seq != d.diffSeq`, a stale slot, or `!m.diffWanted(d)` (the choice is read when the answer lands — a D pressed meanwhile wins). Otherwise `d.showDiff(msg.res, msg.err, msg.agent, d.from.ov.diffPartial, m.docRows(d))`.
- Fill (`model.go` fileContentMsg, the non-overview branch): `tea.Batch(m.reviewNotesCmd(d), m.diffOnLoad(d))` where `diffOnLoad` = `diffReadCmd` when `d.diff != nil || m.diffWanted(d)` (a reload re-reads; the shown diff stays until the new one lands).
- `docSteer`: the replaced document `old.dropDiff(rows)`; a loaded landing returns `tea.Batch(m.remarksOnSteer(d), m.diffOnLoad(d))` when `d.diff == nil` (same file with the diff showing: `landPendingLine` already followed into it).
- Forgetting: `openOverviewSplit`'s `onClose` → `ov.ov.forgetDiff()` and `rd.dropDiff`; `sv.retire()` drops both panes' diffs and forgets; `narrowSplits` saves `ov.ov.diff, diffPartial` before `closeSplit` and restores them (spec §6: a resize keeps it); `backgroundDoc` / `closeDoc` of a document with `d.ov != nil` → `d.ov.forgetDiff()` (the narrow overview viewer's esc / ctrl+] / X).
- Keys: `splitView.updateKey` `case "D":` (either side, after `captures`) → `if r, ok := sv.right.(*docPane); ok && r.d != nil { return m.flipDiff(r.d) }`; `docPane.update` and `fileViewer.update` call `diffKey` after the modal hooks (char, select, search) and before `previewNoteKey`; `fileViewer`'s `diffKey` also takes D. `diffKey`: `f` with the diff showing → toggle `d.from.ov.diffPartial`, `d.diff.partial`, `refoldDiff`; `enter` on a fold row → `d.diff.open[row.at] = true`, `refoldDiff`. Declined otherwise.
- Mirror: `splitView.update` and `splitMouse` call `d.mirrorDiffCursor()` on the right document before `syncOverviewHere`; `fileViewer.update` becomes a wrapper (`updateKey` + mirror).
- Title switch: `previewBoxOpts` gains `sw string`; `docPane.render` and `fileViewer.render` pass `diffSwitch(d.from.ov.diff)` when `m.canFlip(d)`; `renderPreviewBoxOpts` appends it at the right end of `right` (`right = strings.TrimSpace(right + "  " + o.sw)`). `splitMousePress`: a left click on row `msg.Y == 1` of the right side whose `switchHalf(x-2, inner)` names the half NOT in force → focus right, `flipDiff`. The narrow viewer draws the switch; its title is not clickable (spec §9 names splitMouse only — ruling).

- [ ] **Step 1: Write the failing tests** (`split_diff_keys_test.go`; swap Task 3's `flipForTest` seam back to `pressDrain(t, m, "D")`):

```go
func TestDStaysWhileSteppingAndIsForgottenOnClose(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	m = pressDrain(t, m, "D")
	m = pressDrain(t, m, "N") // the overview's next anchor: b.txt, which the review left alone
	d := sv.right.doc()
	if d.path != "b.txt" || d.diff == nil || d.diff.notice != "no change to this file in the review — D shows the file" {
		t.Fatalf("stepped to %s diff %+v", d.path, d.diff)
	}
	m = pressDrain(t, m, "esc")
	if ov := sv.left.doc(); ov.ov.diff || d.diff != nil {
		t.Fatal("closing the split forgets the choice and drops the diff")
	}
}

func TestADPressedWhileTheStepLoadsWins(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	u, cmd := m.Update(keyMsg("N")) // the step's load is out, not landed
	m = u.(Model)
	m = pressDrain(t, m, "D") // pressed while loading
	m = drainCmds(t, m, cmd)  // the load lands, then the diff read
	if d := sv.right.doc(); d.path != "b.txt" || d.diff == nil {
		t.Fatalf("right %s diff %v: the D pressed during the load must win", d.path, d.diff)
	}
}

func TestFFoldsButKeepsTheBandedAndRemarkedRows(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	m = pressDrain(t, m, "D")
	m = pressDrain(t, m, "f")
	d := sv.right.doc()
	if !d.diff.partial || !sv.left.doc().ov.diffPartial {
		t.Fatal("f is the overview's choice too")
	}
	if rowOfNew(d.diff.rows, 3) < 0 || rowOfOld(d.diff.rows, 5) < 0 {
		t.Fatal("the banded line and the remarked deleted line are never folded")
	}
}

func TestNoDOnceTheOverviewIsGone(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	d := sv.right.doc()
	sv.left.doc().ov.closed = true
	m = pressDrain(t, m, "D")
	if d.diff != nil || sv.left.doc().ov.diff {
		t.Fatal("D does nothing for a gone overview")
	}
}

func TestTitleSwitchClickFlips(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	lw, rw, _ := m.splitGeom()
	_, inner := paneGeom(rw, 0)
	x := lw + 2 + inner - 2 // the "diff" half: the last columns of the title
	u, cmd := m.Update(tea.MouseMsg{X: x, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = drainCmds(t, u.(Model), cmd)
	if sv.right.doc().diff == nil {
		t.Fatal("a click on diff flips")
	}
	u, cmd = m.Update(tea.MouseMsg{X: x, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = drainCmds(t, u.(Model), cmd)
	if sv.right.doc().diff == nil {
		t.Fatal("a click on the half in force does nothing")
	}
}

func TestNarrowResizeKeepsTheChoice(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	m = pressDrain(t, m, "D")
	ov := sv.left.doc()
	u, cmd := m.Update(tea.WindowSizeMsg{Width: 50, Height: 40})
	m = drainCmds(t, u.(Model), cmd)
	if !ov.ov.diff {
		t.Fatal("a resize between split and viewer keeps the choice")
	}
	m = pressDrain(t, m, "enter") // the overview viewer opens its selected anchor
	if d := topDoc(m); d == nil || d.diff == nil {
		t.Fatal("the narrow viewer opens the link as a diff")
	}
}
```

Also an agent-overview test in `split_diff_keys_test.go`: `agentOverviewModel(t, 160)` with a.txt edited on disk vs HEAD → D shows `Diff a.txt (HEAD → working tree)`; b.txt unchanged → `no changes in the working tree — D shows the file`. (Read `loadedNavModel`'s fixture: commit a.txt, then write a change before D.)

- [ ] **Step 2: Run** — `go test ./internal/tui -run 'TestDStays|TestADPressed|TestFFolds|TestNoDOnce|TestTitleSwitch|TestNarrowResize|TestDiffShows|TestDiffDraws|TestAgentOverviewDiff'` → FAIL (D/f/click do nothing).

- [ ] **Step 3: Implement** per the Rules above (`overview.go`: `diff, diffPartial bool` + `forgetDiff()` that also clears Task 5's step record; `openFile.diffSeq int`).

- [ ] **Step 4: Run** — step-2 command → PASS; `go test ./internal/tui` → PASS.

- [ ] **Step 5: Commit** — "tui: D flips the overview split's file to its review diff — sticky per overview, f folds, the title switch clicks".

---

### Task 5: Keys that read a line — links, notes, n/p, the mark

**Files:**
- Modify: `open_file_note_keys.go` (`previewNoteKey`, `stepFileNote`, line 162), `file_link.go` (`fileRowPath`, `contextFileLinkRow`), `preview_select.go` (`L`), `split_here.go` (nothing — the mirror makes it right; add the test), `anchor_bands.go` (`stepAnchorBand`: nothing — `landPendingLine` follows)
- Test: `split_diff_keys_test.go`

Rules:
- Note keys in a diff read the ROW: `noteAtCursor(d)` = in a diff the row note under the diff cursor row (`rowNotes`, mapped back by `rowNoteOrigin`), else `d.noteAt(d.p.cur+1)`; d / r on a remark say read-only, enter opens the whole note (old-side too). `stepFileNote` in a diff walks `rowNotes` from the diff cursor and moves the diff cursor (then mirrors).
- Links: `fileRowPath` on a deleted / fold row (`d.diff != nil` and the cursor row has no new number) → the file's link with no line (`line 0`); `contextFileLinkRow` with a marking in a diff maps the marked rows to the new lines they cover (first and last row with a new number inside the marking); none → the row is left out and `L` says `a deleted line has no link in the reviewed version`.
- The overview's mark: on a deleted row it marks the band of the next new line (the mirror already puts `d.p.cur` there).

- [ ] **Step 1: Write the failing tests**:

```go
func TestOldSideRemarkIsReachableAndReadOnly(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	m = pressDrain(t, m, "D")
	d := sv.right.doc()
	d.diff.p.cur = rowOfOld(d.diff.rows, 5)
	m = pressDrain(t, m, "d")
	if !strings.Contains(m.statusMsg, "read-only") {
		t.Fatalf("d on the deleted line's remark: %q", m.statusMsg)
	}
	m = pressDrain(t, m, "}") // to the next box: none after l5's — stays or moves to a later file; the old-side one is reachable by {
	m = pressDrain(t, m, "{")
	if r := d.diff.rows[d.diff.p.cur]; r.old != 5 && r.nu != 3 {
		t.Fatalf("{ lands on a remark's row, got %+v", r)
	}
}

func TestMarkingDeletedRowsOnlyRefusesTheLink(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	m = pressDrain(t, m, "D")
	d := sv.right.doc()
	d.diff.p.cur = rowOfOld(d.diff.rows, 5)
	m = pressDrain(t, m, " ")
	m = pressDrain(t, m, " ")
	m = pressDrain(t, m, "L")
	if m.statusMsg != "a deleted line has no link in the reviewed version" {
		t.Fatalf("L = %q", m.statusMsg)
	}
}

func TestOverviewMarkFollowsADeletedRowToTheNextLine(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	m = pressDrain(t, m, "D")
	d := sv.right.doc()
	m = pressDrain(t, m, "alt+up") // the deleted l3 row above the banded L3
	if d.diff.rows[d.diff.p.cur].kind != diffDel || d.p.cur != 2 {
		t.Fatalf("row %+v file line %d", d.diff.rows[d.diff.p.cur], d.p.cur+1)
	}
	// the selected anchor's own band: no mark (R2) — the mirror kept line 3
	if sv.left.doc().ov.here() != -1 {
		t.Fatal("the selected anchor itself is never marked")
	}
}
```

- [ ] **Step 2: Run** — `go test ./internal/tui -run 'TestOldSideRemark|TestMarkingDeletedRows|TestOverviewMarkFollows'` → FAIL.

- [ ] **Step 3: Implement** per the Rules.

- [ ] **Step 4: Run** — step-2 command → PASS; `go test ./internal/tui` → PASS.

- [ ] **Step 5: Commit** — "tui: in the split's diff, links, note keys and the overview's mark read the row's new line".

---

### Task 6: Same content as the previous link's file (D6)

**Files:**
- Modify: `overview.go` (step record), `split_overview.go` (`steerSplit` / `docSteer` begin a step; loaded landing compares), `overview_keys.go` (`openAnchor` / `anchorStatted` begin a step in the narrow viewer), `model.go` (fill → `stepContent`)
- Test: `split_diff_keys_test.go`

Rules: the overview keeps `step struct{ prev, cur stepFile }` (`stepFile{path, tag string; text []string; known bool}`). A step (steer / narrow open) sets `prev = cur`, `cur = {path, tag}`. `stepContent(d)` — when the step's file content is in (`docLoaded(d)`, the FILE popup's raw lines; a placeholder is no content) — fills `cur.text` once and, when `prev.known`, `prev.path != cur.path` and `sameText(prev.text, cur.text)`, sets `m.statusMsg = i18n.T("same content as %s", elidePath(prev.path, 48))` (after `steerSplit`'s anchor hint, so it wins). `forgetDiff` clears the record.

- [ ] **Step 1: Write the failing test**:

```go
func TestSameContentAsThePreviousLinksFile(t *testing.T) {
	t.Parallel()
	m, _ := diffSplitModel(t)
	m = pressDrain(t, m, "N") // b.txt
	if strings.Contains(m.statusMsg, "same content") {
		t.Fatalf("a.txt → b.txt differ: %q", m.statusMsg)
	}
	m = pressDrain(t, m, "N") // c.txt: b.txt's text
	if m.statusMsg != "same content as b.txt" {
		t.Fatalf("status %q", m.statusMsg)
	}
}
```

- [ ] **Step 2: Run** — `go test ./internal/tui -run TestSameContentAsThePrevious` → FAIL.
- [ ] **Step 3: Implement** per the Rules.
- [ ] **Step 4: Run** → PASS; `go test ./internal/tui` → PASS.
- [ ] **Step 5: Commit** — "tui: a link step to a file with the previous step's text says same content as …".

---

### Task 7: Hints, help, translations

**Files:**
- Modify: `split_doc_pane.go` (`hint`), `file_preview.go` (the viewer's `[bksp] back` lead), `help.go` (rows by 309–310), `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: the existing i18n gates (`go test ./internal/tui -run 'I18n|Vocab|MenuLabels|EngineProse'`), plus one hint test.

Strings (each a literal key, all four bundles): `[D] diff`, `[D] file`, `[f] changed only`, `[f] whole file`, `file`, `diff`, `Diff %s (%s → %s)`, `⋯ %d unchanged lines`, the six notices, `a deleted line has no link in the reviewed version`, `same content as %s`, and the help rows:
- `D` — `in a file an overview's anchor opened (the split's right side, or the narrow viewer): flip between the file at the reviewed version and the review's diff of it — one column, deleted lines −, added +, the link's lines banded, the review's remarks under their lines on both sides; the choice holds while you step links until D again or the overview closes; a click on file | diff in the title flips too`
- `f` — `in that diff: only the changed lines (3 lines of context; a banded or remarked line is never folded; enter opens a ⋯ fold), f again for the whole file`

- [ ] **Step 1: Failing test**:

```go
func TestSplitHintListsD(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	if h := sv.right.hint(m); !strings.Contains(h, "[D] diff") {
		t.Fatalf("hint %q", h)
	}
	m = pressDrain(t, m, "D")
	if h := sv.right.hint(m); !strings.Contains(h, "[D] file") || !strings.Contains(h, "[f] changed only") {
		t.Fatalf("diff hint %q", h)
	}
}
```

- [ ] **Step 2: Run** → FAIL. **Step 3:** implement the hints (lead with them after `[n/p] anchors`), the viewer lead, the help rows; add every new key to the four bundles (translate each; keep `%s`/`%d` verbs and `D`/`f` literal). **Step 4:** run the hint test and the i18n gates → PASS; `go test ./internal/tui` → PASS. **Step 5:** commit "tui: D and f in the split's hints and help, in four languages".

---

### Task 8: e2e golden screen

**Files:**
- Create: `e2e/scenarios/tui_overview_split_diff.toml` (+ its `.screens/` via `-update`)

Scenario: `a.txt` committed, then a commit changing line 2 and deleting line 4 plus adding `b.txt`?? — keep it to the Task 3 fixture's shape: c0 adds `a.txt` (`one…five`) and `b.txt`; c1 edits `a.txt` (`two`→`TWO`, drops `four`); `gg review save gg://{{cwd}}@{{rev:HEAD}}` with an overview `Start at [two](a.txt:2), then [b](b.txt:1).` and a remark `newRange [2,2]`. Steps: `files` (right, enter) → `review` (down, enter) → `split` (down, enter; wait) → `focus` (enter) → `diff` (`D`; wait; `screen_contains = ["Diff a.txt (", "file | diff", "− two", "+ TWO", "− four"]`) → `step` (`N`; wait; `screen_contains = ["no change to this file in the review — D shows the file"]`) → `close` (`esc`; `["≡ Overview"]`) → `reopen` (`enter`; wait; `["View a.txt @"]`).

- [ ] **Step 1:** write the scenario; run `go test ./e2e -run 'TestScenarios/tui_overview_split_diff' -update` (check the e2e runner's flag and test name in `e2e/*_test.go`), read every written screen, confirm the `diff` screen shows the signs, the band gutter and the switch.
- [ ] **Step 2:** run without `-update` → PASS.
- [ ] **Step 3:** commit "e2e: the overview split's diff mode, golden screens".

---

### Task 9: Docs, the gg-overview skill, version

**Files:**
- Modify: `CHANGELOG.md`, `README.md` (the overview split paragraph), `docs/CLAUDE-details.md` (the review overview split section: a TUI "file ⇄ review diff" paragraph — `d.diff` second popup, the `d.p.cur` mirror, `shownP`, the choice on `ov.ov`, `WorkingFileDiff`), `internal/agentskill/gg-overview.md` (step 5's parenthesis: "…the file follows; D flips the file to the review's diff (gg web too), a note anchor shows your note…"), `internal/agentskill/agentskill.go` (`GGOverviewVersion` 5 → 6), `.claude/skills/gg-overview/SKILL.md` (regenerated)
- [ ] **Step 1:** edit the docs and the skill; bump the version.
- [ ] **Step 2:** regenerate the repo's skill copy: `go run ./cmd/gg init --update` in the worktree (or the command the agentskill tests name — `grep -rn "claude/skills" internal/agentskill/*_test.go internal/agentinit/*.go`); check `git diff .claude/skills/gg-overview/SKILL.md` shows only the sentence and the version.
- [ ] **Step 3:** `go test ./internal/agentskill ./internal/agentinit` → PASS.
- [ ] **Step 4:** commit "docs+skill: the TUI split's file / review diff".

---

### Final

- `./test.sh race` with the four `GG_*` vars unset → "all green".
- `./build.sh` in the worktree → `bin/gg`; `./tui-capture.sh` against it on a scratch repo with a saved review: the split, D, the diff screen, N to an unchanged file, esc — read the snapshots for what is VISIBLE (signs, tints as text can show, the switch, the notice).
- Whole-branch review by one read-only subagent (opus), then the fix pass, then ask the user before merging.
