# Context ops Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add three context operations — copy branch name, rename branch (`git branch -m`), and rename/reword a commit — to the TUI `.` menu and the CLI.

**Architecture:** Each op threads through the existing layers: new git verbs (`internal/git`, one invocation each, added to `engine.GitOps`) → engine ops (`internal/engine`) run via `domain.Execute` → CLI (`internal/cli`) + TUI (`internal/tui`). Copy-branch-name is render-only (reuses `internal/clipboard` via `contextCopyRows`). Reword reuses `engine.InteractiveRebase`'s internals rather than re-implementing rebase.

**Tech Stack:** Go 1.26, Bubble Tea TUI, `gitcmd` argv builder, `gitexec.FakeRunner` for argv tests, real `git` in `t.TempDir()` for engine/CLI/e2e tests.

## Global Constraints

- Module `github.com/gigagit/gg`; Go 1.26.
- **A git verb is one invocation** — build argv with `gitcmd.New(...)`, run via `r.Runner.Run`. Never shell out directly.
- Every new verb an op uses **must be added to the `engine.GitOps` interface** (`internal/engine/gitops.go`).
- Frontends (`internal/tui`, `internal/cli`) **never import `internal/git`** — reach git via `domain` queries / `engine` ops (archtest-guarded).
- Ops run via `domain.Execute`; declare `LockMode()` only when needing less than the default `TreeWrite`.
- Every new TUI keybinding lands in **both** `help.go` (the `?` pane) **and** the context footer (`footer.go`).
- TDD: failing test → run it red → minimal code → run green → commit. Frequent commits.
- When the CLI surface changes: bump `agentskill.Version`, update `internal/agentskill/using-gg.md`, run `gg init --update`, update `README.md` + `CHANGELOG.md`.
- Work happens on branch `worktree-context-ops` (already created, based on local `main` tip `6e05dd7`).

---

## File map

| File | Responsibility | Tasks |
|------|----------------|-------|
| `internal/tui/action_menu.go` | `contextCopyRows` branch/remote cases; reword + rename-branch menu rows | 1, 5, 10 |
| `internal/git/mutate.go` | `RenameBranch` verb | 2 |
| `internal/git/query2.go` | `RevParse`, `CommitMessage` verbs | 6 |
| `internal/engine/gitops.go` | interface additions | 2, 6 |
| `internal/engine/rename_branch.go` (new) | `RenameBranch` op | 3 |
| `internal/engine/reword.go` (new) | `Reword` op | 7 |
| `internal/cli/branch.go` | `gg branch rename` | 4 |
| `internal/cli/cli.go` | route `gg commit reword` | 9 |
| `internal/cli/commit_reword.go` (new) | `cmdCommitReword` | 9 |
| `internal/tui/rename_branch_popup.go` (new) | rename-branch input popup | 5 |
| `internal/tui/reword_popup.go` (new) | reword message popup + wiring | 10 |
| `internal/tui/model.go` | popup fields + key routing | 5, 10 |
| `internal/domain/query.go` | `CommitMessage` read query | 8 |
| `internal/tui/footer.go`, `help.go` | hints | 5, 10 |
| `e2e/scenarios/*.toml` | branch rename + commit reword | 11 |

---

## Task 1: Copy branch name (TUI `.` menu)

**Files:**
- Modify: `internal/tui/action_menu.go` (`contextCopyRows`)
- Modify: `internal/tui/help.go`
- Test: `internal/tui/action_menu_copyrows_test.go`

**Interfaces:**
- Consumes: existing `m.copyRow(id, label, okMsg, text)`, `m.backingIndex(panel)`, `m.branches`, `m.remoteBranchList`.
- Produces: copy rows on the Branches/Remotes panels.

- [ ] **Step 1: Write the failing test**

Look at the existing tests in `action_menu_copyrows_test.go` for the model-construction helper (e.g. a `newTestModel`/fixture that sets `m.focus`, `m.branches`, and selection). Mirror it. Add:

```go
func TestContextCopyRowsBranchName(t *testing.T) {
	m := newCopyRowsModel(t) // existing helper used by sibling tests
	m.focus = panelBranches
	m.branches = []model.Branch{{Name: "feature/x"}}
	// ensure the panel selection points at row 0 (mirror how sibling tests seed selection)
	rows := m.contextCopyRows()
	if len(rows) != 1 || rows[0].id != "copy-branch-name" || rows[0].copyText != "feature/x" {
		t.Fatalf("want one copy-branch-name row for feature/x, got %+v", rows)
	}
}

func TestContextCopyRowsRemoteName(t *testing.T) {
	m := newCopyRowsModel(t)
	m.focus = panelRemotes
	m.remoteBranchList = []model.RemoteBranch{{Name: "origin/foo", Remote: "origin", Branch: "foo"}}
	rows := m.contextCopyRows()
	if len(rows) != 1 || rows[0].copyText != "origin/foo" {
		t.Fatalf("want copy row for origin/foo, got %+v", rows)
	}
}
```

If `newCopyRowsModel` is not the helper name, use whatever the file's other tests use; the assertion logic is what matters. Confirm the field names `m.branches` (`[]model.Branch`) and `m.remoteBranchList` (`[]model.RemoteBranch`) against `model.go` and adjust selection seeding to match sibling tests.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestContextCopyRows(BranchName|RemoteName)' -v`
Expected: FAIL (no rows returned for these panels yet).

- [ ] **Step 3: Add the panel cases**

In `internal/tui/action_menu.go`, inside `contextCopyRows`, extend the final `switch` (the one with `case m.focus == panelCommits:` / `case m.isFilesPanel(m.focus):`) by adding:

```go
	case m.focus == panelBranches:
		if bi, ok := m.backingIndex(panelBranches); ok {
			name := m.branches[bi].Name
			return []actionRow{m.copyRow("copy-branch-name", "Copy branch name", "Copied branch name "+name, name)}
		}
	case m.focus == panelRemotes:
		if bi, ok := m.backingIndex(panelRemotes); ok {
			name := m.remoteBranchList[bi].Name
			return []actionRow{m.copyRow("copy-branch-name", "Copy branch name", "Copied branch name "+name, name)}
		}
```

Verify `m.backingIndex(panelRemotes)` exists and indexes `m.remoteBranchList`; if remotes uses a differently named slice/indexer, use that (grep `remoteRows`/`remoteBranchList` in the package).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestContextCopyRows(BranchName|RemoteName)' -v`
Expected: PASS

- [ ] **Step 5: Document + commit**

In `help.go`, under the Branches and Remotes panel help, add a line: `.  copy branch name`. Then:

```bash
go test ./internal/tui/
git add internal/tui/action_menu.go internal/tui/help.go internal/tui/action_menu_copyrows_test.go
git commit -m "feat(tui): . menu Copy branch name on Branches/Remotes panels"
```

---

## Task 2: `RenameBranch` git verb

**Files:**
- Modify: `internal/git/mutate.go`
- Modify: `internal/engine/gitops.go`
- Test: `internal/git/mutate_test.go`

**Interfaces:**
- Produces: `func (r *Repo) RenameBranch(ctx context.Context, old, new string) error` running `git branch -m <old> <new>`.

- [ ] **Step 1: Write the failing argv test**

In `mutate_test.go`, mirror the existing `FakeRunner` argv-assertion tests (find one, e.g. for `CreateBranch`). Add:

```go
func TestRenameBranchArgv(t *testing.T) {
	fr := &gitexec.FakeRunner{}
	r := &Repo{Runner: fr} // match how sibling tests construct Repo
	if err := r.RenameBranch(context.Background(), "old", "new"); err != nil {
		t.Fatal(err)
	}
	fr.AssertArgv(t, []string{"branch", "-m", "old", "new"}) // use the file's existing assertion helper
}
```

Use the exact `FakeRunner` construction + assertion idiom the neighbouring tests use (the helper may be `fr.Last().Argv` or similar — match it).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestRenameBranchArgv -v`
Expected: FAIL (`RenameBranch` undefined).

- [ ] **Step 3: Implement the verb**

In `mutate.go`, mirroring `CreateBranch` (line ~38):

```go
// RenameBranch renames local branch old to new (git branch -m). git refuses
// when new already exists; renaming a branch checked out in another worktree
// succeeds and updates that worktree's HEAD.
func (r *Repo) RenameBranch(ctx context.Context, old, new string) error {
	argv := gitcmd.New("branch").Arg("-m", old, new).ToArgv()
	_, err := r.Runner.Run(ctx, "git branch -m", argv)
	return err
}
```

Add to the `GitOps` interface in `gitops.go`, next to `CreateBranch`:

```go
	RenameBranch(ctx context.Context, old, new string) error
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/git/ ./internal/engine/ -run TestRenameBranchArgv`
Then `go build ./...` (confirms `*git.Repo` still satisfies `GitOps`).
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/git/mutate.go internal/git/mutate_test.go internal/engine/gitops.go
git commit -m "feat(git): RenameBranch verb (git branch -m)"
```

---

## Task 3: `engine.RenameBranch` op

**Files:**
- Create: `internal/engine/rename_branch.go`
- Test: `internal/engine/rename_branch_test.go`

**Interfaces:**
- Consumes: `deps.Repo.CheckRefFormatBranch`, `deps.Repo.RenameBranch`, `deps.emit`.
- Produces: `type RenameBranch struct { Old, New string }` implementing `Operation`.

- [ ] **Step 1: Write the failing test**

Mirror `create_branch`-style engine tests (real temp repo; find the `newRepo`/`gitIn` helpers already used in the engine test package). Add:

```go
func TestRewameBranchOp(t *testing.T) {} // placeholder to avoid name clash — delete
```

Then the real tests:

```go
func TestRenameBranchOpSuccess(t *testing.T) {
	repo, deps := newEngineRepo(t)          // use the package's existing helper
	gitIn(t, repo.Dir(), "branch", "old")   // create a branch to rename
	res, err := RenameBranch{Old: "old", New: "renamed"}.Run(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Fatalf("want Changed")
	}
	if exists := branchExists(t, repo.Dir(), "renamed"); !exists {
		t.Fatalf("renamed branch missing")
	}
}

func TestRenameBranchOpExistingTarget(t *testing.T) {
	repo, deps := newEngineRepo(t)
	gitIn(t, repo.Dir(), "branch", "old")
	gitIn(t, repo.Dir(), "branch", "taken")
	if _, err := (RenameBranch{Old: "old", New: "taken"}).Run(context.Background(), deps); err == nil {
		t.Fatalf("want error renaming onto an existing branch")
	}
}

func TestRenameBranchOpInvalidName(t *testing.T) {
	repo, deps := newEngineRepo(t)
	gitIn(t, repo.Dir(), "branch", "old")
	if _, err := (RenameBranch{Old: "old", New: "bad name"}).Run(context.Background(), deps); err == nil {
		t.Fatalf("want validation error for illegal name")
	}
}
```

Match the package's actual helpers (`newEngineRepo`, `gitIn`, `branchExists`) — grep `create_branch_test.go` / `delete_branch_test.go` for their real names and reuse verbatim. Do **not** redeclare `gitIn` (it already exists in the package — known gotcha).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestRenameBranchOp -v`
Expected: FAIL (`RenameBranch` op undefined).

- [ ] **Step 3: Implement the op**

```go
package engine

import (
	"context"
	"fmt"
)

// RenameBranch renames a local branch (git branch -m). Mirrors CreateBranch:
// the new name is format-validated up front; git refuses an existing target.
type RenameBranch struct {
	Old string // required
	New string // required
}

func (op RenameBranch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Old == "" || op.New == "" {
		return Result{}, fmt.Errorf("rename branch: Old and New are required")
	}
	if err := deps.Repo.CheckRefFormatBranch(ctx, op.New); err != nil {
		return Result{}, fmt.Errorf("rename branch: invalid branch name %q: %w", op.New, err)
	}
	deps.emit(ctx, Progress{Step: "renaming branch", Detail: op.Old + " → " + op.New})
	if err := deps.Repo.RenameBranch(ctx, op.Old, op.New); err != nil {
		return Result{}, fmt.Errorf("rename branch: %w", err)
	}
	res := Result{Summary: "renamed branch " + op.Old + " → " + op.New, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = RenameBranch{}
```

(Default `TreeWrite` lock — no `LockMode`, matching `CreateBranch`/`DeleteBranch`.) Delete the placeholder test stub from Step 1.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run TestRenameBranchOp -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/engine/rename_branch.go internal/engine/rename_branch_test.go
git commit -m "feat(engine): RenameBranch op"
```

---

## Task 4: `gg branch rename` CLI

**Files:**
- Modify: `internal/cli/branch.go`
- Test: `internal/cli/branch_test.go`

**Interfaces:**
- Consumes: `engine.RenameBranch`, `runOperation`, `finish`, `cliDecider{}`.
- Produces: `gg branch rename <old> <new>`.

- [ ] **Step 1: Write the failing test**

Mirror the existing branch CLI tests (real clone/repo via the package helper). Add:

```go
func TestBranchRenameCLI(t *testing.T) {
	svc, dir := newCLIService(t)        // package helper used by sibling CLI tests
	gitIn(t, dir, "branch", "old")
	code := cmdBranch(svc, []string{"rename", "old", "renamed"}, nil, io.Discard, io.Discard)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !branchExists(t, dir, "renamed") {
		t.Fatalf("branch not renamed")
	}
}

func TestBranchRenameUsage(t *testing.T) {
	svc, _ := newCLIService(t)
	if code := cmdBranch(svc, []string{"rename", "only-one"}, nil, io.Discard, io.Discard); code != 2 {
		t.Fatalf("want usage exit 2, got %d", code)
	}
}
```

Use the package's real helpers; match `cmdBranch`'s signature `(svc, args, stdin, stdout, stderr)`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestBranchRename -v`
Expected: FAIL (`rename` unknown subcommand → exit 2 for the happy-path test).

- [ ] **Step 3: Implement**

In `branch.go`, add to the `cmdBranch` switch and update the usage strings:

```go
	case "rename":
		return cmdBranchRename(svc, args[1:], stdout, stderr)
```

Change the two usage strings from `<create|delete>` / `(use create or delete)` to include `rename`. Add:

```go
// cmdBranchRename implements `gg branch rename <old> <new>`.
func cmdBranchRename(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[0] == "" || args[1] == "" {
		fmt.Fprintln(stderr, "usage: gg branch rename <old> <new>")
		return 2
	}
	res, err := runOperation(context.Background(), svc,
		engine.RenameBranch{Old: args[0], New: args[1]}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/cli/ -run TestBranchRename -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/branch.go internal/cli/branch_test.go
git commit -m "feat(cli): gg branch rename <old> <new>"
```

---

## Task 5: TUI rename-branch popup + `.` menu row

**Files:**
- Create: `internal/tui/rename_branch_popup.go`
- Modify: `internal/tui/model.go` (popup field + key routing), `internal/tui/action_menu.go` (menu row), `internal/tui/footer.go`, `internal/tui/help.go`
- Test: `internal/tui/rename_branch_popup_test.go`

**Interfaces:**
- Consumes: `m.backingIndex(panelBranches)`, `m.branches`, `m.startOp`, `engine.RenameBranch`.
- Produces: `m.renameBranchPopup *renameBranchPopup`, a `.`-menu row id `rename-branch`.

- [ ] **Step 1: Write the failing test**

```go
func TestRenameBranchPopupSubmit(t *testing.T) {
	m := newTestModel(t)             // package helper
	m.focus = panelBranches
	m.branches = []model.Branch{{Name: "old"}}
	m, ok := m.openRenameBranchPopup()
	if !ok || m.renameBranchPopup == nil {
		t.Fatalf("popup did not open")
	}
	if m.renameBranchPopup.name != "old" {
		t.Fatalf("want prefilled current name, got %q", m.renameBranchPopup.name)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestRenameBranchPopup -v`
Expected: FAIL (`openRenameBranchPopup`/field undefined).

- [ ] **Step 3: Implement the popup**

Create `rename_branch_popup.go`, modelled on `branch_popup.go`:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// renameBranchPopup holds the in-flight rename-branch dialog. The text field is
// pre-filled with the branch's current name.
type renameBranchPopup struct {
	old  string // the branch being renamed
	name string // typed new name
}

// openRenameBranchPopup opens the dialog for the selected Branches-panel row,
// prefilled with its current name. Returns (model, false) when no row selected.
func (m Model) openRenameBranchPopup() (Model, bool) {
	bi, ok := m.backingIndex(panelBranches)
	if !ok {
		return m, false
	}
	cur := m.branches[bi].Name
	m.renameBranchPopup = &renameBranchPopup{old: cur, name: cur}
	return m, true
}

// updateRenameBranchPopupKey handles one key while the popup is open.
func (m Model) updateRenameBranchPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.renameBranchPopup
	switch msg.Type {
	case tea.KeyEsc:
		m.renameBranchPopup = nil
	case tea.KeyEnter:
		if p.name == "" || p.name == p.old {
			m.renameBranchPopup = nil
			return m, nil
		}
		op := engine.RenameBranch{Old: p.old, New: p.name}
		m.renameBranchPopup = nil
		return m.startOp(op)
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(p.name); len(r) > 0 {
			p.name = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		// branch names cannot contain spaces — ignore
	case tea.KeyRunes:
		p.name += string(msg.Runes)
	}
	return m, nil
}

// renderRenameBranchPopup draws the rename dialog.
func (m Model) renderRenameBranchPopup() string {
	p := m.renameBranchPopup
	var b strings.Builder
	b.WriteString("Rename branch " + p.old + "\n\n")
	b.WriteString("name: " + p.name + "\n\n")
	b.WriteString("[type] name  [enter] rename  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
```

In `model.go`: add the field `renameBranchPopup *renameBranchPopup` next to `branchPopup`; in the key-routing section that dispatches to `updateBranchPopupKey` when `m.branchPopup != nil`, add a sibling branch routing to `updateRenameBranchPopupKey` when `m.renameBranchPopup != nil`; in the render/overlay section that renders `branchPopup`, add the `renameBranchPopup` case. (Grep `branchPopup` across `model.go`/`view.go` and mirror every site.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestRenameBranchPopup -v`
Expected: PASS

- [ ] **Step 5: Wire the `.` menu row + footer + help**

In `action_menu.go` `availableActions`, add a Branches-panel row (mirror how `remotePruneRow` / shelf rows are appended, with a `run` handler):

```go
	if m.focus == panelBranches {
		if _, ok := m.backingIndex(panelBranches); ok {
			out = append(out, actionRow{
				id:    "rename-branch",
				label: "Rename branch",
				scope: scopeWindow,
				run: func(m Model) (Model, tea.Cmd) {
					m, _ = m.openRenameBranchPopup()
					return m, nil
				},
			})
		}
	}
```

Match the actual `actionRow` field names + `run` signature used by existing menu-only rows (grep `remotePruneRow` in `remote_actions.go`). Add a footer entry in `footer.go` (`scopeWindow`, gated on `m.focus == panelBranches`) and a help line in `help.go` under the Branches panel.

- [ ] **Step 6: Run + commit**

```bash
go test ./internal/tui/
git add internal/tui/rename_branch_popup.go internal/tui/rename_branch_popup_test.go internal/tui/model.go internal/tui/action_menu.go internal/tui/footer.go internal/tui/help.go
git commit -m "feat(tui): . menu Rename branch + popup on Branches panel"
```

---

## Task 6: `RevParse` + `CommitMessage` git verbs

**Files:**
- Modify: `internal/git/query2.go` (or `query.go` — put both reads together)
- Modify: `internal/engine/gitops.go`
- Test: `internal/git/query2_test.go` (or the matching `_test.go`)

**Interfaces:**
- Produces:
  - `func (r *Repo) RevParse(ctx context.Context, rev string) (string, error)` → full SHA, error on bad rev (used for HEAD detection + root detection via `rev+"^"`).
  - `func (r *Repo) CommitMessage(ctx context.Context, rev string) (string, error)` → full `%B` message (reword pre-fill).

- [ ] **Step 1: Write failing tests (real temp repo)**

```go
func TestRevParseAndCommitMessage(t *testing.T) {
	r := newTestRepo(t)                      // package helper that inits a repo + 1 commit
	gitIn(t, r.Dir(), "commit", "--allow-empty", "-m", "second\n\nbody line")
	head, err := r.RevParse(context.Background(), "HEAD")
	if err != nil || len(head) < 7 {
		t.Fatalf("rev-parse HEAD: %v %q", err, head)
	}
	if _, err := r.RevParse(context.Background(), "HEAD~5"); err == nil {
		t.Fatalf("want error for out-of-range rev")
	}
	msg, err := r.CommitMessage(context.Background(), "HEAD")
	if err != nil || !strings.Contains(msg, "body line") {
		t.Fatalf("commit message: %v %q", err, msg)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestRevParseAndCommitMessage -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Implement**

```go
// RevParse resolves rev to a full object id (git rev-parse --verify). It errors
// on an unknown/out-of-range revision — callers use that to detect a root
// commit (RevParse(sha+"^") fails when sha has no parent).
func (r *Repo) RevParse(ctx context.Context, rev string) (string, error) {
	argv := gitcmd.New("rev-parse").Arg("--verify", "--quiet", rev).ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-parse", argv)
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		return "", fmt.Errorf("rev-parse: unknown revision %q", rev)
	}
	return out, nil
}

// CommitMessage returns rev's full commit message (subject + body).
func (r *Repo) CommitMessage(ctx context.Context, rev string) (string, error) {
	argv := gitcmd.New("log").Arg("-1", "--pretty=%B", rev).ToArgv()
	res, err := r.Runner.Run(ctx, "git log -1 --pretty=%B", argv)
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}
```

Note: `--quiet` makes `rev-parse --verify` exit non-zero (so `Run` returns err) on a bad rev; the empty-stdout guard is belt-and-suspenders. Add the import of `fmt`/`strings` if the file lacks them. Add both methods to `GitOps`:

```go
	RevParse(ctx context.Context, rev string) (string, error)
	CommitMessage(ctx context.Context, rev string) (string, error)
```

- [ ] **Step 4: Run test + build**

Run: `go test ./internal/git/ -run TestRevParseAndCommitMessage -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/git/query2.go internal/git/query2_test.go internal/engine/gitops.go
git commit -m "feat(git): RevParse + CommitMessage verbs"
```

---

## Task 7: `engine.Reword` op

**Files:**
- Create: `internal/engine/reword.go`
- Test: `internal/engine/reword_test.go`

**Interfaces:**
- Consumes: `deps.Repo.RevParse`, `deps.Repo.CurrentBranch`, `deps.Repo.IsDirty`, `deps.Repo.Commit`, `deps.Repo.LogRangeMessages`, `deps.Repo.HasMergeCommits`; reuses `InteractiveRebase.wrapped` (same package) + `writePlanFile`.
- Produces: `type Reword struct { Commit, NewMsg, GGBin string }` implementing `Operation`.

**Logic (decided in the spec + an empirically-driven refinement):**
- `target := RevParse(Commit)`, `head := RevParse("HEAD")`.
- **clean HEAD or root-as-HEAD** (`target == head && (!IsDirty || RevParse(Commit+"^") fails)`) → `Commit(NewMsg, all=false, amend=true)`. Cheap, message-only, and the only path that can reword a single-commit repo's root.
- else if `RevParse(Commit+"^")` **fails** → **refuse**: "reword: cannot reword the root commit (needs rebase -i --root)".
- else (has a parent — covers mid-branch commits AND a dirty HEAD) → build a 1-reword plan over `parent..currentBranch` and run it through `InteractiveRebase.wrapped`, which stash-wraps the dirty tree (restoring the staged/unstaged split) for free. Refuse first if the range has merge commits (mirrors `InteractiveRebase`).

- [ ] **Step 1: Write the failing tests (real temp repo, ≥3 commits)**

```go
func TestRewordHeadAmend(t *testing.T) {
	repo, deps := newEngineRepo(t)
	gitIn(t, repo.Dir(), "commit", "--allow-empty", "-m", "top")
	head, _ := deps.Repo.RevParse(context.Background(), "HEAD")
	res, err := Reword{Commit: head, NewMsg: "top reworded", GGBin: ggBinForTest(t)}.Run(context.Background(), deps)
	if err != nil || !res.Changed {
		t.Fatalf("reword HEAD: %v %+v", err, res)
	}
	if msg, _ := deps.Repo.CommitMessage(context.Background(), "HEAD"); !strings.Contains(msg, "top reworded") {
		t.Fatalf("HEAD message not changed: %q", msg)
	}
}

func TestRewordMidBranchPreservesLater(t *testing.T) {
	repo, deps := newEngineRepo(t) // assume helper leaves 1 commit "c1"
	for _, m := range []string{"c2", "c3"} {
		gitIn(t, repo.Dir(), "commit", "--allow-empty", "-m", m)
	}
	mid, _ := deps.Repo.RevParse(context.Background(), "HEAD~1") // c2
	_, err := Reword{Commit: mid, NewMsg: "c2 reworded", GGBin: ggBinForTest(t)}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("reword mid: %v", err)
	}
	logOut := gitOut(t, repo.Dir(), "log", "--format=%s", "-3")
	if !strings.Contains(logOut, "c2 reworded") || !strings.Contains(logOut, "c3") {
		t.Fatalf("reworded mid commit or dropped later commit: %q", logOut)
	}
}

func TestRewordNonHeadRootRefused(t *testing.T) {
	repo, deps := newEngineRepo(t) // ≥2 commits: root + more
	gitIn(t, repo.Dir(), "commit", "--allow-empty", "-m", "c2")
	root := gitOut(t, repo.Dir(), "rev-list", "--max-parents=0", "HEAD")
	root = strings.TrimSpace(root)
	if _, err := (Reword{Commit: root, NewMsg: "x", GGBin: ggBinForTest(t)}).Run(context.Background(), deps); err == nil {
		t.Fatalf("want refusal rewording a non-HEAD root commit")
	}
}
```

`ggBinForTest` must return a path to a built `gg` binary that handles `__rebase-seq` — the interactive-rebase engine tests already solve this; reuse their helper (grep `interactive_rebase_test.go` for how it obtains `GGBin`, likely `go build`-ing into `t.TempDir()` once). The HEAD-amend test does NOT need a real GGBin (no rebase), but pass one for uniformity.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run TestReword -v`
Expected: FAIL (`Reword` undefined).

- [ ] **Step 3: Implement the op**

```go
package engine

import (
	"context"
	"fmt"
	"os"

	"github.com/gigagit/gg/internal/rebaseplan"
)

// Reword changes a single commit's message. HEAD (clean) is a cheap
// `git commit --amend`; a mid-branch commit replays its branch onto the
// commit's parent with a one-entry reword plan, reusing InteractiveRebase's
// stash-wrapped internals. The root commit of a multi-commit repo is refused
// (it needs `rebase -i --root`, unsupported here).
type Reword struct {
	Commit string // commit to reword (any rev)
	NewMsg string // the new full message
	GGBin  string // path to the gg binary (rebase sequence editor); needed only for the rebase path
}

var _ Operation = Reword{}

func (op Reword) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Commit == "" || op.NewMsg == "" {
		return Result{}, fmt.Errorf("reword: Commit and NewMsg are required")
	}
	target, err := deps.Repo.RevParse(ctx, op.Commit)
	if err != nil {
		return Result{}, fmt.Errorf("reword: %w", err)
	}
	head, err := deps.Repo.RevParse(ctx, "HEAD")
	if err != nil {
		return Result{}, err
	}
	parent, parentErr := deps.Repo.RevParse(ctx, target+"^") // err ⇒ root commit

	// Cheap path: rewording the tip with a clean tree (or a single-commit repo
	// whose root IS HEAD) is a message-only amend — no rebase, no stash.
	if target == head {
		dirty, derr := deps.Repo.IsDirty(ctx)
		if derr != nil {
			return Result{}, derr
		}
		if !dirty || parentErr != nil {
			deps.emit(ctx, Progress{Step: "rewording", Detail: short(target)})
			if err := deps.Repo.Commit(ctx, op.NewMsg, false, true); err != nil {
				return Result{}, fmt.Errorf("reword (amend): %w", err)
			}
			res := Result{Summary: "reworded " + short(target), Changed: true}
			deps.emit(ctx, Done{Result: res})
			return res, nil
		}
	}

	if parentErr != nil {
		return Result{}, fmt.Errorf("reword: cannot reword the root commit (needs rebase -i --root, unsupported)")
	}

	cur, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	if cur == "" {
		return Result{}, fmt.Errorf("reword: detached HEAD — check out a branch first")
	}
	if op.GGBin == "" {
		return Result{}, fmt.Errorf("reword: GGBin is required for a non-HEAD reword")
	}
	hasMerges, err := deps.Repo.HasMergeCommits(ctx, "", parent, cur)
	if err != nil {
		return Result{}, err
	}
	if hasMerges {
		return Result{}, fmt.Errorf("reword: range %s..%s contains merge commits (not supported)", short(parent), cur)
	}

	commits, err := deps.Repo.LogRangeMessages(ctx, parent, cur)
	if err != nil {
		return Result{}, err
	}
	entries := make([]rebaseplan.Entry, 0, len(commits))
	for _, c := range commits { // oldest-first, exactly git todo order
		e := rebaseplan.Entry{Sha: c.Hash, Action: rebaseplan.Pick, Orig: c.Message}
		if c.Hash == target {
			e.Action = rebaseplan.Reword
			e.NewMsg = op.NewMsg
		}
		entries = append(entries, e)
	}
	plan := rebaseplan.Plan{Entries: entries}

	planPath, err := writePlanFile(plan)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(planPath) // a pure reword cannot conflict, so it never pauses
	env := []string{"GIT_SEQUENCE_EDITOR=" + op.GGBin + " __rebase-seq " + planPath}

	deps.emit(ctx, Progress{Step: "rewording", Detail: short(target)})
	ir := InteractiveRebase{Branch: cur, Onto: parent, Plan: plan, GGBin: op.GGBin}
	res, _, rerr := ir.wrapped(ctx, deps, "", env)
	if rerr != nil {
		return res, rerr
	}
	res = Result{Summary: "reworded " + short(target), Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

If a `short(sha string) string` helper doesn't already exist in the engine package, add a tiny one (`if len(s) > 7 { return s[:7] }; return s`) or reuse the TUI's pattern — grep first. Confirm `InteractiveRebase.wrapped`'s signature matches `(ctx, deps, switchTo, env) (Result, bool, error)` (it does, per interactive_rebase.go:110).

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/engine/ -run TestReword -v`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/reword.go internal/engine/reword_test.go
git commit -m "feat(engine): Reword op (amend HEAD / rebase mid-branch; refuse root)"
```

---

## Task 8: `svc.CommitMessage` domain read query

**Files:**
- Modify: `internal/domain/query.go`
- Test: `internal/domain/query_test.go`

**Interfaces:**
- Produces: `func (s *Service) CommitMessage(ctx context.Context, rev string) (string, error)` (Read reservation), used by the TUI to pre-fill the reword popup.

- [ ] **Step 1: Write the failing test**

Mirror an existing single-read query test (e.g. `CurrentBranch` at query.go:196). Add:

```go
func TestServiceCommitMessage(t *testing.T) {
	svc, dir := newDomainService(t)      // package helper
	gitIn(t, dir, "commit", "--allow-empty", "-m", "hello\n\nbody")
	msg, err := svc.CommitMessage(context.Background(), "HEAD")
	if err != nil || !strings.Contains(msg, "body") {
		t.Fatalf("CommitMessage: %v %q", err, msg)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run TestServiceCommitMessage -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Implement**

Mirror `CurrentBranch`'s read-reservation wrapper exactly (acquire Read gate, call the verb, release). Find that method (query.go:196) and copy its structure:

```go
// CommitMessage returns rev's full commit message, under a Read reservation.
func (s *Service) CommitMessage(ctx context.Context, rev string) (string, error) {
	// ... same Read-reservation scaffolding as CurrentBranch ...
	return s.repo.CommitMessage(ctx, rev)
}
```

Use the precise gate/singleflight pattern the neighbouring read queries use (don't invent new scaffolding).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/ -run TestServiceCommitMessage -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/domain/query.go internal/domain/query_test.go
git commit -m "feat(domain): CommitMessage read query"
```

---

## Task 9: `gg commit reword` CLI

**Files:**
- Create: `internal/cli/commit_reword.go`
- Modify: `internal/cli/cli.go` (`cmdCommit` first-arg dispatch)
- Test: `internal/cli/commit_reword_test.go`

**Interfaces:**
- Consumes: `engine.Reword`, `runOperation`, `finish`, `os.Executable`, `svc.CurrentBranch`.
- Produces: `gg commit reword <commit> -m <message>`.

- [ ] **Step 1: Write the failing test**

```go
func TestCommitRewordCLIHead(t *testing.T) {
	svc, dir := newCLIService(t)
	gitIn(t, dir, "commit", "--allow-empty", "-m", "orig")
	code := cmdCommit(svc, []string{"reword", "HEAD", "-m", "new message"}, io.Discard, io.Discard)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := gitOut(t, dir, "log", "-1", "--pretty=%B"); !strings.Contains(got, "new message") {
		t.Fatalf("message not reworded: %q", got)
	}
}

func TestCommitRewordCLIUsage(t *testing.T) {
	svc, _ := newCLIService(t)
	if code := cmdCommit(svc, []string{"reword", "HEAD"}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("want usage exit 2 (missing -m), got %d", code)
	}
}
```

Match `cmdCommit`'s real signature (`svc, args, stdout, stderr`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestCommitReword -v`
Expected: FAIL (`reword` parsed as a stray positional by the commit flagset).

- [ ] **Step 3: Implement dispatch + command**

At the very top of `cmdCommit` (cli.go:130), before building the flagset:

```go
	if len(args) > 0 && args[0] == "reword" {
		return cmdCommitReword(svc, args[1:], stdout, stderr)
	}
```

Create `commit_reword.go`:

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
)

// cmdCommitReword implements `gg commit reword <commit> -m <message>`. The
// commit positional precedes or follows -m (flag parsing tolerates either via
// a manual split). v1 requires -m (no editor).
func cmdCommitReword(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("commit reword", flag.ContinueOnError)
	fs.SetOutput(stderr)
	msg := fs.String("m", "", "new commit message (required)")
	// Allow `<commit> -m msg`: pull the first non-flag arg out as the commit.
	commit, rest := splitFirstPositional(args)
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if commit == "" || *msg == "" {
		fmt.Fprintln(stderr, "usage: gg commit reword <commit> -m <message>")
		return 2
	}
	ggBin, err := os.Executable()
	if err != nil {
		fmt.Fprintln(stderr, "commit reword:", err)
		return 1
	}
	res, err := runOperation(context.Background(), svc,
		engine.Reword{Commit: commit, NewMsg: *msg, GGBin: ggBin}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// splitFirstPositional returns the first non-flag token and the remaining args
// (so `<commit> -m msg` and `-m msg <commit>` both work, mirroring cmdCheckout).
func splitFirstPositional(args []string) (first string, rest []string) {
	for i, a := range args {
		if first == "" && a != "" && a[0] != '-' {
			first = a
			rest = append(rest, args[:i]...)
			rest = append(rest, args[i+1:]...)
			return first, rest
		}
	}
	return "", args
}
```

If a positional-splitting helper already exists from `cmdCheckout` (Task/chunk-3 added an order-independent parse), reuse it instead of adding `splitFirstPositional` (grep `cmdCheckout` in `ops.go`).

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/cli/ -run TestCommitReword -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/commit_reword.go internal/cli/cli.go internal/cli/commit_reword_test.go
git commit -m "feat(cli): gg commit reword <commit> -m <message>"
```

---

## Task 10: TUI reword popup + `.` menu row (Commits panel)

**Files:**
- Create: `internal/tui/reword_popup.go`
- Modify: `internal/tui/model.go` (popup field, key routing, async prefill msg), `internal/tui/action_menu.go` (menu row), `internal/tui/footer.go`, `internal/tui/help.go`
- Test: `internal/tui/reword_popup_test.go`

**Interfaces:**
- Consumes: `m.backingIndex(panelCommits)`, `m.commits` (`[]model.Commit`), `m.startOp`, `engine.Reword`, `splitMessage`, `commitPopup`, and a prefill via `svc.CommitMessage` (async `tea.Cmd`).
- Produces: `m.rewordPopup *rewordPopup`.

**Design:** reuse `commitPopup` for the title/desc editing surface (same `applyEditKey`), wrapped in a `rewordPopup` that remembers the target commit hash. Opening it kicks off an async `CommitMessage` fetch to pre-fill (so the full body is preserved); until it lands, pre-fill from the row's `Subject`.

- [ ] **Step 1: Write the failing test**

```go
func TestRewordPopupSubmitBuildsOp(t *testing.T) {
	m := newTestModel(t)
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "abc1234", Subject: "old subject", Parents: []string{"p"}}}
	m, ok := m.openRewordPopup()
	if !ok || m.rewordPopup == nil {
		t.Fatalf("popup did not open")
	}
	if m.rewordPopup.commit != "abc1234" {
		t.Fatalf("target commit not captured: %q", m.rewordPopup.commit)
	}
	if m.rewordPopup.popup.title != "old subject" {
		t.Fatalf("title not prefilled from subject: %q", m.rewordPopup.popup.title)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestRewordPopup -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Implement the popup**

Create `reword_popup.go`:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// rewordPopup wraps the shared commit-message editor (commitPopup) with the
// target commit. Pre-filled from the row Subject immediately; the full message
// arrives async via rewordPrefillMsg and replaces it if the user hasn't typed.
type rewordPopup struct {
	commit  string
	popup   commitPopup
	touched bool // user edited before the async prefill landed
}

// rewordPrefillMsg carries the fetched full message for commit.
type rewordPrefillMsg struct {
	commit string
	msg    string
	err    error
}

// openRewordPopup opens the dialog for the selected Commits-panel row.
func (m Model) openRewordPopup() (Model, bool) {
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return m, false
	}
	c := m.commits[bi]
	t, d := splitMessage(c.Subject)
	m.rewordPopup = &rewordPopup{commit: c.Hash, popup: commitPopup{title: t, desc: d}}
	return m, true
}

// updateRewordPopupKey handles one key while the reword popup is open.
func (m Model) updateRewordPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	rp := m.rewordPopup
	submit, cancel := rp.popup.applyEditKey(msg)
	rp.touched = true
	switch {
	case cancel:
		m.rewordPopup = nil
	case submit:
		if strings.TrimSpace(rp.popup.title) == "" {
			m.statusMsg = "title required"
			return m, nil
		}
		op := engine.Reword{Commit: rp.commit, NewMsg: rp.popup.message(), GGBin: m.ggBin()}
		m.rewordPopup = nil
		return m.startOp(op)
	}
	return m, nil
}

// renderRewordPopup draws the reword dialog (reuses the commit field renderer).
func (m Model) renderRewordPopup() string {
	var b strings.Builder
	b.WriteString("Reword commit " + shortHash(m.rewordPopup.commit) + "\n\n")
	b.WriteString(renderCommitFields(&m.rewordPopup.popup))
	b.WriteString("\n[tab] switch field  [enter] newline/next  [ctrl+s] reword  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
```

- `m.ggBin()`: if a helper doesn't exist, inline `ggBin, _ := os.Executable()` where the op is built (mirror how `model.go:921` obtains it for the irebase editor — it computes it in the `Update` msg handler). Simplest: capture `ggBin` when opening and store it on `rewordPopup`. Grep `os.Executable` in `model.go` and follow that pattern.
- In `model.go`: add field `rewordPopup *rewordPopup`; route keys to `updateRewordPopupKey` when non-nil (sibling to `commitPopup` routing); render `renderRewordPopup` in the overlay; handle `rewordPrefillMsg` in `Update` (if `!rp.touched && msg.err == nil`, set `rp.popup.title, rp.popup.desc = splitMessage(msg.msg)`).
- Opening via the menu row (next step) returns a `tea.Cmd` that calls `svc.CommitMessage` and emits `rewordPrefillMsg`. Mirror how other async domain reads are dispatched in the package (grep for an existing `tea.Cmd` wrapping a `svc.` call).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestRewordPopup -v`
Expected: PASS

- [ ] **Step 5: Wire `.` menu row + async prefill + footer + help**

In `action_menu.go` `availableActions`, add a Commits-panel row that, on `run`, opens the popup AND returns the prefill command:

```go
	if m.focus == panelCommits {
		if bi, ok := m.backingIndex(panelCommits); ok {
			hash := m.commits[bi].Hash
			out = append(out, actionRow{
				id:    "reword-commit",
				label: "Rename commit",
				scope: scopeWindow,
				run: func(m Model) (Model, tea.Cmd) {
					m, _ = m.openRewordPopup()
					return m, m.fetchRewordPrefill(hash)
				},
			})
		}
	}
```

Add `fetchRewordPrefill(hash string) tea.Cmd` (returns a `tea.Cmd` that calls `m.svc.CommitMessage` and wraps the result in `rewordPrefillMsg`). Add footer entry (`scopeWindow`, gated `m.focus == panelCommits`) + help line under the Commits panel.

- [ ] **Step 6: Run + commit**

```bash
go test ./internal/tui/
git add internal/tui/reword_popup.go internal/tui/reword_popup_test.go internal/tui/model.go internal/tui/action_menu.go internal/tui/footer.go internal/tui/help.go
git commit -m "feat(tui): . menu Rename commit + reword popup on Commits panel"
```

---

## Task 11: e2e scenarios

**Files:**
- Create: `e2e/scenarios/s51_branch_rename.toml`, `e2e/scenarios/s52_commit_reword.toml`

(Confirm the next free `sNN` number — grep `e2e/scenarios/` and bump past the highest, accounting for any taken since this plan was written.)

**Interfaces:**
- Consumes: the e2e harness verbs (`[[run]]` with `exit`, `[expect]` with `branch`/`branches`/`log`). Reuse the schema documented in `.claude/skills/writing-e2e-scenarios` / existing scenarios.

- [ ] **Step 1: Write `s51_branch_rename.toml`**

Mirror an existing local-only scenario (e.g. a `gg branch` one). Build a repo with a branch `old`, run `gg branch rename old renamed`, assert the branch set:

```toml
name = "branch rename"

[[input.steps]]
branch = "old"

[[run]]
args = ["branch", "rename", "old", "renamed"]
exit = 0

[expect]
branches = ["main", "renamed"]   # match the harness's exact branch-set assertion key/shape
```

Check the real `[expect]` schema (`e2e/scenario.go`) — use whatever key asserts the local branch set (it may be `branches`), and confirm whether `main` appears.

- [ ] **Step 2: Run it**

Run: `go test ./e2e/ -run 'TestScenarios/.*branch_rename' -v` (match the harness's test-name shape)
Expected: PASS

- [ ] **Step 3: Write `s52_commit_reword.toml`**

Repo with commits `c1`,`c2`,`c3`; reword `HEAD~1` (`c2`); assert the log shows the new subject and still has `c3`:

```toml
name = "commit reword"

[[input.steps]]
commit = "c1"
[[input.steps]]
commit = "c2"
[[input.steps]]
commit = "c3"

[[run]]
args = ["commit", "reword", "HEAD~1", "-m", "c2 reworded"]
exit = 0

[expect.log]
contains = ["c2 reworded", "c3", "c1"]   # match the LogExpect schema (Task: check scenario.go LogExpect fields)
```

Use the real `LogExpect` fields (the remotes chunk used `log` assertions; confirm `contains`/`messages`/`Branch` field names in `e2e/scenario.go`). If reword-of-HEAD is simpler to assert, also acceptable; the point is to exercise the CLI end-to-end against real git.

- [ ] **Step 4: Run both scenarios**

Run: `go test ./e2e/ -run 'TestScenarios/.*(branch_rename|commit_reword)' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add e2e/scenarios/s51_branch_rename.toml e2e/scenarios/s52_commit_reword.toml
git commit -m "test(e2e): branch rename + commit reword scenarios"
```

---

## Task 12: Docs + agentskill

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (`Version`), and the dogfood copy via `gg init --update`.

- [ ] **Step 1: CHANGELOG + README**

Add a `### Added` block to `CHANGELOG.md`: copy branch name (`.` menu), `gg branch rename`, `gg commit reword` + the TUI `.`-menu Rename branch / Rename commit. In `README.md`, add the two CLI verbs to the verb list and mention the new `.`-menu actions in the Branches/Commits sections.

- [ ] **Step 2: agentskill**

Bump `agentskill.Version` (current 13 → 14) in `internal/agentskill/agentskill.go`. Add `gg branch rename` and `gg commit reword` to `internal/agentskill/using-gg.md` (mirror the format of the existing verb entries).

- [ ] **Step 3: Refresh the dogfood skill copy**

Run: `go build ./cmd/gg && ./gg init --update`
(This rewrites `.claude/skills/using-gg/SKILL.md` to match `using-gg.md` so `TestDogfoodSkillCopyInSync` passes. Note: it also rewrites the user's global `~/.junie/skills/using-gg/SKILL.md` — disclose this side effect.)

- [ ] **Step 4: Verify the dogfood test + commit**

Run: `go test ./internal/agentskill/`
Expected: PASS

```bash
git add CHANGELOG.md README.md internal/agentskill/ .claude/skills/using-gg/SKILL.md
git commit -m "docs: document copy/rename branch + reword commit; agentskill v14"
```

---

## Final verification

- [ ] **Run the full race gate**

Run: `./test.sh race`
Expected: all stages green (vet+gofmt → unit → e2e), including the new scenarios. Fix any `gofmt -l` alignment flags with `gofmt -w` + `git commit --amend --no-edit` on the offending task's commit.

- [ ] **Hand off for merge** — the human merges `worktree-context-ops` into `main` (per project convention), then the worktree + branch are cleaned up.

---

## Self-review notes (coverage vs spec)

- Copy branch name → Task 1. ✓
- Rename branch (verb/op/CLI/TUI) → Tasks 2–5. ✓ No worktree refusal (matches spec). ✓
- Reword (verb/op/CLI/TUI, HEAD amend + mid-branch rebase + root refusal + dirty-HEAD via rebase) → Tasks 6–10. ✓
- e2e → Task 11; docs/agentskill → Task 12. ✓
- The one refinement vs the spec's wording: a **dirty** HEAD reword routes through the rebase path (not amend) so the staged/unstaged split is preserved by the reused stash-wrap — strictly stronger than "amend with a stash guard", and avoids re-implementing the staged-restore logic. Clean HEAD stays the cheap amend. Root-as-HEAD (single-commit repo) still uses amend.
