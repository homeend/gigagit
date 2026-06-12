# Branch Create/Delete — Design Spec

**Date:** 2026-06-12
**Status:** Approved (design agreed in chat; this document records it)
**Scope:** Create and delete local branches from the TUI Branches panel
(`b` / `B` / `d`) and a new `gg branch` CLI command, via two new engine
operations. First feature to exercise the agent-skill update convention
(`agentskill.Version` 1 → 2).

## Goal

From the Branches panel: `b` creates a branch off the selected branch, `B`
creates it and switches to it, `d` deletes the selected branch with the same
safety rails git gives (`-d` first, force only via an explicit decision).
Scripts and agents get the same through `gg branch create` / `gg branch
delete`, driven by the same operations and decision IDs.

## 1. Git verbs (`internal/git`)

Everything needed already exists except a start point on branch creation:

- **Extend `Repo.CreateBranch(ctx, name string)` → `CreateBranch(ctx, name,
  startPoint string)`** (`internal/git/mutate.go`). Empty `startPoint` means
  HEAD: `gitcmd.New("branch").Arg(name).ArgIf(startPoint != "", startPoint)`.
  Still one invocation. Callers today are tests only (5 call sites) — update
  them to pass `""`.
- `CheckRefFormatBranch` (worktree.go) — reused as the fast-fail validator.
- `DeleteBranch(ctx, name, force)` (worktree.go) — reused unchanged.
- `CurrentBranch`, `Worktrees` — reused for the delete guards.

No new verbs.

## 2. Engine operations (`internal/engine`)

### `CreateBranch` (`create_branch.go`)

```go
// CreateBranch creates a new local branch without switching to it.
type CreateBranch struct {
    Name       string // required
    StartPoint string // "" = HEAD
}
```

Run: guard `Name != ""`; `CheckRefFormatBranch` fast-fail (the CreateWorktree
precedent — clear message before touching refs); emit
`Progress{Step: "creating branch", Detail: name (+ " from " + start)}`; call
the verb. **An already-existing branch is refused by git itself** — wrap the
error, no separate existence probe (one-verb rule). No decisions. On success
`Result{Summary: "created branch " + name, Changed: true}` + `Done`.

### `DeleteBranch` (`delete_branch.go`)

```go
// DeleteBranch deletes a local branch. Force is resolved reactively via the
// Decider — only when git refuses the safe -d.
type DeleteBranch struct {
    Name string // required
}
```

Run, mirroring RemoveWorktree's shape:

1. **Guards (fail-fast, before any decision):**
   - `Name != ""`.
   - Not the current branch (`CurrentBranch`): "cannot delete the checked-out
     branch %s — switch away first".
   - Not checked out in any worktree (`Worktrees`; compare `wt.Branch`):
     "branch %s is checked out in worktree %s — remove the worktree first
     (d on the Worktrees panel / gg worktree remove)".
2. **Decision: confirm** — `ID: "delete-branch"`,
   `Prompt: "Delete branch <name>?"`, `Options: ["delete", "abort"]`.
   In the TUI a bare `d` keypress must not destroy a ref without one
   confirmation (the RemoveWorktree `remove-scope` precedent). The CLI
   pre-answers this (below) — typing the command *is* the confirmation.
   `abort` → `Result{Summary: "cancelled", Changed: false}`, no error.
3. `Progress{Step: "deleting branch", Detail: name}`; safe
   `DeleteBranch(name, false)`.
4. **On failure, reactive force** — **reuse the existing decision ID and
   options verbatim from RemoveWorktree** (decision IDs are cross-frontend
   API; one unmerged-branch fork, one shape):
   `ID: "branch-unmerged"`,
   `Prompt: "Branch <name> is not fully merged; force-delete discards its
   unmerged commits."`, `Options: ["force-delete", "keep"]`.
   `force-delete` → `DeleteBranch(name, true)`; `keep` →
   `Result{Summary: "kept branch " + name, Changed: false}`.
   (Esc in the TUI modal falls back to the last option = `keep` — safe.)
5. Success: `Result{Summary: "deleted branch " + name, Changed: true}` + `Done`.

> Deviation from the chat sketch, for the record: the unmerged fork uses
> `["force-delete","keep"]` (not `"abort"`) to match the already-shipped
> `branch-unmerged` decision in RemoveWorktree, and an upfront
> `delete-branch` confirm was added so a single TUI keypress can't delete a
> ref unconfirmed.

## 3. TUI (`internal/tui`)

Per the adding-tui-windows taxonomy: one transient **input popup** (create)
plus plain key-driven ops (delete). All Branches-panel actions resolve the
selected row through `backingIndex(panelBranches)` (filter/sort safe).

### Create popup (`branch_popup.go`, new)

- **`b`** (focus == Branches, not running/loading): opens the popup.
  Title: `Create branch from <selected>` where `<selected>` is the branch
  under the cursor (its name becomes `StartPoint`). One text input: the new
  branch name (typed runes, backspace; the worktreePopup editing precedent).
- **`B`**: same popup, but flagged **switch after create** — title
  `Create + switch branch from <selected>`.
- **enter** (non-empty name): close popup, `startOp(engine.CreateBranch{Name,
  StartPoint})`. If switch-after: set a new Model field
  `pendingSwitchBranch string`; in the `opFinishedMsg` success path, when set,
  clear it and `startOp(engine.SmartSwitch{Branch: name})` (the
  `pendingSwitch` worktree precedent — SmartSwitch's own decisions then flow
  through the normal modal). On op error the pending field is cleared, no
  switch.
- **esc** closes, ctrl+c quits; the popup swallows all other keys.
- Standard popup contract: pointer field `branchPopup *branchPopup` on Model;
  routing precedence: modal → worktree popup → repo popup → settings →
  **branch popup** → filterTyping → normal keys; rendered via the shared
  overlay helpers.

### Delete

- **`d`** (focus == Branches, not running/loading): with a valid
  `backingIndex`, `startOp(engine.DeleteBranch{Name: m.branches[bi].Name})`.
  The existing `d` case in model.go is currently gated to the Worktrees
  panel; it becomes panel-dispatched (Worktrees → RemoveWorktree, Branches →
  DeleteBranch). Confirm + unmerged forks arrive as ordinary decision modals.
- Branch list refreshes via the existing post-op reload.

### Footer

Footer hint (view.go) gains `[b] branch` and the `d` hint covers both panels
(e.g. `[d] delete`).

## 4. CLI — `gg branch` (`internal/cli/branch.go`, new)

Subcommand pattern mirrors `gg worktree`:

```
gg branch create <name> [<start-point>]
gg branch delete <name> [--force]
```

- **create:** runs `engine.CreateBranch` with a plain `cliDecider{}` (no
  decisions today). Wrong arg count → usage, exit 2.
- **delete:** policy map pre-answers the confirm —
  `{"delete-branch": "delete"}` (an explicit command is its own
  confirmation; the `gg worktree remove` flag-policy precedent). `--force`
  adds `{"branch-unmerged": "force-delete"}`. Without `--force` an unmerged
  branch follows the cliDecider contract: interactive TTY → prompt with the
  option list; non-TTY → exit 1 with the options on stderr (never hang,
  never guess).
- Registration: `commands` map + dispatch in `cli.Run`; `cmd/gg/main.go`
  help string gains `branch`.
- Exit codes: 0 ok (including `keep`/`abort` outcomes — user chose them),
  1 op failure, 2 usage.

## 5. Agent skill update (the convention's first exercise)

1. `internal/agentskill/using-gg.md`: add
   `gg branch create <name> [<start>]` and `gg branch delete <name>
   [--force]` to the command reference (delete bullet notes the
   `branch-unmerged` fork and `--force`). **Verify flags against the code
   before writing — the `-a` lesson.**
2. Bump `agentskill.Version` to **2**.
3. `gg init --update` on the dev machine; **commit** the regenerated
   `.claude/skills/using-gg/SKILL.md` (now stamped v2).

## 6. Files touched

| File | Change |
|------|--------|
| `internal/git/mutate.go` (+ test callers) | `CreateBranch` gains `startPoint`. |
| `internal/engine/create_branch.go` (new) | `CreateBranch` op. |
| `internal/engine/delete_branch.go` (new) | `DeleteBranch` op (guards + 2 decisions). |
| `internal/tui/branch_popup.go` (new) | Create-branch input popup (b/B). |
| `internal/tui/model.go` | `b`/`B` keys, panel-dispatched `d`, routing, `pendingSwitchBranch` chaining. |
| `internal/tui/view.go` | Footer hints, popup overlay. |
| `internal/cli/branch.go` (new) | `gg branch create|delete`. |
| `internal/cli/cli.go`, `cmd/gg/main.go` | Registration + help. |
| `internal/agentskill/using-gg.md`, `agentskill.go` | Command reference + `Version = 2`. |
| `.claude/skills/using-gg/SKILL.md` | Regenerated v2 (committed). |
| `CHANGELOG.md`, `README.md` | Feature entry; keys + CLI tables. |

## 7. Testing

- **git verb:** `CreateBranch` with and without start point (real repo in
  `t.TempDir()`); argv assertion via `FakeRunner` for the start-point form.
- **engine CreateBranch:** creates at HEAD; creates from a start point
  (branch tip differs from HEAD); invalid name fails fast with no decision;
  existing name surfaces git's error; `Changed: true` + Done on success.
- **engine DeleteBranch:** current-branch guard; checked-out-in-worktree
  guard (add a linked worktree, try deleting its branch); confirm `abort` →
  cancelled, branch still exists; merged branch deletes with confirm
  `delete`; unmerged branch → `branch-unmerged` fork, `keep` keeps it,
  `force-delete` removes it (scriptDecider, the existing engine test
  pattern).
- **TUI:** `b` on Branches opens the popup with the selected branch (incl.
  under filter/sort — backingIndex); typing + enter starts CreateBranch with
  the right fields; `B` chains SmartSwitch after success
  (`pendingSwitchBranch`); esc closes without an op; `d` on Branches starts
  DeleteBranch for the visible row; `d` on Worktrees still removes worktrees;
  popup swallows keys; `b` is inert on other panels.
- **CLI:** `gg branch create` happy path + arg-count usage errors;
  `gg branch delete` deletes merged without prompting (confirm pre-answered);
  unmerged + `--force` deletes; unmerged non-TTY without `--force` exits 1
  listing the options; unknown subcommand exits 2.
- **agentskill:** body mentions `gg branch`; `Version == 2`.

Quality gates as always: `go test ./...` (`-race` before merging), `go vet`,
`gofmt -l internal cmd` clean.

## 8. Out of scope (YAGNI)

- `gg branch list` / TUI branch listing changes (the panel already lists).
- Rename, remote branch deletion (`push --delete`), tracking setup.
- Creating from arbitrary refs/commits in the TUI (CLI's `<start-point>`
  accepts anything git does; the TUI offers the selected branch).
- `SmartMerge` (separate roadmap candidate).
