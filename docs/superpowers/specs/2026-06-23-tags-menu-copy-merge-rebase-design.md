# Tags Menu: Copy + Merge/Rebase (GitKraken parity, Tags-A) — Design

**Date:** 2026-06-23
**Status:** Approved, ready for plan

## Goal

Bring the Tags-panel `.` (action) menu closer to GitKraken's tag context menu by
surfacing capabilities that already exist in the engine: copy actions (the Tags
panel has none today) and one-click merge/rebase relative to the selected tag.
This is the "easy wiring wins" slice (Tags-A) of the GitKraken tag-menu gap
analysis. Explicitly out of scope (separate features / dropped): delete tag from
remote (Tags-B), annotate an existing tag (Tags-C), copy web link, hide/solo.

## Background

GitKraken's tag row is *both a ref and a commit*, so its menu blends commit-ops
(cherry-pick, reset, revert, create-branch) with tag-ref-ops (delete, copy,
annotate). `gg` splits these: commit-ops live on the **Commits** panel (reached
from a tag via `enter` → jump to the tag's commit), tag-ref-ops on the **Tags**
panel. The Tags `.` menu today offers Check out / Push / Delete (local) and
`enter` → go to commit, but **no copy action at all** (`contextCopyRows` has no
`panelTags` case) and no merge/rebase. This feature closes those two gaps.

This mirrors the already-shipped remote-menu "Bucket A" (copy id/sha +
merge/rebase on the Remotes row); it reuses the same helpers.

## Scope

Two parts, both pure TUI wiring of existing engine ops and helpers.

### Part 1 — Copy group on the Tags row

Add a `panelTags` case to `contextCopyRows`:

- **Copy tag name** → `tag.Name` (static `copyRow`).
- **Copy commit id** → `tag.Target` (static `copyRow`). `model.Tag.Target` is the
  dereferenced commit (the tag parser peels annotated tags to `*objectname`), so
  this is the commit the tag resolves to, short form.
- **Copy commit sha** → `copyShaRow(tag.Target, tag.Target)` — resolves the
  **target's** full 40-char commit SHA on invoke via `domain.RevParse`. Passing
  `tag.Target` (not `tag.Name`) is deliberate and load-bearing: `rev-parse
  <annotated-tag>` returns the *tag object* SHA, whereas resolving the peeled
  target yields the commit — correct for both lightweight and annotated tags. The
  fallback (nil svc / resolve error) is the short `tag.Target`.

`copyShaRow` and `domain.RevParse` already exist (shipped in remote Bucket A).

### Part 2 — Merge / Rebase on the Tags row

Add two rows to `tags_actions.go`, gated on a tag selected + `opsIdle` + a
non-detached current branch (reusing the existing `Model.remoteCurrentBranch()`
helper, which dual-guards `""` and `"(detached)"`):

- **Merge `<tag>` into current (`<cur>`)** → `startOp(engine.SmartMerge{Source:
  tag.Name})` (empty Target defaults to the current branch).
- **Rebase current (`<cur>`) onto `<tag>`** → `startOp(engine.SmartRebase{Onto:
  tag.Name})` (empty Branch defaults to the current branch).

Conflicts/dirty trees are handled by the existing SmartMerge/SmartRebase Decider
ladder (mapped to the TUI modal). No new confirm modal — local and reversible,
matching GitKraken's one-click offering and the remote-menu precedent.

## Architecture & components

No new types or engine work. The selected tag resolves through the existing
view-transform path (`backingIndex(panelTags)` → `m.tags[bi]`), the same access
the existing `tagCheckoutRow`/`tagDeleteRow` use.

`internal/tui/tags_actions.go`:

```go
// tagMergeRow offers "Merge <tag> into current". SmartMerge with an empty Target
// defaults to the current branch; conflicts handled by SmartMerge's Decider
// (mapped to the TUI modal). Hidden on detached HEAD.
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
	// ... same guards ...
	return actionRow{
		id:    "tag-rebase",
		label: "Rebase current (" + cur + ") onto " + name,
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.startOp(engine.SmartRebase{Onto: name}) },
	}, true
}
```

`internal/tui/action_menu.go`:
- Add a `case m.focus == panelTags:` to `contextCopyRows` returning the three
  copy rows above.
- Append `tagMergeRow` and `tagRebaseRow` beside the existing tag rows
  (`tagCheckoutRow`/`tagPushRow`/`tagDeleteRow`).

## Error handling

- **Copy sha resolve failure** → fall back to the short `tag.Target`; copy still
  yields a usable value. No error surfaced.
- **Merge/rebase conflicts / dirty tree** → handled by the existing
  SmartMerge/SmartRebase Decider ladder → TUI modal. No new path.

## Testing

`internal/tui/tags_actions_test.go` (extend):

- Copy rows: with `m.focus = panelTags` and a tag whose `Name`/`Target` are set,
  `contextCopyRows` returns `copy-tag-name` (== Name), `copy-commit-id`
  (== Target), and `copy-commit-sha` (present).
- `copy-commit-sha` resolves the full SHA via a fake svc whose `FakeRunner`
  returns a 40-char `git rev-parse` result; falls back to short on error/nil-svc.
- Merge/rebase rows: `tag-merge`/`tag-rebase` present when a tag is selected +
  attached HEAD; absent on detached HEAD (`m.status.Branch == ""`). Dispatch
  returns a non-nil cmd (fake svc, as `startOp` spawns a goroutine).

Use the package's existing tag test-model setup + a fake svc
(`domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})`).

## Files

- Modify: `internal/tui/action_menu.go` — `panelTags` copy case + wire the two rows.
- Modify: `internal/tui/tags_actions.go` — `tagMergeRow` + `tagRebaseRow`.
- Modify: `internal/tui/tags_actions_test.go` — copy + merge/rebase tests.
- Modify: `CHANGELOG.md`, `README.md` (Tags `.` menu surface).

No engine, git-verb, domain, CLI, or agentskill changes (all reused).

## Non-goals

- Delete tag from remote (Tags-B), annotate existing tag (Tags-C).
- Copy web link to tag, hide/solo tags.
- "Create branch at tag, stay" (overlaps the existing checkout "Create branch…").
