# Go to branch tip in commits — design

**Pipeline #4.** "I want to hit go-to-branch and be at the last commit of this
branch." A `.`-menu action that moves the Commits cursor to a branch's tip commit
and focuses the Commits panel.

## Decision: Branches-panel action, no new popup

The action lives on the **Branches panel** `.` menu (mirrors `commitSoloRow`,
which also operates on the selected branch): select a branch → `.` → **Go to tip
in commits** → the Commits cursor jumps to that branch's tip and focus moves to
the Commits panel. This reuses `selectedBranch()` and needs no new picker.

(A branch picker invoked from *within* the Commits panel — closer to the literal
"in the commits tree" phrasing — is a richer follow-up; this no-popup version
delivers the core "jump to a branch's last commit" with minimal surface.)

## Finding the tip

A branch ref decorates only its tip commit, so the tip is the loaded commit whose
`Refs` contains a local ref named the branch. Map through `panelView(panelCommits)`
so the jump is correct under filter/sort: walk the display→backing `idx`, find the
display row whose backing commit carries the branch's local ref, set
`m.sel[panelCommits]` to that display index, and `m.focus = panelCommits`.

If the tip is not among the loaded commits (paged out / filtered away), set a
`statusMsg` ("branch <name> tip not in the loaded commits") and leave focus where
it is. (Paging-to-find the tip is a follow-up; in the date-ordered multi-branch
feed a branch's tip is normally near the top and already loaded.)

## Implementation (`internal/tui/commit_scope.go`)

```go
func (m Model) commitGotoTipRow() (actionRow, bool) {
	if m.focus != panelBranches {
		return actionRow{}, false
	}
	b, ok := m.selectedBranch()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commits-goto-tip",
		label: "Go to tip in commits",
		run: func(m Model) (tea.Model, tea.Cmd) {
			_, idx := m.panelView(panelCommits)
			for di, bi := range idx {
				if bi >= 0 && bi < len(m.commits) && commitHasLocalRef(m.commits[bi], b.Name) {
					m.sel[panelCommits] = di
					m.focus = panelCommits
					return m, nil
				}
			}
			m.statusMsg = "branch " + b.Name + " tip not in the loaded commits"
			return m, nil
		},
	}, true
}

func commitHasLocalRef(c model.Commit, name string) bool {
	for _, r := range c.Refs {
		if r.Kind == model.RefLocal && r.Name == name {
			return true
		}
	}
	return false
}
```

Wire `commitGotoTipRow` into `availableActions` (near `commitSoloRow`). No
keybinding → menu-only + help.go advertising.

## Testing (TDD)

- Row present on the Branches panel; running it with a feed containing a commit
  decorated `feat` (local) sets `sel[panelCommits]` to that commit's display index
  and `focus == panelCommits`.
- Not-found: a feed without the branch's tip leaves focus on Branches and sets a
  `statusMsg`.
- `commitHasLocalRef` matches a local ref by name (ignoring remote/tag kinds and
  the Head flag).

No git/argv change → no real-git or e2e scenario.

## Out of scope

- A Commits-panel branch picker popup (richer "go to branch" from the commit
  tree) — follow-up.
- Paging/seeking to load a tip that isn't in the current feed window.
