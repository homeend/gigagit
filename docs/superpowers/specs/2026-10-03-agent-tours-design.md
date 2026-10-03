# Agent orchestration stage 4 — briefs and reports as tours

Status: design agreed in brainstorming 2026-10-03 (§1–§2 approved in chat,
§3–§5 written here for review).
Builds on `2026-10-01-agent-spawn-design.md` (stage 2: `agent_start`, the
brief, `agent_task`), `2026-10-03-agent-wait-report-design.md` (stage 3b:
`agent_report`) and `2026-09-30-agent-overview-documents-design.md`
(overview documents: markdown with anchors, opened as a file).

## 1. Problem

An overseer writes a worker's brief and the worker writes a report back,
but the person watching sees neither as something to read with the code:
the brief is only reachable by the worker (`agent_task`), the report only as
one line on a row and a notice. gg already has the right surface — an
**overview document**, markdown whose links are anchors into a worktree
(`[parser](internal/x/parse.go:40-60)`) that you walk with tab / enter /
backspace. Stage 4 files every brief and every report as one.

Out of scope: AI-task agents reporting through the channel (the 3b
deferral) — a later, separate stage. Moving `gg session` steering off the
inbox — later.

## 2. Rulings (brainstorm 2026-10-03, do not re-ask)

1. Both the **brief and the report** become tours.
2. Opening one from a worker's row **switches to the worker's worktree**
   (anchors open the worker's own files there) and opens the tour.
3. Tours live **until gg quits**; the user closes one with X. Removing the
   worker does not close them.
4. The AI-task report bridge is a **separate, later stage**.

## 3. Behaviour

### 3.1 The brief tour

When the TUI starts a worker for an overseer (`agent_start`, the TUI's
`onAgentSpawned`), it files the brief as an overview in the **worker's
worktree** (`AddOverview(CheckoutKey(dir), dir, title, text)`):

- title `Brief — <tool> · <worktree name> (<HH:MM>)` — the start time tells
  briefs in a recycled worktree apart;
- text = the brief. A brief over the overview cap (64 KiB; a brief may be
  256 KiB) is cut at the last line end before the cap and ends with the
  line `… cut here — the worker has the whole brief (agent_task).`
- in the **background**: nothing opens, focus stays where it is (stage-2
  ruling: a spawn never takes over the screen). The spawn status line
  gains nothing unless filing failed (§3.4).

The worker still reads the same text, whole, through `agent_task`.

### 3.2 The report tour

Every `agent_report` of any session (spawned or not) files or **replaces**
that session's report tour in the session's worktree:

- one report tour per session; a newer report replaces its text and title
  (`SetOverview`), so the tour always shows the latest report;
- title `Report — <tool> · <worktree name> (<HH:MM of the session start>)`,
  `Final report — …` when the report is final;
- text = the report, read as markdown — a worker that writes
  `[changed](src/a.go:10-30)` gives a clickable "what I changed" walk;
- in the background, never opened by itself; the 3b notice stays the only
  announcement.

If the user closed the report tour (X), the next report files a fresh one.

### 3.3 Opening them

**TUI.** The worker's session sub-row (Worktrees / Branches) `.` menu gains
**Open brief** (when the session has a brief — a spawned worker) and **Open
report** (when it has reported). Either:

1. switches the TUI to the worker's worktree when it is not the current one
   (the ordinary `reRoot`, with its guards — an unreachable checkout is
   refused with the usual message);
2. files the tour again if it is not open (closed with X), from the spawn
   record's brief / the latest kept report — possible while the session is
   listed;
3. opens it in the viewer (`pushLayer(&fileViewer{…})`), as an overview
   opened by the user.

After a switch the open happens once the new worktree is up (a pending
"open this overview" on the Model, the `startAt` precedent). Both tours
also sit in that worktree's open files (F window, ctrl+\ Open files) like
any overview.

**Web.** The session row right-click menu (`sessionRowMenu`: sidebar
sub-rows) and the ctrl+\ switcher's session rows gain **Open brief** /
**Open report** under the same conditions. Either asks the server to file
the tour if needed (`POST /api/agent-tour {id, kind}` → `{overview, worktree}`),
then — when `worktree` is not the served one — switches the page there
(`doReroot`; a hosted page asks the terminal, which switches too) and opens
the overview after the reload (a one-shot `sessionStorage` key read on
boot); otherwise opens it at once. The session wire carries `has_brief`
and `has_report` (booleans) so the rows show only what exists. A standalone
`gg web` has no spawned workers and never shows the rows.

### 3.4 Limits and failures

- At the 20-overviews-per-worktree cap the spawn / report still succeeds;
  the TUI status line says `brief not filed: 20 overviews are open in <wt>`
  (or `report …`). **Open brief / Open report** then fails with the same
  sentence until the user closes an overview.
- A tour's anchors are checked like any overview's (missing file → struck
  through); the brief was written by the overseer from its own worktree,
  and paths are repo-relative, so they resolve in the worker's worktree
  unless the worker's branch lacks the file.
- Tours are memory only (like every overview): a gg restart loses them;
  the brief stays readable through `agent_task`, the reports through
  `agent_screen --reports` while the session lives.

### 3.5 Who files them

The **TUI** (it owns spawned agents; `domain` stays free of `agentdocs`):

- **brief** — in `onAgentSpawned`, after answering the agent (the reply is
  not delayed by filing);
- **report** — on every session-activity wake (`onSessionActivity`, which
  already runs on each notice), for each listed session whose latest
  report seq is above the seq its tour holds: file or replace. Reading the
  level (latest report) instead of the notices means a burst of reports
  overflowing the notice ring cannot be missed. Quiet / headless mode files
  no report tours (no activity wakes — the e2e golden screens stay stable).

The TUI keeps `agentTours map[SessionID]*tourRef` on a pointer field:
`{brief, report string (overview ids), reportSeq uint64, title parts}`.
Closing a tour (X) leaves a stale id, detected by `docs.Overview(id)`
failing → re-filed on demand. Tours of removed sessions keep their entries
until gg quits (ruling 3) but cannot be re-filed once closed.

The hosted web page shares `agentdocs.Shared()`, so it sees the tours with
no extra work; `/api/agent-tour` asks the TUI to (re)file through the
`WebHost` seam (a request into the Update loop, the web-switch precedent)
because the TUI owns the `agentTours` map.

## 4. What agents are taught (skill `using-gg.md`, v132)

- **Overseer:** write the brief as markdown; link the files and lines the
  worker should start from with repo-relative anchors
  (`[the parser](internal/x/parse.go:40-60)`) — the user reads the brief as
  a tour of the worker's worktree.
- **Worker:** write the final report as markdown with anchors to what you
  changed (`[new check](src/a.go:10-30)`), most important first; keep the
  first line a one-line summary (rows and notices show it).
- The anchor grammar line from the overview section is referenced, not
  repeated.

## 5. Testing

- **TUI** (headless model tests): a spawn reply files a brief overview in
  the worker's root with the title and text; a 70 KiB brief is cut at a
  line end with the closing line; the cap refusal leaves the spawn
  succeeded and sets the status line; a report files a tour, a second
  report replaces it (one overview, newer text, `Final report` title); a
  closed tour is re-filed by the next report; **Open brief** on a worker in
  another worktree re-roots and opens the overview after the switch; on
  the current worktree it opens at once; closed → re-filed from the
  record; menu rows appear only when there is something to open; quiet
  mode files no report tours. i18n gates for every new string.
- **Web**: `/api/agent-tour` files/reuses and returns `{overview,
  worktree}`; refuses an unknown session, a session without a brief /
  report, and a standalone server; wire `has_brief` / `has_report`; pure
  JS model for the menu rows; the browser check hosts a reporting worker in
  a second worktree and asserts **Open report** switches and shows the tour
  (visible), seen failing on the build without it.
- **Live**: the 3b shell stand-in setup (overseer + worker in `job`),
  brief with an anchor, report with an anchor; open both from the TUI and
  from the web page; screens read before every key.
