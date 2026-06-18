# Remotes tab (read-only) + behind-indicator — design

Date: 2026-06-18
Status: approved (brainstorm); ready for plan
Branch/worktree: `worktree-remotes-tab`

## Summary

Add a third tab — **Remotes** — to the shared left tab slot that today toggles
between Branches and Worktrees, listing the repo's remote-tracking branches
(`refs/remotes/*`). Separately, decorate each row on the local **Branches** tab
with a `(↓N)` indicator when that branch is behind its upstream.

Both are **read-only, render-only** changes. There is no engine operation, no
commit-feed change, and no fetch in this chunk. Checkout (`c`/`s`), the
ephemeral commit preview, and fetch/prune are deliberately deferred to later
features (see *Out of scope*).

This is the first chunk of the larger "remote branches" effort
(`2026-06-18-remotes-tab-design.md` is chunk 1).

## Goals

1. A **Remotes** tab, ordered `Branches · Remotes · Worktrees`, reachable via the
   existing `ctrl+←/→` tab cycle, listing remote-tracking branches.
2. A **behind-indicator** `(↓N)` after a local branch's name on the Branches tab
   whenever `Branch.Behind > 0`.

## Non-goals / Out of scope (later chunks)

- `c` (checkout, stay) / `s` (checkout-and-switch) on a remote branch — needs a
  new fast-forward-safe `SmartCheckout` engine op.
- Ephemeral commit-preview feed when a remote branch is selected — needs the
  commit log rev-parameterized.
- Fetch + prune on the Remotes tab.
- GitKraken-inspired large-repo features (sparse-checkout, smart branch
  visibility, pin/favorite branches, auto-fetch throttle). Roadmap appendix only.

## Large-repo cost model (why no streaming loader)

`for-each-ref refs/remotes` is cheap even in a 100GB monorepo: refs are tiny
metadata; the expensive part of a big repo is *objects*, which we don't touch
here. The "stream by 50" requirement is satisfied without a streaming
ref-loader:

- The branch **list** is loaded in one `for-each-ref` and displayed through the
  panels' existing row virtualization (`panelRowsCap` / windowing).
- The commit **preview** (a later chunk) reuses the commit feed's existing
  50/200 paging.

So: **no streaming ref-loader is built.** One invocation, windowed display.

## Architecture by layer

The Remotes tab mirrors the existing Branches tab at every layer; the
behind-indicator is a one-spot render change.

### model — `internal/model/model.go`

New type, kept distinct from `Branch` (remote refs carry no
upstream/ahead/behind of their own — forcing them onto `Branch` would be
misleading):

```go
// RemoteBranch is one entry from `git for-each-ref refs/remotes`.
type RemoteBranch struct {
    Name     string // short ref, e.g. "origin/feature/x"
    Remote   string // "origin"
    Branch   string // "feature/x" (Name with the remote prefix removed)
    Hash     string // short object name
    UnixTime int64  // committer time (unix seconds); 0 if unknown
}
```

### git — verb + parser

`internal/git/repo.go`: new verb parallel to `Branches()` (repo.go:29):

```go
func (r *Repo) RemoteBranches(ctx context.Context) ([]model.RemoteBranch, error) {
    const format = "%(refname:short)%00%(objectname:short)%00%(committerdate:unix)"
    argv := gitcmd.New("for-each-ref").Arg("--format="+format, "refs/remotes").ToArgv()
    res, err := r.Runner.Run(ctx, "git for-each-ref (remotes)", argv)
    ...
    return ParseRemoteBranches([]byte(res.Stdout))
}
```

New `ParseRemoteBranches` (new file `internal/git/remote_parse.go`, mirroring
`branch_parse.go`):

- Split each line on `\x00` → `refname:short`, `objectname:short`,
  `committerdate:unix`.
- `Remote` / `Branch` split on the **first** `/` in `refname:short`
  (`origin/feature/x` → remote `origin`, branch `feature/x`).
- **Filter out the `*/HEAD` symbolic ref** (e.g. `origin/HEAD`) — it's a pointer,
  not a branch, and would otherwise show as a duplicate of the default branch.

### domain — query + Snapshot

`internal/domain/query.go`: add a `RemoteBranches(ctx)` query running under a
**Read** reservation, parallel + singleflight-coalesced, exactly like the
existing `Branches`/`Worktrees` queries. Fold the result into the same
`Snapshot` load the TUI uses at startup/reload so remote branches arrive in the
same `dataLoadedMsg` (no extra round trip). The `Snapshot` result struct gains a
`RemoteBranches []model.RemoteBranch` field.

### tui — enum, tab cycle, list, render

- **`internal/tui/model.go`**:
  - New `panelRemotes` enum member (placed after `panelWorktrees`, before
    `panelFiles`, so the left-tab panels stay grouped; bump nothing else — maps
    are keyed by `panel`). Enum *value* order does not drive display order.
  - New field `remoteBranches []model.RemoteBranch`.
  - **Explicit tab order**: `var leftTabs = []panel{panelBranches, panelRemotes,
    panelWorktrees}`. Rework the `ctrl+←/→` handler (today the hard-coded
    "two tabs, either direction toggles" at ~model.go:566) into a real
    directional cycle over `leftTabs`: `ctrl+→` advances, `ctrl+←` retreats,
    wrapping; then focus the now-active tab.
- **`internal/tui/load.go`** (the `dataLoadedMsg` apply, ~model.go:226): store
  `msg.remoteBranches` into `m.remoteBranches`.
- **`internal/tui/viewstate.go`**:
  - New `remoteBranchList` implementing `panelList` (`Len`/`Row`/`Name`/`Date`),
    mirroring `branchList` (viewstate.go:146). `Name(i)` returns the full
    `origin/...` ref so the `/` filter matches on it; `Date(i)` returns
    `UnixTime` so date sort works.
  - `listFor` gains a `case panelRemotes:` returning
    `remoteBranchList{items: m.remoteBranches, rows: m.remoteRows()}`.
- **`internal/tui/view.go`**:
  - New `remoteRows()` mirroring `branchRows()`: `"  " + rb.Name` per row (no
    HEAD marker; remotes have no current-branch concept). Worktree marker is
    not applicable.
  - `tabBarLabel(active)` becomes 3-way, bracketing the active tab among
    `Branches`, `Remotes`, `Worktrees` (plain ASCII, truncate-safe).
  - **Behind-indicator**: in `branchRows()` (view.go:581), when `b.Behind > 0`
    append `" (↓" + strconv.Itoa(b.Behind) + ")"` to the row. (Ahead is already
    shown for HEAD in the header; this chunk adds *behind* only, per request.)

### Focus / nav / help

- The active-tab slot already participates in focus cycling
  (`[activeTab, Status/Files, Staged, Commits]`); since Remotes occupies the
  same slot as Branches/Worktrees, no new focus-order entry is needed — it's the
  same slot, different content. Verify the focus-order tests (`nav_test.go`,
  `pgnav_test.go`, `focus_test.go`) still hold with a third tab and update the
  ones that assert the two-tab toggle wording.
- **`internal/tui/help.go`** (the `?` pane) and the context-help **footer**: the
  `ctrl+←/→` hint already exists; update any "Branches/Worktrees" copy that
  enumerates the two tabs to include Remotes. No new keybinding is introduced in
  this chunk, so footer/menu binding tables are unchanged.

## Testing (TDD)

- **git parser** (`internal/git/remote_parse_test.go`): multi-remote output,
  branch names containing `/`, the `origin/HEAD` symref filtered out, empty
  input, malformed line skipped. Mirror `branch_parse_test.go`.
- **git verb** (`repo_test.go` or `FakeRunner`): asserts the `for-each-ref
  refs/remotes` argv and format string.
- **domain** (`query` test): `RemoteBranches` returns parsed entries under a Read
  reservation; Snapshot includes them.
- **tui render** (`fit_test.go`-style): the Remotes tab renders its rows; the
  3-way `tabBarLabel` brackets the active tab; `remoteRows` content.
- **tui nav** (`nav_test.go`): `ctrl+→`/`ctrl+←` cycles `Branches → Remotes →
  Worktrees → Branches` and back, focusing the active tab.
- **behind-indicator** (branch render test): a branch with `Behind=3` renders
  `(↓3)`; `Behind=0` renders no indicator.

## Risks / watch-items

- **Enum insertion** ripples: any code that ranges panels by value or assumes
  `panelFiles == panelWorktrees+1` must be found and updated. Grep for
  arithmetic on panel constants before inserting.
- **Test naming**: do NOT end new test files with a `_GOOS`/`_GOARCH` token
  before `_test.go` (e.g. `_remotes_test.go` is fine; `_remote_test.go` is fine;
  avoid `_windows`/`_linux`/etc.) — such files silently don't compile on other
  platforms and produce a false "ok".
- **Filter on `/`**: `remoteBranchList.Name` returns the full `origin/...` ref so
  the `/` substring filter behaves like Branches.

## Slicing for the plan

Small enough for one branch; suggested commit order:

1. model + git verb + parser (+ tests).
2. domain query + Snapshot wiring (+ tests).
3. tui enum + tab cycle + list + render + behind-indicator (+ tests).
4. help/footer copy + focus-test updates; docs (CHANGELOG, README if surface
   copy changed).
