# Links in, links out (plan 3b-1) — Implementation Plan

> **Execution:** THIS session, task by task, in the worktree
> `/mnt/t/others/gigagit/.claude/worktrees/links-tui` (branch `feat/links-tui`).
> **Never subagents** (CLAUDE.md). Steps use `- [ ]` for tracking.

**Goal:** Every link the TUI shows can be copied, every copy is recorded, and
the `#` prompt can paste one back from the history — with a stash link that is
actually correct.

**Architecture:** Link *describing* and *recording* move into `domain` so the
TUI stops being the one frontend that cannot reach them; the TUI records at
its single clipboard chokepoint. The compare algebra learns §3.4 (a `-u`
stash's untracked files) through a per-path byte source on `FileSet`. Four
copy surfaces and one reusable history-picker component sit on top.

**Tech stack:** Go 1.26, Bubble Tea, real `git` in `t.TempDir()`, the e2e
TOML harness, `./tui-capture.sh` for headless confirmation.

**Spec:** `docs/superpowers/specs/2026-09-19-links-tui-design.md` (§3.1–3.4,
§3.10), under `docs/superpowers/specs/2026-09-16-unified-links-design.md`
(§3.4, §5, §5.2).

## Global constraints

- A pair is BOUNDED, a point is UNBOUNDED; compare is closed. Nothing here
  may make a frontend screen a pair.
- **`stash@{N}` never appears in a link** (§3.4 rule 1). It is an input to a
  resolve, never output.
- A link is built through `model.Link{…}.String()` — never string
  concatenation — and every producer refuses what the grammar cannot hold
  (`LinkRefOK`, `LinkPathOK`, `LinkAbsOK`, full shas ≥ 40).
- `internal/tui` never imports `internal/git`, `internal/linkhist` or
  `internal/savedcompare` (archtest).
- Recording is BEST-EFFORT: it never fails, delays or re-words a copy.
- Every user-visible TUI string goes through `i18n.T` with a literal key in
  **ja, ko, zh, ru**. Engine/CLI prose stays English.
- New tests call `t.Parallel()` unless they touch process-global state
  (`t.Setenv`, package vars) — those stay serial and say why.
- **"Watch it fail" means with the GUARD removed.** Each task has a break
  table; run every row, restore, re-run green. Two arms that look alike get
  ONE fixture on which they must disagree.
- Commit trailers on every commit:
  `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01XuwzeyRENXkAu7UGZ5J3Up`.
- Every shell command is prefixed `cd /mnt/t/others/gigagit/.claude/worktrees/links-tui &&`
  (the shell cwd resets). Gate exit codes are captured with
  `> log 2>&1; echo EXIT=$?`, never through `| tail`.

## File map

| file | change |
|---|---|
| `internal/domain/linkdesc.go` (new) | `LinkDesc`, `DescMax`, `truncateDesc`, `DescribeLink`, `RecordCopiedLink` |
| `internal/domain/linkdescjs_test.go` (moved from `internal/cli`) | the Go↔JS gate, repointed |
| `internal/domain/linkhiststore.go` | `UseLinkHistDir` per-service seam |
| `internal/cli/link.go`, `internal/cli/compare.go` | call `svc.DescribeLink`; the moved functions deleted |
| `internal/git/parents.go` (new) | `CommitParents` verb |
| `internal/domain/stashshape.go` (new) | `stashShape`, `StashPair` |
| `internal/domain/fileset.go`, `evallink.go`, `comparesets.go` | `FileSet.Source`, the stash arm, `narrowTo` carries the override |
| `internal/tui/clipboard_cmd.go`, `model.go` | `clipWrite` seam; record at the chokepoint |
| `internal/tui/link.go`, `action_menu.go` | `refLinkFor`, `pairLinkFor`, `hintedLinkFor`; branch/remote/tag arms |
| `internal/tui/stash_link.go` (new) | the asynchronous stash copy row |
| `internal/tui/bookmark_popup.go`, `shelf_popup.go` | `L` |
| `internal/tui/linkhist_picker.go` (new), `goto_commit_popup.go` | the picker + its first host |
| `internal/cli/compare.go`, `e2e/scenarios/s95_saved_compare.toml` | `--remove`, `--rename` |
| `internal/i18n/lang/{ja,ko,zh,ru}.toml`, `internal/tui/help.go` | strings + help |
| `CHANGELOG.md`, `README.md`, `internal/agentskill/using-gg.md` (+`Version`) | docs |

---

### Task 1: `domain.DescribeLink` — one describer for every frontend

**Files:** create `internal/domain/linkdesc.go`, `internal/domain/linkdesc_test.go`;
move `internal/cli/linkdescjs_test.go` → `internal/domain/linkdescjs_test.go`;
modify `internal/cli/link.go`, `internal/cli/compare.go`,
`internal/domain/linkhiststore.go`, `internal/web/static/links.js` (one comment).

**Interfaces — produces:**
```go
const DescMax = 60 // was cli.descMax; copy the value from internal/cli/link.go, do not retype it from memory
func LinkDesc(kind, id, subject string) string
func (s *Service) DescribeLink(ctx context.Context, l model.Link) string
func (s *Service) RecordCopiedLink(ctx context.Context, text string)
func (s *Service) UseLinkHistDir(dir string) // per-service, parallel-safe; re-arms an already-resolved store
```

- [ ] **Step 1 — read before moving.** `sed -n 285,400p internal/cli/link.go`;
  note `descMax`'s value, `truncateDesc`, `linkDesc`, `linkRecordFields`, and
  every caller: `grep -rn 'linkDesc\|linkRecordFields\|truncateDesc\|descMax' internal`.
  Known today: `link.go`, `compare.go`, `linkdescjs_test.go`, and
  `link_test.go:745` (`TestLinkDescTruncatesLongFreeText` and its
  neighbours) — those unit tests **move with the function** into
  `internal/domain/linkdesc_test.go`; they are not deleted.

- [ ] **Step 2 — failing tests** (`internal/domain/linkdesc_test.go`, parallel,
  `newRealRepo(t)` + `svc.UseLinkHistDir(t.TempDir())`):

```go
func TestDescribeLinkTable(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	sha := headHash(t, dir)
	cases := []struct{ name, link, want string }{
		{"branch", "gg://r@ref:feat/x", "branch: feat/x"},
		{"preview", "gg://r@main...feat/x", "preview: main...feat/x"},
		{"file", "gg://r/a/b.go", "file: a/b.go"},
		{"commit", "gg://r@" + sha, "commit: " + sha[:7] + " "}, // + the real subject; assert HasPrefix
		{"pair falls back", "gg://r@" + sha + ".." + sha, "link: gg://r@"},
		{"hint wins over target", "gg://r@" + sha + "?bookmark=nosuch", "bookmark: nosuch"},
	}
	for _, c := range cases {
		l, err := model.ParseLink(c.link)
		if err != nil { t.Fatalf("%s: %v", c.name, err) }
		if got := svc.DescribeLink(context.Background(), l); !strings.HasPrefix(got, c.want) {
			t.Errorf("%s: got %q, want prefix %q", c.name, got, c.want)
		}
	}
}

func TestRecordCopiedLinkRecordsLinksOnly(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	svc.UseLinkHistDir(t.TempDir())
	ctx := context.Background()
	svc.RecordCopiedLink(ctx, "not a link")
	svc.RecordCopiedLink(ctx, "gg://r@ref:main")
	h := svc.LinkHistory(ctx)
	if len(h) != 1 || h[0].Link != "gg://r@ref:main" || h[0].Desc != "branch: main" {
		t.Fatalf("history = %+v", h)
	}
}

func TestUseLinkHistDirReArmsAResolvedStore(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	ctx := context.Background()
	a, b := t.TempDir(), t.TempDir()
	svc.UseLinkHistDir(a)
	svc.RecordCopiedLink(ctx, "gg://r@ref:one") // resolves the store on a
	svc.UseLinkHistDir(b)
	if h := svc.LinkHistory(ctx); len(h) != 0 {
		t.Fatalf("store still on the old dir: %+v", h) // the 3a UseSavedCompareDir defect, pre-empted
	}
}
```

- [ ] **Step 3 — run, expect compile failure** (`DescribeLink` undefined):
  `go test ./internal/domain -run 'DescribeLink|RecordCopiedLink|UseLinkHistDir'`.

- [ ] **Step 4 — implement.** Move the three functions verbatim into
  `linkdesc.go` (`linkRecordFields` becomes an unexported method
  `(s *Service) linkDescFields(ctx, l)`; it already calls only `svc.*`).
  `RecordCopiedLink`: `ParseLink` error → return; else
  `s.RecordLink(ctx, text, s.DescribeLink(ctx, l))`. `UseLinkHistDir` sets a
  new `s.linkHistRoot` **and** `s.linkhist = nil` under `s.mu`;
  `linkHistStore` prefers `s.linkHistRoot`, then the package var, then XDG.

- [ ] **Step 5 — repoint the CLI.** `link.go:125` and `compare.go:312` become
  `svc.RecordLink(ctx, text, svc.DescribeLink(ctx, l))`; delete the moved
  functions and any now-dead imports. `git mv` the JS gate into
  `internal/domain`, change `package cli` → `package domain`, fix the three
  doc-comment mentions of `internal/cli`, and call `LinkDesc`. Update the
  `links.js:138` comment ("The Go twin is internal/domain.DescMax").

- [ ] **Step 6 — green:** `go test ./internal/domain ./internal/cli ./internal/web ./internal/archtest`.

- [ ] **Step 7 — break table.**

| break | must turn red |
|---|---|
| `RecordCopiedLink`: drop the ParseLink error return (record anyway) | `…RecordsLinksOnly` (len 2) |
| `UseLinkHistDir`: do not nil `s.linkhist` | `…ReArmsAResolvedStore` |
| `linkDescFields`: move the hint switch below the target switch | table row "hint wins over target" |
| in `links.js` change `descMax` to 59 | `TestLinkDescJSMatchesGo` (proves the moved gate still reads the JS) |

- [ ] **Step 8 — commit:** `refactor(domain): DescribeLink — one link describer for every frontend`.

---

### Task 2: §3.4 — a `-u` stash's untracked files join the change-set

**Files:** create `internal/git/parents.go` (+`parents_test.go`),
`internal/domain/stashshape.go` (+`stashshape_test.go`); modify
`internal/domain/fileset.go`, `evallink.go`, `comparesets.go`, `linkdesc.go`.

**Interfaces — produces:**
```go
// git
func (r *Repo) CommitParents(ctx context.Context, rev string) ([]string, error) // full shas, in order; rev-list --parents -n 1
// domain
type stashShapeInfo struct{ Parent, Untracked string } // Untracked == "" ⇒ a plain two-parent stash
func (s *Service) stashShape(ctx context.Context, a, b string) (stashShapeInfo, bool, error)
func (s *Service) StashPair(ctx context.Context, ref string) (parent, sha string, err error) // both FULL shas
func (f FileSet) Source(path string) model.Endpoint
```

**Two rules (spec §3.3), kept in two functions so neither can borrow the
other's looseness:**
- `stashShape` — the **set** rule, structural only: exactly three parents,
  `a` is the first, the third is a **root**. `ok == true` ⇒ `Untracked != ""`.
- `looksLikeAStash(ctx, a, b)` — the **description** rule, best-effort: `a`
  is `b`'s first parent, two or three parents, subject starts `WIP on ` or
  `On `. Used by `linkDescFields` only.

- [ ] **Step 1 — fixture helper** in `stashshape_test.go`:

```go
// stashFixture: one tracked edit + one untracked file, stashed with -u.
// Returns the repo dir, the stash sha and its first parent.
func stashFixture(t *testing.T) (dir string, svc *Service, parent, sha string) {
	t.Helper()
	dir, svc = newRealRepo(t)
	writeIn(t, dir, "tracked.txt", "v1\n"); gitIn(t, dir, "add", "."); gitIn(t, dir, "commit", "-qm", "seed")
	writeIn(t, dir, "tracked.txt", "v2\n")
	writeIn(t, dir, "scratch/notes.txt", "untracked\n")
	gitIn(t, dir, "stash", "push", "-u", "-m", "my wip")
	p, s, err := svc.StashPair(context.Background(), "stash@{0}")
	if err != nil { t.Fatal(err) }
	return dir, svc, p, s
}
```

- [ ] **Step 2 — failing tests.**

```go
func TestStashPairSetIncludesUntrackedFromTheThirdParent(t *testing.T) {
	t.Parallel()
	_, svc, parent, sha := stashFixture(t)
	ctx := context.Background()

	// THE TWO ARMS MUST DIFFER, asserted up front: the plain first-parent diff
	// does not contain the untracked file. If it ever does, this test proves nothing.
	plain, err := svc.CompareFiles(ctx, mustTestCommit(t, parent), mustTestCommit(t, sha))
	if err != nil { t.Fatal(err) }
	if _, ok := statuses(plain)["scratch/notes.txt"]; ok {
		t.Fatal("fixture broken: a..b already lists the untracked file")
	}

	fs, err := svc.EvalEndpoint(ctx, mustTestPair(t, parent, sha))
	if err != nil { t.Fatal(err) }
	if got := fs.Paths(); !slices.Equal(got, []string{"scratch/notes.txt", "tracked.txt"}) {
		t.Fatalf("paths = %v", got)
	}
	if !fs.Has("scratch/notes.txt") { t.Error("untracked member must have bytes") }
	if fs.Source("tracked.txt") != fs.Endpoint() { t.Error("tracked member reads from b") }
	if fs.Source("scratch/notes.txt") == fs.Endpoint() { t.Error("untracked member must read from the third parent") }
	b, err := svc.ResolveBytes(ctx, fs.Source("scratch/notes.txt").FileRef("scratch/notes.txt"))
	if err != nil || string(b) != "untracked\n" { t.Fatalf("bytes = %q, %v", b, err) }
}

func TestStashSetAgainstItsParentReportsTheUntrackedFileAsAdded(t *testing.T) {
	t.Parallel()
	_, svc, parent, sha := stashFixture(t)
	ctx := context.Background()
	left, _ := svc.EvalEndpoint(ctx, mustTestCommit(t, parent))
	right, _ := svc.EvalEndpoint(ctx, mustTestPair(t, parent, sha))
	files, err := svc.CompareSets(ctx, left, right)
	if err != nil { t.Fatal(err) } // without Source(): `git show b:scratch/notes.txt` hard-errors here
	st := statuses(files)
	if st["scratch/notes.txt"] != "A" || st["tracked.txt"] != "M" { t.Fatalf("statuses = %v", st) }
}

func TestNarrowedStashLinkKeepsTheThirdParentSource(t *testing.T) {
	t.Parallel()
	_, svc, parent, sha := stashFixture(t)
	l, err := model.ParseLink("gg://r/scratch/notes.txt@" + parent + ".." + sha)
	if err != nil { t.Fatal(err) }
	fs, err := svc.EvalLink(context.Background(), l)
	if err != nil { t.Fatal(err) }
	if !fs.Has("scratch/notes.txt") || fs.Source("scratch/notes.txt") == fs.Endpoint() {
		t.Fatal("narrowTo dropped the per-path source") // narrowTo rebuilds the set — the two-arms trap
	}
}

func TestOctopusMergeIsNotAStash(t *testing.T) {
	t.Parallel()
	// three branches off one seed, each adding a file; `git merge b2 b3` from b1 → a 3-parent merge
	// whose third parent is NOT a root. Its set must be exactly a..b.
	… build it with gitIn; a = first parent, m = merge sha …
	fs, err := svc.EvalEndpoint(ctx, mustTestPair(t, a, m))
	plain, _ := svc.CompareFiles(ctx, mustTestCommit(t, a), mustTestCommit(t, m))
	if len(fs.Paths()) != len(plain) { t.Fatalf("octopus widened: %v vs %d plain rows", fs.Paths(), len(plain)) }
}

// BOTH stash arms are described: the -u one (three parents) and a plain one
// (two parents, which an ordinary merge also has — hence the subject test).
func TestDescribeLinkNamesAPlainStashButNotAMerge(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	… seed; edit tracked.txt; `git stash push -m plain` (NO -u) → StashPair → desc has prefix "stash: " …
	… then a real two-parent merge m with first parent a → DescribeLink(@a..m) has prefix "link: " …
	_ = dir
}

func TestDescribeLinkNamesAStash(t *testing.T) {
	t.Parallel()
	_, svc, parent, sha := stashFixture(t)
	l, _ := model.ParseLink("gg://r@" + parent + ".." + sha)
	if got := svc.DescribeLink(context.Background(), l); got != "stash: On master: my wip" && got != "stash: On main: my wip" {
		t.Fatalf("desc = %q", got)
	}
}
```
  (The octopus test's elided lines are fixture plumbing only: three
  `git checkout -qb`, three commits, one `git merge -q --no-edit b2 b3`; write
  them out in full when implementing, and assert `len(parents)==3` on the
  merge first so the fixture cannot silently degrade to a fast-forward.)

- [ ] **Step 3 — run, expect red / compile failure.**

- [ ] **Step 4 — implement.**
  - `git.CommitParents`: `gitcmd.New("rev-list").Arg("--parents", "-n", "1", rev)`;
    split the one line on spaces, drop field 0. FakeRunner argv test in
    `parents_test.go` plus one real-repo test (root commit → empty slice).
  - `stashShape` under `query(…"stash-shape:"+a+":"+b…)`: parents of `b`;
    `len < 2 || parents[0] != a` → `false`; `len == 3` → parents of
    `parents[2]` must be empty, else `false`; `len > 3` → `false`.
  - `StashPair(ref)`: `StashCommit(ref)` then `CommitParents(sha)[0]`; an
    empty parent list is an error ("not a stash").
  - `FileSet` gains `src map[string]model.Endpoint` and
    `Source(path)` (`f.src[path]` if present, else `f.ep`).
  - `EvalEndpoint`'s pair arm: after the existing loop, `stashShape`; when
    `Untracked != ""`, `TreeFiles(Untracked)` → for each file not already a
    member: append path, `has[p] = true`, `src[p] = CommitEndpoint(Untracked)`.
    Build with a new `boundedSetFull(ep, paths, has, src)`; keep
    `boundedSetWith` as the `src == nil` wrapper.
  - `narrowTo`: carry `fs.src[path]` into the narrowed set (when present).
  - `comparesets.go`: `compareOne`/`sameBytes` take the two **sets** and read
    `left.Source(path)`, `right.Source(path)`. `grep -n 'Endpoint()' internal/domain/comparesets.go`
    — every byte read moves; the endpoint-equality and live-pair checks stay.
  - `linkDescFields`: before the final fallback, `l.Target.Pair != nil` →
    resolve both halves, `stashShape` → when ok and `CommitLookup(b)`'s
    subject starts `WIP on ` or `On ` → `("stash", "", subject)`. Any error →
    fall through (best-effort).

- [ ] **Step 5 — green:** `go test ./internal/git ./internal/domain`.

- [ ] **Step 6 — break table.**

| break | must turn red |
|---|---|
| skip the third-parent merge in the pair arm | `…IncludesUntracked…` |
| `Source` always returns `f.ep` | `…IncludesUntracked…` (bytes) **and** `…ReportsTheUntrackedFileAsAdded` (hard error) |
| `narrowTo` does not carry `src` | `TestNarrowedStashLink…` |
| drop the "third parent is a root" test | `TestOctopusMergeIsNotAStash` |
| `looksLikeAStash` drops the subject test | `…PlainStashButNotAMerge` (the merge row) |
| `linkDescFields` asks `stashShape` instead of `looksLikeAStash` | `…PlainStashButNotAMerge` (the plain-stash row) |
| `compareOne` reads `right.Endpoint()` again | `…ReportsTheUntrackedFileAsAdded` |

- [ ] **Step 7 — the MCP/CLI answer moved too.** Add one row to an existing
  CLI compare test: `gg compare <parent> gg://…@<parent>..<sha>` lists
  `A\tscratch/notes.txt`. (`newCLIRepo`, `runCLI`, serial.)

- [ ] **Step 8 — commit:** `feat(domain): a -u stash's untracked files join its change-set (spec §3.4)`.

---

### Task 3: the TUI records every copied link — at the clipboard chokepoint

**Files:** modify `internal/tui/clipboard_cmd.go`, `internal/tui/model.go`
(`New`); create `internal/tui/link_record_test.go`.

**Interfaces — produces:** `Model.clipWrite func(tty io.Writer, text string) (string, error)`
(defaults to `clipboard.Copy` in `New`); `copyToClipboardCmd` records.

**Ruling (amends spec §3.1's wording):** record **before** the clipboard
write and **regardless of its outcome**. When the clipboard is broken (WSL
interop down) the history is the only place the link survives — that is when
it matters most.

- [ ] **Step 1 — failing tests.**

```go
func recordingModel(t *testing.T) (Model, *[]string) {
	t.Helper()
	m := newTestModel(t)
	m.svc.UseLinkHistDir(t.TempDir())
	var wrote []string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { wrote = append(wrote, s); return "fake", nil }
	return m, &wrote
}

func TestCopyingALinkRecordsIt(t *testing.T) {
	t.Parallel()
	m, wrote := recordingModel(t)
	m.copyToClipboardCmd("ok", "gg://r@ref:main")()
	h := m.svc.LinkHistory(context.Background())
	if len(*wrote) != 1 || len(h) != 1 || h[0].Link != "gg://r@ref:main" || h[0].Desc != "branch: main" {
		t.Fatalf("wrote=%v history=%+v", *wrote, h)
	}
}

func TestCopyingPlainTextRecordsNothing(t *testing.T) {
	t.Parallel()
	m, _ := recordingModel(t)
	m.copyToClipboardCmd("ok", "internal/tui/link.go")()
	if h := m.svc.LinkHistory(context.Background()); len(h) != 0 { t.Fatalf("history = %+v", h) }
}

func TestALinkIsRecordedEvenWhenTheClipboardFails(t *testing.T) {
	t.Parallel()
	m, _ := recordingModel(t)
	m.clipWrite = func(io.Writer, string) (string, error) { return "", errors.New("no clipboard") }
	msg := m.copyToClipboardCmd("ok", "gg://r@ref:main")()
	if c, ok := msg.(clipboardCopiedMsg); !ok || c.err == nil { t.Fatalf("msg = %#v", msg) }
	if h := m.svc.LinkHistory(context.Background()); len(h) != 1 { t.Fatalf("history = %+v", h) }
}

// The chokepoint is a FACT only while it is the sole clipboard writer.
func TestTheTUIHasOneClipboardWriter(t *testing.T) {
	t.Parallel()
	n := 0
	for _, f := range nonTestGoFiles(t, ".") { // existing helper? else filepath.Glob("*.go") minus _test.go
		n += strings.Count(readFile(t, f), "clipboard.Copy")
	}
	if n != 1 { t.Fatalf("clipboard.Copy appears %d times in internal/tui; it must be reachable only through copyToClipboardCmd", n) }
}
```
  The gate counts a **reference**, not a line: `New` assigns
  `clipWrite: clipboard.Copy` (one occurrence) and `copyToClipboardCmd` calls
  `m.clipWrite`. If a doc comment mentions `clipboard.Copy`, strip comments
  with `go/scanner` before counting (the token-stream trick in
  `memory/comment-audit-token-stream-verifier.md`) rather than rewording the
  comment to please the gate.

- [ ] **Step 2 — red**, then **implement**:

```go
func (m Model) copyToClipboardCmd(ok, text string) tea.Cmd {
	svc, write := m.svc, m.clipWrite
	return func() tea.Msg {
		if svc != nil && isLinkText(text) {
			svc.RecordCopiedLink(context.Background(), text)
		}
		var tty io.Writer
		if isatty.IsTerminal(os.Stderr.Fd()) { tty = os.Stderr }
		if _, err := write(tty, text); err != nil { return clipboardCopiedMsg{err: err} }
		return clipboardCopiedMsg{ok: ok}
	}
}
```
  `grep -rn 'Model{' internal/tui/*_test.go | head` — any test that builds a
  `Model` literal and runs a copy cmd needs `clipWrite`; give `copyToClipboardCmd`
  a nil-guard (`if write == nil { write = clipboard.Copy }` is **forbidden** —
  it would be a second reference; instead nil → return
  `clipboardCopiedMsg{err: errNoClipboard}`).

- [ ] **Step 3 — green:** `go test ./internal/tui -run 'Cop|Clipboard|Link'`, then the whole package.

- [ ] **Step 4 — break table.**

| break | must turn red |
|---|---|
| delete the `isLinkText` branch | `TestCopyingALinkRecordsIt` |
| record unconditionally (drop `isLinkText`) | — none: `RecordCopiedLink` refuses non-links itself. **So also** break `RecordCopiedLink`'s own guard *and* this one together → `TestCopyingPlainTextRecordsNothing`. Note in the test why both layers exist. |
| move the record after the failed-write `return` | `TestALinkIsRecordedEvenWhenTheClipboardFails` |
| add a second `clipboard.Copy(` call anywhere in `internal/tui` | `TestTheTUIHasOneClipboardWriter` |

- [ ] **Step 5 — commit:** `feat(tui): every copied gg:// link is recorded, at the one clipboard writer`.

---

### Task 4: Copy link on branch, remote-branch and tag rows

**Files:** modify `internal/tui/link.go`, `internal/tui/action_menu.go`;
tests in `internal/tui/link_ref_test.go`; four bundles untouched (the row
reuses "Copy link" / "Copied link: %s").

**Interfaces — produces:**
```go
func (m Model) refLinkFor(name string) (string, bool) // gg://<repo>@ref:<name>
```

- [ ] **Step 1 — failing tests.** One model with a branch `feat/x`, a remote
  branch `origin/main`, a tag `v1`, and `linkRepoName = "r"`:

```go
func TestRefRowsCopyTheNameNotTheSha(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ focus panel; want string }{
		{panelBranches, "gg://r@ref:feat/x"},
		{panelRemotes, "gg://r@ref:origin/main"},
		{panelTags, "gg://r@ref:v1"},
	} {
		m := refRowsModel(t, c.focus)
		got, ok := m.contextLinkText()
		if !ok || got != c.want { t.Errorf("%v: %q, %v", c.focus, got, ok) }
		if _, err := model.ParseLink(got); err != nil { t.Errorf("does not reparse: %v", err) }
		r, ok := rowByID(m.actionRows(), "copy-link") // the menu offers the SAME text
		if !ok || r.copyText != c.want { t.Errorf("%v menu row: %+v", c.focus, r) }
	}
}

// Emission is half the contract. An ANNOTATED tag's name resolves to the tag
// OBJECT unless it is peeled (TestGotoCommitAnnotatedTagLoadsFiles exists
// because `#` once got that wrong), so the copied link is EVALUATED too — for
// a lightweight and an annotated tag on the same commit, which must agree.
func TestACopiedTagLinkEvaluatesToItsCommit(t *testing.T) {
	t.Parallel()
	… real repo: `git tag light`, `git tag -a ann -m x` on HEAD …
	for _, name := range []string{"light", "ann"} {
		text, _ := m.refLinkFor(name)
		l, _ := model.ParseLink(text)
		fs, err := m.svc.EvalLink(context.Background(), l)
		if err != nil || fs.Bounded() || fs.Endpoint().Hash() != head { t.Errorf("%s: %+v, %v", name, fs.Endpoint(), err) }
	}
}

func TestRefLinkRefusesANameTheGrammarCannotHold(t *testing.T) {
	t.Parallel()
	m := newTestModel(t); m.linkRepoName = "r"
	for _, bad := range []string{"a@b", "a:b", "a#b", "a?b", "a..b", ""} {
		if s, ok := m.refLinkFor(bad); ok { t.Errorf("%q → %q", bad, s) }
	}
}

// A content window over the Branches panel must not answer with the branch.
func TestRefArmIsGatedOnTheContentWindow(t *testing.T) { … open a files view over panelBranches → contextLinkText must not return an @ref: link … }
```
  (`m.actionRows()` is a placeholder name — `grep -n 'func (m Model).*\[\]actionRow' internal/tui/action_menu.go`
  and use the real top-level builder the existing copy-row tests use:
  `action_menu_copyrows_test.go:142`.)

- [ ] **Step 2 — implement.** `refLinkFor`: `LinkRefOK` + `linkRepoFor("")`
  + `model.Link{Repo, Target: model.LinkTarget{State: model.StateCommitted, Ref: name}, Side: model.NoteSideNew}.String()`.
  In `contextLinkText`, after the Previews arm and before Commits, three
  arms gated `!m.inContentWindow()`. `insertCopyLinkRow` already anchors on
  `copy-commit-id` for these panels — verify with the menu-row assertion
  above rather than assuming.

- [ ] **Step 3 — break table.**

| break | must turn red |
|---|---|
| remotes arm reads `m.branches[bi]` | row 2 of `TestRefRowsCopy…` (distinct names per panel is what makes it visible) |
| emit the tip sha (`@<hash>`) instead of `@ref:` | all three rows |
| drop `LinkRefOK` | `TestRefLinkRefuses…` |
| drop the `inContentWindow` gate | `TestRefArmIsGated…` |

- [ ] **Step 4 — headless check:** `./tui-capture.sh` on a scratch repo:
  focus Branches, `.`, confirm a "Copy link" row. Save the snapshot path in
  the commit body.

- [ ] **Step 5 — commit:** `feat(tui): Copy link on branch, remote-branch and tag rows`.

---

### Task 5: Copy link on a stash row — the pair, resolved

**Files:** create `internal/tui/stash_link.go`, `internal/tui/stash_link_test.go`;
modify `internal/tui/link.go` (`pairLinkFor`), `action_menu.go` (stash arm),
`model.go` (route `stashLinkMsg`), `stash_menu_tab_test.go` (the pinned order
gains `copy-link` after `copy-stash-ref` — a contract change, say so in the commit).

**Interfaces — consumes** `svc.StashPair` (Task 2). **Produces:**
```go
func (m Model) pairLinkFor(a, b string) (string, bool) // both ≥ 40 hex; gg://<repo>@<a>..<b>
type stashLinkMsg struct{ ref, parent, sha string; err error }
func (m Model) stashLinkCmd(ref string) tea.Cmd
```

This is the **one asynchronous copy row**: the menu is built synchronously
and a stash sha is a git call. The row's `run` returns `stashLinkCmd(ref)`;
the message handler builds the link and returns `copyToClipboardCmd`.
`copyText` stays empty (the text is not known at build time) — check what
reads `copyText` (`grep -n 'copyText' internal/tui/*.go`) and make sure an
empty one is inert there.

- [ ] **Step 1 — failing tests.**

```go
func TestStashLinkIsTheResolvedPairAndSurvivesARenumbering(t *testing.T) {
	t.Parallel()
	m, dir := stashModel(t) // real repo, two stashes: "first" then "second" (so "first" is stash@{1})
	msg := m.stashLinkCmd("stash@{1}")().(stashLinkMsg)
	if msg.err != nil { t.Fatal(msg.err) }
	text, ok := m.pairLinkFor(msg.parent, msg.sha)
	if !ok { t.Fatal("refused") }
	if strings.Contains(text, "stash@") || strings.ContainsAny(text, "{}") { t.Fatalf("positional ref leaked: %s", text) }

	firstSha := gitOutT(t, dir, "rev-parse", "stash@{1}")
	// RENUMBER: a third stash pushes "first" to stash@{2}. The copied link must still name it.
	writeFileT(t, dir, "c.txt", "c\n"); gitRunT(t, dir, "stash", "push", "-u", "-m", "third")
	if gitOutT(t, dir, "rev-parse", "stash@{1}") == firstSha { t.Fatal("fixture broken: the renumbering did not move stash@{1}") }
	l, err := model.ParseLink(text); if err != nil { t.Fatal(err) }
	if l.Target.Pair == nil || l.Target.Pair.B != firstSha { t.Fatalf("pair = %+v, want b=%s", l.Target.Pair, firstSha) }
}

func TestStashLinkMsgCopiesAndTheCopyRecordsAsAStash(t *testing.T) {
	t.Parallel()
	m, _ := stashModel(t); m.svc.UseLinkHistDir(t.TempDir())
	var wrote string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { wrote = s; return "fake", nil }
	msg := m.stashLinkCmd("stash@{0}")()
	nm, cmd := m.Update(msg); _ = nm
	runAll(cmd) // execute the returned cmd (and a tea.Batch's members)
	h := m.svc.LinkHistory(context.Background())
	if wrote == "" || len(h) != 1 || h[0].Link != wrote || !strings.HasPrefix(h[0].Desc, "stash: ") {
		t.Fatalf("wrote=%q history=%+v", wrote, h)
	}
}

func TestADroppedStashCopiesNothing(t *testing.T) {
	t.Parallel()
	m, _ := stashModel(t)
	var wrote int
	m.clipWrite = func(io.Writer, string) (string, error) { wrote++; return "", nil }
	nm, cmd := m.Update(stashLinkMsg{ref: "stash@{9}", err: errors.New("gone")})
	runAll(cmd)
	if wrote != 0 || nm.(Model).statusMsg == "" { t.Fatalf("wrote=%d status=%q", wrote, nm.(Model).statusMsg) }
}
```

- [ ] **Step 2 — implement**; new i18n key **"stash is gone: %s"** in all four
  bundles (use the `adding-translations` skill's procedure).

- [ ] **Step 3 — break table.**

| break | must turn red |
|---|---|
| build the link from `ref` (`@stash@{1}^..stash@{1}` cannot even parse) — instead break by resolving `stash@{0}` regardless of the row | `…SurvivesARenumbering` (b ≠ firstSha) |
| `pairLinkFor` accepts a 7-char sha | add `TestPairLinkRefusesAShortSha`; it turns red |
| handler copies on `err != nil` | `TestADroppedStashCopiesNothing` |
| swap `a`/`b` in `pairLinkFor` | `…SurvivesARenumbering` (asserts `Pair.B`) |

- [ ] **Step 4 — headless check** with a real `-u` stash: open the stash
  window, `.`, Copy link; then `gg links` shows `stash: …` and
  `gg compare <that link>` lists the untracked file as `A`. This is the
  built-binary proof that Tasks 2, 3 and 5 meet.

- [ ] **Step 5 — commit:** `feat(tui): Copy link on a stash row — the resolved pair, never stash@{N}`.

---

### Task 6: `L` copies a bookmark's or a shelf entry's link

**Files:** modify `internal/tui/link.go` (`hintedLinkFor`),
`bookmark_popup.go`, `shelf_popup.go`, their `?` cheat-sheet builders;
tests `internal/tui/switcher_link_test.go`; bundles for the two help lines.

**Interfaces — produces:**
```go
func (m Model) hintedLinkFor(addr model.FileAddress, hint model.LinkHint) (string, bool)
```
`linkFor` and `hintedLinkFor` share one unexported builder; `hintedLinkFor`
additionally refuses `!model.LinkHintKindOK(kind) || !model.LinkHintIDOK(id)`.
A shelf entry links its **`Origin`** address + `?shelf=<id>`; `linkFor`'s
`StateShelf` refusal stays (a bookmark *of* a shelf entry has no form).

- [ ] **Step 1 — failing tests**, one table over both switchers:

| entry | want |
|---|---|
| bookmark, file @ commit | `gg://r/a.go@<sha>?bookmark=<id>` |
| bookmark, commit pointer | `gg://r@<sha>?bookmark=<id>` |
| shelf, file from the working tree | `gg://r/a.go?shelf=<id>` |
| shelf, file from the index | `gg://r/a.go@staged?shelf=<id>` |
| shelf, commit | `gg://r@<sha>?shelf=<id>` |

  Each: press `L`, run the cmd with a fake `clipWrite`, assert the written
  text, that it reparses, and that `svc.EndpointForLink` of the working-tree
  shelf row is a **shelf** endpoint (the §3.3 exception actually reached —
  the assertion that separates rows 3 and 4 from a plain file link).
  Plus: `L` in compare-pick mode is inert (no cmd, no status change).

- [ ] **Step 2 — implement**; the no-form case sets the switcher's status
  line to the existing **"▸ no gg link for this place"** key.

- [ ] **Step 3 — break table.**

| break | must turn red |
|---|---|
| drop the hint | every row |
| shelf rows use `bookmark` as the kind | rows 3–5 |
| working-tree shelf row emits `@staged` | row 3 vs row 4 (they must differ — assert it up front) |
| remove the compare-mode guard | the inert test |

- [ ] **Step 4 — commit:** `feat(tui): L copies a bookmark's or shelf entry's gg:// link`.

---

### Task 7: the history picker, first hosted by the `#` prompt

**Files:** create `internal/tui/linkhist_picker.go`, `linkhist_picker_test.go`;
modify `goto_commit_popup.go`, `model.go` (route `linkHistLoadedMsg`),
`help.go`, the four bundles.

**Interfaces — produces** (3b-2's dialog reuses all of it):
```go
type histRow struct{ link, desc string }          // TUI-local: no linkhist import
type linkHistPicker struct{ rows []histRow; sel int; loaded, active bool }
type linkHistLoadedMsg struct{ gen int; rows []histRow }
func (m Model) linkHistCmd(gen int) tea.Cmd       // svc.LinkHistory → rows, off the UI thread
func (p *linkHistPicker) key(msg tea.KeyMsg) (picked string, handled bool)
func (p *linkHistPicker) view(width int) string
```
Host contract: `↓` in the input with `len(rows) > 0` sets `active`; while
active `↑`/`↓` move, `↑` on row 0 deactivates, `enter` returns `picked`,
`esc` deactivates (it does **not** close the host — never trap, never
surprise); any other key deactivates and falls through to the input.
The `#` host, on `picked`: set the input **and submit** through the same
path `enter` takes (one code path; extract it as `submit(m, text)`).
The load is fired by `openGotoCommitPopup` with a fresh `m.linkHistGen`;
the handler drops a stale gen or a popup that is no longer on top.

- [ ] **Step 1 — failing tests:** load populates rows newest-first; stale
  gen dropped; `↓ ↓ enter` submits row 2's link (assert the returned cmd is
  the **link** resolver, by checking `p.resolving` and the input text);
  `esc` while active keeps the popup open and a second `esc` closes it;
  typing while active returns to the field and the rune lands in the input;
  empty history renders the one dim line **"no copied links yet"** and `↓`
  is inert; a 25-entry store renders 20 rows (the store caps at 20 — so
  seed **through a fake with 25** or this proves nothing; if the domain
  seam cannot hold 25, assert on `view()` with a hand-built 25-row picker).
  Rendering: a long link is middle-elided to the popup width and no row
  exceeds it (`lipgloss.Width`), using a wide-glyph desc (`"☰ 日本"`) —
  the row-overflow class in `memory/tui-wide-glyph-row-overflow.md`.

- [ ] **Step 2 — implement**; footer line becomes
  **"[enter] go  [↓] copied links  [esc] cancel"**; `help.go`'s `#` entry
  gains "…; ↓ picks from the links you copied". All four bundles.

- [ ] **Step 3 — break table.**

| break | must turn red |
|---|---|
| handler ignores `gen` | stale-gen test |
| `enter` on a row fills without submitting | the submit assertion |
| `esc` while active pops the layer | the two-esc test |
| no width clamp in `view` | the overflow test |

- [ ] **Step 4 — headless check:** copy two links (Task 4's row), `#`, `↓`,
  snapshot shows both with their descs; `enter` lands.

- [ ] **Step 5 — commit:** `feat(tui): the # prompt picks from the copied-link history`.

---

### Task 8: `gg compare --remove` and `--rename`

**Files:** modify `internal/cli/compare.go` (+usage string),
`internal/cli/comparesave_test.go`, `e2e/scenarios/s95_saved_compare.toml`.

- [ ] **Step 1 — failing tests** (serial; `t.Setenv("XDG_STATE_HOME", …)`):
  save → `--remove <label>` exit 0, stdout empty, stderr `# removed: <id>\t<label>`,
  `--list` empty; `--remove nosuch` exit 2; `--rename <id> "new name"` →
  `--list` shows the new label under the **same id**; `--rename` with one
  arg → exit 2 + usage; `--remove` combined with `--save`/`--saved`/`--list`/positionals → exit 2.
  **Two-row fixture:** save two comparisons, remove the *second by label*,
  assert the first is still listed — a remove that deletes everything, or
  the first match, must not pass.
  A converted **preview** (SET-shaped) row is removable through this flag
  too (it is the same store); assert `gg preview list` no longer shows it.

- [ ] **Step 2 — implement** beside `--list`; mirror `--saved`'s not-found
  message. e2e rows appended to `s95`.

- [ ] **Step 3 — break table:** resolve by id only (label lookup removed) →
  the by-label test; remove ignores the spec and deletes row 0 → the two-row
  test; print to stdout → the stdout-empty assertion.

- [ ] **Step 4 — commit:** `feat(cli): gg compare --remove / --rename`.

---

### Task 9: docs, skill, and the merged-tree gate

- [ ] `CHANGELOG.md` entry; `README.md` (copy rows, `#` history, the two CLI
  flags); `docs/CLAUDE-details.md` (the chokepoint, `FileSet.Source`, the
  stash shape rule); `CLAUDE.md` only if a map row is now wrong (`domain`
  gains "describes and records copied links" — keep it one line).
- [ ] `internal/agentskill/using-gg.md`: `--remove`/`--rename`, and that a
  stash link includes untracked files. **Verify each sentence against the
  built binary first.** Bump `agentskill.Version` to 84; `./build.sh install`
  is post-merge, so run `go run ./cmd/gg init --update` from the worktree only if
  the user's installed copy should not move yet — otherwise leave it for after the merge.
- [ ] Update memory: `unified-links-feature.md` (3b-1 state, §3.4 no longer
  parked), `known-bugs-backlog.md` (strike the `--remove` TODO),
  `MEMORY.md` index line.
- [ ] `git -C /mnt/t/others/gigagit log --oneline -1 main` — **if main moved,
  merge main INTO `feat/links-tui` first** and re-read the merge for a guard
  it invalidated without touching (3a's defect #3).
- [ ] `./test.sh race > /tmp/claude-1000/…/scratchpad/race-3b1.log 2>&1; echo EXIT=$?`
  on the merged tree. Report the real exit code and the failure/race counts.
- [ ] **ASK before merging.** Then: checkout main in the main checkout,
  `--no-ff -m "Merge feat/links-tui: …"` + body + trailers, stage conflicted
  paths only, `./build.sh install`, verify `command gg --version`, remove the
  worktree and branch. Never push.

---

## Self-review

- **Spec coverage:** §3.1 → T1+T3 · §3.2 → T4+T5+T6 · §3.3 → T2 · §3.4 → T7 ·
  §3.10 → T8 · §5's gates → each task's break table. §3.5–3.9 are plan 3b-2.
- **No mergeable boundary splits a producer from its correctness:** the stash
  algebra (T2) lands *before* the first stash producer (T5), and T5's
  headless check is the point where T2, T3 and T5 are proven together.
- **Names are consistent:** `DescribeLink`, `RecordCopiedLink`,
  `UseLinkHistDir`, `StashPair`, `Source`, `clipWrite`, `refLinkFor`,
  `pairLinkFor`, `hintedLinkFor`, `linkHistPicker`, `histRow`.
- **Known soft spots, to settle while executing, not before:** the real name
  of the top-level action-row builder (T4); whether anything reads an empty
  `copyText` (T5); whether `nonTestGoFiles` exists (T3). Each has its grep
  written into the step.
