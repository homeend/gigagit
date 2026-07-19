# Worktree Switch Guard + Cross-Environment Repair Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Before any TUI repo/worktree switch, verify the target is reachable; refuse cleanly when it isn't, and — on the Worktrees panel — offer to `git worktree repair` a worktree recorded in the other environment's path notation (WSL `/mnt/t/…` ↔ Windows `T:\…`), then switch to the repaired path.

**Architecture:** A pure, injectable path-translation + verdict layer (`internal/tui/crossenv.go`), a one-invocation git verb (`Repo.WorktreeRepair`) behind a new engine op (`engine.RepairWorktree`, default TreeWrite), and a `Model.guardedReRoot(path, offerRepair)` wrapper routed through all five production `reRoot` call sites. The repair modal is a frontend-only `onResolve` decision; the post-repair switch chains through a `pendingRepairSwitch` field using the `pendingPushTags` capture-only-on-success pattern.

**Tech Stack:** Go 1.26, Bubble Tea, real-git tests in `t.TempDir()`, `gitexec.FakeRunner` for argv assertions, `internal/i18n` English-text-as-key bundles.

**Spec:** `docs/superpowers/specs/2026-07-19-worktree-switch-guard-design.md`

## Global Constraints

- **A git verb is one invocation** — argv via `gitcmd`, run via `r.Runner.Run`.
- **Engine English prose is protocol** — op summary/step built ONLY via the `msg.go` helpers (`Result{…}.WithSummary(fmt, args)`, plain `Progress{Step: …}`); never hand-assign `.Summary`. Format/step literals must exist in ALL FOUR bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`) in the same change (gates: `engine_prose_test.go`, `i18n_scan_test.go`).
- **Decision option VALUES are English protocol** — the new `"repair"` value needs an `optionDisplayName` case (`internal/tui/i18n_display.go`) AND entries in all four bundles (gate: `options_vocab_test.go`). Options lists put the safe option first and the cancel option LAST — the modal's esc maps to `abortOption` = `"abort"` if present, else the LAST option.
- **Every user-visible TUI string** = English literal key at the call site via `i18n.T` + translations in all four bundles, same change. Bundles are NOT key-sorted — insert near related keys, never re-sort.
- **Exact strings (verbatim, from the spec):**
  - status message key: `cannot switch: %s is not reachable from here`
  - modal prompt key: `This worktree is linked for another environment. Repair it for this one? It will stop working there until repaired back.`
  - op summary format: `repaired worktree link: %s`
  - op progress step: `repairing worktree`
  - decision ID: `worktree-cross-env-repair`, options `[]string{"repair", "cancel"}`
- `opAffectedSources`: `engine.RepairWorktree` → `[]sourceKey{srcWorktrees}`.
- **Repair is offered ONLY at the Worktrees-panel enter site** (`offerRepair: true`); all other sites pass `false` (plain guard).
- `internal/tui` production code never imports `internal/git` (archtest); test files may.
- Tests: real `git` in `t.TempDir()` for behavior, `FakeRunner` for argv. TDD — write the failing test first. Run test commands in the foreground.
- The 4 stale worktrees under the main checkout's `.claude/worktrees/` (branches-refresh, ctrlf-eager, ctrlf-keep-filter, search-from-cursor) are the user's manual repro fixture — NEVER prune or repair them from a test or by hand.

---

### Task 1: `WorktreeRepair` git verb

**Files:**
- Modify: `internal/git/worktree.go` (append after `PruneWorktrees`, ~line 112)
- Test: `internal/git/worktree_verbs_test.go`

**Interfaces:**
- Consumes: `gitcmd.New`, `r.Runner.Run` (existing).
- Produces: `func (r *Repo) WorktreeRepair(ctx context.Context, path string) error` — Task 2's op calls it through the `GitOps` interface.

- [ ] **Step 1: Write the failing argv test**

Append to `internal/git/worktree_verbs_test.go`:

```go
func TestWorktreeRepairArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git worktree repair", gitexec.Result{})
	repo := &Repo{Runner: f}
	if err := repo.WorktreeRepair(context.Background(), "/x/wt"); err != nil {
		t.Fatalf("repair: %v", err)
	}
	want := []string{"worktree", "repair", "/x/wt"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
	if f.Calls[0].Name != "git worktree repair" {
		t.Fatalf("span = %q, want %q", f.Calls[0].Name, "git worktree repair")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/git/ -run TestWorktreeRepairArgv -v`
Expected: FAIL — `repo.WorktreeRepair undefined`.

- [ ] **Step 3: Implement the verb**

Append to `internal/git/worktree.go` after `PruneWorktrees`:

```go
// WorktreeRepair rebinds a linked worktree's two absolute-path link records
// (the main repo's admin gitdir file and the worktree's .git back-link) to
// the given on-disk path (`git worktree repair <path>`), run from the
// current repo. Used when a worktree was created under another environment's
// path notation (WSL vs Windows) or moved on disk.
func (r *Repo) WorktreeRepair(ctx context.Context, path string) error {
	argv := gitcmd.New("worktree").Arg("repair", path).ToArgv()
	_, err := r.Runner.Run(ctx, "git worktree repair", argv)
	return err
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/git/ -run TestWorktreeRepairArgv -v`
Expected: PASS. Also run the whole package: `go test ./internal/git/` — PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/worktree.go internal/git/worktree_verbs_test.go
git commit -m "feat(git): WorktreeRepair verb (git worktree repair <path>)"
```

---

### Task 2: `engine.RepairWorktree` op + engine bundle keys

**Files:**
- Create: `internal/engine/repair_worktree.go`
- Modify: `internal/engine/gitops.go` (worktree block, after `PruneWorktrees(ctx context.Context) error`, ~line 78)
- Modify: `internal/i18n/lang/ja.toml`, `ko.toml`, `zh.toml`, `ru.toml` (two engine keys each)
- Test: `internal/engine/repair_worktree_test.go`

**Interfaces:**
- Consumes: Task 1's `WorktreeRepair(ctx, path) error` (via `GitOps`); `OpDeps.emit`, `Result.WithSummary` (existing).
- Produces: `engine.RepairWorktree{Path string}` op — `Run` returns `Result{Changed: true}` with summary `repaired worktree link: <path>` on success; default `TreeWrite` lock (no `LockMode()` override); no decisions. Task 4/5 dispatch it via `m.startOp`.

- [ ] **Step 1: Write the failing real-git moved-worktree test**

Create `internal/engine/repair_worktree_test.go`. The moved-worktree scenario reproduces the cross-environment breakage in a notation-independent way (a stale absolute path in the admin gitdir record), so it runs on any platform. Basenames `alpha`/`beta` are deliberately not substrings of each other (and don't collide with the `tmp-branch` name), so `strings.Contains` on the porcelain listing is unambiguous.

```go
package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairWorktreeRebindsMovedWorktree(t *testing.T) {
	dir, repo := newRepo(t)
	parent := t.TempDir()
	wt := filepath.Join(parent, "alpha")
	gitE(t, dir, "worktree", "add", wt, "-b", "tmp-branch")
	moved := filepath.Join(parent, "beta")
	if err := os.Rename(wt, moved); err != nil {
		t.Fatal(err)
	}
	list := func() string {
		out, err := exec.Command("git", "-C", dir, "worktree", "list", "--porcelain").CombinedOutput()
		if err != nil {
			t.Fatalf("worktree list: %v\n%s", err, out)
		}
		return string(out)
	}
	// Broken: the admin gitdir record still names the vanished old path —
	// the same stale-absolute-link state a cross-environment worktree is in.
	if out := list(); !strings.Contains(out, "alpha") {
		t.Fatalf("precondition: admin record should still name the old path\n%s", out)
	}

	res, err := RepairWorktree{Path: moved}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if want := "repaired worktree link: " + moved; res.Summary != want {
		t.Fatalf("summary = %q, want %q", res.Summary, want)
	}
	out := list()
	if !strings.Contains(out, "beta") || strings.Contains(out, "alpha") {
		t.Fatalf("after repair, worktree list should name the new path only\n%s", out)
	}
	// The repaired worktree actually works again.
	if o, err := exec.Command("git", "-C", moved, "rev-parse", "--is-inside-work-tree").CombinedOutput(); err != nil || strings.TrimSpace(string(o)) != "true" {
		t.Fatalf("repaired worktree unusable: %v\n%s", err, o)
	}
}

func TestRepairWorktreeErrorPassesThrough(t *testing.T) {
	_, repo := newRepo(t)
	// A path that is not a worktree: git worktree repair reports an error.
	res, err := RepairWorktree{Path: filepath.Join(t.TempDir(), "nope")}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatalf("expected an error, got %+v", res)
	}
	if res.Changed {
		t.Fatalf("failed repair must not report Changed: %+v", res)
	}
}
```

Note for the implementer: `newRepo(t)` and `gitE(t, dir, args...)` already exist in `internal/engine/ops_basic_test.go`. If `TestRepairWorktreeErrorPassesThrough` fails because your git version exits 0 with only a warning for an unrepairable path (verify with `git worktree repair /nonexistent; echo $?`), replace that test's body with a `t.Skip("git worktree repair exits 0 on unrepairable paths on this git version")` guarded by the observed exit code — the pass-through shape is already covered by the verb's error return; do not weaken the moved-worktree test.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/engine/ -run TestRepairWorktree -v`
Expected: FAIL — `undefined: RepairWorktree`.

- [ ] **Step 3: Implement the op and extend `GitOps`**

In `internal/engine/gitops.go`, add to the worktree method block directly after `PruneWorktrees(ctx context.Context) error`:

```go
	WorktreeRepair(ctx context.Context, path string) error
```

Create `internal/engine/repair_worktree.go`:

```go
package engine

import (
	"context"
)

// RepairWorktree rebinds a linked worktree's two absolute-path link records
// (the admin gitdir file and the worktree's .git back-link) to the given
// on-disk path (git worktree repair <path>). Backs the TUI's
// cross-environment repair offer for a worktree recorded under the other
// environment's path notation (WSL vs Windows). Default TreeWrite
// reservation: it rewrites .git admin metadata. No decisions — the
// repair/cancel confirm is frontend-side.
type RepairWorktree struct {
	Path string // the worktree path as reachable from THIS environment
}

var _ Operation = RepairWorktree{}

func (op RepairWorktree) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "repairing worktree"})
	if err := deps.Repo.WorktreeRepair(ctx, op.Path); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true}.WithSummary("repaired worktree link: %s", op.Path)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/engine/ -run TestRepairWorktree -v`
Expected: PASS (both tests).

- [ ] **Step 5: Add the two engine prose keys to all four bundles**

The TUI's `engine_prose_test.go` collector now sees the new format and step literals; without bundle entries `go test ./internal/tui/` fails. In each of `internal/i18n/lang/{ja,ko,zh,ru}.toml`:

Insert `"repairing worktree"` directly after the existing `"pruning worktrees"` line, and `"repaired worktree link: %s"` directly after the existing `"pruned stale worktrees"` line (find both with grep; bundles are not sorted — do not re-sort):

`ja.toml`:
```toml
"repairing worktree" = "ワークツリーを修復中"
"repaired worktree link: %s" = "ワークツリーのリンクを修復しました: %s"
```

`ko.toml`:
```toml
"repairing worktree" = "워크트리 복구 중"
"repaired worktree link: %s" = "워크트리 연결을 복구했습니다: %s"
```

`zh.toml`:
```toml
"repairing worktree" = "正在修复工作树"
"repaired worktree link: %s" = "已修复工作树链接：%s"
```

`ru.toml`:
```toml
"repairing worktree" = "восстановление рабочего дерева"
"repaired worktree link: %s" = "связь рабочего дерева восстановлена: %s"
```

- [ ] **Step 6: Run the i18n gates and both packages**

Run: `go test ./internal/engine/ && go test ./internal/tui/ -run 'I18n|EngineProse|DecisionOptionValues|CheckVerbs'`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/engine/repair_worktree.go internal/engine/repair_worktree_test.go internal/engine/gitops.go internal/i18n/lang/
git commit -m "feat(engine): RepairWorktree op over the WorktreeRepair verb"
```

---

### Task 3: pure cross-environment path logic

**Files:**
- Create: `internal/tui/crossenv.go`
- Test: `internal/tui/crossenv_test.go`

**Interfaces:**
- Consumes: nothing beyond `strings`.
- Produces (Task 4 consumes all three):
  - `type switchVerdict int` with `switchOK` / `switchRepairable` / `switchUnreachable`
  - `func translatePath(goos, path string) (string, bool)`
  - `func checkSwitchTarget(stat func(string) error, goos, path string) (switchVerdict, string)` — second return is the path to use: the input for `switchOK`, the translated path for `switchRepairable`, `""` for `switchUnreachable`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/crossenv_test.go`:

```go
package tui

import (
	"errors"
	"testing"
)

func TestTranslatePath(t *testing.T) {
	cases := []struct {
		goos, in, want string
		ok             bool
	}{
		// windows: WSL-recorded /mnt/<x>/… → <X>:\…
		{"windows", "/mnt/t/others/gigagit", `T:\others\gigagit`, true},
		{"windows", "/mnt/c/Users/x", `C:\Users\x`, true},
		{"windows", "/mnt/t", `T:\`, true},                // bare mount root → drive root, never a drive-relative "T:"
		{"windows", "/mnt/", "", false},                   // no drive letter
		{"windows", "/mnt/tt/x", "", false},               // multi-letter mount is not a drive
		{"windows", "/home/user/repo", "", false},         // plain Linux path: no counterpart
		{"windows", `T:\already\windows`, "", false},      // already native
		// linux (WSL): Windows-recorded <X>:… → /mnt/<x>/…
		{"linux", `T:\others\gigagit`, "/mnt/t/others/gigagit", true},
		{"linux", "T:/others/gigagit", "/mnt/t/others/gigagit", true}, // forward-slash Windows form
		{"linux", `t:\lower`, "/mnt/t/lower", true},
		{"linux", "T:", "/mnt/t", true},
		{"linux", "/mnt/t/others", "", false}, // already native
		{"linux", `1:\x`, "", false},          // not a drive letter
		// any other GOOS: never translatable
		{"darwin", `T:\x`, "", false},
		{"darwin", "/mnt/t/x", "", false},
	}
	for _, c := range cases {
		got, ok := translatePath(c.goos, c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("translatePath(%q, %q) = (%q, %v), want (%q, %v)", c.goos, c.in, got, ok, c.want, c.ok)
		}
	}
}

// statSet returns a stat func that succeeds only for the listed paths.
func statSet(ok ...string) func(string) error {
	set := map[string]bool{}
	for _, p := range ok {
		set[p] = true
	}
	return func(p string) error {
		if set[p] {
			return nil
		}
		return errors.New("stat: missing")
	}
}

func TestCheckSwitchTarget(t *testing.T) {
	if v, p := checkSwitchTarget(statSet("/x"), "linux", "/x"); v != switchOK || p != "/x" {
		t.Fatalf("reachable: got (%v, %q)", v, p)
	}
	// Recorded in WSL notation, reachable under the Windows translation.
	if v, p := checkSwitchTarget(statSet(`T:\x`), "windows", "/mnt/t/x"); v != switchRepairable || p != `T:\x` {
		t.Fatalf("repairable: got (%v, %q)", v, p)
	}
	// Neither the path nor its translation exists.
	if v, p := checkSwitchTarget(statSet(), "windows", "/mnt/t/x"); v != switchUnreachable || p != "" {
		t.Fatalf("unreachable+translatable: got (%v, %q)", v, p)
	}
	// Not translatable at all (deleted native dir).
	if v, p := checkSwitchTarget(statSet(), "linux", "/gone"); v != switchUnreachable || p != "" {
		t.Fatalf("unreachable: got (%v, %q)", v, p)
	}
	// On non-WSL Linux a C:\… string translates but the mount never stats.
	if v, _ := checkSwitchTarget(statSet(), "linux", `C:\repo`); v != switchUnreachable {
		t.Fatalf("non-WSL linux: got %v", v)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'TestTranslatePath|TestCheckSwitchTarget' -v`
Expected: FAIL — `undefined: translatePath` (compile error).

- [ ] **Step 3: Implement**

Create `internal/tui/crossenv.go`:

```go
package tui

import "strings"

// Cross-environment path logic behind the worktree switch guard. A repo on a
// disk shared between WSL and Windows (/mnt/t/… vs T:\…) can hold worktrees
// whose recorded absolute paths only parse in the environment that created
// them. Everything here is pure: GOOS and stat are injected by the caller,
// so every branch is testable on any platform.

// switchVerdict classifies a switch target before any reRoot teardown.
type switchVerdict int

const (
	switchOK          switchVerdict = iota // reachable as recorded — switch normally
	switchRepairable                       // reachable only under the other notation — offer git worktree repair
	switchUnreachable                      // not reachable at all — refuse
)

// translatePath converts a path between the WSL and Windows notations of the
// same disk location: on "windows", /mnt/<x>/rest → <X>:\rest; on "linux"
// (the WSL case), <X>:\rest or <X>:/rest → /mnt/<x>/rest. Any other input or
// GOOS is not translatable. Pure string work — it never stats.
func translatePath(goos, path string) (string, bool) {
	switch goos {
	case "windows":
		if len(path) >= 6 && strings.HasPrefix(path, "/mnt/") {
			d := path[5]
			if d < 'a' || d > 'z' {
				return "", false
			}
			drive := string(d - 'a' + 'A')
			rest := path[6:]
			if rest == "" {
				return drive + `:\`, true // never a drive-relative "<X>:"
			}
			if rest[0] != '/' {
				return "", false // /mnt/tt/… — a multi-letter mount, not a drive
			}
			return drive + ":" + strings.ReplaceAll(rest, "/", `\`), true
		}
	case "linux":
		if len(path) >= 2 && path[1] == ':' {
			d := path[0]
			switch {
			case d >= 'A' && d <= 'Z':
				d = d - 'A' + 'a'
			case d >= 'a' && d <= 'z':
			default:
				return "", false
			}
			rest := strings.ReplaceAll(path[2:], `\`, "/")
			if rest == "" {
				return "/mnt/" + string(d), true
			}
			if rest[0] != '/' {
				return "", false
			}
			return "/mnt/" + string(d) + rest, true
		}
	}
	return "", false
}

// checkSwitchTarget stats path; failing that, stats its cross-environment
// translation. The stat IS the WSL detection: translating and statting
// /mnt/c/… on a non-WSL Linux simply fails → switchUnreachable, no
// osrelease probe needed. Returns the verdict plus the path to use — the
// input for switchOK, the translated path for switchRepairable.
func checkSwitchTarget(stat func(string) error, goos, path string) (switchVerdict, string) {
	if stat(path) == nil {
		return switchOK, path
	}
	if tp, ok := translatePath(goos, path); ok && stat(tp) == nil {
		return switchRepairable, tp
	}
	return switchUnreachable, ""
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestTranslatePath|TestCheckSwitchTarget' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/crossenv.go internal/tui/crossenv_test.go
git commit -m "feat(tui): pure cross-environment path translation + switch verdict"
```

---

### Task 4: `guardedReRoot` + repair modal + TUI i18n keys

**Files:**
- Create: `internal/tui/switch_guard.go`
- Modify: `internal/tui/model.go` (one new field in the pending block, next to `pendingPushTags`, ~line 53)
- Modify: `internal/tui/i18n_display.go` (`optionDisplayName`: new `"repair"` case between `case "rebase":` and `case "reset":`, ~line 209)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (three TUI keys each)
- Test: `internal/tui/switch_guard_test.go`

**Interfaces:**
- Consumes: Task 3's `checkSwitchTarget`; Task 2's `engine.RepairWorktree`; existing `decisionState`/`onResolve` (`op.go`), `m.startOp`, `m.reRoot`, `abortOption` (esc → last option).
- Produces:
  - `func (m Model) guardedReRoot(path string, offerRepair bool) (tea.Model, tea.Cmd)` — Task 5 wires the five call sites to it.
  - `Model.pendingRepairSwitch string` — set by the modal's repair answer; Task 5 adds the capture/clear/dispatch in `opFinishedMsg` and the `reRoot` clear.
  - package-level test seams `guardStat` / `guardGOOS`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/switch_guard_test.go`:

```go
package tui

import (
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/i18n"
)

// setGuardSeams fabricates stat/GOOS results for guardedReRoot: only the
// listed paths "exist". Restored on cleanup. The repairable verdict cannot
// be produced with a real stat on a Linux CI box (it needs a foreign-
// notation path that exists), hence the seam.
func setGuardSeams(t *testing.T, goos string, existing ...string) {
	t.Helper()
	set := map[string]bool{}
	for _, p := range existing {
		set[p] = true
	}
	oldStat, oldGOOS := guardStat, guardGOOS
	guardStat = func(p string) error {
		if set[p] {
			return nil
		}
		return errors.New("stat: missing")
	}
	guardGOOS = goos
	t.Cleanup(func() { guardStat, guardGOOS = oldStat, oldGOOS })
}

func TestGuardedReRootReachableSwitches(t *testing.T) {
	m := newTestModel(t)
	setGuardSeams(t, "linux", "/ok/path")
	u, cmd := m.guardedReRoot("/ok/path", false)
	got := u.(Model)
	if got.switchTarget != "/ok/path" || !got.loading {
		t.Fatalf("reachable target must reRoot: switchTarget=%q loading=%v", got.switchTarget, got.loading)
	}
	if cmd == nil {
		t.Fatal("reRoot must return its reload command")
	}
}

func TestGuardedReRootUnreachableRefuses(t *testing.T) {
	m := newTestModel(t)
	setGuardSeams(t, "linux") // nothing exists
	u, cmd := m.guardedReRoot("/gone", true)
	got := u.(Model)
	if got.loading || got.switchTarget != "" {
		t.Fatal("unreachable target must not start a switch")
	}
	if got.modal != nil {
		t.Fatal("unreachable (untranslatable) target must not offer repair")
	}
	if want := i18n.T("cannot switch: %s is not reachable from here", "/gone"); got.statusMsg != want {
		t.Fatalf("statusMsg = %q, want %q", got.statusMsg, want)
	}
	if cmd != nil {
		t.Fatal("refusal returns no command")
	}
}

func TestGuardedReRootRepairableWithoutOfferRefuses(t *testing.T) {
	m := newTestModel(t)
	setGuardSeams(t, "windows", `T:\x`)
	u, _ := m.guardedReRoot("/mnt/t/x", false)
	got := u.(Model)
	if got.modal != nil || got.loading {
		t.Fatal("repairable without offerRepair must plain-refuse")
	}
	if want := i18n.T("cannot switch: %s is not reachable from here", "/mnt/t/x"); got.statusMsg != want {
		t.Fatalf("statusMsg = %q, want %q", got.statusMsg, want)
	}
}

func TestGuardedReRootRepairableOffersModal(t *testing.T) {
	m := newTestModel(t)
	setGuardSeams(t, "windows", `T:\x`)
	u, cmd := m.guardedReRoot("/mnt/t/x", true)
	got := u.(Model)
	if cmd != nil || got.loading {
		t.Fatal("the offer itself must not switch")
	}
	if got.modal == nil {
		t.Fatal("repairable + offerRepair must push the modal")
	}
	if got.modal.req.ID != "worktree-cross-env-repair" {
		t.Fatalf("modal ID = %q", got.modal.req.ID)
	}
	wantOpts := []string{"repair", "cancel"}
	if len(got.modal.req.Options) != 2 || got.modal.req.Options[0] != wantOpts[0] || got.modal.req.Options[1] != wantOpts[1] {
		t.Fatalf("options = %v, want %v (cancel LAST: esc maps to the last option)", got.modal.req.Options, wantOpts)
	}

	// cancel: stays put, nothing pending, no op.
	u2, cmd2 := got.modal.onResolve(got, "cancel")
	c := u2.(Model)
	if cmd2 != nil || c.running || c.pendingRepairSwitch != "" {
		t.Fatal("cancel must not dispatch anything")
	}

	// repair: arms the chain with the TRANSLATED path and starts the op.
	u3, cmd3 := got.modal.onResolve(got, "repair")
	r := u3.(Model)
	if r.pendingRepairSwitch != `T:\x` {
		t.Fatalf("pendingRepairSwitch = %q, want %q", r.pendingRepairSwitch, `T:\x`)
	}
	if !r.running {
		t.Fatal("repair must start the RepairWorktree op")
	}
	// Drain the (failing — T:\x isn't a real worktree here) op so nothing
	// leaks. The dispatched-op shape (RepairWorktree on the TRANSLATED path)
	// is pinned by pendingRepairSwitch above plus the engine op's own tests.
	driveOp(t, r, cmd3)
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run TestGuardedReRoot -v`
Expected: FAIL — `undefined: guardStat` / `m.guardedReRoot undefined` (compile error).

- [ ] **Step 3: Implement `switch_guard.go` + the Model field**

Create `internal/tui/switch_guard.go`:

```go
package tui

import (
	"os"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// Test seams for guardedReRoot's environment probes. Production always uses
// the real stat and GOOS; tests override both to fabricate the repairable
// verdict, which needs a foreign-notation path that "exists" (the copyTexts
// seam precedent).
var (
	guardStat = func(p string) error { _, err := os.Stat(p); return err }
	guardGOOS = runtime.GOOS
)

// guardedReRoot checks a switch target before any reRoot teardown. Reachable
// → switch as today. Reachable only under the other environment's path
// notation AND offerRepair (the Worktrees-panel enter site) → repair/cancel
// modal; repair runs engine.RepairWorktree on the translated path and, on
// success, chains the switch through pendingRepairSwitch (the
// pendingPushTags capture-only-on-success pattern, wired in opFinishedMsg).
// Anything else → refuse with a status message, session untouched — never
// the raw chdir crash.
func (m Model) guardedReRoot(path string, offerRepair bool) (tea.Model, tea.Cmd) {
	verdict, translated := checkSwitchTarget(guardStat, guardGOOS, path)
	switch verdict {
	case switchOK:
		return m.reRoot(path)
	case switchRepairable:
		if offerRepair {
			return m.offerWorktreeRepair(translated), nil
		}
	}
	m.statusMsg = i18n.T("cannot switch: %s is not reachable from here", path)
	return m, nil
}

// offerWorktreeRepair pushes the frontend-only repair/cancel modal for a
// worktree linked under the other environment's notation. "cancel" is LAST
// so abortOption maps esc to a genuine cancel (never-trap).
func (m Model) offerWorktreeRepair(translated string) Model {
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "worktree-cross-env-repair",
			Prompt:  i18n.T("This worktree is linked for another environment. Repair it for this one? It will stop working there until repaired back."),
			Options: []string{"repair", "cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt == "repair" {
				m.pendingRepairSwitch = translated
				return m.startOp(engine.RepairWorktree{Path: translated})
			}
			return m, nil
		},
	}
	return m
}
```

In `internal/tui/model.go`, add one field to the pending block, directly after the `pendingPushTags` line (~line 53):

```go
	pendingRepairSwitch   string              // translated worktree path to switch to after a successful RepairWorktree (chained in opFinishedMsg)
```

- [ ] **Step 4: Add the `optionDisplayName` case**

In `internal/tui/i18n_display.go`, between `case "rebase":` and `case "reset":` (alphabetical order of the option cases):

```go
	case "repair":
		return i18n.T("repair")
```

- [ ] **Step 5: Add the three TUI keys to all four bundles**

In each of `internal/i18n/lang/{ja,ko,zh,ru}.toml`: insert the `"repair"` option key directly after the existing `"cancel"` line, and the two sentence keys directly after the existing `"go to worktree"` line (grep for the anchors; do not re-sort):

`ja.toml`:
```toml
"repair" = "修復"
```
```toml
"cannot switch: %s is not reachable from here" = "切り替えできません: %s はこの環境から到達できません"
"This worktree is linked for another environment. Repair it for this one? It will stop working there until repaired back." = "このワークツリーは別の環境向けにリンクされています。この環境用に修復しますか？修復し直すまで、元の環境では使えなくなります。"
```

`ko.toml`:
```toml
"repair" = "복구"
```
```toml
"cannot switch: %s is not reachable from here" = "전환할 수 없습니다: %s 은(는) 이 환경에서 접근할 수 없습니다"
"This worktree is linked for another environment. Repair it for this one? It will stop working there until repaired back." = "이 워크트리는 다른 환경용으로 연결되어 있습니다. 이 환경에 맞게 복구할까요? 다시 복구하기 전까지 원래 환경에서는 사용할 수 없습니다."
```

`zh.toml`:
```toml
"repair" = "修复"
```
```toml
"cannot switch: %s is not reachable from here" = "无法切换：%s 在当前环境不可访问"
"This worktree is linked for another environment. Repair it for this one? It will stop working there until repaired back." = "此工作树链接到另一个环境。要为当前环境修复它吗？在修复回去之前，它将无法在原环境中使用。"
```

`ru.toml`:
```toml
"repair" = "восстановить"
```
```toml
"cannot switch: %s is not reachable from here" = "не удалось переключиться: %s недоступен из этого окружения"
"This worktree is linked for another environment. Repair it for this one? It will stop working there until repaired back." = "Это рабочее дерево привязано к другому окружению. Восстановить его для текущего? Оно перестанет работать там, пока не будет восстановлено обратно."
```

- [ ] **Step 6: Run the tests and the i18n gates**

Run: `go test ./internal/tui/ -run 'TestGuardedReRoot|I18n|EngineProse|DecisionOptionValues|ActionMenuLabels|CheckVerbs' -v`
Expected: PASS. Then the whole package: `go test ./internal/tui/` — PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/switch_guard.go internal/tui/switch_guard_test.go internal/tui/model.go internal/tui/i18n_display.go internal/i18n/lang/
git commit -m "feat(tui): guardedReRoot switch guard + cross-env repair modal"
```

---

### Task 5: wire the call sites + the post-repair switch chain

**Files:**
- Modify: `internal/tui/model.go` (five spots: the three `reRoot` call sites at ~1269/~1460/~2044; the `opFinishedMsg` handler ~2001–2056; the `reRoot` pending-clears block ~3026)
- Modify: `internal/tui/repo_popup.go` (~line 147)
- Modify: `internal/tui/repo_path_popup.go` (~line 101)
- Modify: `internal/tui/source.go` (`opAffectedSources`, ~line 257)
- Test: `internal/tui/switch_guard_test.go` (append), `internal/tui/source_test.go` or inline (mapping assertion)

**Interfaces:**
- Consumes: Task 4's `guardedReRoot` + `pendingRepairSwitch`; existing `opFinishedMsg` chain structure (the `pendingPushTags` pattern at model.go:2001–2056).
- Produces: the finished feature — every production `reRoot` goes through the guard; `RepairWorktree` success chains `guardedReRoot(translated, false)`.

- [ ] **Step 1: Write the failing chain tests**

Append to `internal/tui/switch_guard_test.go` (mirrors `notify_test.go`'s `pendingNoticeConfig` chain trio). Add `"github.com/homeend/gigagit/internal/engine"` to that file's imports (these tests are its first use of `engine`):

```go
func TestOpFinishedChainsRepairSwitch(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	m.pendingRepairSwitch = "/repaired/path"
	setGuardSeams(t, "linux", "/repaired/path")

	u, cmd := m.Update(opFinishedMsg{res: engine.Result{Changed: true}})
	got := u.(Model)

	if got.pendingRepairSwitch != "" {
		t.Fatalf("pendingRepairSwitch = %q after success, want cleared", got.pendingRepairSwitch)
	}
	if got.switchTarget != "/repaired/path" || !got.loading {
		t.Fatalf("must reRoot to the repaired path: switchTarget=%q loading=%v", got.switchTarget, got.loading)
	}
	if cmd == nil {
		t.Fatal("the chained reRoot must return its reload command")
	}
}

func TestOpFinishedErrorClearsRepairSwitch(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	m.pendingRepairSwitch = "/repaired/path"
	setGuardSeams(t, "linux", "/repaired/path")

	u, _ := m.Update(opFinishedMsg{err: errors.New("boom")})
	got := u.(Model)

	if got.pendingRepairSwitch != "" {
		t.Fatalf("pendingRepairSwitch = %q after error, want cleared", got.pendingRepairSwitch)
	}
	if got.switchTarget != "" || got.loading {
		t.Fatal("a failed repair must not switch")
	}
}

func TestAbortedOpDoesNotChainRepairSwitch(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	m.pendingRepairSwitch = "/repaired/path"
	setGuardSeams(t, "linux", "/repaired/path")

	// Changed:false, err:nil — an aborted/cancelled op must not chain.
	u, _ := m.Update(opFinishedMsg{res: engine.Result{Changed: false}})
	got := u.(Model)

	if got.pendingRepairSwitch != "" {
		t.Fatalf("pendingRepairSwitch = %q after abort, want cleared", got.pendingRepairSwitch)
	}
	if got.switchTarget != "" || got.loading {
		t.Fatal("an aborted op must not switch")
	}
}

func TestReRootClearsPendingRepairSwitch(t *testing.T) {
	m := newTestModel(t)
	m.pendingRepairSwitch = "/stale"
	u, _ := m.reRoot(t.TempDir())
	if got := u.(Model); got.pendingRepairSwitch != "" {
		t.Fatal("reRoot must clear pendingRepairSwitch")
	}
}

func TestOpAffectedSourcesRepairWorktree(t *testing.T) {
	got := opAffectedSources(engine.RepairWorktree{})
	if len(got) != 1 || got[0] != srcWorktrees {
		t.Fatalf("opAffectedSources(RepairWorktree) = %v, want [srcWorktrees]", got)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'TestOpFinished.*RepairSwitch|TestAbortedOpDoesNotChainRepairSwitch|TestReRootClearsPendingRepairSwitch|TestOpAffectedSourcesRepairWorktree' -v`
Expected: FAIL — chain/clear not wired (`pendingRepairSwitch` survives, no switch happens; mapping returns nil).

- [ ] **Step 3: Wire the `opFinishedMsg` chain**

In `internal/tui/model.go`'s `opFinishedMsg` handler (~2001–2056), make these three edits:

Next to the existing capture locals:

```go
		switchTo := ""
		chainSwitch := ""
		repairSwitch := ""
```

In the success branch, inside the existing `if msg.res.Changed {` block (alongside `pushTags`/`noticeCfg` — capture only on success):

```go
			if msg.res.Changed {
				pushTags = m.pendingPushTags
				noticeCfg = m.pendingNoticeConfig
				repairSwitch = m.pendingRepairSwitch
			}
```

In the unconditional-clears block (alongside `m.pendingPushTags = nil`):

```go
		m.pendingRepairSwitch = "" // unconditional; covers both error and success paths
```

And in the dispatch chain, directly after the `if switchTo != "" { return m.reRoot(switchTo) }` block:

```go
		if repairSwitch != "" {
			// The repair just made this path reachable; the guard re-verifies
			// (offerRepair=false — a repair that somehow didn't take refuses
			// instead of crashing).
			return m.guardedReRoot(repairSwitch, false)
		}
```

- [ ] **Step 4: Clear the pending on `reRoot` and add the source mapping**

In `internal/tui/model.go`'s `reRoot` (~3026), next to `m.pendingPushTags = nil`:

```go
	m.pendingRepairSwitch = "" // a repo switch must not fire a stale repair chain
```

In `internal/tui/source.go`'s `opAffectedSources` switch, after the `case engine.RemoveWorktree:` arm:

```go
	case engine.RepairWorktree:
		// Only the worktree admin metadata changed. (The success path chains
		// a full reRoot before this mapping is consulted; this covers the
		// failure path without a full-reload + remote-tags probe.)
		return []sourceKey{srcWorktrees}
```

- [ ] **Step 5: Route the five production `reRoot` call sites through the guard**

1. `internal/tui/model.go` ~1269 — the "checked out in another worktree → go to worktree" modal (plain guard; the repair offer lives on the Worktrees panel):

```go
						onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
							if opt == "go to worktree" {
								return m.guardedReRoot(wtPath, false)
							}
							return m, nil
						},
```

2. `internal/tui/model.go` ~1460 — the Worktrees-panel enter site (THE repair-offer site):

```go
			if m.focus == panelWorktrees && m.canEnterWorktree() {
				wt, _ := m.selectedWorktree()
				return m.guardedReRoot(wt.Path, true)
			}
```

3. `internal/tui/model.go` ~2044 — the post-create jump (`pendingSwitch` → `switchTo`; a just-created worktree is native notation, so the guard is a no-op there):

```go
		if switchTo != "" {
			return m.guardedReRoot(switchTo, false)
		}
```

4. `internal/tui/repo_popup.go` ~147 — the repo switcher (a foreign entry is refused; repairing another repo's linkage from a switcher row is out of scope):

```go
		tm, cmd := m.guardedReRoot(target, false)
		return tm.(Model), cmd
```

5. `internal/tui/repo_path_popup.go` ~101 — the palette "Open repo" (already validated off-thread via `TopLevel`, so the guard is a no-op; wired for the every-reRoot-is-guarded invariant):

```go
	return m.guardedReRoot(msg.top, false)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestOpFinished|TestAborted|TestReRoot|TestGuardedReRoot|TestOpAffectedSources' -v`
Expected: PASS — including the pre-existing `TestOpFinishedChainsPushTags`/`TestOpFinishedChainsNoticeConfig`/`TestReRootPointsAtNewWorktreeAndReloads` (the wiring must not disturb the existing chains; note `TestReRootPointsAtNewWorktreeAndReloads` calls `reRoot` directly and is unaffected).

- [ ] **Step 7: Run the whole TUI package + vet**

Run: `go vet ./... && go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/repo_popup.go internal/tui/repo_path_popup.go internal/tui/source.go internal/tui/switch_guard_test.go
git commit -m "feat(tui): route all reRoot sites through the switch guard; chain the post-repair switch"
```

---

### Task 6: documentation + full suite

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `CLAUDE.md` (package-map rows: `engine`, `git`, `tui`)
- Check (likely no change): `README.md`

**Interfaces:** none — prose only. `internal/agentskill/using-gg.md` stays untouched (no CLI surface change).

- [ ] **Step 1: CHANGELOG entry**

Read the top of `CHANGELOG.md` and add a new entry in the established format (newest first) describing, in this order: the switch guard (every TUI switch verifies the target first; unreachable targets are refused with a status message instead of the raw `chdir` crash), the cross-environment repair offer on the Worktrees panel (WSL ↔ Windows path notation; `git worktree repair` + switch to the repaired path), and the new `engine.RepairWorktree` op / `WorktreeRepair` verb.

- [ ] **Step 2: CLAUDE.md package-map updates**

- `engine` row: append a sentence alongside the other op listings: `` Also `RepairWorktree{Path}` — runs `git worktree repair <path>` (default TreeWrite; no decisions — the repair confirm is TUI-side), rebinding a linked worktree's two absolute-path link records to the current environment's notation; backs the TUI's cross-environment worktree repair. ``
- `git` row: append: `` `WorktreeRepair(ctx, path)` (`git worktree repair <path>`) rebinds a moved or cross-environment worktree's admin gitdir file + `.git` back-link; backs `engine.RepairWorktree`. ``
- `tui` row: append a sentence: `` **Worktree switch guard** (`crossenv.go`, `switch_guard.go`): every production `reRoot` site routes through `guardedReRoot(path, offerRepair)` — stat OK → switch; a WSL↔Windows foreign-notation path whose translation exists (`translatePath`; injected goos+stat seams `guardStat`/`guardGOOS`) gets a repair/cancel modal at the Worktrees-panel enter site only (`engine.RepairWorktree` on the translated path, then a `pendingRepairSwitch` capture-only-on-success chain to the repaired path); anything else is refused with a status message, session untouched. ``

- [ ] **Step 3: README check**

Run: `grep -in "worktree" README.md | head -20`. Only edit if it documents switching behavior that this feature changes (the raw-error behavior is not documented there today — expected outcome: no change).

- [ ] **Step 4: Full suite in the foreground**

Run: `./test.sh`
Expected: all stages green (vet+gofmt → unit → e2e).

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md CLAUDE.md
git commit -m "docs: worktree switch guard + cross-environment repair"
```
