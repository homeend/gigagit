# Tags — Stage 2 (Create) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Create a tag (lightweight or annotated) at a selected commit from the Commits `.`-menu and from `gg tag create`.

**Architecture:** New git verb `CreateTag` → engine op `CreateTag{Name,Commit,Message}` (decision-free, mirrors `CreateBranch`) → TUI `tagPopup` (name + optional message) opened by a Commits `.`-menu row → `gg tag create` CLI. After a successful create the existing post-op reload refreshes the Tags tab (Stage 1 already wired `snap.Tags` into the reload).

**Tech Stack:** Go 1.26, Bubble Tea, gitcmd/gitexec, real-git tests, TOML e2e.

## Global Constraints

- A git verb is one git invocation; `internal/tui`/`internal/cli` never import `internal/git`.
- Message `""` ⇒ lightweight (`git tag <name> <commit>`); non-empty ⇒ annotated (`git tag -a -m <msg> <name> <commit>`).
- No Decider — a duplicate name / bad ref is a surfaced git error (decision-free op, like `CreateBranch`).
- Work in worktree `/mnt/t/others/gg-tags2` (branch `tags-stage2-create`).
- CLI surface changes ⇒ bump `agentskill.Version` + `gg init --update` (the `TestDogfoodSkillCopyInSync` gate).

---

### Task 1: git `CreateTag` verb + `GitOps` interface entry

**Files:**
- Modify: `internal/git/mutate.go` (add `CreateTag` after `CreateBranch` ~line 44)
- Modify: `internal/engine/gitops.go` (add `CreateTag` to the interface, after `CreateBranch` ~line 48)
- Create: `internal/git/tag_create_test.go`

**Interfaces:**
- Produces: `(*git.Repo).CreateTag(ctx, name, commit, message string) error`; same signature added to `engine.GitOps`.

- [ ] **Step 1: Failing real-git test** — `internal/git/tag_create_test.go`:

```go
package git

import (
	"context"
	"testing"
)

func TestRepoCreateTag(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	head := gitOutIn(t, dir, "rev-parse", "HEAD")

	if err := repo.CreateTag(context.Background(), "v1.0.0", head, ""); err != nil {
		t.Fatalf("lightweight: %v", err)
	}
	if err := repo.CreateTag(context.Background(), "v2.0.0", head, "release two"); err != nil {
		t.Fatalf("annotated: %v", err)
	}
	if typ := gitOutIn(t, dir, "cat-file", "-t", "v1.0.0"); typ != "commit" {
		t.Fatalf("v1.0.0 type = %q, want commit (lightweight)", typ)
	}
	if typ := gitOutIn(t, dir, "cat-file", "-t", "v2.0.0"); typ != "tag" {
		t.Fatalf("v2.0.0 type = %q, want tag (annotated)", typ)
	}
}
```

- [ ] **Step 2: Run — expect FAIL** (`repo.CreateTag undefined`):
`cd /mnt/t/others/gg-tags2 && go test ./internal/git/ -run TestRepoCreateTag`

- [ ] **Step 3: Add the verb** — in `internal/git/mutate.go` after `CreateBranch`:

```go
// CreateTag creates a tag at commit (empty commit = HEAD). A non-empty message
// makes it annotated (git tag -a -m); otherwise it is lightweight. git refuses
// an existing tag name.
func (r *Repo) CreateTag(ctx context.Context, name, commit, message string) error {
	b := gitcmd.New("tag").
		ArgIf(message != "", "-a", "-m", message).
		Arg(name).
		ArgIf(commit != "", commit)
	_, err := r.Runner.Run(ctx, "git tag", b.ToArgv())
	return err
}
```

- [ ] **Step 4: Add to the `GitOps` interface** — in `internal/engine/gitops.go` after the `CreateBranch` line:

```go
	CreateTag(ctx context.Context, name, commit, message string) error
```

- [ ] **Step 5: Run — expect PASS:** `cd /mnt/t/others/gg-tags2 && go test ./internal/git/ -run TestRepoCreateTag`

- [ ] **Step 6: Commit:**
```bash
cd /mnt/t/others/gg-tags2
gofmt -w internal/git/mutate.go internal/engine/gitops.go
git add internal/git/mutate.go internal/git/tag_create_test.go internal/engine/gitops.go
git commit -m "feat(git): CreateTag verb (lightweight/annotated) + GitOps entry"
```

---

### Task 2: engine `CreateTag` op

**Files:**
- Create: `internal/engine/create_tag.go`
- Create: `internal/engine/create_tag_test.go`

**Interfaces:**
- Consumes: `GitOps.CreateTag` (Task 1).
- Produces: `engine.CreateTag{Name, Commit, Message string}` implementing `Operation`.

- [ ] **Step 1: Failing engine test** — `internal/engine/create_tag_test.go` (mirror `create_branch_test.go`; reuse this package's `newRepo`/`drain` helpers — check `create_branch_test.go` for exact names):

```go
package engine

import (
	"context"
	"testing"
)

func TestCreateTagLightweightAndAnnotated(t *testing.T) {
	repo, _ := newRepo(t) // real-git engine test helper (see create_branch_test.go)
	deps := OpDeps{Repo: repo, emit: func(context.Context, Event) {}}

	if _, err := (CreateTag{Name: "v1.0.0", Message: ""}).Run(context.Background(), deps); err != nil {
		t.Fatalf("lightweight: %v", err)
	}
	res, err := CreateTag{Name: "v2.0.0", Message: "rel2"}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("annotated: %v", err)
	}
	if !res.Changed || res.Summary == "" {
		t.Fatalf("result = %+v", res)
	}
}

func TestCreateTagRequiresName(t *testing.T) {
	repo, _ := newRepo(t)
	deps := OpDeps{Repo: repo, emit: func(context.Context, Event) {}}
	if _, err := (CreateTag{Name: ""}).Run(context.Background(), deps); err == nil {
		t.Fatal("empty name must error")
	}
}
```

NOTE: confirm `newRepo`'s return shape and the `OpDeps` field names (`emit`, `Repo`) against `create_branch_test.go` — match exactly. If `newRepo` returns only a repo, drop the second var.

- [ ] **Step 2: Run — expect FAIL** (`undefined: CreateTag`):
`cd /mnt/t/others/gg-tags2 && go test ./internal/engine/ -run TestCreateTag`

- [ ] **Step 3: Write the op** — `internal/engine/create_tag.go` (mirror `create_branch.go`):

```go
package engine

import (
	"context"
	"fmt"
)

// CreateTag creates a tag at Commit (empty = HEAD). A non-empty Message makes it
// annotated; otherwise lightweight. Decision-free: a duplicate name or bad ref
// surfaces as a git error.
type CreateTag struct {
	Name    string // required
	Commit  string // "" = HEAD
	Message string // "" = lightweight, else annotated
}

func (op CreateTag) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Name == "" {
		return Result{}, fmt.Errorf("create tag: Name is required")
	}
	detail := op.Name
	if op.Commit != "" {
		detail += " at " + op.Commit
	}
	deps.emit(ctx, Progress{Step: "creating tag", Detail: detail})

	if err := deps.Repo.CreateTag(ctx, op.Name, op.Commit, op.Message); err != nil {
		return Result{}, fmt.Errorf("create tag: %w", err)
	}
	kind := "lightweight"
	if op.Message != "" {
		kind = "annotated"
	}
	res := Result{Summary: "created " + kind + " tag " + op.Name, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = CreateTag{}
```

- [ ] **Step 4: Run — expect PASS:** `cd /mnt/t/others/gg-tags2 && go test ./internal/engine/ -run TestCreateTag`

- [ ] **Step 5: Commit:**
```bash
cd /mnt/t/others/gg-tags2
gofmt -w internal/engine/create_tag.go
git add internal/engine/create_tag.go internal/engine/create_tag_test.go
git commit -m "feat(engine): CreateTag op (decision-free, mirrors CreateBranch)"
```

---

### Task 3: TUI — `tagPopup` + `commitCreateTagRow` + menu + footer

**Files:**
- Create: `internal/tui/tag_popup.go`
- Modify: `internal/tui/commit_scope.go` (add `commitCreateTagRow` near `commitCreateBranchRow`)
- Modify: `internal/tui/action_menu.go` (register the row, ~line 90 near the other commit rows)
- Modify: `internal/tui/view.go` (footer hint, ~line 99 — see NOTE)
- Create: `internal/tui/tag_popup_test.go`

**Interfaces:**
- Consumes: `engine.CreateTag` (Task 2), `pushLayer`/`popLayer`/`startOp`, `backingIndex`.
- Produces: `tagPopup{commit string}` layer; `(Model).commitCreateTagRow() (actionRow, bool)`.

- [ ] **Step 1: Failing test** — `internal/tui/tag_popup_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// Typing a name + message then enter starts a CreateTag with resolved fields.
func TestTagPopupCreatesAnnotatedTag(t *testing.T) {
	m := footerModel()
	m.commits = []model.Commit{{Hash: "1111111aaaa", Subject: "one"}}
	p := &tagPopup{commit: "1111111aaaa"}
	m = m.pushLayer(p)

	for _, r := range "v1.0.0" {
		m, _ = p.update(m, keyMsg(string(r)).(interface{ k() }).(keyish).key()) // placeholder; see NOTE
	}
	_ = p
	// See NOTE: drive via keyMsg; assert the op payload through the test below.
}
```

NOTE: the placeholder above is illustrative. Write the real test by driving keys through `m.Update(keyMsg("v"))`-style calls after focusing the popup, OR by calling `p.update(m, keyMsg("v"))` directly (the helper returns a `tea.KeyMsg`). Mirror an existing popup test — open `internal/tui/branch_popup_test.go` (or search `branchPopup` in `_test.go`) and copy its key-driving + op-assertion shape exactly, swapping `CreateBranch`→`CreateTag`. The assertion: after typing a name (and optionally tabbing to the message field + typing), `enter` pops the layer and starts `engine.CreateTag{Name:..., Commit:"1111111aaaa", Message:...}`. Also assert `commitCreateTagRow` returns ok only when `focus==panelCommits && opsIdle()`.

- [ ] **Step 2: Run — expect FAIL** (`undefined: tagPopup`).

- [ ] **Step 3: Write `tagPopup`** — `internal/tui/tag_popup.go` (two fields; `tab` toggles; mirrors `branchPopup`):

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// tagPopup is the create-tag dialog at a commit. An empty message creates a
// lightweight tag; a non-empty one an annotated tag.
type tagPopup struct {
	commit  string // full SHA the tag points at
	name    string
	message string
	onMsg   bool // false = editing name, true = editing message (tab toggles)
}

func (p *tagPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyTab:
		p.onMsg = !p.onMsg
		return m, nil
	case tea.KeyEnter:
		if p.name == "" {
			return m, nil
		}
		op := engine.CreateTag{Name: p.name, Commit: p.commit, Message: p.message}
		m = m.popLayer()
		return m.startOp(op)
	case tea.KeyBackspace, tea.KeyCtrlH:
		if p.onMsg {
			if r := []rune(p.message); len(r) > 0 {
				p.message = string(r[:len(r)-1])
			}
		} else if r := []rune(p.name); len(r) > 0 {
			p.name = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		if p.onMsg { // tag names cannot contain spaces; the message can
			p.message += " "
		}
	case tea.KeyRunes:
		if p.onMsg {
			p.message += string(msg.Runes)
		} else {
			p.name += string(msg.Runes)
		}
	}
	return m, nil
}

func (p *tagPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *tagPopup) box(m Model) string {
	nameMark, msgMark := "> ", "  "
	if p.onMsg {
		nameMark, msgMark = "  ", "> "
	}
	var b strings.Builder
	b.WriteString("Create tag at " + displayStart(p.commit) + "\n\n")
	b.WriteString(nameMark + "name:    " + p.name + "\n")
	b.WriteString(msgMark + "message: " + p.message + "  (empty = lightweight)\n\n")
	b.WriteString("[tab] field  [enter] create  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
```

- [ ] **Step 4: Add `commitCreateTagRow`** — in `internal/tui/commit_scope.go` after `commitCreateBranchRow`:

```go
// commitCreateTagRow offers "Create tag here" on the Commits panel: open the
// create-tag dialog with the selected commit as the target.
func (m Model) commitCreateTagRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash
	return actionRow{
		id:    "commit-create-tag",
		label: "Create tag here",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.pushLayer(&tagPopup{commit: hash}), nil
		},
	}, true
}
```

- [ ] **Step 5: Register the row** — in `internal/tui/action_menu.go` near line 90 (after the create-branch/worktree rows):

```go
	if r, ok := m.commitCreateTagRow(); ok {
		rows = append(rows, r)
	}
```

(match the exact local variable name used by the surrounding `if r, ok := ...` blocks.)

- [ ] **Step 6: Footer hint** — the create-tag action lives in the `.` menu (no dedicated global key), like create-branch/worktree, so **no footer key is required**. Verify `commitCreateBranchRow` has no footer entry either (it doesn't); the `.`-menu surfaces it. Skip step 4-of-checklist for this menu-only action. (If `branchPopup`/create-branch DID add a footer hint, mirror it; grep `view.go` for "branch here" — expected: none.)

- [ ] **Step 7: Run — expect PASS:** `cd /mnt/t/others/gg-tags2 && go test ./internal/tui/ -run 'TestTagPopup|TestCommitCreateTag'`

- [ ] **Step 8: Full TUI package + eyeball:** `go test ./internal/tui/` then build and create a tag interactively (`.` on a commit → Create tag here).

- [ ] **Step 9: Commit:**
```bash
cd /mnt/t/others/gg-tags2
gofmt -w internal/tui/tag_popup.go internal/tui/commit_scope.go internal/tui/action_menu.go
git add internal/tui/tag_popup.go internal/tui/commit_scope.go internal/tui/action_menu.go internal/tui/tag_popup_test.go
git commit -m "feat(tui): create-tag popup + Commits .-menu Create tag here"
```

---

### Task 4: CLI `gg tag create`

**Files:**
- Modify: `internal/cli/tag.go` (add the `create` subcommand)
- Modify: `internal/cli/tag_test.go` (add create tests)

**Interfaces:**
- Consumes: `engine.CreateTag`.
- Produces: `gg tag create <name> [<commit>] [-m <msg>]` (commit defaults to HEAD).

- [ ] **Step 1: Failing CLI test** — append to `internal/cli/tag_test.go`:

```go
func TestTagCreateLightweightAndAnnotated(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, errb := runCLI(t, dir, "tag", "create", "v1.0.0"); code != 0 {
		t.Fatalf("lightweight create exit %d: %s", code, errb)
	}
	if code, _, errb := runCLI(t, dir, "tag", "create", "v2.0.0", "-m", "rel2"); code != 0 {
		t.Fatalf("annotated create exit %d: %s", code, errb)
	}
	_, out, _ := runCLI(t, dir, "tag", "ls")
	if !strings.Contains(out, "v1.0.0") || !strings.Contains(out, "v2.0.0") {
		t.Fatalf("created tags not listed:\n%s", out)
	}
}

func TestTagCreateRequiresName(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "tag", "create"); code == 0 {
		t.Fatal("create with no name must fail")
	}
}
```

- [ ] **Step 2: Run — expect FAIL.**

- [ ] **Step 3: Add the `create` subcommand** — in `internal/cli/tag.go`. Update the dispatcher and add `cmdTagCreate`:

```go
import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
)

// cmdTag dispatches the tag subcommands: ls | create.
func cmdTag(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0 || args[0] == "ls" || args[0] == "list":
		return cmdTagList(svc, stdout, stderr)
	case args[0] == "create":
		return cmdTagCreate(svc, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "tag: unknown subcommand %q (try: ls, create)\n", args[0])
		return 2
	}
}

// cmdTagCreate implements `gg tag create <name> [<commit>] [-m <msg>]`.
func cmdTagCreate(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tag create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	msg := fs.String("m", "", "annotated tag message (empty = lightweight)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) < 1 || len(rest) > 2 || rest[0] == "" {
		fmt.Fprintln(stderr, "usage: gg tag create <name> [<commit>] [-m <msg>]")
		return 2
	}
	commit := ""
	if len(rest) == 2 {
		commit = rest[1]
	}
	res, err := runOperation(context.Background(), svc,
		engine.CreateTag{Name: rest[0], Commit: commit, Message: *msg}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}
```

(Keep the existing `cmdTagList`.)

- [ ] **Step 4: Run — expect PASS:** `cd /mnt/t/others/gg-tags2 && go test ./internal/cli/ -run TestTag`

- [ ] **Step 5: Commit:**
```bash
cd /mnt/t/others/gg-tags2
gofmt -w internal/cli/tag.go
git add internal/cli/tag.go internal/cli/tag_test.go
git commit -m "feat(cli): gg tag create <name> [<commit>] [-m <msg>]"
```

---

### Task 5: e2e + docs + agentskill

**Files:**
- Create: `e2e/scenarios/s61_tag_create.toml`
- Modify: `internal/agentskill/using-gg.md` + `internal/agentskill/agentskill.go` (Version 18 → 19)
- Modify: `.claude/skills/using-gg/SKILL.md` (via `gg init --update`)
- Modify: `CHANGELOG.md`, `README.md` (CLI list + `.`-menu mention)
- Modify: `cmd/gg/main.go` (unknown-command help string — verify drift; tag already routes)

- [ ] **Step 1: e2e scenario** — `e2e/scenarios/s61_tag_create.toml`:

```toml
name = "tag create: lightweight and annotated, then list"

[input]
steps = [
  { write = "f.txt", content = "v1\n" },
  { commit = "c1" },
]

[[run]]
cmd  = ["tag", "create", "v1.0.0"]
exit = 0

[[run]]
cmd  = ["tag", "create", "v2.0.0", "-m", "release two"]
exit = 0

[[run]]
cmd             = ["tag", "ls"]
exit            = 0
stdout_contains = ["v1.0.0", "v2.0.0"]

[expect]
branch = "main"
clean  = true
```

- [ ] **Step 2: Run it:** `cd /mnt/t/others/gg-tags2 && go test ./e2e/ -run 'TestScenarios/s61'`

- [ ] **Step 3: agentskill** — in `internal/agentskill/using-gg.md`, change the `gg tag ls` line to document create:

```markdown
- `gg tag ls | create` — `ls` lists tags newest-first (one name per line);
  `create <name> [<commit>] [-m <msg>]` creates a tag at <commit> (default HEAD):
  lightweight, or annotated when `-m` is given.
```

Bump `agentskill.Version` 18 → 19.

- [ ] **Step 4: Refresh dogfood SKILL.md + verify:**
```bash
cd /mnt/t/others/gg-tags2 && go build -o /tmp/ggi2 ./cmd/gg && /tmp/ggi2 init --update
go test ./internal/agentskill/ -run TestDogfoodSkillCopyInSync
```

- [ ] **Step 5: Docs** — `CHANGELOG.md` Unreleased→Added: note "Create a tag (lightweight or annotated) from the Commits `.` menu (**Create tag here**) and `gg tag create`." `README.md`: add `gg tag create …` to the CLI list and mention **Create tag here** in the `.`-menu row. Verify `cmd/gg/main.go`'s help string isn't missing `tag` (add if drifted).

- [ ] **Step 6: Commit:**
```bash
cd /mnt/t/others/gg-tags2
git add e2e/scenarios/s61_tag_create.toml internal/agentskill/ .claude/skills/using-gg/SKILL.md CHANGELOG.md README.md cmd/gg/main.go
git commit -m "feat(tags): create — e2e + docs + agentskill v19"
```

---

### Final gate

- [ ] `cd /mnt/t/others/gg-tags2 && ./test.sh race` → `all green`.
- [ ] Hand back to the human for merge.

## Self-Review

**Spec coverage** (Stage 2 section of `2026-06-21-tags-design.md`): engine `CreateTag{Name,Commit,Message}` lightweight/annotated → Tasks 1-2 ✅; Commits `.`-menu popup → Task 3 ✅; `gg tag create <name> [<commit>] [-m]` default HEAD → Task 4 ✅; e2e + agentskill → Task 5 ✅. No Decider (spec: "no Decider; a name clash is a surfaced error") ✅.

**Placeholder scan:** Task 3 Step 1 test is explicitly marked illustrative with a NOTE pointing to `branch_popup_test.go` to copy the real key-driving shape — the implementer writes the concrete assertion from that sibling. All other code blocks are complete.

**Type consistency:** `CreateTag{Name,Commit,Message}` identical across git verb signature, `GitOps`, engine op, tagPopup, CLI. `commitCreateTagRow`, `tagPopup{commit,name,message,onMsg}` defined once.

**Decision-free check:** no `deps.decide` anywhere — matches `CreateBranch`. `Done` emitted on success only.
