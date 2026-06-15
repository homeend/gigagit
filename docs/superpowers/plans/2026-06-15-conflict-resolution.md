# Conflict Resolution (whole-file) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect a conflicted repo from `git status`, show a status-bar notice, and let the user resolve each conflicted file at the whole-file level via a popup (keep ours/theirs/base, keep-modified, delete, mark-resolved), plus continue/abort an in-progress merge or rebase.

**Architecture:** Three merge-ready chunks. **A:** the porcelain parser captures the unmerged conflict code, and `model` classifies it. **B:** git verbs (`checkout --ours/--theirs`, `checkout-index --stage=1`, `rm`, `merge/rebase --continue`) and an `engine.ResolveConflict` op + continue/abort ops. **C:** the TUI status-bar notice and the `x` resolution popup.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`, pointer popup fields), `gitcmd` argv builder (`.Dir()`, `.ArgIf()`), `gitexec.Runner` (`Run`/`RunEnv` for editor-safe continue), `FakeRunner` for argv tests, real `git` in `t.TempDir()` for behavior.

**Spec:** `docs/superpowers/specs/2026-06-15-conflict-resolution-design.md`.

---

## Orientation (read once before Task 1)

- **Parser:** `internal/git/status_parse.go` — the `'u'` branch currently sets only `Kind: KindUnmerged` and drops the conflict code. Porcelain v2 `u` line: `u <XY> <sub> <m1> <m2> <m3> <mW> <h1> <h2> <h3> <path>` → `fields[1]` = the code (`UU`/`DU`/…), `fields[10]` = path.
- **Conflict-code facts** (verified against a real merge): stage 1 = base, 2 = ours, 3 = theirs.
  - `UU`/`AA` → both sides have content (both-modified/both-added).
  - `DU` = deleted by us (stage 1+3, theirs present); `UD` = deleted by them (stage 1+2, ours present); `AU` = added by us (ours only); `UA` = added by them (theirs only); `DD` = both deleted.
  - base (stage 1) exists unless either side is `A` (added).
- **Verb patterns:** `internal/git/merge.go` / `rebase.go` — `gitcmd.New("merge").Arg("--abort")`, optional `.Dir(dir)`, run via `r.Runner.Run`. `MergeInProgress`/`RebaseInProgress` are exit-code probes. **`RunEnv(ctx, name, argv, []string{"GIT_EDITOR=true"})`** runs one invocation with extra env (avoids `--continue` opening an editor).
- **Engine op:** `internal/engine/ops_basic.go` — `Run` emits `Progress`, calls verbs, returns `Result{Summary, Changed:true}`, emits `Done`; `var _ Operation = X{}`. `GitOps` interface in `gitops.go` lists every verb (drift guard `var _ GitOps = (*git.Repo)(nil)`).
- **Domain read:** `internal/domain/query.go` — the `query[T]` helper; `Status` is `query(ctx, s, "status", s.repo.Status)`.
- **Popup pattern:** the stash popups (`internal/tui/stash_popup.go`, `stash_action.go`) — pointer field on `Model`, `updateXKey` swallowing handler, `renderX` via `modalStyle.Width(popupInnerWidth(w)).Render(body)` composited by `overlayCenter` in `view.go`'s `render()`; routed in the popup chain in `model.go`.
- **Status line:** `internal/tui/view.go` ~line 188–200: `statusLine := m.statusMsg` … `statusLine = truncate(oneLine(statusLine), g.w)`. `oneLine` collapses whitespace.
- **Write path:** `m.startOp(engine.X{})` (op.go) runs a mutating op and full-reloads on finish.

**Run tests:** `go test ./internal/git/ ./internal/model/ ./internal/engine/ ./internal/domain/ ./internal/tui/`; `./test.sh race` before each chunk's final commit.

## File structure

| File | Responsibility |
|------|----------------|
| `internal/git/status_parse.go` (+test) | capture unmerged XY code |
| `internal/model/conflict.go` (+test) | `ConflictClass`, side/base helpers, `Conflicts()` |
| `internal/git/conflict.go` (+test) | `CheckoutSide`, `CheckoutBaseStage`, `RemoveFile`, `MergeContinue`, `RebaseContinue` |
| `internal/engine/gitops.go` | new verbs on the interface |
| `internal/engine/conflict.go` (+test) | `ResolveConflict`, `ContinueOp`, `AbortOp` |
| `internal/domain/conflict.go` (+test) | `InProgressOp` gated query |
| `internal/tui/conflict_popup.go` (+test) | the popup + per-file actions + continue/abort |
| `internal/tui/view.go`, `model.go`, `footer.go`, `help.go` | status notice, `x` dispatch, routing, hints |
| `CHANGELOG.md`, `README.md` | entries |

---

# CHUNK A — Detection model (Tasks 1–2)

## Task 1: Parser captures the unmerged conflict code

**Files:** Modify `internal/git/status_parse.go`; Test `internal/git/status_parse_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/git/status_parse_test.go`:

```go
func TestParseUnmergedCapturesCode(t *testing.T) {
	// Two unmerged entries: UU (both modified) and DU (deleted by us).
	data := []byte(
		"u UU N... 100644 100644 100644 100644 h1 h2 h3 uu.txt\x00" +
			"u DU N... 100644 000000 000000 100644 h1 h2 h3 md.txt\x00")
	st, err := ParseStatusV2(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Files) != 2 {
		t.Fatalf("want 2 files, got %d", len(st.Files))
	}
	uu := st.Files[0]
	if uu.Kind != model.KindUnmerged || uu.Staged != 'U' || uu.Unstaged != 'U' {
		t.Errorf("uu = %+v, want KindUnmerged U/U", uu)
	}
	md := st.Files[1]
	if md.Kind != model.KindUnmerged || md.Staged != 'D' || md.Unstaged != 'U' {
		t.Errorf("md = %+v, want KindUnmerged D/U", md)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/git/ -run TestParseUnmergedCapturesCode`
Expected: FAIL — `Staged`/`Unstaged` are 0 (the `u` branch drops the code).

- [ ] **Step 3: Capture the code in the parser**

In `internal/git/status_parse.go`, replace the `case 'u':` body:

```go
		case 'u':
			fields := strings.SplitN(tok, " ", 11)
			if len(fields) >= 11 {
				xy := fields[1]
				st.Files = append(st.Files, model.FileStatus{
					Path:     fields[10],
					Staged:   xy[0],
					Unstaged: xy[1],
					Kind:     model.KindUnmerged,
				})
			}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/git/ -run 'TestParseUnmerged|TestParseStatus'`
Expected: PASS. (Existing status tests still green — `Counts()` keys off `Kind`, not the bytes.)

- [ ] **Step 5: Commit**

```bash
git add internal/git/status_parse.go internal/git/status_parse_test.go
git commit -m "feat(git): capture the unmerged conflict code in status parse"
```

## Task 2: Conflict classification in the model

**Files:** Create `internal/model/conflict.go`, `internal/model/conflict_test.go`

- [ ] **Step 1: Write the failing test**

```go
package model

import "testing"

func uf(staged, unstaged byte) FileStatus {
	return FileStatus{Path: "f", Kind: KindUnmerged, Staged: staged, Unstaged: unstaged}
}

func TestConflictClass(t *testing.T) {
	cases := []struct {
		name              string
		f                 FileStatus
		wantClass         ConflictClass
		ours, theirs, base bool
	}{
		{"UU both", uf('U', 'U'), ConflictBothSides, true, true, true},
		{"AA both-added", uf('A', 'A'), ConflictBothSides, true, true, false},
		{"DU deleted-by-us", uf('D', 'U'), ConflictModifyDelete, false, true, true},
		{"UD deleted-by-them", uf('U', 'D'), ConflictModifyDelete, true, false, true},
		{"AU added-by-us", uf('A', 'U'), ConflictModifyDelete, true, false, false},
		{"DD both-deleted", uf('D', 'D'), ConflictModifyDelete, false, false, true},
	}
	for _, c := range cases {
		if got := c.f.ConflictClass(); got != c.wantClass {
			t.Errorf("%s: class = %v, want %v", c.name, got, c.wantClass)
		}
		if c.f.ConflictHasOurs() != c.ours || c.f.ConflictHasTheirs() != c.theirs || c.f.ConflictHasBase() != c.base {
			t.Errorf("%s: ours/theirs/base = %v/%v/%v, want %v/%v/%v", c.name,
				c.f.ConflictHasOurs(), c.f.ConflictHasTheirs(), c.f.ConflictHasBase(), c.ours, c.theirs, c.base)
		}
	}
}

func TestConflictsHelper(t *testing.T) {
	st := WorkingTreeStatus{Files: []FileStatus{
		{Path: "a", Kind: KindTracked, Unstaged: 'M'},
		uf('U', 'U'),
		{Path: "u", Kind: KindUntracked},
	}}
	if c := st.Conflicts(); len(c) != 1 || c[0].Staged != 'U' {
		t.Fatalf("Conflicts() = %+v, want the single unmerged file", c)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/model/ -run Conflict`
Expected: FAIL — undefined `ConflictClass`, etc.

- [ ] **Step 3: Implement the classifier**

Create `internal/model/conflict.go`:

```go
package model

// ConflictClass groups an unmerged file by the resolution actions it supports.
type ConflictClass int

const (
	// ConflictBothSides: both sides hold content (UU, AA) — keep ours/theirs.
	ConflictBothSides ConflictClass = iota
	// ConflictModifyDelete: one side deleted/added the path (DU, UD, AU, UA, DD)
	// — keep the present side / delete / keep base.
	ConflictModifyDelete
)

// ConflictHasOurs reports whether stage 2 (our side) holds content: our side
// is not a delete. (XY code's first byte.)
func (f FileStatus) ConflictHasOurs() bool { return f.Staged != 'D' && f.Staged != 0 }

// ConflictHasTheirs reports whether stage 3 (their side) holds content.
func (f FileStatus) ConflictHasTheirs() bool { return f.Unstaged != 'D' && f.Unstaged != 0 }

// ConflictHasBase reports whether stage 1 (the common ancestor) exists: present
// unless either side added the path.
func (f FileStatus) ConflictHasBase() bool { return f.Staged != 'A' && f.Unstaged != 'A' }

// ConflictClass classifies the unmerged file. both-sides needs content on both.
func (f FileStatus) ConflictClass() ConflictClass {
	if f.ConflictHasOurs() && f.ConflictHasTheirs() {
		return ConflictBothSides
	}
	return ConflictModifyDelete
}

// ConflictLabel is a plain-language description for the resolution UI.
func (f FileStatus) ConflictLabel() string {
	switch {
	case f.ConflictClass() == ConflictBothSides:
		return "modified on both sides"
	case f.ConflictHasTheirs() && !f.ConflictHasOurs():
		return "deleted by us, modified by them"
	case f.ConflictHasOurs() && !f.ConflictHasTheirs():
		return "modified by us, deleted by them"
	default:
		return "deleted on both sides"
	}
}

// Conflicts returns the unmerged files (git's path order).
func (s WorkingTreeStatus) Conflicts() []FileStatus {
	var out []FileStatus
	for _, f := range s.Files {
		if f.Kind == KindUnmerged {
			out = append(out, f)
		}
	}
	return out
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/model/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/model/conflict.go internal/model/conflict_test.go
git commit -m "feat(model): classify unmerged files (both-sides vs modify-delete)"
```

---

# CHUNK B — Verbs + engine ops (Tasks 3–4)

## Task 3: git conflict verbs

**Files:** Create `internal/git/conflict.go`, `internal/git/conflict_test.go`; Modify `internal/engine/gitops.go`

- [ ] **Step 1: Write the failing argv + real-repo tests**

Create `internal/git/conflict_test.go`:

```go
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
)

func TestConflictVerbArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git checkout --theirs", gitexec.Result{})
	f.SetResponse("git checkout-index (base)", gitexec.Result{})
	f.SetResponse("git rm", gitexec.Result{})
	r := &Repo{Runner: f}
	_ = r.CheckoutSide(context.Background(), "p.txt", "theirs")
	_ = r.CheckoutBaseStage(context.Background(), "p.txt")
	_ = r.RemoveFile(context.Background(), "p.txt")
	want := [][]string{
		{"checkout", "--theirs", "--", "p.txt"},
		{"checkout-index", "--stage=1", "-f", "--", "p.txt"},
		{"rm", "--", "p.txt"},
	}
	for i, w := range want {
		if !reflect.DeepEqual(f.Calls[i].Argv, w) {
			t.Errorf("call %d argv = %v, want %v", i, f.Calls[i].Argv, w)
		}
	}
}

// conflictRepo builds a real repo with a UU and a DU conflict, returns its dir.
func conflictRepo(t *testing.T) (string, *Repo) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil && args[0] != "merge" {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) { os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644) }
	run("init", "-q", "-b", "main")
	write("uu.txt", "base\n")
	write("md.txt", "base\n")
	run("add", "-A")
	run("commit", "-qm", "base")
	run("checkout", "-q", "-b", "feature")
	write("uu.txt", "theirs\n")
	write("md.txt", "theirs-mod\n")
	run("add", "-A")
	run("commit", "-qm", "feature")
	run("checkout", "-q", "main")
	write("uu.txt", "ours\n")
	run("add", "-A")
	run("rm", "-q", "md.txt")
	run("commit", "-qm", "main")
	run("merge", "feature") // conflicts (exit 1) — tolerated above
	return dir, &Repo{Runner: gitexec.NewExecRunner("git", dir, observRing())}
}

func TestCheckoutSideAndBaseReal(t *testing.T) {
	dir, r := conflictRepo(t)
	ctx := context.Background()
	// keep theirs on the both-modified file
	if err := r.CheckoutSide(ctx, "uu.txt", "theirs"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "uu.txt")); string(b) != "theirs\n" {
		t.Errorf("uu.txt = %q, want theirs", b)
	}
	// keep base
	if err := r.CheckoutBaseStage(ctx, "uu.txt"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "uu.txt")); string(b) != "base\n" {
		t.Errorf("uu.txt = %q, want base", b)
	}
	// modify/delete: keep the present (theirs) side
	if err := r.CheckoutSide(ctx, "md.txt", "theirs"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "md.txt")); string(b) != "theirs-mod\n" {
		t.Errorf("md.txt = %q, want theirs-mod", b)
	}
}
```

> `observRing()`: match how other `internal/git` real-repo tests build the `ExecRunner` (grep a neighbor like `stage_test.go`/`merge_test.go` for the exact `gitexec.NewExecRunner(...)` args — it takes a `*observ.Ring`; reuse the same helper/import they use, or inline `observ.NewRing(50)`).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/git/ -run 'TestConflictVerbArgv|TestCheckoutSide'`
Expected: FAIL — verbs undefined.

- [ ] **Step 3: Implement the verbs**

Create `internal/git/conflict.go`:

```go
package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// CheckoutSide restores the "ours" (stage 2) or "theirs" (stage 3) version of a
// conflicted path into the working tree. Caller stages it to mark resolved.
func (r *Repo) CheckoutSide(ctx context.Context, path, side string) error {
	_, err := r.Runner.Run(ctx, "git checkout --"+side,
		gitcmd.New("checkout").Arg("--"+side, "--", path).ToArgv())
	return err
}

// CheckoutBaseStage restores the common-ancestor (stage 1) version of a
// conflicted path. Errors if there is no stage 1 (e.g. added-by-both).
func (r *Repo) CheckoutBaseStage(ctx context.Context, path string) error {
	_, err := r.Runner.Run(ctx, "git checkout-index (base)",
		gitcmd.New("checkout-index").Arg("--stage=1", "-f", "--", path).ToArgv())
	return err
}

// RemoveFile removes a path from the working tree and index (resolves a
// modify/delete conflict toward deletion).
func (r *Repo) RemoveFile(ctx context.Context, path string) error {
	_, err := r.Runner.Run(ctx, "git rm", gitcmd.New("rm").Arg("--", path).ToArgv())
	return err
}

// MergeContinue finalizes a merge after conflicts are resolved. GIT_EDITOR=true
// keeps it non-interactive (the prepared MERGE_MSG is used unchanged).
func (r *Repo) MergeContinue(ctx context.Context, dir string) error {
	b := gitcmd.New("merge").Arg("--continue")
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.RunEnv(ctx, "git merge --continue", b.ToArgv(), []string{"GIT_EDITOR=true"})
	return err
}

// RebaseContinue resumes a rebase after conflicts are resolved, non-interactive.
func (r *Repo) RebaseContinue(ctx context.Context, dir string) error {
	b := gitcmd.New("rebase").Arg("--continue")
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.RunEnv(ctx, "git rebase --continue", b.ToArgv(), []string{"GIT_EDITOR=true"})
	return err
}
```

- [ ] **Step 4: Add the verbs to GitOps**

In `internal/engine/gitops.go`, after the existing `Merge`/`Rebase` verb groups, add:

```go
	CheckoutSide(ctx context.Context, path, side string) error
	CheckoutBaseStage(ctx context.Context, path string) error
	RemoveFile(ctx context.Context, path string) error
	MergeContinue(ctx context.Context, dir string) error
	RebaseContinue(ctx context.Context, dir string) error
```

(The `StagePaths` verb the ops also use is already on the interface.)

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/git/ -run Conflict && go build ./...`
Expected: PASS, build OK (`var _ GitOps = (*git.Repo)(nil)` satisfied).

- [ ] **Step 6: Commit**

```bash
git add internal/git/conflict.go internal/git/conflict_test.go internal/engine/gitops.go
git commit -m "feat(git): conflict-resolution verbs (checkout side/base, rm, continue)"
```

## Task 4: engine ResolveConflict + continue/abort ops

**Files:** Create `internal/engine/conflict.go`, `internal/engine/conflict_test.go`

- [ ] **Step 1: Write the failing test**

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConflictKeepTheirs(t *testing.T) {
	dir, repo := newConflictRepo(t) // see note
	ctx := context.Background()
	_, err := ResolveConflict{Path: "uu.txt", Action: KeepTheirs}.Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "uu.txt")); string(b) != "theirs\n" {
		t.Errorf("uu.txt = %q, want theirs", b)
	}
	st, _ := repo.Status(ctx)
	for _, f := range st.Files {
		if f.Path == "uu.txt" && f.Kind.String() == "" { // still unmerged?
		}
	}
	if len(st.Conflicts()) != 1 { // only md.txt remains
		t.Errorf("want 1 remaining conflict, got %d", len(st.Conflicts()))
	}
}

func TestResolveConflictDelete(t *testing.T) {
	dir, repo := newConflictRepo(t)
	ctx := context.Background()
	if _, err := (ResolveConflict{Path: "md.txt", Action: DeleteFile}).Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "md.txt")); !os.IsNotExist(err) {
		t.Error("md.txt should be deleted")
	}
}

func TestAbortOpClearsConflict(t *testing.T) {
	_, repo := newConflictRepo(t)
	ctx := context.Background()
	if _, err := (AbortOp{}).Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)}); err != nil {
		t.Fatal(err)
	}
	st, _ := repo.Status(ctx)
	if len(st.Conflicts()) != 0 {
		t.Errorf("abort should clear conflicts, got %d", len(st.Conflicts()))
	}
}
```

> `newConflictRepo(t) (string, *git.Repo)`: reuse the engine package's `newRepo` git-runner construction but build the two-conflict tree (copy the sequence from `conflictRepo` in Task 3's test, returning the engine package's `*git.Repo`). If `Kind.String()` doesn't exist, drop that dead inner loop (it was only illustrative) — the `len(st.Conflicts())` assertion is the real check.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/engine/ -run 'TestResolveConflict|TestAbortOp'`
Expected: FAIL — ops undefined.

- [ ] **Step 3: Implement the ops**

Create `internal/engine/conflict.go`:

```go
package engine

import (
	"context"
	"errors"
)

// ConflictAction is a whole-file resolution choice.
type ConflictAction int

const (
	KeepOurs ConflictAction = iota
	KeepTheirs
	MarkResolved
	DeleteFile
	KeepBase
)

// ResolveConflict resolves one conflicted file at the whole-file level, then
// stages it (or removes it) so the unmerged index entry clears.
type ResolveConflict struct {
	Path   string
	Action ConflictAction
}

func (op ResolveConflict) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "resolving", Detail: op.Path})
	var summary string
	switch op.Action {
	case KeepOurs:
		if err := deps.Repo.CheckoutSide(ctx, op.Path, "ours"); err != nil {
			return Result{}, err
		}
		if err := deps.Repo.StagePaths(ctx, []string{op.Path}); err != nil {
			return Result{}, err
		}
		summary = "resolved " + op.Path + " (kept ours)"
	case KeepTheirs:
		if err := deps.Repo.CheckoutSide(ctx, op.Path, "theirs"); err != nil {
			return Result{}, err
		}
		if err := deps.Repo.StagePaths(ctx, []string{op.Path}); err != nil {
			return Result{}, err
		}
		summary = "resolved " + op.Path + " (kept theirs)"
	case KeepBase:
		if err := deps.Repo.CheckoutBaseStage(ctx, op.Path); err != nil {
			return Result{}, err
		}
		if err := deps.Repo.StagePaths(ctx, []string{op.Path}); err != nil {
			return Result{}, err
		}
		summary = "resolved " + op.Path + " (kept base)"
	case DeleteFile:
		if err := deps.Repo.RemoveFile(ctx, op.Path); err != nil {
			return Result{}, err
		}
		summary = "resolved " + op.Path + " (deleted)"
	case MarkResolved:
		if err := deps.Repo.StagePaths(ctx, []string{op.Path}); err != nil {
			return Result{}, err
		}
		summary = "marked " + op.Path + " resolved"
	default:
		return Result{}, errors.New("unknown conflict action")
	}
	res := Result{Summary: summary, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// MarkAllResolved stages every given path (the user edited them by hand).
type MarkAllResolved struct{ Paths []string }

func (op MarkAllResolved) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "resolving", Detail: "all"})
	if err := deps.Repo.StagePaths(ctx, op.Paths); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "marked all resolved", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// ContinueOp finalizes whichever of merge/rebase is in progress.
type ContinueOp struct{}

func (op ContinueOp) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "continuing", Detail: ""})
	if ok, _ := deps.Repo.MergeInProgress(ctx, ""); ok {
		if err := deps.Repo.MergeContinue(ctx, ""); err != nil {
			return Result{}, err
		}
		return done(deps, ctx, "merge continued")
	}
	if ok, _ := deps.Repo.RebaseInProgress(ctx, ""); ok {
		if err := deps.Repo.RebaseContinue(ctx, ""); err != nil {
			return Result{}, err
		}
		return done(deps, ctx, "rebase continued")
	}
	return Result{}, errors.New("no merge or rebase in progress")
}

// AbortOp aborts whichever of merge/rebase is in progress.
type AbortOp struct{}

func (op AbortOp) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "aborting", Detail: ""})
	if ok, _ := deps.Repo.MergeInProgress(ctx, ""); ok {
		if err := deps.Repo.MergeAbort(ctx, ""); err != nil {
			return Result{}, err
		}
		return done(deps, ctx, "merge aborted")
	}
	if ok, _ := deps.Repo.RebaseInProgress(ctx, ""); ok {
		if err := deps.Repo.RebaseAbort(ctx, ""); err != nil {
			return Result{}, err
		}
		return done(deps, ctx, "rebase aborted")
	}
	return Result{}, errors.New("no merge or rebase in progress")
}

func done(deps OpDeps, ctx context.Context, summary string) (Result, error) {
	res := Result{Summary: summary, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var (
	_ Operation = ResolveConflict{}
	_ Operation = MarkAllResolved{}
	_ Operation = ContinueOp{}
	_ Operation = AbortOp{}
)
```

> `MergeAbort`/`RebaseAbort`/`MergeInProgress`/`RebaseInProgress` are already on `GitOps`. If a `done` helper name collides with an existing one in the package, rename to `conflictDone`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/engine/ -run 'Conflict|AbortOp'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/conflict.go internal/engine/conflict_test.go
git commit -m "feat(engine): ResolveConflict + continue/abort ops"
```

---

# CHUNK C — TUI notice + popup (Tasks 5–8)

## Task 5: Status-bar notice + `x` opens the popup

**Files:** Create `internal/tui/conflict_popup.go`, `internal/tui/conflict_popup_test.go`; Modify `internal/tui/model.go` (field, `x` dispatch, routing), `internal/tui/view.go` (status notice + overlay)

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/conflict_popup_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func conflictModel() Model {
	m := Model{width: 120, height: 30, focus: panelStatus, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Branch: "zzz", Files: []model.FileStatus{
		{Path: "uu.txt", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'},
		{Path: "md.txt", Kind: model.KindUnmerged, Staged: 'D', Unstaged: 'U'},
	}}
	return m
}

func TestStatusBarShowsConflictNotice(t *testing.T) {
	m := conflictModel()
	out := m.View()
	if !strings.Contains(out, "2 conflicts") || !strings.Contains(out, "[x]") {
		t.Errorf("status bar should announce conflicts:\n%s", out)
	}
}

func TestXOpensConflictPopup(t *testing.T) {
	m := conflictModel()
	mm, _ := m.Update(keyMsg("x"))
	if mm.(Model).conflictPopup == nil {
		t.Fatal("x should open the conflict popup when conflicts exist")
	}
}

func TestXNoOpWithoutConflicts(t *testing.T) {
	m := Model{width: 120, height: 30, sel: map[panel]int{}}
	mm, _ := m.Update(keyMsg("x"))
	if mm.(Model).conflictPopup != nil {
		t.Fatal("x must do nothing when there are no conflicts")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'ConflictNotice|XOpens|XNoOp'`
Expected: FAIL — `conflictPopup` undefined; no notice.

- [ ] **Step 3: Popup skeleton + open**

Create `internal/tui/conflict_popup.go`:

```go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

// conflictPopup resolves unmerged files at the whole-file level.
type conflictPopup struct {
	files      []model.FileStatus // refreshed from status after each action
	sel        int
	inProgress string // "merge"/"rebase"/"" — gates the continue/abort actions
}

func (m Model) openConflictPopup() (Model, tea.Cmd) {
	files := m.status.Conflicts()
	if len(files) == 0 {
		return m, nil
	}
	m.conflictPopup = &conflictPopup{files: files}
	return m, m.loadInProgressCmd()
}

func (m Model) renderConflictPopup() string {
	p := m.conflictPopup
	var b strings.Builder
	b.WriteString("Resolve conflicts\n\n")
	if len(p.files) == 0 {
		b.WriteString("  (all resolved)\n")
	}
	for i, f := range p.files {
		cursor := "  "
		if i == p.sel {
			cursor = "> "
		}
		row := fmt.Sprintf("%s%s  — %s", cursor, f.Path, f.ConflictLabel())
		if i == p.sel {
			b.WriteString(selectedRow.Render(row) + "\n")
		} else {
			b.WriteString(row + "\n")
		}
	}
	b.WriteString("\n" + p.actionHint() + "\n")
	b.WriteString("[esc] close")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}

// actionHint lists the keys available for the selected file (+ continue/abort).
func (p *conflictPopup) actionHint() string {
	var parts []string
	if p.sel >= 0 && p.sel < len(p.files) {
		f := p.files[p.sel]
		if f.ConflictClass() == model.ConflictBothSides {
			parts = append(parts, "[o] ours", "[t] theirs", "[m] mark resolved")
		} else {
			parts = append(parts, "[k] keep modified", "[d] delete")
			if f.ConflictHasBase() {
				parts = append(parts, "[b] keep base")
			}
		}
	}
	parts = append(parts, "[A] all resolved")
	if len(p.files) == 0 && p.inProgress != "" {
		parts = []string{"[c] continue " + p.inProgress, "[a] abort"}
	} else if p.inProgress != "" {
		parts = append(parts, "[a] abort")
	}
	return strings.Join(parts, "  ")
}
```

Add the field to `Model` (near the stash popups): `conflictPopup *conflictPopup`.

In `internal/tui/model.go`, add the `x` arm in the normal-key switch:

```go
		case "x":
			if m.opsIdle() && len(m.status.Conflicts()) > 0 {
				return m.openConflictPopup()
			}
```

Add to the popup routing chain (before the stash handlers):

```go
		if m.conflictPopup != nil {
			return m.updateConflictPopupKey(msg)
		}
```

The `loadInProgressCmd` + `updateConflictPopupKey` land in Tasks 6–7; for now add minimal stubs so it compiles and the open test passes:

```go
// loadInProgressCmd probes whether a merge/rebase is in progress (Task 7 fills
// the handler). Stub returns nil until then.
func (m Model) loadInProgressCmd() tea.Cmd { return nil }

func (m Model) updateConflictPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if msg.String() == "esc" {
		m.conflictPopup = nil
	}
	return m, nil
}
```

- [ ] **Step 4: Status-bar notice + overlay wiring in `view.go`**

In `render()`, add the popup overlay branch alongside the others (e.g. before the stash one):

```go
	if m.conflictPopup != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderConflictPopup(), w, h)
	}
```

Add `&& m.conflictPopup == nil` to the tooltip-suppression guard (the long `if m.popup == nil && …` line).

In the status-line assembly (the `statusLine := m.statusMsg` … block), prepend the conflict notice:

```go
	statusLine := m.statusMsg
	if n := len(m.status.Conflicts()); n > 0 {
		notice := fmt.Sprintf("⚠ %d conflict", n)
		if n != 1 {
			notice += "s"
		}
		notice += " — press [x] to resolve"
		if statusLine != "" {
			statusLine = notice + " · " + statusLine
		} else {
			statusLine = notice
		}
	}
```

(Add `"fmt"` to `view.go` imports if not present.)

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/tui/ -run 'ConflictNotice|XOpens|XNoOp' && go build ./...`
Expected: PASS, build OK.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/conflict_popup.go internal/tui/conflict_popup_test.go internal/tui/model.go internal/tui/view.go
git commit -m "feat(tui): conflict status-bar notice + x opens the resolution popup"
```

## Task 6: Per-file resolution actions

**Files:** Modify `internal/tui/conflict_popup.go`, `internal/tui/conflict_popup_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestConflictPopupKeepTheirsDispatches(t *testing.T) {
	m := conflictModel()
	mm, _ := m.Update(keyMsg("x"))
	m = mm.(Model)
	m.sel = map[panel]int{}
	// select uu.txt (index 0, both-sides) and keep theirs
	mm, cmd := m.updateConflictPopupKey(keyMsg("t"))
	got := mm.(Model)
	if !got.running || cmd == nil {
		t.Fatal("t should dispatch a ResolveConflict op")
	}
	if got.conflictPopup != nil {
		t.Error("popup should close while the op runs (reopens on refresh)")
	}
}

func TestConflictPopupModifyDeleteKeys(t *testing.T) {
	m := conflictModel()
	mm, _ := m.Update(keyMsg("x"))
	m = mm.(Model)
	m.conflictPopup.sel = 1 // md.txt — modify/delete (DU)
	// 'o' (ours) is NOT a valid key here; 'k' keep-modified IS.
	mm, _ = m.updateConflictPopupKey(keyMsg("o"))
	if mm.(Model).running {
		t.Error("ours must be inert on a modify/delete file")
	}
	mm, cmd := m.updateConflictPopupKey(keyMsg("k"))
	if !mm.(Model).running || cmd == nil {
		t.Error("k (keep modified) should dispatch on a modify/delete file")
	}
}

func TestConflictPopupOpMapping(t *testing.T) {
	// keep-modified on a DU file resolves to KeepTheirs (the present side).
	du := model.FileStatus{Path: "md.txt", Kind: model.KindUnmerged, Staged: 'D', Unstaged: 'U'}
	if got := keepModifiedAction(du); got != engineKeepTheirs {
		t.Errorf("DU keep-modified = %v, want KeepTheirs", got)
	}
	ud := model.FileStatus{Path: "x", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'D'}
	if got := keepModifiedAction(ud); got != engineKeepOurs {
		t.Errorf("UD keep-modified = %v, want KeepOurs", got)
	}
}
```

> `engineKeepTheirs`/`engineKeepOurs` are aliases for `engine.KeepTheirs`/`engine.KeepOurs` used to keep the test readable; use `engine.KeepTheirs` directly if you prefer (import `engine`).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run ConflictPopup`
Expected: FAIL — action keys inert; `keepModifiedAction` undefined.

- [ ] **Step 3: Implement the action handler**

Replace `updateConflictPopupKey` in `internal/tui/conflict_popup.go` and add the mapping helper:

```go
// keepModifiedAction maps a modify/delete file to the side that has content.
func keepModifiedAction(f model.FileStatus) engine.ConflictAction {
	if f.ConflictHasTheirs() {
		return engine.KeepTheirs
	}
	return engine.KeepOurs
}

func (m Model) updateConflictPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.conflictPopup
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case "esc":
		m.conflictPopup = nil
		return m, nil
	case "up", "k2unused": // placeholder removed below
	}
	// movement
	switch msg.String() {
	case "up":
		if p.sel > 0 {
			p.sel--
		}
		return m, nil
	case "down", "j":
		if p.sel < len(p.files)-1 {
			p.sel++
		}
		return m, nil
	case "A":
		var paths []string
		for _, f := range p.files {
			paths = append(paths, f.Path)
		}
		if len(paths) == 0 {
			return m, nil
		}
		m.conflictPopup = nil
		return m.startOp(engine.MarkAllResolved{Paths: paths})
	}
	if p.sel < 0 || p.sel >= len(p.files) {
		return m, nil
	}
	f := p.files[p.sel]
	both := f.ConflictClass() == model.ConflictBothSides
	var action engine.ConflictAction
	switch msg.String() {
	case "o":
		if !both {
			return m, nil
		}
		action = engine.KeepOurs
	case "t":
		if !both {
			return m, nil
		}
		action = engine.KeepTheirs
	case "m":
		if !both {
			return m, nil
		}
		action = engine.MarkResolved
	case "k":
		if both {
			return m, nil
		}
		action = keepModifiedAction(f)
	case "d":
		if both {
			return m, nil
		}
		action = engine.DeleteFile
	case "b":
		if both || !f.ConflictHasBase() {
			return m, nil
		}
		action = engine.KeepBase
	default:
		return m, nil
	}
	m.conflictPopup = nil // reopened after the refresh (Task 7 re-syncs the list)
	return m.startOp(engine.ResolveConflict{Path: f.Path, Action: action})
}
```

> Delete the stray first `switch` with the `k2unused` placeholder — it's only there to show the vim `k`-vs-keep-modified collision: **`k` means keep-modified, not up**, so up is `↑` only and `j` is down only (no `k` for movement in this popup). Keep the single clean `switch` above.

Add `"github.com/gigagit/gg/internal/engine"` to the file's imports.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/tui/ -run ConflictPopup`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/conflict_popup.go internal/tui/conflict_popup_test.go
git commit -m "feat(tui): per-file whole-file conflict resolution keys"
```

## Task 7: Reopen-on-refresh, in-progress probe, continue/abort

**Files:** Create `internal/domain/conflict.go`, `internal/domain/conflict_test.go`; Modify `internal/tui/conflict_popup.go`, `internal/tui/model.go` (reopen after op, in-progress msg)

- [ ] **Step 1: Domain in-progress query + test**

Create `internal/domain/conflict.go`:

```go
package domain

import "context"

// InProgressOp reports "merge", "rebase", or "" for the current working tree.
func (s *Service) InProgressOp(ctx context.Context) (string, error) {
	return query(ctx, s, "inprogress", func(ctx context.Context) (string, error) {
		if ok, err := s.repo.MergeInProgress(ctx, ""); err == nil && ok {
			return "merge", nil
		}
		if ok, err := s.repo.RebaseInProgress(ctx, ""); err == nil && ok {
			return "rebase", nil
		}
		return "", nil
	})
}
```

Test `internal/domain/conflict_test.go`:

```go
package domain

import (
	"context"
	"testing"
)

func TestInProgressOpNoneWhenClean(t *testing.T) {
	s := newTestService(t) // see note
	got, err := s.InProgressOp(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("clean repo InProgressOp = %q, want \"\"", got)
	}
}
```

> `newTestService(t)`: match how `internal/domain` tests build a `*Service` over a real temp repo (grep an existing `_test.go` in the package for the constructor — likely `New(&git.Repo{...})` or an `open`/helper). Reuse it. A clean repo asserts the "" path; a merge-in-progress case is already covered by the engine `ContinueOp`/`AbortOp` tests.

- [ ] **Step 2: Wire the in-progress probe into the popup**

In `internal/tui/conflict_popup.go`, replace the `loadInProgressCmd` stub and add the message + handler:

```go
type inProgressMsg struct{ op string }

func (m Model) loadInProgressCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		op, _ := svc.InProgressOp(context.Background())
		return inProgressMsg{op: op}
	}
}
```

Add `"context"` to the file imports. In `internal/tui/model.go` `Update`, handle the message (near the other popup messages):

```go
	case inProgressMsg:
		if m.conflictPopup != nil {
			m.conflictPopup.inProgress = msg.op
		}
		return m, nil
```

- [ ] **Step 3: Reopen the popup after a resolution op + continue/abort keys**

The resolution ops close the popup and full-reload. After the reload, reopen the popup with the refreshed conflict list (so the resolved file drops off and the user keeps going). In `model.go`'s `opFinishedMsg` success path, after `m.loadGen++`, add a reopen flag. Simplest: track that the conflict popup was open when the op started.

Add a `Model` field `conflictResuming bool`. In `updateConflictPopupKey`, before each `m.startOp(...)` for a resolution/mark-all, set `m.conflictResuming = true`. In the `opFinishedMsg` handler, after the reload decision, if `m.conflictResuming` is set:

```go
	if m.conflictResuming {
		m.conflictResuming = false
		if len(m.status.Conflicts()) > 0 || m.inProgressOpActive() {
			// reopen with the freshly-reloaded status on the next dataLoadedMsg
			m.reopenConflict = true
		}
	}
```

This is getting indirect because the conflict list comes from the *next* `dataLoadedMsg`, not synchronously. **Use this cleaner approach instead:** after `dataLoadedMsg` applies the new `m.status`, if `m.reopenConflict` is set, rebuild the popup from `m.status.Conflicts()` and re-probe in-progress:

In `model.go`, replace the indirection with two concrete edits.

(a) In `updateConflictPopupKey`, set `m.reopenConflict = true` right before every `m.startOp(...)` (the resolution keys and `A`). Add `reopenConflict bool` to `Model`.

(b) In the `dataLoadedMsg` handler (after `m.status = msg.status`), add:

```go
		if m.reopenConflict {
			m.reopenConflict = false
			if c := m.status.Conflicts(); len(c) > 0 {
				m.conflictPopup = &conflictPopup{files: c}
				return m, m.loadInProgressCmd()
			}
			// all resolved: if an op is in progress, keep a tiny popup to offer
			// continue/abort; otherwise drop it (user commits with c).
			m.conflictPopup = &conflictPopup{files: nil}
			return m, m.loadInProgressCmd()
		}
```

(c) Add the `[c]`/`[a]` keys to `updateConflictPopupKey` (before the per-file switch):

```go
	case "c":
		if p.inProgress != "" && len(p.files) == 0 {
			m.conflictPopup = nil
			return m.startOp(engine.ContinueOp{})
		}
		return m, nil
	case "a":
		if p.inProgress != "" {
			m.conflictPopup = nil
			return m.startOp(engine.AbortOp{})
		}
		return m, nil
```

(`continue` is allowed only when no conflicts remain; abort any time an op is in progress. After either, the reload clears the conflicted state and the popup is not reopened because `reopenConflict` was not set for these.)

- [ ] **Step 4: Write the reopen test**

```go
func TestConflictPopupReopensAfterResolve(t *testing.T) {
	m := conflictModel()
	m.reopenConflict = true
	// Simulate the post-op reload arriving with one conflict already cleared.
	one := model.WorkingTreeStatus{Branch: "zzz", Files: []model.FileStatus{
		{Path: "md.txt", Kind: model.KindUnmerged, Staged: 'D', Unstaged: 'U'},
	}}
	mm, cmd := m.Update(dataLoadedMsg{gen: m.loadGen, status: one})
	got := mm.(Model)
	if got.conflictPopup == nil || len(got.conflictPopup.files) != 1 {
		t.Fatalf("popup should reopen with the remaining conflict, got %+v", got.conflictPopup)
	}
	_ = cmd
}
```

> Match `dataLoadedMsg`'s required fields; `gen` must equal `m.loadGen` or the message is dropped as stale. If the handler needs other non-nil fields to not panic, set them (the real handler guards most with `msg.err == nil`).

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/domain/ ./internal/tui/ -run 'InProgress|Conflict'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/conflict.go internal/domain/conflict_test.go internal/tui/conflict_popup.go internal/tui/model.go
git commit -m "feat(tui): reopen conflict popup after resolve; continue/abort"
```

## Task 8: Footer, help, docs, merge gate

**Files:** Modify `internal/tui/footer.go`, `internal/tui/help.go`, `CHANGELOG.md`, `README.md`

- [ ] **Step 1: Footer + help**

In `footer.go` `globalBindings`, add (gated on conflicts):

```go
	{"x", "[x] resolve", func(m Model) bool { return m.opsIdle() && len(m.status.Conflicts()) > 0 }},
```

In `help.go`, add a section:

```go
		h("Conflicts (x)"),
		r("↑/↓ j", "move between conflicted files"),
		r("o/t", "keep ours / theirs (both-modified files)"),
		r("k/d/b", "keep modified / delete / keep base (modify-delete files)"),
		r("m", "mark resolved (after editing by hand)"),
		r("A", "mark all resolved"),
		r("c/a", "continue / abort the merge or rebase"),
		r("esc", "close"),
```

Ensure `TestHelpFooterCoverage` stays green (the footer `x` key now has the `h("Conflicts (x)")` row whose key column starts with `↑/↓` — add an explicit `r("x", "resolve conflicts")` row if the guard needs the literal `x` key present; check the test).

- [ ] **Step 2: CHANGELOG + README**

CHANGELOG `### Added`:
```
- TUI: conflicted repos show a status-bar notice (`⚠ N conflict — press [x]`);
  `x` opens a resolution popup with whole-file actions (keep ours/theirs/base,
  keep-modified, delete, mark-resolved, mark-all) and continue/abort for an
  in-progress merge or rebase. Partial/hunk resolution is a later feature.
```
README: add `x` to the keybinding table (resolve conflicts) and a short note.

- [ ] **Step 3: Full verification**

Run: `./test.sh race`
Expected: PASS (vet+gofmt → unit → e2e).

- [ ] **Step 4: Manual smoke (the real test repo)**

```bash
go build -o /tmp/gg ./cmd/gg
cd /mnt/t/others/test-1 && /tmp/gg
```
Confirm: the status bar shows `⚠ 1 conflict — press [x]`; `x` opens the popup listing `timing3.log — deleted by us, modified by them` with `[k] keep modified  [d] delete  [b] keep base`; resolving clears it and the notice disappears.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/footer.go internal/tui/help.go CHANGELOG.md README.md
git commit -m "docs(tui): advertise x=resolve conflicts in footer/help + CHANGELOG/README"
```

- [ ] **Step 6: Finish the branch**

Use `superpowers:finishing-a-development-branch`.

---

## Self-Review

**Spec coverage:**
- Status-driven detection (not MERGE_HEAD) → Task 1 parser + `Conflicts()` (Task 2); the notice/popup read `status.Conflicts()`. ✓
- Capture conflict type → Task 1; classifier + present-side/base helpers → Task 2. ✓
- Status-bar notice → Task 5. ✓
- Popup with context-aware whole-file actions (ours/theirs/mark; keep-modified/delete/keep-base) → Task 6; key→action mapping incl. present-side for keep-modified → `keepModifiedAction`. ✓
- Keep-base hidden when no base stage → `actionHint` + the `b` guard (`ConflictHasBase`). ✓
- Mark-all-resolved → Task 6 (`A`). ✓
- Continue/abort when a merge/rebase is in progress → Task 4 ops + Task 7 keys + `InProgressOp` probe. ✓
- Verbs/semantics table → Task 3 (verbs) + Task 4 (op action→verb sequence). ✓
- Reopen-after-resolve so the user works through the list → Task 7. ✓
- Out-of-scope (partial editing, 3-way view, CLI) → not built. ✓

**Placeholder scan:** No TBD/TODO. The two "match the existing helper" notes (`observRing`, `newTestService`) name a concrete exemplar to copy and an exact import fallback — they resolve a constructor name, not logic. The `k2unused`/dead-loop illustrations are explicitly flagged for deletion in their steps.

**Type consistency:** `engine.ConflictAction` (`KeepOurs`/`KeepTheirs`/`MarkResolved`/`DeleteFile`/`KeepBase`) used identically in Task 4 op and Task 6 popup. `ResolveConflict{Path, Action}`, `MarkAllResolved{Paths}`, `ContinueOp{}`, `AbortOp{}` consistent. `FileStatus.ConflictClass/ConflictHasOurs/HasTheirs/HasBase/ConflictLabel`, `WorkingTreeStatus.Conflicts()`, `model.ConflictBothSides/ConflictModifyDelete` consistent across model/tui. `conflictPopup{files, sel, inProgress}`, `m.reopenConflict`, `m.conflictPopup`, `inProgressMsg`, `loadInProgressCmd` consistent across Tasks 5–7. Verbs `CheckoutSide(path, side)`, `CheckoutBaseStage(path)`, `RemoveFile(path)`, `MergeContinue(dir)`, `RebaseContinue(dir)` match git→GitOps→engine.
