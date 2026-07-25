# TUI Stash Untracked-Files Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `-u` stash's untracked files (stored in the invisible `^3` third parent) appear in the TUI stash drill-in, and diff/preview/history/bookmark resolve them against the right commit.

**Architecture:** One task. `loadStashFilesCmd` merges the `^3` parent's files (best-effort) into the tree; `contentLine` gains a per-line `sha` override; a `lineHash` resolver is applied at every consumer that reads content by `m.filesHash`.

**Tech Stack:** Go 1.26, Bubble Tea TUI, real-git test fixtures.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-25-tui-stash-untracked-design.md` (this worktree).
- Best-effort semantics: a failed `^3` resolve (plain stash) skips silently; a failed `CommitFiles` on the `^3` sha degrades to the tracked-only list. Never an error, never a changed message shape.
- The merge happens at the `[]model.CommitFile` level BEFORE `commitFileLines` (one call keeps the directory grouping/headings correct), then untracked paths get their sha stamped by path lookup. Tracked and untracked paths cannot collide within one stash.
- Working dir: `/mnt/t/others/gigagit.worktrees/fix-tui-stash-untracked`, branch `fix/tui-stash-untracked`. Verify with `git branch --show-current` before any edit. This branch is cut from `main` (NOT web-dev).
- Do not touch `internal/web` (already fixed there).

---

### Task 1: The fix + tests + docs

**Files:**
- Modify: `internal/tui/content_popup.go` (contentLine struct, line 22)
- Modify: `internal/tui/stash_view.go` (loadStashFilesCmd, lines 44–59)
- Modify: `internal/tui/files_view.go` (lineHash helper + lines 514, 526, 648–649)
- Modify: `internal/tui/file_preview.go` (lines 44, 78)
- Modify: `internal/tui/bookmark.go` (line 59)
- Test: `internal/git/log_test.go` (append one test)
- Test: `internal/tui/stash_untracked_test.go` (new)
- Modify: `CHANGELOG.md`, `CLAUDE.md`

**Interfaces:**
- Consumes: `svc.StashCommit(ctx, ref) (string, error)`, `svc.CommitFiles(ctx, sha) ([]model.CommitFile, error)`, `commitFileLines([]model.CommitFile) []contentLine`.
- Produces: `contentLine.sha string` ("" = view hash) and `func (m Model) lineHash(l contentLine) string`.

- [ ] **Step 1: Write the failing git-level test**

Append to `internal/git/log_test.go` (helpers `newTestRepo`, `gitIn`, `writeFile` already exist in this file's package; `exec`, `strings`, `os`, `filepath`, `context` already imported):

```go
// A -u stash stores untracked files in a THIRD parent (^3, a root commit).
// CommitFiles must list that parent's files as adds — the first test
// anywhere to drive ^3 (the TUI/web stash drill-ins depend on it).
func TestCommitFilesStashUntrackedParent(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	gitIn(t, dir, "config", "user.email", "t@t")
	gitIn(t, dir, "config", "user.name", "t")
	writeFile(t, dir, "brand-new.txt", "untracked\n")
	gitIn(t, dir, "stash", "push", "-u", "-m", "wip")

	out, err := exec.Command("git", "-C", dir, "rev-parse", "stash@{0}^3").Output()
	if err != nil {
		t.Fatal(err)
	}
	usha := strings.TrimSpace(string(out))

	if got, err := repo.StashCommit(context.Background(), "stash@{0}^3"); err != nil || got != usha {
		t.Fatalf("StashCommit(^3) = %q, %v; want %q", got, err, usha)
	}
	files, err := repo.CommitFiles(context.Background(), usha)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "brand-new.txt" || files[0].Status != "A" {
		t.Fatalf("untracked-parent files = %+v, want [A brand-new.txt]", files)
	}
}
```

- [ ] **Step 2: Write the failing TUI tests**

Create `internal/tui/stash_untracked_test.go`:

```go
package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func stashGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func stashWrite(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// untrackedStashRepo builds a repo whose stash@{0} carries one tracked
// change and one untracked file (-u), returning the dir and the ^3 sha.
func untrackedStashRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	stashGit(t, dir, "init", "-q", "-b", "main")
	stashGit(t, dir, "config", "user.email", "t@t")
	stashGit(t, dir, "config", "user.name", "t")
	stashWrite(t, dir, "tracked.txt", "one\n")
	stashGit(t, dir, "add", "-A")
	stashGit(t, dir, "commit", "-q", "-m", "base")
	stashWrite(t, dir, "tracked.txt", "two\n")
	stashWrite(t, dir, "brand-new.txt", "u\n")
	stashGit(t, dir, "stash", "push", "-u", "-m", "wip")
	return dir, stashGit(t, dir, "rev-parse", "stash@{0}^3")
}

func TestStashFilesIncludeUntrackedParent(t *testing.T) {
	dir, usha := untrackedStashRepo(t)
	m := Model{svc: domain.Open(dir)}
	msg := m.loadStashFilesCmd("stash@{0}")().(stashFilesMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	var tracked, untracked *contentLine
	for i := range msg.lines {
		switch msg.lines[i].path {
		case "tracked.txt":
			tracked = &msg.lines[i]
		case "brand-new.txt":
			untracked = &msg.lines[i]
		}
	}
	if tracked == nil || untracked == nil {
		t.Fatalf("lines = %+v, want tracked.txt and brand-new.txt", msg.lines)
	}
	if tracked.sha != "" {
		t.Errorf("tracked line sha = %q, want empty (view hash)", tracked.sha)
	}
	if untracked.sha != usha || untracked.status != "A" {
		t.Errorf("untracked line = %+v, want sha %s status A", *untracked, usha)
	}
}

func TestStashFilesPlainStashUnchanged(t *testing.T) {
	dir := t.TempDir()
	stashGit(t, dir, "init", "-q", "-b", "main")
	stashGit(t, dir, "config", "user.email", "t@t")
	stashGit(t, dir, "config", "user.name", "t")
	stashWrite(t, dir, "tracked.txt", "one\n")
	stashGit(t, dir, "add", "-A")
	stashGit(t, dir, "commit", "-q", "-m", "base")
	stashWrite(t, dir, "tracked.txt", "two\n")
	stashGit(t, dir, "stash", "push", "-m", "wip") // no -u: no ^3 parent
	m := Model{svc: domain.Open(dir)}
	msg := m.loadStashFilesCmd("stash@{0}")().(stashFilesMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if len(msg.lines) != 1 || msg.lines[0].path != "tracked.txt" || msg.lines[0].sha != "" {
		t.Fatalf("plain stash lines = %+v, want the single tracked file with no sha override", msg.lines)
	}
}

func TestStashUntrackedDiffOpensAgainstParent(t *testing.T) {
	dir, usha := untrackedStashRepo(t)
	m := Model{width: 100, height: 40, sel: map[panel]int{}, svc: domain.Open(dir)}
	m.filesHash = stashGit(t, dir, "rev-parse", "stash@{0}")
	m.filesView = &contentPopup{}
	mm, _ := m.openDiffForFileLine(contentLine{path: "brand-new.txt", status: "A", sha: usha})
	got := mm.(Model)
	if want := "commit:" + usha + ":brand-new.txt"; got.diffTag != want {
		t.Errorf("diffTag = %q, want %q", got.diffTag, want)
	}
}
```

(If a zero-value `Model` panics on a missing field in `openDiffForFileLine`, mirror the minimal fields the existing `Model{width: 100, height: 30, sel: map[panel]int{}}` tests use and note it in your report — do not switch to `loadedModel`.)

- [ ] **Step 3: Run all new tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/fix-tui-stash-untracked && go test ./internal/git/ -run TestCommitFilesStashUntrackedParent -count=1; go test ./internal/tui/ -run 'TestStashFilesIncludeUntrackedParent|TestStashFilesPlainStashUnchanged|TestStashUntrackedDiffOpensAgainstParent' -count=1`
Expected: the git-level test PASSES already (it drives existing verbs — it is the regression anchor); the TUI tests FAIL to compile (`contentLine` has no field `sha`).

- [ ] **Step 4: Implement**

`internal/tui/content_popup.go` — replace the struct (line 22):

```go
type contentLine struct {
	text    string
	heading bool
	path    string // file's (new) path
	oldPath string // set only for renames/copies
	status  string // model.CommitFile.Status letter ("A","M","D","R","C","T")
	sha     string // per-line commit override (a -u stash's ^3 parent); "" = the view's hash
}
```

`internal/tui/stash_view.go` — replace `loadStashFilesCmd`:

```go
// loadStashFilesCmd resolves the stash ref to a SHA, then loads its changed
// files, tagged by ref for stale-gating. A -u stash stores its untracked
// files in a THIRD parent (^3, a root commit) invisible to the first-parent
// diff — resolve it best-effort and merge its files in with a per-line sha,
// so diff/preview/history read them from the right commit. No ^3 = a plain
// stash (skip); a failed ^3 file read degrades to the tracked-only list.
func (m Model) loadStashFilesCmd(ref string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		sha, err := svc.StashCommit(context.Background(), ref)
		if err != nil {
			return stashFilesMsg{tag: ref, err: err}
		}
		files, err := svc.CommitFiles(context.Background(), sha)
		if err != nil {
			return stashFilesMsg{tag: ref, sha: sha, err: err}
		}
		lines := commitFileLines(files)
		if usha, uerr := svc.StashCommit(context.Background(), ref+"^3"); uerr == nil {
			if ufiles, ferr := svc.CommitFiles(context.Background(), usha); ferr == nil && len(ufiles) > 0 {
				upaths := make(map[string]bool, len(ufiles))
				for _, f := range ufiles {
					upaths[f.Path] = true
				}
				lines = commitFileLines(append(files, ufiles...))
				for i := range lines {
					if !lines[i].heading && upaths[lines[i].path] {
						lines[i].sha = usha
					}
				}
			}
		}
		return stashFilesMsg{tag: ref, sha: sha, lines: lines}
	}
}
```

`internal/tui/files_view.go` — add next to `filesViewCommit` (~line 267):

```go
// lineHash resolves the commit a tree line's content lives in: a per-line
// sha (a -u stash's untracked ^3 parent) wins over the view-wide hash.
func (m Model) lineHash(l contentLine) string {
	if l.sha != "" {
		return l.sha
	}
	return m.filesHash
}
```

Same file, the `h` history case (line 514) and `b` blame case (line 526) — both:

```go
		ctx := navContext{path: vis[p.sel].path, rev: m.filesHash}
```

become:

```go
		ctx := navContext{path: vis[p.sel].path, rev: m.lineHash(vis[p.sel])}
```

Same file, `openDiffForFileLine`'s plain-commit tail (lines 648–649):

```go
	m.diffTag = "commit:" + m.filesHash + ":" + l.path
	return m, m.loadCommitDiffCmd(m.filesHash, l)
```

becomes:

```go
	hash := m.lineHash(l)
	m.diffTag = "commit:" + hash + ":" + l.path
	return m, m.loadCommitDiffCmd(hash, l)
```

(Leave line 613's `rev: m.filesHash` — view-level context only. Leave the full-tree/compare/shelf branches untouched.)

`internal/tui/file_preview.go` — line 44:

```go
	path, hash := l.path, m.filesHash
```

becomes:

```go
	path, hash := l.path, m.lineHash(l)
```

and line 78:

```go
	path, hash, svc := l.path, m.filesHash, m.svc
```

becomes:

```go
	path, hash, svc := l.path, m.lineHash(l), m.svc
```

`internal/tui/bookmark.go` — line 59:

```go
				return model.Bookmark{State: model.StateCommitted, Commit: m.filesHash, Path: vis[v.sel].path}, true
```

becomes:

```go
				return model.Bookmark{State: model.StateCommitted, Commit: m.lineHash(vis[v.sel]), Path: vis[v.sel].path}, true
```

- [ ] **Step 5: Run the new tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/fix-tui-stash-untracked && go test ./internal/git/ -run TestCommitFilesStashUntrackedParent -count=1 && go test ./internal/tui/ -run 'TestStashFilesIncludeUntrackedParent|TestStashFilesPlainStashUnchanged|TestStashUntrackedDiffOpensAgainstParent' -count=1`
Expected: PASS (all four).

- [ ] **Step 6: Full gate**

Run: `cd /mnt/t/others/gigagit.worktrees/fix-tui-stash-untracked && go build ./... && go test ./internal/tui/ ./internal/git/ -count=1 && go vet ./internal/tui/ ./internal/git/ && gofmt -l internal/`
Expected: all PASS, vet clean, gofmt silent.

- [ ] **Step 7: Docs**

`CHANGELOG.md`: add, following the file's existing newest-first convention:

```
- tui: the stash drill-in now shows a -u stash's untracked files (the ^3
  third parent, previously invisible); their diff, preview, history/blame,
  and bookmarks resolve against that parent via a per-line sha override.
```

`CLAUDE.md`: run `grep -n "unfixed" CLAUDE.md` — the two hits are the stash `^3` callouts ("the same first-parent blind spot exists in the TUI, unfixed" in the `web` row, and "the same first-parent blind spot exists in the TUI, unfixed" in the web-ui memory-adjacent sentence in the `web` row's stash description). Rewrite each parenthetical so it reads that the TUI now shares the fix, e.g. `(the TUI's stash drill-in merges the ^3 file list with per-line sha overrides — both frontends fixed)`. Keep each row's surrounding text byte-identical otherwise.

- [ ] **Step 8: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/fix-tui-stash-untracked
git add internal/tui/ internal/git/log_test.go CHANGELOG.md CLAUDE.md
git commit -m "fix(tui): stash drill-in shows -u untracked files via the ^3 parent"
```
