# gg web: what marked commits can do (parity bundle B)

Port of the TUI's ◉-mark actions on the Commits panel (`internal/tui/commit_scope.go`,
`saved_pair_save.go`, `review.go` `markedRangeReviewRow`, `help.go` Commits rows) to
gg web. The TUI is the spec; this document records the wire and the web gestures.
Gap list: `docs/web-tui-parity.md` → *Commits, marks and compare*.

## What the web has today

ctrl+click marks commits (`state.cmarks`, hashes); the right-click menu offers
"compare the 2 marked commits", "squash N marked commits…" (adjacent only —
`/api/commit-squash` answers 422 otherwise) and "unmark all".

## What it gains

| # | TUI | Web |
|---|---|---|
| 1 | Drop N selected commits (2+, gaps allowed, one rebase) | menu row "drop N marked commits…" (danger, local confirm) → `POST /api/commit-drop {shas}` |
| 2 | Squash, non-adjacent → "Reorder & squash" prompt | the squash 422 carries `not_adjacent: true`; the page asks "Selected commits aren't adjacent. Reorder them adjacent, then squash?" [reorder & squash / cancel] and re-posts with `reorder: true` (`rebaseplan.BuildSquashReorder`) |
| 3 | Cherry-pick the marked commits (oldest→newest, one sequencer run); merge commits refused, named | when marks exist the cherry-pick row reads "cherry-pick N marked commits onto X" ("the marked commit" for one) → `POST /api/commit-cherry-pick {shas}` |
| 4 | Compare selection, 3+ = combined diff oldest^..newest, refused from a root | "compare range of N commits (combined diff)"; base = the oldest mark's first parent from the feed |
| 5 | shift+↑/↓ grows the marks as a contiguous run | shift+↑/↓ on the Commits pane (list layout) and shift+click (marks every row between the cursor and the click) |
| 6 | Save / Save reversed to previews (exactly 2) | two rows → `POST /api/saved-compares {a, b, label}`; label = the NEWER commit's subject (both directions); marks kept; a duplicate says which entry it is |
| 7 | Review marked range (AI), 2+: exactly what Compare selection shows | "review marked range (AI)…" → review target `range` with `shas`; label `<old7>..<new7> — <newest subject>` built server-side |
| 8 | On a branch's tip commit: Rename branch <name> / Delete branch <name> per local tip; delete hidden for the checked-out branch | the same pair per local ref on the row, over the branch menu's `rename-branch` / `delete-branch` ops |

## Wire rules (the squash endpoint's precedent)

- Shas arrive NEWEST FIRST (feed order), hex only (`isHexSha`), de-duplicated.
  The order is a hint; for rebases the server reads `oldest^..branch` itself and
  the plan builder's membership check refuses anything off the branch (422,
  history untouched). Cherry-pick replays the reversed list (oldest first); a
  merge commit (`<sha>^2` resolves) is refused 422 naming its short sha.
- Range review: 2 shas → `older..newer`; 3+ → `oldest^..newest`, the parent
  resolved server-side; a root oldest is refused.
- All three new/changed POSTs sit behind `writeGuard`.

## Not in this bundle

◇ Working tree / Staged rows as marks (bundle C), multi-branch scope, `@`
highlight search (bundle F).
