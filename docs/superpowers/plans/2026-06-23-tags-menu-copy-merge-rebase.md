# Tags Menu: Copy + Merge/Rebase (Tags-A) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add to the Tags-panel `.` menu: Copy tag name + commit id/sha, and one-click Merge `<tag>` into current / Rebase current onto `<tag>`.

**Architecture:** Pure TUI wiring of helpers and engine ops that already exist (`copyShaRow` + `domain.RevParse` + `SmartMerge`/`SmartRebase`, all shipped in the remote-menu work). Rows are self-gating `Model` methods appended in `availableActions`; copy rows are a new `panelTags` case in `contextCopyRows`.

**Tech Stack:** Go 1.26, Bubble Tea TUI, `internal/gitexec` FakeRunner for tests.

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26.
- `internal/tui` MUST NOT import `internal/git` for production logic (tests may construct `&git.Repo{Runner: fake}` for a fake svc, as existing tests do).
- TUI `Model` is a value receiver; helper methods take/return `Model` by value.
- `model.Tag.Target` is the dereferenced commit (annotated tags peeled to `*objectname`), so it is the commit the tag resolves to.
- Copy-sha resolves `tag.Target` (NOT `tag.Name`) — `rev-parse <annotated-tag>` returns the tag object, the target resolves to the commit.
- Merge/rebase are hidden on a detached HEAD (reuse `Model.remoteCurrentBranch()`, which dual-guards `""` and `"(detached)"`).
- Every commit message ends with these two trailers, verbatim:
  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro
  ```
- Tests: `go test ./internal/tui/`. Run `./test.sh race` before merge (human's call).
- No engine/git/domain/CLI/agentskill changes.

---

### Task 1: Copy group on the Tags row

**Files:**
- Modify: `internal/tui/action_menu.go` (add a `case m.focus == panelTags:` to `contextCopyRows`, beside the `panelBranches`/`panelRemotes` cases ~lines 324-342)
- Modify: `internal/tui/tags_actions_test.go` (+ copy tests; add `domain`/`git`/`gitexec` imports if missing)

**Interfaces:**
- Consumes: `Model.copyRow(id, label, okMsg, text string) actionRow`; `Model.copyShaRow(ref, fallbackShort string) actionRow` (exists from remote Bucket A); `Model.backingIndex(panelTags)`; `model.Tag{Name, Target}`.
- Produces: a `panelTags` branch in `contextCopyRows` returning `copy-tag-name`, `copy-commit-id`, `copy-commit-sha` rows.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/tags_actions_test.go` (ensure the import block has `"github.com/gigagit/gg/internal/domain"`, `"github.com/gigagit/gg/internal/git"`, `"github.com/gigagit/gg/internal/gitexec"`, `"github.com/gigagit/gg/internal/model"`):

```go
func TestTagsCopyRows(t *testing.T) {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234", Annotated: true}}
	rows := m.contextCopyRows()
	if r, ok := findRow(rows, "copy-tag-name"); !ok || r.copyText != "v1.0.0" {
		t.Fatalf("missing copy-tag-name=v1.0.0; rows=%v", rows)
	}
	if r, ok := findRow(rows, "copy-commit-id"); !ok || r.copyText != "abc1234" {
		t.Fatalf("missing copy-commit-id=abc1234; rows=%v", rows)
	}
	if _, ok := findRow(rows, "copy-commit-sha"); !ok {
		t.Fatalf("missing copy-commit-sha; rows=%v", rows)
	}
}

func TestTagsCopyShaResolvesTarget(t *testing.T) {
	fr := gitexec.NewFakeRunner()
	fr.SetResponse("git rev-parse", gitexec.Result{Stdout: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\n"})
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234", Annotated: true}}
	m.svc = domain.New(&git.Repo{Runner: fr})
	rows := m.contextCopyRows()
	row, ok := findRow(rows, "copy-commit-sha")
	if !ok {
		t.Fatal("missing copy-commit-sha")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("copy-commit-sha run returned nil cmd")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestTagsCopy' -v`
Expected: FAIL — no `copy-tag-name`/`copy-commit-id`/`copy-commit-sha` rows for `panelTags` (the `contextCopyRows` switch has no `panelTags` case, so it returns nil for the Tags panel).

- [ ] **Step 3: Add the `panelTags` copy case**

In `internal/tui/action_menu.go`, in `contextCopyRows`, after the `case m.focus == panelRemotes:` branch, add:

```go
	case m.focus == panelTags:
		if bi, ok := m.backingIndex(panelTags); ok && bi >= 0 && bi < len(m.tags) {
			tg := m.tags[bi]
			return []actionRow{
				m.copyRow("copy-tag-name", "Copy tag name", "Copied tag name "+tg.Name, tg.Name),
				m.copyRow("copy-commit-id", "Copy commit id", "Copied commit id "+shortHash(tg.Target), tg.Target),
				m.copyShaRow(tg.Target, tg.Target),
			}
		}
```

(The `bi < len(m.tags)` guard mirrors the existing tag rows in `tags_actions.go`, since the Tags backing slice can be empty.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestTagsCopy' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/action_menu.go internal/tui/tags_actions_test.go
git commit  # "feat(tui): copy tag name + commit id/sha on the Tags . menu" + trailers
```

---

### Task 2: Merge / Rebase on the Tags row

**Files:**
- Modify: `internal/tui/tags_actions.go` (+ `tagMergeRow`, `tagRebaseRow`)
- Modify: `internal/tui/action_menu.go` (append the two rows beside `tagCheckoutRow`/`tagPushRow`/`tagDeleteRow`, ~lines 242-251)
- Modify: `internal/tui/tags_actions_test.go` (+ gating + dispatch tests)

**Interfaces:**
- Consumes: `Model.opsIdle()`, `Model.backingIndex(panelTags)`, `Model.remoteCurrentBranch() (string, bool)` (exists in `remote_actions.go`), `Model.startOp`, `engine.SmartMerge{Source}`, `engine.SmartRebase{Onto}`, `model.Tag{Name}`.
- Produces: `tagMergeRow() (actionRow, bool)` (id `tag-merge`), `tagRebaseRow() (actionRow, bool)` (id `tag-rebase`).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/tags_actions_test.go`:

```go
func tagsMergeModel() Model {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m.status.Branch = "main"
	return m
}

func TestTagMergeRebaseRowsPresent(t *testing.T) {
	m := tagsMergeModel()
	got := ids(availableActions(m))
	if !got["tag-merge"] || !got["tag-rebase"] {
		t.Fatalf("expected tag-merge + tag-rebase; got %v", got)
	}
}

func TestTagMergeRebaseHiddenOnDetachedHEAD(t *testing.T) {
	m := tagsMergeModel()
	m.status.Branch = "" // detached
	got := ids(availableActions(m))
	if got["tag-merge"] || got["tag-rebase"] {
		t.Fatalf("merge/rebase must be hidden on detached HEAD; got %v", got)
	}
}

func TestTagMergeRowDispatches(t *testing.T) {
	m := tagsMergeModel()
	row, ok := m.tagMergeRow()
	if !ok {
		t.Fatal("tagMergeRow not available")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("merge row run returned nil cmd")
	}
}

func TestTagRebaseRowDispatches(t *testing.T) {
	m := tagsMergeModel()
	row, ok := m.tagRebaseRow()
	if !ok {
		t.Fatal("tagRebaseRow not available")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("rebase row run returned nil cmd")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestTagMerge|TestTagRebase' -v`
Expected: FAIL — `m.tagMergeRow`/`m.tagRebaseRow` undefined.

- [ ] **Step 3: Implement the rows**

In `internal/tui/tags_actions.go`, add (the file already imports `tea` and `engine`):

```go
// tagMergeRow offers "Merge <tag> into current". SmartMerge with an empty Target
// defaults to the current branch; conflicts/dirty trees are handled by
// SmartMerge's Decider ladder (mapped to the TUI modal). Hidden on detached HEAD.
func (m Model) tagMergeRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	cur, attached := m.remoteCurrentBranch()
	if !attached {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-merge",
		label: "Merge " + name + " into current (" + cur + ")",
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.startOp(engine.SmartMerge{Source: name}) },
	}, true
}

// tagRebaseRow offers "Rebase current onto <tag>". Hidden on detached HEAD.
func (m Model) tagRebaseRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	cur, attached := m.remoteCurrentBranch()
	if !attached {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-rebase",
		label: "Rebase current (" + cur + ") onto " + name,
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.startOp(engine.SmartRebase{Onto: name}) },
	}, true
}
```

- [ ] **Step 4: Wire the rows into `availableActions`**

In `internal/tui/action_menu.go`, after the existing tag-row appends (`tagCheckoutRow`/`tagPushRow`/`tagDeleteRow`), add:

```go
	if r, ok := m.tagMergeRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.tagRebaseRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestTagMerge|TestTagRebase' -v`
Expected: PASS. Then `go test ./internal/tui/` for no regressions.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/tags_actions.go internal/tui/action_menu.go internal/tui/tags_actions_test.go
git commit  # "feat(tui): merge/rebase relative to a tag on the Tags . menu" + trailers
```

---

### Task 3: Docs

**Files:**
- Modify: `CHANGELOG.md` (Unreleased → Added)
- Modify: `README.md` (the Tags-tab `.`-menu description)

**Interfaces:** none.

- [ ] **Step 1: CHANGELOG**

Under `## [Unreleased]` → `### Added` in `CHANGELOG.md`, add:

```markdown
- **More Tags menu actions.** The Tags panel `.` menu now offers **Copy tag name**, **Copy commit id** / **Copy commit sha** (the tag's target commit, full SHA resolved on demand), plus one-click **Merge `<tag>` into current** and **Rebase current onto `<tag>`** (reusing SmartMerge/SmartRebase; merge/rebase hidden on a detached HEAD).
```

If a concurrent branch already opened the Added block, append the bullet; don't duplicate headings.

- [ ] **Step 2: README**

In `README.md`, find the Tags-tab description (grep for "The **Tags** tab lists tags"). Add that the Tags `.` menu now includes Copy tag name / commit id / commit sha and Merge/Rebase relative to the tag. Match the surrounding style.

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md README.md
git commit  # "docs: Tags menu copy + merge/rebase (Tags-A)" + trailers
```

---

## Self-review notes

- **Spec coverage:** Part 1 (copy group) → Task 1; Part 2 (merge/rebase) → Task 2; docs → Task 3. All covered.
- **Verified before writing:** `copyShaRow`/`domain.RevParse` exist (remote Bucket A); `remoteCurrentBranch()` exists in `remote_actions.go`; `model.Tag.Target` is the peeled commit (tag_parse.go); test helpers `footerModel`/`ids`/`findRow` + the `m.focus=panelTags` / `m.tags=` setup pattern (tags_actions_test.go); `shortHash` helper.
- **Type consistency:** row ids `copy-tag-name`/`copy-commit-id`/`copy-commit-sha`/`tag-merge`/`tag-rebase` stable across helpers, wiring, and tests; `copyShaRow(tag.Target, tag.Target)` identical in spec and Task 1.
- **Note:** `copy-commit-id`/`copy-commit-sha` ids are shared with the Branches/Remotes copy rows, but `contextCopyRows` returns at most one panel's rows per call (switch on `m.focus`), so there is no id collision within a single menu.
