# GitHub write-back: local PR review, send to GitHub, PR cache (design)

Status: **design for review** (brainstorm 2026-10-07). Supersedes the
publish part (§2–§3, stage 3) of
`2026-09-27-notes-forge-agent-context-strategy.md` (branch
`feat/notes-forge-agent`, never merged).

## Goal

Review a GitHub pull request inside gg: open it fast, write local notes
(yourself or an AI agent), and send a single note, a whole review group,
or a verdict to GitHub when you decide. After a note is sent GitHub is
its only home. Replies and resolving threads work from gg too.

What the user said (rulings, 2026-10-07):

1. **Write scope v1** = send comments + review verdict + thread actions
   (reply, resolve/unresolve). PR lifecycle (create, edit, ready/draft,
   merge, close) and metadata (labels, reviewers, assignees) are **v2**;
   the write seam must take them without redesign.
2. **Send a whole review** (every unsent note of one review group, one
   GitHub review with a verdict) **and send a single note**: one remark
   out of an AI review, or a standalone note from anywhere.
3. **Notes of one review group are distinguishable** by a colour detail.
4. **Every note shows its sync state**: local only / on GitHub / failed.
5. **One source of truth per note.** Once a note is on GitHub, gg deletes
   the local copy; the GitHub comment *is* the note from then on.
6. **A PR's view shows every local note on the PR's commits** (today's
   behaviour, kept), **plus notes from elsewhere** (another commit, the
   working tree) **whose noted content is identical in the PR**. This
   closes the parked "PR view gathers every local note" issue: keep it,
   and widen it.
7. **Surfaces v1**: TUI, CLI, web, MCP.
8. **AI-written content** posts with an attribution trailer; **an agent
   never posts without the user's confirmation in gg**.
9. **Approach 1**: gg writes through the `gh` CLI; drafts stay local
   (not mirrored into a GitHub pending review while you work).
10. **PR cache**: a disk cache read first, refreshed in the background,
    screens updated in place when the refresh finds changes. Entries
    expire after **8 hours** (configurable); an expired entry is read
    from GitHub first.

Assumptions (not said, chosen here): GitHub only in v1 (the seam stays
forge-neutral for GitLab/Gitea); sending is always an explicit gesture;
editing or deleting an already-sent comment from gg is out of scope (it
lives on GitHub; v2 if wanted).

Success looks like: reopening a PR seen in the last hours draws at once
with no network wait; a review drafted locally lands on GitHub as one
review with its verdict; nothing is ever shown twice, and nothing is lost
when a send fails half-way or gg crashes.

## 1. Data model, groups and marks

### 1.1 Where a note lives

| State | Stored | Mark |
|---|---|---|
| Unsent local note (user or agent) | notes store, as today | `○` local |
| Unsent remark of an AI review | inside its review note, as today | `○` local, review's group colour |
| Unsent local reply to a GitHub thread | notes store, `ParentID = "forge:<thread root id>"` | `○` local reply, under the thread |
| On GitHub (sent from gg or written there) | GitHub only; gg's PR cache | `●` GitHub + author |
| Not sendable (file not changed by the PR) | local | `○` dimmed, "not in this PR" |
| Last send failed | local | `○!` with the error; retry from the same menu |
| Send in flight / interrupted | local, with a send stamp (§3.4) | `◌` sending |

`NoteReply` today refuses a `forge:` parent (`ErrReadOnlyNote`). That
refusal is lifted for **replies only**; edit and delete of a `forge:` id
stay refused.

### 1.2 What a successful send deletes

- A plain note: deleted from wherever it lived (the PR's commit, another
  commit, the working tree).
- **A thread moves whole.** Sending a root also sends its local replies
  (as thread replies, in order) and, when the root carries a local
  resolved mark (`ThreadResolution`), resolves the GitHub thread. The
  confirm lists every item. The store's cascade then has nothing left to
  delete silently.
- One remark of an AI review: the review note gains a **moved entry**
  `{RemarkFP, ForgeThreadID, ForgeURL, At}`. The remark is hidden
  locally from then on and the review knows where it went. It is keyed
  on the remark fingerprint, so re-saving the review keeps it hidden; a
  re-RUN of the review (a new review note) is a new review and is not
  affected. Replies to that remark move with it.
- A whole review: its summary becomes the GitHub review body, its
  remarks the review's threads; the review note is deleted.
- A local reply to a GitHub thread: deleted once GitHub has it.

### 1.3 Groups and colour

- Local groups: **each AI review** is one group; the user's own
  hand-written notes in the PR form one implicit group, **"my draft
  review"**. Agent notes written with `gg note add` (not a review) join
  "my draft review" with their agent attribution.
- GitHub groups: **one per GitHub review** (comments sent together share
  a review id). The read query gains each comment's `pullRequestReview
  { id }` and each thread's `id`.
- A group's colour comes from a fixed palette of 6 theme roles
  (`note_group_1` … `note_group_6`, through `theme.roleFields`), picked
  stably from the group id (hash mod 6). Drawn as a coloured bar at the
  note box's left edge (TUI) / a left border (web).
- Sending a group records `local group id → GitHub review id` in the PR
  cache entry, and the GitHub review then uses the local group's colour,
  so a review keeps its colour across the move.

### 1.4 Which local notes show in a PR

- Every local note on any of the PR's commits (today's rule, kept; the
  `PreviewNoteSet.scope()` PR exemption stays).
- **Carried notes**: a note stored on another commit or on the working
  tree shows in the PR when its file is changed by the PR and its
  anchored lines' content hash (`ContextHash`, the existing re-anchor
  key) is found in the PR head's version of that file. It is drawn with
  its origin ("from a1b2c3d" / "from working tree") and is sendable like
  any other note. Sending deletes it from its origin.
- Carried notes are computed when the PR's notes are read (one pass over
  the candidate notes of the files the PR changes), and the result is
  cached with the PR's derived data (§2.2).

## 2. PR cache (stale-first, background refresh)

Measured from the user's operation log (babel): each `gh` call 0.5–0.7 s,
sequential; the PR head fetch 1–2 s; the git work for a PR diff 1–3 s
(merge-base, counts, file lists, commit list), with the same `rev-parse
--verify` repeated up to 6× per open. Today's cache (`forge_cache.go`) is
in memory only and evicts after 5 idle minutes, so every new session —
and every PR not opened in the last 5 minutes — is cold; the git work is
recomputed on every open.

### 2.1 On-disk entries

Per repository (keyed by the git common dir, the same key scheme as the
other per-repo state), under the state dir `prcache/<key>/`:

- `list.json`: the open-PR listing and when it was read;
- `repo.json`: the forge's base repository (slug, URL) — today a
  `gh repo view` on the first open of every session;
- `pr-<n>.json`: one entry per recently opened PR — the PR (with body),
  threads and reviews (with review ids and thread ids), head and base
  sha, `read_at`, the group-colour map (§1.3), and the derived data of
  §2.2.

Written with temp + rename under the shared `filelock` (several gg
processes may share a repo). Bounded: the **50** most recently opened
PRs; opening a 51st drops the least recently opened entry. The fetched
`refs/gg/pr/<n>` refs are unaffected (git's, as today).

### 2.2 Derived git data

Keyed by `(merge base, head sha)` inside the PR entry — the diff a PR
shows is merge-base..head, so the base branch moving on (background
fetches, pulls) is still a hit: the changed-file list (`--name-status`),
the file count, the commit list, and the carried-note matches (§1.4).
The merge base and the ahead count are computed live on every open (two
cheap calls; the merge base is the key). Derived data has its own
computed-at time and expires under the same limit. A new push or a merge
of the base into the PR is a new key and a recompute. Within one open,
each ref is resolved once (no repeated `rev-parse --verify`).

### 2.3 Expiry — `[forge] cache_hours` (default 8)

- **Entry younger than the limit**: the PR view draws from the entry at
  once (no `gh` call, no fetch, no git recompute when the local ref
  matches the cached head), then refreshes in the background (§2.4).
- **Entry older than the limit, or none**: read from GitHub first, with a
  loading line in the view (never a blank wait); the old entry is
  discarded. The local `refs/gg/pr/<n>` is still reused when its sha
  equals the fresh head.
- A PR row the user can see in the list still opens with its row data
  at once, as today.

### 2.4 Background refresh

One GraphQL query per PR replaces today's `gh pr view` + threads (+
`gh repo view` when not cached): head sha, state, body, threads (with
ids, review ids), reviews, `viewerDidAuthor`, `viewerLatestReview
{ id state }`, and the PR node id (the write calls' target).

- Nothing changed → the freshness mark clears.
- Comments changed → notes update in place; the cursor stays.
- Head moved → fetch the new head, recompute §2.2 in the background,
  then the open diff updates in place with a notice ("PR updated: 2 new
  commits"); the cursor stays on the same file and line where it still
  exists.
- Refresh failed → the cached view stays; the mark reads "offline ·
  last read 3 h ago".

The existing comment re-poll heartbeat (TUI `prCommentsTick`, web
re-poll) runs this same refresh.

### 2.5 Startup and prefetch

- The PR list draws from `list.json` at once, then refreshes in the
  background. When the last session's `gh` worked (a flag in the list
  entry), the separate detect call is skipped — the list call proves
  `gh` works; if it fails, detection runs as today.
- **Prefetch** (`[forge] prefetch`, default 5, 0 = off): after the list
  refresh, the background lane fetches and recomputes §2.2 for the 5
  most recently opened PRs whose head moved. Low priority, under
  `LimitRunner`'s cap.

### 2.6 Write-through

A successful send (§3) writes GitHub's answer (new review, threads,
comment ids) into the entry at once, so the view shows the sent state
without waiting for the next refresh.

## 3. Sending and thread actions

### 3.1 The write seam

`forge` gains an optional **`Writer`** interface beside `Provider`
(still read-only); `GH` implements both. All writes are GraphQL
mutations through `gh api graphql`; the variables JSON goes in a temp
file passed with `--input` (gg's runner has no stdin). Targets are node
ids read by §2.4 (the PR's node id lives in the base repository, so fork
PRs post to the base repository with no extra resolution).

| Writer method | GraphQL |
|---|---|
| `StartReview(prID, headSha, threads)` → reviewID, threadIDs | `addPullRequestReview` (no event = pending) with line `threads` |
| `AddThread(prID, reviewID, thread)` → threadID | `addPullRequestReviewThread` (`subjectType: FILE` for file-level) |
| `SubmitReview(prID, reviewID, verdict, body)` | `submitPullRequestReview` |
| `DeletePendingReview(reviewID)` | `deletePullRequestReview` |
| `Reply(threadID, body)` → commentID | `addPullRequestReviewThreadReply` |
| `Resolve(threadID)` / `Unresolve(threadID)` | `resolveReviewThread` / `unresolveReviewThread` |

v2 adds lifecycle and metadata methods to the same interface (create /
update / merge / close PR, labels, reviewers, assignees), all through
the same op (§3.3).

No write-permission probe: GitHub exposes no "viewer can comment" field
and a public repo accepts comments from any logged-in user, so send
actions show whenever `gh` is detected and a refusal (403, a read-only
token) surfaces as the send's error. `viewerDidAuthor` hides Approve /
Request changes on the user's own PR (GitHub refuses them).

### 3.2 Anchoring at send time

Each item is re-anchored against the PR head as it is NOW
(`PreviewHunkAnchor`); if the cached head is older than GitHub's, the
op refreshes (§2.4) first.

- The line is inside the PR's diff → a line thread (a range → `startLine`
  + `line`; the old side → `LEFT`).
- The file is changed by the PR but the line is outside its diff → a
  file-level thread whose body opens with a quote of the noted line(s)
  and its line number.
- The file is not changed by the PR → not sendable; listed in the
  confirm and skipped.

### 3.3 One operation: `engine.SendToForge`

Run through `domain.Execute` like every op. **Inputs are collected by
the frontend before the op starts** (decisions are option lists only):
the items (notes / remarks / replies / thread actions), and for a review
the group and the review body (an AI review's summary is the default;
the user may edit it in the frontend's input before sending).

The op:

1. Refreshes if needed and re-anchors (§3.2).
2. Emits one `DecisionNeeded` confirm: target (`owner/repo #n`), item
   count, the rendered bodies, the skipped items with reasons. Options:
   - a review: `comment` / `approve` / `request-changes` / `abort`
     (`approve`/`request-changes` absent on the user's own PR);
   - single items, replies, resolve: `send` / `abort`;
   - when `viewerLatestReview` is PENDING (the user started a review in
     the browser): `submit-with-pending` (gg's threads join that pending
     review and are submitted together) / `abort`.
3. Writes (§3.4), then updates the cache (§2.6), then deletes the local
   copies (§1.2).

`LockMode` is Read: the op touches no git state; the notes store has its
own lock.

### 3.4 Order of writes and crash safety

- **A review** (group, or a single note — a single note is a review of
  one thread with verdict `comment` and an empty body):
  1. `StartReview` → the review id. **Stamp** every local item with
     `{reviewID, threadID}` (`◌` sending).
  2. `AddThread` for each file-level item; stamp each.
  3. `SubmitReview`.
  4. Write-through, then delete the local items.
  - Failure in 1–3 → `DeletePendingReview`, clear the stamps; the items
    stay local with the error (`○!`). The review was never visible to
    anyone else: all or nothing.
  - Unverified (probe in plan 2, scratch repository only): whether
    `submitPullRequestReview` accepts `COMMENT` with an empty body (the
    REST twin requires one). If not, a single note's review body is a
    one-line reference to the thread ("1 comment") rather than empty.
- **Replies and resolve** are separate calls, sent one by one: each
  success deletes its local reply (or clears the local resolved mark);
  a failure marks only that item and the rest still go.
- **Crash recovery.** A stamped item found at the next refresh is
  settled from GitHub's state: its review SUBMITTED and the thread
  present → delete the local copy (it was sent); review still PENDING →
  offer "finish sending" (submit) or "discard" (delete pending) in the
  notice centre; review gone → clear the stamp. A note is therefore
  never shown twice and never lost.
- Every posted body ends with an invisible marker `<!-- gg:<note id> -->`
  so an echo is matched to its local note even if a stamp was lost.

### 3.5 Gestures

- **Send review** on a group: body (prefilled) → verdict → send.
- **Send** on one note or remark: posted at once (a one-thread review).
- **Verdict only**: Approve / Request changes / Comment with no threads.
- **Reply** to a GitHub thread: a local draft until sent; **Reply &
  send** does both in one step.
- **Resolve / Unresolve** a GitHub thread: immediate, no confirm for the
  user (GitHub owns that state).

### 3.6 AI-written content

The body ends with `— <agent> via gg`; rationale, tags and confidence
fold into a collapsed `<details>` block. The confirm shows the rendered
body, so the user sees exactly what will be posted.

### 3.7 Agents: the pending-send queue

An agent's send never posts directly. It is queued as a **pending send**
the user approves in gg.

- Home: `domain` + `filelock`, one file per repository under the state
  dir (`pending-sends/<key>.toml`): id, who asked (agent / session),
  the op input (items, verdict wish, body), created.
- The CLI (`gg pr send` run inside a gg session, i.e. `GG_INBOX` set)
  and MCP (`pr_send`, `pr_reply`, `pr_resolve`) append an entry and
  long-poll its outcome (default timeout 10 min; on timeout they return
  "still pending: <id>"; `gg pr pending wait <id>` resumes the wait).
- The TUI and the web watch the file (`filewatch`) and raise a notice
  ("Agent wants to send 4 comments to #123"); opening it runs the same
  `SendToForge` confirm. The outcome (sent / rejected / failed) is
  written back to the entry; the waiting call returns it.
- With no frontend open the entry waits until one opens, or until the
  agent cancels it (`gg pr pending cancel <id>`). Entries older than 24 h
  are dropped as expired.
- `--yes` typed by the user in a plain terminal posts directly; inside a
  gg session `--yes` is refused (`GG_INBOX` set) and the send is queued.
- Resolve/unresolve from an agent also go through the queue.

## 4. Surfaces

### 4.1 TUI (PR diff + PR popup)

- Note marks (§1.1) and group colour bars on every note box and on the
  Files tree's note badges.
- `.` menu on a note: **Send** · **Send review** (its group) · **Reply**
  · **Reply & send** · **Resolve / Unresolve** · **Delete** (local only).
- PR popup: **Send review…** (pick a group, or "my draft review" →
  body → verdict → confirm) and **Verdict only**.
- Freshness mark in the PR view title: "refreshing…", "updated",
  "offline · 3 h old".
- Pending sends and interrupted sends appear in the notice centre (`!`).
- Every string through `i18n.T` in all four bundles; help and footer
  advertise the new keys; every new op mapped in `opAffectedSources`.

### 4.2 Web

The same actions in the note and PR right-click menus, the same marks
and colours, the freshness mark, an approval overlay for pending sends.
Wire values stay allowlisted (the PR number, note ids, thread ids read
from the cache).

### 4.3 CLI

- `gg pr send <n> [--review <id> | --mine | --note <id>…] [--event
  comment|approve|request-changes] [--body <text>] [--yes]`
- `gg pr reply <n> <thread-id> <text> [--send]`
- `gg pr resolve|unresolve <n> <thread-id>`
- `gg pr pending [list | approve <id> | reject <id> | wait <id> | cancel <id>]`
- `gg note list` shows each note's sync state and group.

### 4.4 MCP

`pr_send`, `pr_reply`, `pr_resolve` carry the mutating annotations and
always queue a pending send (§3.7). Read tools report sync state and
group.

### 4.5 Config

- `[forge] cache_hours` (int, default 8, min 0 = always read from
  GitHub first).
- `[forge] prefetch` (int, default 5, 0 = off).

Both get a `settingDoc` entry and the full adding-config-entries
checklist; the 6 group-colour roles go through `theme.roleFields` and
`RoleDocs`.

## 5. Errors

| Case | Behaviour |
|---|---|
| `gh` missing / not logged in | send actions hidden, as forge reads are today |
| 403 / read-only token / archived repo | the send fails, items stay local `○!`, the error shown verbatim |
| Head moved between view and send | the op refreshes and re-anchors before the confirm |
| Item no longer anchors | listed as skipped in the confirm |
| Pending review already on GitHub | `submit-with-pending` / `abort` decision |
| Network drop mid-review | pending review deleted, all items stay local |
| Crash mid-send | settled at the next refresh (§3.4) |
| Secondary rate limit | the op stops, reports which items went (each settled by its stamp) |
| Cache file corrupt | quarantined (`.corrupt-<unix>`), treated as no entry |

## 6. Testing

Nothing ever posts to real GitHub from a test.

- `forge`: argv tests with `FakeRunner`; recorded GraphQL JSON fixtures
  for every mutation and the combined read query.
- The fake `gh` (`internal/forge/testdata/fakegh`, built by `forgetest`)
  is extended to answer the mutations from fixtures and **record** each
  write (a JSON lines log the tests assert on). No second fake.
- `domain`: `SendToForge` with a fake `Writer` — all-or-nothing review,
  pending review deleted on failure, single items failing one by one,
  re-anchoring after a head move, moved entries, thread moves whole,
  crash recovery from stamps, the echo marker match, never shown twice.
- Cache: expiry and the 50-entry bound under a frozen `clock`; stale-first
  open; in-place update when the head moved; corrupt file quarantine.
- Pending-send queue: append / approve / reject / timeout / cancel /
  expiry across two processes (the store's lock).
- e2e scenarios and TUI golden screens with the fake `gh`.
- Web: a browser probe with the slow-`gh` wrapper from the PR web work.
- Manual smoke against a scratch GitHub repository of the user's, only
  with the user's OK.

## 7. Delivery (five plans, each merged on its own)

1. **PR cache** — disk entries, expiry, derived data, the combined
   GraphQL refresh, stale-first open, in-place updates in TUI and web,
   startup and prefetch, config entries. Speeds up today's read-only use.
2. **Send core + CLI** — `forge.Writer`, `SendToForge`, stamps and crash
   recovery, moved entries, thread moves, carried notes, the
   pending-send queue, `gg pr send|reply|resolve|pending`, `gg note list`
   sync state.
3. **TUI** — marks, group colours, menus, Send review popup, verdict
   only, pending-send approval, freshness mark.
4. **Web** — the same in gg web.
5. **MCP + skills** — agent tools; `using-gg` and `reviewing-with-gg`
   updates (version bump, `gg init --update`).

On the merge of plan 2 the read-only ruling is formally reversed: the
CLAUDE.md `forge` row ("never mutates" → "writes only through
`SendToForge`"), `model.NoteSourceForge`'s comment, `ErrReadOnlyNote`'s
comment, and the 2026-09-19 forge-PRs spec's ruling list.

## 8. Out of scope (v1)

PR lifecycle and metadata (v2, §3.1); editing or deleting a comment
already on GitHub; GitLab/Gitea writers; suggested changes (GitHub's
` ```suggestion ` blocks) as a first-class gesture; mirroring local
drafts into a GitHub pending review while working.
