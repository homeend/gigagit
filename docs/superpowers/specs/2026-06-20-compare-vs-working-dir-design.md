# Compare against working dir — design

**Feature 2 of the commit-ops pipeline backlog.** A `.`-menu action that diffs a
focused (non-working-tree) file against its current working-tree version.

## Decision: per-file, not whole-commit

"Compare commit against working dir" is implemented **per file** — the focused
file's committed/staged/shelf version vs the same path in the working tree —
reachable wherever a file is focused (commit files-view, history, blame, the diff
view, the Staged panel). That is the "commit context" for a file: when you've
drilled into a commit you are looking at its files.

A **whole-commit aggregate** diff (every file changed between the commit and the
working tree, in one multi-file surface) is a *larger* feature — the codebase's
diff view is per-file and there is no multi-file aggregate-diff surface — so it is
out of scope here, noted as a possible follow-up. This per-file version reuses the
entire existing compare stack and adds almost no code.

## Reuse — the existing compare stack

The "Compare against bookmark/shelf" actions already establish the pattern:
- `focusedCompareRef() (model.FileRef, string, bool)` freezes the focused file as
  a `FileRef` (Source ∈ {Commit, Staged, Shelf, Unstaged}) + a display label.
- `loadCompareTwoRefsCmd(left, right, title, subtitle, tag)` resolves both sides
  via `domain.ResolveBytes` and runs the `Differ`.
- `openPickerDiff(v, tag, load)` installs the full-screen diff view (clears
  overlays — a no-op when none are open — and the surface stack, so the diff owns
  the screen), and returns the load cmd.

"Compare against working dir" differs only in that the **second side is fixed** —
the working-tree file — so there is **no picker** and no `pendingCompare`: the
action opens the diff immediately.

## The action — `compareAgainstWorkingDirRow`

New `actionRow` helper (in `internal/tui/bookmark.go`, beside the other
compare-against rows):

```go
func (m Model) compareAgainstWorkingDirRow() (actionRow, bool) {
	ref, label, ok := m.focusedCompareRef()
	if !ok || ref.Source == model.SourceUnstaged {
		return actionRow{}, false // nothing focused, or already the working file
	}
	return actionRow{
		id:    "compare-working-dir",
		label: "Compare against working dir",
		run: func(m Model) (tea.Model, tea.Cmd) {
			right := model.FileRef{Source: model.SourceUnstaged, Path: ref.Path}
			title := ref.Path + " ↔ working"
			subtitle := label + " → working dir"
			tag := "cmpwd:" + ref.Path
			v := &diffView{title: title, context: subtitle, loading: true, partial: m.diffPartial, long: m.diffLong}
			v.width, _ = m.overlayDims()
			return m.openPickerDiff(v, tag, m.loadCompareTwoRefsCmd(ref, right, title, subtitle, tag))
		},
	}, true
}
```

- **Gate:** present only when a file is focused **and** its source is not already
  the working tree (`SourceUnstaged`) — comparing the working file against itself
  is meaningless. Staged (index→working = `git diff`), commit, and shelf sources
  are all meaningful.
- **Wiring:** add `compareAgainstWorkingDirRow` to `availableActions` at the same
  two sites as `compareAgainstBookmarkRow`/`compareAgainstShelfRow` (the
  `inContentWindow` branch and the main branch). No keybinding → menu-only,
  help.go advertising only.

## Data flow

1. `.` menu on a focused commit/staged/shelf file → **Compare against working
   dir**.
2. `run` builds the working-tree `FileRef` (same path, `SourceUnstaged`), a
   loading `diffView`, and calls `openPickerDiff` → installs the view + returns
   `loadCompareTwoRefsCmd(focused, working, …)`.
3. The cmd resolves both sides via `ResolveBytes`, runs the `Differ`, returns a
   `diffMsg{tag}` that paints the side-by-side diff.

## Testing (TDD)

Mirror `bookmark_compare_test.go`:

1. **Row present + opens the diff:** with a focused commit file (set
   `m.diffView = &diffView{title: "a.go", rev: "abc1234"}` so `focusedBookmark`
   yields a committed ref), `compareAgainstWorkingDirRow` returns `ok`; running it
   sets `m.diffView` with title `"a.go ↔ working"`, tag `"cmpwd:a.go"`, and a
   non-nil load cmd.
2. **Gated off for a working file:** with the focused file already unstaged (focus
   `panelFiles` over an unstaged status file, or a `diffView` with empty `rev`),
   the row is absent (`ok == false`).
3. **Staged file is allowed:** a focused staged ref (`SourceStaged`) yields the
   row (index-vs-working is a valid compare).
4. **End-to-end resolve (FakeRunner):** drive `loadCompareTwoRefsCmd` for a
   commit-vs-working pair against a `FakeRunner` that answers `git show`/`cat-file`
   (commit side) and reads the working file; assert the `diffMsg` view has no
   error. (Reuse the harness the bookmark/shelf compare tests use; if that proves
   heavy, keep coverage at the row + open-diff level, which is where the new code
   lives.)

No git verb change (ResolveBytes already supports all sources) → no new real-git
verb or e2e scenario.

## Out of scope

- Whole-commit aggregate diff (multi-file) — larger follow-up.
- Comparing against the index explicitly as a separate action (Staged-source
  focus already gives index→working).
