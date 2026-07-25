# Web discard-changes op + shared CRLF hunk fix — design

Date: 2026-07-25 · Branch: `feat/web-discard` (off `web-dev`) · Wave 3, track 3

Two queued smalls that share a "make the write path honest" theme and touch
NO client region the other wave-3 tracks own beyond one declared merge point.

## Part A — discard-changes reaches the web

### What exists (nothing new to invent)

`engine.Discard{Restore []string, Remove []string, All bool}` shipped long
ago (`internal/engine/discard.go`): `Restore` paths → `git restore
--worktree --` (discards unstaged edits, KEEPS staged hunks); `Remove`
paths → `git clean -f -d --`; decision-free; default TreeWrite reservation;
both steps run even if one errors (no silent half-discard). The TUI (d/D)
and CLI (`gg discard`) already dispatch it. The web op switch does not.

### Server: new `case "discard"` in `handleOpStart` (`internal/web/ophttp.go`)

Request: `{op: "discard", path}` (reuses the existing `Path` field; no new
request fields). Resolution is server-side against a fresh status read —
the established allowlist pattern (`remove-worktree` resolves against
`Worktrees()`, stash ops against `StashList()`):

1. `path == ""` or `!isGitArgSafe(path)` → 400 `"invalid path"`.
2. `svc.Status(r.Context())` error → 500.
3. Path not present in `st.Files` → 404 `"unknown path"` (also the
   freshness guard — a stale client row 404s instead of discarding the
   wrong thing).
4. `Kind == model.KindUnmerged` → 422 `"conflicted — resolve instead"`.
5. `Kind == model.KindUntracked` → `engine.Discard{Remove: []string{path}}`;
   otherwise → `engine.Discard{Restore: []string{path}}`.

Decision-free op through the normal `startOp` SSE transport (the delete-tag
pattern: the CLIENT confirms before POSTing; the op never forks). `done`
carries `changed: true` → the client's existing `refreshAfterOp` handles the
repaint. Whole-tree discard (`All`) is NOT exposed over the wire in this
increment.

### Client: RMB entries (danger + local confirm)

In the `#files-list` contextmenu handler, append at the END of the items
list (after the `unstage all` block, the declared merge point with the
hunks-UI track):

- `section === "changes"` rows:
  `{label: "discard changes", danger: true, act}` where act =
  `showLocalConfirm("Discard changes to <path>? This cannot be undone.",
  ["discard", "abort"], (o) => { if (o === "discard")
  startOp({op: "discard", path: f.path}, "discard " + f.path); })`.
- `section === "untracked"` rows:
  `{label: "delete untracked file", danger: true, act}` — same confirm shape
  with prompt `"Delete untracked <path>? This cannot be undone."`.
- `staged`-only rows get NO discard entry (`git restore --worktree` would
  no-op on a fully staged file); conflicts get none (server 422s anyway).

`"discard"` is already in `DANGER_OPTIONS` → the confirm button renders red
with zero styling work. `showCtxMenu` already supports `danger: true` on
menu rows (red row, ctx-menu precedent).

## Part B — the shared CRLF fix in hunkpick

### The bug (confirmed, documented in-repo)

`textdiff.splitLines` strips `\r` from EVERY line (deliberate: display
alignment identity) and `hunkpick.Resolved()` rejoins with a hardcoded
`"\n"` — so any CRLF file run through `FromDiff → Resolved` comes back
entirely LF, including untouched lines. The TUI's `H` stage-picker silently
does this to real files today (contradicting the original hunk-staging
design doc's "byte-faithful round-trip" claim); the web backend refuses CRLF
outright (422) instead of replicating the bug. `ParseConflict` (the x-editor
path) is NOT affected the same way — it splits on `\n` without stripping
`\r`, so content lines keep their `\r` and its output is already
CRLF-preserving; it is left alone.

### Fix: dominant-EOL rejoin (option 2 — no per-line terminator threading)

- `hunkpick.Doc` gains `EOL string`. Zero value `""` means `"\n"` — every
  existing constructor and test is untouched by default.
- `FromDiff(left, right)` sets `EOL = "\r\n"` iff both sides are
  **consistently CRLF**: a side is consistently CRLF when
  `bytes.Count(b, []byte("\n")) == bytes.Count(b, []byte("\r\n")) > 0`;
  a nil/empty side (or one with zero newlines) expresses no opinion. Any
  other combination (pure LF, mixed, disagreement) leaves `EOL` at `""`
  (LF) — mixed files keep today's normalize-to-LF behavior, documented.
- `Resolved()` joins with `d.EOL` (defaulting `""` → `"\n"`) and the
  `FinalNewline` append becomes that same terminator.

Consumers audited (all keep working unchanged): TUI stage picker
(`FromDiff` → display uses `\r`-stripped lines as today → `Resolved` now
emits CRLF for CRLF files → `engine.StageHunks` → `StageBlob` stages
byte-faithful content); TUI conflict pickers (`ParseConflict` docs keep
`EOL == ""`, behavior identical); web `loadHunkDoc` (below).

### Web guard: narrow from "any CRLF" to "mixed EOL only"

With the fix in, `internal/web/hunks.go` drops the blanket CRLF 422 and
replaces it with a mixed-EOL refusal (the one case dominant-rejoin would
still silently normalize):

```go
func mixedEOL(b []byte) bool {
    crlf := bytes.Count(b, []byte("\r\n"))
    return crlf > 0 && bytes.Count(b, []byte("\n")) > crlf
}
```

Guard: `if mixedEOL(work) || mixedEOL(index)` → 422
`"file mixes CRLF and LF line endings — stage the whole file instead"`.
Pure-CRLF files now get real hunk staging over the web AND in the TUI.

## Declared edit regions (for cross-track merge safety)

- Go (no other wave-3 track touches Go): `internal/web/ophttp.go` (one new
  switch case + doc-comment op list), `internal/web/hunks.go` (guard swap),
  `internal/hunkpick/hunkpick.go` (+`EOL`, `Resolved`),
  `internal/hunkpick/fromdiff.go` (EOL detection), tests beside each.
- `app.js`: ONE appended item block at the END of the `#files-list`
  contextmenu items (declared merge point with the hunks-UI track, which
  inserts after `stage <path>`; textual adjacency may conflict — resolution
  keep-both). Nothing else client-side.

## Testing

- Go: `internal/hunkpick`: `TestFromDiffCRLFRoundTrip` (both sides CRLF →
  `TakeIncoming` reproduces the right bytes exactly, `TakeCurrent` the
  left, `\r\n` intact incl. final newline), `TestFromDiffMixedEOLStaysLF`
  (documents dominant behavior). `internal/web`: the CRLF-422 test flips to
  a 200 + stage round-trip asserting the staged index blob still contains
  `\r\n` (read back via `svc.ShowFile(ctx, "", path)`); new mixed-EOL 422
  test; `TestOpDiscard` (tracked path → restore, untracked → clean, unknown
  → 404, conflicted → 422, empty/unsafe → 400) via the op transport, and a
  writeGuard spot-check rides the existing harness.
- Playwright: RMB "discard changes" on a modified file → red confirm →
  discard → file leaves Changes (content restored on disk); "delete
  untracked file" removes it; abort leaves the file untouched.
- Full race suite before merge (`./test.sh race`) — this track touches the
  engine-adjacent shared picker.

## Out of scope

- Whole-tree discard over the web (`Discard{All}` — needs its own scarier
  confirm surface; the palette track can add it later against this case).
- Line-ending conversion features (autocrlf awareness etc.) — gg stages
  bytes, byte-faithful round-trip is the whole contract.
- TUI mixed-EOL warnings (silent LF normalization for mixed files is
  pre-existing, now documented).
