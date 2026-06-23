# Annotate an Existing Tag (Tags-C) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Annotate an existing tag — `git tag -a -f -m <msg> <tag> <target>` — from the Tags `.` menu (TUI) and the CLI (`gg tag annotate <tag> -m <msg>`).

**Architecture:** Add `Force` to the `git.CreateTag` verb and the `engine.CreateTag` op; annotate = force-recreate annotated at the tag's current target commit. TUI opens a message popup (modelled on `renameBranchPopup`) prefilled with the tag's subject; CLI resolves the target via `svc.RevParse(tag+"^{commit}")`. Local-only, no Decider confirm.

**Tech Stack:** Go 1.26, `internal/git` verbs, `internal/engine` ops, Bubble Tea TUI, CLI, declarative e2e harness.

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26.
- A git verb is one invocation via `gitcmd`/`r.Runner.Run`; `internal/tui` and `internal/cli` never import `internal/git` (tests may build `&git.Repo{Runner: fake}`).
- TUI `Model` is a value receiver.
- Annotate is local-only and reversible → NO Decider confirm (consistent with local tag create/delete).
- A non-empty message is REQUIRED to annotate (empty rejected at TUI + CLI).
- The tag's target commit is PRESERVED (TUI passes `tag.Target`; CLI resolves `RevParse(tag+"^{commit}")`).
- Every commit message ends with these two trailers, verbatim:
  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro
  ```
- Run `./test.sh race` before merge (human's call).

---

### Task 1: `git.CreateTag` gains `force`

**Files:**
- Modify: `internal/git/mutate.go` (`CreateTag` signature + `-f`)
- Modify (callers, pass `false`): `internal/engine/create_tag.go`, `internal/git/tag_create_test.go` (×2), `internal/engine/checkout_test.go` (×2), `internal/engine/delete_tag_test.go` (×1)
- Modify: `internal/git/tag_create_test.go` (+ force argv + real annotate test)

**Interfaces:**
- Produces: `func (r *Repo) CreateTag(ctx, name, commit, message string, force bool) error` — `git tag [-a -m <msg>] [-f] <name> [<commit>]`.

- [ ] **Step 1: Update the verb signature + `-f`**

In `internal/git/mutate.go`, change `CreateTag` to:

```go
// CreateTag creates a tag at commit (empty commit = HEAD). A non-empty message
// makes it annotated (git tag -a -m); force (-f) replaces an existing tag.
func (r *Repo) CreateTag(ctx context.Context, name, commit, message string, force bool) error {
	argv := gitcmd.New("tag").
		ArgIf(message != "", "-a", "-m", message).
		ArgIf(force, "-f").
		Arg(name).
		ArgIf(commit != "", commit).
		ToArgv()
	_, err := r.Runner.Run(ctx, "git tag", argv)
	return err
}
```

- [ ] **Step 2: Fix all callers to the new arity (pass `false`)**

- `internal/engine/create_tag.go`: `deps.Repo.CreateTag(ctx, op.Name, op.Commit, op.Message, op.Force)` — but `op.Force` doesn't exist yet (Task 2 adds it). For THIS task, pass `false`; Task 2 changes it to `op.Force`. (Or do Task 2's struct field first — either order; keep this task compiling by passing `false` here and updating in Task 2.)
- `internal/git/tag_create_test.go:14,17`, `internal/engine/checkout_test.go:10,26`, `internal/engine/delete_tag_test.go:12`: append `, false` to each `repo.CreateTag(...)` call.

Run `go build ./... ` and grep `\.CreateTag(` to confirm no caller is left at the old arity.

- [ ] **Step 3: Write the failing tests**

Append to `internal/git/tag_create_test.go` (it already has `head`, the repo helper, and uses `repo.CreateTag`):

```go
func TestCreateTagForceArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git tag", gitexec.Result{})
	repo := &Repo{Runner: f}
	if err := repo.CreateTag(context.Background(), "v1.0.0", "abc1234", "rel", true); err != nil {
		t.Fatalf("CreateTag force: %v", err)
	}
	var argv []string
	for _, c := range f.Calls {
		if c.Name == "git tag" {
			argv = c.Argv
		}
	}
	want := []string{"tag", "-a", "-m", "rel", "-f", "v1.0.0", "abc1234"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
}

func TestCreateTagForceAnnotatesExisting(t *testing.T) {
	dir, repo := newRepoForTags(t) // use this package's real-repo helper (grep below)
	head := gitOut(t, dir, "rev-parse", "HEAD")
	if err := repo.CreateTag(context.Background(), "v1.0.0", head, "", false); err != nil {
		t.Fatalf("lightweight: %v", err)
	}
	if typ := gitOut(t, dir, "cat-file", "-t", "v1.0.0"); typ != "commit" {
		t.Fatalf("lightweight tag object type = %q, want commit", typ)
	}
	if err := repo.CreateTag(context.Background(), "v1.0.0", head, "now annotated", true); err != nil {
		t.Fatalf("force annotate: %v", err)
	}
	if typ := gitOut(t, dir, "cat-file", "-t", "v1.0.0"); typ != "tag" {
		t.Fatalf("after annotate, tag object type = %q, want tag (annotated)", typ)
	}
	if got := gitOut(t, dir, "rev-parse", "v1.0.0^{commit}"); got != head {
		t.Fatalf("annotate moved the target: %q != %q", got, head)
	}
}
```

Use the real names this package's `tag_create_test.go` already uses for the temp-repo constructor and the string-returning git helper (grep `func newRepo`, `func gitOut`, and how the existing `TestCreateTag*` builds its repo + `head`; replace `newRepoForTags`/`gitOut` with the actual names — do NOT invent helpers, mirror the existing test in the same file).

- [ ] **Step 4: Run**

Run: `go test ./internal/git/ -run TestCreateTag -v` → PASS. Then `go test ./internal/git/ ./internal/engine/` to confirm the caller updates compile + pass.

- [ ] **Step 5: Commit**

```bash
git add internal/git/mutate.go internal/git/tag_create_test.go internal/engine/create_tag.go internal/engine/checkout_test.go internal/engine/delete_tag_test.go
git commit  # "feat(git): CreateTag force flag (-f, replace existing tag)" + trailers
```

---

### Task 2: `engine.CreateTag` gains `Force`

**Files:**
- Modify: `internal/engine/create_tag.go`
- Modify: `internal/engine/create_tag_test.go`

**Interfaces:**
- Produces: `engine.CreateTag{Name, Commit, Message string, Force bool}`.

- [ ] **Step 1: Write the failing test**

Append to `internal/engine/create_tag_test.go` (mirror its existing real-repo setup):

```go
func TestCreateTagForceReplacesExisting(t *testing.T) {
	dir, repo := newRepo(t)
	head := gitE_out(t, dir, "rev-parse", "HEAD") // use this file's real-repo + git-output helpers
	if _, err := (CreateTag{Name: "v1", Commit: head}).Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Without Force, re-tagging the same name must fail (git refuses).
	if _, err := (CreateTag{Name: "v1", Commit: head, Message: "x"}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("re-tag without Force must error")
	}
	// With Force, it succeeds and becomes annotated.
	res, err := CreateTag{Name: "v1", Commit: head, Message: "annotated now", Force: true}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil || !res.Changed {
		t.Fatalf("force annotate: res=%+v err=%v", res, err)
	}
	if typ := gitE_out(t, dir, "cat-file", "-t", "v1"); typ != "tag" {
		t.Fatalf("tag type = %q, want tag", typ)
	}
}
```

Replace `newRepo`/`gitE_out` with this package's actual helpers (the engine tests use `newRepo(t) (string, *git.Repo)` and `gitOut(t, dir, ...)` — grep `func gitOut` / `func newRepo` in `internal/engine/*_test.go` and use those).

- [ ] **Step 2: Run to verify fail**

Run: `go test ./internal/engine/ -run TestCreateTagForce -v`
Expected: FAIL — `CreateTag{...}.Force` unknown field.

- [ ] **Step 3: Add the field + pass it through**

In `internal/engine/create_tag.go`: add `Force bool` to the struct (after `Message`), and change the verb call to `deps.Repo.CreateTag(ctx, op.Name, op.Commit, op.Message, op.Force)`. (This replaces the temporary `false` from Task 1.)

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/engine/ -run TestCreateTag -v` → PASS. Then `go test ./internal/engine/`.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/create_tag.go internal/engine/create_tag_test.go
git commit  # "feat(engine): CreateTag Force field (annotate/replace existing tag)" + trailers
```

---

### Task 3: TUI Annotate row + message popup

**Files:**
- Create: `internal/tui/annotate_tag_popup.go`
- Modify: `internal/tui/tags_actions.go` (+ `tagAnnotateRow`)
- Modify: `internal/tui/action_menu.go` (wire it beside the other tag rows)
- Modify: `internal/tui/tags_actions_test.go`

**Interfaces:**
- Consumes: `Model.backingIndex(panelTags)`, `Model.opsIdle`, `Model.pushLayer`, `Model.popLayer`, `Model.startOp`, `newTextField`, `textfield`, `viewField`, `overlayDims`/`overlayCenter`/`clipToHeight`/`modalStyle`/`popupContentWidth`/`popupInnerWidth` (all used by `renameBranchPopup`), `layerOf[T]` (tests), `engine.CreateTag`, `model.Tag{Name,Target,Subject}`.
- Produces: `annotateTagPopup` (layer with `update`/`render`), `openAnnotateTagPopup() (Model, bool)`, `tagAnnotateRow() (actionRow, bool)` (id `tag-annotate`).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/tags_actions_test.go`:

```go
func TestTagAnnotateRowPresent(t *testing.T) {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	got := ids(availableActions(m))
	if !got["tag-annotate"] {
		t.Fatalf("expected tag-annotate; got %v", got)
	}
}

func TestAnnotatePopupPrefillsSubject(t *testing.T) {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234", Annotated: true, Subject: "old message"}}
	m, ok := m.openAnnotateTagPopup()
	if !ok {
		t.Fatal("openAnnotateTagPopup returned false")
	}
	p := layerOf[*annotateTagPopup](m)
	if p == nil || p.message.Value() != "old message" {
		t.Fatalf("popup message = %+v, want prefilled 'old message'", p)
	}
	if p.target != "abc1234" {
		t.Fatalf("popup target = %q, want abc1234", p.target)
	}
}

func TestAnnotatePopupEmptyMessageKeepsOpen(t *testing.T) {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}} // lightweight → blank subject
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m, _ = m.openAnnotateTagPopup()
	p := layerOf[*annotateTagPopup](m)
	if p == nil {
		t.Fatal("no popup")
	}
	um, _ := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = um
	if layerOf[*annotateTagPopup](m) == nil {
		t.Fatal("empty message must keep the popup open (annotate requires a message)")
	}
}

func TestAnnotatePopupSubmitDispatches(t *testing.T) {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m, _ = m.openAnnotateTagPopup()
	p := layerOf[*annotateTagPopup](m)
	p.message = newTextField("a message")
	_, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("submit with a message must start the op (non-nil cmd)")
	}
}
```

(`tea` is already imported in this test package via other tests; if not, add `tea "github.com/charmbracelet/bubbletea"`.)

- [ ] **Step 2: Run to verify fail**

Run: `go test ./internal/tui/ -run 'TestTagAnnotate|TestAnnotatePopup' -v`
Expected: FAIL — `openAnnotateTagPopup`/`annotateTagPopup`/`tagAnnotateRow` undefined.

- [ ] **Step 3: Create the popup**

Create `internal/tui/annotate_tag_popup.go` (mirror `renameBranchPopup`, but the message field accepts spaces):

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// annotateTagPopup edits the message for an existing tag, force-recreating it
// as annotated at its current target. Prefilled with the tag's subject.
type annotateTagPopup struct {
	tag     string    // the tag being annotated (fixed)
	target  string    // its current commit, preserved
	message textfield // prefilled with the tag's current subject
}

// openAnnotateTagPopup opens the dialog for the selected Tags-panel row.
func (m Model) openAnnotateTagPopup() (Model, bool) {
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return m, false
	}
	t := m.tags[bi]
	m = m.pushLayer(&annotateTagPopup{tag: t.Name, target: t.Target, message: newTextField(t.Subject)})
	return m, true
}

func (p *annotateTagPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
	case tea.KeyEnter:
		if p.message.Value() == "" { // annotate requires a message
			return m, nil // keep the popup open
		}
		op := engine.CreateTag{Name: p.tag, Commit: p.target, Message: p.message.Value(), Force: true}
		m = m.popLayer()
		return m.startOp(op)
	default:
		p.message.HandleEditKey(msg) // the message allows spaces
	}
	return m, nil
}

func (p *annotateTagPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *annotateTagPopup) box(m Model) string {
	var b strings.Builder
	b.WriteString("Annotate tag " + p.tag + "\n\n")
	w, _ := m.overlayDims()
	b.WriteString(viewField("message: ", p.message, true, popupContentWidth(w)) + "\n\n")
	b.WriteString("[type] message  [enter] annotate  [esc] cancel")
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
```

Verify `tea.KeySpace` reaches `HandleEditKey` so spaces are typed: if the layer dispatch sends space as `tea.KeySpace` and the `default` case doesn't catch it, add `case tea.KeySpace: p.message.HandleEditKey(msg)` (check how `tagPopup` handles space in its message field and match it).

- [ ] **Step 4: Add the row + wire it**

In `internal/tui/tags_actions.go`:

```go
// tagAnnotateRow offers "Annotate <tag>" on the Tags panel: open a message
// popup (prefilled with the tag's subject) that force-recreates the tag as
// annotated at its current target. Local-only, no confirm.
func (m Model) tagAnnotateRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-annotate",
		label: "Annotate " + name,
		run: func(m Model) (tea.Model, tea.Cmd) {
			m, _ = m.openAnnotateTagPopup()
			return m, nil
		},
	}, true
}
```

In `internal/tui/action_menu.go`, beside the other tag-row appends, add:

```go
	if r, ok := m.tagAnnotateRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/tui/ -run 'TestTagAnnotate|TestAnnotatePopup' -v` → PASS. Then `go test ./internal/tui/`.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/annotate_tag_popup.go internal/tui/tags_actions.go internal/tui/action_menu.go internal/tui/tags_actions_test.go
git commit  # "feat(tui): Annotate tag on the Tags . menu (message popup)" + trailers
```

---

### Task 4: CLI `gg tag annotate`

**Files:**
- Modify: `internal/cli/tag.go` (+ `annotate` case + `cmdTagAnnotate`)
- Modify: `internal/cli/tag_test.go`

**Interfaces:**
- Consumes: `flag`, `svc.RevParse`, `runOperation`, `cliDecider{}`, `finish`, `engine.CreateTag`.
- Produces: `cmdTagAnnotate(svc *domain.Service, args []string, stdout, stderr io.Writer) int`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/tag_test.go` (uses `newRepoDir(t)`, `runCLI`, `runGit` — grep to confirm; `runGit`/`runCLI` exist in this package):

```go
func TestTagAnnotateMakesAnnotated(t *testing.T) {
	dir := newRepoDir(t)
	runGit(t, dir, "tag", "v1.0.0") // lightweight
	if code, _, errb := runCLI(t, dir, "tag", "annotate", "v1.0.0", "-m", "release one"); code != 0 {
		t.Fatalf("tag annotate exit = %d (stderr: %s)", code, errb)
	}
	if typ := strings.TrimSpace(runGit(t, dir, "cat-file", "-t", "v1.0.0")); typ != "tag" {
		t.Fatalf("tag type = %q, want tag (annotated)", typ)
	}
}

func TestTagAnnotateRequiresMessage(t *testing.T) {
	dir := newRepoDir(t)
	runGit(t, dir, "tag", "v1.0.0")
	if code, _, _ := runCLI(t, dir, "tag", "annotate", "v1.0.0"); code != 2 {
		t.Fatalf("missing -m exit = %d, want 2", code)
	}
}

func TestTagAnnotateUnknownTag(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "tag", "annotate", "nope", "-m", "x"); code == 0 {
		t.Fatal("unknown tag must exit non-zero")
	}
}
```

Note: `gg tag annotate v1.0.0 -m "release one"` — flags precede positionals, but here the positional `v1.0.0` comes before `-m`. Go's `flag` stops at the first non-flag arg, so `-m` AFTER the tag would be left unparsed. To match `cmdTagCreate`'s convention (flags first), the canonical form is `gg tag annotate -m "release one" v1.0.0`. WRITE THE TESTS WITH `-m` BEFORE the tag name: `runCLI(t, dir, "tag", "annotate", "-m", "release one", "v1.0.0")`. Update the three tests accordingly.

- [ ] **Step 2: Run to verify fail**

Run: `go test ./internal/cli/ -run TestTagAnnotate -v`
Expected: FAIL — `annotate` is an unknown subcommand (exit 2 with the wrong message) / `cmdTagAnnotate` undefined.

- [ ] **Step 3: Implement**

In `internal/cli/tag.go`, add to `cmdTag`'s switch (before `default`):

```go
	case args[0] == "annotate":
		return cmdTagAnnotate(svc, args[1:], stdout, stderr)
```

Update the unknown-subcommand help string to include `annotate`. Add:

```go
// cmdTagAnnotate implements `gg tag annotate -m <msg> <name>` — force-recreate
// the tag as annotated at its current target. Flags precede the name.
func cmdTagAnnotate(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tag annotate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	msg := fs.String("m", "", "annotation message (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg tag annotate -m <message> <name>")
		return 2
	}
	if *msg == "" {
		fmt.Fprintln(stderr, "tag annotate: -m <message> is required")
		return 2
	}
	name := fs.Arg(0)
	target, err := svc.RevParse(context.Background(), name+"^{commit}")
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	res, err := runOperation(context.Background(), svc,
		engine.CreateTag{Name: name, Commit: target, Message: *msg, Force: true}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}
```

Confirm `"flag"` is imported in `tag.go` (it is — `cmdTagCreate` uses it).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/cli/ -run TestTagAnnotate -v` → PASS. Then `go test ./internal/cli/`.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/tag.go internal/cli/tag_test.go
git commit  # "feat(cli): gg tag annotate -m <msg> <name>" + trailers
```

---

### Task 5: e2e scenario

**Files:**
- Create: `e2e/scenarios/s74_tag_annotate.toml`

- [ ] **Step 1: Write the scenario**

Create `e2e/scenarios/s74_tag_annotate.toml`:

```toml
name = "tag annotate: turn a lightweight tag annotated"

[input]
steps = [
  { write = "f.txt", content = "v1\n" },
  { commit = "c1" },
  { tag = "v1.0.0" },
]

[[run]]
cmd  = ["tag", "annotate", "-m", "release one", "v1.0.0"]
exit = 0

[expect]
clean = true

[[expect.tags]]
name = "v1.0.0"
```

Verify against the `writing-e2e-scenarios` skill: confirm the local-tags assertion shape (`[[expect.tags]]` / `[expect] tags = [...]` — use whatever the harness actually supports for asserting a LOCAL tag exists; if it only supports a name list, use that). The harness cannot assert annotated-ness, so this scenario is a full-stack smoke that the command runs (exit 0) and the tag persists; annotated-ness is covered by the CLI/verb integration tests. If the harness has no local-tag assertion at all, assert `[expect] clean = true` plus the `exit = 0` run (the command reaching the engine end to end is the coverage) and note it in the report.

- [ ] **Step 2: Run**

Run: `go test ./e2e/ -run 'TestScenarios/s74_tag_annotate' -v` → PASS. Iterate the assertion to whatever the harness supports; do not leave it failing.

- [ ] **Step 3: Commit**

```bash
git add e2e/scenarios/s74_tag_annotate.toml
git commit  # "test(e2e): s74 tag annotate runs end to end" + trailers
```

---

### Task 6: Docs + agentskill

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go`

- [ ] **Step 1: CHANGELOG**

Under `## [Unreleased]` → `### Added`:

```markdown
- **Annotate an existing tag.** The Tags panel `.` menu now offers **Annotate `<tag>`** — a message dialog (prefilled with the tag's current subject) that turns a lightweight tag annotated, or updates an annotated tag's message, keeping its target commit. The CLI adds `gg tag annotate -m <message> <name>`.
```

If a concurrent branch already opened the Added block, append; don't duplicate.

- [ ] **Step 2: README**

(a) add **Annotate `<tag>`** to the Tags `.`-menu description; (b) add `gg tag annotate -m <message> <name>` to the CLI `gg tag` cheatsheet. Match surrounding style.

- [ ] **Step 3: agentskill + bump Version**

In `internal/agentskill/using-gg.md`, document `gg tag annotate -m <message> <name>` in the tag section. Bump `Version` in `internal/agentskill/agentskill.go` by exactly 1.

- [ ] **Step 4: Refresh installed skill copies**

Run: `go run ./cmd/gg init --update`, then `go test ./internal/agentskill/` — `TestDogfoodSkillCopyInSync` must pass.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md internal/agentskill/ .claude/skills/using-gg/
git commit  # "docs: annotate existing tag (menu + gg tag annotate); agentskill bump" + trailers
```

---

## Self-review notes

- **Spec coverage:** verb force → T1; engine Force → T2; TUI popup + row → T3; CLI → T4; e2e → T5; docs + agentskill → T6.
- **Verified before writing:** the single production verb caller + the 5 direct test verb callers (T1 lists all); `svc.RevParse` exists (domain/query.go:308); `renameBranchPopup` is the layer template (single field, `update`/`render`/`box`, `viewField`/`overlayDims`/`modalStyle`/`popupContentWidth`/`popupInnerWidth`); `tagPopup`'s message field accepts spaces (mirror it); `model.Tag.Subject` is in-model (prefill source); s74 is the next free scenario number; `cmdTagCreate` uses `flag` with flags-before-positional.
- **Type consistency:** `engine.CreateTag{Name, Commit, Message, Force}` and verb `CreateTag(name, commit, message, force)` used identically across tasks; row id `tag-annotate`; popup type `*annotateTagPopup` with fields `tag`/`target`/`message`.
- **Confirm at execution (adapt, don't redesign):** the `git`/`engine` test temp-repo + git-output helper names (T1/T2 grep — `newRepo`/`gitOut`); whether the popup needs an explicit `tea.KeySpace` case (T3, mirror `tagPopup`); the exact local-tags e2e assertion key (T5, via the e2e skill).
```
