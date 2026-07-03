# Branches panel: enter = go to tip, ctrl+g = solo + go to tip

**Date**: 2026-07-03
**Status**: approved

## Problem

Jumping the Commits panel to a branch's tip currently takes three keys
(`.` menu → find "Go to tip in commits" → enter), and when the tip is not in
the loaded commit page the action dead-ends with a status message. There is
also no single gesture for "focus this branch": solo it AND land on its tip.

## Behavior

### enter (Branches panel)

Pressing enter with a branch selected in the Branches panel executes the
existing `.`-menu action **"Go to tip in commits"** (`commitGotoTipRow`):

1. **Fast path (unchanged)**: if a loaded Commits row matches the branch's tip
   hash (`commitIsHash`, display-index space), move the Commits cursor there
   and focus the Commits panel (`focusCommitsPanel`).
2. **Fallback (new — replaces the status message)**: call
   `startEagerSearch(b.Hash)` — the exact ctrl+f deep-search machinery:
   - clears any active `/`-filter ("go to" semantics; the found commit lands
     in the full list),
   - pages history up to `commit_search_max_pages` (default 5) per pass with
     the ⏳ loading state,
   - shows the "Search deeper?" prompt at the budget cap,
   - reports `'<hash>' not found in full history` on exhaustion.

The fallback lives inside the shared row's `run`, so the `.`-menu row gains it
too — enter and the menu can never drift apart.

Matching note: the eager-search haystack (`commitHaystackAt`) begins with the
full `c.Hash`, so a full tip SHA matches exactly and unambiguously.

Accepted side effects:

- A `/`-filter that hides an already-loaded tip no longer dead-ends: the
  eager fallback clears the filter and finds it (consistent with ctrl+f).
- If the feed is soloed/scoped away from the branch, the search pages within
  that scope and ends in "not found in full history" (after offering to go
  deeper) — exact ctrl+f parity.

Gating: same as the row today — Branches focus + a selected branch. No
`opsIdle` requirement (pure navigation).

### ctrl+g (Branches panel)

Pressing ctrl+g with a branch selected executes **"Solo this branch" followed
by "Go to tip in commits"**, as if both `.`-menu rows ran back-to-back:

1. Run `commitSoloRow`'s action verbatim: toggle `commitScopeBranches` to
   `[branch]` (or un-solo if it is already the sole scope) and
   `startFeedReload()`. Toggle semantics are preserved deliberately:
   ctrl+g twice on the same branch = solo → un-solo, cursor on the tip both
   times; ctrl+g on B while soloed to A re-solos to B.
2. Chain the goto-tip after the reload lands, via the established
   pending-state pattern (cf. `pendingPushTags`): ctrl+g sets
   `pendingGotoTip = b.Hash` before the reload; the `commitsReloadedMsg`
   handler captures-and-clears it and runs the same goto-tip logic (jump by
   hash, else `startEagerSearch(hash)` — relevant after un-solo when the tip
   may be deep in the full feed).
3. `pendingGotoTip` is cleared on `reRoot` so a repo switch cannot fire a
   stale jump.

Gating: solo's gating — Branches focus + `opsIdle` + a selected branch
(stricter than enter, since it mutates the feed scope).

Key choice: ctrl+g (0x07) is a plain C0 control, detectable in every
terminal under Bubble Tea v1 — unlike ctrl+enter, which most terminals send
as a bare CR. ctrl+g is currently unbound in the TUI.

## Implementation shape

- `commit_scope.go` — `commitGotoTipRow.run`: replace the
  `"tip not in the loaded commits"` status-message line with
  `return m.startEagerSearch(b.Hash)`.
- `model.go` key dispatch —
  - `enter` case: if focus is Branches, offer-and-run `commitGotoTipRow`.
  - new `ctrl+g` case: offer-and-run `commitSoloRow` with
    `pendingGotoTip = b.Hash` set first (one small helper so the gating and
    hash capture stay together).
- `model.go` `commitsReloadedMsg` handler: after the gen-guarded state apply,
  capture-and-clear `pendingGotoTip` and run the goto-tip logic.
- `reRoot`: clear `pendingGotoTip`.
- Footer (`footer.go`): `[enter] tip` (mirrors the Tags `[enter] go to
  commit` row) and `[ctrl+g] solo+tip`, both gated like their actions.
- Help (`help.go`): extend the Branches lines so "Go to tip in commits"
  names enter, "Solo this branch" names ctrl+g, and both mention the
  deep-search fallback.
- `CHANGELOG.md` entry.

## Testing

TUI tests beside the existing goto-tip/solo/eager tests:

- enter with the tip loaded → Commits cursor on the tip row, Commits focused.
- enter with the tip not loaded and `CanLoadMore` → eager search active
  (`m.eager.active`, query = tip hash, load kicked).
- enter with the tip not loaded and the feed exhausted → "not found in full
  history" status message.
- enter with no branch selected (empty/filtered list) → no-op.
- ctrl+g → scope becomes `[branch]`, reload started, `pendingGotoTip` set;
  after `commitsReloadedMsg` → cursor on tip, focus Commits, pending cleared.
- ctrl+g on the already-soloed branch → scope cleared (un-solo), pending
  still chains the jump.
- ctrl+g while `!opsIdle` → no-op.
- `reRoot` clears `pendingGotoTip`.

Budget-cap prompt / deeper-search behavior is already covered by the eager
machinery's own tests; hash matching for slash-named branches is covered by
the row's existing tests (hash compare, not decoration parsing).

## Non-goals

- No equivalent bindings on the Remotes panel.
- No files-view-by-hash fallback (tags-enter style) — deep search instead.
- No change to solo's toggle semantics.
- No Bubble Tea keyboard-protocol work (ctrl+enter was rejected for exactly
  that reason).
