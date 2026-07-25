# Web hunk-staging backend — design

Date: 2026-07-25 · Branch: `feat/web-hunks` off `web-dev` · Status: awaiting review

## Goal

Server-side hunk staging for the web frontend: list a dirty file's change
blocks and stage a client-chosen subset. Backend only — the diff-pane UI
lands in wave 3. Reuses the TUI's existing machinery end to end; no new
engine/domain/git code.

## Background (verified)

- The TUI's `H` flow is NOT patch-based: `hunkpick.FromDiff(indexBytes,
  worktreeBytes)` (pure; `textdiff.Compare` under the hood) builds a
  `Doc` of literal runs + change `Block`s; picks set `Block.Mode`;
  `Doc.Resolved()` yields the full new file content;
  `engine.StageHunks{Path, Content}` → `git.StageBlob` (hash-object +
  `update-index --cacheinfo`). Working tree never touched; nothing
  client-supplied is ever parsed as a patch.
- Bytes come from `svc.ShowFile(ctx, "", path)` (index) and
  `svc.WorktreeFile(ctx, path)`; `internal/web` already runs engine ops
  via the same `runOp`/`domain.Execute` path `POST /api/stage` uses.
- No hunk-level UNstage exists anywhere (explicitly deferred in the TUI's
  own spec) — same scope here.
- TUI guards: untracked excluded (no index blob), binary rejected
  (`textdiff.IsBinary` either side).
- Known shared gotcha: `textdiff` strips `\r` on every line and
  `Resolved()` rejoins with `\n` only — running a CRLF file through the
  picker silently converts the WHOLE file to LF (the TUI has this today,
  contradicting its own design doc's byte-faithful claim).

## Changes (`internal/web/hunks.go`, new)

### 1. `GET /api/hunks?path=<p>`

Unstaged changes of one file. Server reads the index + worktree bytes,
runs `hunkpick.FromDiff`, and returns:

```json
{ "count": 2, "hash": "<hex>", "blocks": [
    {"index": 0, "del": ["old line"], "add": ["new line"]}, ...] }
```

- `hash` = sha256 over `indexBytes + "\x00" + worktreeBytes` — the
  freshness token for the write (the stash ref+sha pattern).
- `blocks[i].del/add` are `Block.Current`/`Block.Incoming` verbatim (the
  wave-3 UI labels blocks; today's client already derives the same
  contiguous non-same grouping for its change arrows — same
  `textdiff.Compare` alignment).
- Refusals: 404 unknown/undiffable path; 422 untracked (`no index blob —
  stage the whole file instead`), 422 binary, 422 CRLF (`file uses CRLF
  line endings; hunk staging would rewrite them — stage the whole file
  instead`; detected by `\r\n` in the worktree bytes). The CRLF refusal is
  deliberately STRICTER than the TUI, which silently normalizes — noted
  as a shared-fix candidate, out of scope here.
- Path guard: `isGitArgSafe` (the diff.go/stage.go precedent for file
  paths).

### 2. `POST /api/stage-hunks` (writeGuard)

Body: `{"path": ..., "picks": [0, 2], "hash": "<from GET /api/hunks>"}`.

Server: re-read both byte sides fresh; recompute the hash — mismatch →
409 `file changed; refresh` (closes the selection-index TOCTOU the
exploration flagged: picks are positional, so they are only valid against
the exact bytes the client saw); re-run `FromDiff`;
`doc.SetAll(TakeCurrent)`; for each pick `doc.Blocks()[i].Mode =
TakeIncoming` (an out-of-range index → 400); `content, ok :=
doc.Resolved()` (ok is always true for staging mode); dispatch
`engine.StageHunks{Path, Content}` through the existing `runOp` (the
`POST /api/stage` twin — synchronous, decision-free). Same 422 guards as
the GET. Empty `picks` → 400 (a no-op request is a client bug).

### 3. Explicitly out of scope

Hunk-level unstage (nowhere in gg); staged-vs-HEAD hunk lists; the
diff-pane UI (wave 3); line-level picks (`hunkpick` supports them — later
increment if wanted); fixing the CRLF normalization inside
hunkpick/textdiff (shared TUI+web fix, its own increment).

## Testing (Go, real-git fixtures)

- Two separated edits in one tracked file: `GET /api/hunks` → count 2 with
  the right del/add lines; `picks:[0]` stages exactly hunk 1 (`git diff
  --cached` contains edit 1, not edit 2; worktree untouched); a second
  round with the fresh hash and `picks:[0]` (the remaining block) stages
  the rest.
- Stale hash (file edited between GET and POST) → 409, index untouched.
- Guards: untracked 422, binary 422, CRLF 422, out-of-range pick 400,
  empty picks 400, missing/unsafe path 400/404.
- Hash changes when the worktree changes; stable when it doesn't.
- writeGuard on the POST: non-JSON 415, cross-origin 403.

## Parallel-track safety

Server-only new file + route registration in `server.go`'s `Handler()`
(one line — the only shared-file touch; trivially concatenation-mergeable
against the other wave-2 tracks, which don't add routes).

## Docs

CHANGELOG entry; CLAUDE.md web row gains the hunks sentence (endpoints,
freshness hash, guards incl. the deliberate CRLF refusal).
