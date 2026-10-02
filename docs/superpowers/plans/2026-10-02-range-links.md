# Range Links Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (inline, this session — no implementer subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mark lines in the TUI diff or a file view, copy one `gg://…:<a>-<b>` link, and have every consumer (TUI landing, CLI, MCP, notes, highlights) honour the range; `gg link text <link>` prints the lines.

**Architecture:** The range lives in the grammar (`model.Link.End`) and is resolved once (`domain.ResolveLink` → `Resolved.End`, strict block-fingerprint check → `ErrLinkStale`). `steer.Line.End` carries it to the one TUI landing funnel, which restores a frozen `lineSel`. Producers read the selection bounds and ask the domain for the block fingerprint.

**Tech Stack:** Go 1.26, Bubble Tea TUI, real-git tests in `t.TempDir()`, e2e TOML scenarios.

**Spec:** `docs/superpowers/specs/2026-10-02-range-links-design.md`

## Global Constraints

- Work only in `/work/gigagit/.claude/worktrees/range-links` (branch `feat/range-links`); never `git add -A`; never chain `git commit` after `| tail`.
- `tui`/`cli` never import `internal/git`; reads go through `domain`.
- Every user-visible TUI string goes through `i18n.T` with a literal key present in all four bundles (ja/ko/zh/ru).
- Engine/CLI/steer-reply prose stays English.
- New tests call `t.Parallel()` where the package's tests do.
- After any `internal/web/static/*.js` edit run `go test ./internal/web`.
- Single-line links (`End == 0`) keep today's lenient behaviour, byte for byte.
- Fingerprint slot is `~<8 lowercase hex>`; `LinkFingerprintOK` unchanged.
- Stale message, verbatim: `the link is no longer valid: lines <a>-<b> of <path> have changed since it was copied`.
- CLI surface changes → `internal/agentskill/using-gg.md` + `agentskill.Version` bump (check the current value on `main` right before bumping).

## Review Focus

- A range link with `~fp` pasted where the block moved intact → must be the stale error, not a followed landing.
- `gg://C:/src/repo/a-b.go:3-7` / a path segment containing `-` or `~` after a drive colon → path survives, range parses.
- A diff selection whose first/last rows are gap rows on the cursor side → range trimmed to numbered rows; all-gap → no link.
- A range link landing in a diff where line `b` is past the last line or under a fold → fold opened, selection clamped, no panic.
- `gg session highlight add gg://…:3-7~<fp>` → bands 3..7 (the old shim silently dropped the range).

---

### Task 1: Grammar — `Link.End` and the block fingerprint

**Files:**
- Modify: `internal/model/link.go` (Link struct, `String`, `ParseLink`, `splitLinkLine`, grammar doc comment)
- Modify: `internal/model/linkfp.go`
- Test: `internal/model/link_range_test.go` (new)

**Interfaces:**
- Produces: `Link.End int` (0 = single line; else `End > Line`); `model.BlockFingerprint(lines []string) string`; `splitLinkLine(t) (rest string, side NoteSide, line, end int, fp string, err error)`.

- [ ] **Step 1: failing tests** (`link_range_test.go`)

```go
func TestParseLinkRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in        string
		line, end int
		side      NoteSide
		fp        string
	}{
		{"gg://r/a.go:3-7", 3, 7, NoteSideNew, ""},
		{"gg://r/a.go@staged:old:3-7", 3, 7, NoteSideOld, ""},
		{"gg://r/a.go:3-7~0123abcd", 3, 7, NoteSideNew, "0123abcd"},
		{"gg://r/a.go:3-3", 3, 0, NoteSideNew, ""},          // collapses
		{"gg:///C:/src/r/a-b~c.go:3-7", 3, 7, NoteSideNew, ""}, // drive colon + odd name
		{"gg://r/a.go@" + strings.Repeat("a", 40) + ":3-7", 3, 7, NoteSideNew, ""},
	}
	for _, c := range cases {
		l, err := ParseLink(c.in)
		if err != nil { t.Fatalf("%s: %v", c.in, err) }
		if l.Line != c.line || l.End != c.end || l.Side != c.side || l.Fingerprint != c.fp {
			t.Errorf("%s: got %d-%d %s %q", c.in, l.Line, l.End, l.Side, l.Fingerprint)
		}
	}
}

func TestParseLinkRangeErrors(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"gg://r/a.go:7-3",   // ends before it starts
		"gg://r/a.go:0-3",
		"gg://r/a.go:3-",
		"gg://r/a.go@" + strings.Repeat("a", 40) + ":3-7~0123abcd", // fp on a commit
	} {
		if _, err := ParseLink(in); !errors.Is(err, ErrLink) { t.Errorf("%s: %v", in, err) }
	}
}

func TestLinkRangeString(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"gg://r/a.go:3-7", "gg://r/a.go@staged:old:3-7~0123abcd"} {
		l, _ := ParseLink(s)
		if got := l.String(); got != s { t.Errorf("round trip: %s → %s", s, got) }
	}
	if got := (Link{Repo: LinkRepo{Name: "r"}, Path: "a.go", Target: LinkTarget{State: StateUnstaged}, Line: 3, End: 3}).String(); got != "gg://r/a.go:3" {
		t.Errorf("End == Line prints as one line, got %s", got)
	}
}

func TestBlockFingerprint(t *testing.T) {
	t.Parallel()
	a := BlockFingerprint([]string{"  x := 1", "", "return x\r"})
	if a != BlockFingerprint([]string{"x := 1", "", "\treturn x"}) { t.Error("indent / CR must not matter") }
	if a == BlockFingerprint([]string{"x := 1", "return x"}) { t.Error("a dropped blank line is a change") }
	if BlockFingerprint([]string{" ", ""}) != "" { t.Error("all-blank block has no fingerprint") }
	if !LinkFingerprintOK(a) { t.Errorf("shape: %q", a) }
}
```

- [ ] **Step 2:** `go test ./internal/model -run 'Range|BlockFingerprint'` → FAIL (undefined `End`, `BlockFingerprint`).
- [ ] **Step 3: implement.**
  - `Link`: add `End int // last line of a range (1-based, > Line); 0 = a single line`.
  - `BlockFingerprint`: trim each line, if all empty return `""`, FNV-1a 32 over `strings.Join(trimmed, "\n")`, `%08x`.
  - `splitLinkLine`: after peeling `~fp` (peel only when what precedes `~` is `<n>` or `<n>-<m>`), split `num` on the first `-`; both halves must be numbers for it to be a range; `b < a` → `"%w: a range ends before it starts, got %q"`; missing end → `"%w: a line number is missing after \"-\""`; `a == b` → end 0. A non-numeric tail is still left alone (drive colon rule).
  - `ParseLink`: `l.End = end` beside `l.Line`; "a line or a hunk, not both" already covers range + hunk.
  - `String`: after the line number, `if l.End > l.Line { "-" + End }`, then `~fp`.
  - Grammar comment: `[:<line>[-<end>]]`.
- [ ] **Step 4:** `go test ./internal/model` → PASS (whole package).
- [ ] **Step 5:** commit `feat(model): a gg:// link can name a range of lines`.

### Task 2: Resolver — `Resolved.End`, strict block check, `LinkText`

**Files:**
- Modify: `internal/domain/linkresolve.go` (Resolved, the two places that fill `Line`, the anchor call's error)
- Modify: `internal/domain/linkanchor.go`
- Create: `internal/domain/linktext.go`
- Test: `internal/domain/linkrange_test.go` (new), `internal/domain/linktext_test.go` (new)

**Interfaces:**
- Consumes: `model.Link.End`, `model.BlockFingerprint`.
- Produces:
  - `Resolved.End int`
  - `var ErrLinkStale = errors.New("the link is no longer valid")`; resolver returns `fmt.Errorf("%w: lines %d-%d of %s have changed since it was copied", ErrLinkStale, a, b, path)`
  - `func (s *Service) LinkBlockFingerprint(ctx, l model.Link) string` — `""` when `l.End == 0`, committed, unreadable, past EOF or all blank
  - `type LinkText struct { Path, Target string; Side model.NoteSide; Start, End int; Lines []string }`
  - `func (s *Service) LinkText(ctx context.Context, res Resolved) (LinkText, error)` — the lines `res.Line..max(res.End,res.Line)`; errors: no line, a hunk-only link, past the end (`"%s has %d lines"`), unreadable.

- [ ] **Step 1: failing tests.** In a real temp repo (existing `newTestRepo`-style helper in `internal/domain`, as `linkanchor_test.go` uses): file `a.go` with 10 distinct lines, committed, then:
  - `TestResolveRangeSame`: link `:3-5~<BlockFingerprint(lines 3..5)>` → `res.Line==3, res.End==5`, `Anchor.State==AnchorSame`.
  - `TestResolveRangeEditedInside`: change line 4 on disk → `errors.Is(err, ErrLinkStale)`, message contains `lines 3-5 of a.go`.
  - `TestResolveRangeMovedIsStale`: insert a line at the top (block intact at 4-6) → `ErrLinkStale`.
  - `TestResolveRangePastEnd`, `TestResolveRangeUnreadable` (file deleted) → `ErrLinkStale`.
  - `TestResolveRangeNoFingerprint`: `:3-5` → resolves, `End==5`, no read.
  - `TestResolveRangeStagedOld`: `@staged:old:3-5~fp` reads HEAD.
  - `TestResolveRangeCommitted`: `@<sha>:3-5` → `End==5`.
  - `TestLinkBlockFingerprint`: equals `model.BlockFingerprint` of the lines; `""` past EOF.
  - `TestLinkText`: working tree, `@staged`, `@<sha>` new and `:old:` (parent), pair `@a..b` both sides; single line; past-end error.
- [ ] **Step 2:** `go test ./internal/domain -run 'Range|LinkText|LinkBlock'` → FAIL.
- [ ] **Step 3: implement.**
  - `Resolved.End`: set from `l.End` wherever `Line: l.Line` is set.
  - `anchorLink` returns `error`; when `l.End > 0`: read lines, `l.End > len(lines)` or `!ok` or `BlockFingerprint(lines[l.Line-1:l.End]) != l.Fingerprint` → stale error; else `res.Anchor = LineAnchor{Asked: l.Line, State: AnchorSame, Matches: 1}`. The `End == 0` path is untouched. `ResolveLink` returns the error.
  - `LinkBlockFingerprint` beside `LinkLineFingerprint`.
  - `LinkText`: uncommitted → `linkSideLines` on a link rebuilt from `res` (state + side); committed → `ResolveBytes(FileRef{Source: SourceCommit, Locator: <sha>, Path})` where the locator is `res.Commit` (new), `res.Commit+"^"` (`:old:` of a plain commit), `res.Pair.A` / `res.Pair.B`, or the preview tip; same CRLF/binary/size normalisation — factor the tail of `linkSideLines` into `splitTextLines(data) ([]string, bool)` and use it in both.
- [ ] **Step 4:** `go test ./internal/domain` → PASS.
- [ ] **Step 5:** commit `feat(domain): resolve a range link; a changed block is a stale link`.

### Task 3: Steer + linknav carry the end

**Files:**
- Modify: `internal/steer/steer.go` (`Line.End int json:"end,omitempty"`; validation: `End != 0 && End < No` is refused where `Line` is validated)
- Modify: `internal/linknav/linknav.go` (`lineOf` sets `End: res.End`; `AtLink` sets `l.End = c.Line.End`)
- Test: `internal/linknav/linknav_test.go`, `internal/steer/steer_test.go`

**Interfaces:**
- Produces: `steer.Line.End` (0 = one line), set by `linknav.Command` for every target kind that goes through `lineOf`.

- [ ] **Step 1:** tests `TestCommandCarriesRangeEnd` (working tree, commit, pair links with `:3-5` → `c.Line.End == 5`), `TestAtLinkKeepsRange` (`AtLink(...).String()` ends `:3-5`), steer JSON round trip with `end`.
- [ ] **Step 2:** run → FAIL. **Step 3:** implement. `AtLink` deliberately drops the fingerprint (already verified by the resolver), as it does today.
- [ ] **Step 4:** `go test ./internal/steer ./internal/linknav` → PASS. **Step 5:** commit `feat(linknav): a navigate carries a range's last line`.

### Task 4: CLI — produce, resolve, highlight, note

**Files:**
- Modify: `internal/cli/link.go` (`buildLink`: keep `probe.End`, fingerprint via `LinkBlockFingerprint` unless `--no-fingerprint`; `linkResolve` JSON `end_line`; plain output prints the range)
- Modify: `internal/cli/session.go` (delete `splitLinkRange`; highlight add uses `res.End`; keep the `--end` conflict error when `res.End > 0`)
- Modify: `internal/cli/note.go` (`case link.Line > 0`: `rng = [2]int{link.Line, max(link.End, link.Line)}`)
- Modify: `linkExit` (or its equivalent) so `domain.ErrLinkStale` prints the message and exits 1
- Test: `internal/cli/link_range_test.go` (new)

- [ ] **Step 1: failing tests**
  - `TestLinkProducesRange`: `gg link a.go:3-5` prints `…/a.go:3-5~<fp>`; `--no-fingerprint` prints `:3-5`; `--cached` → `@staged:3-5~<fp>`.
  - `TestLinkResolveRangeJSON`: `end_line: 5`.
  - `TestLinkResolveStaleRange`: edit line 4 → exit 1, stderr holds the verbatim stale message.
  - `TestHighlightAddRangeLink`: with a fake inbox (existing highlight tests' seam) `:3-5~<fp>` sends `Start 3, End 5`; `--end 9` with it → exit 2.
  - `TestNoteAddRangeLink`: stored note `Range == [3,5]`.
- [ ] **Step 2:** run → FAIL. **Step 3:** implement. **Step 4:** `go test ./internal/cli` → PASS.
- [ ] **Step 5:** commit `feat(cli): range links in gg link, link resolve, highlight add and note add`.

### Task 5: `gg link text` + MCP

**Files:**
- Modify: `internal/cli/link.go` (subcommand `text`, `--json`)
- Modify: `internal/mcp/links.go` (`gg_link_text`; `end_line` on `gg_link_resolve`; tool descriptions)
- Test: `internal/cli/link_text_test.go` (new), `internal/mcp/links_test.go`

- [ ] **Step 1: failing tests**
  - `TestLinkTextRange`: stdout is exactly
    `a.go @ working tree (new), lines 3-5\n3\t<l3>\n4\t<l4>\n5\t<l5>\n`.
  - header variants: `@ index`, `@ <sha7>`, `@ <a7>..<b7>` with `(old)`; a single line says `line 3`.
  - `TestLinkTextJSON`: `{"path","target","side","start","end","lines"}`.
  - `TestLinkTextStale` → exit 1, no stdout. `TestLinkTextNoLine` (file-only or `#1`) → exit 2.
  - MCP: `gg_link_text` returns the same JSON; stale → tool error.
- [ ] **Step 2–4:** fail → implement over `svc.LinkText` → `go test ./internal/cli ./internal/mcp` PASS.
- [ ] **Step 5:** commit `feat(cli,mcp): gg link text prints the lines a link names`.

### Task 6: TUI — copy a range link

**Files:**
- Modify: `internal/tui/link.go` (`buildLinkFor` gains `end int` and block text; new `linkRangeAtSelection`; `contextLinkText` diff arm; `compareLinkText`)
- Modify: `internal/tui/saved_pair_notes.go` (`scopeLinkFor`), `internal/tui/stash_link.go` (`pairFileLinkFor`) — an `end` parameter
- Modify: `internal/tui/file_link.go` (`fileRowPath` returns the selection's range for the viewer/preview)
- Modify: `internal/tui/diff_select.go` (menu row label; selection footer hint mentions `L`)
- Modify: `internal/i18n` bundles (4) — keys: `Copy link to selected lines (%d)`, `▸ nothing to link on this side`, `▸ a link marks lines of one file`
- Test: `internal/tui/link_range_test.go` (new)

**Interfaces:**
- Consumes: `Service.LinkBlockFingerprint` is NOT used here — the TUI already holds the rows' raw text; it computes `model.BlockFingerprint(lines)` over the side's text for rows `a..b`. Where the selection spans folded context (rows not in `v.lines`), the fingerprint comes from `m.svc.LinkBlockFingerprint` instead (one sync read, `updateThreadCtx`).
- Produces: `func (m Model) linkRangeAtSelection() (side model.NoteSide, a, b int, lines []string, st selState)` where `st` is `selNone | selOK | selEmptySide | selCrossFile`.

- [ ] **Step 1: failing tests** (Headless model, existing `link_fp_test.go` patterns)
  - `TestRangeLinkDiffNewSide`: unstaged diff, cursor new side, space + 2×down, `L` → clipboard `…:<a>-<b>~<fp>` with `fp == BlockFingerprint` of those working-file lines; selection still on.
  - `TestRangeLinkDiffOldSide`: `…:old:<a>-<b>~fp` (fp of the index text).
  - `TestRangeLinkTrimsGapRows`: selection starting on a deletion-only row with a new-side cursor → range starts at the next numbered row.
  - `TestRangeLinkAllGaps` → nothing copied, notice `▸ nothing to link on this side`.
  - `TestRangeLinkStackedCrossFile` → notice `▸ a link marks lines of one file`.
  - `TestRangeLinkCommitDiff` → `@<sha>:a-b`, no fp. `TestRangeLinkPair` → `@a..b:old:a-b`.
  - `TestRangeLinkFileViewer` / `TestRangeLinkPreview` → `…:a-b~fp?view=content`.
  - `TestRangeLinkOneLineSelection` → plain single-line link `:<n>~<lineFP>`.
  - menu: `.` shows `Copy link to selected lines (3)`.
- [ ] **Step 2–4:** fail → implement → `go test ./internal/tui -run 'RangeLink|Link|Select'` then whole `./internal/tui ./internal/i18n` PASS.
- [ ] **Step 5:** commit `feat(tui): L copies a link to the selected lines`.

### Task 7: TUI — land on a range

**Files:**
- Modify: `internal/tui/steer_nav.go` (diff landing: after `setCursorLine`, when `c.Line.End > c.Line.No` find the row of `End` on the same side — clamp to `lastLineNoIn`, expand its fold — and set `v.lsel = lineSel{on: true, anchor: li, end: liEnd, fixed: true}` BEFORE `landOnSide` is consulted, with `v.onOld = old` set directly; detail/notice `opened <file>:<a>-<b>`)
- Modify: `internal/tui/diff_stack_notes.go` (stacked `land` carries `end`; same restore)
- Modify: `internal/tui/steer_nav.go` content-link arm + `file_viewer.go` / `steer_files.go` (`d.pendingEnd = c.Line.End` — `landPendingLine` already selects)
- Modify: `internal/tui/goto_link.go`, `internal/tui/model.go` (start-at path keeps `End` through `AtLink`; a stale error from the `#` prompt shows the translated notice)
- Modify: i18n bundles — key `the link is no longer valid: lines %d-%d of %s have changed since it was copied`
- Test: `internal/tui/link_range_land_test.go` (new)

- [ ] **Step 1: failing tests**
  - `TestLandRangeDiffNew`: steer navigate with `Line{Side:"new", No:3, End:5}` → `v.lsel` on, fixed, bounds map to rows of lines 3 and 5, cursor on line 3, `!v.onOld`, notice holds `:3-5`.
  - `TestLandRangeDiffOld` → `v.onOld`.
  - `TestLandRangeRoundTrip`: copy a range link with `L`, clear, paste it into `#` → same `lsel.bounds`, same side.
  - `TestLandRangeFolded`: range end under a fold → fold expanded.
  - `TestLandRangeClamped`: `End` past the last line (unfingerprinted) → selection ends at the last line.
  - `TestLandRangeStacked`, `TestLandRangeFileViewer`, `TestLandRangePreview`.
  - `TestPasteStaleRange`: edit the file, paste the fingerprinted link → nothing opens, status/notice holds the translated stale sentence.
  - `TestLandSingleLineUnchanged`: `End == 0` → no selection (guards today's behaviour).
- [ ] **Step 2–4:** fail → implement → `go test ./internal/tui` PASS.
- [ ] **Step 5:** commit `feat(tui): opening a range link marks its lines again`.

### Task 8: TUI — a ranged note marks its lines

**Files:**
- Modify: `internal/tui/diff_notes.go` (new `noteRangeRows() map[int]bool` per side: rows whose side number is within any active note's `Range` with `Range[1] > Range[0]`, cached with the note row index)
- Modify: `internal/tui/diff_render.go` (`segCell`/`diffCell`: the one separator column between gutter number and text shows `▎` in the note style on marked rows of that side)
- Modify: `internal/tui/styles.go` only if no note-coloured style is reachable
- Test: `internal/tui/diff_note_range_test.go` (new)

- [ ] **Step 1: failing tests**: a note with `Range [3,5]` side new → rendered rows 3..5 carry `▎` in the right pane, row 2 and 6 do not, the left pane never; a `[4,4]` note marks nothing; stacked view marks only the note's file; width of every rendered row unchanged (`lipgloss.Width` equality against an unmarked render).
- [ ] **Step 2–4:** fail → implement → `go test ./internal/tui` PASS (goldens under `e2e` are regenerated in Task 10 if a scenario shows a hunk note).
- [ ] **Step 5:** commit `feat(tui): a note on a range marks the lines it is about`.

### Task 9: Web — land on the first line, name the range

**Files:**
- Modify: `internal/web/steer.go` (op-line text `opened <path>:<a>-<b>` when `Line.End > Line.No`; stale error passes through as the navigate error)
- Modify: `internal/web/static/*.js` only if the op line is built client-side
- Test: `internal/web/steer_test.go`

- [ ] **Step 1:** test: a navigate with `End` yields the range in the op line / reply. **Step 2–4:** fail → implement → `go test ./internal/web` PASS. **Step 5:** commit `feat(web): a range link lands on its first line and names the range`.

### Task 10: e2e, docs, skill

**Files:**
- Create: `e2e/scenarios/s107_range_links.toml` (number = next free; check the directory), TUI golden via `-update`
- Modify: `CHANGELOG.md`, `docs/CLAUDE-details.md`, `CLAUDE.md` (model row: `:<line>[-<end>]~<fp>`), `README.md` (links section), `internal/agentskill/using-gg.md` + `agentskill.Version`

- [ ] **Step 1:** scenario: commit a 10-line file; `gg link a.txt:3-5` (assert stdout shape); `gg link text <link>` (assert the three lines); modify line 4; `gg link text <link>` → exit 1 + stale message; `[tui]` step: open the diff, select 3 lines, `L`, golden screen; paste link, golden screen of the landed range.
- [ ] **Step 2:** `./test.sh e2e` → PASS (after `-update` for the new goldens only; inspect them by eye).
- [ ] **Step 3:** docs + skill; `go test ./internal/agentskill`.
- [ ] **Step 4:** `./test.sh race > <workspace>/race.log 2>&1`; green only if the log's last lines say `all green`.
- [ ] **Step 5:** commit `docs: range links` (and the e2e commit before it).

## Self-review

- Spec §1 → Task 1; §2–3 → Task 2; §4 → Task 6; §5 → Tasks 3, 7; §6 → Tasks 2, 5; §7 → Task 4; §8 → Tasks 4, 8; §9 → Task 9; §10 edges → tests in Tasks 1, 2, 6, 7; §11–12 → Task 10.
- Names used across tasks: `Link.End`, `BlockFingerprint`, `Resolved.End`, `ErrLinkStale`, `LinkBlockFingerprint`, `LinkText`, `steer.Line.End` — consistent.
