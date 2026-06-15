# Conflict Source Attribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (or subagent-driven-development) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Name the source of a conflict — "merging X into Y" / "rebasing X onto Y" — in the status-bar notice and the resolve popup.

**Architecture:** New `*git.Repo` read verbs resolve the merge/rebase parties; `domain.Snapshot` carries a `ConflictState` computed only when conflicts exist; the TUI threads it into the status bar and popup. Presentation only — no resolution-behavior change.

**Tech Stack:** Go 1.26, `gitcmd` builder, `gitexec` (`Run`/`FakeRunner`), real `git` in `t.TempDir()`, Bubble Tea value-receiver Model.

**Spec:** `docs/superpowers/specs/2026-06-15-conflict-source-design.md`.

---

## Orientation

- Verbs land in `internal/git/conflict.go` (already holds the resolution verbs).
  Merge: `name-rev --name-only --refs=refs/heads/* MERGE_HEAD`. Rebase: read
  `<git-dir>/rebase-merge/{head-name,onto}`; git-dir via `rev-parse
  --absolute-git-dir`. `name-rev` an `onto` SHA the same way.
- `domain.loadSnapshot` (`internal/domain/query.go`) runs reads in parallel then
  `wg.Wait()`. Compute conflict state AFTER the wait, gated on
  `snap.Status.Counts().Conflicted > 0`.
- `model.Counts().Conflicted` already counts `KindUnmerged` files.
- TUI load: `dataLoadedMsg` (`internal/tui/load.go`) → applied in
  `model.go` `case dataLoadedMsg` `if msg.err == nil`. Status bar phrase in
  `view.go` (`statusLine := m.statusMsg` block, the conflict-notice `if`).
  Popup header in `conflict_popup.go` `renderConflictPopup`.

**Run tests:** `go test ./internal/git/ ./internal/domain/ ./internal/tui/`;
`./test.sh race` before the final commit.

---

## Task 1: git read verbs for the conflict parties

**Files:** Modify `internal/git/conflict.go`; Test `internal/git/conflict_test.go`

- [ ] **Step 1: Failing tests** — append to `internal/git/conflict_test.go`:

```go
func TestMergeHeadNameReal(t *testing.T) {
	_, r := conflictRepo(t) // builds a merge-conflict repo (feature into main)
	name, err := r.MergeHeadName(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "feature" {
		t.Errorf("MergeHeadName = %q, want feature", name)
	}
}

func TestRebasePartiesReal(t *testing.T) {
	dir := t.TempDir()
	gitRun := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil && args[0] != "rebase" {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	w := func(name, c string) { os.WriteFile(filepath.Join(dir, name), []byte(c), 0o644) }
	gitRun("init", "-q", "-b", "main")
	w("f.txt", "base\n")
	gitRun("add", "-A")
	gitRun("commit", "-qm", "base")
	gitRun("checkout", "-q", "-b", "feature")
	w("f.txt", "theirs\n")
	gitRun("add", "-A")
	gitRun("commit", "-qm", "feature")
	gitRun("checkout", "-q", "main")
	w("f.txt", "ours\n")
	gitRun("add", "-A")
	gitRun("commit", "-qm", "main")
	gitRun("checkout", "-q", "feature")
	gitRun("rebase", "main") // conflicts (exit 1) — tolerated above
	r := &Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
	branch, onto, err := r.RebaseParties(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feature" || onto != "main" {
		t.Errorf("RebaseParties = (%q,%q), want (feature,main)", branch, onto)
	}
}
```

- [ ] **Step 2: Run — expect FAIL** (`go test ./internal/git/ -run 'MergeHeadName|RebaseParties'`): verbs undefined.

- [ ] **Step 3: Implement** — append to `internal/git/conflict.go` (add imports `os`, `path/filepath`, `strings`):

```go
// MergeHeadName resolves MERGE_HEAD to a local branch name (e.g. "feature"),
// for attributing a merge conflict. "" when it cannot be named.
func (r *Repo) MergeHeadName(ctx context.Context, dir string) (string, error) {
	b := gitcmd.New("name-rev").Arg("--name-only", "--refs=refs/heads/*", "MERGE_HEAD")
	if dir != "" {
		b = b.Dir(dir)
	}
	res, err := r.Runner.Run(ctx, "git name-rev MERGE_HEAD", b.ToArgv())
	if err != nil {
		return "", err
	}
	return cleanRefName(strings.TrimSpace(res.Stdout)), nil
}

// RebaseParties returns the branch being rebased and the branch it is being
// rebased onto, read from the rebase-merge state. Empty strings (no error) when
// the state is absent or uses a backend we don't model.
func (r *Repo) RebaseParties(ctx context.Context, dir string) (branch, onto string, err error) {
	b := gitcmd.New("rev-parse").Arg("--absolute-git-dir")
	if dir != "" {
		b = b.Dir(dir)
	}
	res, err := r.Runner.Run(ctx, "git rev-parse --absolute-git-dir", b.ToArgv())
	if err != nil {
		return "", "", err
	}
	gitDir := strings.TrimSpace(res.Stdout)
	headName, herr := os.ReadFile(filepath.Join(gitDir, "rebase-merge", "head-name"))
	if herr != nil {
		return "", "", nil // no merge-backend rebase state
	}
	branch = cleanRefName(strings.TrimSpace(string(headName)))
	if ontoSHA, oerr := os.ReadFile(filepath.Join(gitDir, "rebase-merge", "onto")); oerr == nil {
		nb := gitcmd.New("name-rev").Arg("--name-only", "--refs=refs/heads/*", strings.TrimSpace(string(ontoSHA)))
		if dir != "" {
			nb = nb.Dir(dir)
		}
		if nr, nerr := r.Runner.Run(ctx, "git name-rev (onto)", nb.ToArgv()); nerr == nil {
			onto = cleanRefName(strings.TrimSpace(nr.Stdout))
		}
	}
	return branch, onto, nil
}

// cleanRefName strips a refs/heads/ prefix and drops a name-rev suffix
// (e.g. "feature~2" → "feature"; an undecorated SHA is returned unchanged).
func cleanRefName(s string) string {
	s = strings.TrimPrefix(s, "refs/heads/")
	if i := strings.IndexAny(s, "~^"); i >= 0 {
		s = s[:i]
	}
	return s
}
```

- [ ] **Step 4: Run — expect PASS** (`go test ./internal/git/ -run 'MergeHeadName|RebaseParties'`).

- [ ] **Step 5: Commit** — `git add internal/git/conflict.go internal/git/conflict_test.go && git commit -m "feat(git): resolve merge/rebase conflict parties (name verbs)"`

---

## Task 2: domain ConflictState + Snapshot.Conflict

**Files:** Modify `internal/domain/conflict.go`, `internal/domain/query.go`; Test `internal/domain/conflict_test.go`

- [ ] **Step 1: Failing test** — append to `internal/domain/conflict_test.go` (reuse the merge-conflict repo helper pattern; build one inline like Task 1's rebase repo but stop after a conflicting `git merge feature`):

```go
func TestConflictStateMerge(t *testing.T) {
	dir := mergeConflictDir(t) // helper: builds feature-into-main merge conflict
	svc := New(&git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))})
	st, err := svc.repo.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cs := svc.conflictState(context.Background(), st)
	if cs.Op != "merge" || cs.Source != "feature" || cs.Target != "main" {
		t.Fatalf("conflictState = %+v, want {merge feature main}", cs)
	}
	if got := cs.Describe(); got != "merging feature into main" {
		t.Errorf("Describe = %q", got)
	}
}

func TestConflictStateCleanIsZero(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir) // helper: init + one commit (no conflict)
	svc := New(&git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))})
	st, _ := svc.repo.Status(context.Background())
	if cs := svc.conflictState(context.Background(), st); cs.Op != "" || cs.Describe() != "" {
		t.Errorf("clean conflictState = %+v, want zero", cs)
	}
}
```

Add `mergeConflictDir(t)` and `gitInit(t,dir)` helpers in the test file (mirror
Task 1's git-run scaffolding; `mergeConflictDir` ends with a conflicting
`git merge feature` whose non-zero exit is tolerated).

- [ ] **Step 2: Run — expect FAIL** (`go test ./internal/domain/ -run ConflictState`): `conflictState`/`ConflictState` undefined.

- [ ] **Step 3: Implement** — append to `internal/domain/conflict.go`:

```go
// ConflictState attributes the current conflict to the operation that produced
// it. Zero value (Op == "") means no in-progress merge/rebase (e.g. a stash-pop
// conflict) — no source to show.
type ConflictState struct {
	Op     string // "merge" | "rebase" | ""
	Source string // branch being merged / rebased
	Target string // branch merged-into / rebased-onto
}

// Describe renders the human phrase, or "" when there is nothing to attribute.
func (c ConflictState) Describe() string {
	switch {
	case c.Op == "merge" && c.Source != "" && c.Target != "":
		return "merging " + c.Source + " into " + c.Target
	case c.Op == "rebase" && c.Source != "" && c.Target != "":
		return "rebasing " + c.Source + " onto " + c.Target
	}
	return ""
}

// conflictState attributes st's conflicts to a merge/rebase in progress. It runs
// git probes only when st actually has unmerged files.
func (s *Service) conflictState(ctx context.Context, st model.WorkingTreeStatus) ConflictState {
	if st.Counts().Conflicted == 0 {
		return ConflictState{}
	}
	if ok, err := s.repo.MergeInProgress(ctx, ""); err == nil && ok {
		src, _ := s.repo.MergeHeadName(ctx, "")
		return ConflictState{Op: "merge", Source: src, Target: st.Branch}
	}
	if ok, err := s.repo.RebaseInProgress(ctx, ""); err == nil && ok {
		branch, onto, _ := s.repo.RebaseParties(ctx, "")
		return ConflictState{Op: "rebase", Source: branch, Target: onto}
	}
	return ConflictState{}
}
```

(`model` is already imported in the package; if not, add it.)

- [ ] **Step 4: Surface on Snapshot** — in `internal/domain/query.go`:
  - add `Conflict ConflictState` to the `Snapshot` struct (after `HeadTimes`);
  - in `loadSnapshot`, after `wg.Wait()` and the `firstErr` check, before
    `return snap, nil`:

```go
	snap.Conflict = s.conflictState(ctx, snap.Status)
	return snap, nil
```

- [ ] **Step 5: Snapshot test** — append to `internal/domain/conflict_test.go`:

```go
func TestSnapshotCarriesConflictSource(t *testing.T) {
	dir := mergeConflictDir(t)
	snap, err := New(&git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Conflict.Describe() != "merging feature into main" {
		t.Errorf("snapshot conflict = %+v", snap.Conflict)
	}
}
```

- [ ] **Step 6: Run — expect PASS** (`go test ./internal/domain/ -run 'ConflictState|SnapshotCarries'`).

- [ ] **Step 7: Commit** — `git add internal/domain/conflict.go internal/domain/conflict_test.go internal/domain/query.go && git commit -m "feat(domain): attribute conflicts to the merge/rebase in progress"`

---

## Task 3: TUI status-bar phrase + popup subtitle

**Files:** Modify `internal/tui/load.go`, `internal/tui/model.go`, `internal/tui/view.go`, `internal/tui/conflict_popup.go`; Test `internal/tui/conflict_popup_test.go`

- [ ] **Step 1: Failing tests** — append to `internal/tui/conflict_popup_test.go`:

```go
func conflictModelWithSource() Model {
	m := conflictModel()
	m.conflict = domain.ConflictState{Op: "merge", Source: "feature", Target: "main"}
	return m
}

func TestStatusBarShowsConflictSource(t *testing.T) {
	m := conflictModelWithSource()
	out := m.View()
	if !strings.Contains(out, "merging feature into main") {
		t.Errorf("status bar should name the source:\n%s", out)
	}
}

func TestConflictPopupShowsSourceSubtitle(t *testing.T) {
	m := conflictModelWithSource()
	mm, _ := m.Update(keyMsg("x"))
	m = mm.(Model)
	out := m.View()
	if !strings.Contains(out, "merging feature into main") {
		t.Errorf("popup should show the source subtitle:\n%s", out)
	}
}
```

(`domain` is already imported in `conflict_popup_test.go`.)

- [ ] **Step 2: Run — expect FAIL** (`go test ./internal/tui/ -run 'ConflictSource|SourceSubtitle'`): `m.conflict` undefined.

- [ ] **Step 3: Model field** — in `internal/tui/model.go`, next to `reopenConflict`:

```go
	conflict domain.ConflictState // source of the current conflict (for the notice/popup)
```

In the `case dataLoadedMsg` `if msg.err == nil` block, set it (near `m.status = msg.status`):

```go
			m.conflict = msg.conflict
```

- [ ] **Step 4: Carry on the message** — in `internal/tui/load.go`:
  - add `conflict domain.ConflictState` to `dataLoadedMsg`;
  - in `loadCmd`, set `out.conflict = snap.Conflict` where `out` is assembled.

- [ ] **Step 5: Status bar** — in `internal/tui/view.go`, the conflict-notice `if`:

```go
	if n := len(m.status.Conflicts()); n > 0 {
		notice := fmt.Sprintf("⚠ %d conflict", n)
		if n != 1 {
			notice += "s"
		}
		if src := m.conflict.Describe(); src != "" {
			notice += " " + src
		}
		notice += " — press [x] to resolve"
		...
	}
```

- [ ] **Step 6: Popup subtitle** — in `internal/tui/conflict_popup.go` `renderConflictPopup`, after the `"Resolve conflicts\n\n"` header:

```go
	if src := m.conflict.Describe(); src != "" {
		b.WriteString(dimStyle.Render(src) + "\n\n")
	}
```

Use the package's existing dim style (grep for one, e.g. `dimStyle`/`subtleStyle`/
`helpStyle`); if none fits, plain `b.WriteString(src + "\n\n")`.

- [ ] **Step 7: Run — expect PASS** (`go test ./internal/tui/ -run 'ConflictSource|SourceSubtitle'`) and `go build ./...`.

- [ ] **Step 8: Commit** — `git add internal/tui/load.go internal/tui/model.go internal/tui/view.go internal/tui/conflict_popup.go internal/tui/conflict_popup_test.go && git commit -m "feat(tui): show conflict source in the status notice and popup"`

---

## Task 4: docs + full gate

**Files:** `CHANGELOG.md`, `README.md`

- [ ] **Step 1: CHANGELOG** — under `### Added`, note the conflict notice now
  names its source ("merging X into Y" / "rebasing X onto Y").

- [ ] **Step 2: README** — extend the `x` keybinding row to mention the source
  phrase in the notice/popup.

- [ ] **Step 3: Full gate** — `./test.sh race` (expect all green).

- [ ] **Step 4: Manual smoke** — build, reproduce a merge conflict, confirm the
  status bar reads `⚠ 1 conflict merging <branch> into <branch> — press [x]`.

- [ ] **Step 5: Commit** — `git add CHANGELOG.md README.md && git commit -m "docs: conflict source attribution"`

- [ ] **Step 6: Finish** — `superpowers:finishing-a-development-branch`.

---

## Self-Review

- Spec coverage: merge naming (T1 `MergeHeadName` + T2 merge branch), rebase
  naming (T1 `RebaseParties` + T2), no-op/stash case (gated `conflictState` → zero
  → `Describe()==""` → bare notice), status bar (T3 S5), popup subtitle (T3 S6),
  always-available via Snapshot (T2 S4). ✓
- No placeholders: helper names (`mergeConflictDir`, `gitInit`, `dimStyle`) flag a
  concrete pattern to mirror/grep. ✓
- Type consistency: `domain.ConflictState{Op,Source,Target}` + `Describe()` used
  identically across domain/tui; `Snapshot.Conflict`, `dataLoadedMsg.conflict`,
  `m.conflict` consistent; verbs `MergeHeadName(dir)`, `RebaseParties(dir)`
  match git→domain. ✓
