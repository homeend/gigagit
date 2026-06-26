# Copy to working dir — design

Date: 2026-06-26
Status: approved (brainstorm)
Scope: TUI-only (`internal/tui`)

## Problem

When viewing a stash's (or a commit's) files in the file tree, there is no way
to pull a single file's content into the working tree. The user wants, from the
`.` context menu on a focused file, an option to "make the file local" — write
that file's stashed/committed version into the working directory as a normal
(unstaged) working file.

## Goal

Add one `.`-menu action, **"Copy to working dir"**, on a focused non-working
file. Running it writes that file's resolved bytes into the working tree at its
own path. If a working file already exists there with different bytes, the
existing `engine.WriteFile` Overwrite/Cancel modal asks before clobbering.

## Why it's small

The write primitive (`engine.WriteFile{Path, Data}` — writes bytes to the
working tree as an unstaged change, with an Overwrite/Cancel decider fork) and
the resolve seam (`domain.ResolveBytes(ctx, model.FileRef)` → `git show`) already
exist and already back shelf-restore and bookmark-paste. The focused file is
already frozen into a `model.FileRef` by the shared, surface-aware
`focusedCompareRef()` — the same helper that powers the existing **"Compare
against working dir"** row. This feature is the **write-sibling of that compare
row**: same availability, same ref, same guard. No engine, domain, git, or model
changes — one new TUI row builder, wired alongside `compareAgainstWorkingDirRow`,
plus a test.

Key enabling fact: a **stash** file tree stores the stash's *resolved commit SHA*
in `m.filesHash` (set from `stashFilesMsg.sha`, model.go:1402), so
`focusedBookmark`/`focusedCompareRef` already return a clean
`{Source: SourceCommit, Locator: <sha>, Path}` for stash files — using the stable
SHA, not the positional `stash@{0}`. No instability and no new addressing code.

## Availability (chosen scope)

Offered **wherever "Compare against working dir" is offered** — i.e. on any
focused file whose source is not the working tree itself. Concretely:

- a focused file in a **stash** file tree (the original request),
- a focused file in a **commit** file tree (restore an old version),
- a **Staged** panel file (write the index blob to the working file),
- a single file on a history/blame/diff surface (these already feed
  `focusedCompareRef`).

It is **absent** when `focusedCompareRef` returns `!ok` (nothing focused, or a
deletion — see below) or when the focused file is already the working-tree
version (`ref.Source == model.SourceUnstaged`) — copying a working file onto
itself is meaningless. This guard is byte-for-byte the same as
`compareAgainstWorkingDirRow`.

## Design

One new row builder in `internal/tui` (colocated with the compare rows, e.g. in
`bookmark.go` next to `compareAgainstWorkingDirRow`):

```go
// copyToWorkingDirRow is the menu action "Copy to working dir": it writes the
// focused file's resolved bytes into the working tree at its own path, as an
// unstaged change. The write-sibling of compareAgainstWorkingDirRow — same
// focused ref, same guard. engine.WriteFile owns the overwrite-or-cancel fork.
func (m Model) copyToWorkingDirRow() (actionRow, bool) {
	ref, label, ok := m.focusedCompareRef()
	if !ok || ref.Source == model.SourceUnstaged {
		return actionRow{}, false
	}
	return actionRow{
		id:    "copy-working-dir",
		label: "Copy to working dir",
		run: func(m Model) (tea.Model, tea.Cmd) {
			data, err := m.svc.ResolveBytes(context.Background(), ref)
			if err != nil {
				m.statusMsg = "copy to working dir: " + err.Error()
				return m, nil
			}
			return m.startOp(engine.WriteFile{Path: ref.Path, Data: data})
		},
	}, true
}
```

Wiring: add `if r, ok := m.copyToWorkingDirRow(); ok { rows/out = append(..., r) }`
immediately after **each** `compareAgainstWorkingDirRow()` call site in
`availableActions` (`action_menu.go` — the content-window block and the
panel-focus block), so the two siblings always sit together.

Resolve is synchronous, matching `bookmarkPastePrompt`: one `git show` of a
single file is fast, and errors surface as `statusMsg`. `startOp(WriteFile{…})`
gives the overwrite modal and the normal post-op status/reload for free.

## Behavior decisions

- **Destination is the file's own path** (`ref.Path`). No destination prompt —
  the request is "make this file local," not "paste to an arbitrary path"
  (that's bookmark-paste's job). A pre-existing differing working file triggers
  `engine.WriteFile`'s Overwrite/Cancel modal; identical bytes are a no-op
  ("unchanged").
- **Deletions are excluded.** `focusedCompareRef` (via `focusedBookmark`)
  already returns `!ok` for a `status == "D"` tree entry (a file deleted in that
  commit/stash has no content to resolve), so the row is absent there — and
  "copy" never means "delete the working file."
- **Binary content is fine** — it is a byte copy; `ResolveBytes`/`WriteFile`
  don't interpret content.
- **Resolve failure** (missing path, unreadable) sets `statusMsg` and no-ops —
  never crashes.
- **Not a stash mutation.** This reads one file out of the stash; the stash is
  untouched (no pop/drop). Independent of apply/pop.

## Testing

Drive-tests in `internal/tui` (real `git` in a `t.TempDir`, existing helpers):

1. **Row present on a stash tree file:** open a stash file tree, focus the tree
   on a file, assert `availableActions(m)` contains a row with id
   `copy-working-dir`.
2. **Row absent for a working-tree file:** focus an unstaged Files-panel entry
   (`ref.Source == SourceUnstaged`); assert no `copy-working-dir` row.
3. **Row absent for a deletion:** a tree file with `status == "D"`; assert no
   `copy-working-dir` row.
4. **End-to-end write:** build a repo, stash a change to `f`, modify/revert `f`
   in the working tree so the stashed version differs, run the
   `copy-working-dir` row's handler (resolve + `WriteFile`), drive the resulting
   op (auto-answering Overwrite via the test decider), and assert the working
   file on disk now holds the stashed bytes.
5. **Resolve error path:** force `ResolveBytes` to fail (e.g. a bogus path) and
   assert `statusMsg` is set and no panic.

(Reuse the existing stash/files-view test helpers; the `engine.WriteFile`
overwrite fork is already covered by its own engine tests — here we only assert
the row wiring and that the op is dispatched with the right `Path`/`Data`.)

## Docs to update on completion

- `CHANGELOG.md` (always).
- `internal/tui/help.go` — extend the `.`-menu help text to mention "Copy to
  working dir" (and the footer if it lists file-tree menu actions).
- No CLI surface change → no `agentskill` bump.
- `README.md` only if it enumerates `.`-menu file actions (likely a one-liner or
  nothing).
