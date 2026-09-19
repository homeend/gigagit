# Saved Commit Pairs Implementation Plan

> **For agentic workers:** executed INLINE by the session that wrote it
> (superpowers:executing-plans). This repo forbids subagents. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** save the diff between two commits as a named entry and show it in the TUI Previews tab beside merge previews.

**Architecture:** no new store. A saved pair is a set-shaped `savedcompare.Entry{Left: gg://<repo>@<A40>..<B40>, Right: nil}` — the shape a merge preview already has. A new `domain/pair.go` is the twin of `domain/preview.go` (producer + recogniser); the TUI's `previewRow` becomes a two-kind row; `gg preview` grows a pair arm.

**Tech Stack:** Go 1.26, Bubble Tea TUI, real-git tests in `t.TempDir()`, TOML e2e scenarios.

**Spec:** `docs/superpowers/specs/2026-09-19-saved-commit-pairs-design.md` (rev 2)

## Global Constraints

- A pair is **two FULL shas frozen at save time** (P2). A branch name passed to `PairAdd` is resolved THERE and never stored.
- Save gesture = `m` + `m` then the `.` menu (P6). `space` is untouched.
- `internal/tui` and `internal/cli` never import `internal/git` or `internal/savedcompare` — domain only.
- Every user-visible TUI string goes through `i18n.T` with a literal key present in ALL FOUR bundles (ja/ko/zh/ru). CLI/engine prose stays English.
- New tests call `t.Parallel()`; TUI tests build repos via `testRepo(t, dir)`; tests that `t.Setenv` stay serial.
- Do NOT touch `internal/cli/compare.go`, the web frontend, or MCP (plans 3b/3c own them).
- Every guard is watched failing **with the guard removed**, not the feature.
- Every command is prefixed `cd /mnt/t/others/gigagit/.claude/worktrees/saved-pairs &&` (the shell cwd resets); Write/Edit use that absolute prefix.
- Commit after each task with the session's `Co-Authored-By` / `Claude-Session` trailers. Never `git add -A`.
- No commit may leave a surface dark: domain lands before its consumers, and nothing existing changes meaning until Task 4.

## File Structure

| file | responsibility |
|---|---|
| `internal/domain/pair.go` (new) | `CommitPair`, `pairFromEntry`, `PairAdd/List/Get/Rename/Remove`, `PairSummary`, `PairOpen` |
| `internal/domain/pair_test.go` (new) | all domain tests for the above |
| `internal/cli/preview.go` | the pair arm on add / list / show / rm / rename / diff |
| `internal/cli/preview_pair_test.go` (new) | CLI tests |
| `e2e/scenarios/s96_saved_pairs.toml` (new; s95 is the last taken) | end-to-end |
| `internal/tui/preview_panel.go` | two-kind `previewRow`, accessors, `readPreviews`, row text |
| `internal/tui/preview_actions.go`, `preview_open.go`, `model.go`, `link.go` | every `selectedPreview()` consumer handles a pair |
| `internal/tui/pair_save.go` (new) | the two Commits-panel `.` rows + the save command |
| `internal/tui/pair_test.go` (new) | TUI tests + the consumer gate |
| `internal/i18n/bundles/*.toml` | new keys ×4 |
| `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, `internal/agentskill/using-gg.md` (+ `Version`) | docs |

---

### Task 1: domain — the pair record (producer + recogniser + CRUD)

**Files:** Create `internal/domain/pair.go`, `internal/domain/pair_test.go`.

**Interfaces:**
- Consumes: `s.savedCompareStore(ctx)`, `s.ResolveRev(ctx, rev) (sha string, ok bool, err error)`, `s.LinkRepo(ctx)`, `savedcompare.Entry`, `model.LinkPair{A,B}`, `previewRepo(t)` test fixture.
- Produces:
  ```go
  type CommitPair struct{ ID, Label, A, B string; Created time.Time }
  func (p CommitPair) DefaultLabel() string            // "<a7>..<b7>"
  var ErrPairExists, ErrPairNotFound error              // wrap savedcompare.ErrExists / ErrNotFound
  func (s *Service) PairAdd(ctx, a, b, label string) (CommitPair, error)
  func (s *Service) PairList(ctx) ([]CommitPair, error)
  func (s *Service) PairGet(ctx, idOrLabel string) (CommitPair, error)
  func (s *Service) PairRename(ctx, id, label string) error
  func (s *Service) PairRemove(ctx, id string) error
  ```

- [ ] **Step 1: failing tests** — `internal/domain/pair_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
)

func revOf(t *testing.T, dir, rev string) string {
	t.Helper()
	return gittest.Output(t, dir, "rev-parse", rev) // trimmed full sha
}

func TestPairAddFreezesNamesToFullShas(t *testing.T) {
	t.Parallel()
	dir, svc := previewRepo(t)
	ctx := context.Background()
	wantA, wantB := revOf(t, dir, "main"), revOf(t, dir, "feat/x")
	p, err := svc.PairAdd(ctx, "main", "feat/x", "")
	if err != nil || p.A != wantA || p.B != wantB {
		t.Fatalf("add = %+v, %v; want A=%s B=%s", p, err, wantA, wantB)
	}
	if p.Label != wantA[:7]+".."+wantB[:7] {
		t.Fatalf("default label = %q", p.Label)
	}
	// Advance feat/x: a frozen pair must not follow it.
	gittest.Run(t, dir, "checkout", "-q", "feat/x")
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c\n"), 0o644)
	gittest.Run(t, dir, "add", "c.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "add c")
	got, err := svc.PairGet(ctx, p.ID)
	if err != nil || got.B != wantB {
		t.Fatalf("after the branch moved: %+v, %v; want B still %s", got, err, wantB)
	}
}

func TestPairAddRefusals(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	if _, err := svc.PairAdd(ctx, "main", "main", ""); err == nil {
		t.Fatal("same commit accepted")
	}
	// Two SPELLINGS of one commit are the same commit.
	if _, err := svc.PairAdd(ctx, "main", "main^{commit}", ""); err == nil {
		t.Fatal("same commit under two spellings accepted")
	}
	if _, err := svc.PairAdd(ctx, "main", "no-such-rev", ""); err == nil {
		t.Fatal("unknown rev accepted")
	}
}

func TestPairAddDuplicateReturnsStoredRow(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	first, err := svc.PairAdd(ctx, "main", "feat/x", "mine")
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.PairAdd(ctx, "main", "feat/x", "other label")
	if !errors.Is(err, ErrPairExists) || again.ID != first.ID || again.Label != "mine" {
		t.Fatalf("duplicate = %+v, %v", again, err)
	}
}

// The two recognisers must DISAGREE on one fixture: three legitimate rows,
// each surface sees exactly its own.
func TestPairAndPreviewRecognisersDisagree(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	pv, err := svc.PreviewAdd(ctx, "feat/x", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	pr, err := svc.PairAdd(ctx, "main", "feat/x", "")
	if err != nil {
		t.Fatal(err)
	}
	all0, _ := svc.SavedCompareList(ctx)
	if _, err := svc.SavedCompareAdd(ctx, all0[0].Left, all0[1].Left, "cmp"); err != nil {
		t.Fatal(err)
	}
	previews, _ := svc.PreviewList(ctx)
	pairs, _ := svc.PairList(ctx)
	all, _ := svc.SavedCompareList(ctx)
	if len(all) != 3 || len(previews) != 1 || previews[0].ID != pv.ID || len(pairs) != 1 || pairs[0].ID != pr.ID {
		t.Fatalf("all=%d previews=%v pairs=%v", len(all), previews, pairs)
	}
}

func TestPairRecogniserRejectsShapesItDidNotProduce(t *testing.T) {
	t.Parallel()
	dir, svc := previewRepo(t)
	ctx := context.Background()
	repo, err := svc.LinkRepo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	a, b := revOf(t, dir, "main"), revOf(t, dir, "feat/x")
	base := "gg://" + repo.String()
	for _, left := range []string{
		base + "@main..feat/x",          // names, not shas
		base + "@" + a[:8] + ".." + b,   // a short sha
		base + "/a.txt@" + a + ".." + b, // path-narrowed
	} {
		if _, err := svc.SavedCompareAdd(ctx, left, "", "x"); err != nil {
			t.Fatalf("fixture %s: %v", left, err)
		}
	}
	if pairs, _ := svc.PairList(ctx); len(pairs) != 0 {
		t.Fatalf("recognised foreign shapes: %+v", pairs)
	}
}

func TestPairRenameRemoveOnlyTouchPairs(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	pv, _ := svc.PreviewAdd(ctx, "feat/x", "main", "")
	pr, _ := svc.PairAdd(ctx, "main", "feat/x", "")
	if err := svc.PairRename(ctx, pv.ID, "x"); !errors.Is(err, ErrPairNotFound) {
		t.Fatalf("renamed a PREVIEW through the pair surface: %v", err)
	}
	if err := svc.PairRemove(ctx, pv.ID); !errors.Is(err, ErrPairNotFound) {
		t.Fatalf("removed a PREVIEW through the pair surface: %v", err)
	}
	if err := svc.PairRename(ctx, pr.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.PairGet(ctx, "renamed"); got.ID != pr.ID {
		t.Fatal("get by new label failed")
	}
	if err := svc.PairRemove(ctx, pr.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PairGet(ctx, pr.ID); !errors.Is(err, ErrPairNotFound) {
		t.Fatalf("get after remove = %v", err)
	}
}
```

If `gittest.Output` / `repo.String()` do not exist under those names, use the
helper the neighbouring tests use (`mergeBaseOf` in `preview_test.go` shows
the rev-parse idiom; `savedcompare_test.go` shows how a link text is composed)
— fix the test helper, never the assertion.

- [ ] **Step 2: run, expect a compile failure** — `rtk go test ./internal/domain/ -run 'TestPair' 2>&1 | tail -20` → undefined `PairAdd` etc.

- [ ] **Step 3: implement** — `internal/domain/pair.go`:

```go
package domain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/savedcompare"
)

// ErrPairNotFound / ErrPairExists WRAP the store's errors, like their preview
// twins, so frontends can errors.Is them without importing the store.
var (
	ErrPairNotFound = fmt.Errorf("%w", savedcompare.ErrNotFound)
	ErrPairExists   = fmt.Errorf("%w", savedcompare.ErrExists)
)

// CommitPair is a saved commits diff: the change-set A..B between two FROZEN
// commits. It is the second set-shaped savedcompare row a Previews surface can
// show; a merge preview is the first. A holds the old side, B the new.
type CommitPair struct {
	ID, Label string
	A, B      string
	Created   time.Time
}

// DefaultLabel is "<a7>..<b7>".
func (p CommitPair) DefaultLabel() string { return shortSha(p.A) + ".." + shortSha(p.B) }

func shortSha(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// fullSha reports whether s is a whole object id (sha-1 or sha-256), which is
// the only spelling PairAdd ever stores.
func fullSha(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// pairFromEntry is previewFromEntry's twin: it reads a set-shaped entry back
// as a commit pair and reports false for everything PairAdd did not produce —
// a comparison (Right set), a merge preview, and a pair set somebody stored by
// NAME, by short sha, narrowed to a path or carrying a hint. Those are
// legitimate rows this surface does not promise to render.
func pairFromEntry(e savedcompare.Entry) (CommitPair, bool) {
	if e.Right != nil {
		return CommitPair{}, false
	}
	pr := e.Left.Target.Pair
	if pr == nil || e.Left.Path != "" || e.Left.Hint != (model.LinkHint{}) {
		return CommitPair{}, false
	}
	if !fullSha(pr.A) || !fullSha(pr.B) {
		return CommitPair{}, false
	}
	return CommitPair{ID: e.ID, Label: e.Label, A: pr.A, B: pr.B, Created: e.Created}, true
}

// PairAdd FREEZES a and b to full shas (a branch name stops following its
// branch here) and stores the change-set a..b. A duplicate returns the
// EXISTING row with ErrPairExists so a frontend can focus it.
func (s *Service) PairAdd(ctx context.Context, a, b, label string) (CommitPair, error) {
	st := s.savedCompareStore(ctx)
	if st == nil {
		return CommitPair{}, ErrSavedComparesDisabled
	}
	var shas [2]string
	for i, rev := range []string{a, b} {
		sha, ok, err := s.ResolveRev(ctx, rev)
		if err != nil {
			return CommitPair{}, err
		}
		if !ok {
			return CommitPair{}, fmt.Errorf("pair: %q is not a commit", rev)
		}
		shas[i] = sha
	}
	if shas[0] == shas[1] {
		return CommitPair{}, errors.New("pair: a pair needs two different commits")
	}
	repo, err := s.LinkRepo(ctx)
	if err != nil {
		return CommitPair{}, err
	}
	// Through String→ParseLink, like entryFromPreview: the store must never
	// hold a row that will not read back.
	l, err := model.ParseLink(model.Link{Repo: repo, Target: model.LinkTarget{
		State: model.StateCommitted,
		Pair:  &model.LinkPair{A: shas[0], B: shas[1]},
	}}.String())
	if err != nil {
		return CommitPair{}, err
	}
	if label == "" {
		label = CommitPair{A: shas[0], B: shas[1]}.DefaultLabel()
	}
	stored, err := st.Add(savedcompare.Entry{Left: l, Label: label})
	p, ok := pairFromEntry(stored)
	if !ok {
		return CommitPair{}, fmt.Errorf("pair: stored entry %q is not a commit pair", stored.ID)
	}
	if errors.Is(err, savedcompare.ErrExists) {
		return p, ErrPairExists
	}
	return p, err
}

// PairList returns the commit pairs among the saved comparisons, in insertion
// order.
func (s *Service) PairList(ctx context.Context) ([]CommitPair, error) {
	st := s.savedCompareStore(ctx)
	if st == nil {
		return nil, ErrSavedComparesDisabled
	}
	es, err := st.List()
	if err != nil {
		return nil, err
	}
	out := make([]CommitPair, 0, len(es))
	for _, e := range es {
		if p, ok := pairFromEntry(e); ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// PairGet finds a pair by id, else by exact label (first match).
func (s *Service) PairGet(ctx context.Context, idOrLabel string) (CommitPair, error) {
	ps, err := s.PairList(ctx)
	if err != nil {
		return CommitPair{}, err
	}
	for _, p := range ps {
		if p.ID == idOrLabel {
			return p, nil
		}
	}
	for _, p := range ps {
		if p.Label == idOrLabel {
			return p, nil
		}
	}
	return CommitPair{}, ErrPairNotFound
}

// PairRename relabels a PAIR; an id naming any other shape is not found, the
// rule PreviewRename applies from its side.
func (s *Service) PairRename(ctx context.Context, id, label string) error {
	if _, err := s.PairGet(ctx, id); err != nil {
		return err
	}
	if err := s.SavedCompareRename(ctx, id, label); errors.Is(err, ErrSavedCompareNotFound) {
		return ErrPairNotFound
	} else {
		return err
	}
}

// PairRemove deletes a PAIR by id.
func (s *Service) PairRemove(ctx context.Context, id string) error {
	if _, err := s.PairGet(ctx, id); err != nil {
		return err
	}
	if err := s.SavedCompareRemove(ctx, id); errors.Is(err, ErrSavedCompareNotFound) {
		return ErrPairNotFound
	} else {
		return err
	}
}
```

If the store's `Add` defaults an empty label itself, keep the explicit default
above anyway — the pair vocabulary owns its label, as `PreviewAdd` says of its.
If `LinkTarget.String()` refuses a `Pair` with `State != StateCommitted` or
wants another field set, follow what `model/link.go`'s pair round-trip test
constructs.

- [ ] **Step 4: run** — `rtk go test ./internal/domain/ -run 'TestPair|TestPreview|TestSavedCompare' 2>&1 | tail -20` → PASS.
- [ ] **Step 5: watch two guards fail.** (a) Delete the `fullSha` check → `TestPairRecogniserRejectsShapesItDidNotProduce` must FAIL. (b) Delete the `PairGet` guard in `PairRemove` → `TestPairRenameRemoveOnlyTouchPairs` must FAIL. Restore both.
- [ ] **Step 6: commit** — `git add internal/domain/pair.go internal/domain/pair_test.go && git commit -m "feat(domain): saved commit pairs — a second set-shaped savedcompare row"`.

---

### Task 2: domain — `PairSummary` + `PairOpen`

**Files:** Modify `internal/domain/pair.go`, `internal/domain/pair_test.go`.

**Interfaces:**
- Consumes: `s.ResolveRev`, `s.CompareFiles(ctx, left, right model.Endpoint) ([]model.CommitFile, error)`, `model.CommitEndpoint(sha) (model.Endpoint, error)`.
- Produces:
  ```go
  type PairState int // PairInvalid, PairOK, PairMissingA, PairMissingB
  type PairSummary struct{ State PairState; Files int }
  type PairEndpoints struct{ Summary PairSummary; Left, Right model.Endpoint }
  func (s *Service) PairSummary(ctx, a, b string) (PairSummary, error)
  func (s *Service) PairOpen(ctx, a, b string) (PairEndpoints, error)
  ```

- [ ] **Step 1: failing tests** (append to `pair_test.go`):

```go
func TestPairSummaryAndOpen(t *testing.T) {
	t.Parallel()
	dir, svc := previewRepo(t)
	ctx := context.Background()
	a, b := revOf(t, dir, "main"), revOf(t, dir, "feat/x")
	// main..feat/x TWO-dot: a.txt, b.txt added AND m.txt absent on feat/x = 3 paths.
	sum, err := svc.PairSummary(ctx, a, b)
	if err != nil || sum.State != PairOK || sum.Files != 3 {
		t.Fatalf("summary = %+v, %v; want PairOK with 3 files", sum, err)
	}
	eps, err := svc.PairOpen(ctx, a, b)
	if err != nil || eps.Left.CacheTag() != a || eps.Right.CacheTag() != b {
		t.Fatalf("open = %+v, %v", eps, err)
	}
	gone := "0123456789abcdef0123456789abcdef01234567"
	if sum, _ := svc.PairSummary(ctx, gone, b); sum.State != PairMissingA {
		t.Fatalf("missing A = %+v", sum)
	}
	if sum, _ := svc.PairSummary(ctx, a, gone); sum.State != PairMissingB {
		t.Fatalf("missing B = %+v", sum)
	}
	if eps, err := svc.PairOpen(ctx, a, gone); err != nil || eps.Summary.State != PairMissingB || eps.Left != (model.Endpoint{}) {
		t.Fatalf("open with a gone side = %+v, %v", eps, err)
	}
}
```

The file count `3` is the point of the fixture: a THREE-dot diff of the same
two commits is 2 files, so a summary that reused `DiffNameOnlyRange` would be
wrong and this assertion sees it. (Add the `model` import.)

- [ ] **Step 2: run, expect FAIL** (undefined).
- [ ] **Step 3: implement** (append to `pair.go`):

```go
// PairState is why a pair can or cannot open. Zero is invalid on purpose.
type PairState int

const (
	PairInvalid PairState = iota
	PairOK
	PairMissingA // the old side is not in this repository (gc, another clone)
	PairMissingB
)

// PairSummary is a pair's row state. A frozen pair never moves, so unlike
// PreviewSummary there is no movement to detect.
type PairSummary struct {
	State PairState
	Files int // paths differing between A and B — TWO-dot, tip to tip
}

// PairEndpoints is what a frontend opens the compare view with. Both zero
// unless PairOK.
type PairEndpoints struct {
	Summary     PairSummary
	Left, Right model.Endpoint
}

func (s *Service) PairSummary(ctx context.Context, a, b string) (PairSummary, error) {
	eps, err := s.PairOpen(ctx, a, b)
	return eps.Summary, err
}

func (s *Service) PairOpen(ctx context.Context, a, b string) (PairEndpoints, error) {
	for i, sha := range []string{a, b} {
		if _, ok, err := s.ResolveRev(ctx, sha); err != nil {
			return PairEndpoints{}, err
		} else if !ok {
			return PairEndpoints{Summary: PairSummary{State: PairMissingA + PairState(i)}}, nil
		}
	}
	left, err := model.CommitEndpoint(a)
	if err != nil {
		return PairEndpoints{}, err
	}
	right, err := model.CommitEndpoint(b)
	if err != nil {
		return PairEndpoints{}, err
	}
	files, err := s.CompareFiles(ctx, left, right)
	if err != nil {
		return PairEndpoints{}, err
	}
	return PairEndpoints{Summary: PairSummary{State: PairOK, Files: len(files)}, Left: left, Right: right}, nil
}
```

- [ ] **Step 4: run → PASS.** Also add one assertion to `TestPairAndPreviewRecognisersDisagree`'s neighbour proving the spec §7 guarantee — new test:

```go
func TestPairLeftEvaluatesToABoundedSetComparableWithAPreview(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	if _, err := svc.PreviewAdd(ctx, "feat/x", "main", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PairAdd(ctx, "main", "feat/x", ""); err != nil {
		t.Fatal(err)
	}
	all, _ := svc.SavedCompareList(ctx)
	var sets []FileSet
	for _, c := range all {
		l, err := model.ParseLink(c.Left)
		if err != nil {
			t.Fatal(err)
		}
		fs, err := svc.EvalLink(ctx, l)
		if err != nil || !fs.Bounded() {
			t.Fatalf("%s: bounded=%v err=%v", c.Left, fs.Bounded(), err)
		}
		sets = append(sets, fs)
	}
	if _, err := svc.CompareSets(ctx, sets[0], sets[1]); err != nil {
		t.Fatalf("preview × pair: %v", err)
	}
}
```

Match `EvalLink` / `CompareSets` / `Bounded` to their real signatures in
`internal/domain/fileset.go` and its tests before running.

- [ ] **Step 5: cache the summary by (A,B).** `chainPreviewsRead` fires on every branches/remotes arrival, and a frozen pair's file count is immutable — on a 100GB monorepo one `diff-tree` per saved pair per refresh is not acceptable. Read how `PreviewSummary` caches by hash pair (`TestPreviewSummaryCachedByHashPair`, `callCount`) and reuse that cache with a `"pair:"+a+":"+b` key. Only a `PairOK` result is cached (a missing commit may appear after a fetch). Test: two `PairSummary` calls on a `FakeRunner`-counted service run the diff ONCE; a missing-side result is NOT cached.
- [ ] **Step 6: commit** — `feat(domain): PairSummary + PairOpen (two-dot, frozen, cached)`.

---

### Task 3: CLI — `gg preview` pair arm + e2e

**Files:** Modify `internal/cli/preview.go`; create `internal/cli/preview_pair_test.go`, `e2e/scenarios/s96_saved_pairs.toml` (use the next free `sNN`; invoke the `writing-e2e-scenarios` skill first).

**Interfaces:** consumes Task 1–2's `Pair*`; produces the CLI surface below.

Behaviour (read `preview.go` fully first; keep every existing output byte-identical):

| verb | pair behaviour |
|---|---|
| `preview add <a>..<b> [--label L]` | exactly ONE positional containing `..` and NOT `...` → `PairAdd`. Prints the id alone, like `preview add` today. `ErrPairExists` → stderr `preview add: <a7>..<b7> already saved as <id> (<label>)`, exit 1. Bad rev / same commit → exit 2. |
| `preview list` | merge previews first (unchanged lines), then pair rows: `<id>\t<label>\t<a7>..<b7>\t<N files \| missing: <sha7>>`; `--json` rows gain `"kind":"pair","a":…,"b":…` (previews gain `"kind":"preview"`). |
| `preview show <id\|label>` | try `PreviewGet`, on `ErrPreviewNotFound` try `PairGet`; pair prints id, label, full `a`, full `b`, state. |
| `preview rm` / `rename` | same fall-through → `PairRemove` / `PairRename`. |
| `preview diff <id\|label> [--hunks]` | pair → the same diff path `gg diff <a>..<b>` uses, missing commit → exit 1 `commit <sha7> is not in this repository`. |

The split point is one helper, tested on its own:

```go
// pairSpec splits "<a>..<b>". A three-dot text is a merge preview and is NOT
// a pair; a refname cannot contain "..", so the first ".." is the separator.
func pairSpec(s string) (a, b string, ok bool) {
	if strings.Contains(s, "...") {
		return "", "", false
	}
	a, b, ok = strings.Cut(s, "..")
	return a, b, ok && a != "" && b != ""
}
```

- [ ] **Step 1: failing tests** — table test for `pairSpec` (`"a..b"` ok; `"main...feat"`, `"a.."`, `"..b"`, `"ab"` not ok) + `TestPreviewAddPair`, `TestPreviewListShowsBothKinds`, `TestPreviewRmRenameShowFallThroughToPairs`, `TestPreviewAddOneThreeDotArgIsStillAUsageError` (today `NArg() != 2` → usage, exit 2; a lone `main...feat` must keep that, never reach `PairAdd`), using the package's existing CLI test repo helper (`previewRepo` in the cli tests uses `t.Setenv` → these tests stay SERIAL, no `t.Parallel()`).
- [ ] **Step 2: run → FAIL.**
- [ ] **Step 3: implement** in `preview.go`. Update the unknown-subcommand usage strings to mention `<a>..<b>`.
- [ ] **Step 4: run** `rtk go test ./internal/cli/ -run 'Preview|PairSpec'` → PASS.
- [ ] **Step 5: e2e scenario** — build repo with two commits on main; `gg preview add HEAD~1..HEAD --label last`; assert `gg preview list` contains `last`; `gg preview diff last` lists the changed file; `gg preview rm last`; list no longer contains it. Run `./test.sh e2e`.
- [ ] **Step 6: commit** — `feat(cli): gg preview add <a>..<b> — saved commit pairs`.

---

### Task 4: TUI — the Previews tab shows both kinds

**Files:** Modify `internal/tui/preview_panel.go`, `preview_actions.go`, `model.go` (~2291, ~2310, ~2404), `link.go` (~193); create `internal/tui/pair_test.go`; i18n bundles. Invoke the `adding-translations` skill before adding keys.

**Interfaces:**
- Consumes: `svc.PairList/PairSummary/PairOpen/PairRename/PairRemove/PairAdd`, `m.openCompareFiles(left, right)`, `m.linkFor…`/link recording helpers in `link.go`.
- Produces:
  ```go
  type previewRowKind int // rowMerge (zero), rowPair
  func (r previewRow) merge() (model.MergePreview, bool)
  // previewRow gains: kind previewRowKind; pair domain.CommitPair; psum domain.PairSummary
  func (r previewRow) id() string
  func (r previewRow) label() string
  func (r previewRow) created() time.Time
  ```

- [ ] **Step 1: failing tests** (`pair_test.go`, `t.Parallel()`, repo via `testRepo`): (1) a model whose `m.previews` holds one `rowMerge` and one `rowPair` renders two rows, the pair row containing `<a7>..<b7>` and `N files`; a `PairMissingB` row contains `missing: <b7>`; (2) `previewList.Key/Name/Date` return the pair's id/label/created; (3) enter on an OK pair row returns a cmd whose message opens compare-files with endpoints A, B and leaves `m.previewOpen == nil` and `filesPreviewSet` zero; enter on a missing pair sets `statusMsg` and returns no cmd; (4) `e` opens the rename popup prefilled with the pair label and its command calls `PairRename`; (5) `d` confirm → `PairRemove`; (6) `s` → `PairAdd(b, a)`; (7) copy-link on a pair row yields `gg://<repo>@<A>..<B>` (full shas) and records it in linkhist; (8) **the consumer gate** — type-level, not a caller list (a list lets a new caller be appended without ever handling a pair): `previewRow.rec` is read ONLY through

```go
// merge is the row as a merge preview; false for a pair, whose rec is ZERO —
// handing its empty Source/Target to a merge-preview path is a confusing git
// error, so every consumer is forced through this ok.
func (r previewRow) merge() (model.MergePreview, bool) { return r.rec, r.kind != rowPair }
```

and the gate is an AST test over `internal/tui/*.go` (non-test): every `SelectorExpr` whose `Sel.Name == "rec"` must sit in `preview_panel.go`. Today 19 `.rec` reads exist across the package — all move behind `merge()` / `id()` / `label()` / `created()` in this task.

**Kind zero = merge, deliberately.** `previewRowKind` is `rowMerge` (0), `rowPair` (1) — NOT Invalid-first: existing fixtures (`preview_notes_test.go`, 3 literals) and every producer build `previewRow{rec: …}` with no kind, and a row with a zero kind and a filled `rec` IS a merge preview. The Invalid-first convention protects values that cross a boundary; this one never leaves the package.

- [ ] **Step 2: run → FAIL.**
- [ ] **Step 3: implement.**
  - `preview_panel.go`: add the kind + fields + accessors; `readPreviews` leaves existing rows at the zero kind then appends, per `PairList` entry, `previewRow{kind: rowPair, pair: p, psum: sum, err: err}`; `previewList` and `previewRows` use the accessors; `previewStateText` gains the pair arm:
    ```go
    if r.kind == rowPair {
        switch r.psum.State {
        case domain.PairMissingA:
            return i18n.T("missing: %s", shortHash(r.pair.A))
        case domain.PairMissingB:
            return i18n.T("missing: %s", shortHash(r.pair.B))
        }
        if r.psum.Files == 1 {
            return i18n.T("1 file")
        }
        return i18n.T("%d files", r.psum.Files)
    }
    ```
    and the middle column is `shortHash(A)+".."+shortHash(B)` for a pair (use the package's existing short-hash helper).
  - `preview_actions.go`: `previewRenameCmd(kind, id, label)` / `previewRemoveCmd(kind, id)` dispatch to `Pair*` for `rowPair`; the rename popup and the remove confirm carry the kind; `previewSwapCmd` on a pair → a `pairAddCmd(b, a)` returning `previewMutatedMsg{focusID: p.ID, fromTab: true}` with `ErrPairExists` folded to nil.
  - `model.go` enter arm: `rowPair` → missing → `m.statusMsg = i18n.T("missing: %s", …)`; else a cmd running `svc.PairOpen` off-thread → `pairOpenMsg{eps, err, label, gen}` stamped with `m.previewGen` exactly as `openPreviewCmd` does (the handler DROPS a message whose gen is stale — a second enter or a row switch must not deliver an old open) → handler calls `m.openCompareFiles(eps.Left, eps.Right)`. `openCompareFiles(left, right)` takes no title: read how `previewOpenMsg.title` reaches the files view and thread the same override for `i18n.T("Saved diff: %s", label)`; if that override is reachable only through `m.previewOpen`, add a `filesTitle string` field the files view prefers when set and clear it in `closeFilesView`. It must NOT set `m.previewOpen` nor arm `filesPreviewSet` (notes are out of scope; spec §5.2).
  - `link.go`: pair row → the stored link text (compose via the same producer `previewLinkFor` uses, with `LinkTarget.Pair`), recorded in linkhist like every copy row.
- [ ] **Step 4: run** `rtk go test ./internal/tui/ -run 'Pair|Preview|I18n|Vocab|MenuLabels'` → PASS (the i18n AST gates included).
- [ ] **Step 5: watch the gate fail** — add a throwaway `_ = r.rec.Source` in `preview_actions.go` → gate FAILS; remove it.
- [ ] **Step 6: commit** — `feat(tui): the Previews tab lists saved commit pairs`.

---

### Task 5: TUI — save from the Commits panel

**Files:** Create `internal/tui/pair_save.go`; modify `internal/tui/action_menu.go` (~340, right after the `commitCompareSelectionRow` block), `opAffectedSources` only if the save is routed as an op (it is NOT — it is a domain call, so it chains `m.chainPreviewsRead()` instead); help rows in `help.go`; footer advert; i18n bundles. Invoke `adding-features` checklist + `advertise in help AND footer`.

**Interfaces:**
- Consumes: `m.validCompareKeys()`, `m.compareKeyRank(k)`, `wipKey(wipRow{kind: wipWorktree|wipStaged})`, `svc.PairAdd`.
- Produces:
  ```go
  func (m Model) pairSaveShas() (a, b string, ok bool) // exactly 2 commit keys, no ◇ rows; direction = compareSelectionEndpoints'
  func (m Model) commitSavePairRows() []actionRow              // ids "commit-save-pair", "commit-save-pair-reversed"
  type pairSavedMsg struct{ p domain.CommitPair; existed bool; err error }
  ```

- [ ] **Step 1: failing tests** — gating table:

| ◉ set | rows |
|---|---|
| none / 1 commit | 0 |
| 2 commits | 2 |
| 3 commits | 0 |
| 1 commit + ◇ Working tree | 0 |
| 1 commit + ◇ Staged | 0 |

plus: marks made **newest first** still save `A = older, B = newer` (direction by feed rank, not mark order) — asserted as EQUAL to `compareSelectionEndpoints()`' left/right on the same model, so the two can never drift; the reversed row swaps; `pairSavedMsg` sets status `saved to previews: <label>` / `already saved as <label>`, chains a previews read, sets `m.previewFocusID`, and **leaves `m.commitCompareSet` untouched**; rows are absent when `m.focus != panelCommits` or ops are running.

- [ ] **Step 2: run → FAIL.**
- [ ] **Step 3: implement** `pair_save.go`:

```go
// pairSaveShas is the pair "Save to previews" stores. It does NOT rank the
// marks itself: it takes the two endpoints compareSelectionEndpoints already
// chose, so the saved direction IS the direction Compare selection diffs in,
// by construction rather than by two arms agreeing.
func (m Model) pairSaveShas() (a, b string, ok bool) {
	keys := m.validCompareKeys()
	if len(keys) != 2 {
		return "", "", false
	}
	for _, k := range keys {
		if k == wipKey(wipRow{kind: wipWorktree}) || k == wipKey(wipRow{kind: wipStaged}) {
			return "", "", false
		}
	}
	left, right, _, ok := m.compareSelectionEndpoints()
	if !ok {
		return "", "", false
	}
	return left.CacheTag(), right.CacheTag(), true // a commit endpoint's tag is its sha
}

func (m Model) commitSavePairRows() []actionRow {
	if m.focus != panelCommits || !m.opsIdle() {
		return nil
	}
	older, newer, ok := m.pairSaveShas()
	if !ok {
		return nil
	}
	row := func(id, label, a, b string) actionRow {
		return actionRow{id: id, label: label, run: func(m Model) (tea.Model, tea.Cmd) {
			svc := m.svc
			return m, func() tea.Msg {
				p, err := svc.PairAdd(context.Background(), a, b, "")
				existed := errors.Is(err, domain.ErrPairExists)
				if existed {
					err = nil
				}
				return pairSavedMsg{p: p, existed: existed, err: err}
			}
		}}
	}
	return []actionRow{
		row("commit-save-pair", i18n.T("Save to previews"), older, newer),
		row("commit-save-pair-reversed", i18n.T("Save reversed to previews"), newer, older),
	}
}
```

Wire `append(rows, m.commitSavePairRows()...)` directly after the Compare-selection row in `action_menu.go`; handle `pairSavedMsg` in `Update`. Add the same rows to the files view's commits-side `.` menu only if that menu already reuses the Commits-panel row builders with a focus override — if it needs its own plumbing, SKIP it and record that in the CHANGELOG entry as not done (spec §5.1 last paragraph is the optional part).

- [ ] **Step 4: run** the TUI package tests → PASS, including `menu_labels_test.go`, `options_vocab_test.go`, `i18n_scan_test.go`.
- [ ] **Step 5: headless smoke** (invoke `driving-tui-headless`): build `go build -o /tmp/…scratchpad/gg ./cmd/gg`, then `./tui-capture.sh --gg <bin> --repo <scratch repo with ≥3 commits> "<focus commits; m; down; m; . ; filter 'Save to'; enter; focus previews tab; enter>"` — read the snapshots: the row `<a7>..<b7>  N files` is VISIBLE in the Previews tab and enter opens the files view.
- [ ] **Step 6: commit** — `feat(tui): m + m then . → Save to previews`.

---

### Task 6: docs, skill, gates, delivery

- [ ] `CHANGELOG.md` entry; `README.md` Previews section + the `.` menu list; `docs/CLAUDE-details.md` (pair = set-shaped entry, the two recognisers, the consumer gate, hint-id-is-a-lookup note from spec §7); `internal/agentskill/using-gg.md` documents `gg preview add <a>..<b>` and bump `agentskill.Version`. `CLAUDE.md` unchanged (no new package).
- [ ] Update the TUI help (`?`) row for the Previews tab and the Commits `.` list (`help.go:186` row) — four bundles.
- [ ] `./test.sh` then `./test.sh race` from a CLEAN tree, started immediately (do not wait for other sessions' runs).
- [ ] Merge `main` into the branch again; re-run `./test.sh race` on the MERGED tree (plan 3a's defects #3/#4 were only visible there; `action_menu.go` is the likely conflict with plan 3b).
- [ ] Build the verify binary in the worktree, send it + its absolute path to the user unprompted.
- [ ] ASK before merging. Merge = `checkout main`, `--no-ff -m "Merge feat/saved-pairs: saved commit pairs in the Previews tab"` + trailers; then `./build.sh install`, `./build.sh web`; remove worktree + branch; update memory.

## Self-review notes

- Spec §4.1 → Tasks 1–2 · §4.2 → Task 2 (missing states) + Task 4 (row text) · §5.1 → Task 5 · §5.2 → Task 4 · §6 → Task 3 · §8 → per-task refusals · §9 → each task's tests · §10 ordering kept (domain → CLI → tab → save rows).
- Deviation from spec §4.1: `PairOpen` returns its own `PairEndpoints` (not `PreviewEndpoints`, whose `Summary` field is a `PreviewSummary`); and `PairRename/PairRemove` exist as guarded wrappers, mirroring `PreviewRename/Remove`, instead of frontends calling `SavedCompare*` directly — so a pair surface can never relabel a comparison.
- Spec §5.2 says "six" consumer sites; the gate test asserts the REAL set at implementation time.
