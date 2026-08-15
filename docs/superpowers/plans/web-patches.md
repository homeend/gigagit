# Task 1 — patches: import one, export one

**Depends on task 0.** Read `README-web-parallel-tasks.md` for the ground
rules, ownership table and the browser-check recipe.

The TUI can hand a commit to someone as a `.patch` and take one back. The
browser can do neither, so a change that starts in `gg web` cannot leave it
except through git itself.

## Your files

| Server | Client |
|--------|--------|
| `internal/web/op_patch.go` (new) | `static/patch.js` (new) |
| `internal/web/patch_http.go` (new) | |
| `internal/web/oppatch_test.go` (new) | |

Register your op and routes from `init()` in your own files (task 0). Do not
edit `ophttp.go`, `server.go`, `index.html`, `commits.js` or `files.js` — add
menu rows with `registerRows("commit", …)` / `registerRows("file", …)` and a
help row with `registerHelp`.

## What already exists

- `engine.ApplyPatch{Path string, Mode ApplyPatchMode}` — `ApplyModeCommits`
  replays a `git format-patch` mailbox as real commits (`git am --3way`);
  the other mode lands the diff in the working tree. It refuses a mailbox in
  the wrong mode with a typed error, and parks its own decision when the input
  is ambiguous. The TUI reaches it from the palette's *Apply patch…*.
- `domain.Service.CommitPatch(ctx, sha) ([]byte, filename string, error)` —
  a whole commit as `git format-patch -1 --binary`, plus the filename the TUI
  suggests (`<short>.patch`). It refuses a merge commit with
  `ErrMergeCommitPatch`.
- `engine.ExportFile` / `engine.ExportToDir` — the TUI's *export to a path*
  lane, and what backs its `t` **copy to a temp dir** action: it writes a
  bookmark's or shelf entry's files into `<repo>.tmp/<subdir>`, anchored on the
  MAIN worktree even from a linked one. That action is yours too (item 4).
- The web already streams bytes back for other reads; a download is a plain
  handler with `Content-Disposition`.

## Work

1. **Export a commit as a patch.** `GET /api/commit-patch?sha=<hex>` →
   `CommitPatch`, served as an attachment (`Content-Disposition:
   attachment; filename="<short>.patch"`, `application/octet-stream`). Hex-only
   sha (`isHexSha`), the merge refusal surfaced as 422 with git's own message.
   Client: a **export as patch…** row in the commit menu that navigates to the
   URL so the browser saves it.
2. **Export one file's diff.** The TUI offers this inside the diff view for a
   commit-vs-parent file. Same handler shape with a `path` parameter, filename
   `<short>-<basename>.patch`. Row registered on the `"file"` menu, only when
   the file is being viewed at a commit.
3. **Import a patch.** `POST /api/op {op:"apply-patch", path, mode}` where the
   path is a **server-side** path the user types (the browser cannot hand the
   server a file path from an `<input type=file>`, and this server is
   loopback-only, so typing a path is the honest lane — the TUI's palette does
   the same). Prompt: *Apply patch — path to the .patch file*. Mode: default
   to the mailbox lane when the file parses as one, and let the engine's own
   decision park in the modal for the ambiguous case rather than pre-deciding
   in the client.
4. **Copy to a temp dir.** A row on a bookmark / shelf entry that writes its
   files to `<repo>.tmp/<name>` via `ExportToDir`, with an editable
   destination prefilled the way the TUI prefills it (`commit-<short>` for a
   commit entry, the entry id for a file). An existing directory prompts
   overwrite/cancel — that decision is the engine's; let it park.
5. Refusals are the feature, not an afterthought: a missing file, a mailbox
   applied in the wrong mode, and a conflicted apply must each reach the user
   as the engine's own sentence, not "500".

## Acceptance

- Go tests: export returns the patch bytes and the right filename; a merge
  commit is refused; a non-hex sha is 400; apply with a real `format-patch`
  file recreates the commit; apply with a missing path fails cleanly.
- Browser, control run first: the commit menu shows the export row and
  clicking it downloads a file whose first bytes are `From ` (assert via a
  `fetch()` inside the page rather than the download itself — the headless
  browser's download directory is not worth wiring up); the apply prompt
  accepts a path and the op line reports the commits it created.
- `./test.sh race` green. CHANGELOG bullet. `registerHelp` row.

## Notes

- A patch is bytes, so the response must not go through `writeJSON`.
- Do not invent an upload endpoint. The server is loopback-only and reads
  paths the user names; an upload lane needs its own design and is out of
  scope here.
- Keep the apply prompt's path repo-relative-or-absolute as the user typed it;
  the engine resolves it. Do not silently rewrite what they typed.
