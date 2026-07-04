# Resume Paused Rebase/Merge After External Conflict Resolution — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a rebase/merge/cherry-pick/revert is paused and its conflicts were resolved outside gg, a status refresh (`r`, background, watcher, startup) detects it and asks once — continue or abort — with a persistent `⏸` status segment and a relaxed `x` re-entry as the always-available fallback.

**Architecture:** A pure stat-level probe (`git.PausedOpIn`) detects a paused sequencer op from the worktree's git dir. `domain.Conflict`/`conflictState` report the op even with zero conflicted files (git dir cached once per Service, so a clean repo's steady state costs zero subprocesses). The TUI evaluates every status arrival: a one-shot popup (`resumePromptPopup`) offers Continue/Abort/Not now, dispatching the **existing** `engine.ContinueOp`/`AbortOp`. No engine or CLI changes.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model`), real-git tests in `t.TempDir()`, `FakeRunner` for invocation-count assertions.

**Spec:** `docs/superpowers/specs/2026-07-03-resume-paused-op-design.md`

## Global Constraints

- Work in the worktree at `.claude/worktrees/resume-paused-op` (branch `feat/resume-paused-op`). All paths below are relative to its root.
- TDD: write the failing test first, watch it fail, implement, watch it pass, commit.
- `gofmt -l internal cmd` clean and `go vet ./...` clean before every commit.
- LF line endings (repo `.gitattributes` forces LF for Go source).
- `internal/tui` non-test files must NOT import `internal/git` (archtest-guarded). Test files may.
- TUI `Model` is a value receiver; state that must survive Model copies lives in pointer fields (the layer stack already is one).
- Every commit message ends with:
  ```
  Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_0169sFgFY7F4cQ4mwBxE7jB6
  ```

---

### Task 1: `git.PausedOpIn` — pure stat-level paused-op probe

**Files:**
- Modify: `internal/git/conflict.go` (append at end)
- Test: `internal/git/conflict_test.go` (append at end)

**Interfaces:**
- Consumes: nothing (pure function over the filesystem).
- Produces: `func PausedOpIn(gitDir string) string` — returns `"merge" | "rebase" | "cherry-pick" | "revert" | ""`. Task 2 calls it from `internal/domain`.

- [ ] **Step 1: Write the failing test**

Append to `internal/git/conflict_test.go`:

```go
// PausedOpIn is a pure stat probe over a git dir's sequencer markers; each
// case fabricates the marker files directly — no git needed.
func TestPausedOpIn(t *testing.T) {
	touch := func(t *testing.T, dir string, parts ...string) {
		t.Helper()
		p := filepath.Join(append([]string{dir}, parts...)...)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkdir := func(t *testing.T, dir string, name string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  string
	}{
		{"clean", func(*testing.T, string) {}, ""},
		{"merge", func(t *testing.T, d string) { touch(t, d, "MERGE_HEAD") }, "merge"},
		{"rebase merge backend", func(t *testing.T, d string) { mkdir(t, d, "rebase-merge") }, "rebase"},
		{"rebase apply backend", func(t *testing.T, d string) {
			mkdir(t, d, "rebase-apply")
			touch(t, d, "rebase-apply", "rebasing")
		}, "rebase"},
		{"git-am is not modeled", func(t *testing.T, d string) { mkdir(t, d, "rebase-apply") }, ""},
		{"cherry-pick", func(t *testing.T, d string) { touch(t, d, "CHERRY_PICK_HEAD") }, "cherry-pick"},
		{"revert", func(t *testing.T, d string) { touch(t, d, "REVERT_HEAD") }, "revert"},
		{"rebase wins over cherry-pick head", func(t *testing.T, d string) {
			mkdir(t, d, "rebase-merge")
			touch(t, d, "CHERRY_PICK_HEAD")
		}, "rebase"},
		{"missing git dir", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "gitdir")
			if tc.setup != nil {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				tc.setup(t, dir)
			}
			if got := PausedOpIn(dir); got != tc.want {
				t.Errorf("PausedOpIn = %q, want %q", got, tc.want)
			}
		})
	}
}
```

`internal/git/conflict_test.go` may not yet import `os`/`filepath`/`testing`-adjacent packages — check its import block and add `"os"` and `"path/filepath"` if missing.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git -run TestPausedOpIn -v`
Expected: FAIL (compile error: `undefined: PausedOpIn`)

- [ ] **Step 3: Write the implementation**

Append to `internal/git/conflict.go`:

```go
// PausedOpIn reports which sequencer operation is paused in the worktree
// whose git dir is gitDir: "merge", "rebase", "cherry-pick", "revert", or "".
// Pure file stats — no git invocation — so callers can probe on every status
// refresh for free. Unlike the *InProgress verbs it is independent of the
// conflicted-file count, so it also detects an op whose conflicts were
// resolved outside gg but never continued. Probe order mirrors
// conflictState/InProgressOp: rebase must win over cherry-pick (a paused
// rebase pick also sets CHERRY_PICK_HEAD). A rebase-apply dir WITHOUT its
// "rebasing" marker is git-am, which gg does not model — reported as "".
func PausedOpIn(gitDir string) string {
	exists := func(parts ...string) bool {
		_, err := os.Stat(filepath.Join(append([]string{gitDir}, parts...)...))
		return err == nil
	}
	switch {
	case exists("MERGE_HEAD"):
		return "merge"
	case exists("rebase-merge"):
		return "rebase"
	case exists("rebase-apply", "rebasing"):
		return "rebase"
	case exists("CHERRY_PICK_HEAD"):
		return "cherry-pick"
	case exists("REVERT_HEAD"):
		return "revert"
	}
	return ""
}
```

`internal/git/conflict.go` already imports `os` and `path/filepath` (used by `RebaseParties`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git -run TestPausedOpIn -v`
Expected: PASS (all subtests)

- [ ] **Step 5: Commit**

```bash
gofmt -l internal cmd && go vet ./internal/git
git add internal/git/conflict.go internal/git/conflict_test.go
git commit -m "feat(git): PausedOpIn stat-level sequencer probe

Detects a paused merge/rebase/cherry-pick/revert from the git dir's marker
files alone — no git invocation, independent of the conflicted-file count,
so a rebase resolved outside gg is still visible."
```

(Append the standard trailers from Global Constraints to this and every commit.)

---

### Task 2: domain — report a paused op even with zero conflicted files

**Files:**
- Modify: `internal/domain/conflict.go` (doc comments, `Conflict`, `conflictState`, new helpers)
- Modify: `internal/domain/service.go` (two new Service fields)
- Test: `internal/domain/conflict_test.go` (append)

**Interfaces:**
- Consumes: `git.PausedOpIn(gitDir string) string` (Task 1); existing verbs `GitDir`, `MergeHeadName`, `RebaseParties`, `CherryPickHeadSummary`, `RevertHeadSummary`.
- Produces: unchanged signatures `Conflict(ctx, st) ConflictState` / `conflictState(ctx, st) ConflictState`, with the NEW contract: `Op != ""` whenever a sequencer op is in progress, even when `st.Counts().Conflicted == 0`. Task 3/4 rely on this via the existing `statusPayload.conflict` / `Snapshot.Conflict` plumbing (no TUI plumbing changes needed for detection).

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/conflict_test.go`:

```go
// resolvePause stages a resolution for the f.txt conflict without continuing
// the paused op — the "resolved outside gg" state the resume prompt detects.
func resolvePause(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, dir, "f.txt", "resolved\n")
	gitRunDir(t, dir, "", "add", "-A")
}

func TestConflictDetectsResolvedPausedRebase(t *testing.T) {
	dir := rebaseConflictDir(t)
	resolvePause(t, dir)
	s := svcAt(dir)
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n := st.Counts().Conflicted; n != 0 {
		t.Fatalf("Conflicted = %d, want 0 after resolving", n)
	}
	cs := s.Conflict(context.Background(), st)
	if cs.Op != "rebase" {
		t.Fatalf("Conflict = %+v, want Op=rebase", cs)
	}
	if cs.Source != "feature" || cs.Target != "main" {
		t.Errorf("attribution = %+v, want feature onto main", cs)
	}
}

func TestConflictDetectsResolvedPausedMerge(t *testing.T) {
	dir := mergeConflictDir(t)
	resolvePause(t, dir)
	s := svcAt(dir)
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cs := s.Conflict(context.Background(), st)
	if cs.Op != "merge" || cs.Source != "feature" || cs.Target != "main" {
		t.Fatalf("Conflict = %+v, want {merge feature main}", cs)
	}
}

func TestSnapshotCarriesResolvedPausedOp(t *testing.T) {
	dir := rebaseConflictDir(t)
	resolvePause(t, dir)
	snap, err := svcAt(dir).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Conflict.Op != "rebase" {
		t.Errorf("snapshot conflict = %+v, want Op=rebase", snap.Conflict)
	}
}

// Steady-state clean path: after the first Conflict call caches the git dir,
// repeated clean-status calls must run ZERO further git invocations — the
// paused-op probe is pure file stats.
func TestConflictCleanSteadyStateCachesGitDir(t *testing.T) {
	fake := gitexec.NewFakeRunner()
	gitDir := t.TempDir() // stands in for the resolved git dir; no sequencer markers
	fake.SetResponse("git rev-parse (git-dir)", gitexec.Result{Stdout: gitDir + "\n"})
	fake.SetResponse("git rev-parse (common-dir)", gitexec.Result{Stdout: gitDir + "\n"})
	s := New(&git.Repo{Runner: fake})
	st := model.WorkingTreeStatus{} // clean: zero conflicted files
	for i := 0; i < 3; i++ {
		if cs := s.Conflict(context.Background(), st); cs != (ConflictState{}) {
			t.Fatalf("call %d: clean Conflict = %+v, want zero", i, cs)
		}
	}
	n := 0
	for _, c := range fake.Calls {
		if c.Name == "git rev-parse (git-dir)" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("git dir resolved %d times across 3 clean calls, want 1 (cached)", n)
	}
}
```

Add `"github.com/homeend/gigagit/internal/model"` to the test file's imports (`git`, `gitexec`, `observ` are already there).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/domain -run 'TestConflictDetectsResolved|TestSnapshotCarriesResolvedPausedOp|TestConflictCleanSteadyState' -v`
Expected: FAIL — the three detection tests get `Op=""` (early return on zero conflicts); the caching test fails because the fast path doesn't exist yet (the fake errors on the unconfigured status… it doesn't run status — it fails on `Conflict` returning zero but `n != 1`: zero `git rev-parse (git-dir)` calls).

- [ ] **Step 3: Implement**

**3a.** In `internal/domain/service.go`, add two fields to the `Service` struct, after the `prefixRepo` field block:

```go
	// gitDirMu guards gitDirPath — this worktree's git dir, resolved once on
	// first use (a repo's git dir never moves during a session; reRoot builds
	// a fresh Service). "" = not yet resolved; a failed resolution retries on
	// the next call. Backs the stat-level paused-op probe in conflictState.
	gitDirMu   sync.Mutex
	gitDirPath string
```

(`sync` is already imported.)

**3b.** Rewrite `internal/domain/conflict.go`'s type comment, `Conflict`, and `conflictState`, and add helpers. The full new file body below the `Describe` function (which is unchanged) — replace everything from the `Conflict` doc comment down:

```go
// Conflict derives the conflict/paused-op state from a status the caller
// already read. It is the public face of conflictState, used by the TUI's
// status source so a per-panel status refresh carries the same attribution
// the full Snapshot did. Cheap on the steady-state clean path: once the git
// dir is cached, a clean working tree with no paused op returns after pure
// file stats — zero git invocations and no gate. Otherwise the git probes
// run under their own Read reservation. The reservation is taken HERE, not
// inside conflictState — the helper's other caller (loadSnapshot) already
// holds one, and a nested Read can deadlock behind a queued writer under the
// gate's writer-preferring FIFO.
func (s *Service) Conflict(ctx context.Context, st model.WorkingTreeStatus) ConflictState {
	if st.Counts().Conflicted == 0 {
		if d := s.cachedGitDir(); d != "" && git.PausedOpIn(d) == "" {
			return ConflictState{}
		}
		// First call (git dir not yet cached) or a paused op detected:
		// fall through to the gated read.
	}
	cs, err := query(ctx, s, "conflict:"+st.Branch, func(ctx context.Context) (ConflictState, error) {
		return s.conflictState(ctx, st), nil
	})
	if err != nil {
		// Gate acquisition failed (ctx cancelled mid-refresh): no attribution
		// this round; the next status refresh retries.
		return ConflictState{}
	}
	return cs
}

// conflictState attributes st's conflicts — or a paused sequencer op whose
// conflicts were all resolved (e.g. outside gg) — to the operation in
// progress. With unmerged files present it probes via the git verbs exactly
// as before; with none it falls back to the stat-level PausedOpIn probe, so
// a resolved-but-not-continued rebase is still reported. During a rebase
// HEAD is detached, so the rebase target comes from the rebase state
// (RebaseParties), not st.Branch. It assumes the caller holds a Read
// reservation (Conflict and loadSnapshot both do) — it must not acquire its
// own.
func (s *Service) conflictState(ctx context.Context, st model.WorkingTreeStatus) ConflictState {
	if st.Counts().Conflicted > 0 {
		if ok, err := s.repo.MergeInProgress(ctx, ""); err == nil && ok {
			return s.attributeOp(ctx, "merge", st)
		}
		if ok, err := s.repo.RebaseInProgress(ctx, ""); err == nil && ok {
			return s.attributeOp(ctx, "rebase", st)
		}
		// Cherry-pick/revert probed last: a paused rebase pick also sets
		// CHERRY_PICK_HEAD.
		if ok, err := s.repo.CherryPickInProgress(ctx, ""); err == nil && ok {
			return s.attributeOp(ctx, "cherry-pick", st)
		}
		if ok, err := s.repo.RevertInProgress(ctx, ""); err == nil && ok {
			return s.attributeOp(ctx, "revert", st)
		}
		return ConflictState{}
	}
	d := s.gitDirCached(ctx)
	if d == "" {
		return ConflictState{}
	}
	if op := git.PausedOpIn(d); op != "" {
		return s.attributeOp(ctx, op, st)
	}
	return ConflictState{}
}

// attributeOp fills Source/Target for a detected op. Best-effort: a failed
// read leaves the field empty rather than erroring — the Op alone is enough
// to drive the UI.
func (s *Service) attributeOp(ctx context.Context, op string, st model.WorkingTreeStatus) ConflictState {
	switch op {
	case "merge":
		src, _ := s.repo.MergeHeadName(ctx, "")
		return ConflictState{Op: "merge", Source: src, Target: st.Branch}
	case "rebase":
		branch, onto, _ := s.repo.RebaseParties(ctx, "")
		return ConflictState{Op: "rebase", Source: branch, Target: onto}
	case "cherry-pick":
		src, _ := s.repo.CherryPickHeadSummary(ctx, "")
		return ConflictState{Op: "cherry-pick", Source: src}
	case "revert":
		src, _ := s.repo.RevertHeadSummary(ctx, "")
		return ConflictState{Op: "revert", Source: src}
	}
	return ConflictState{}
}

// cachedGitDir returns the memoized git dir, or "" when not yet resolved.
// Lock-only — safe to call outside any gate reservation.
func (s *Service) cachedGitDir() string {
	s.gitDirMu.Lock()
	defer s.gitDirMu.Unlock()
	return s.gitDirPath
}

// gitDirCached resolves and memoizes this worktree's git dir. The first call
// runs one git invocation, so the caller must hold a Read reservation (its
// only caller, conflictState, always does). A failed resolution returns ""
// and retries on the next call.
func (s *Service) gitDirCached(ctx context.Context) string {
	s.gitDirMu.Lock()
	defer s.gitDirMu.Unlock()
	if s.gitDirPath == "" {
		if d, err := s.repo.GitDir(ctx); err == nil {
			s.gitDirPath = d
		}
	}
	return s.gitDirPath
}
```

Update the `ConflictState` type comment (top of the file) to the new contract:

```go
// ConflictState attributes the current conflict — or a paused sequencer op —
// to the operation that produced it. Op != "" whenever a merge/rebase/
// cherry-pick/revert is in progress: with unresolved conflicts, OR paused
// with everything resolved (e.g. resolved outside gg but never continued).
// The zero value (Op == "") means no such op is in progress (e.g. a
// stash-pop conflict — there is no source to show). Callers distinguish
// "conflicted" from "paused, resolved" via the status's own conflicted-file
// count.
type ConflictState struct {
```

Add `"github.com/homeend/gigagit/internal/git"` to `internal/domain/conflict.go`'s imports.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/domain -run 'Conflict|InProgress|Snapshot' -v`
Expected: PASS — the four new tests AND every pre-existing conflict test (`TestConflictStateMerge/Rebase/CherryPick/Revert`, `TestConflictStateCleanIsZero`, `TestConflictCleanRepoIsZero`, `TestSnapshotCarriesConflictSource`, `TestInProgressOp*`) — the conflicted-path behavior is unchanged by the `attributeOp` extraction.

- [ ] **Step 5: Run the full domain + git packages**

Run: `go test ./internal/domain ./internal/git`
Expected: ok (both packages)

- [ ] **Step 6: Commit**

```bash
gofmt -l internal cmd && go vet ./internal/domain
git add internal/domain/conflict.go internal/domain/service.go internal/domain/conflict_test.go
git commit -m "feat(domain): Conflict reports a paused sequencer op with zero conflicted files

conflictState falls back to the stat-level git.PausedOpIn probe when no
unmerged files remain, so a rebase/merge resolved outside gg is still
attributed. The git dir is resolved once per Service and cached; a clean
repo's steady-state Conflict call runs zero git invocations, preserving the
'clean status costs nothing' contract. Conflicted-path probes unchanged
(extracted into attributeOp)."
```

---

### Task 3: TUI — one-shot resume prompt on status arrival

**Files:**
- Create: `internal/tui/resume_prompt_popup.go`
- Test: `internal/tui/resume_prompt_test.go`
- Modify: `internal/tui/model.go` (new field ~line 91; two wiring lines ~660 and ~737; one reset line in `reRoot` ~line 2725)

**Interfaces:**
- Consumes: `m.conflict domain.ConflictState` (Task 2's new contract), `m.status.Conflicts()`, `m.opsIdle()`, `m.pushLayer/popLayer/topLayer`, `m.startOp(engine.Operation)`, existing `engine.ContinueOp{}`/`engine.AbortOp{}`, popup helpers `overlayDims/popupInnerWidth/popupTextWidth/wrapWidth/selectedRow/modalStyle/overlayCenter/clipToHeight` and `conflictSrcStyle` (all package-local).
- Produces: `func (m Model) maybeResumePrompt() Model` (called from the two status-arrival sites), `type resumePromptPopup` (a `layer`), `Model.resumePromptShown bool`. Task 4's `x` path is independent of these.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/resume_prompt_test.go`:

```go
package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// pausedResolvedModel is a Model observing "rebase paused, zero conflicted
// files" — the state the one-shot prompt fires on. Zero-value running/loading
// means opsIdle() is true; zero-value status has no conflicts.
func pausedResolvedModel() Model {
	return Model{conflict: domain.ConflictState{Op: "rebase", Source: "feature", Target: "main"}}
}

func TestMaybeResumePromptFiresOncePerPause(t *testing.T) {
	m := pausedResolvedModel()
	m = m.maybeResumePrompt()
	if _, ok := m.topLayer().(*resumePromptPopup); !ok {
		t.Fatalf("expected resumePromptPopup on top, got %T", m.topLayer())
	}
	if !m.resumePromptShown {
		t.Fatal("one-shot flag not set when the prompt was shown")
	}
	m = m.popLayer() // user chose Not now
	m = m.maybeResumePrompt()
	if m.topLayer() != nil {
		t.Fatal("re-prompted for the same pause after Not now")
	}
}

func TestMaybeResumePromptSkipsWhileBusyThenRetries(t *testing.T) {
	m := pausedResolvedModel()
	m.running = true // an op is in flight
	m = m.maybeResumePrompt()
	if m.topLayer() != nil || m.resumePromptShown {
		t.Fatal("prompted (or burned the one-shot flag) while not idle")
	}
	m.running = false
	m = m.maybeResumePrompt()
	if _, ok := m.topLayer().(*resumePromptPopup); !ok {
		t.Fatal("did not retry once idle — the flag must only be set when actually shown")
	}
}

func TestMaybeResumePromptSkipsWhileLayerOpen(t *testing.T) {
	m := pausedResolvedModel()
	m = m.pushLayer(&commitPopup{}) // any other window owns the screen
	m = m.maybeResumePrompt()
	if m.resumePromptShown {
		t.Fatal("burned the one-shot flag under another layer")
	}
	if _, ok := m.topLayer().(*resumePromptPopup); ok {
		t.Fatal("prompted on top of another popup")
	}
}

func TestMaybeResumePromptNotWhileConflictsRemain(t *testing.T) {
	m := pausedResolvedModel()
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "f.txt", Kind: model.KindUnmerged}}}
	m = m.maybeResumePrompt()
	if m.topLayer() != nil {
		t.Fatal("prompted while unmerged files remain — that is the conflict notice's job")
	}
}

func TestMaybeResumePromptReArmsOnStateChange(t *testing.T) {
	m := pausedResolvedModel()
	m = m.maybeResumePrompt()
	m = m.popLayer() // Not now
	m.conflict = domain.ConflictState{} // op finished/aborted outside
	m = m.maybeResumePrompt()
	if m.resumePromptShown {
		t.Fatal("flag not re-armed when the paused op cleared")
	}
	m.conflict = domain.ConflictState{Op: "merge"} // a NEW pause
	m = m.maybeResumePrompt()
	if _, ok := m.topLayer().(*resumePromptPopup); !ok {
		t.Fatal("no prompt for a new pause instance")
	}
}

func TestResumePromptEscClosesWithoutOp(t *testing.T) {
	m := pausedResolvedModel()
	m = m.maybeResumePrompt()
	p := m.topLayer().(*resumePromptPopup)
	m2, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m2.topLayer() != nil {
		t.Fatal("esc did not close the prompt")
	}
	if cmd != nil || m2.running {
		t.Fatal("esc must not start anything")
	}
}

// pausedResolvedRepoDir builds a real repo mid-rebase whose conflict has been
// resolved and staged but not continued.
func pausedResolvedRepoDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(tolerate string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil && args[0] != tolerate {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("", "init", "-q", "-b", "main")
	write("base\n")
	run("", "add", "-A")
	run("", "commit", "-qm", "base")
	run("", "checkout", "-q", "-b", "feature")
	write("theirs\n")
	run("", "add", "-A")
	run("", "commit", "-qm", "feature")
	run("", "checkout", "-q", "main")
	write("ours\n")
	run("", "add", "-A")
	run("", "commit", "-qm", "main")
	run("", "checkout", "-q", "feature")
	run("rebase", "rebase", "main") // pauses on the f.txt conflict
	write("resolved\n")
	run("", "add", "-A") // resolved + staged, NOT continued
	return dir
}

func TestResumePromptContinueDispatchesOp(t *testing.T) {
	dir := pausedResolvedRepoDir(t)
	m := Model{svc: domain.OpenTUI(dir), conflict: domain.ConflictState{Op: "rebase"}}
	m = m.maybeResumePrompt()
	p, ok := m.topLayer().(*resumePromptPopup)
	if !ok {
		t.Fatalf("expected resumePromptPopup, got %T", m.topLayer())
	}
	m2, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // sel 0 = Continue
	if m2.topLayer() != nil {
		t.Fatal("prompt did not close on enter")
	}
	if !m2.running || cmd == nil {
		t.Fatal("Continue did not dispatch an operation")
	}
	// Drain the real op to completion so the temp dir isn't torn down under a
	// live git process.
	for {
		if _, done := (<-m2.opMsgs).(opFinishedMsg); done {
			break
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestMaybeResumePrompt|TestResumePrompt' -v`
Expected: FAIL (compile error: `undefined: resumePromptPopup` / `m.maybeResumePrompt`)

- [ ] **Step 3: Implement the popup + gate**

Create `internal/tui/resume_prompt_popup.go`:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
)

// resumePromptPopup asks the one continue/abort question after gg detects a
// paused merge/rebase/cherry-pick/revert whose conflicts were all resolved
// outside gg (nothing left unmerged, op never continued). esc = Not now —
// the ⏸ status segment and the x key remain the way back in (never trap the
// user). Continue/abort dispatch the existing generic engine ops.
type resumePromptPopup struct {
	op     string // "merge" | "rebase" | "cherry-pick" | "revert"
	detail string // ConflictState.Describe() at push time; "" when unattributed
	sel    int    // 0 = continue, 1 = abort, 2 = not now
}

// maybeResumePrompt pushes the one-shot continue/abort popup when a status
// arrival observes a paused sequencer op with zero conflicted files. The
// flag is set only when the popup is actually shown — a busy skip (op
// running, another window open) retries on the next status arrival — and it
// re-arms whenever the repo leaves the paused-resolved state, so a NEW pause
// prompts again. Call at every point that assigns m.conflict from a fresh
// status read.
func (m Model) maybeResumePrompt() Model {
	pausedResolved := m.conflict.Op != "" && len(m.status.Conflicts()) == 0
	if !pausedResolved {
		m.resumePromptShown = false
		return m
	}
	if m.resumePromptShown || !m.opsIdle() || m.proc != nil || m.modal != nil ||
		m.topLayer() != nil || m.stashView != nil || m.filesView != nil {
		return m
	}
	m.resumePromptShown = true
	return m.pushLayer(&resumePromptPopup{op: m.conflict.Op, detail: m.conflict.Describe()})
}

// options returns the fixed three-choice list, continue first.
func (p *resumePromptPopup) options() []string {
	return []string{"Continue " + p.op, "Abort " + p.op, "Not now"}
}

func (p *resumePromptPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	n := len(p.options())
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc: // esc = Not now — never trap; [x] and the ⏸ segment remain
		return m.popLayer(), nil
	case tea.KeyUp:
		p.sel = (p.sel - 1 + n) % n
		return m, nil
	case tea.KeyDown:
		p.sel = (p.sel + 1) % n
		return m, nil
	case tea.KeyEnter:
		sel := p.sel
		m = m.popLayer()
		switch sel {
		case 0:
			return m.startOp(engine.ContinueOp{})
		case 1:
			return m.startOp(engine.AbortOp{})
		}
		return m, nil
	}
	switch msg.String() { // direct shortcuts, mirroring the conflict process keys
	case "c":
		return m.popLayer().startOp(engine.ContinueOp{})
	case "a":
		return m.popLayer().startOp(engine.AbortOp{})
	}
	return m, nil // swallow everything else — no fallthrough to global keys
}

func (p *resumePromptPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	textW := popupTextWidth(popupInnerWidth(w))
	var b strings.Builder
	b.WriteString("⏸ " + p.op + " paused — all conflicts resolved\n")
	if p.detail != "" {
		b.WriteString(conflictSrcStyle.Render(p.detail) + "\n")
	}
	b.WriteString("\n")
	for _, line := range wrapWidth("Continue the "+p.op+" now, or abort it? You can come back any time with [x].", textW, 1<<20) {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n")
	for i, opt := range p.options() {
		prefix := "  "
		if i == p.sel {
			prefix = "> "
		}
		row := prefix + opt
		if i == p.sel {
			row = selectedRow.Render(row)
		}
		b.WriteString(row + "\n")
	}
	b.WriteString("\n[↑/↓] select  [enter] choose  [c] continue  [a] abort  [esc] not now")
	box := modalStyle.Width(popupInnerWidth(w)).Render(strings.TrimRight(b.String(), "\n")) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
```

- [ ] **Step 4: Wire the Model**

**4a.** In `internal/tui/model.go`, add the field directly under the existing `conflict` field (~line 91):

```go
	conflict          domain.ConflictState // (existing line — anchor, do not change)
	resumePromptShown bool                 // one-shot: the continue/abort prompt fired for the current paused-op instance; re-arms when the state clears (maybeResumePrompt)
```

**4b.** `dataLoadedMsg` path (~line 658): after

```go
			if m.proc != nil {
				return m.proc.refreshed(m)
			}
```

insert:

```go
			m = m.maybeResumePrompt()
```

(before the `feedUpstreams` re-walk `if`, so the early `return m, reload` branch also carries the pushed layer).

**4c.** `srcStatus` case (~line 735): after

```go
			if m.proc != nil {
				return m.proc.refreshed(m) // process re-derives from fresh status
			}
```

insert:

```go
			m = m.maybeResumePrompt()
```

**4d.** In `reRoot` (~line 2725), next to `m.pendingPushTags = nil`, add:

```go
	m.resumePromptShown = false // the new repo's paused state (if any) prompts fresh
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'TestMaybeResumePrompt|TestResumePrompt' -v`
Expected: PASS (all seven)

- [ ] **Step 6: Run the full TUI package**

Run: `go test ./internal/tui`
Expected: ok

- [ ] **Step 7: Commit**

```bash
gofmt -l internal cmd && go vet ./internal/tui
git add internal/tui/resume_prompt_popup.go internal/tui/resume_prompt_test.go internal/tui/model.go
git commit -m "feat(tui): one-shot continue/abort prompt when a paused op's conflicts were resolved externally

Every status arrival (r, background, watcher, snapshot/startup) evaluates
paused-op && zero-conflicts; the first idle observation pushes a popup
offering Continue/Abort/Not now over the existing engine.ContinueOp/AbortOp.
The one-shot flag is set only when actually shown, re-arms when the state
clears, and resets on repo switch."
```

---

### Task 4: TUI — `⏸` status segment, relaxed `x` re-entry, footer/help

**Files:**
- Modify: `internal/tui/avail.go` (new predicate), `internal/tui/model.go:1080-1083` (`x` case), `internal/tui/conflict_process.go:46-55` (`startConflictProcess` guard), `internal/tui/view.go:350-362` (notice), `internal/tui/footer.go:116` (resolve binding), `internal/tui/help.go:106` (x line)
- Test: `internal/tui/resume_prompt_test.go` (append)

**Interfaces:**
- Consumes: `m.conflict.Op` (Task 2 contract), existing `conflictProcess` zero-file rendering (`canContinue`, `loadInProgressCmd`, `inProgressMsg` auto-release).
- Produces: `func (m Model) canEnterConflict() bool` — shared by the `x` dispatch and the footer binding.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/resume_prompt_test.go`:

```go
func TestCanEnterConflictGates(t *testing.T) {
	if (Model{}).canEnterConflict() {
		t.Fatal("x available on a clean repo")
	}
	paused := Model{conflict: domain.ConflictState{Op: "rebase"}}
	if !paused.canEnterConflict() {
		t.Fatal("x unavailable while a rebase is paused with zero conflicts")
	}
	conflicted := Model{status: model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "f", Kind: model.KindUnmerged}}}}
	if !conflicted.canEnterConflict() {
		t.Fatal("x unavailable while conflicts exist (regression)")
	}
	busy := paused
	busy.running = true
	if busy.canEnterConflict() {
		t.Fatal("x must stay opsIdle-gated")
	}
}

func TestStartConflictProcessOpensForPausedOpWithZeroFiles(t *testing.T) {
	m := Model{conflict: domain.ConflictState{Op: "rebase"}}
	m2, cmd := startConflictProcess(m)
	if m2.proc == nil {
		t.Fatal("process did not open for a paused op with zero conflicted files")
	}
	if cmd == nil {
		t.Fatal("expected the in-progress probe cmd")
	}
	if m3, _ := startConflictProcess(Model{}); m3.proc != nil {
		t.Fatal("process opened on a clean repo")
	}
}

func TestResolveFooterBindingAdvertisesPausedOp(t *testing.T) {
	m := Model{conflict: domain.ConflictState{Op: "rebase"}}
	for _, b := range globalBindings {
		if b.id == "resolve" {
			if !b.when(m) {
				t.Fatal("[x] resolve not advertised while a rebase is paused")
			}
			return
		}
	}
	t.Fatal("resolve binding not found in globalBindings")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui -run 'TestCanEnterConflict|TestStartConflictProcessOpens|TestResolveFooterBinding' -v`
Expected: FAIL (compile error: `undefined: canEnterConflict`; the process test fails on the zero-file guard; the footer test fails on the old predicate)

- [ ] **Step 3: Implement**

**3a.** Append to `internal/tui/avail.go`:

```go
// canEnterConflict gates x (and its footer hint): the conflict process opens
// when unmerged files exist OR a sequencer op is paused with everything
// resolved (continue/abort still pending — e.g. resolved outside gg). Shared
// by the x dispatch and the footer binding so the footer never advertises a
// state the handler would refuse.
func (m Model) canEnterConflict() bool {
	return m.opsIdle() && (len(m.status.Conflicts()) > 0 || m.conflict.Op != "")
}
```

**3b.** `internal/tui/model.go` `x` case (line 1080-1083) becomes:

```go
		case "x":
			if m.canEnterConflict() {
				return startConflictProcess(m) // enter / resume from the notice
			}
```

**3c.** `internal/tui/conflict_process.go` `startConflictProcess` (lines 46-55) becomes:

```go
// startConflictProcess fills the active-process slot from the current
// conflicted status. A no-op when nothing is conflicted AND no sequencer op
// is paused (the caller stays as it was). With zero conflicted files and a
// paused op the process opens straight into its "all resolved — continue/
// abort" state.
func startConflictProcess(m Model) (Model, tea.Cmd) {
	files := m.status.Conflicts()
	if len(files) == 0 && m.conflict.Op == "" {
		return m, nil
	}
	m.proc = &conflictProcess{st: confListing, files: files, src: m.conflict}
	return m, m.loadInProgressCmd() // probe merge/rebase so continue/abort can be offered
}
```

**3d.** `internal/tui/view.go` notice block (lines 350-362) — extend with an `else if`:

```go
	if n := len(m.status.Conflicts()); n > 0 && m.proc == nil {
		notice = fmt.Sprintf("⚠ %d conflict", n)
		if n != 1 {
			notice += "s"
		}
		if src := m.conflict.Describe(); src != "" {
			notice += " " + src
		}
		notice += " — press [x] to resolve"
	} else if m.conflict.Op != "" && m.proc == nil {
		// A sequencer op is paused with nothing left unmerged (resolved
		// outside gg, or all handled and the process left open-ended).
		notice = "⏸ " + m.conflict.Op + " paused"
		if src := m.conflict.Describe(); src != "" {
			notice += " (" + src + ")"
		}
		notice += " — press [x] to continue or abort"
	}
```

**3e.** `internal/tui/footer.go` line 116 becomes:

```go
	{"resolve", "x", "[x] resolve", Model.canEnterConflict, scopeGlobal},
```

**3f.** `internal/tui/help.go` line 106 becomes:

```go
		r("x", "enter / resume the conflict process (repo conflicted, or a merge/rebase paused after external resolution)"),
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui -run 'TestCanEnterConflict|TestStartConflictProcessOpens|TestResolveFooterBinding' -v`
Expected: PASS

- [ ] **Step 5: Run the full TUI package (drift guards included)**

Run: `go test ./internal/tui`
Expected: ok — in particular `TestHelpFooterCoverage` (help/footer sync guard) must stay green.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal cmd && go vet ./internal/tui
git add internal/tui/avail.go internal/tui/model.go internal/tui/conflict_process.go internal/tui/view.go internal/tui/footer.go internal/tui/help.go internal/tui/resume_prompt_test.go
git commit -m "feat(tui): ⏸ paused-op status segment + x re-enters the conflict process with zero conflicts

canEnterConflict (shared dispatch/footer predicate) relaxes the x gate to
'conflicts exist OR an op is paused'; startConflictProcess opens into its
existing all-resolved continue/abort state; the status bar shows a
persistent '⏸ <op> paused — press [x]' segment so declining the one-shot
prompt never traps the user."
```

---

### Task 5: Verification + docs wrap-up

**Files:**
- Modify: `CHANGELOG.md` (root, `[Unreleased]`), `README.md` (conflict-resolution section), `CLAUDE.md` (domain + tui package-map rows)

**Interfaces:** none — documentation and final gates.

- [ ] **Step 1: Full test suite**

Run from the worktree root: `./test.sh`
Expected: vet+gofmt stage clean, unit tests ok, e2e ok.

- [ ] **Step 2: Race pass**

Run: `./test.sh race`
Expected: ok (the new `gitDirMu` cache and the popup paths are exercised by the domain/tui tests).

- [ ] **Step 3: CHANGELOG entry**

Add under `## [Unreleased]` → `### Added` (create the subsection if missing, matching the file's existing style):

```markdown
- Resume a paused rebase/merge after external conflict resolution: when a
  merge/rebase/cherry-pick/revert is paused and its conflicts were resolved
  outside gg, the next status refresh (`r`, background, watcher, or startup)
  shows a one-shot prompt — Continue / Abort / Not now — backed by the
  existing continue/abort ops. A persistent `⏸ <op> paused` status segment
  stays visible while the op is paused, and `x` now opens the conflict
  process even with zero conflicted files (straight into its continue/abort
  state). Detection is a stat-level probe (cached git dir → pure file
  stats), so a clean repo still pays zero extra git invocations.
```

- [ ] **Step 4: README**

In the conflict-resolution / keys documentation (search for the `x` key or "Resolve conflicts" section), document: the `⏸ <op> paused — press [x] to continue or abort` segment, the one-shot prompt (Continue / Abort / esc = Not now, `c`/`a` shortcuts), and that `x` re-enters the conflict process when an op is paused with everything resolved. Match the surrounding prose/table style.

- [ ] **Step 5: CLAUDE.md package map**

- `domain` row: update the `Conflict(ctx, st)` description — it now reports a paused sequencer op even with zero conflicted files (stat-level `git.PausedOpIn` probe over a once-per-Service cached git dir; clean steady state = zero git invocations; conflicted-path probes unchanged).
- `tui` row: add a sentence — **Resume-paused-op prompt** (`resume_prompt_popup.go`): every status arrival calls `maybeResumePrompt()` (one-shot flag `resumePromptShown`, set only when shown, re-armed on state change/reRoot); popup dispatches `engine.ContinueOp`/`AbortOp`; `canEnterConflict()` (avail.go) shares the relaxed `x` gate with the footer; `⏸` status segment in view.go.
- `git` row: mention `PausedOpIn` (stat-level sequencer probe, no git invocation).

- [ ] **Step 6: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: changelog/readme/claude.md for the resume-paused-op feature"
```

- [ ] **Step 7: Build a binary for manual verification and report**

```bash
go build -o ./gg ./cmd/gg && echo BINARY: $(pwd)/gg
```

Report the absolute binary path so the human can drive the flow live:
`git rebase` a conflicting branch in a scratch repo → resolve + `git add`
outside gg → press `r` in gg → the prompt appears; esc → `⏸` segment stays;
`x` re-enters; `c` continues.

---

## Self-review notes (already applied)

- **Spec coverage:** detection (Task 1+2), ask-once prompt incl. busy-skip-retry and re-arm (Task 3), indicator + `x` + footer/help (Task 4), docs + no-CLI/no-engine confirmation (Task 5). Non-goals honored: no polling machinery, no `--skip`, no e2e.
- **Type consistency:** `PausedOpIn(gitDir string) string` (Task 1) ⇔ callers in Task 2; `maybeResumePrompt() Model` and `resumePromptPopup` fields consistent across Task 3 code and tests; `canEnterConflict` shared by model.go/footer.go in Task 4; `footerBinding` fields are `id/key/label/when/scope` — the Task 4 test uses `b.id`/`b.when` accordingly.
- **Known-good anchors:** all quoted "existing" code blocks were read from the worktree at plan time (model.go:658-660, 735-737, 1080-1083; conflict_process.go:46-55; view.go:350-362; footer.go:116; help.go:106). If drifted, match by content, not line number.
