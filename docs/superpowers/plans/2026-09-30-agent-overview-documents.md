# Overview Documents Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo forbids implementer subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An agent posts an in-memory markdown overview whose links are anchors to files, lines, ranges and notes; the user tabs between anchors, opens one, and backspace returns to the overview.

**Architecture:** `internal/markdown` gains an opt-in `ParseWith` that keeps accepted non-http link destinations as `InAnchor`. The TUI adds a `srcOverview` open-file kind whose rows are laid out by gg at the reading-column width (one display row = one line), with anchor spans recovered from temporary per-anchor classes. Steer verbs `overview_*` and CLI `gg session overview …` drive it.

**Tech Stack:** Go 1.26, Bubble Tea, the repo's steer inbox, i18n TOML bundles.

**Spec:** `docs/superpowers/specs/2026-09-30-agent-overview-documents-design.md`

## Global Constraints

- Every user-visible TUI string goes through `i18n.T` with a literal key present in `internal/i18n/lang/{ja,ko,zh,ru}.toml`; prose never in an argument. Steer reply / CLI prose stays English.
- `internal/tui` and `internal/cli` never import `internal/git`.
- New tests call `t.Parallel()`.
- Limits: text ≤ 64 KiB (`overviewMaxBytes = 64 << 10`), ≤ 100 anchors (the per-anchor ids ride the `uint8` class mask), ≤ 20 overviews per worktree, title one line ≤ 200 runes.
- `steer.MaxCommandBytes` = 512 KiB.
- Skill: `agentskill.Version` 106 → 107; `.claude/skills/using-gg/SKILL.md` must equal `agentskill.SkillFile()`.
- Commit with `gg add <paths>` + `git commit -F <msgfile>`; never `git add -A`.

## Review Focus

1. An anchor label that wraps over two rows, or sits in a table cell / list item / quote — spans must still select and click correctly (Task 2 tests).
2. Two adjacent anchors `[a](x)[b](y)` stay two anchors (Task 2 test).
3. Relayout after a resize keeps the selected anchor selected and visible (Task 3 test).
4. backspace after the overview was closed must not crash or trap (Task 4 test).
5. `overview_set` while the user is reading keeps the selection when the same destination survives (Task 5 test).

---

### Task 1: `markdown.ParseWith` + `InAnchor`

**Files:**
- Modify: `internal/markdown/doc.go` (kind `InAnchor = "anchor"`, `Options`, doc comment), `internal/markdown/block.go` (`ParseWith`; thread `o Options` through `parseBlocks`, `parseList`, `parseTable`, `cellInline`), `internal/markdown/inline.go` (`parseInline(s, o)`, `inliner.o`, `child` inherits, `link` asks `o.Anchor`)
- Test: `internal/markdown/anchor_test.go`

**Interfaces — Produces:**
```go
type Options struct {
    // Anchor is asked about every link destination that is not http(s); a
    // destination it accepts becomes an InAnchor (URL = dest, In = label).
    Anchor func(dest string) bool
}
func ParseWith(src string, o Options) Doc
func Parse(src string) Doc // == ParseWith(src, Options{})
// ParseInline keeps its signature (Options{}).
```
`InAnchor` nodes: `URL` = the destination as written (trimmed), `In` = the parsed label (a label that parses empty gets `[{InText, dest}]`), `Text` empty (a consumer may number anchors there).

- [ ] **Step 1: failing tests** — `anchor_test.go`:
  - `TestParseNeverEmitsAnchors`: `Parse("[a](x.go:3)")` → no `InAnchor` anywhere (walk helper), text contains `a (x.go:3)` as today.
  - `TestParseWithKeepsAcceptedDestinations`: accept func `strings.HasSuffix(d, ".go") || strings.HasPrefix(d, "note:")`; `"see [the loader](a/b.go) and [n](note:t7)"` → two `InAnchor` with URLs `a/b.go`, `note:t7`, labels `the loader`, `n`.
  - `TestParseWithLeavesHTTPAlone`: `[w](https://x.y)` → `InLink` even when the func accepts everything; func never called with it (count calls).
  - `TestParseWithRefusedKeepsTodaysText`: func returns false → identical tree to `Parse`.
  - `TestParseWithReachesTablesListsQuotes`: an anchor inside a table cell, a list item, a blockquote → found.
  - `TestParseWithDangerousSchemeNeverAsked`: `javascript:x` dropped as today (func not called).
- [ ] **Step 2:** `go test ./internal/markdown/ -run 'Anchor|ParseWith' ` → FAIL (undefined `ParseWith`).
- [ ] **Step 3: implement.** In `link()`: compute `url, verdict := safeURL(dest)`; if `!image && verdict == urlShow && p.o.Anchor != nil && p.o.Anchor(strings.TrimSpace(dest))` → `p.add(Inline{Kind: InAnchor, URL: strings.TrimSpace(dest), In: inner-or-dest})` and return `end`. Thread `o` by value everywhere a `parseInline`/`newInliner` is reached. `Parse` delegates. Update doc.go package comment: "…survives only when it is http(s) — or, under ParseWith, when the caller's Anchor accepts it (as InAnchor, never InLink)".
- [ ] **Step 4:** `go test ./internal/markdown/` → PASS (golden + fuzz still green).
- [ ] **Step 5: commit** `feat(markdown): ParseWith keeps a caller's accepted link destinations as anchors`.

### Task 2: anchor grammar + overview layout

**Files:**
- Create: `internal/tui/overview.go` (types, `parseAnchorDest`, `overviewLines`, `paint`)
- Modify: `internal/tui/md_render.go` (classes `mdAnchor`, `mdAnchorSel`, `mdAnchorGone` before `mdClassEnd`; `mdAnchorID0 syntax.Class = 150`, `overviewMaxAnchors = 100`; `mdInlineRuns` case `InAnchor`; `mdStyle` cases)
- Test: `internal/tui/overview_test.go`

**Interfaces — Produces:**
```go
type anchorTarget struct{ path string; start, end int; note string }
type anchorSpan struct{ line, from, to int } // rune offsets in contentLine.text, [from,to)
type anchor struct{ dest string; target anchorTarget; label string; spans []anchorSpan; missing bool }
type overview struct{ text string; anchors []anchor; sel int; w int }
func parseAnchorDest(dest string) (anchorTarget, bool)
func overviewLines(text string, width int) ([]contentLine, []anchor)
func (ov *overview) paint(lines []contentLine) // re-classes every span: mdAnchor / mdAnchorSel (i == sel) / mdAnchorGone (missing)
```
Ruling (ledger): anchors capped at 100, not the spec's 200 — per-anchor ids ride the `uint8` class mask (150..249); links past the 100th render as plain label text. Cost if wrong: a very long overview loses anchors past 100.

Layout rules: `markdown.ParseWith(text, markdown.Options{Anchor: func(d) bool { _, ok := parseAnchorDest(d); return ok }})`; walk the tree in document order, numbering `InAnchor`s into `Text` (`strconv.Itoa(i)`); past 100 → replace with `InText` of the flattened label. `mdInlineRuns` `InAnchor`: `n, err := strconv.Atoi(n.Text)`; class `mdAnchorID0+n` (err → `mdLink`); label flattened (`mdFlat`) so emphasis cannot overwrite the id. `mdRows(doc, width)` → each row becomes `contentLine{text, raw: row.text, src: true, cls, noWrap: true}`; then scan each line's cls: runs of `mdAnchorID0+k` → `anchors[k].spans` (a wrapped label yields a span per row), class rewritten to `mdAnchor`. An anchor with no span (clipped away) keeps `spans == nil`. Empty text → one placeholder line `(empty overview)` (i18n, `src: false`).

`parseAnchorDest`: trim; `note:t<digits>` → note; else strip `./`; reject empty, leading `/`, `\`, a `..` segment, any rune ≤ ' ' or 0x7f, `://`, a Windows drive (`C:`); split an optional `:N` or `:N-M` suffix (N ≥ 1, M ≥ N) off the LAST colon only when what follows is digits (so `a:b.go` is a path); what remains must be non-empty.

- [ ] **Step 1: failing tests** (`overview_test.go`):
  - `TestParseAnchorDest` table: `a/b.go` ✓; `./a.go:12` → {a.go,12,12}; `a.go:3-9` ✓; `a.go:9-3` ✗; `a.go:0` ✗; `note:t7` ✓ note; `note:x` ✗; `/etc/passwd` ✗; `../x` ✗; `a/../b` ✗; `C:/x` ✗; `http://x` ✗(not reached but ✗); `a b.go` ✗; `` ✗.
  - `TestOverviewLinesSpansInAParagraph`: `"See [open files](internal/tui/open_files.go:20) and [x](a.go)."` width 80 → 2 anchors, spans on line 0 whose text slices equal the labels; classes are `mdAnchor`.
  - `TestOverviewLinesWrappedLabelHasTwoSpans`: label of 30 chars at width 20 → spans on two lines, concatenated slices == label words.
  - `TestOverviewLinesAdjacentAnchorsStayApart`: `"[a](x.go)[b](y.go)"` → 2 anchors, spans `[0,1)` and `[1,2)`.
  - `TestOverviewLinesAnchorsInListTableQuote`: one in each → 3 anchors each with one span, slice == label.
  - `TestOverviewLinesCapsAnchors`: 101 links → 100 anchors; the 101st label present as plain text, no `mdAnchorID0+` class left anywhere (every cls < mdClassEnd).
  - `TestOverviewPaintMarksSelectedAndMissing`: sel=1, anchors[0].missing → classes Gone, Sel.
  - `TestOverviewLinesEmptyText` → placeholder, no anchors.
- [ ] **Step 2:** `go test ./internal/tui/ -run 'Overview|AnchorDest'` → FAIL (undefined).
- [ ] **Step 3: implement** as above; `mdStyle`: `mdAnchor` → Underline; `mdAnchorSel` → Reverse+Bold; `mdAnchorGone` → Faint+Strikethrough. Add the placeholder key to the four bundles.
- [ ] **Step 4:** tests PASS; `go test ./internal/tui/ -run 'I18n|Markdown|Md'` PASS.
- [ ] **Step 5: commit** `feat(tui): overview layout — anchor grammar, spans through wrapping`.

### Task 3: the overview document

**Files:**
- Modify: `internal/tui/open_file.go` (`srcOverview` kind; `openFile.ov *overview`, `from *openFile`, `pendingEnd int`), `internal/tui/open_files.go` (`touch` skips `d.ov != nil`; `openFilesProto` source `overview` + Title; `onDisk` unchanged false), `internal/tui/open_files_watch.go` (`loadDocWith`: overview → a cmd returning `fileContentMsg{tag, loadNo, overview: true}`), `internal/tui/model.go` (`fileContentMsg` case: an overview fills via `d.fillOverview(rows, innerW)`), `internal/tui/file_viewer.go` (title; ctrl+w inert for overview), `internal/tui/file_preview.go` (`renderPreviewBox`: `if d := m.previewDoc(p); d != nil && d.ov != nil && d.ov.w != innerW { d.relayout(innerW, rowsCap) }`; overview hint), `internal/tui/sessions_popup.go` (row `<title>  overview · %d anchors`), `internal/steer/steer.go` (`OpenFile.Title`)
- Create in `overview.go`: `newOverviewDoc`, `fillOverview`, `relayout`, `selectAnchor`
- Test: `internal/tui/overview_doc_test.go`

**Interfaces — Produces:**
```go
func newOverviewDoc(title, text string) *openFile // srcOverview, path "overview-<seq>.md", backgrounded, markdown-less, p.prose=true, p.mode=modeScroll, ov.sel=-1
func (d *openFile) fillOverview(rows, innerW int)   // lays out at the reading column width for innerW (readingColumn(innerW, readingWidthDefault-or-m)); keeps place like a reload; paints
func (d *openFile) relayout(innerW, rows int)        // width changed: lay out again, cursor → selected anchor's first span line, ensure visible
func (d *openFile) selectAnchor(i, rows int)         // sel=i, cursor to its first span line, ensureCursorVisible, paint
func (m Model) overviewCount() int                   // overviews in the current worktree's list
```
Ruling (ledger): `fillOverview` takes the layout width from the caller (the frame's reading column), since the frame is only known there; `renderPreviewBox` re-lays out when `ov.w` differs — the `noteW` precedent (draw-time state on the document).

- [ ] **Step 1: failing tests:**
  - `TestOverviewDocIsABackgroundedInMemoryOpenFile`: `newOverviewDoc("Tour", md)`: `src.kind == srcOverview`, `backgrounded`, `!onDisk()`, `id()` is `f<n>`.
  - `TestOverviewDocLoadsFromMemory`: register in `loadedNavModel`, `bringToFront`, pump → lines hold the rendered text, anchors have spans.
  - `TestOverviewNeverEvictedByTheCap`: 25 docs incl. one overview first-opened → overview still listed.
  - `TestOverviewRelayoutKeepsTheSelection`: select anchor 3, render at width 60 then 120 → `ov.sel == 3`, cursor on its span line, visible.
  - `TestOverviewSwitcherRowAndProto`: switcher row contains `Tour` and `overview · 2 anchors`; `openFilesProto()` row `Source=="overview"`, `Title=="Tour"`.
  - `TestOverviewViewerTitleAndHint`: rendered box contains `Tour` and `[tab] next`.
- [ ] **Step 2:** FAIL.
- [ ] **Step 3: implement.** Hint (i18n): `%d/%d  [tab] next  [enter] open  [r] reference  [esc] background  [X] close  [/] find  [↑/↓] scroll`. Switcher row suffix key: `overview · %d anchors`. Add keys to all four bundles.
- [ ] **Step 4:** PASS + `go test ./internal/tui/ -run 'Switcher|OpenFile|I18n'`.
- [ ] **Step 5: commit** `feat(tui): overview documents in the open-files list`.

### Task 4: anchor keys, opening, back, click

**Files:**
- Create: `internal/tui/overview_keys.go` (`overviewKey`, `openAnchor`, `anchorBack`, `overviewClick`, `overviewRows` for the `.` menu, `anchorReference`)
- Modify: `internal/tui/file_viewer.go` (call `m.overviewKey(msg)` after `previewSelectKey`/`previewSearchKey`; `backspace` → `m.anchorBack(fv.openFile)`), `internal/tui/open_file.go` (`landPendingLine` sets a fixed `lsel` when `pendingEnd > line`), `internal/tui/mouse.go` (left press on a `*fileViewer` over an overview → `overviewClick`), `internal/tui/action_menu.go` (overview rows + "Back to overview"), `internal/tui/help.go` (an "Overview" section), `internal/tui/file_preview.go` (a file with `from` leads its hint with `[bksp] back`)
- Test: `internal/tui/overview_keys_test.go`

**Interfaces — Produces:**
```go
func (m Model) overviewKey(d *openFile, msg tea.KeyMsg) (Model, tea.Cmd, bool) // tab, shift+tab, enter, r, y
func (m Model) openAnchor(ov *openFile, i int) (Model, tea.Cmd)
func (m Model) anchorBack(d *openFile) (Model, bool)                            // false = d has no from
func (m Model) overviewClick(ov *openFile, x, y int) (Model, tea.Cmd)
func anchorReference(ov *openFile, a anchor) string // gg overview f12 "Tour" → a.go:3
```
Behaviour:
- tab: next anchor after `sel` having spans (from -1: the first whose first span line ≥ `p.sel`); wraps; shift+tab reverse. No anchors → consumed, no-op.
- enter (no live `lsel`, sel ≥ 0) → `openAnchor`. Path: `stat` via `m.docAbs`-style join of `m.currentWorktree` + path (`os.Stat`); missing → `anchors[i].missing = true`, paint, status `no file %s`, no open. Else `m.openFileViewerEv(path, start)`, the doc gets `pendingEnd = end` (when end > start), `backgrounded = true`, `from = ov`. Note: `findFileNote(m, id)` (existing helper over the open files) → nil → missing + status `note %s is gone`; else `d.pendingLine = n.start`, `from = ov`, `backgrounded = true`, `bringToFront(d)`.
- backspace in a viewer whose doc has `from`: if `from` is not in `m.openFiles.list(m.currentWorktree)` → status `the overview was closed`, stay. Else `m = m.backgroundDoc(d)`; `m, cmd = m.bringToFront(from)`; paint keeps `sel`.
- r: `anchorReference` to clipboard, status `Copied anchor reference`. y: whole text (`ov.text`), status `copied`.
- click: y−2 = row index into the window starting at `p.clampTop(p.sel, rows)`; x − 2 − margin → display column → rune index (walk runes adding `lipgloss.Width`); anchor whose span contains it → if it is already `sel` and the click is a double (`registerClick`) → open; else select.
- `.` menu rows (overview): `Next anchor` (tab), `Open anchor` (enter), `Copy anchor reference` (r); a file with `from`: `Back to overview` (bksp).

- [ ] **Step 1: failing tests** (fixture: `loadedNavModel` + `a.txt` 40 lines; overview text with anchors `a.txt`, `a.txt:12`, `a.txt:5-8`, `note:<id>` of a note added with `addNote`, `missing.txt`):
  - `TestTabStepsThroughAnchorsAndWraps`, `TestShiftTabStepsBack`.
  - `TestEnterOpensAPathAnchorOnTopWithItsLine` (pump; top layer is a viewer on `a.txt`, cursor line 12, `from == ov`, `backgrounded`).
  - `TestEnterOnARangeSelectsTheLines` (`lsel` fixed 4..7, cursor 4).
  - `TestEnterOnANoteAnchorLandsOnTheNote`.
  - `TestEnterOnAMissingFileMarksItAndStays` (status `no file missing.txt`, top layer still the overview).
  - `TestEnterOnAGoneNoteMarksIt`.
  - `TestBackspaceReturnsToTheOverviewAndKeepsTheFileOpen` (sel unchanged; `a.txt` still in the list).
  - `TestBackspaceAfterTheOverviewClosedSaysSo`.
  - `TestBackspaceWithoutAnOriginDoesNothing`.
  - `TestLatestJumpWins` (two overviews open a.txt; back goes to the second).
  - `TestReferenceAndCopyKeys` (clipboard via the existing test seam used by note `r` tests).
  - `TestClickSelectsThenDoubleClickOpens`.
  - `TestOverviewMenuRows`.
- [ ] **Step 2:** FAIL.
- [ ] **Step 3: implement**; i18n keys in four bundles; help rows.
- [ ] **Step 4:** PASS + `go test ./internal/tui/` (whole package) green.
- [ ] **Step 5: commit** `feat(tui): overview anchors — tab/enter/click open, backspace back`.

### Task 5: steer verbs

**Files:**
- Modify: `internal/steer/steer.go` (`MaxCommandBytes = 512 << 10` + comment; `Command.Title`, `Command.Text`; `Reply.Overviews []Overview`; `type Overview`; cmd list comment), `internal/tui/steer.go` (dispatch `overview_` prefix before `steerRefusal`; enum refusals: add needs text, set/show/rm need a file id)
- Create: `internal/tui/steer_overview.go` (`steerOverview`, `steerOverviewAdd/Set/List/Show/Rm`, `overviewProto`, `checkAnchorsCmd` + `anchorsCheckedMsg`)
- Test: `internal/tui/steer_overview_test.go`, `internal/steer/steer_test.go` (a 100 KiB command round-trips)

**Interfaces — Produces:**
```go
type Overview struct {
    ID string `json:"id"`; Title string `json:"title"`; State string `json:"state"`
    Anchors int `json:"anchors"`; Unresolved []string `json:"unresolved,omitempty"`; Text string `json:"text,omitempty"`
}
type anchorsCheckedMsg struct{ tag string; missing []bool; cmd steer.Command; reply bool }
```
- add: validate (text non-empty, ≤ 64 KiB, title one line ≤ 200 runes, `overviewCount() < 20`); `d := newOverviewDoc`; foreground unless `c.Background` or `m.steerRefusal() != ""` (then `Detail` = `added f12 in the background (<why>)`), else `Detail` = `showing f12`; register; return `checkAnchorsCmd` which stats every path anchor off-thread (`os.Stat(filepath.Join(m.currentWorktree, filepath.FromSlash(path)))`) — note anchors checked on the UI thread in the msg handler; the handler sets `missing`, paints, and answers with `Overviews[0]` incl. `Unresolved` (dests, document order).
- set: find by id (must be an overview, else `no overview f<n>`); remember `oldDest := anchors[sel].dest`; replace text (+title if set); `keepPlace`; lay out again (`fillOverview` with the frame size from `liveDoc`/`viewerGeom`); sel = first anchor with `dest == oldDest` else -1; then check anchors and answer.
- list/show/rm as the spec; rm = `closeDoc`.
- [ ] **Step 1: failing tests:** `TestOverviewAddShowsItAndAnswers`, `TestOverviewAddBackground`, `TestOverviewAddWhileBusyFallsBackToBackground` (`m.filterTyping = true`), `TestOverviewAddListsUnresolvedAnchors`, `TestOverviewAddLimits` (empty, too big, 21st, two-line title), `TestOverviewSetKeepsTheSelection`, `TestOverviewSetUnknownID`, `TestOverviewListShowRm`, steer `TestLargeCommandRoundTrips`.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS (`go test ./internal/steer/ ./internal/tui/ -run 'Overview|Command'`).
- [ ] **Step 5: commit** `feat(steer,tui): overview_add/set/list/show/rm`.

### Task 6: CLI `gg session overview`

**Files:**
- Create: `internal/cli/session_overview.go`, `internal/cli/session_overview_test.go`
- Modify: `internal/cli/session.go` (dispatch `case "overview"`, usage line)

Behaviour: as the spec's CLI block. Text from `--file` else stdin (`io.ReadAll(io.LimitReader(stdin, 64<<10+1))`); > 64 KiB → exit 2 `session overview: the text is over 64 KiB`; empty → exit 2; title required for add, one line. Needs a TUI like notes (`overviews need a gg TUI (only gg web is live for this worktree)` exit 1). Plain output: add/set → `f12` then `unresolved: <dest>` lines; list → `f12  shown  3 anchors  Tour`; show → title line, blank, text; rm → `closed f12`. `--json` prints `Reply.Overviews`.

The CLI needs stdin: check how `sessionNote` gets stdin (it does not); thread `stdin io.Reader` from `session.go`'s caller the way `gg apply`/`gg batch` read stdin (grep `stdin` in `internal/cli`).

- [ ] **Step 1: failing tests** using the fake-consumer pattern from `session_note_test.go` (a goroutine answering the inbox): add from stdin, add from file, set, list plain/json, show, rm, web-only exit 1, misuse exit 2 cases.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** `go test ./internal/cli/ -run 'Overview|Session'` PASS.
- [ ] **Step 5: commit** `feat(cli): gg session overview add|set|list|show|rm`.

### Task 7: docs, skill, live check

**Files:** `CHANGELOG.md` (entry on top), `README.md` (session verbs), `docs/CLAUDE-details.md` (Overview documents subsection), `internal/agentskill/using-gg.md` (Overviews section), `internal/agentskill/*.go` Version 107, `.claude/skills/using-gg/SKILL.md` (regenerate = `agentskill.SkillFile()` via a throwaway `go run`).

- [ ] **Step 1:** write docs + skill; regenerate the dogfood copy.
- [ ] **Step 2:** `go test ./internal/agentskill/ ./internal/tui/ -run 'Skill|Dogfood|Help|I18n'` PASS.
- [ ] **Step 3:** build `bin/gg` in the worktree; drive it headlessly (own tmux session `claude-overview`): open gg, `gg session overview add` a 5-anchor overview over this repo's files from a second pane (with `GG_INBOX` / the worktree inbox), tab ×2, enter, check the file and line, backspace, check the overview + selected anchor, tab, enter. Record what was seen in the ledger.
- [ ] **Step 4:** `./test.sh race` → "all green".
- [ ] **Step 5: commit** `docs: overview documents — changelog, readme, details, skill v107`.
