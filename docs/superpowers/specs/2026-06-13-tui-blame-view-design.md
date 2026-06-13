# TUI Blame View (`b`) — Design

**Date:** 2026-06-13
**Status:** Approved — ready for planning
**Depends on:** the view-stack primitive (`internal/tui/stack.go`) and the
file-history view (`internal/tui/history_view.go`), both shipped on `main`.

---

## 1. Goal

Pressing `b` on a file opens a **blame view**: the file's current content in a
single full-width pane, each line prefixed by a gutter naming the commit that
last touched it. Consecutive lines from the same commit collapse into one
**grouped block** — the gutter remark (hash + author + relative age) is drawn
only on the block's first line. Blame is **cross-linked** with history: `enter`
on a block drills into that commit's file history; `esc`/`b` unwinds one hop.

Blame is the **second consumer** of the view-stack primitive (history was the
first). It is additive: no existing surface changes behaviour.

## 2. Non-goals (v1)

YAGNI — explicitly out of scope for this version:

- **Reblame-parent walking** — drilling back through history is covered by the
  `enter` → history-at-commit cross-link.
- Line-range blame, `-w` / `--ignore-rev` toggles.
- Horizontal scroll of long source lines (blame lines are clipped to width).
- Mouse interaction (keyboard-only, like history).
- Any CLI surface (`gg blame` / `gg log <file>` stay deferred). No
  `agentskill.Version` bump.

## 3. Data layer

### 3.1 Model — `internal/model/blame.go`

```go
// BlameLine is one source line annotated with the commit that last changed it.
// Hash "" / all-zero means the line is not yet committed (working-tree change).
type BlameLine struct {
	Hash    string // full commit sha; "" for not-yet-committed
	Author  string // author name
	Time    int64  // author-time, unix epoch seconds
	Summary string // commit subject (first line)
	LineNo  int    // final line number, 1-based
	Content string // the source line text (no trailing newline)
}
```

Flat, one entry per source line. Grouping into blocks is a view concern
(§4.2), kept out of the model so the parser stays simple and the grouping is
independently testable.

### 3.2 Parser — `internal/git/blame.go`

`git blame --porcelain` emits, for the **first** appearance of each commit, a
full header:

```
<sha> <orig-line> <final-line> <num-lines-in-group>
author <name>
author-mail <email>
author-time <epoch>
author-tz <tz>
committer <name>
committer-mail ...
committer-time ...
committer-tz ...
summary <subject>
previous <sha> <path>        (optional; present when the file was renamed)
filename <path>
\t<source line content>
```

For **subsequent** lines from an already-seen commit, only the abbreviated
header line (`<sha> <orig> <final>`) plus the `\t<content>` line are emitted.

```go
// ParseBlamePorcelain parses `git blame --porcelain` output into one BlameLine
// per source line. Commit metadata (author, time, summary) is emitted in full
// only the first time a sha appears, so we cache it by sha and reuse it for the
// abbreviated repeats. The all-zero sha is normalised to Hash "" (uncommitted).
func ParseBlamePorcelain(data []byte) []model.BlameLine
```

Parsing rules:

- A line matching `^<40-hex> <int> <int>( <int>)?$` opens a new line record:
  capture the sha and the final line number (2nd integer).
- Header keys that follow (`author`, `author-time`, `summary`) populate the
  cache entry for the current sha. Unknown keys are ignored.
- The `\t`-prefixed line is the content; it closes the current record. Strip
  exactly one leading tab; keep the rest verbatim.
- The all-zero sha (`0000000000000000000000000000000000000000`) → `Hash = ""`.
  Git reports its author as "Not Committed Yet"; store that author as-is.
- Already-seen sha → reuse cached `Author`/`Time`/`Summary`.

### 3.3 Verb — `internal/git/blame.go`

```go
// Blame returns one BlameLine per line of path as of rev (rev "" = working
// tree). One invocation. The caller bounds nothing — blame is whole-file.
func (r *Repo) Blame(ctx context.Context, rev, path string) ([]model.BlameLine, error) {
	b := gitcmd.New("blame").
		Arg("--porcelain").
		ArgIf(rev != "", rev).
		Arg("--", path)
	res, err := r.Runner.Run(ctx, "git blame", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return ParseBlamePorcelain([]byte(res.Stdout)), nil
}
```

### 3.4 Query — `internal/domain/query.go`

`Service.repo` is a concrete `*git.Repo`; the engine `GitOps` interface is for
write operations only, so no interface threading is needed.

```go
// Blame returns per-line blame for path at rev under a Read reservation,
// coalesced per (rev, path).
func (s *Service) Blame(ctx context.Context, rev, path string) ([]model.BlameLine, error) {
	return query(ctx, s, "blame:"+rev+":"+path, func(ctx context.Context) ([]model.BlameLine, error) {
		return s.repo.Blame(ctx, rev, path)
	})
}
```

## 4. The surface — `internal/tui/blame_view.go`

### 4.1 Struct

```go
type blameView struct {
	ctx     navContext        // reused {path, rev}; rev "" = blame HEAD working content
	lines   []model.BlameLine
	blocks  []blameBlock      // grouped runs, computed once after load
	sel     int               // line cursor (index into lines)
	loading bool
	err     error
	tag     string            // gates stale loads
}

func newBlameView(ctx navContext) *blameView
```

It implements `surface`: `render(m Model) string` and
`update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)`.

### 4.2 Grouping

```go
type blameBlock struct {
	start, end int    // inclusive line-index range into lines
	hash       string // "" = uncommitted
	author     string
	time       int64
}

// groupBlame collapses maximal runs of lines sharing a Hash into blocks.
func groupBlame(lines []model.BlameLine) []blameBlock
```

Pure, no git/TUI deps. Each block records the range plus the first line's
commit metadata. `blockAt(blocks, sel int) blameBlock` (or -1) finds the block
containing the cursor for `enter` and continuation-line gutter blanking.

### 4.3 Async load

Mirrors history's `loadHistoryListCmd` tag-gating:

```go
type blameMsg struct {
	tag   string
	lines []model.BlameLine
	err   error
}

func (m Model) loadBlameCmd(ctx navContext, tag string) tea.Cmd // calls m.svc.Blame
```

`newBlameView` sets `loading=true` and a `tag` of `"blame:"+rev+":"+path`. The
`blameMsg` handler in `model.go` (gated by tag against `stackTop`) stores
`lines`, computes `blocks = groupBlame(lines)`, clears `loading`.

### 4.4 Rendering

Single full-width pane (history is split; blame is not):

- **Header**: `blame: <path>`, plus ` @ <shorthash>` when `ctx.rev != ""`.
- **Body**: for each visible line, `gutter + "│" + truncate(content, codeW)`.
  - Gutter width is fixed (≈ `shortHash(7) + 1 + author(≤12) + 1 + age(≤4)`,
    padded). The gutter text is drawn **only on a block's first line**; on
    continuation lines the gutter is blank-filled to the same width.
  - Uncommitted block (`hash == ""`) → gutter shows `(uncommitted)`.
  - Age is a short relative string (e.g. `3mo`, `6d`, `2y`) from `time`.
  - The cursor line (`i == sel`) is rendered with `selectedRow`.
- Vertical scrolling via the shared `windowRows(rows, body, sel)` helper.
- Loading / error / empty states mirror history (`(loading…)`, `error: …`,
  `(empty)`).
- **Hint**: `[↑↓] line  [enter] history  [esc] back  [q] quit`.
- Output clipped with `clipToHeight`.

### 4.5 Update (input)

```
ctrl+c / q     → tea.Quit
esc / b        → m.popSurface()        (unwind one hop)
down / j       → sel++ (clamped), re-window
up / k         → sel-- (clamped), re-window
enter          → block := blockAt(blocks, sel)
                 if block.hash != "":
                     m.pushSurface(newHistoryView(navContext{path: ctx.path, rev: block.hash}))
                 else: no-op
```

No right-pane reload on cursor move (unlike history) — blame has no side pane.

## 5. Entry points

All push `newBlameView` onto the stack via the existing dispatch (checked
right after the modal). The `rev` carried in is the only thing that differs:

| Source | Key | navContext |
|--------|-----|------------|
| Status panel (selected file) | `b` | `{path: file, rev: ""}` |
| Files-view tree (selected row) | `b` | `{path: treePath, rev: m.filesHash}` |
| Diff view | `b` | `{path: v.title, rev: v.rev}` |
| **History view** (selected commit) | `b` | `{path: h.ctx.path, rev: selectedFC.Hash}` |

The first three mirror exactly where `h` (history) is already wired — add a
sibling `case "b"` next to each existing `case "h"`. The history-view case is
new: it lets you pivot history → blame at the commit you're inspecting,
completing the cross-link cycle (blame `enter` → history; history `b` → blame).

Mouse: `handleMouse` already swallows events while the stack is non-empty
(`internal/tui/mouse.go`) — no change needed.

## 6. Testing (TDD)

**`internal/git/blame_test.go`**
- `ParseBlamePorcelain`: full-header-then-abbreviated-repeat across a
  multi-block fixture; uncommitted (all-zero sha → `Hash==""`, author kept);
  a renamed file (`previous`/`filename` lines present and ignored cleanly);
  correct `LineNo`/`Content` (one leading tab stripped, rest verbatim).
  Use a captured porcelain fixture string.
- `(*Repo).Blame`: against a real temp repo (`newTestRepo`) — make 2 commits
  touching different lines of a file, assert the blame splits into the right
  shas/line-counts.

**`internal/tui/blame_view_test.go`**
- `groupBlame`: consecutive runs collapse; singletons stay; all-same → one
  block; empty → none.
- `blameView.render`: gutter present on a block's first line and blank on its
  continuation lines; selected line highlighted; window clips to body height;
  uncommitted gutter shows `(uncommitted)`.
- `blameView.update`: `j`/`k` move + clamp the cursor; `enter` on a committed
  block pushes a `historyView` with `navContext{path, rev: block.hash}`;
  `enter` on an uncommitted block is a no-op; `esc` and `b` pop the surface.
- Entry points: `TestStatusBOpensBlame`, `TestFilesViewBOpensBlame`,
  `TestDiffViewBOpensBlame`, `TestHistoryBOpensBlameAtSelected` — each asserts
  the right surface type sits on top with the expected `navContext`.

**`internal/domain`** — a `Service.Blame` test paralleling the existing
`FileLog` query test (real temp repo, asserts non-empty per-line result).

## 7. Docs

- `CHANGELOG.md` — always.
- `README.md` — add `b` to the key reference.
- `internal/tui/help.go` — `b` rows + a "Blame view (b)" section.
- No `agentskill` bump (no CLI surface change).

## 8. File map

| File | Action |
|------|--------|
| `internal/model/blame.go` | create — `BlameLine` |
| `internal/git/blame.go` | create — `ParseBlamePorcelain` + `(*Repo).Blame` |
| `internal/git/blame_test.go` | create |
| `internal/domain/query.go` | modify — `Service.Blame` |
| `internal/domain/query_test.go` (or sibling) | modify — `Service.Blame` test |
| `internal/tui/blame_view.go` | create — `blameView`, `blameBlock`, `groupBlame`, load cmd, render, update |
| `internal/tui/blame_view_test.go` | create |
| `internal/tui/model.go` | modify — `blameMsg` case; `case "b"` on Status panel |
| `internal/tui/files_view.go` | modify — `case "b"` |
| `internal/tui/diff_view.go` | modify — `case "b"` in `updateDiffViewKey` |
| `internal/tui/history_view.go` | modify — `case "b"` → blame at selected commit |
| `internal/tui/diff_render.go` | modify — `diffHint` adds `[b] blame` |
| `internal/tui/help.go` | modify |
| `CHANGELOG.md`, `README.md` | modify |
