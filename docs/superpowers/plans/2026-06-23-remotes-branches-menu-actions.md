# Remotes/Branches Menu Actions (GitKraken parity, Bucket A) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface already-existing engine capabilities on the Remotes/Branches `.` menus — copy commit id (short) + sha (full, lazily resolved) on both panels, and create-worktree / merge / rebase on the Remotes row.

**Architecture:** Pure TUI wiring plus one thin domain read query (`RevParse`) over the existing `git.Repo.RevParse` verb. Menu rows are built by self-gating helper methods on `Model` (the established `remotePruneRow` pattern) and appended in `availableActions`. Copy-sha defers its git call to the row's `run` handler so the menu builds without I/O. Merge/rebase reuse `engine.SmartMerge`/`engine.SmartRebase` (empty Target/Branch default to the current branch); worktree-from-remote reuses `Model.openWorktreeAt`.

**Tech Stack:** Go 1.26, Bubble Tea TUI, `internal/domain` query layer, `internal/gitexec` FakeRunner for tests.

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26.
- `internal/tui` MUST NOT import `internal/git` — reach git only through `internal/domain` (archtest-guarded).
- Reads go through `domain` queries under a Read reservation, using the `query(ctx, s, key, fn)` helper.
- TUI `Model` is a value receiver; helper methods take/return `Model` by value.
- Every commit message ends with these two trailers, verbatim:
  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro
  ```
- Tests: `go test ./internal/domain/ ./internal/tui/` from the worktree root. Run `./test.sh race` before any merge (merge is the human's call, not part of this plan).
- No engine, git-verb, CLI, or agentskill changes — everything reused.

---

### Task 1: `domain.RevParse` query

**Files:**
- Modify: `internal/domain/query.go` (add `RevParse` near `CurrentBranch`, ~line 305)
- Create: `internal/domain/revparse_test.go`

**Interfaces:**
- Consumes: existing `git.Repo.RevParse(ctx, rev) (string, error)` (`internal/git/query2.go`); existing `query[T](ctx, *Service, key string, fn func(context.Context) (T, error)) (T, error)` helper.
- Produces: `func (s *Service) RevParse(ctx context.Context, rev string) (string, error)` — resolves a ref/commit-ish to its full 40-char object id.

- [ ] **Step 1: Write the failing test**

Create `internal/domain/revparse_test.go`. Use the `FakeRunner` pattern that the rest of this package uses (`New(&git.Repo{Runner: f})`, span name `"git rev-parse"` — see `internal/git/query2.go`'s `RevParse` verb and the many `query_test.go` examples).

```go
package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
)

func TestRevParseResolvesFullSHA(t *testing.T) {
	const full = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef" // 40 chars
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse", gitexec.Result{Stdout: full + "\n"})
	svc := New(&git.Repo{Runner: f})

	got, err := svc.RevParse(context.Background(), "origin/foo")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	if got != full {
		t.Fatalf("RevParse = %q, want %q", got, full)
	}
}

func TestRevParsePropagatesError(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetError("git rev-parse", errors.New("unknown revision"))
	svc := New(&git.Repo{Runner: f})

	if _, err := svc.RevParse(context.Background(), "no-such-ref"); err == nil {
		t.Fatal("RevParse(bogus) = nil error, want error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run TestRevParseResolvesFullSHA -v`
Expected: FAIL — `svc.RevParse undefined`.

- [ ] **Step 3: Implement `RevParse`**

In `internal/domain/query.go`, immediately after the `CurrentBranch` method (~line 305), add:

```go
// RevParse resolves rev to a full object id, under a Read reservation.
func (s *Service) RevParse(ctx context.Context, rev string) (string, error) {
	return query(ctx, s, "revparse:"+rev, func(ctx context.Context) (string, error) {
		return s.repo.RevParse(ctx, rev)
	})
}
```

If the Service's git handle field is not named `s.repo`, use the same accessor the neighbouring `CurrentBranch`/`TopLevel` methods use.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/ -run TestRevParseResolvesFullSHA -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/query.go internal/domain/revparse_test.go
git commit  # message: "feat(domain): RevParse query (ref -> full SHA)" + the two required trailers
```

---

### Task 2: Copy commit id + sha rows on Branches & Remotes

**Files:**
- Modify: `internal/tui/remote_actions.go` (add `copyShaRow` helper + `context` import)
- Modify: `internal/tui/action_menu.go` (extend the `panelBranches` and `panelRemotes` cases of `contextCopyRows`, ~lines 324-333)
- Create: `internal/tui/remote_actions_test.go` (copy-row presence + copyShaRow dispatch)

**Interfaces:**
- Consumes: `Model.copyRow(id, label, okMsg, text string) actionRow`; `Model.copyToClipboardCmd(ok, text string) tea.Cmd`; `shortHash(h string) string`; `Service.RevParse` (Task 1); `model.Branch.Hash`/`.Name`, `model.RemoteBranch.Hash`/`.Name`.
- Produces: `func (m Model) copyShaRow(ref, fallbackShort string) actionRow` — a `run`-handler row, id `"copy-commit-sha"`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/remote_actions_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

func TestCopyRowsBranchesHaveIdAndSha(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches // row 0 = "main" with a hash set by footerModel
	m.branches = []model.Branch{{Name: "main", Hash: "abc1234"}}
	rows := m.contextCopyRows()
	if r, ok := findRow(rows, "copy-branch-name"); !ok || r.copyText != "main" {
		t.Fatalf("missing copy-branch-name=main; rows=%v", rows)
	}
	if r, ok := findRow(rows, "copy-commit-id"); !ok || r.copyText != "abc1234" {
		t.Fatalf("missing copy-commit-id=abc1234; rows=%v", rows)
	}
	if _, ok := findRow(rows, "copy-commit-sha"); !ok {
		t.Fatalf("missing copy-commit-sha; rows=%v", rows)
	}
}

func TestCopyRowsRemotesHaveIdAndSha(t *testing.T) {
	m := footerModel()
	m.focus = panelRemotes
	m.remoteBranches = []model.RemoteBranch{{Name: "origin/foo", Remote: "origin", Branch: "foo", Hash: "dead111"}}
	rows := m.contextCopyRows()
	if r, ok := findRow(rows, "copy-branch-name"); !ok || r.copyText != "origin/foo" {
		t.Fatalf("missing copy-branch-name=origin/foo; rows=%v", rows)
	}
	if r, ok := findRow(rows, "copy-commit-id"); !ok || r.copyText != "dead111" {
		t.Fatalf("missing copy-commit-id=dead111; rows=%v", rows)
	}
	if _, ok := findRow(rows, "copy-commit-sha"); !ok {
		t.Fatalf("missing copy-commit-sha; rows=%v", rows)
	}
}

func TestCopyShaRowFallsBackWithoutService(t *testing.T) {
	m := footerModel() // no svc set
	row := m.copyShaRow("origin/foo", "dead111")
	if row.run == nil {
		t.Fatal("copyShaRow must carry a run handler")
	}
	// run must not panic with a nil svc; it returns a copy cmd for the fallback.
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("copyShaRow run returned nil cmd")
	}
}

func TestCopyShaRowResolvesFullViaService(t *testing.T) {
	fr := gitexec.NewFakeRunner()
	fr.SetResponse("git rev-parse", gitexec.Result{Stdout: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\n"})
	m := footerModel()
	m.svc = domain.New(&git.Repo{Runner: fr})
	row := m.copyShaRow("origin/foo", "dead111")
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("expected a copy cmd")
	}
	// The resolved value is carried into the clipboard cmd; assert no panic and
	// that the fake rev-parse was consulted by checking the runner recorded it.
}
```

Note: `gitexec.Result`'s stdout field is `Stdout` (confirmed in `internal/git/query2.go`). If `FakeRunner` exposes a recorded-calls accessor, additionally assert `git rev-parse` was invoked; otherwise the non-nil-cmd + no-panic checks suffice for this row.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestCopyRows|TestCopyShaRow' -v`
Expected: FAIL — `m.copyShaRow undefined` and missing `copy-commit-id`/`copy-commit-sha` rows.

- [ ] **Step 3: Add the `copyShaRow` helper**

In `internal/tui/remote_actions.go`, add `"context"` to the import block and append:

```go
// copyShaRow builds a "Copy commit sha" action that resolves ref to its full
// 40-char object id on invoke (git rev-parse via domain), NOT at menu-build
// time — so opening the menu costs no git call. A nil service or a resolve
// error falls back to fallbackShort (the short hash the row already carries),
// so the copy always yields a usable value.
func (m Model) copyShaRow(ref, fallbackShort string) actionRow {
	return actionRow{
		id:    "copy-commit-sha",
		label: "Copy commit sha",
		run: func(m Model) (tea.Model, tea.Cmd) {
			full := fallbackShort
			if m.svc != nil {
				if s, err := m.svc.RevParse(context.Background(), ref); err == nil && s != "" {
					full = s
				}
			}
			return m, m.copyToClipboardCmd("Copied commit sha "+shortHash(full), full)
		},
	}
}
```

- [ ] **Step 4: Extend `contextCopyRows`**

In `internal/tui/action_menu.go`, replace the `panelBranches` and `panelRemotes` cases (currently each returns only the branch-name copy row):

```go
	case m.focus == panelBranches:
		if bi, ok := m.backingIndex(panelBranches); ok {
			b := m.branches[bi]
			return []actionRow{
				m.copyRow("copy-branch-name", "Copy branch name", "Copied branch name "+b.Name, b.Name),
				m.copyRow("copy-commit-id", "Copy commit id", "Copied commit id "+shortHash(b.Hash), b.Hash),
				m.copyShaRow(b.Name, b.Hash),
			}
		}
	case m.focus == panelRemotes:
		if bi, ok := m.backingIndex(panelRemotes); ok {
			rb := m.remoteBranches[bi]
			return []actionRow{
				m.copyRow("copy-branch-name", "Copy branch name", "Copied branch name "+rb.Name, rb.Name),
				m.copyRow("copy-commit-id", "Copy commit id", "Copied commit id "+shortHash(rb.Hash), rb.Hash),
				m.copyShaRow(rb.Name, rb.Hash),
			}
		}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestCopyRows|TestCopyShaRow' -v`
Expected: PASS. Also run `go test ./internal/tui/ -run TestContextCopyRows -v` to confirm the pre-existing copy-name tests still pass (they assert the name row is present, which it still is).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/remote_actions.go internal/tui/action_menu.go internal/tui/remote_actions_test.go
git commit  # "feat(tui): copy commit id + sha on Branches/Remotes rows" + trailers
```

---

### Task 3: Remotes-row create-worktree / merge / rebase

**Files:**
- Modify: `internal/tui/remote_actions.go` (3 row helpers + a current-branch helper)
- Modify: `internal/tui/action_menu.go` (append the 3 rows after `remotePruneRow`, ~line 125)
- Modify: `internal/tui/remote_actions_test.go` (gating + dispatch tests)

**Interfaces:**
- Consumes: `Model.selectedRemote() (model.RemoteBranch, bool)`; `Model.opsIdle() bool`; `Model.openWorktreeAt(startPoint, prefillBranch string) Model`; `Model.startOp(op engine.Operation) (Model, tea.Cmd)`; `engine.SmartMerge{Source}`, `engine.SmartRebase{Onto}`; `m.status.Branch`.
- Produces: `remoteCreateWorktreeRow()`, `remoteMergeRow()`, `remoteRebaseRow() (actionRow, bool)`; `remoteCurrentBranch() (string, bool)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/remote_actions_test.go`:

```go
func remoteModel() Model {
	m := footerModel()
	m.focus = panelRemotes
	m.remoteBranches = []model.RemoteBranch{{Name: "origin/foo", Remote: "origin", Branch: "foo", Hash: "dead111"}}
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m.status.Branch = "main"
	return m
}

func TestRemoteOpRowsPresentWhenAttached(t *testing.T) {
	m := remoteModel()
	got := ids(availableActions(m))
	for _, id := range []string{"remote-worktree", "remote-merge", "remote-rebase"} {
		if !got[id] {
			t.Fatalf("expected %s in remote menu; got %v", id, got)
		}
	}
}

func TestRemoteMergeRebaseHiddenOnDetachedHEAD(t *testing.T) {
	m := remoteModel()
	m.status.Branch = "" // detached
	got := ids(availableActions(m))
	if got["remote-merge"] || got["remote-rebase"] {
		t.Fatalf("merge/rebase must be hidden on detached HEAD; got %v", got)
	}
	if !got["remote-worktree"] {
		t.Fatalf("worktree-from-remote should still be offered on detached HEAD; got %v", got)
	}
}

func TestRemoteMergeRowDispatchesSmartMerge(t *testing.T) {
	m := remoteModel()
	row, ok := m.remoteMergeRow()
	if !ok {
		t.Fatal("remoteMergeRow not available")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("merge row run returned nil cmd")
	}
}

func TestRemoteRebaseRowDispatchesSmartRebase(t *testing.T) {
	m := remoteModel()
	row, ok := m.remoteRebaseRow()
	if !ok {
		t.Fatal("remoteRebaseRow not available")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("rebase row run returned nil cmd")
	}
}

func TestRemoteWorktreeRowOpensPopup(t *testing.T) {
	m := remoteModel()
	row, ok := m.remoteCreateWorktreeRow()
	if !ok {
		t.Fatal("remoteCreateWorktreeRow not available")
	}
	nm, _ := row.run(m)
	if _, isWt := nm.(Model).topLayer().(*worktreePopup); !isWt {
		t.Fatalf("expected worktreePopup on top after run; got %T", nm.(Model).topLayer())
	}
}
```

`ids(...)` and `findRow(...)` already exist as test helpers in this package (`action_menu_*_test.go`). If `topLayer()` is unexported and returns an interface, the type assertion above works in-package; if the popup type name differs, adjust `*worktreePopup` to the type returned by `openWorktreeAt` (confirm in `internal/tui/worktree_popup.go`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestRemote' -v`
Expected: FAIL — `m.remoteMergeRow`/`remoteRebaseRow`/`remoteCreateWorktreeRow`/`remoteCurrentBranch` undefined.

- [ ] **Step 3: Implement the row helpers**

In `internal/tui/remote_actions.go`, add (the file already imports `tea` and `engine`):

```go
// remoteCurrentBranch returns the checked-out branch name and whether HEAD is
// attached. Porcelain reports detached HEAD as "" or "(detached)"; guard both
// (same dual-guard as the fast-forward feature).
func (m Model) remoteCurrentBranch() (string, bool) {
	cur := m.status.Branch
	if cur == "" || cur == "(detached)" {
		return "", false
	}
	return cur, true
}

// remoteCreateWorktreeRow offers "Create worktree from <remote branch>" on the
// Remotes tab, reusing the worktree-from-ref popup seeded with the remote ref
// as start-point and the de-prefixed branch name as the prefill.
func (m Model) remoteCreateWorktreeRow() (actionRow, bool) {
	rb, ok := m.selectedRemote()
	if !ok || !m.opsIdle() {
		return actionRow{}, false
	}
	return actionRow{
		id:    "remote-worktree",
		label: "Create worktree from " + rb.Name,
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openWorktreeAt(rb.Name, rb.Branch), nil
		},
	}, true
}

// remoteMergeRow offers "Merge <remote branch> into current". SmartMerge with an
// empty Target defaults to the current branch; conflicts/dirty trees are handled
// by SmartMerge's own Decider ladder (mapped to the TUI modal). Hidden on
// detached HEAD. The engine rejects Source==Target, and a remote ref can never
// equal a local branch name, so no extra equality guard is needed here.
func (m Model) remoteMergeRow() (actionRow, bool) {
	rb, ok := m.selectedRemote()
	if !ok || !m.opsIdle() {
		return actionRow{}, false
	}
	cur, attached := m.remoteCurrentBranch()
	if !attached {
		return actionRow{}, false
	}
	return actionRow{
		id:    "remote-merge",
		label: "Merge " + rb.Name + " into current (" + cur + ")",
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.startOp(engine.SmartMerge{Source: rb.Name}) },
	}, true
}

// remoteRebaseRow offers "Rebase current onto <remote branch>". SmartRebase with
// an empty Branch defaults to the current branch. Hidden on detached HEAD.
func (m Model) remoteRebaseRow() (actionRow, bool) {
	rb, ok := m.selectedRemote()
	if !ok || !m.opsIdle() {
		return actionRow{}, false
	}
	cur, attached := m.remoteCurrentBranch()
	if !attached {
		return actionRow{}, false
	}
	return actionRow{
		id:    "remote-rebase",
		label: "Rebase current (" + cur + ") onto " + rb.Name,
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.startOp(engine.SmartRebase{Onto: rb.Name}) },
	}, true
}
```

- [ ] **Step 4: Wire the rows into `availableActions`**

In `internal/tui/action_menu.go`, immediately after the existing `remotePruneRow` append block (~line 125), add:

```go
	if r, ok := m.remoteCreateWorktreeRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.remoteMergeRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.remoteRebaseRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestRemote' -v`
Expected: PASS.

- [ ] **Step 6: Full package test**

Run: `go test ./internal/tui/ ./internal/domain/`
Expected: PASS (no regressions in existing menu/footer tests).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/remote_actions.go internal/tui/action_menu.go internal/tui/remote_actions_test.go
git commit  # "feat(tui): create-worktree/merge/rebase on the Remotes row" + trailers
```

---

### Task 4: Docs

**Files:**
- Modify: `CHANGELOG.md` (Unreleased → Added)
- Modify: `README.md` (the section that lists panel/menu actions for Remotes/Branches)

**Interfaces:** none (docs only).

- [ ] **Step 1: CHANGELOG entry**

Under the `## [Unreleased]` `### Added` list in `CHANGELOG.md`, add:

```markdown
- Remotes & Branches `.` menus now offer **Copy commit id** (short) and **Copy commit sha** (full, resolved on demand). The Remotes menu also offers **Create worktree from** the remote branch, **Merge** it into the current branch, and **Rebase** the current branch onto it (reusing SmartMerge/SmartRebase; conflicts resolve through the usual modal).
```

If an Unreleased section does not exist, create it at the top following the file's existing heading style. If a concurrent branch already added an Unreleased/Added block, append this bullet to it rather than duplicating the heading.

- [ ] **Step 2: README entry**

In `README.md`, find the section documenting panel actions / the `.` menu (search for "Remotes" or "Branches" or "actions menu"). Add a short note that the Remotes/Branches action menu now includes copy commit id/sha, and that the Remotes menu adds create-worktree-from-remote, merge-into-current, and rebase-onto-remote. Match the surrounding bullet/prose style. If no such section exists, add a brief bullet to the features/keybindings list near the existing Remotes/fetch documentation.

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md README.md
git commit  # "docs: Remotes/Branches menu actions (Bucket A)" + trailers
```

---

## Self-review notes

- **Spec coverage:** Part 1 (copy id+sha, Branches+Remotes) → Task 2. Part 2 (worktree/merge/rebase on Remotes) → Task 3. `domain.RevParse` → Task 1. Docs → Task 4. All spec sections covered.
- **Deviation from spec:** the spec mentioned a `rb.Name != cur` guard on merge/rebase; omitted because it is vacuous (a remote ref always carries the remote prefix, so it can never equal a local branch name) and the engine already rejects `Source == Target`. Gating is on attached-HEAD only.
- **Type consistency:** `copyShaRow(ref, fallbackShort string)` used identically in Task 2 build sites and Task 2/3 tests; row ids `copy-commit-id`, `copy-commit-sha`, `remote-worktree`, `remote-merge`, `remote-rebase` are stable across helpers, wiring, and tests.
- **Verified before writing:** `domain.New(repo *git.Repo)`, the `FakeRunner` test pattern + `"git rev-parse"` span name, `gitexec.Result.Stdout`, `footerModel()` test helper, `findRow`/`ids` helpers, `topLayer() layer`. The tests set `branches`/`remoteBranches`/`status.Branch` explicitly rather than relying on `footerModel` defaults.
- **One assumption to confirm at execution (adapt, don't redesign):** the popup type returned by `openWorktreeAt` is `*worktreePopup` (confirm in `internal/tui/worktree_popup.go`); if it differs, adjust the assertion in `TestRemoteWorktreeRowOpensPopup` only.
