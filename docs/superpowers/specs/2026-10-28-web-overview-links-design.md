# gg web: overview links (parity with the TUI)

Date: 2026-10-28 · Branch: `feat/web-overview-links` · Follows
`2026-10-28-overview-links-design.md` (TUI + CLI, merged 8206a6d5).

## Intent

Every overview has a `gg://` link, so one agent's analysis can be handed to
another agent. The TUI and CLI shipped that. gg web has neither the copy
actions nor the landing: it refuses a session overview link ("a session
overview link opens only in the gg terminal for now"). This round ports the
TUI surface to gg web as it is. **The TUI is the spec**; where the web has no
matching surface, the nearest web idiom carries the same action.

Non-goals: the IDE (JetBrains plugin) target — left out of consideration;
`--to ide` keeps refusing a session overview link (reworded, below). The web
tree listing stored (review / permanent) overviews — a separate row of
`docs/web-tui-parity.md`, unchanged here. The web `#` prompt's handling of a
link to *another* checkout — unchanged for every link kind (below).

## Link kinds (unchanged, for reference)

- Session overview: `gg://<abs worktree top>?overview=<id>` (no address).
- Permanent overview: `gg://<repo>@<sha>?overview=<8hex>`.
- A review's overview: the review's own `?review=<id>` link.

Links are built **server-side**, never composed in the page; every copy goes
through `links.js` `copyLink(link, desc)`, which records it in the link
history (`/api/linkhist`).

## 1. Copy actions

| Web surface | TUI source | Web action |
|---|---|---|
| ctrl+\ → Overviews & notes tab, an overview row | `treeKey` `y` + footer `[y] copy link` | key `y` copies that overview's link; on any other row `y` does nothing. Foot `FOOT_FILES` gains `y copy link` after `enter open`. The row's right-click menu (`nodeMenu`) gains **Copy gg link** on an overview row. |
| Commit files → Overviews rows, Branches `└ Overview` rows (`overviews.js` `overviewMenu`) | `branchOverviewRows` / `noteRowMenu`: Open overview · Export as HTML · **Copy gg link** · Delete overview | the same row, same place: after Export as HTML, before Delete overview. |
| Review view ≡ Overview menu | — | already has Copy gg link (no change). |
| An open overview — standalone (document mode) and the split's left column | `overviewRows` / `splitOverviewRows` row **Copy gg link** (`overviewLinkRow`) | the web has no `.` menu here, only key chips: key **`L`** + chip **`L copy link`** in all three overview foot variants (split, stored, session). *My call:* `L` is already the web's copy-link key in file views; `y` is taken (copy text). |

What `L` / the rows copy, per kind (the TUI's `overviewLinkRow` dispatch):

- a session overview → its session overview link, built from the store's
  `AttachedWorktree(root)`. A memory-only overview (not attached to a
  worktree) is refused with the TUI's words: **"this overview is not saved to
  a worktree — no link"** (op line, error tone).
- a stored permanent overview (`ov.stored.kind === "overview"`) → `GET
  /api/overview/{id}/link` (exists).
- a stored review overview (`ov.stored.kind === "review"`) → `GET
  /api/review/{id}/link` (exists).

New endpoint: `GET /api/session-overview/link?id=<id>` → `{link}` or 422 with
the refusal above (id validated with `agentdocs.ValidID`).

Copy notice: the page's usual `copyLink` notice. desc for the link history:
`overview: <title>` (review rows keep `review: <id>`).

The web help (`index.html`, "commit right-click" row) lists the overview row
menu as "open / export as HTML / delete" → add "copy gg link".

## 2. Opening a session overview link in gg web

Every entry point that lands a link on the page: the `#` prompt
(`/api/link-command`), an agent's `gg session navigate --to web` and `gg open
--web` to a live page (`/api/steer`), and `gg open --web` starting a fresh
`gg web` (its start-at).

Behaviour (the TUI's `steerNavigateSessionOverview`):

1. A navigate whose hint is `overview` and which names no file, commit or
   target is a **session overview** navigate.
2. It is the session-docs document of that id in the shown worktree: found
   by **id only** (never a path match — the TUI fix bea3f794; the web's
   `openFiles.resolve` path fallback must not be used), after a docs reload
   when it is not listed yet (it may have been written a moment ago). Found →
   it opens as `gg session files focus` opens a document (the existing
   `file_focus` landing: the page's `openViewer({id})`, the console gives
   way).
3. Not a session overview here → the permanent overview of that id (the
   existing `steerNavigateOverview`).
4. Neither → **"overview <id> no longer exists"**, and nothing else lands —
   the TUI's `onOverviewHint` guard. Today the web's 404 arm says "that
   overview no longer exists; opened its commit" and lands the command
   without its hint; for an address-less navigate that is a landing on
   nothing, so this case gets its own words and no landing.

Where it runs: server-side where the server already does the same work
(`steerFileFocus`: reload, resolve, focus, broadcast). `/api/steer` and
`/api/link-command` detect the shape and answer with the `file_focus` wire
when the document is found; on a miss they hand the page the plain overview
navigate (step 3). The start-at path lands through the page's
`steerNavigate`, so the page also recognises the shape (step 1) and asks the
server to find it — one server helper behind both. The exact split is the
plan's.

**Another worktree.** A session overview link names ITS worktree. Steered at
a gg web showing another worktree, it is worktree-bound: `steer_ask.go`
`steerWorktreeBound` gains the TUI's clause (`navigate` with the overview
hint and no commit), so the web's existing "an agent wants to show … —
Switch / Ignore" notice asks first; it never opens the shown worktree's
overview of the same id (overview ids repeat across worktrees: tours).

The notice names the place as `File`, else `Commit` — both empty here, so it
read "wants to show  in wt". For this shape it says **`overview <id>`**. The
same defect is in the TUI's `steer_switch_ask.go`; fixed there too (i18n:
"overview %s" in all four bundles).

The `#` prompt with a link to another checkout keeps today's web answer for
every link kind: "that link is in <checkout> — open gg web there". (The TUI's
`#` asks to switch; the web's `#` never has. Not this round.)

## 3. CLI routing

- `gg open --web <session overview link>`: no refusal; it takes the ordinary
  `--web` path (a live page gets the command over HTTP, else `gg web` starts
  there with it as the start-at).
- `gg open <link>` (no `--web`): the session overview special case that
  steered only a live TUI and launched a TUI beside a lone live page goes;
  the ordinary routing table applies (TUI first, else a live page, else
  launch the TUI).
- `gg session navigate --to web <link>`: allowed. The forced `dir.to = "tui"`
  goes (the ordinary preference applies). `--to ide` refuses with **"a session
  overview link opens in the gg terminal or gg web, not the IDE"** (exit 2).
  `sessionOverviewTUIOnly` is renamed to match.
- Tests that pinned the old refusals flip: `linkcommand_test.go`'s 422,
  `TestSessionNavigateSessionOverviewRefusesWebIDE` (web ok, ide refused),
  and the open/web refusal test.

## 4. Docs

`CHANGELOG.md`; `docs/web-tui-parity.md` (the overview-links row); the
overview links section of `docs/CLAUDE-details.md`; `README.md` if it states
the terminal-only rule; `internal/agentskill/using-gg.md` /
`gg-overview.md` if they state it (then bump the version, `gg init
--update`, commit the regenerated `.claude/skills`). The old plan file keeps
its history (not edited).

## 5. Verification

- Go tests per endpoint and per routing change; `go test ./internal/web`
  source pins after every `static/*.js` edit.
- A Playwright probe on a fixture with scratch `XDG_STATE_HOME` /
  `XDG_CONFIG_HOME`: copy from each surface (clipboard text = the link),
  open via `#`, via `gg session navigate --to web`, via `gg open --web`
  (fresh server), the gone case, the memory-only refusal, and the
  other-worktree notice; screenshots compared with a TUI capture
  (`driving-tui-headless`) of the same surfaces.
- `./test.sh race` → "all green"; a final whole-branch review.

## Left to the builder

The server/page split in §2, endpoint shapes beyond the one named, test
names, and the probe fixture.
