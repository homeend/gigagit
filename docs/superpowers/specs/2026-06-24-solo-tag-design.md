# Solo This Tag — scope the Commits list to a tag's history (Design)

**Date:** 2026-06-24
**Status:** Approved, ready for plan

## Goal

Add a Tags-panel `.`-menu action **"Solo this tag"** that scopes the Commits
panel to the selected tag's commit history (`git log <tag>`) — the tag's commit
at the top, its full ancestor history below, lazily paged. This lets a user
browse everything that went into a release, which is impossible today on a large
repo where the tag's commit is far outside the loaded feed window.

## Background

The Commits feed already supports scoping to arbitrary refs: `CommitFeed` holds a
`LogScope{Branches []string}` whose entries are passed straight to `git log
<refs…>` (`git.LogScoped`). The TUI drives this with the **"Solo this branch"**
machinery — `m.commitScopeBranches` → `startFeedReload()` (`SetScope` +
`LoadInitial`, with the loading indicator, gen-dropping, and the `Commits
(solo: …)` header). A tag is just a ref, so "Solo this tag" is a near-clone of
"Solo this branch", sourced from the Tags panel.

This is the list-scoping counterpart to the just-shipped `enter`-on-a-tag fix
(which opens the tag's commit *files* by hash). Both stay: `enter` inspects the
release commit's files; "Solo this tag" browses the release's commit history.

## Decisions (from brainstorming)

- **Gesture:** a Tags `.`-menu action "Solo this tag" (NOT overloading `enter`,
  which keeps opening the files view).
- **Scope depth:** ancestors only (`git log <tag>`). Descendants/"after" commits
  are out of scope (a tag has no forward direction; fuzzy and repo-dependent).

## Architecture & components

`internal/tui/tags_actions.go` — new `tagSoloRow`:

```go
// tagSoloRow offers "Solo this tag" on the Tags panel: scope the Commits feed to
// the tag's history (git log <tag>) and focus the Commits panel, or un-solo if it
// is already the sole scope. Mirrors commitSoloRow; a tag is just a ref to git
// log, so the existing scope machinery handles it.
func (m Model) tagSoloRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-solo",
		label: "Solo this tag",
		run: func(m Model) (tea.Model, tea.Cmd) {
			if len(m.commitScopeBranches) == 1 && m.commitScopeBranches[0] == name {
				m.commitScopeBranches = nil // re-solo → un-solo
			} else {
				m.commitScopeBranches = []string{name}
			}
			m.focus = panelCommits // land on the freshly-scoped list (Tags is mid-column)
			return m.startFeedReload()
		},
	}, true
}
```

`internal/tui/action_menu.go` — append `tagSoloRow` beside the other tag rows in
`availableActions`.

That is the whole change. `startFeedReload`, the loading indicator, lazy paging,
the `Commits (solo: <ref>)` header, and the "Show all branches" reset are all
reused unchanged.

## Behaviour notes

- The tag's commit is row 0 (it is the tip of `git log <tag>`); history pages in
  as the user scrolls, exactly like soloing a branch. Performant on large repos
  (a normal log walk, paged 50 at a time).
- Clearing: the existing **"Show all branches"** (`commitShowAllRow`, reachable
  from the Commits panel once focus lands there) or re-running "Solo this tag".
- Header: reuses the generic `Commits (solo: <ref>)` — it shows the tag name
  (it cannot distinguish a tag from a branch; the scope is just a ref string).
  Accepted; no special "tag:" label.
- The Branches-panel ◉ "in scope" marker won't light for a tag scope (it matches
  branch names). Cosmetic; the header still shows the active scope.
- Name ambiguity (a branch and tag sharing a name) is left to git's normal ref
  resolution — the same as the existing bare-name branch scope; not handled
  specially.

## Error handling

None new. A bad/last-deleted tag ref would surface as an empty/erroring log walk
through the existing feed-load path (the feed already tolerates load errors).

## Testing

`internal/tui/tags_actions_test.go`:

- `tag-solo` row present when a tag is selected on the Tags panel + idle; absent
  off the Tags panel.
- Dispatch: running the row sets `m.commitScopeBranches == []string{tag.Name}`,
  sets `m.focus == panelCommits`, and returns a non-nil reload cmd.
- Toggle: with `commitScopeBranches` already `[tag.Name]`, running the row clears
  it to nil (un-solo).

(A real reload/`git log <tag>` walk is covered by the existing feed/scope tests;
these assert the row wires the scope correctly.)

## Files

- Modify: `internal/tui/tags_actions.go` (+ `tagSoloRow`).
- Modify: `internal/tui/action_menu.go` (wire it).
- Modify: `internal/tui/tags_actions_test.go` (tests).
- Modify: `CHANGELOG.md`, `README.md` (Tags `.`-menu description).

No engine, git-verb, domain, CLI, or agentskill changes.

## Non-goals

- Descendants / "commits after the tag" (would need a containing-branch choice +
  cursor positioning).
- A distinct `tag:` header label or a Tags-panel in-scope indicator.
- A CLI surface (`git log <tag>` is already available via raw git / future work).
