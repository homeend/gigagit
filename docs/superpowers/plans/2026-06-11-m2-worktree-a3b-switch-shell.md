# M2 Worktree A3b — Create-and-Switch + Shell Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make worktree switching take you there: `W` creates a worktree and re-roots the TUI into it; Enter on the Worktrees panel jumps into an existing worktree; and with the optional `gg` shell wrapper, your shell `cd`s into the worktree on exit (so `mc`/`vim` open in the right place).

**Architecture:** A `reRoot(path)` Model helper points the live TUI at a new repo root and reloads. The engine reports the worktree it created via a new `Result.Path`, so re-root uses one authoritative path (no recomputation drift). The TUI records the switch target; `tui.Run` returns it; `cmd/gg` writes it to a `--cwd-file` (only on an actual switch). `gg shell-init [bash|zsh|fish]` prints a `gg()` wrapper (from a testable `internal/shellinit` package) that runs the real binary with a temp `--cwd-file` and `cd`s to its contents.

**Tech Stack:** Go 1.26, Bubble Tea + lipgloss, existing `internal/{engine,git,tui,gitexec,observ,cli}`, `cmd/gg`. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-06-11-worktree-management-design.md` §7 (create-and-switch re-root), §7.1 (shell integration — `--cwd-file` + `gg shell-init`).

**Explicitly DEFERRED to A3c (do NOT build here):** the `gg worktree add/list` CLI. (Per spec §11 it's a follow-up; building it well means extracting a shared config→template→resolve helper that both the popup and CLI call — its own slice.)

**Conventions (read before starting):**
- TDD red→green. After each task: `go test ./...`, `go vet ./...`, `gofmt -l internal cmd` clean; `-race` for the goroutine/channel paths.
- LF line endings only (`.gitattributes`; Windows-mounted drive — never reintroduce CRLF).
- Commit messages end with a `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` trailer.
- Plain `fmt.Errorf` for errors.
- Engine tests build a real repo via `newRepo(t)` (`internal/engine/ops_basic_test.go`); TUI tests use `loadedModel(t)` (`internal/tui/nav_test.go`) and `newRepo(t)` (`internal/tui`). `keyMsg(s)` returns specials by name else `KeyRunes`.
- `cmd/gg` is `package main`; it can have `cmd/gg/*_test.go` files in `package main` for unit tests.

---

## File Structure

- `internal/engine/operation.go` (modify): add `Path` to `Result`.
- `internal/engine/create_worktree.go` (modify): populate `Result.Path`.
- `internal/tui/model.go` (modify): `reRoot` helper, `switchTarget`/`pendingSwitch` fields, `W`/Enter wiring, `opFinishedMsg` switch handling.
- `internal/tui/worktree_popup.go` (modify): `startCreateFromPopup(switchAfter bool)`, `W` case, render hint.
- `internal/tui/run.go` (modify): `Run` returns the switch target.
- `internal/shellinit/shellinit.go` (new): `Script(shell)` for bash/zsh/fish.
- `cmd/gg/main.go` (modify): `extractCwdFile`, write `--cwd-file` on switch, `shell-init` subcommand.
- Tests: engine, tui, `internal/shellinit`, `cmd/gg`.

---

## Task 1: `engine.Result.Path` — the authoritative created path

So re-root uses the path the engine actually created, not a recomputation.

**Files:** Modify `internal/engine/operation.go`, `internal/engine/create_worktree.go`; extend `internal/engine/create_worktree_test.go`.

- [ ] **Step 1: Write the failing test** — append to `internal/engine/create_worktree_test.go`

```go
func TestCreateWorktreeResultCarriesAbsolutePath(t *testing.T) {
	dir, repo := newRepo(t)
	res, err := CreateWorktree{StartPoint: "main", Branch: "feature/p", Path: "../wt-p"}.
		Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	want := filepath.Clean(filepath.Join(dir, "..", "wt-p"))
	if res.Path != want {
		t.Fatalf("Result.Path = %q, want %q", res.Path, want)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/engine/ -run TestCreateWorktreeResultCarriesAbsolutePath -v`
Expected: FAIL — `res.Path` is empty (`""` != want).

- [ ] **Step 3: Implement**

In `internal/engine/operation.go`, add a field to `Result`:
```go
// Result is the outcome of an operation.
type Result struct {
	Summary string
	Changed bool
	Path    string // when an operation creates/targets a path (e.g. CreateWorktree), its absolute path
}
```

In `internal/engine/create_worktree.go`, set `Path` in the success result. Change:
```go
	res := Result{Summary: "worktree created: " + abs, Changed: true}
```
to:
```go
	res := Result{Summary: "worktree created: " + abs, Changed: true, Path: abs}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/engine/ -run TestCreateWorktreeResultCarriesAbsolutePath -v`
Expected: PASS. Then `go test ./internal/engine/` — all pass.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/engine
git add internal/engine/operation.go internal/engine/create_worktree.go internal/engine/create_worktree_test.go
git commit -m "feat(engine): report the created worktree path in Result.Path

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: `reRoot` helper + Model switch fields

Point the live Model at a new repo root and reload; record where the shell should follow.

**Files:** Modify `internal/tui/model.go`; create `internal/tui/reroot_test.go`.

- [ ] **Step 1: Write the failing test** — `internal/tui/reroot_test.go`

Reuse the existing `newRepoDir(t) (string, *git.Repo)` helper (defined in `internal/tui/op_test.go` — creates a temp repo with one commit on `main` and returns both the dir and the repo). Add a package-level `runGit` helper here (no existing one collides). Then the test:

```go
package tui

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// runGit runs a git command in dir, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestReRootPointsAtNewWorktreeAndReloads(t *testing.T) {
	dir, repo := newRepoDir(t)
	m := New(repo)
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)

	// Create a sibling worktree on a new branch.
	wt := filepath.Join(filepath.Dir(dir), "wt-reroot")
	runGit(t, dir, "worktree", "add", "-b", "feature/r", wt, "main")

	updated, cmd := m.reRoot(wt)
	m = updated.(Model)
	if m.switchTarget != wt {
		t.Fatalf("switchTarget = %q, want %q", m.switchTarget, wt)
	}
	if !m.loading {
		t.Error("reRoot should put the model into the loading state")
	}
	if cmd == nil {
		t.Fatal("reRoot should return a reload command")
	}
	// Apply the reload; the model should now be rooted in the new worktree.
	m = m.Update(cmd()).(Model)
	resolvedWant, _ := filepath.EvalSymlinks(wt)
	resolvedGot, _ := filepath.EvalSymlinks(m.currentWorktree)
	if resolvedGot != resolvedWant {
		t.Fatalf("after reRoot currentWorktree = %q, want %q", resolvedGot, resolvedWant)
	}
	if m.status.Branch != "feature/r" {
		t.Fatalf("after reRoot branch = %q, want feature/r", m.status.Branch)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestReRootPointsAtNewWorktreeAndReloads -v`
Expected: FAIL — `m.reRoot` / `m.switchTarget` undefined.

- [ ] **Step 3: Implement** — in `internal/tui/model.go`:

Add fields to the `Model` struct (after `pendingSeqBump`):
```go
	pendingSeqBump []string
	pendingSwitch  bool
	switchTarget   string
```

Add the imports `"github.com/gigagit/gg/internal/gitexec"` and `"github.com/gigagit/gg/internal/observ"` to `internal/tui/model.go`.

Add the helper (near the other Model methods):
```go
// reRoot points the model at the repository rooted at path and triggers a full
// reload. switchTarget records where a shell should follow on exit (written to
// --cwd-file by cmd/gg). A fresh span ring is used for the new root; the cmd/gg
// panic dump still references the original repo (acceptable for a debug aid).
func (m Model) reRoot(path string) (tea.Model, tea.Cmd) {
	m.repo = &git.Repo{Runner: gitexec.NewExecRunner("git", path, observ.NewRing(200))}
	m.switchTarget = path
	m.loading = true
	return m, m.loadCmd()
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run TestReRootPointsAtNewWorktreeAndReloads -v`
Expected: PASS. Then `go test ./internal/tui/` — all pass.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui
git add internal/tui/model.go internal/tui/reroot_test.go
git commit -m "feat(tui): add reRoot helper to switch the model to another worktree

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: `W` create-and-switch

`W` in the popup's ACTION state creates the worktree and, on success, re-roots into it.

**Files:** Modify `internal/tui/worktree_popup.go`, `internal/tui/model.go`; extend `internal/tui/worktree_popup_test.go`.

- [ ] **Step 1: Write the failing test** — append to `internal/tui/worktree_popup_test.go`

```go
func TestPopupCreateAndSwitchSetsPendingSwitch(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("W")) // create AND switch
	m = updated.(Model)
	if m.popup != nil {
		t.Error("popup should close on create-and-switch")
	}
	if !m.running {
		t.Error("create-and-switch should start the op")
	}
	if !m.pendingSwitch {
		t.Error("W should mark pendingSwitch so the model re-roots on success")
	}
}

func TestPlainCreateDoesNotSwitch(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("w")) // plain create
	m = updated.(Model)
	if m.pendingSwitch {
		t.Error("plain create (w) must not set pendingSwitch")
	}
}

func TestOpFinishedSwitchesOnPendingSwitch(t *testing.T) {
	dir, repo := newRepoDir(t)
	m := New(repo)
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)

	// Pre-create a worktree so reRoot has a real target.
	wt := filepath.Join(filepath.Dir(dir), "wt-sw")
	runGit(t, dir, "worktree", "add", "-b", "feature/sw", wt, "main")

	m.pendingSwitch = true
	updated, cmd := m.Update(opFinishedMsg{res: engine.Result{Summary: "created", Changed: true, Path: wt}})
	m = updated.(Model)
	if m.switchTarget != wt {
		t.Fatalf("switchTarget = %q, want %q (should re-root to Result.Path)", m.switchTarget, wt)
	}
	if m.pendingSwitch {
		t.Error("pendingSwitch should be cleared after handling")
	}
	if cmd == nil {
		t.Fatal("expected a reload command from the switch")
	}
}
```

Add `"path/filepath"` to the test imports if not present. (`runGit` was added in Task 2's `reroot_test.go`, same package.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run 'TestPopupCreateAndSwitch|TestPlainCreate|TestOpFinishedSwitches' -v`
Expected: FAIL — `W` not handled; `pendingSwitch` not set/honored.

- [ ] **Step 3a: `switchAfter` param** — in `internal/tui/worktree_popup.go`, change `startCreateFromPopup` to take a flag and set `pendingSwitch`:

```go
func (m Model) startCreateFromPopup(switchAfter bool) (tea.Model, tea.Cmd) {
	p := m.popup
	if p.previewErr != nil {
		m.statusMsg = "cannot create: " + p.previewErr.Error()
		return m, nil
	}
	m.pendingSeqBump = p.consumedSeqNames()
	m.pendingSwitch = switchAfter
	m.popup = nil
	return m.startOp(p.createOp())
}
```

In `updatePopupKey` (the `default: // stAction` switch), change the create case and add `W`:
```go
		case "w", "enter":
			return m.startCreateFromPopup(false)
		case "W":
			return m.startCreateFromPopup(true)
```

- [ ] **Step 3b: Honor `pendingSwitch` on success** — in `internal/tui/model.go`, replace the `case opFinishedMsg:` body with:

```go
	case opFinishedMsg:
		m.running = false
		m.opMsgs = nil
		switchTo := ""
		if msg.err != nil {
			m.statusMsg = "error: " + msg.err.Error()
		} else {
			if msg.res.Summary != "" {
				m.statusMsg = msg.res.Summary
			}
			for _, name := range m.pendingSeqBump {
				_, _ = config.BumpSeq(m.gitCommonDir, name)
			}
			if m.pendingSwitch && msg.res.Path != "" {
				switchTo = msg.res.Path
			}
		}
		m.pendingSeqBump = nil
		m.pendingSwitch = false
		if switchTo != "" {
			return m.reRoot(switchTo)
		}
		return m, m.loadCmd()
```

- [ ] **Step 3c: Popup hint** — in `internal/tui/worktree_popup.go` `renderWorktreePopup`, change the ACTION (`default:`) hint to advertise `W`:
```go
	default:
		b.WriteString("[w] create  [W] create & switch  [e] edit name  [esc] cancel")
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run 'TestPopupCreateAndSwitch|TestPlainCreate|TestOpFinishedSwitches' -v`
Expected: PASS. Then `go test ./internal/tui/` and `go test -race ./internal/tui/` — all pass. (If `TestRenderWorktreePopupShowsPreview` asserted the old hint text, it only checks for `"create"`, which is still present — no change needed.)

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui
git add internal/tui/worktree_popup.go internal/tui/model.go internal/tui/worktree_popup_test.go
git commit -m "feat(tui): W creates a worktree and re-roots into it

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: Enter on the Worktrees panel switches into it

The "switch back" half: jump into an existing worktree from the panel.

**Files:** Modify `internal/tui/model.go`; create `internal/tui/worktree_switch_test.go`.

- [ ] **Step 1: Write the failing test** — `internal/tui/worktree_switch_test.go`

```go
package tui

import (
	"path/filepath"
	"testing"
)

func TestEnterOnWorktreePanelSwitches(t *testing.T) {
	dir, repo := newRepoDir(t)
	m := New(repo)
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)

	wt := filepath.Join(filepath.Dir(dir), "wt-enter")
	runGit(t, dir, "worktree", "add", "-b", "feature/e", wt, "main")
	// Reload so the new worktree is in m.worktrees.
	updated, _ = m.Update(m.loadCmd()())
	m = updated.(Model)

	// Focus the Worktrees panel and select the non-current worktree.
	m.focus = panelWorktrees
	idx := -1
	for i, w := range m.worktrees {
		if w.Path != m.currentWorktree {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("expected a second worktree in %v", m.worktrees)
	}
	m.sel[panelWorktrees] = idx

	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(Model)
	resolvedWant, _ := filepath.EvalSymlinks(m.worktrees[idx].Path)
	resolvedGot, _ := filepath.EvalSymlinks(m.switchTarget)
	if resolvedGot != resolvedWant {
		t.Fatalf("switchTarget = %q, want %q", resolvedGot, resolvedWant)
	}
	if cmd == nil {
		t.Fatal("enter on a worktree should return a reload command")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestEnterOnWorktreePanelSwitches -v`
Expected: FAIL — Enter is not handled in normal mode, so `switchTarget` stays empty.

- [ ] **Step 3: Implement** — in `internal/tui/model.go`, in the normal-key `switch msg.String() {` block (NOT inside the modal/popup interception), add an `enter` case:

```go
		case "enter":
			if !m.running && !m.loading && m.focus == panelWorktrees && len(m.worktrees) > 0 {
				target := m.worktrees[m.sel[panelWorktrees]].Path
				if target != "" && target != m.currentWorktree {
					return m.reRoot(target)
				}
			}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run TestEnterOnWorktreePanelSwitches -v`
Expected: PASS. Then `go test ./internal/tui/` — all pass.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui
git add internal/tui/model.go internal/tui/worktree_switch_test.go
git commit -m "feat(tui): Enter on the Worktrees panel switches into that worktree

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: `tui.Run` returns the switch target; `cmd/gg` writes `--cwd-file`

The shell bridge: surface where the shell should `cd`, and write it only when a switch happened.

**Files:** Modify `internal/tui/run.go`, `cmd/gg/main.go`; create `cmd/gg/cwdfile_test.go`.

- [ ] **Step 1: Write the failing test** — `cmd/gg/cwdfile_test.go`

```go
package main

import (
	"reflect"
	"testing"
)

func TestExtractCwdFile(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPath string
		wantRest []string
	}{
		{"absent", []string{"status"}, "", []string{"status"}},
		{"space form", []string{"--cwd-file", "/tmp/x", "status"}, "/tmp/x", []string{"status"}},
		{"equals form", []string{"--cwd-file=/tmp/y"}, "/tmp/y", []string{}},
		{"before subcommand", []string{"--cwd-file", "/tmp/z", "worktree", "add"}, "/tmp/z", []string{"worktree", "add"}},
		{"no value is dropped safely", []string{"--cwd-file"}, "", []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path, rest := extractCwdFile(tc.args)
			if path != tc.wantPath {
				t.Errorf("path = %q, want %q", path, tc.wantPath)
			}
			if !reflect.DeepEqual(rest, tc.wantRest) {
				t.Errorf("rest = %v, want %v", rest, tc.wantRest)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/gg/ -run TestExtractCwdFile -v`
Expected: FAIL — `extractCwdFile` undefined.

- [ ] **Step 3a: `Run` returns the switch target** — replace `internal/tui/run.go` with:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/git"
)

// Run launches the TUI for repo, taking over the alternate screen until the
// user quits. It returns the directory the shell should switch to (the worktree
// the user switched into during the session, or "" if none) so a wrapper can
// cd there on exit.
func Run(repo *git.Repo) (string, error) {
	p := tea.NewProgram(New(repo), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	if m, ok := final.(Model); ok {
		return m.switchTarget, nil
	}
	return "", nil
}
```

- [ ] **Step 3b: `extractCwdFile` + wire into `main`** — in `cmd/gg/main.go`:

Add the helper:
```go
// extractCwdFile pulls a global --cwd-file flag (in either "--cwd-file path" or
// "--cwd-file=path" form) out of args, returning its value and the remaining
// args. A trailing "--cwd-file" with no value is dropped.
func extractCwdFile(args []string) (string, []string) {
	path := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--cwd-file":
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--cwd-file="):
			path = strings.TrimPrefix(a, "--cwd-file=")
		default:
			rest = append(rest, a)
		}
	}
	return path, rest
}
```

In `main`, change the first line from `args := os.Args[1:]` to:
```go
	cwdFile, args := extractCwdFile(os.Args[1:])
```

And change the TUI launch block. Replace:
```go
	if err := tui.Run(repo); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
```
with:
```go
	cwd, err := tui.Run(repo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	// Only write the cwd file when the user actually switched worktrees, so a
	// gg-wrapped shell stays put otherwise.
	if cwdFile != "" && cwd != "" {
		_ = os.WriteFile(cwdFile, []byte(cwd), 0o644)
	}
```

(`strings` and `os` are already imported in `main.go`.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./cmd/gg/ -run TestExtractCwdFile -v` then `go build ./...`
Expected: test PASS; build OK (the `tui.Run` signature change is now consumed by `main`).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui cmd/gg
git add internal/tui/run.go cmd/gg/main.go cmd/gg/cwdfile_test.go
git commit -m "feat(cli): write --cwd-file on worktree switch; Run returns the target

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: `gg shell-init [bash|zsh|fish]`

Emit a `gg()` wrapper that runs the real binary with a temp `--cwd-file` and `cd`s to its contents.

**Files:** Create `internal/shellinit/shellinit.go`, `internal/shellinit/shellinit_test.go`; modify `cmd/gg/main.go`.

- [ ] **Step 1: Write the failing test** — `internal/shellinit/shellinit_test.go`

```go
package shellinit

import (
	"strings"
	"testing"
)

func TestScriptPosix(t *testing.T) {
	for _, sh := range []string{"bash", "zsh"} {
		s, err := Script(sh)
		if err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		// Must call the real binary (not recurse) and cd to the file's contents.
		if !strings.Contains(s, "command gg --cwd-file") {
			t.Errorf("%s: wrapper must invoke `command gg --cwd-file`:\n%s", sh, s)
		}
		if !strings.Contains(s, "gg()") {
			t.Errorf("%s: wrapper must define gg():\n%s", sh, s)
		}
		if !strings.Contains(s, "cd ") {
			t.Errorf("%s: wrapper must cd:\n%s", sh, s)
		}
	}
}

func TestScriptFish(t *testing.T) {
	s, err := Script("fish")
	if err != nil {
		t.Fatalf("fish: %v", err)
	}
	if !strings.Contains(s, "function gg") {
		t.Errorf("fish wrapper must define `function gg`:\n%s", s)
	}
	if !strings.Contains(s, "command gg --cwd-file") {
		t.Errorf("fish wrapper must invoke `command gg --cwd-file`:\n%s", s)
	}
	if !strings.Contains(s, "cd (cat") {
		t.Errorf("fish wrapper must `cd (cat ...)`:\n%s", s)
	}
}

func TestScriptUnknownShell(t *testing.T) {
	if _, err := Script("powershell"); err == nil {
		t.Fatal("unknown shell should error")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/shellinit/ -v`
Expected: FAIL — package/`Script` undefined.

- [ ] **Step 3a: Implement the package** — `internal/shellinit/shellinit.go`

```go
// Package shellinit emits shell wrapper functions that let `gg` change the
// calling shell's directory on exit (via a temp --cwd-file), so create-and-
// switch and worktree switching drop the user into the chosen worktree.
package shellinit

import "fmt"

const posixWrapper = `gg() {
  local _gg_cwd
  _gg_cwd="$(mktemp)"
  command gg --cwd-file "$_gg_cwd" "$@"
  local _gg_code=$?
  if [ -s "$_gg_cwd" ]; then
    cd "$(cat "$_gg_cwd")" || true
  fi
  rm -f "$_gg_cwd"
  return $_gg_code
}
`

const fishWrapper = `function gg
    set -l _gg_cwd (mktemp)
    command gg --cwd-file "$_gg_cwd" $argv
    set -l _gg_code $status
    if test -s "$_gg_cwd"
        cd (cat "$_gg_cwd")
    end
    rm -f "$_gg_cwd"
    return $_gg_code
end
`

// Script returns the wrapper function for the given shell ("bash", "zsh", or
// "fish"). bash and zsh share a POSIX wrapper.
func Script(shell string) (string, error) {
	switch shell {
	case "bash", "zsh":
		return posixWrapper, nil
	case "fish":
		return fishWrapper, nil
	default:
		return "", fmt.Errorf("shell-init: unsupported shell %q (use bash, zsh, or fish)", shell)
	}
}
```

- [ ] **Step 3b: Wire the subcommand** — in `cmd/gg/main.go`, add the import `"github.com/gigagit/gg/internal/shellinit"`, and add a dispatch branch in `main` (after the `extractCwdFile` line and BEFORE the `inspect` branch):

```go
	if len(args) > 0 && args[0] == "shell-init" {
		runShellInit(args[1:])
		return
	}
```

Add the handler:
```go
func runShellInit(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: gg shell-init [bash|zsh|fish]")
		os.Exit(2)
	}
	script, err := shellinit.Script(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Fprint(os.Stdout, script)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/shellinit/ -v` then `go build ./...`
Expected: tests PASS; build OK. Manual check: `go run ./cmd/gg shell-init zsh` prints the wrapper; `go run ./cmd/gg shell-init nope` exits 2.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/shellinit cmd/gg
git add internal/shellinit/shellinit.go internal/shellinit/shellinit_test.go cmd/gg/main.go
git commit -m "feat(cli): add gg shell-init [bash|zsh|fish] cd-on-switch wrapper

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 7: Full-package verification

**Files:** none (verification only)

- [ ] **Step 1: Full suite** — `go test ./...` — Expected: all PASS.
- [ ] **Step 2: Race** — `go test -race ./internal/tui/ ./internal/engine/` — Expected: PASS, no races.
- [ ] **Step 3: Vet** — `go vet ./...` — Expected: no output.
- [ ] **Step 4: Format** — `gofmt -l internal cmd` — Expected: empty (else `gofmt -w` and amend).
- [ ] **Step 5: Manual smoke (document result, no automated TTY):**
  - `go build -o /tmp/gg ./cmd/gg`
  - `/tmp/gg shell-init bash` prints the wrapper.
  - In a real repo: `eval "$(/tmp/gg shell-init bash)"` then `gg`, create-and-switch a worktree with `W`, confirm the shell lands in the new worktree dir on exit.

No commit needed if everything is already committed.

---

## Self-Review Notes (plan author)

- **Spec coverage:** §7 create-and-switch re-root → Tasks 1–3 (`Result.Path` → `reRoot` → `W`); §7 "switch into a worktree" → Task 4 (panel Enter); §7.1 `--cwd-file` written on switch only → Task 5; §7.1 `gg shell-init [bash|zsh|fish]` wrapper → Task 6. Both switch triggers (`W`, panel Enter) flow through the single `reRoot(path)` (spec notes Plan B reuses it).
- **Single source of truth:** re-root consumes `engine.Result.Path` (the path the op created), never a popup recomputation — no drift (Task 1 + Task 3b).
- **`--cwd-file` semantics:** written only on an actual switch (`switchTarget` non-empty); a `gg`-wrapped shell otherwise stays put (Task 5; wrapper's `[ -s ]`/`test -s` guard in Task 6).
- **Deferred to A3c (correctly):** `gg worktree add/list` CLI — building it well means extracting a shared `worktree.Resolve(cfg, repo)` helper used by both the popup and the CLI; its own slice.
- **Known limitation (documented in `reRoot`):** a fresh span ring per re-root means `cmd/gg`'s panic dump references the original repo. Acceptable for a debug aid.
- **Type consistency:** `reRoot(path) (tea.Model, tea.Cmd)`, `switchTarget`/`pendingSwitch` Model fields, `startCreateFromPopup(switchAfter bool)`, `Result.Path`, `extractCwdFile(args) (string, []string)`, `tui.Run(repo) (string, error)`, `shellinit.Script(shell) (string, error)` are used consistently across tasks.
