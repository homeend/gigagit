# gg MCP server — stage 1 (read + compare + export) — design

**Date:** 2026-07-19 · **Status:** approved direction, spec for planning
**Owner decision trail:** MCP scope deliberately narrowed (user, 2026-07-18): the MCP
server exposes gg's **non-git value** — live TUI situational awareness + bookmarks/shelves +
gg-specific compare/export — **not** normal git operations (commit/pull/push/merge/rebase
stay with the CLI/TUI). Stage 1 is the **safe surface only**: reads, compares, and
export-to-a-directory. Repo-mutating actions (cherry-pick, restore, apply) are stage 2.

## Goal

An AI agent connected over MCP can (a) see what the gg TUI is currently presenting to the
user — selected/marked commits, selected branch/tag/worktree, the open diff/compare view
and which file+version it shows, the highlighted bookmark/shelf entry — and (b) list,
inspect, read, compare, and export bookmarks and shelf entries, so the user can say
"look at what I've selected and compare it / put it in a directory" and the agent can.

## Non-goals (stage 1)

- No mutation of the repository: no cherry-pick, restore, apply, stage, commit, branch,
  or any ref/tree write. The only writes are (1) the TUI's own snapshot file under XDG
  state and (2) `gg_export` writing into a caller-named directory outside the repo flow.
- No live/streaming state: `gg_ui_state` returns a point-in-time snapshot (user decision —
  "it doesn't need to be live, just a current snapshot").
- No driving of the TUI (no "select this commit for me").
- No normal-git read duplication (log/diff/status): agents already have the `gg` CLI
  (using-gg skill) for those.

## Architecture

Two independent halves joined by one file:

```
gg TUI (running)                       agent (Claude Code / Desktop)
   │  debounced, atomic write             │ MCP over stdio
   ▼                                      ▼
$XDG_STATE_HOME/gg/sessions/          gg mcp  (new subcommand)
  <EncodeRepoKey(commonDir)>/            │
    ui-state.json          ◀── reads ────┤
                                         └── all other tools → internal/domain
```

- **`internal/mcp`** (new package): a domain-only frontend like `internal/cli`/`internal/tui`.
  Imports `internal/domain`, `internal/model`, `internal/config` (for the snapshot path
  helpers) and the MCP SDK. **Never imports `internal/git`, `internal/tui`, or
  `internal/cli`** — enforced by a new archtest rule.
- **`gg mcp`** (new subcommand): routed in `cmd/gg/main.go` beside `shell-init`/`inspect`
  (it is a long-running stdio server, not a one-shot CLI verb, so it does NOT go through
  `internal/cli.runOne` and is NOT driveable by `gg batch`). It resolves the repo from the
  process cwd (`domain.Open(".")`, the CLI convention), then serves MCP over stdio.
- **Snapshot writer** (TUI side): a small `internal/tui` addition that serializes the
  agent-relevant slice of `Model` to JSON and writes it debounced + atomically.
- **SDK:** the official `github.com/modelcontextprotocol/go-sdk` (the one new dependency;
  exact version pinned at plan time). Stdio transport for serving; its in-memory transport
  for tests.

Bookmark/shelf/compare/export tools work with **no TUI running** (the stores are persisted
under XDG keyed by git common dir; `domain` reaches them directly). Only `gg_ui_state`
depends on a live TUI having written a snapshot.

## The session snapshot

### Writer (TUI)

- Path: `$XDG_STATE_HOME/gg/sessions/<EncodeRepoKey(commonDir)>/ui-state.json`
  (Windows: the same `%LocalAppData%` root `internal/repos` uses). Keyed by **git common
  dir** so all worktrees of a repo share one session identity; the payload names the
  specific worktree the TUI is in.
- Written from the TUI's **existing perpetual 1-second heartbeat tick** (the busy-line
  heartbeat started in `Init`), via the standard atomic pattern (write temp file, rename).
  "State-affecting" is defined by the payload, not the input: each heartbeat serializes
  the snapshot (timestamp excluded) and writes only when the serialized bytes differ from
  the last written payload — no field-by-field dirty tracking, no write when nothing the
  agent can see changed. Snapshot latency is therefore ≤1 s, which satisfies the
  point-in-time contract (user decision: "doesn't need to be live"); serialization is
  cheap (a few hundred bytes of IDs and paths), so a once-per-second serialize-compare
  costs nothing and needs no new timer.
- **Removed best-effort on clean TUI exit**, and on `reRoot` the old repo's file is removed
  and the new repo's file written — the file doubles as session presence. A crashed TUI
  leaves a stale file; the payload's `written_at` + `pid` let the agent judge freshness
  (stage 1 does not probe pid liveness — documented limitation).
- **Multiple TUIs on one repo:** last-writer-wins (documented v1 simplification).
- **English/protocol values only** — hashes, repo-relative paths, engine op names, config
  states. Never translated display strings (the snapshot is an agent-facing surface, same
  rule as CLI/engine prose in the i18n arc).
- Always on; no config knob in stage 1 (same standing as the `internal/repos` MRU file —
  local-machine state under XDG).

### Schema (version 1 — the agent-facing contract)

All fields lower_snake_case. Optional object fields are omitted (not null) when the
surface is closed/empty; `session` itself is `null` in `gg_ui_state`'s reply when no
snapshot file exists.

```jsonc
{
  "version": 1,
  "pid": 12345,
  "written_at": "2026-07-19T10:00:00Z",       // RFC 3339 UTC
  "repo": {
    "common_dir": "/abs/path/.git",
    "worktree": "/abs/path",                   // the worktree this TUI runs in
    "branch": "main",                          // current branch ("" when detached)
    "head": "<short sha>"                    // git's disambiguated short hash — valid input to any rev-taking tool
  },
  "focus": {
    "panel": "commits",                        // branches|worktrees|remotes|files|staged|commits|tags|reflog
    "left_tab": "branches",                    // active left-column tab
    "bottom_tab": "commits"                    // active bottom tab when split
  },
  "cursor": {                                  // per-panel cursor VALUE (not index); field absent when panel empty
    "commit": { "hash": "<sha>", "subject": "…" },
    "branch": "feature/x",
    "remote_branch": "origin/feature/x",
    "tag": "v1.2",
    "worktree": "/abs/path/wt",
    "file": "internal/tui/model.go"            // focused file in the files panel/view
  },
  "marked_commits": ["<sha>", "<sha>"],        // ◉ compare-marks, feed order
  "marked_files": ["path/a.go"],               // space-marked files
  "files_view": {                              // present only while a files/diff surface is open
    "mode": "compare",                         // changed|full_tree|compare|stash|shelf
    "title": "…",
    "commit": "<sha>",                          // the shown commit, when mode is commit-backed
    "left":  { "kind": "commit", "hash": "<sha>" },   // compare endpoints (kind: worktree|index|commit)
    "right": { "kind": "worktree" },
    "shelf_id": "<entry-id>",                  // when mode == "shelf"
    "selected_file": "path/b.go",
    "diff_open": true                          // a diff for selected_file is on screen
  },
  "switcher": {                                // present only while the g/G switcher popup is open
    "kind": "bookmark",                        // bookmark|shelf
    "selected_id": "<id>",
    "display": "…"                             // the entry's FileAddress.Display() (protocol-safe)
  },
  "filter": {                                  // present only when a /-filter or @-highlight is active
    "panel": "commits",
    "query": "fix",
    "highlight": "wip"
  },
  "commit_scope": ["main", "feature/x"],       // Commits-panel branch scope, when narrowed
  "conflict": {                                // present when a merge/rebase/… is paused
    "op": "merge",                             // engine op name (English protocol)
    "conflicted_files": ["path/c.go"]
  },
  "running_op": "SmartPull",                   // present while an operation runs
  "status": "…"                                // last status line (may be localized — display-only, agents should not parse)
}
```

The `status` field is the one deliberate exception to the English-only rule: it is a
verbatim copy of the visible status line for *display* to the human, explicitly documented
as non-parseable. Everything an agent should branch on is a dedicated protocol field.

Schema evolution: additive fields only within `version: 1`; a breaking change bumps
`version` and `gg_ui_state` reports both raw and, if unknown, a clear
"snapshot version newer than this gg" error.

## MCP server

### Registration & lifecycle

- `gg mcp` serves on stdio. Users register it per-project, e.g.
  `claude mcp add gg -- gg mcp` from the repo directory (README documents this).
- Repo resolution: process cwd at startup, like the CLI. Not in a git repo → the server
  starts and every tool returns a clear "not a git repository" error (a server that dies
  at startup shows up as an opaque client-side failure).
- Read tools run under the same `domain` read reservations as any frontend;
  `gg_export` runs `engine.ExportToDir` via `domain.Execute` like every op.
- **Decider policy:** a static option-list decider (the CLI-policy pattern). Stage 1's
  only reachable decision is `ExportToDir`'s overwrite/cancel fork, answered from the
  tool's `overwrite` parameter (`false` → "cancel" → the tool returns
  "directory exists — pass overwrite:true"). Any other decision id → error (fail loud).

### Tools (11)

Every tool's JSON reply includes `"repo": {"common_dir", "worktree"}` so an agent juggling
projects can sanity-check which repo answered.

**1. `gg_ui_state`** — no params.
Reads the snapshot file for the server's repo. Reply: `{"session": <schema above>}` or
`{"session": null, "hint": "no gg TUI session snapshot for this repository"}`.
Backing: file read + `config.EncodeRepoKey`; no git access.

**2. `gg_bookmarks_list`** — params: `skip?`, `limit?` (default 100).
Reply rows: `{id, display, state, worktree, branch, commit, shelf_id, path, is_commit}`
(the `model.Bookmark` address fields + `FileAddress.Display()`).
Backing: `domain.BookmarkList`.

**3. `gg_bookmark_get`** — params: `id` (required).
Reply: the same row shape, full fidelity. Backing: `domain.BookmarkGet`.

**4. `gg_bookmark_read`** — params: `id`, `max_bytes?` (default 262144).
Reply: `{text, truncated, size}` for UTF-8 content; `{binary: true, size}` (no text) when
the bytes contain NUL or are not valid UTF-8 — the reply hints `gg_export` for binaries.
Refused with a clear error for a path-less commit bookmark (`IsCommit()` — nothing to
read; the hint names `gg_export`). Backing: `domain.BookmarkGet` + `domain.BookmarkBytes`.

**5. `gg_shelf_buckets`** — no params. Reply: `[{name, entries}]`.
Backing: `domain.ShelfBuckets`.

**6. `gg_shelf_list`** — params: `bucket?` (default "default"), `skip?`, `limit?` (default 100).
Reply rows: `{id, kind, origin_display, label, path, commit, size, has_patch, created_at}`
(`kind`: `"file"` | `"commit"`; `origin_display` = the entry's `FileAddress.Display()`).
Backing: `domain.ShelfList`.

**7. `gg_shelf_commit_files`** — params: `id` (required).
Members of a shelved commit: `[{path, status, old_path}]` (`model.CommitFile` — status
letter A/M/D/R/C/T plus the pre-rename path when applicable; no numstat counts, the
compare read-model does not carry them).
Clear error when the entry is a file entry. Backing: `domain.ShelfCommitFiles`.

**8. `gg_shelf_read`** — params: `id`, `member?`, `max_bytes?` (default 262144).
A file entry's blob, or one member of a commit entry (`member` = repo-relative path inside
the tar). Same text/binary/truncation contract as `gg_bookmark_read`. A commit entry
without `member` is refused with the member list hint (`gg_shelf_commit_files`).
Backing: `domain.ResolveBytes(FileRef{Source: SourceShelf, Locator: id, Path: member})`
(member-aware per the shelf design); file entries via `domain.ShelfBlob`.

**9. `gg_compare_trees`** — params: `left`, `right`, each
`{"kind": "worktree"|"index"|"commit", "rev"?}` (`rev` required for `"commit"`; any
rev-parseable name — resolved to a sha before the compare, mirroring the TUI's
resolve-to-tip-hash rule so the diff cache is never keyed on a mutable name).
Reply: `{left_display, right_display, files: [{path, status, old_path}]}`.
Backing: rev resolution via the existing commit-lookup query + `domain.CompareFiles`
(`model.Endpoint` pair).

**10. `gg_compare_file`** — params: `left`, `right`, each one of
`{"source": "unstaged"|"staged"|"commit"|"shelf", "locator"?, "path"}` (a `model.FileRef`:
`locator` = rev for commit / entry id for shelf) **or** `{"source": "bookmark", "id"}`.
Reply: `{left_display, right_display, identical, unified_diff}` — a real unified diff whose
`---`/`+++` header labels are the two sides' display names; binary content on either side
degrades to `{binary: true}` with sizes. Path-less commit bookmarks refused (as in tool 4).
Backing: `domain.ResolveBytes`/`domain.BookmarkBytes` for content; the diff itself is
produced by materializing both sides to temp files (the `ConflictFileVersions` temp-file
precedent) and running a new thin git verb `DiffNoIndex(ctx, a, b)`
(`git diff --no-index -- <a> <b>`; exit 1 = "differs", not an error), then rewriting the
two header lines to the display labels. One git invocation, canonical output, git's own
binary detection.

**11. `gg_export`** — params: `source` (`{"bookmark": id}` or `{"shelf": id}`), `dir?`,
`overwrite?` (default false).
Copies the bookmark / shelf entry (file or whole shelved commit) into a local directory.
`dir` defaults to `domain.TempExportBase() + the per-type default subdir` (the TUI `[t]`
behavior); an explicit `dir` is used as-is. Reply: `{dir, files: [paths], count}`.
Existing-dir handling per the decider policy above.
Backing: `domain.ExportBookmark`/`domain.ExportShelfEntry` → `domain.Execute(engine.ExportToDir)`.

### Error contract

Every failure is an MCP tool error with a one-line English message naming the fix where
one exists ("bookmark not found: <id>", "directory exists — pass overwrite:true",
"entry is a file entry — gg_shelf_read without member"). No stack traces, no git stderr
dumps (the underlying error's text is included after the summary line when it adds
information). Failures are recorded through the existing `observ.NoteFailure` seam like
every domain-surfaced error.

## Testing

- **Snapshot writer:** unit tests in `internal/tui` — serialize a seeded `Model`, assert
  the JSON contract (golden-ish field assertions, not byte-golden); debounce collapses N
  updates to one write; atomic write leaves no partial file; clean-exit and `reRoot`
  removal. Round-trip read via the `internal/mcp` reader.
- **MCP tools:** tests in `internal/mcp` against a real temp repo (`newTestRepo` pattern)
  with seeded bookmarks + shelf entries (file and commit kinds), driven through the SDK's
  in-memory client↔server transport — asserting tool replies, the text/binary/truncation
  contract, compare diffs, export file layout, and every documented error message.
- **Verb:** `DiffNoIndex` argv + exit-1-is-not-an-error via `FakeRunner`, plus one real-git
  test.
- **Archtest:** `internal/mcp` may import `domain`/`model`/`config` + the SDK; must not
  import `internal/git`/`internal/tui`/`internal/cli`. The TUI snapshot writer stays
  inside `internal/tui` (no new TUI dependency edges).
- Full `./test.sh race` green before merge.

## Documentation (on completion)

- `CHANGELOG.md`: the stage-1 surface.
- `CLAUDE.md`: package-map row for `internal/mcp`; snapshot-writer note in the `tui` row;
  the M3 roadmap line updated to the narrowed MCP scope.
- `README.md`: `gg mcp` — what it exposes, how to register (`claude mcp add gg -- gg mcp`),
  the snapshot file's location and lifecycle.
- `internal/agentskill/using-gg.md`: **unchanged** — the skill teaches the CLI verbs;
  agents do not launch MCP servers themselves. (Revisit if a future stage adds
  agent-relevant CLI entry points.)

## Stage 2 (deferred, explicitly out of scope here)

Repo-mutating tools on the proven base: cherry-pick a shelved/bookmarked commit (live or
patch replay, the `gg shelf cherry-pick` two-lane logic), restore a shelf file into the
working tree, apply a patch — each MCP-annotated destructive, conflict handling as an
`on_conflict: keep|abort` parameter (the CLI flag-policy pattern), and possibly a
consent/approval story. Also deferred: pid-liveness probing for stale snapshots,
multi-session awareness, any TUI-driving capability.
