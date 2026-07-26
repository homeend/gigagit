# Web UI: drag-and-drop branch merge/rebase, and the branch-menu parity backlog

Date: 2026-07-26
Branch: `feat/web-branch-dnd` (off `web-dev`)

Two things: the feature to build now (part A), and the ordered backlog it comes
out of (part B).

---

## Part A — drag-and-drop merge/rebase (this increment)

### Problem

The web sidebar's branch right-click menu offers `go to tip`, `switch to <b>`,
and `delete <b>`. There is no way to merge or rebase one branch onto another —
the TUI's core two-branch operation, reached there by marking a branch with `m`
and pairing it with a second (`pairOpsFor(panelBranches)`).

### Interaction

Branch rows in the sidebar become draggable. Dragging one and hovering another
highlights the hovered row; the drag source and every non-branch row refuse the
drop. Releasing opens the existing right-click menu at the drop point:

```
merge feat/x into web-dev
rebase feat/x onto web-dev
```

Direction is spelled out in the label, exactly as the TUI's `pairOp.label`
does, so marked-vs-selected never carries implicit meaning.

**The menu is the confirmation.** A labeled row plus a deliberate click is the
same standing the TUI's pair-op popup has. No second confirm dialog. Both ops
snapshot the target branch tip into `refs/gg/versions/<branch>/…` before
mutating, so both are recoverable.

Dropping a branch on itself opens no menu.

### Client (`internal/web/static/app.js`, `style.css`)

- Branch `<li>` rows get `draggable="true"`.
- `dragstart` stashes the source branch name in `state.dragBranch` and sets
  `dataTransfer` so the browser draws its own drag image.
- `dragover` calls `preventDefault()` — which is what makes a row a valid drop
  target — only when a drag is live and the hovered row is not the source, and
  adds a `.drop-target` class.
- `dragleave` / `dragend` clear the class and `state.dragBranch`.
- `drop` builds two items and hands them to the existing
  `showCtxMenu(items, x, y)`. No new overlay component and no new layer type:
  the ctx-menu is already a layer on the client layer stack.

### Server (`internal/web/ophttp.go`)

Two new cases on the existing op switch, plus one new request field (`onto`):

```go
case "merge":   // engine.SmartMerge{Source: req.Branch, Target: req.Onto}
case "rebase":  // engine.SmartRebase{Branch: req.Branch, Onto: req.Onto}
```

Both names are validated with `isGitArgSafe` — the `delete-branch` / `switch`
precedent. The engine then verifies both branches exist and refuses
source == target itself, so the handler does not duplicate that.

Nothing else is new. Both ops run through the existing `runOp` + SSE transport.

### Why the engine needs no help

`SmartMerge` is worktree-aware by design: target checked out here → merge in
place; target checked out in another worktree → merge there and stay put;
otherwise autostash, switch, merge, end on target. The web dispatches and gets
out of the way. `SmartRebase` is the same shape.

### Conflicts

A conflicted merge forks the `merge-conflict` decision (`keep-conflicts` /
`abort`); rebase forks its equivalent. Both park in the browser decision modal
that pull's non-fast-forward fork already exercises in production. No new
transport work.

**To verify during implementation, not assume:** `keep-conflicts` returns
`Result.Changed == true` *and* a non-nil error. Confirm what `runOp` reports
for that combination and make the browser say "conflicts left in the tree"
rather than a bare failure — the TUI and CLI both treat it as
success-with-conflicts.

### Testing

- `internal/web/opmerge_test.go`, `oprebase_test.go`, following
  `opdeletebranch_test.go`: real repo in `t.TempDir()`, POST `/api/op`, drain
  the SSE events, assert the result with git itself. Cover the bad-input 400
  and the source == target refusal.
- Client verified by hand plus a Playwright screenshot (rebuild, restart,
  hard-reload — see the project's web-verification notes).

---

## Part A2 — align the line-mode dot with the graph's leftmost lane

A small rendering fix that rides along with this increment.

### Problem

The commits pane has two modes (`state.graphMode`): `svg` (lane graph) and
`off` (flat ●-gutter list, the TUI `show_graph = off` parity). The dots do not
line up between them, so toggling the mode shifts the whole gutter.

Cause: the two modes render the dot with different machinery.

- `svg` mode: `graphSVG` draws `<circle cx="${x + HALF}">`, so the leftmost
  lane's centre sits at `CELL_W / 2` = **7px** from the `.graph` span's left
  edge (`CELL_W = 14`).
- `off` mode: `graphHTML` returns a text glyph, `<span class="flatdot">●</span>`,
  which starts at x=0 and centres wherever the font's advance width puts it —
  7px only by coincidence.
- The working-tree row (`wtRowHTML`) hard-codes a bare text `●` in **both**
  modes, so it aligns with neither.

### Fix

Make line mode use the graph's geometry instead of tuning a margin against it:
`graphHTML`'s `off` branch returns a **one-cell SVG** — `CELL_W × ROW_H`,
`<circle cx=HALF cy=MID r=4>`, filled with `laneColor(col >> 1)` from the same
`●` column lookup it already does. The centre is then `CELL_W / 2` **by
construction**, identical to the leftmost lane in graph mode, and cannot drift
when the font changes.

`wtRowHTML` gets the same one-cell SVG so the working-tree row's dot aligns in
both modes.

The now-redundant `.crow .graph .flatdot { margin-right: 10px }` rule goes; the
gap between the dot column and the subject moves onto `.crow .graph` so the
subject column starts at a consistent x in both modes.

Line mode keeps its point: the gutter is one cell wide, and the rest of the
lane column's space still goes to subjects.

### Verification

Toggle the mode with a commit selected and confirm the dot does not move
horizontally; check a working-tree row in both modes. A Playwright screenshot
per mode makes the comparison exact.

---

## Part B — branch-menu parity backlog

Every remaining TUI Branches-panel action, ordered by **verification cost**:
how much new machinery a reviewer has to trust before believing the row works.

Already in the web: `go to tip`, `switch`, `delete branch`.

### Tier 0 — client only. No server code, no Go test.

| # | Item | Note |
|---|------|------|
| 1 | **Resizable sidebar / panes** | Drag handle on the divider, width persisted under the existing `gg.sidebar.*` localStorage keys. Blocking today: long branch names are unreadable and nothing can be done about it. Supersedes porting the TUI's `[t]` / `[ctrl+t]` maximize toggles — real resizing beats a maximize toggle. |
| 2 | Copy branch name | `copyText()` already exists |
| 3 | Copy commit id | `branchRow.Hash` is already in the branches JSON |
| 4 | Copy worktree absolute path | Join `state.branches` ↔ `state.worktrees`, both already fetched; row appears only when the branch has a worktree |

### Tier 1 — one op-switch case + one Go test each. No new UI.

| # | Item | Op | Test shape |
|---|------|-----|-----------|
| 5 | Fetch (global, ☰ / palette) | `engine.Fetch{}` | zero args, decision-free — the cheapest op on the list |
| 6 | **merge** (drop menu) | `SmartMerge{Source, Target}` | this increment |
| 7 | **rebase** (drop menu) | `SmartRebase{Branch, Onto}` | this increment |
| 8 | Pull `<branch>` (stay here) | `SmartPull{Branch, Intent: PullInBackground}` | `oppull_test.go`'s HTTP git-server fixture; assert the branch ref moved and HEAD did not |
| 9 | Push `<branch>` | `Push{Remote:"origin", Branch, SetUpstream:true}` | generalizes today's current-branch-only case to a named, validated branch; `oppush_test.go` |

### Tier 1.5 — read-model state, no new view.

| # | Item | Note |
|---|------|------|
| 10 | **Branch solo mode** | `CommitFeed.ApplyScope(ctx, LogScope{Branches: []string{name}})` — the same call the TUI's solo uses, on the feed the web already owns. Three constraints: (a) the server holds **one** feed, so solo is server state, not per-tab — a second tab sees it too; (b) `done{changed:true}` currently resets the feed, which would silently drop the scope, so the scope must be re-applied on reset or a merge kicks you out of solo; (c) it needs a visible exit — a `solo: <branch> ✕` chip in the commits header. A mode you can enter and cannot see or leave is a trap. |

### Tier 2 — op + a reusable one-line prompt.

Build the prompt component once at #11; #12–13 are then near-free.

| # | Item | Note |
|---|------|------|
| 11 | Rename branch | `RenameBranch{Old, New}` — old from the row, new from the prompt. Simplest consumer, so the prompt gets built here |
| 12 | Create branch | `CreateBranch{Name, StartPoint}`; start point = the clicked branch |
| 13 | Create worktree | Path prompt plus the post-create-hook approval decision, which the existing parking modal handles. Slowest op to test |

### Tier 3 — new server read-model + new client view.

| # | Item | Note |
|---|------|------|
| 14 | Compare A ↔ B | `domain.CompareOrigins` / `CompareFiles` → changed-file list rendered through the diff pane that already exists. The natural third row in the drop menu |
| 15 | Previous versions… | One read endpoint (`BranchVersions`) + two ops (`RestoreBranchVersion`, `DeleteBranchVersion`) + a list popup |

### Tier 4 — own increments.

| # | Item | Note |
|---|------|------|
| 16 | Force push | The code is trivial (`Push{Force:true}`), but it **reverses a deliberate posture**: `Force` is currently never wire-settable, reachable only through the op's own parked push-rejected → push-force decisions. Needs an explicit decision before implementation |
| 17 | Review branch (AI) | Runs a configured external agent: long-running capture, report viewer, first-run approval consent. Hardest to verify — depends on an installed agent |
| 18 | Interactive rebase | Full plan editor (reorder / squash / drop). A multi-session feature on its own |

### Deliberately not porting

- `[m]ark` / pair — drag-and-drop is its replacement.
- `[t]` / `[ctrl+t]` maximize — superseded by #1, real pane resizing.

### The shape of the ordering

Items 1–9 need **zero new UI concepts**: each is a menu row plus an engine op
that already exists. The first genuinely new thing the web has to learn is
read-model state (#10), then a text prompt (#11), then a new view (#14).
