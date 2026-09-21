# Symmetric Compare (gg web) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans,
> INLINE in the session that wrote this plan. **NEVER subagents.**

**Goal:** a link comparison of two bounded sets can be viewed as two aligned
file lists around one diff, with a flippable direction arrow.

**Architecture:** domain aligns the two sets (`LinkComparison.SymmetricRows`,
pure); `/api/compare-links` adds an optional `sym` array; a new
`static/symcompare.js` paints both lists, the chips and the direction bar, and
`files.js` defers to it through a few dispatch lines.

**Spec:** `docs/superpowers/specs/2026-09-21-web-symmetric-compare-design.md`
· Mock: `docs/superpowers/specs/mocks/symmetric-compare-mock.html`

## Global constraints

- Worktree `/mnt/t/others/gigagit/.claude/worktrees/web-symmetric-compare`;
  every command `cd`s there; absolute paths in Write/Edit; `rtk`; never
  `git add -A`; never push; ask before merging.
- `files` on the wire stays byte-identical; `sym` is additive.
- Classic view unchanged. No TUI/CLI/MCP change.
- gg web hides by ID (`#id.hidden`), never a bare `.hidden` class.
- A new coloured/hidden element needs its own CSS rule; browser checks assert
  VISIBILITY and run against a broken build first.
- Preference in `/api/uistate` (`sym_compare`), never localStorage.
- New Go tests `t.Parallel()`; grep for helper-name collisions in
  `internal/web` tests before naming.
- Commit trailers as in this session.

## Tasks

### Task 1 — domain `SymmetricRows`
Files: `internal/domain/symrows.go`, `symrows_test.go`.
- [ ] Test: build two bounded sets with `boundedSetWith` (paths + has map)
  covering all seven state combinations; `Files` lists the differing paths;
  assert rows sorted, states, `Differs`; unbounded side → `ok=false`.
- [ ] Implement; watch the deleted-state assertion fail with `Has` ignored.
- [ ] Commit `feat(domain): SymmetricRows — two bounded sets, row-aligned`.

### Task 2 — wire
Files: `internal/web/linkcompare.go`, `linkcompare_test.go`.
- [ ] Test (real repo, two preview links over branches where: both change a
  file differently, both identically, one deletes a file the other keeps, one
  adds a file): `sym` rows + states; a point link → no `sym`; `a`+`b` form →
  no `sym`; `files` unchanged vs a golden taken before the edit.
- [ ] Implement: `symWire{Path, Left, Right, Differs, LeftSpec, RightSpec}`;
  specs via the existing `linkSideSpec(c.Left.Source(p))` rule; skip when
  `pair != nil`.
- [ ] Commit `feat(web): /api/compare-links answers the aligned rows (sym)`.

### Task 3 — uistate key
Files: wherever `/api/uistate`'s struct lives + its test, `static/uistate.js`,
`core.js` default.
- [ ] Add `sym_compare bool` (default false) following `diff_view`'s five
  edit sites (memory: PUT replaces).
- [ ] Commit.

### Task 4 — web view
Files: new `static/symcompare.js`; `static/index.html` (`#symleft-pane`,
`#sym-dir`); `static/style.css`; dispatch lines in `files.js`
(`openLinkCompare`, `updateLinkCompareFiles`, `renderFiles`,
`renderCompareBar`, `openFile`'s link arm, `setLayout`/`drillOut` class
cleanup), `keys.js` (`1`–`4`, `s`, `x` gated); embed/import guard test.
- [ ] `symActive()`, `visibleRows()`, `kindOf(row)`, `statusFor(row, flipped)`.
- [ ] `renderSymLists()` paints both panes from `state.files`; placeholder
  rows inert; selected row highlighted on both; scroll mirrored.
- [ ] chips + counts + `⇄ symmetric` toggle (disabled < 1200px, `resize`
  listener re-evaluates and falls back).
- [ ] direction bar; flip re-opens the current row with swapped specs/labels.
- [ ] identical / no-content rows paint a notice, no fetch.
- [ ] ONE chokepoint `setCompareRows()` assigns `state.files` (sym-visible rows
  when active, else `c.all`) — called from `openLinkCompare`,
  `applyCompareFilter` AND `updateLinkCompareFiles`; `refreshLinkCompare`
  (linkcompare.js) passes `body.sym` along. `c.all` stays the plain `files`.
- [ ] sym chips use `data-sf`, never `data-f` (the bar's handler owns that).
- [ ] both lists start at the same y: one fixed header height under `.sym`.
- [ ] every sym row carries a synthesized `status` (direction-aware, recomputed
  on flip) — menus, stepper and copy-link read `f.status`.
- [ ] check `keys.js` for `s`, `x`, `1`–`4` before binding; pick free keys.
- [ ] "no content" keys on the two STATES, not on `!differs`; an identical row
  DOES fetch its diff (all-Same) — notice only when it would render nothing.
- [ ] help overlay rows + footer hint for `s` / `x`.
- [ ] Commit `feat(web): symmetric view for a link comparison`.

### Task 5 — probe
Scratchpad `symprobe.mjs` (playwright, `executablePath` = the cached
chromium): fixture repo + two saved previews + a saved comparison; assertions
from spec §6. Run against a build with `sym` stripped (must FAIL), then the
real build.

### Task 6 — docs + gate
- [ ] `CHANGELOG.md`, `README.md` (web section), `docs/CLAUDE-details.md`,
  `docs/web-tui-parity.md` (web-only row).
- [ ] `git log branch..main`, merge main, BUILD the merged tree, `./test.sh race`.
- [ ] Verify binary (stripped) via SendUserFile + absolute path; screenshot.
- [ ] Ask before merging; then `./build.sh install`, `./build.sh web`.
