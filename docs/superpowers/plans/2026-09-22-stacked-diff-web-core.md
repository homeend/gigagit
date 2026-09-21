# Stacked diff view — plan 1: web core — Implementation Plan

> **For agentic workers:** this repo FORBIDS subagents (CLAUDE.md "NEVER USE
> SUB AGENTS"). Execute with superpowers:executing-plans, task by task, in THIS
> session. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In `gg web`, an `S` toggle that renders every file of the open change
set (commit, compare, preview/PR, working-tree section) as one scroll of
per-file sections — a sticky header (`▾ M path +a −d`) above each file's diff —
loaded lazily, auto-collapsed above 100 files.

**Architecture:** A pure, import-free `stack.js` (slots, pick order, reconcile,
counts) is unit-tested under node. A DOM module `stackview.js` owns the stack
lifecycle, rendering, the IntersectionObserver loader (≤3 in flight), collapse
and scroll↔file-cursor sync. `files.js` routes `openFile` into it when the pref
is on and gains one shared `fileDiffURL(f)` so both views fetch identically.
Counts come from a new read-only `GET /api/numstat`, asked only while a stack
is open.

**Tech Stack:** Go 1.26 (net/http, `internal/git` verbs via `gitcmd`),
vanilla ES modules, node (JS unit tests driven from Go tests), Playwright
(browser probe, scratchpad only).

**Spec:** `docs/superpowers/specs/2026-09-22-stacked-diff-view-design.md`
(this plan = spec §13 "Plan 1 — web core"; the symmetric view is plan 2 and
stays single-file here).

## Global Constraints

- Toggle key `S`; collapse current file `-`; collapse/expand all `_`. Web
  `n`/`p` are NOT stack keys (`p` = pull).
- Pref: `stacked_diff` (bool) in `/api/uistate`; never localStorage.
- Auto-collapse threshold: **more than 100** files. Max fetches in flight: **3**.
- Header: status letter + path (`old → new` on renames) + `+add −del` (or
  `bin`) + a `▾/▸` fold control; sticky under `#diff-header`.
- No new diff endpoint: slots fetch the URL `fileDiffURL(f)` builds, the same
  one single-file mode uses.
- A new element with `class="hidden"` needs its own `#id.hidden { display:none }`
  CSS rule (memory: web hides by ID).
- `internal/web` stays domain-only (no `internal/git` import) — archtest.
- Tests: `t.Parallel()` on new tests unless they touch global state
  (`t.Setenv` tests stay serial).
- Commit trailer on every commit:
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01194j7ji9skyAsLfZx4vZVQ
  ```
- Work in `/mnt/t/others/gigagit/.claude/worktrees/stacked-diff-view`; use
  `git -C <abs>` / absolute paths (the shell cwd can reset).

## File map

| File | Change |
|------|--------|
| `internal/git/log.go` | + `CommitNumstat` verb |
| `internal/git/log_test.go` | + argv + real-repo tests |
| `internal/domain/query_cli.go` | + `CommitStat` query |
| `internal/web/numstat.go` | NEW — `GET /api/numstat` |
| `internal/web/numstat_test.go` | NEW |
| `internal/promptstate/webui.go` | + `StackedDiff` field |
| `internal/web/uistate.go` / `uistate_test.go` | + `stacked_diff` wire field |
| `internal/web/static/uistate.js` | + `stacked_diff: false` in the save base |
| `internal/web/static/stack.js` | NEW — pure slot model |
| `internal/web/stackjs_test.go` | NEW — node harness for stack.js |
| `internal/web/static/files.js` | + `fileDiffURL`, route `openFile`, teardown in `setLayout`, reconcile/rerender hooks, `closeConflictPick` |
| `internal/web/static/stackview.js` | NEW — DOM half |
| `internal/web/stackviewjs_test.go` | NEW — structural wiring guards |
| `internal/web/static/keys.js` | `S`, `-`, `_`, `/` guard, `j/k` scroll, footer chip |
| `internal/web/static/index.html` | toolbar buttons + footer chip |
| `internal/web/static/style.css` | `.stk-*` rules |
| `internal/web/static/palette.js` | "toggle stacked diff" row |
| `CHANGELOG.md`, `README.md`, `docs/web-tui-parity.md`, `docs/CLAUDE-details.md` | docs |

---

### Task 1: `CommitNumstat` git verb + `CommitStat` domain query

**Files:**
- Modify: `internal/git/log.go` (after `CommitFiles`, ~line 130)
- Modify: `internal/domain/query_cli.go` (after `DiffStat`)
- Test: `internal/git/log_test.go`

**Interfaces:**
- Produces: `func (r *Repo) CommitNumstat(ctx context.Context, hash string) (string, error)` — raw `--numstat -z` records for hash against its FIRST parent (root: against the empty tree), same pairing as `CommitFiles`.
- Produces: `func (s *Service) CommitStat(ctx context.Context, hash string) ([]model.DiffStat, error)`.
- Consumes: `git.ParseNumstat(out string) []model.DiffStat` (existing), `model.DiffStat{Path, OldPath, Added, Deleted, Binary}` (existing).

- [ ] **Step 1: Write the failing tests** — append to `internal/git/log_test.go`:

```go
func TestCommitNumstatArgv(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log (commit numstat)", gitexec.Result{Stdout: "3\t1\tfile.txt\x00"})
	repo := &Repo{Runner: f}
	out, err := repo.CommitNumstat(context.Background(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(f.Calls[0].Argv, " ")
	for _, part := range []string{"log", "-1", "-m", "--first-parent", "--root", "--numstat", "-M", "-z", "--format=", "abc123"} {
		if !strings.Contains(argv, part) {
			t.Fatalf("argv = %q, missing %q", argv, part)
		}
	}
	if got := ParseNumstat(out); len(got) != 1 || got[0].Added != 3 || got[0].Deleted != 1 {
		t.Fatalf("parsed = %+v", got)
	}
}

// The stacked diff's header counts must name the SAME paths CommitFiles lists:
// root commit, a rename, and a binary file, against a real git.
func TestCommitNumstatRealRepo(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t) // root commit adds README.md ("hello\n")
	repo := &Repo{Runner: runner}
	head := func() string {
		out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	parse := func(rev string) []model.DiffStat {
		out, err := repo.CommitNumstat(context.Background(), rev)
		if err != nil {
			t.Fatal(err)
		}
		return ParseNumstat(out)
	}

	if got := parse(head()); len(got) != 1 || got[0].Path != "README.md" || got[0].Added != 1 || got[0].Deleted != 0 {
		t.Fatalf("root commit stats = %+v, want [README.md +1 -0]", got)
	}

	gitIn(t, dir, "mv", "README.md", "DOCS.md")
	gitIn(t, dir, "commit", "-m", "rename")
	if got := parse(head()); len(got) != 1 || got[0].Path != "DOCS.md" || got[0].OldPath != "README.md" {
		t.Fatalf("rename stats = %+v, want DOCS.md renamed from README.md", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0, 1, 2, 0, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "blob.bin")
	gitIn(t, dir, "commit", "-m", "binary")
	if got := parse(head()); len(got) != 1 || got[0].Path != "blob.bin" || !got[0].Binary {
		t.Fatalf("binary stats = %+v, want blob.bin binary", got)
	}
}
```

(`os`, `filepath`, `exec`, `model` are already imported by `log_test.go`; add any that the compiler reports missing.)

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/stacked-diff-view && go test ./internal/git -run 'TestCommitNumstat' -count=1`
Expected: FAIL — `repo.CommitNumstat undefined`.

- [ ] **Step 3: Implement the verb** — in `internal/git/log.go`, after `CommitFiles`:

```go
// CommitNumstat returns `--numstat -z` records (parse with ParseNumstat) for
// one commit against its FIRST parent — a root commit against the empty tree
// — the exact pairing CommitFiles lists, so the stacked diff's per-file
// counts name the same paths. log's empty --format= can leave separator
// bytes ahead of the first record; they are trimmed so ParseNumstat sees
// records only. One invocation.
func (r *Repo) CommitNumstat(ctx context.Context, hash string) (string, error) {
	argv := gitcmd.New("log").
		Arg("-1", "-m", "--first-parent", "--root", "--numstat", "-M", "-z", "--format=", hash).
		ToArgv()
	res, err := r.Runner.Run(ctx, "git log (commit numstat)", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimLeft(res.Stdout, "\n\x00"), nil
}
```

And in `internal/domain/query_cli.go`, after `DiffStat`:

```go
// CommitStat returns per-file line counts for one commit against its first
// parent (a root commit against the empty tree) — the pairing CommitFiles
// lists, so the two agree path for path.
func (s *Service) CommitStat(ctx context.Context, hash string) ([]model.DiffStat, error) {
	return query(ctx, s, "commit-stat:"+hash, func(ctx context.Context) ([]model.DiffStat, error) {
		out, err := s.repo.CommitNumstat(ctx, hash)
		if err != nil {
			return nil, err
		}
		return git.ParseNumstat(out), nil
	})
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/git -run 'TestCommitNumstat' -count=1 && go build ./...`
Expected: PASS, build OK. If the root-commit case fails with `Added == 0`, print the raw output (`t.Logf("%q", out)`) and extend the `TrimLeft` set to the separator git actually emitted — never trim inside records.

- [ ] **Step 5: Commit**

```bash
git -C /mnt/t/others/gigagit/.claude/worktrees/stacked-diff-view add internal/git/log.go internal/git/log_test.go internal/domain/query_cli.go
git -C /mnt/t/others/gigagit/.claude/worktrees/stacked-diff-view commit -m "feat(git,domain): CommitNumstat verb + CommitStat query for per-file counts

<trailer>"
```

---

### Task 2: `GET /api/numstat`

**Files:**
- Create: `internal/web/numstat.go`
- Create: `internal/web/numstat_test.go`

**Interfaces:**
- Consumes: `svc.CommitStat(ctx, hash)` (Task 1); `svc.DiffStat(ctx, model.DiffSpec)` (existing); `isHexSha`, `writeJSON`, `writeErr`, `RegisterRoutes` (existing, package `web`).
- Produces (wire): `GET /api/numstat?sha=<hex>` | `?left=<hex>&right=<hex>` | `?wt=staged|unstaged` → `{"files":[{"path":"…","old_path":"…","add":N,"del":N,"binary":bool}]}`. 400 on any other shape.

- [ ] **Step 1: Write the failing test** — `internal/web/numstat_test.go`:

```go
package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type numstatWire struct {
	Files []struct {
		Path    string `json:"path"`
		OldPath string `json:"old_path"`
		Add     int    `json:"add"`
		Del     int    `json:"del"`
		Binary  bool   `json:"binary"`
	} `json:"files"`
}

func revParse(t *testing.T, dir, rev string) string {
	t.Helper()
	return strings.TrimSpace(gitOut(t, dir, "rev-parse", rev))
}

// The stacked diff asks for a change set's per-file counts in ONE request;
// each of the three sources the web can name by hash answers here.
func TestNumstatSources(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3) // f.txt rewritten by each of 3 commits
	ts := serve(t, New(domain.Open(dir)))
	head, first := revParse(t, dir, "HEAD"), revParse(t, dir, "HEAD~2")

	var got numstatWire
	if code := getJSON(t, ts, "/api/numstat?sha="+head, &got); code != http.StatusOK {
		t.Fatalf("sha form: code %d", code)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "f.txt" || got.Files[0].Add != 1 || got.Files[0].Del != 1 {
		t.Fatalf("sha form = %+v, want f.txt +1 -1", got.Files)
	}

	got = numstatWire{}
	if code := getJSON(t, ts, "/api/numstat?left="+first+"&right="+head, &got); code != http.StatusOK {
		t.Fatalf("left/right form: code %d", code)
	}
	if len(got.Files) != 1 || got.Files[0].Add != 1 || got.Files[0].Del != 1 {
		t.Fatalf("left/right form = %+v", got.Files)
	}

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = numstatWire{}
	if code := getJSON(t, ts, "/api/numstat?wt=unstaged", &got); code != http.StatusOK {
		t.Fatalf("wt=unstaged: code %d", code)
	}
	if len(got.Files) != 1 || got.Files[0].Add != 2 || got.Files[0].Del != 1 {
		t.Fatalf("wt=unstaged = %+v, want f.txt +2 -1", got.Files)
	}
	got = numstatWire{}
	if code := getJSON(t, ts, "/api/numstat?wt=staged", &got); code != http.StatusOK || len(got.Files) != 0 {
		t.Fatalf("wt=staged: code %d files %+v, want 200 and none", code, got.Files)
	}
}

func TestNumstatRejectsBadInput(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	for _, q := range []string{"", "sha=-x", "sha=main", "left=abc1234", "left=abc1234&right=--output", "wt=both"} {
		if code := getJSON(t, ts, "/api/numstat?"+q, nil); code != http.StatusBadRequest {
			t.Errorf("?%s: code %d, want 400", q, code)
		}
	}
}
```

Check for a `gitOut` helper first: `grep -n "^func gitOut" internal/web/*_test.go`. If none exists, add it to `numstat_test.go`:

```go
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
```
(with `"os/exec"` imported).

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web -run 'TestNumstat' -count=1`
Expected: FAIL — 404 (no such route) where 200/400 was wanted.

- [ ] **Step 3: Implement** — `internal/web/numstat.go`:

```go
package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/model"
)

// /api/numstat answers a change set's per-file line counts in one request —
// the stacked diff's header badges (+a −d). It is asked ONLY while a stack is
// open, so the listing endpoints and the status poll never pay for a numstat.
// Three sources, the ones git can name by a tree pair:
//
//	?sha=<hex>              a commit against its first parent (CommitFiles' pairing)
//	?left=<hex>&right=<hex> a hash comparison (CompareFiles' pairing)
//	?wt=staged|unstaged     a working-tree section
//
// Entry, shelf and link sets have no git pair; the client fills their counts
// from each file's diff when it loads.
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/numstat", s.handleNumstat)
	})
}

type numstatFile struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
	Add     int    `json:"add"`
	Del     int    `json:"del"`
	Binary  bool   `json:"binary,omitempty"`
}

func (s *Server) handleNumstat(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	q := r.URL.Query()
	var (
		stats []model.DiffStat
		err   error
	)
	switch sha, left, right, wt := q.Get("sha"), q.Get("left"), q.Get("right"), q.Get("wt"); {
	case sha != "" && left == "" && right == "" && wt == "":
		if !isHexSha(sha) {
			writeErr(w, http.StatusBadRequest, errors.New("sha must be a hex commit id"))
			return
		}
		stats, err = svc.CommitStat(r.Context(), sha)
	case left != "" && right != "" && sha == "" && wt == "":
		// hex-only: the pair is the one /api/compare resolved, never a name
		if !isHexSha(left) || !isHexSha(right) {
			writeErr(w, http.StatusBadRequest, errors.New("left/right must be hex commit ids"))
			return
		}
		stats, err = svc.DiffStat(r.Context(), model.DiffSpec{Rev: left + ".." + right})
	case wt == "staged" || wt == "unstaged":
		if sha != "" || left != "" || right != "" {
			writeErr(w, http.StatusBadRequest, errors.New("wt excludes sha/left/right"))
			return
		}
		stats, err = svc.DiffStat(r.Context(), model.DiffSpec{Cached: wt == "staged"})
	default:
		writeErr(w, http.StatusBadRequest, errors.New("want sha, left+right, or wt=staged|unstaged"))
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]numstatFile, len(stats))
	for i, st := range stats {
		out[i] = numstatFile{Path: st.Path, OldPath: st.OldPath, Add: st.Added, Del: st.Deleted, Binary: st.Binary}
	}
	writeJSON(w, map[string]any{"files": out})
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/web -run 'TestNumstat' -count=1 && go vet ./internal/web`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /mnt/t/others/gigagit/.claude/worktrees/stacked-diff-view add internal/web/numstat.go internal/web/numstat_test.go
git -C /mnt/t/others/gigagit/.claude/worktrees/stacked-diff-view commit -m "feat(web): GET /api/numstat — a change set's per-file counts in one request

<trailer>"
```

---

### Task 3: `stacked_diff` preference

**Files:**
- Modify: `internal/promptstate/webui.go` (the `WebUI` struct)
- Modify: `internal/web/uistate.go` (`uiStateWire`, both handlers, the no-store default)
- Modify: `internal/web/uistate_test.go` (`TestUIStateRoundTrips`)
- Modify: `internal/web/static/uistate.js` (the `saveUI` base)

**Interfaces:**
- Produces: wire field `stacked_diff` (bool) on GET/PUT `/api/uistate`; `state.ui.stacked_diff` on the client.

- [ ] **Step 1: Extend the round-trip test** — in `TestUIStateRoundTrips` add `"stacked_diff":true` to `body`, and `|| !st.StackedDiff` to the failure condition.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/web -run TestUIStateRoundTrips -count=1`
Expected: FAIL to compile — `st.StackedDiff undefined`.

- [ ] **Step 3: Implement**

`internal/promptstate/webui.go`, after `SymCompare`:
```go
	// StackedDiff is gg web's stacked diff view: every file of the open change
	// set in one scroll (header + diff per file) instead of one file at a time.
	StackedDiff bool `toml:"stacked_diff,omitempty"`
```

`internal/web/uistate.go` — in `uiStateWire`, after `SymCompare`:
```go
	// StackedDiff is the stacked (all files, one scroll) diff view: on or off
	// (stackview.js).
	StackedDiff bool `json:"stacked_diff"`
```
and add `StackedDiff: st.StackedDiff,` to the GET literal and `StackedDiff: in.StackedDiff,` to the `promptstate.WebUI{…}` literal in the PUT handler.

`internal/web/static/uistate.js` — in the `saveUI` base object, after `sym_compare: false,`:
```js
    stacked_diff: false,
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/web ./internal/promptstate -run 'UIState|WebUI' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit** (`feat(web): remember the stacked diff toggle in /api/uistate`).

---

### Task 4: pure slot model `stack.js` + node harness

**Files:**
- Create: `internal/web/static/stack.js`
- Create: `internal/web/stackjs_test.go`

**Interfaces (all exported from `stack.js`, import-free):**
- `STACK_COLLAPSE_OVER = 100`, `STACK_MAX_IN_FLIGHT = 3`
- `slotKey(f) → string` — `(f.section || "") + "\u0000" + f.path`
- `stackGroup(f) → "staged" | "worktree"` — a status row's stack group
- `stackRows(list, group) → [{f, idx}]` — `group === ""` = every row; else rows whose `stackGroup(f) === group`
- `statusLetter(f) → string` — a commit/compare row's `f.status`; a status row's letter per section (`staged` → `f.staged`, `changes` → `f.unstaged`, `untracked` → `"?"`, `conflicts` → `"U"`)
- `buildSlots(rows, collapse = rows.length > STACK_COLLAPSE_OVER) → slot[]`, slot = `{key, idx, f, path, oldPath, status, counts: null, load: "idle"|"none", collapsed, diff: null, folds: Set, error: "", again: false}` (`load: "none"` for a conflict row — it is never fetched)
- `countsFromDiff(d) → {add, del} | {binary: true} | null`
- `nextToLoad(slots, near: Set<number>, anchor) → index | -1` — the idle, expanded slot in `near` nearest `anchor` (a tie goes to the one BELOW: reading direction)
- `reconcileSlots(old, rows) → slot[]` — reuses old slot OBJECTS by key (so an in-flight load lands on the live object); keeps `collapsed` and `folds`; drops `diff`/`counts` when status or oldPath changed; every other kept slot goes back to `load: "idle"` (a refresh may have changed its content) but keeps showing its old `diff` until the re-fetch lands; a slot mid-load gets `again = true`
- `estimateHeight(slot, rowPx) → px` — placeholder height

- [ ] **Step 1: Write the failing test** — `internal/web/stackjs_test.go`:

```go
package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// stack.js is the pure half of the stacked diff view: which rows a stack
// holds, what loads next, how a working-tree refresh keeps the reader's
// sections. Import-free, so it runs under node against real shapes.
const stackHarness = `
import * as S from "./stack.mjs";
const out = {};
const rows = (n) => Array.from({ length: n }, (_, i) => ({ f: { path: "p" + i, status: "M" }, idx: i }));

// 1. the auto-collapse threshold is "more than 100"
out.hundredOpen = S.buildSlots(rows(100)).every((s) => !s.collapsed);
out.hundredOneShut = S.buildSlots(rows(101)).every((s) => s.collapsed);

// 2. a working-tree stack holds its group only; a conflict is never fetched
const list = [
  { path: "a", section: "changes", unstaged: "M" },
  { path: "b", section: "untracked" },
  { path: "c", section: "conflicts" },
  { path: "a", section: "staged", staged: "A" },
];
const wt = S.stackRows(list, "worktree");
out.wtIdx = wt.map((r) => r.idx);
out.stagedIdx = S.stackRows(list, "staged").map((r) => r.idx);
out.allIdx = S.stackRows(list, "").map((r) => r.idx);
const ws = S.buildSlots(wt);
out.letters = ws.map((s) => s.status).join("");
out.conflictLoad = ws[2].load;
out.keysDiffer = S.slotKey(list[0]) !== S.slotKey(list[3]);

// 3. pick order: nearest the anchor, ties go below, skip collapsed/loaded/far
const sl = S.buildSlots(rows(6));
sl[3].load = "ok";
sl[4].collapsed = true;
out.pick1 = S.nextToLoad(sl, new Set([1, 2, 3, 4, 5]), 3); // 2 and 5 → dist 1 vs 2 → 2
out.pickTie = S.nextToLoad(S.buildSlots(rows(5)), new Set([1, 3]), 2); // tie → 3 (below)
out.pickNone = S.nextToLoad(sl, new Set([3, 4]), 3);

// 4. counts from a diff's aligned rows; binary; none
out.counts = S.countsFromDiff({ rows: [{ kind: "add" }, { kind: "del" }, { kind: "change" }, { kind: "same" }] });
out.bin = S.countsFromDiff({ binary: true });
out.none = S.countsFromDiff(null);

// 5. reconcile keeps objects, collapse and folds; changed status drops the diff
const old = S.buildSlots(rows(3));
old[0].collapsed = true; old[0].folds.add(7);
old[1].diff = { rows: [] }; old[1].load = "ok";
old[2].load = "loading";
const next = S.reconcileSlots(old, [
  { f: { path: "p1", status: "M" }, idx: 0 },
  { f: { path: "new", status: "A" }, idx: 1 },
  { f: { path: "p0", status: "D" }, idx: 2 },
  { f: { path: "p2", status: "M" }, idx: 3 },
]);
out.order = next.map((s) => s.path).join(",");
out.sameObject = next[0] === old[1];
out.keptDiffIdle = next[0].diff !== null && next[0].load === "idle";
out.statusChangeDropsDiff = next[2].diff === null && next[2].collapsed === true && next[2].folds.has(7);
out.midLoadAgain = next[3].again === true && next[3].load === "loading";
out.idxUpdated = next[3].idx === 3;

console.log(JSON.stringify(out));
`

func TestStackJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "stack.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stack.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(stackHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
		t.Fatalf("not the harness JSON: %v\n%s", err, out)
	}
	want := map[string]any{
		"hundredOpen": true, "hundredOneShut": true,
		"letters": "M?U", "conflictLoad": "none", "keysDiffer": true,
		"pick1": float64(2), "pickTie": float64(3), "pickNone": float64(-1),
		"order": "p1,new,p0,p2",
		"sameObject": true, "keptDiffIdle": true, "statusChangeDropsDiff": true,
		"midLoadAgain": true, "idxUpdated": true,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	idx := func(k string) string { b, _ := json.Marshal(got[k]); return string(b) }
	if idx("wtIdx") != "[0,1,2]" || idx("stagedIdx") != "[3]" || idx("allIdx") != "[0,1,2,3]" {
		t.Errorf("stackRows: wt=%s staged=%s all=%s", idx("wtIdx"), idx("stagedIdx"), idx("allIdx"))
	}
	if idx("counts") != `{"add":2,"del":2}` || idx("bin") != `{"binary":true}` || idx("none") != "null" {
		t.Errorf("countsFromDiff: %s %s %s", idx("counts"), idx("bin"), idx("none"))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/web -run TestStackJS -count=1`
Expected: FAIL — `open static/stack.js: no such file`.

- [ ] **Step 3: Implement** — `internal/web/static/stack.js`:

```js
// stack.js — the PURE half of the stacked diff view (stackview.js is the DOM
// half): which rows a stack holds, what loads next, and how a working-tree
// refresh keeps the reader's sections. Import-free on purpose, so
// stackjs_test.go runs it under node.

export const STACK_COLLAPSE_OVER = 100; // a bigger stack opens with every file folded to its header
export const STACK_MAX_IN_FLIGHT = 3; // the server answers reads one at a time under the repo gate

// slotKey is a row's identity across a refresh. A partially staged file is
// listed twice (Changes and Staged), so the section is part of it.
export function slotKey(f) {
  return (f.section || "") + "\u0000" + f.path;
}

// stackGroup: a working-tree stack is the staged files OR everything else
// (changes, untracked, conflicts) — git diff --cached vs git diff.
export function stackGroup(f) {
  return f.section === "staged" ? "staged" : "worktree";
}

// stackRows picks a stack's rows from the active list, keeping each row's
// index in that list (the file cursor's index space). group "" = all rows.
export function stackRows(list, group) {
  const out = [];
  list.forEach((f, idx) => {
    if (!group || stackGroup(f) === group) out.push({ f, idx });
  });
  return out;
}

// statusLetter is the header's status: a listed change carries its own; a
// working-tree row's comes from its section.
export function statusLetter(f) {
  switch (f.section) {
    case "staged":
      return f.staged || "M";
    case "changes":
      return f.unstaged || "M";
    case "untracked":
      return "?";
    case "conflicts":
      return "U";
  }
  return f.status || "";
}

export function buildSlots(rows, collapse = rows.length > STACK_COLLAPSE_OVER) {
  return rows.map(({ f, idx }) => ({
    key: slotKey(f),
    idx,
    f,
    path: f.path,
    oldPath: f.old_path || f.orig_path || "",
    status: statusLetter(f),
    counts: null,
    // a conflict is resolved in the hunk picker, never diffed in a stack
    load: f.section === "conflicts" ? "none" : "idle",
    collapsed: collapse,
    diff: null,
    folds: new Set(), // this file's unfolded runs in the changes-only view (diffHTML's `open`)
    error: "",
    again: false, // a refresh landed mid-load: fetch once more when this one settles
  }));
}

// countsFromDiff derives a header's +a −d from the aligned rows the diff
// endpoint returns — the fallback for sources /api/numstat cannot answer.
export function countsFromDiff(d) {
  if (!d) return null;
  if (d.binary) return { binary: true };
  if (d.too_large) return null;
  let add = 0;
  let del = 0;
  for (const r of d.rows || []) {
    if (r.kind === "add") add++;
    else if (r.kind === "del") del++;
    else if (r.kind === "change") {
      add++;
      del++;
    }
  }
  return { add, del };
}

// nextToLoad picks the next slot to fetch: idle, expanded, near the viewport,
// nearest the current file; a tie goes to the one below (reading direction).
// The near set is re-read at every pick, so a slot scrolled away before its
// turn is simply never picked. -1 = nothing to load.
export function nextToLoad(slots, near, anchor) {
  let best = -1;
  let bestD = Infinity;
  for (const i of near) {
    const s = slots[i];
    if (!s || s.collapsed || s.load !== "idle") continue;
    const d = Math.abs(i - anchor);
    if (d < bestD || (d === bestD && i > best)) {
      bestD = d;
      best = i;
    }
  }
  return best;
}

// reconcileSlots re-lists a working-tree stack after a status re-read. Old
// slot OBJECTS are reused by key, so a load still in flight lands on the
// live slot; the reader's folds survive. A kept slot re-fetches (a refresh
// may have changed its bytes) but keeps painting its old diff meanwhile; a
// status or rename change drops the stale diff and counts outright.
export function reconcileSlots(old, rows) {
  const byKey = new Map(old.map((s) => [s.key, s]));
  const fresh = buildSlots(rows, rows.length > STACK_COLLAPSE_OVER);
  return fresh.map((n) => {
    const o = byKey.get(n.key);
    if (!o) return n;
    const changed = o.status !== n.status || o.oldPath !== n.oldPath;
    o.idx = n.idx;
    o.f = n.f;
    o.status = n.status;
    o.oldPath = n.oldPath;
    if (changed) {
      o.diff = null;
      o.counts = null;
    }
    if (n.load === "none") o.load = "none";
    else if (o.load === "loading") o.again = true;
    else o.load = "idle";
    o.error = "";
    return o;
  });
}

// estimateHeight sizes an unloaded file's placeholder so the scrollbar does
// not lurch as sections load: its changed lines plus context when counted.
export function estimateHeight(slot, rowPx) {
  const c = slot.counts;
  if (!c || c.binary) return 3 * rowPx;
  return Math.min(4000, Math.max(3, c.add + c.del + 6) * rowPx);
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/web -run TestStackJS -count=1 -v 2>&1 | tail -5`
Expected: PASS (or SKIP if node is missing — it is present on this machine; a SKIP here is a failure to investigate).

- [ ] **Step 5: Commit** (`feat(web): stack.js — the pure slot model of the stacked diff view`).

---

### Task 5: `fileDiffURL` — one URL builder for both views

**Files:**
- Modify: `internal/web/static/files.js` (`openFile` ~1043–1145, `openStatusDiff` ~1148–1180)
- Test: `internal/web/stackviewjs_test.go` (new; structural guards grow in later tasks)

**Interfaces:**
- Produces: `fileDiffURL(f) → string | null` (exported from files.js). Status rows → `/api/diff?wt=…`; link/frozen compares → `/api/entry-diff?…`; hash compares → `/api/diff?left=&right=`; commits → `/api/diff?sha=`; a conflict row → `null`.

- [ ] **Step 1: Write the failing guard** — `internal/web/stackviewjs_test.go`:

```go
package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readStatic(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("static", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The stacked view fetches each file through the SAME URL builder the
// single-file view uses, so the two can never disagree on what a row's diff
// is. A regression here is a stack that quietly shows a different diff.
func TestFileDiffURLIsShared(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	if !strings.Contains(files, "function fileDiffURL(f)") {
		t.Fatal("files.js: fileDiffURL is gone")
	}
	if n := strings.Count(files, "getJSON(fileDiffURL("); n < 2 {
		t.Errorf("single-file opens use fileDiffURL %d times, want >= 2 (commit/compare and working tree)", n)
	}
	if strings.Contains(files, `getJSON("/api/diff?" + q)`) {
		t.Error("files.js builds a /api/diff URL inline again — route it through fileDiffURL")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/web -run TestFileDiffURLIsShared -count=1`
Expected: FAIL — `fileDiffURL is gone`.

- [ ] **Step 3: Implement** — in `files.js`, add above `openFile`:

```js
// fileDiffURL is the ONE place a listed file becomes the URL its diff is read
// from, in the screen's current mode — the single-file opens and the stacked
// view's loader (stackview.js) both fetch through it, so the two can never
// show different diffs for one row. null = there is nothing to diff (a
// conflict is resolved in the hunk picker).
function fileDiffURL(f) {
  if (state.filesMode === "status") {
    if (f.section === "conflicts") return null;
    const q = new URLSearchParams({ wt: f.section === "staged" ? "staged" : "unstaged", path: f.path });
    if (f.orig_path) q.set("old", f.orig_path);
    return "/api/diff?" + q;
  }
  const c = state.compare;
  // A link comparison (a row may name its own sides) and a frozen entry
  // compare address their sides by SPEC: one may be a snapshot git cannot read.
  if (state.filesMode === "compare" && (c.links || c.frozen)) {
    const q = new URLSearchParams({
      left: (c.links && f.left_spec) || c.aSpec,
      right: (c.links && f.right_spec) || c.bSpec,
      path: f.path,
    });
    if (f.status) q.set("status", f.status);
    if (c.links && f.old_path) q.set("old_path", f.old_path);
    return "/api/entry-diff?" + q;
  }
  const q = new URLSearchParams({ path: f.path, status: f.status });
  if (state.filesMode === "compare") {
    q.set("left", c.aHash);
    q.set("right", c.bHash);
  } else {
    q.set("sha", f.sha || state.fileSha);
  }
  if (f.old_path) q.set("old", f.old_path);
  return "/api/diff?" + q;
}
```

In `openFile`, delete the block that builds `q` (from `const q = new URLSearchParams({ path: f.path, status: f.status });` through `if (f.old_path) q.set("old", f.old_path);`) and change the fetch to:

```js
    const [d] = await Promise.all([getJSON(fileDiffURL(f)), fetchNotes(false)]);
```

In `openStatusDiff`, delete `const q = new URLSearchParams({ wt: …});` and `if (f.orig_path) q.set("old", f.orig_path);`, and change its fetch the same way. Add `fileDiffURL` to the file's `export { … }` list.

(The links and frozen single-file paths keep going through `openEntryFileDiff`, which the symmetric view also calls with explicit specs — untouched in this plan.)

- [ ] **Step 4: Run to verify**

Run: `go test ./internal/web -run 'TestFileDiffURLIsShared|JS' -count=1`
Expected: PASS (every existing `*js_test.go` still green).

- [ ] **Step 5: Commit** (`refactor(web): fileDiffURL — one URL builder for a listed file's diff`).

---

### Task 6: `stackview.js` — render, lazy load, collapse, sync

**Files:**
- Create: `internal/web/static/stackview.js`
- Modify: `internal/web/static/files.js` (routing + hooks, listed in Step 3)
- Modify: `internal/web/static/core.js` (`state.stack: null`)
- Modify: `internal/web/static/app.js` (`import "./stackview.js";` with the other feature imports)
- Modify: `internal/web/static/style.css`
- Test: `internal/web/stackviewjs_test.go` (extend)

**Interfaces:**
- Consumes: from `stack.js` — every export of Task 4. From `files.js` — `activeFileList, clearDiffHunks, closeConflictPick, diffHTML, enterFilesStage, fileDiffURL, mountPanBars, openFile, openStatusDiff, renderFiles, setLayout, updateDiffNav`. From `core.js` — `$, esc, getJSON, state`. From `keys.js` — `focusPane`. From `symcompare.js` — `symActive`.
- Produces (exported from `stackview.js`): `stackOn() → bool`, `openStack(i)`, `teardownStack()`, `reconcileStack()`, `rerenderStack(resetFolds = false)`, `toggleStacked()`, `collapseCurrent()`, `toggleAllCollapsed()`.
- Produces (files.js): `closeConflictPick()` — `conflictPick = null; renderResolveBar();`.

- [ ] **Step 1: Extend the structural guard** — append to `stackviewjs_test.go`:

```go
// The stack is only reachable if the doors route into it and every exit
// tears it down; each of these lines is one of those doors or exits.
func TestStackViewWired(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	for _, want := range []string{
		"if (stackOn()) return openStack(i);", // openFile routes into the stack
		`if (mode !== "diff") teardownStack();`, // leaving the diff stage drops it
		"if (state.stack) return rerenderStack();", // f / w / resize re-render the stack
		"reconcileStack();",                       // a status re-read keeps its sections
	} {
		if !strings.Contains(files, want) {
			t.Errorf("files.js is missing %q", want)
		}
	}
	view := readStatic(t, "stackview.js")
	for _, want := range []string{
		"new IntersectionObserver(",
		"STACK_MAX_IN_FLIGHT",
		`getJSON("/api/numstat?"`,
		"getJSON(fileDiffURL(",
		"state.ui && state.ui.stacked_diff",
		"saveUI({ stacked_diff:",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("stackview.js is missing %q", want)
		}
	}
	css := readStatic(t, "style.css")
	for _, want := range []string{".stk-head", "position: sticky", "var(--diff-head-h", "#stack-fold-all.hidden"} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css is missing %q", want)
		}
	}
	if !strings.Contains(readStatic(t, "app.js"), `import "./stackview.js";`) {
		t.Error("app.js does not load stackview.js")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/web -run TestStackViewWired -count=1`
Expected: FAIL — missing strings / no stackview.js.

- [ ] **Step 3a: `core.js`** — in the `state` object after `lastDiff: null,`:

```js
  stack: null, // the stacked diff view's live stack (stackview.js), else null
```

- [ ] **Step 3b: `stackview.js`** — create:

```js
// stackview.js — the stacked diff view: every file of the change set on
// screen as one section (a sticky header, its diff below) in ONE scroll —
// GitHub's "Files changed". The pure half (slots, pick order, reconcile,
// counts) is stack.js. files.js routes openFile here while the S toggle is
// on and calls the lifecycle hooks (teardown on leaving the diff stage,
// reconcile after a status re-read, re-render on f / w / resize).
//
// state.stack = {list, group, slots, near, anchor, inFlight}. list is the
// active file list ARRAY it was built from: a new commit, a filter change or
// a status re-read hands out a new array, which is how openStack knows a
// click is a scroll within this stack or a new one.

import { $, esc, getJSON, state } from "./core.js";
import { saveUI } from "./uistate.js";
import { registerHelp } from "./menus.js";
import { focusPane } from "./keys.js";
import { symActive } from "./symcompare.js";
import {
  activeFileList,
  clearDiffHunks,
  closeConflictPick,
  diffHTML,
  enterFilesStage,
  fileDiffURL,
  mountPanBars,
  openFile,
  openStatusDiff,
  renderFiles,
  setLayout,
  updateDiffNav,
} from "./files.js";
import {
  STACK_MAX_IN_FLIGHT,
  buildSlots,
  countsFromDiff,
  estimateHeight,
  nextToLoad,
  reconcileSlots,
  stackGroup,
  stackRows,
} from "./stack.js";

const ROW_PX = 20; // a diff row's rendered height, for placeholder estimates
let observer = null;
let syncRaf = 0;

// stackOn: the toggle is on AND this screen can stack. The symmetric view
// keeps its single diff until plan 2 teaches it the stack.
function stackOn() {
  return !!(state.ui && state.ui.stacked_diff) && !symActive();
}

function teardownStack() {
  if (observer) observer.disconnect();
  observer = null;
  state.stack = null;
  syncStackChrome();
}

// openStack shows list row i inside a stack: a scroll when the stack on
// screen was built from this same list (and working-tree group), else a new
// stack anchored there. openFile has already put the diff stage up.
function openStack(i) {
  const list = activeFileList();
  const group = state.filesMode === "status" ? stackGroup(list[i] || {}) : "";
  const st = state.stack;
  if (st && st.list === list && st.group === group) return scrollToFile(st, i);
  buildStack(list, group, i);
}

async function buildStack(list, group, anchorIdx) {
  teardownStack();
  state.detailGen++; // a single-file diff still loading must not land over the stack
  clearDiffHunks();
  closeConflictPick();
  state.lastDiff = null; // resize / f / live refresh must not repaint a single diff here
  state.diffCtx = null;
  state.diffRow = null;
  state.notes = [];
  if (state.layout !== "diff") {
    state.pane = "files";
    setLayout("diff");
    focusPane();
  }
  const slots = buildSlots(stackRows(list, group));
  const st = { list, group, slots, near: new Set(), anchor: 0, inFlight: 0 };
  state.stack = st;
  // Counts FIRST: they size every placeholder. Painted with no counts, all
  // sections are a few rows tall, the whole change set sits "near" the
  // viewport, and the loader fetches the first three files wherever the
  // reader is — lazy in name only. (Best-effort: no counts → small
  // placeholders, as for entry/link sets.)
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  await loadCounts(st);
  if (state.stack !== st) return; // superseded while the counts loaded
  paintStack(st);
  scrollToFile(st, anchorIdx);
}

// --- painting -------------------------------------------------------------

function headHTML(s) {
  const path =
    s.oldPath && s.oldPath !== s.path ? `${esc(s.oldPath)} → ${esc(s.path)}` : esc(s.path);
  const c = s.counts;
  const counts = !c
    ? ""
    : c.binary
    ? `<span class="stk-bin">bin</span>`
    : `<span class="stk-add">+${c.add}</span> <span class="stk-del">−${c.del}</span>`;
  return (
    `<div class="stk-head" title="${esc(s.path)} — click to collapse / expand (-)">` +
    `<span class="stk-fold">${s.collapsed ? "▸" : "▾"}</span>` +
    `<span class="st ${esc(s.status)}">${esc(s.status)}</span>` +
    `<span class="stk-path">${path}</span>` +
    `<span class="stk-counts">${counts}</span></div>`
  );
}

function bodyHTML(s) {
  if (s.collapsed) return "";
  if (s.load === "none") {
    return `<div class="notice">conflicted — <button class="stk-resolve">open resolver</button></div>`;
  }
  if (s.load === "error") return `<div class="notice">error: ${esc(s.error)}</div>`;
  // a kept slot re-fetching after a refresh paints its old diff until the new one lands
  if (s.diff) return diffHTML(s.diff, $("diff-pane").clientWidth, false, s.folds);
  return `<div class="stk-ph" style="height:${estimateHeight(s, ROW_PX)}px">loading…</div>`;
}

function sectionHTML(s, k) {
  return (
    `<section class="stk-file${s.collapsed ? " collapsed" : ""}" data-k="${k}">` +
    headHTML(s) +
    `<div class="stk-body">${bodyHTML(s)}</div></section>`
  );
}

function paintStack(st) {
  const body = $("diff-body");
  body.innerHTML = `<div class="stk">${st.slots.map(sectionHTML).join("")}</div>`;
  // the file headers stick just under the pane's own sticky toolbar
  $("diff-pane").style.setProperty("--diff-head-h", $("diff-header").offsetHeight + "px");
  const n = st.slots.length;
  $("diff-title").textContent = `${n} file${n === 1 ? "" : "s"} · stacked`;
  if (observer) observer.disconnect();
  observer = new IntersectionObserver(
    (entries) => {
      if (state.stack !== st) return;
      for (const e of entries) {
        const k = Number(e.target.dataset.k);
        if (e.isIntersecting) st.near.add(k);
        else st.near.delete(k);
      }
      pump(st);
    },
    { root: $("diff-pane"), rootMargin: "100% 0px" }
  );
  st.near = new Set();
  for (const el of body.querySelectorAll(".stk-file")) observer.observe(el);
  mountPanBars(body, $("diff-hbars"));
  syncStackChrome();
  updateDiffNav();
}

function sectionEl(k) {
  return document.querySelector(`#diff-body .stk-file[data-k="${k}"]`);
}

function repaintSlot(st, k) {
  const el = sectionEl(k);
  if (!el) return;
  const s = st.slots[k];
  el.classList.toggle("collapsed", s.collapsed);
  el.querySelector(".stk-head").outerHTML = headHTML(s);
  el.querySelector(".stk-body").innerHTML = bodyHTML(s);
  mountPanBars($("diff-body"), $("diff-hbars"));
  updateDiffNav(); // the ‹ change › buttons count the rendered change runs
}

// rerenderStack repaints every loaded body for a new width, the f view or
// the w text mode. Browser scroll anchoring keeps the reader's place.
function rerenderStack(resetFolds = false) {
  const st = state.stack;
  if (!st) return;
  // #diff-header wraps (flex-wrap) at narrow widths: re-measure what the
  // file headers stick under
  $("diff-pane").style.setProperty("--diff-head-h", $("diff-header").offsetHeight + "px");
  st.slots.forEach((s, k) => {
    if (resetFolds) s.folds = new Set();
    if (s.diff && !s.collapsed) repaintSlot(st, k);
  });
}

// --- loading --------------------------------------------------------------

function pump(st) {
  while (state.stack === st && st.inFlight < STACK_MAX_IN_FLIGHT) {
    const k = nextToLoad(st.slots, st.near, st.anchor);
    if (k < 0) return;
    load(st, st.slots[k]);
  }
}

async function load(st, s) {
  s.load = "loading";
  st.inFlight++;
  try {
    const d = await getJSON(fileDiffURL(s.f));
    s.diff = d;
    s.load = "ok";
    if (!s.counts) s.counts = countsFromDiff(d);
  } catch (e) {
    s.load = "error";
    s.error = e.message || String(e);
  }
  if (state.stack !== st) return; // the stack left the screen while this loaded
  st.inFlight--;
  if (s.again) {
    s.again = false;
    s.load = "idle"; // a refresh landed mid-load: this answer may predate it
  }
  const k = st.slots.indexOf(s);
  if (k >= 0) repaintSlot(st, k);
  pump(st);
}

// numstatQuery names the change set to /api/numstat, or null when git has no
// tree pair for it (entry, shelf and link sets count from each loaded diff).
function numstatQuery(st) {
  if (state.filesMode === "status") return new URLSearchParams({ wt: st.group === "staged" ? "staged" : "unstaged" });
  if (state.filesMode === "compare") {
    const c = state.compare;
    if (!c || c.links || c.frozen || !c.aHash || !c.bHash) return null;
    return new URLSearchParams({ left: c.aHash, right: c.bHash });
  }
  if (!state.fileSha || st.slots.some((s) => s.f.sha && s.f.sha !== state.fileSha)) return null;
  return new URLSearchParams({ sha: state.fileSha });
}

async function loadCounts(st) {
  const q = numstatQuery(st);
  if (!q) return;
  let r;
  try {
    r = await getJSON("/api/numstat?" + q);
  } catch {
    return; // counts are decoration: each loaded diff still fills its own
  }
  if (state.stack !== st) return;
  const by = new Map((r.files || []).map((x) => [x.path, x]));
  st.slots.forEach((s, k) => {
    const x = by.get(s.path);
    if (!x) return;
    s.counts = x.binary ? { binary: true } : { add: x.add, del: x.del };
    const el = sectionEl(k); // absent on a first build: paintStack comes after
    if (!el) return;
    el.querySelector(".stk-head").outerHTML = headHTML(s);
    if (!s.diff && !s.collapsed) el.querySelector(".stk-body").innerHTML = bodyHTML(s); // re-size the placeholder
  });
}

// --- navigation -----------------------------------------------------------

// scrollToFile brings list row i's header to the top of the pane, expanding
// it (which lets the loader fetch it). A row outside this stack (the other
// working-tree group) builds that group's stack instead.
function scrollToFile(st, i, expand = true) {
  const k = st.slots.findIndex((s) => s.idx === i);
  if (k < 0) return buildStack(st.list, stackGroup(st.list[i] || {}), i);
  const s = st.slots[k];
  if (expand && s.collapsed) {
    s.collapsed = false;
    repaintSlot(st, k);
  }
  st.anchor = k;
  state.fileCursor = i;
  const pane = $("diff-pane");
  const el = sectionEl(k);
  if (el) {
    pane.scrollTop +=
      el.getBoundingClientRect().top - pane.getBoundingClientRect().top - $("diff-header").offsetHeight;
  }
  renderFiles();
  updateDiffNav();
  pump(st);
}

// topSlot: the section whose header sits at (or last passed) the line just
// under the pane's toolbar — the file being read. Binary search: sections
// are in document order.
function topSlot() {
  const line = $("diff-pane").getBoundingClientRect().top + $("diff-header").offsetHeight + 1;
  const els = document.querySelectorAll("#diff-body .stk-file");
  let lo = 0;
  let hi = els.length - 1;
  let ans = els.length ? 0 : -1;
  while (lo <= hi) {
    const m = (lo + hi) >> 1;
    if (els[m].getBoundingClientRect().top <= line) {
      ans = m;
      lo = m + 1;
    } else hi = m - 1;
  }
  return ans;
}

function syncCursor() {
  syncRaf = 0;
  const st = state.stack;
  if (!st) return;
  const k = topSlot();
  if (k < 0 || k === st.anchor) return;
  st.anchor = k;
  state.fileCursor = st.slots[k].idx;
  renderFiles(); // the list highlight follows the file being read
  updateDiffNav();
}

$("diff-pane").addEventListener("scroll", () => {
  if (state.stack && !syncRaf) syncRaf = requestAnimationFrame(syncCursor);
});

// --- collapse -------------------------------------------------------------

function toggleSlot(k) {
  const st = state.stack;
  const s = st && st.slots[k];
  if (!s) return;
  s.collapsed = !s.collapsed;
  repaintSlot(st, k);
  pump(st);
}

function collapseCurrent() {
  if (state.stack) toggleSlot(state.stack.anchor);
}

// toggleAllCollapsed: every file folded → expand all; otherwise fold all.
function toggleAllCollapsed() {
  const st = state.stack;
  if (!st) return;
  const expand = st.slots.every((s) => s.collapsed);
  for (const s of st.slots) s.collapsed = !expand;
  const at = st.slots[st.anchor] ? st.slots[st.anchor].idx : 0;
  paintStack(st);
  scrollToFile(st, at, false); // keep the reader's file at the top, folded or not
}

$("diff-body").addEventListener("click", (e) => {
  const st = state.stack;
  if (!st) return;
  const sec = e.target.closest(".stk-file");
  if (!sec) return;
  const k = Number(sec.dataset.k);
  if (e.target.closest(".stk-resolve")) {
    const i = st.slots[k].idx;
    teardownStack();
    openStatusDiff(i); // sets the title/ctx the picker's exits expect, then opens it
    return;
  }
  if (e.target.closest(".stk-head")) return toggleSlot(k);
  // a fold row unfolds that run of THIS file (the single-file listener
  // ignores clicks while no single diff is open)
  const tr = e.target.closest("tr.fold[data-fold]");
  if (tr) {
    st.slots[k].folds.add(Number(tr.dataset.fold));
    repaintSlot(st, k);
  }
});

// --- lifecycle hooks ------------------------------------------------------

// reconcileStack follows a status re-read: the working-tree stack keeps its
// sections, their folds and the file being read.
function reconcileStack() {
  const st = state.stack;
  if (!st || state.filesMode !== "status") return;
  const rows = stackRows(state.statusEntries, st.group);
  if (!rows.length) {
    enterFilesStage(); // the group emptied (all staged / unstaged): back to the list (tears the stack down)
    return;
  }
  const anchorKey = st.slots[st.anchor] && st.slots[st.anchor].key;
  st.list = state.statusEntries;
  st.slots = reconcileSlots(st.slots, rows);
  const k = Math.max(0, st.slots.findIndex((s) => s.key === anchorKey));
  paintStack(st);
  scrollToFile(st, st.slots[k].idx);
  loadCounts(st); // a refresh changes counts too; heads and placeholders repaint in place
}

// --- the toggle -----------------------------------------------------------

function syncStackChrome() {
  const on = !!(state.ui && state.ui.stacked_diff);
  const btn = $("stack-btn");
  btn.classList.toggle("on", on);
  btn.setAttribute("aria-pressed", on ? "true" : "false");
  const chip = document.querySelector('#foot button[data-act="stacked"]');
  if (chip) chip.classList.toggle("on", on);
  $("stack-fold-all").classList.toggle("hidden", !state.stack);
}

// toggleStacked is S: flip the preference; on the diff stage, re-open the
// current file in the other view (the file being read stays on screen).
function toggleStacked() {
  const on = !(state.ui && state.ui.stacked_diff);
  saveUI({ stacked_diff: on });
  syncStackChrome();
  if (state.layout !== "diff" || symActive()) return;
  if (!on) teardownStack();
  openFile(state.fileCursor);
}

$("stack-btn").addEventListener("click", toggleStacked);
$("stack-fold-all").addEventListener("click", toggleAllCollapsed);

window.addEventListener("resize", () => {
  if (state.stack) rerenderStack();
});

registerHelp({
  key: "S · stacked diff",
  html:
    "show <b>every file</b> of the open commit, comparison or working-tree section in ONE scroll — " +
    "a header per file (status, path, <b>+added −deleted</b>) with its diff below, like GitHub's " +
    "<i>Files changed</i>. Files load as they scroll into view; a change set of more than 100 files " +
    "opens with every file folded to its header. Clicking a file in the list scrolls to it, and the " +
    "list follows the file you are reading. The <b>stacked</b> chip in the diff toolbar is the same " +
    "switch; the choice is remembered per machine. Search (/) works in the single-file view",
});
registerHelp({
  key: "- / _ · fold files",
  html:
    "in the stacked diff: <b>-</b> folds (or unfolds) the file you are reading to its header, " +
    "<b>_</b> folds every file (or, when all are folded, unfolds them all). A click on a file's " +
    "header does the same for that file",
});

export { collapseCurrent, openStack, reconcileStack, rerenderStack, stackOn, teardownStack, toggleAllCollapsed, toggleStacked };
```

- [ ] **Step 3c: `files.js` hooks** — imports: add
  `import { openStack, reconcileStack, rerenderStack, stackOn, teardownStack } from "./stackview.js";`

  1. `openFile` — right after the existing `updateDiffNav();` (before `if (state.filesMode === "status") return openStatusDiff(i);`):
     ```js
       // The stacked view (S) shows every file in one scroll: this row is a
       // place in it, not a diff of its own (stackview.js).
       if (stackOn()) return openStack(i);
       teardownStack(); // a single-file open replaces any stack on screen
     ```
  2. `setLayout` — first line of the body:
     ```js
       if (mode !== "diff") teardownStack(); // the stack lives in the diff stage only
     ```
  3. `rerenderDiffKeepingPlace` — first line:
     ```js
       if (state.stack) return rerenderStack();
     ```
  4. `toggleDiffView` — replace `rerenderDiffKeepingPlace(keepScroll);` with:
     ```js
       if (state.stack) rerenderStack(on); // flipping ON starts every file folded
       else rerenderDiffKeepingPlace(keepScroll);
     ```
  5. `reconcileStatusView` — last line of the function:
     ```js
       reconcileStack();
     ```
  6. Add, next to `openConflictPicker`, and export it:
     ```js
     // closeConflictPick drops an open hunk picker's state (a stack is taking
     // the pane) and hides its bar.
     function closeConflictPick() {
       conflictPick = null;
       renderResolveBar();
     }
     ```
  7. `updateDiffNav` — the history/blame buttons already disable on `!state.diffCtx` (null in a stack). No change.

- [ ] **Step 3d: `app.js`** — add `import "./stackview.js";` next to `import "./review.js";`.

- [ ] **Step 3e: `style.css`** — append:

```css
/* Stacked diff (stackview.js): one bordered section per file; its header
   sticks just under #diff-header (whose height paintStack writes into
   --diff-head-h). overflow-anchor stays auto: a section above the viewport
   growing as it loads must not push the reader's file away. */
.stk { padding: 4px 6px 24px; }
.stk-file { border: 1px solid var(--border); border-radius: 4px; margin: 8px 0; }
.stk-head {
  position: sticky; top: var(--diff-head-h, 0px); z-index: 1;
  display: flex; align-items: center; gap: 8px; padding: 4px 8px;
  background: var(--bg-alt); border-bottom: 1px solid var(--border); cursor: pointer;
}
.stk-file.collapsed .stk-head { border-bottom: none; }
.stk-fold { color: var(--dim); width: 1em; }
.stk-path { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.stk-counts { color: var(--dim); white-space: nowrap; }
.stk-add { color: #7bd88f; }
.stk-del { color: #f27a6a; }
.stk-bin { color: var(--dim); }
.stk-ph { color: var(--dim); padding: 8px; }
#stack-btn.on, #foot button[data-act="stacked"].on { color: var(--accent); }
#stack-fold-all.hidden { display: none; }
```

- [ ] **Step 3f: `index.html`** — in `#diff-nav`, after the `diff-mode` button:

```html
        <button id="stack-btn" title="every file in one scroll ↔ one file at a time (S)" aria-pressed="false">stacked</button>
        <button id="stack-fold-all" class="hidden" title="fold / unfold every file (_)">fold all</button>
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/web -count=1 2>&1 | tail -15 && go build -o /tmp/claude-1000/-mnt-t-others-gigagit/9ec3cca2-ba50-4bc2-8475-5c9d5f80f797/scratchpad/gg ./cmd/gg`
Expected: PASS; binary builds. (A JS syntax error only shows in the browser — Task 8's probe is the runtime check.)

- [ ] **Step 5: Commit** (`feat(web): stacked diff view — every file in one scroll, lazy-loaded`).

---

### Task 7: keys, footer chip, palette

**Files:**
- Modify: `internal/web/static/keys.js` (keydown chain; `moveCursor`; footer click switch)
- Modify: `internal/web/static/index.html` (footer chip)
- Modify: `internal/web/static/palette.js` (UI group row)
- Test: `internal/web/stackviewjs_test.go` (extend)

**Interfaces:**
- Consumes: `toggleStacked, collapseCurrent, toggleAllCollapsed` (Task 6); `toast(text)` from `toast.js`.

- [ ] **Step 1: Extend the guard** — append:

```go
func TestStackKeysWired(t *testing.T) {
	t.Parallel()
	keys := readStatic(t, "keys.js")
	for _, want := range []string{
		`e.key === "S"`, "toggleStacked()",
		`e.key === "-"`, "collapseCurrent()",
		`e.key === "_"`, "toggleAllCollapsed()",
		`case "stacked": toggleStacked(); break;`,
		"if (state.stack && state.layout === \"diff\") return openFile(state.fileCursor);",
	} {
		if !strings.Contains(keys, want) {
			t.Errorf("keys.js is missing %q", want)
		}
	}
	if !strings.Contains(readStatic(t, "index.html"), `data-act="stacked"`) {
		t.Error("the footer has no stacked chip (advertise in help AND footer)")
	}
	if !strings.Contains(readStatic(t, "palette.js"), "toggleStacked()") {
		t.Error("the ☰ menu has no stacked-diff row")
	}
}
```

- [ ] **Step 2: Run to verify it fails** — `go test ./internal/web -run TestStackKeysWired -count=1` → FAIL.

- [ ] **Step 3: Implement**

`keys.js` imports: add `import { collapseCurrent, toggleAllCollapsed, toggleStacked } from "./stackview.js";` and `import { toast } from "./toast.js";` (skip whichever is already imported).

`moveCursor` — the files branch becomes:
```js
    state.fileCursor = Math.max(0, Math.min(list.length - 1, state.fileCursor + delta));
    // In a stack j/k walk the sections: the cursor's file scrolls into view.
    if (state.stack && state.layout === "diff") return openFile(state.fileCursor);
    renderFiles();
```
(`openFile` is exported by files.js; add it to keys.js's files.js import if missing.)

Keydown chain — insert before the `} else if (e.key === "/") {` branch:
```js
  } else if (e.key === "S" && !e.ctrlKey && !e.metaKey && !e.altKey) {
    toggleStacked(); // every file in one scroll ↔ one file at a time
  } else if (e.key === "-" && state.stack) {
    e.preventDefault();
    collapseCurrent();
  } else if (e.key === "_" && state.stack) {
    e.preventDefault();
    toggleAllCollapsed();
  } else if (e.key === "/" && state.stack) {
    // in-view search reads ONE diff; the stack has many (a follow-up plan)
    e.preventDefault();
    toast("search works in the single-file view — S switches");
```

Footer click switch — add `case "stacked": toggleStacked(); break;`.

`index.html` footer — after the `textmode` chip: `<button data-act="stacked">S stacked</button>`.

`palette.js` — import `toggleStacked` from `./stackview.js`; in the UI group after `toggle file list`:
```js
    { label: "toggle stacked diff (S)", act: () => toggleStacked() },
```

- [ ] **Step 4: Run** — `go test ./internal/web -count=1 2>&1 | tail -5` → PASS.

- [ ] **Step 5: Commit** (`feat(web): S / - / _ keys, footer chip and menu row for the stacked diff`).

---

### Task 8: browser probe (Playwright, scratchpad — not committed)

**Files:**
- Create (scratchpad only): `$SP/stack-probe/probe.mjs`, `$SP/stack-probe/mkrepo.sh`
  where `SP=/tmp/claude-1000/-mnt-t-others-gigagit/9ec3cca2-ba50-4bc2-8475-5c9d5f80f797/scratchpad`

- [ ] **Step 1: Fixture** — `mkrepo.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
R="$1"; rm -rf "$R"; mkdir -p "$R"; cd "$R"
git init -q -b main
git config user.email p@p; git config user.name p
echo base > base.txt; git add -A; git commit -qm base
for f in a b c; do seq 1 400 | sed "s/^/$f line /" > "$f.txt"; done
git add -A; git commit -qm three          # HEAD~1: 3 files, 400 lines each
mkdir many; for i in $(seq 1 120); do echo "$i" > "many/f$i.txt"; done
git add -A; git commit -qm many           # HEAD: 120 files
```

- [ ] **Step 2: Probe** — `probe.mjs` (Playwright from the scratchpad's `node_modules`; `npm i playwright && npx playwright install chromium` there once):

```js
import { chromium } from "playwright";
import { spawn } from "node:child_process";

const [gg, repo, port] = process.argv.slice(2);
// A PRIVATE state dir: S writes /api/uistate, which must never flip the
// user's real prefs (and a pre-set pref would invert every S below).
const srv = spawn(gg, ["web", "--addr", `127.0.0.1:${port}`, "--no-open"], {
  cwd: repo,
  stdio: "inherit",
  env: { ...process.env, XDG_STATE_HOME: `${repo}/../state-${port}` },
});
const fail = (m) => { console.error("FAIL:", m); srv.kill(); process.exit(1); };
await new Promise((r) => setTimeout(r, 1500));
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1600, height: 900 } });
const diffs = [];
p.on("request", (r) => { if (r.url().includes("/api/diff?")) diffs.push(r.url()); });
await p.goto(`http://127.0.0.1:${port}/`);
// prove the port is OURS (memory: probing while other agents share the machine)
const info = await (await p.request.get(`http://127.0.0.1:${port}/api/repo`)).json();
if (!JSON.stringify(info).includes(repo.split("/").pop())) fail("port serves another repo");
await p.waitForSelector(".crow");

// commit "three": open it, open its first file, press S
await p.locator(".crow", { hasText: "three" }).first().click();
await p.locator("#files-list li").first().click();
await p.keyboard.press("S");
await p.waitForSelector(".stk-file");
const heads = await p.locator(".stk-head:visible").count();
if (heads !== 3) fail(`3 visible headers wanted, got ${heads}`);
await p.waitForTimeout(800);
const loaded = new Set(diffs.map((u) => new URL(u).searchParams.get("path")));
if (loaded.has("c.txt")) fail("c.txt (far below) loaded before it was near the viewport");
const counts = await p.locator(".stk-file").first().locator(".stk-counts").innerText();
if (!counts.includes("+400")) fail(`first header counts = ${JSON.stringify(counts)}`);
await p.locator("#files-list li", { hasText: "c.txt" }).click();
await p.waitForFunction(() => {
  const s = document.querySelector('.stk-file[data-k="2"]');
  return s && !s.querySelector(".stk-ph") && s.querySelector("table.diff");
});
const top = await p.locator('.stk-file[data-k="2"] .stk-head').boundingBox();
const bar = await p.locator("#diff-header").boundingBox();
if (Math.abs(top.y - (bar.y + bar.height)) > 4) fail(`c.txt header not at the pane top (${top.y} vs ${bar.y + bar.height})`);
await p.screenshot({ path: "stack-three.png" });

// commit "many": 120 files open folded, and nothing is fetched
await p.keyboard.press("Escape"); await p.keyboard.press("Escape");
diffs.length = 0;
await p.locator(".crow", { hasText: "many" }).first().click();
await p.locator("#files-list li").nth(5).click();
await p.waitForSelector(".stk-file");
const folded = await p.locator(".stk-file.collapsed").count();
if (folded !== 119) fail(`119 folded (all but the clicked one) wanted, got ${folded}`);
await p.waitForTimeout(800);
if (diffs.length !== 1) fail(`exactly the clicked file fetched, got ${diffs.length}`);

// S back: single-file view on the file being read
await p.keyboard.press("S");
await p.waitForSelector("#diff-path");
if ((await p.locator(".stk-file").count()) !== 0) fail("stack still on screen after S");
await p.screenshot({ path: "stack-off.png" });
console.log("PASS");
await b.close(); srv.kill();
```

(Check `gg web --help` for the no-browser flag name before running; drop `--no-open` if the flag differs.)

- [ ] **Step 3: Run it against the UNFIXED build first** (memory: a browser check must be seen failing):

```bash
SP=/tmp/claude-1000/-mnt-t-others-gigagit/9ec3cca2-ba50-4bc2-8475-5c9d5f80f797/scratchpad
bash $SP/stack-probe/mkrepo.sh $SP/stack-probe/repo
(cd /mnt/t/others/gigagit && go build -o $SP/gg-main ./cmd/gg)
cd $SP/stack-probe && node probe.mjs $SP/gg-main $SP/stack-probe/repo 47811
```
Expected: `FAIL: …` (no `.stk-file` ever appears → timeout).

- [ ] **Step 4: Run it against the branch build**

```bash
(cd /mnt/t/others/gigagit/.claude/worktrees/stacked-diff-view && go build -o $SP/gg ./cmd/gg)
cd $SP/stack-probe && node probe.mjs $SP/gg $SP/stack-probe/repo 47812
```
Expected: `PASS`. Read `stack-three.png` and `stack-off.png` and check: headers sticky and legible, counts coloured, c.txt at the top, no horizontal page scroll. Fix and repeat until PASS twice in a row.

- [ ] **Step 5: No commit** (scratchpad only). Note the probe command in the CHANGELOG entry's verification line (Task 9).

---

### Task 9: docs, full gate, deliver

**Files:**
- Modify: `CHANGELOG.md` (Unreleased, top), `README.md` (web section — the diff-pane feature list), `docs/web-tui-parity.md` (new row: stacked diff — web ✓ / TUI plan 3), `docs/CLAUDE-details.md` (web section: stackview/stack.js, `/api/numstat`, the toggle's teardown points)

- [ ] **Step 1: Write the docs.** CHANGELOG entry (adapt wording to the file's style):

```markdown
- **web: stacked diff (`S`)** — every file of the open commit, comparison,
  preview/PR or working-tree section in ONE scroll: a sticky header per file
  (status, path, `+added −deleted`) with its diff below, GitHub-style. Files
  load as they near the viewport (≤3 at a time); more than 100 files open
  folded to their headers; `-` folds the current file, `_` folds / unfolds
  all, a header click folds one. The file list follows the file being read and
  a click scrolls to it. Remembered per machine (`/api/uistate`
  `stacked_diff`). New `GET /api/numstat` (asked only while a stack is open).
  The symmetric compare and the TUI follow in their own plans; notes, search
  and hunk staging inside the stack are follow-ups.
```

- [ ] **Step 2: Full gate**

Run (start immediately; other sessions' runs are fine — memory):
`cd /mnt/t/others/gigagit/.claude/worktrees/stacked-diff-view && ./test.sh race 2>&1 | tail -30`
Expected: all stages PASS. On a failure: fix, re-run the failing package, then the gate.

- [ ] **Step 3: Commit** (`docs: stacked diff view (web core) — changelog, readme, parity, details`).

- [ ] **Step 4: Deliver a verify binary** (memory: always, unprompted) — build `gg` in the worktree, send it with SendUserFile plus its absolute path, and give the user the 3-step manual check: open a commit → open a file → press `S`.

- [ ] **Step 5: Stop for review.** Do NOT merge; the user merges (`gg merge`) — then `./build.sh install` and `./build.sh web` (memory) after they say so.
```

## Self-review notes (plan author)

- Spec §4 toggle / §5.1 slot / §6.1 loading / §6.3 collapse / §7 header / §8
  web nav / §9 working tree → Tasks 3–7. §10 symmetric → plan 2 (explicitly
  excluded via `stackOn()`'s `!symActive()`). §11 follow-ups: the slot carries
  `folds`, `f`, `idx`; notes/search/hunks attach later (the `/` toast marks the
  seam). Spec's `activeDiff()` accessor: v1 needs none — every single-diff
  global is nulled while a stack is up and each reader already no-ops on null
  (history/blame buttons, search, fold listener, live re-render); plan 4a
  introduces the accessor when notes need per-slot context. Recorded here so
  plan 4a does not assume it exists.
- Link/steer landing (spec §8): `openFile(i)` lands on the file header in a
  stack; `revealDiffRow` finds no row (it reads `state.lastDiff`, null) and
  returns null — the same miss it reports today for an absent line, no throw.
  Line landing is plan 4a.
- Verified during planning: `diffHTML`'s attention bands read
  `state.diffCtx` only when `notesOn` (false in a stack) — safe with a null
  ctx. The » fold goes through `rerenderDiffKeepingPlace` (hooked); the pane
  resizer drag re-renders no diff today either (parity). A compare filter
  change hands out a new `state.files` array and `openFile(0)` → a rebuild.
- `/api/numstat?left&right` uses `DiffStat` (`git diff --numstat`, no
  explicit `-M`): a user with `diff.renames=false` sees a rename's counts on
  two paths (the header then falls back to the loaded diff's counts). Minor;
  not fixed here.
- Mixed single-/two-column tables in one stack share one set of pan bars
  (`mountPanBars` measures the first table's column count): acceptable for
  `w` = scroll in v1; revisit if the probe screenshot shows it.
