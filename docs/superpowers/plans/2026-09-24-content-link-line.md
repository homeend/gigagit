# Content links v2 — line focus Implementation Plan

> **For agentic workers:** executed inline by the session that wrote it (NO subagents — project rule).

**Goal:** a content link's `:<line>` lands the full-screen viewer's cursor on that line; the viewer's own "Copy file link" carries the cursor line; `gg link --content <path>:<line>` works.

**Architecture:** the line already rides `steer.Command.Line`; the TUI parks it on `fileViewer.pendingLine` until the async load fills the lines. The CLI counts the file's lines through `svc.WorktreeFile`.

**Tech Stack:** Go, Bubble Tea.

**Spec:** `docs/superpowers/specs/2026-09-24-file-content-link-design.md` → "Addendum 2".

## Global Constraints

- Every new TUI string goes through `i18n.T` with a literal key in all four bundles (ja/ko/zh/ru).
- Tests call `t.Parallel()`; a real git repo, no mocks.
- Web unchanged.

## Review Focus

1. A line on a placeholder load (empty file, too large, load failed) must not move the cursor or raise the past-EOF notice.
2. A file with trailing blank lines: CLI count and viewer count agree (both trim trailing newlines).
3. The files-tree / Files-panel row link must stay line-less after `fileRowPath` gains a line.
4. A stale `fileContentMsg` (tag mismatch) never consumes `pendingLine`.
5. CRLF files: the CLI counts `a\r\nb\r\n` as 2 lines.

---

### Task 1: CLI — accept `:<line>`, refuse past EOF

**Files:** Modify `internal/cli/link.go` (guard at ~97, presence block at ~150, delete `linkArgHasLine`); Test `internal/cli/link_content_test.go`.

- [ ] Tests: `TestLinkContentWithLine` (`README.md:1` → suffix `/README.md:1?view=content`); `TestLinkContentLinePastEOFExits1` (a 2-line file `x.txt:3` → exit 1, stderr `x.txt has 2 lines`; `x.txt:2` → 0); `TestContentLineCount` table (`""`→0, `"a"`→1, `"a\n"`→1, `"a\n\n\n"`→1, `"a\r\nb\r\n"`→2, `"a\rb"`→2); drop `README.md:3` from the usage-error table, add `README.md:old:1` (exit 2). Watch them fail.
- [ ] Remove `linkArgHasLine` from the guard (message: `--content needs one file path, with no #<hunk>`) and delete the function. After the presence check, when `l.Line > 0`: read `svc.WorktreeFile`, `n := contentLineCount(data)`, `l.Line > n` → `error: <path> has <n> lines`, exit 1.
- [ ] `contentLineCount`: trim trailing `\n`, normalise `\r\n`→`\n` then lone `\r`→`\n`, `""`→0, else `strings.Count(s,"\n")+1`. Note: the trim runs AFTER normalising, so `a\r\n` → 1.
- [ ] `go test ./internal/cli/ -run 'Content'` green; commit.

### Task 2: TUI landing sets the cursor

**Files:** `internal/tui/file_viewer.go`, `internal/tui/steer_nav.go`, `internal/tui/model.go` (fileContentMsg fileViewer arm), i18n bundles; Test `internal/tui/file_viewer_test.go`.

- [ ] Tests: `TestContentLinkLineLandsTheCursor` (a 60-line file, `Line{Side:"new",No:40}` → `fv.p.cur==39`, line 40 inside `[sel, sel+rows)`, rendered screen contains `line 40` text); `TestContentLinkLinePastEOFClamps` (5-line file, line 99 → `cur==4`, status contains `line 99 is past the end of`); `TestContentLinkLineOnEmptyFileIgnored` (empty file, line 3 → `cur==0`, no past-end status). Watch them fail.
- [ ] `fileViewer.pendingLine int`; `openFileViewer(path string, line int)`; steer passes `c.Line.No` when `c.Line != nil`.
- [ ] In the fill: if `pendingLine > 0 && len(lines) > 0 && lines[0].src` → `cur = min(pendingLine, len)-1`; `sel = previewClamp(cur - rows/2, len, rows, mode)`; past end → `m.statusMsg = i18n.T("line %d is past the end of %s (%d lines)", …)`. Clear `pendingLine` after any fill of this tag. A search, if active, still wins (it runs after).
- [ ] i18n key in ja/ko/zh/ru; `go test ./internal/tui/ -run 'FileViewer|ContentLink|I18n|Menu'` green; commit.

### Task 3: the viewer's Copy file link carries the cursor line

**Files:** `internal/tui/file_link.go`; Test `internal/tui/file_link_test.go` / `file_viewer_test.go`.

- [ ] Tests: `TestFileViewerCopyFileLinkCarriesTheCursorLine` (viewer, alt+↓ ×2 → copied ends `/main.go:3?view=content`); the existing Files-panel test additionally asserts no `:` after `a.txt` (already `HasSuffix "/a.txt?view=content"` — keep). Watch fail.
- [ ] `fileRowPath() (path string, line int, ok bool)`; viewer: `line = p.cur+1` when `len(p.lines)>0 && p.lines[p.cur].src`. `contextFileLinkRow` → `m.buildLinkFor(addr, model.NoteSideNew, line, 0, model.ContentHint)` (keep the hint-validity guard of `hintedLinkFor`). Update other callers of `fileRowPath`.
- [ ] Green; commit.

### Task 4: mouse wheel over the viewer

**Files:** `internal/tui/mouse.go`; Test `internal/tui/file_viewer_test.go`.

- [ ] Test `TestFileViewerWheelScrolls` (60-line file, wheel down → `sel` grows by the wheel step, `cur` unchanged). Watch fail.
- [ ] Case `*fileViewer` with `wheel != 0`: `fv.p.sel = previewClamp(fv.p.sel+wheel, len, rows, mode)`.
- [ ] Green; commit.

### Task 5: e2e, docs, skill

- [ ] `s99_content_links.toml`: `link --content f1.txt:1` exit 0 `/f1.txt:1?view=content`; `f1.txt:9` exit 1.
- [ ] CHANGELOG, README, `docs/CLAUDE-details.md` (Content links), `internal/agentskill/using-gg.md` + `agentskill.Version` 92→93, regenerate `.claude/skills/using-gg/SKILL.md`.
- [ ] `./test.sh` then `./test.sh race`; build verify binary; commit.
