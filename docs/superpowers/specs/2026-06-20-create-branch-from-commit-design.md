# Create branch from a commit — design

**Pipeline #5.** Create a new branch starting at the selected commit, from the
Commits panel.

## Almost everything already exists

The whole engine→git→CLI stack is already in place and tested:
- `git.CreateBranch(ctx, name, startPoint)` verb.
- `engine.CreateBranch{Name, StartPoint}` op (StartPoint `""` = HEAD; validates
  the name via `CheckRefFormatBranch`).
- The TUI `branchPopup` dialog (text input → `engine.CreateBranch` via `startOp`),
  today opened from the **Branches** panel with the selected branch as the start
  point.
- CLI `gg branch create <name> [<start-point>]` already accepts a start-point.

So this feature is **only a TUI addition**: a Commits-panel `.`-menu action that
opens the *same* `branchPopup` with the selected commit's hash as the start point.

## The action — `commitCreateBranchRow` (`internal/tui/commit_scope.go`)

```go
func (m Model) commitCreateBranchRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash // full 40-char SHA (unambiguous start-point)
	return actionRow{
		id:    "commit-create-branch",
		label: "Create branch here",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m = m.pushOverlay(&branchPopup{startPoint: hash})
			return m, nil
		},
	}, true
}
```

Wire into `availableActions` (with the other commit-row actions). No keybinding →
menu-only + help.go.

## Display: shorten the SHA in the popup title

`branchPopup.box` titles "Create branch from <startPoint>". A full 40-char SHA is
ugly there, so shorten a full hex SHA to 7 chars **for display only** (the op
still receives the full, unambiguous hash — important in a huge repo where a
7-char prefix could collide):

```go
func displayStart(s string) string {
	if len(s) == 40 && isHex(s) {
		return s[:7]
	}
	return s
}
```

Use `displayStart(p.startPoint)` in the title. Branch start-points (branch names)
render unchanged.

## Testing (TDD)

- `commitCreateBranchRow` present on the Commits panel with a selected commit;
  running it pushes a `branchPopup` whose `startPoint` is the commit's **full**
  hash.
- Absent off the Commits panel / when no commit is selected / when an op is
  running (`opsIdle`).
- `displayStart` shortens a 40-hex SHA to 7 chars and leaves a branch name alone.

The engine op + git verb + CLI are already covered by their existing tests; the
branch is created through the proven `branchPopup`→`startOp`→`engine.CreateBranch`
path, so no new engine/CLI/e2e is needed.

## Out of scope

- A "create + switch" variant from a commit (the Branches popup has `switchAfter`;
  not exposed for commits here — keep v1 to plain create).
