# Web external editor (J1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (inline, this session).

**Goal:** every TUI "open in editor" surface works from the page, the editor running in a page console.

**Architecture:** domain owns the editor argv, the view copies and an editor session (a Terminal PTY session that removes itself and its temp on exit); `POST /api/editor-open` resolves the source and starts it; editor.js opens the console and says `edited` / `viewed` when the session leaves.

**Spec:** docs/superpowers/specs/2026-10-25-web-external-editor-design.md

## Global Constraints

- The editor command comes from the server's env only. Paths are refused when they escape the tree.
- `internal/web` stays domain-only (archtest). The TUI behaviour is unchanged (its editor tests stay green).
- Every new key goes in help + foot chips; export every new JS name (a missing export breaks the page).

## Review Focus

- A path with `..` or an absolute path in the request → refused, no editor started.
- A view's temp file is removed after the editor exits, and so is the session.
- A live edit reloads the status (the file shows modified) — probe.
- An unknown rev / id → 404, no session.
- Hosted mode: the start goes through domain directly (no TUI starter round-trip).

---

### Task 1: domain editor seam
- Tests (domain/editor_test.go): EditorArgv table (vi `+N path`, code `--goto path:N`, hx `path:N`, unknown → path, line 0 → path, empty editor → default); ResolveEditor (VISUAL > EDITOR > default, whitespace = unset); WriteViewCopy (name ends in base, 0400, RemoveViewCopy removes); StartEditor with a script that exits → done ran, session gone from Sessions().List(); WorktreeAbsPath refuses `../x`.
- Move: tui resolveEditor/editorCommandAt/writeReadOnlyTempFile/removeTempFile delegate to domain; tui tests green.

### Task 2: /api/editor-open
- Tests (web/editor_test.go): source worktree → live, argv names the abs path (inject a starter seam to capture argv instead of a PTY); staged/commit/message/bookmark/shelf → a view copy whose bytes match; `../x` → 400; unknown rev → 404; oversized → 413; a missing source → 400.
- Server: handler + seam `s.editorStart` (default: svc.StartEditor), route under writeGuard.

### Task 3: editor.js + surfaces
- Node tests: `editorLine(rows, cur)` picks tr.cur rno else first visible; `diffEditReq(ctx, line)` → {source, path, rev, line} or null (compare / all-files / binary).
- Pins: menus rows (status file menu, staged menu, commit menu, commit files menu, bookmark/shelf entry menus), keys (commits I, diff e, history e, blame e), help rows, foot chips.
- editor.js: openInEditor, finish on sessions change (takeSessions hook), op lines.

### Task 4: probe + docs
- Probe (serve with EDITOR=fake script): live edit from the status menu → console opens, closes, `edited f.txt`, the file modified in the list, session gone; commit `I` → `viewed COMMIT_EDITMSG`, no `gg-*-COMMIT_EDITMSG` left; diff `e` at the clicked line → the fake's log shows the path; history/blame `e`; a sleeping fake keeps the console up until killed.
- CHANGELOG, parity doc (close the row + design differences), CLAUDE-details if needed.
