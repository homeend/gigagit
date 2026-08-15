# Closing the web ↔ TUI gap in parallel

Seven task files: **task 0 first, alone**, then six that can run at the same
time in separate agents without touching each other's files.

| # | File | Area |
|---|------|------|
| 0 | `web-task-0-registries.md` | Prep: op / route / menu-row / help registries so the other six never share a file. **Must land before any other task starts.** |
| 1 | `web-patches.md` | Import a patch, export a commit or a file diff as one |
| 2 | `web-worktrees-locks.md` | Rename & move a worktree, keep-modes, stranded git-lock cleanup |
| 3 | `web-notifications-push.md` | Notification centre, fetch-refspec repair, branch-tip tag push |
| 4 | `web-search.md` | Deep commit search: eager paging, server-side feed filter, fuzzy file finder, solo-from-a-commit |
| 5 | `web-conflicts-stash-compare.md` | Mark-all-resolved, whole-file conflict picks, stash-create checklist, compare against a bookmark / shelf entry |
| 6 | `web-config-agent-ai.md` | Git-config explorer, agent-skill setup, AI commit message, commit amend |

The gap these close is recorded in `docs/web-tui-parity.md`, which is the
source of truth for what is still missing. **Do not edit that file** — report
what you closed, and it is updated once your work is merged. That keeps it out
of every merge.

## Coverage — every remaining gap has exactly one owner

| Gap (from `docs/web-tui-parity.md`) | Task |
|---|---|
| `ApplyPatch` — import a patch | 1 |
| `ExportFile` / `ExportToDir` — export a patch, copy to a temp dir | 1 |
| `MoveWorktree` — rename / move a worktree | 2 |
| `CreateWorktree` keep-modes | 2 |
| `RemoveGitLocks` — stranded lock cleanup | 2 |
| `AddFetchMappings` — fetch-refspec repair | 3 |
| `PushTags` — branch-tip tag prompt | 3 |
| Notification centre + related-option prompts | 3 |
| Server-side feed filter, eager search, fuzzy finder | 4 |
| Solo from a commit | 4 |
| Squash marked commits, compare the marked pair | 4 |
| `MarkAllResolved`, `ResolveConflict` whole-file picks | 5 |
| Stash a selection (`Stash{Paths}`) | 5 |
| Compare against a bookmark / shelf entry | 5 |
| Git config explorer | 6 |
| Agent-skill setup | 6 |
| `GenerateMessage` — AI commit message | 6 |
| Commit amend | 6 |

Nothing else in the parity doc is open. Sparse-checkout is missing from both
frontends and is not part of this effort.

## File ownership

After task 0, each task owns its files outright. Nobody else may edit them.

| Task | Owns (server) | Owns (client) |
|------|---------------|---------------|
| 1 | `op_patch.go`, `patch_http.go` | `static/patch.js` |
| 2 | `op_worktree_extra.go`, `locks.go` | `static/locks.js` |
| 3 | `notifications.go`, `op_push_extra.go` | `static/notifications.js` |
| 4 | `search.go`, `commits.go`, `solo.go`, `commitedit.go` | `static/commits.js`, `static/search.js` |
| 5 | `conflict.go`, `op_conflict_extra.go`, `stashes.go`, `compare.go` | `static/files.js`, `static/conflicts.js` |
| 6 | `gitconfig.go`, `agentsetup.go`, `op_ai.go` | `static/gitconfig.js`, `static/agentsetup.js` |

Shared, and therefore **off limits** to tasks 1–6: `ophttp.go`, `server.go`,
`index.html`, `style.css`, `layers.js`, `core.js`, `ops.js`, `sidebar.js`.
Task 0 exists so you do not need them:

- a new operation registers itself: `RegisterOp("my-op", buildMyOp)` in your
  own file — no `case` in `ophttp.go`
- a new endpoint registers itself: `RegisterRoutes(func(mux *http.ServeMux){…})`
- a menu row registers itself: `registerRows("commit", (c) => [...])` — no edit
  to `commits.js` / `files.js` / `sidebar.js`
- a help row registers itself: `registerHelp("my feature", "…")` — no edit to
  `index.html`
- a new overlay mounts itself: `mountOverlay("my-view")` — no markup to add

`CHANGELOG.md` is the one file everyone still touches (one bullet at the top of
`## [Unreleased]`). If two tasks land together the conflict is adjacent bullets:
keep both.

## Ground rules

- **Branch from `web-dev`, merge into `web-dev`** — never `main`. Web work has
  its own trunk in this repo.
- Work in a worktree: `git worktree add -b feat/web-<area> .claude/worktrees/<area> web-dev`.
  Never build up work in the shared checkout.
- **Ask the human before merging.** Do not merge your own branch.
- The Bash shell's working directory silently resets to the main checkout
  between calls. Use `git -C <abs-worktree-path>` for every git command and
  absolute paths inside heredoc scripts, or your commit lands on `main`.
- Prefer the Edit tool over `sed`/`str.replace` scripts: a replacement that
  does not match writes the file back unchanged and reports nothing. That has
  silently dropped whole features here.

## Done means all four

1. **Go tests** in `internal/web` — endpoint shape, guards, and refusals. Real
   git in a `t.TempDir()` (see `newRepoDir`), `t.Setenv("XDG_STATE_HOME", …)`
   whenever gg's own stores are involved.
2. **A browser run, control first** — see below. Paste both outputs into your
   report: the failing control and the passing fixed run.
3. **`./test.sh race`** on your branch, green, before you ask for a merge.
4. **`CHANGELOG.md` + in-app help** — a bullet under `## [Unreleased]`, and a
   help row via `registerHelp` for anything a user can see or click.

## The browser check (mandatory)

Go tests do not prove a browser surface works. Everything genuinely broken here
this year — a cursor anchored one row off, two menu rows silently missing, a
menu cut off at the viewport floor, an ES-module export error — was invisible
to Go and obvious in the browser after one run.

The `playwright` npm package does not resolve on this machine, but its Chromium
binary is on disk and node 22 speaks CDP with no dependencies.

```bash
# 1. a throwaway repo — never point the server at a real one
R=/tmp/probe-repo; rm -rf $R; mkdir -p $R; cd $R
git init -q -b main .; git config user.email t@t; git config user.name t
for i in 1 2 3; do echo "l$i" >> a.txt; git add a.txt; git commit -qm "c$i"; done

# 2. the build UNDER TEST, and an isolated state dir when the stores are involved
go build -o /tmp/gg-probe ./cmd/gg          # from YOUR worktree
XDG_STATE_HOME=/tmp/probe-state /tmp/gg-probe web -addr 127.0.0.1:8990   # background it

# 3. headless Chromium with a debugging port (background it — a trailing & in a
#    foreground call dies with the shell and CDP never binds)
~/.cache/ms-playwright/chromium-*/chrome-linux64/chrome --headless=new \
  --disable-gpu --no-sandbox --disable-dev-shm-usage \
  --remote-debugging-port=9222 --user-data-dir=/tmp/probe-profile about:blank

# 4. drive it from a node SCRIPT FILE (inline `node -e` with a URL is blocked)
node probe.mjs 9222 http://127.0.0.1:8990/
```

**`probe-template.mjs` in this directory is that script** — copy it next to
your work and fill in the steps. It already has the connect/evaluate plumbing,
console-error collection, and helpers for the things you will actually do:
open a context menu on a row, click a row by regex, fill the shared prompt,
answer a parked decision modal, read the op status line, and measure whether
something is really visible.

**Rules that matter more than the mechanics:**

- **Run the probe against the UNFIXED build first and show it failing.** A probe
  that passes before your change is testing nothing.
- **Assert visibility and geometry, not class names.** `offsetHeight === 0`,
  `getBoundingClientRect().bottom <= innerHeight`. A "collapsed" class can be
  present while the element is plainly visible — that exact bug shipped here.
- **Collect console errors and exceptions** (`Runtime.exceptionThrown`,
  `Runtime.consoleAPICalled`) and assert the list is empty. This is how an
  ES-module cycle or a bad export shows up; `node --check` cannot see it.
- **`gg web` binds a random port unless you pass `-addr`**, so browser storage
  is a different origin every run. Nothing may depend on `localStorage`
  surviving a restart — server-side `/api/uistate` is where preferences live.
- Rebuild → restart the server → hard-reload the page. A stale binary serves
  the old SPA and you will debug a bug you already fixed.
