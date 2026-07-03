# Commits panel: esc unmarks all commits

**Date:** 2026-07-03
**Status:** approved

## Problem

Space/`m` mark commits into the ◉ compare selection, but the only ways to
clear the whole selection are the `.`-menu row or unmarking one by one. The
esc key already peels transient Commits-panel state (◆ mark → @-highlight →
filter) yet ignores the ◉ set.

## Design

1. **Behavior.** In the main key dispatch's `esc` case (`model.go`, the
   `case "esc":` inside the panel-key switch): when the Commits panel has
   focus and `len(m.commitCompareSet) > 0`, clear the whole set
   (`m.commitCompareSet = nil`) and return. The check sits at the FRONT of
   the existing peel chain, so successive esc presses peel ◉ marks →
   ◆ mark → @-highlight → filter, one per press. Other panels are
   untouched: esc there never clears commit marks. Ungated on `opsIdle()`,
   matching the rest of the esc chain (clearing pure UI selection state is
   harmless mid-op).
2. **Discoverability.** The space refusal hint becomes
   `2 commits already marked — space a marked one to unmark, esc to unmark all`
   (`commit_space.go`), and the space help row's parenthetical mentions esc
   (`help.go`). Footer unchanged — esc is never advertised there. CHANGELOG:
   extend the existing unreleased space-mark bullet; README: extend the
   `space` row's Commits clause.
3. **Tests** (`commit_space_test.go`): esc clears the set when Commits is
   focused; a set survives esc pressed with another panel focused; peel
   order — with marks + committed highlight, the first esc clears only the
   marks, the second clears the highlight. The existing refusal test still
   pins the hint's stable prefix.

## Out of scope

- No change to what esc does in other panels, in popups/layers, or to the
  ◆ mark / highlight / filter peel steps themselves.
