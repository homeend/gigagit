# Review links — stage 2: replies and resolved threads (design)

Status: agreed in brainstorming 2026-10-05; builds on stage 1
(`2026-10-05-review-links-design.md`, merged `01d00129`).

## Why

The user hands agent A's review link to agent B "so one could check the
remarks from the other". Stage 1 lets B read the review. Stage 2 lets B — and
the user — answer each remark and settle it, the GitHub way: a remark is a
thread, anyone replies in it, and a thread is resolved (folded) or open.
Stage 3 (a second review answering the first) comes after.

## User rulings this builds on

1. An answer is a **reply in the remark's thread** (not a per-agent verdict
   sheet). Several answerers stack up; replies can be edited and deleted.
2. **Agents and the user** both write replies: CLI + MCP for agents, the TUI
   review diff and gg web for the user.
3. A reply may carry an **openable link** (a commit or a `gg://` link) — the
   "fixed, here is the fix" case.
4. **No verdict words.** Two thread states only, the GitHub way:
   **resolved** (stored, shared, anyone may set or clear it) and **collapsed**
   (a view state). A resolved thread shows collapsed by default and can be
   unfolded; unresolving reopens it. Collapsing a root hides its whole
   conversation.
5. Collapsed is **not stored** (today's `o` / `O`, per view).
6. Resolve applies to **every note thread** — review remarks, the user's own
   notes, range-review notes, preview / PR notes. Forge (GitHub) threads keep
   GitHub's resolved state and cannot be toggled until the forge publish
   stage of `2026-09-27-notes-forge-agent-context-strategy.md` exists.
7. Resolved is stored as a **thread-state record** keyed by the thread's root
   id (not as events in the thread).
8. Row tallies use **plain words**, never ✓/✗/✔ glyphs (font coverage and
   double-width cells break the row).

Relation to the notes ↔ forge strategy: its ruling 7 ("local replies to forge
threads") needs exactly the mechanism built here — a stored reply whose root
is a read-only, read-time id. Stage 2 builds it for `review:` roots and leaves
`forge:` roots refused; the publish stage only lifts that refusal.

## 1. Data

### 1.1 Replies to a review remark

- A reply is an ordinary stored `model.Note` with
  `ParentID = "review:<reviewID>:<n>"` (the stage-1 read-time remark id).
- `NoteReply` stops refusing `review:` parents. It refuses `forge:` parents
  as today (`ErrReadOnlyNote`). For a `review:` parent it:
  - loads the review ("no such review <id>" when it is gone, "review <id>
    has no remark <n>" when `n` is out of range);
  - stores the reply at the **review's own address** (a commit review's
    commit-level address, a working review's worktree-level address) — not
    the remark's line. The remark's path, side and range come from the join
    at read time (§1.2), so a re-saved review cannot leave a stale anchor
    behind;
  - stamps `RemarkFP` and `RemarkSummary` (below) on the reply.
- **`Note.RemarkFP`** (new, TOML `remark_fp`, empty for every other note):
  the first 16 hex of sha256 over `path \x00 side \x00 start \x00 end \x00
  summary` of the remark it answers. **`Note.RemarkSummary`** (TOML
  `remark_summary`) keeps that remark's summary so an outdated thread can
  still say what it answered.
- **A remark reply is a stored child of its review.** One model helper,
  `Note.StoredParent()`, returns `<reviewID>` for a
  `review:<reviewID>:<n>` parent and `ParentID` otherwise. Every place that
  treats `ParentID` as "my stored root" uses it:
  - the store's `dropOrphanReplies` (run on EVERY part write — without this
    the next note write anywhere in the part would delete B's replies) and
    `capOldestFirst` (the review is cap-exempt, so its remark replies are
    too);
  - `Store.Remove` ("a root takes its replies"): removing the review takes
    its remark replies — the cascade falls out of the rule;
  - domain sweeps (`NotesClearAtCommit`'s predicate, the background sweep):
    the reply carries the review's address, so the existing "commit-level /
    worktree-level notes never expire" rules already keep it, and the
    worktree-review sweep that drops an outdated working review takes its
    replies through `dropOrphanReplies`.
- **Part routing:** the reply's address is the review's, so `notes.PartOf`
  puts it in the review's part (`commits.toml` / the worktree part); a test
  pins both.
- **`Note.Link`** (new, TOML `link`, optional): a `gg://` link or a commit
  (resolved to a full sha at write time). Any note may carry one; the reply
  verbs set it.
- Editing (`NoteEdit`) and removing (`NoteRemove`) work on a reply to a
  remark like on any stored reply. The remark itself stays read-only.

### 1.2 Joining replies to remarks (read time)

`reviewDocNotes` is the one builder of remark threads (the review diff and
the working-tree diff both call it). It gains the review's stored replies and
attaches them:

1. A reply whose `ParentID` names remark `n` and whose `RemarkFP` equals
   remark `n`'s fingerprint joins remark `n`.
2. Otherwise it joins the remark of this review whose fingerprint equals its
   `RemarkFP` (the review was re-saved and the remark moved).
3. Otherwise its thread is **outdated**: listed with the review's other
   notes (`ReviewOtherNote` gains `Outdated bool` and `Replies`), headed by
   the first reply's `RemarkSummary`.

Replies sort by creation time (today's rule).

### 1.3 Resolved threads

- Each note part gains a `[[resolved]]` table:
  `root` (thread root id), `by` (author), `at` (time), `remark_fp`
  (set only for a `review:` root). One entry per root.
- Store API (`notes.Store`): `Resolve(part, entry) error` (add or replace),
  `Unresolve(part, root) error` (`ErrNotFound` when absent), and
  `LoadResolved(part) ([]Resolved, error)`; `LoadAll`'s sibling
  `LoadAllResolved()`. Same per-part lock, atomic rewrite and quarantine as
  notes.
- Part of an entry: the part of the thread's root (a stored root's
  `PartOf`; a `review:` root's review part).
- Domain: `NoteResolve(ctx, id string, resolved bool, by string)`. `id` may
  be a root or a reply (a reply resolves its thread). A `forge:` id returns
  `ErrForgeResolved` ("resolved on GitHub"). A `review:` entry keeps the
  root id it was made under; at read time it applies to the remark its
  `remark_fp` matches (the same join as §1.2), and to the outdated thread
  when none does.
- `ResolvedNote` gains `Resolved bool`, `ResolvedBy string`,
  `ResolvedAt time.Time`. Every reader that builds threads (stored notes,
  preview/pair notes, review remarks, working-review remarks) sets them; a
  forge thread sets `Resolved` from GitHub's `resolved` tag
  (`model.NoteTagResolved`).
- **Cascade:** every part write drops `[[resolved]]` entries whose root is
  gone from that part — a stored root by id, a `review:<id>:<n>` root by
  its review `<id>` (the `StoredParent` rule). So `NoteRemove` of a note or
  a review, `NotesClearAtCommit`, the cap and every sweep clear the entries
  with no extra code path.

### 1.4 Readers: a remark reply is never shown on its own

A remark reply is visible ONLY through its review's join (§1.2) and the
review's tally (§2.3). Every other reader that walks stored notes skips it:

- `NoteCounts` (✎ markers, Notes rows, file badges) — replies are already
  skipped ("a badge counts THREADS"); a test pins that a remark reply adds
  no Notes row and no marker.
- `resolveNotes` threading (stored line notes) — it never sees one, since
  the reply carries no path; a test pins it.
- `NotesOverview` / View all notes, `gg note list`, MCP `gg_notes_list`,
  the preview / pair readers, `NotesClearAtCommit`: skip notes whose
  `ParentID` is a `review:` id, except where they report the review's
  tally.

## 2. TUI

### 2.1 Thread keys (every diff that draws note threads)

- `R` on a review remark opens the existing reply popup (`noteReply`); today
  it refuses a read-only root. A `forge:` root still refuses.
- `E` and Delete on a reply to a remark work as on any reply; on the remark
  itself they refuse with the existing read-only notice.
- **`x`** toggles Resolve / Unresolve on the thread at the cursor (any
  thread). A forge thread: notice "Resolved on GitHub". New `.` menu rows
  "Resolve thread" / "Unresolve thread", a help row, and the footer hint
  where it fits (the diff footer is near full — help + menu are the
  advertised homes when it does not).
- The reply popup gains an optional **Link** field (one line; empty = none).

### 2.2 Drawing

- A resolved thread's title row carries the word `resolved` (themed muted
  label). On first sight in a view a resolved thread starts **folded**
  (`seedCollapsed`, generalised from forge-only to any resolved thread),
  once per view; `o` unfolds, `O` toggles all. Resolving a thread in place
  folds it; unresolving unfolds it (the toggle is the user's intent).
- A collapsed thread row: `author: summary (N replies) · resolved`.
- A reply with a link draws a last line `→ <link>`; enter on that line opens
  it through the `#` paste path (`linknav`), a commit through the commit
  open.

### 2.3 Rows and lists

- The review overview's remark list marks each remark: `N replies`,
  `resolved`.
- The commit Files-view review row, the Branches review sub-row, the
  working-tree review row and View all notes show
  `12 remarks · 4 resolved`; when the row lacks room, `4/12 resolved`; a
  review without remarks shows nothing extra. Plain words only.
- Every new string goes through `i18n.T` with the four bundles; plurals as
  two keys.

## 3. gg web

Same model, same strings: a Reply button on remark threads (with an optional
link field), Resolve / Unresolve in each thread header (absent on forge
threads, which show GitHub's state), resolved threads folded with a click to
unfold, a reply's link openable, the tally on the review rows and the All
notes list. Endpoints: `POST /api/notes/reply` accepts a `review:` parent
and `link`; new `POST /api/notes/resolve {id, resolved}`; the review and
notes payloads (`WireNote`) carry `resolved`, `resolvedBy`, `resolvedAt`,
`link`, and the review rows carry the tally numbers.

## 4. Agents (CLI + MCP)

- `gg review show <link|id|latest> [--json]` prints each remark's id
  (`review:<id>:<n>`), its replies indented with author and link, and
  `resolved by <who>`; the header carries the tally; outdated threads list
  at the end. `--json`: each remark gains `resolved`, `resolvedBy`,
  `replies[]` (`id`, `author`, `summary`, `rationale`, `link`, `created`);
  the document gains `outdated[]`.
- `gg note reply <id|link> --summary … [--rationale …] [--link <gg://…|sha>]`
  accepts a `review:` remark id.
- `gg note resolve <id>` / `gg note unresolve <id>` — any thread root or
  reply id; `--json` prints the thread state.
- `gg note apply` (comment-apply shape, which already has `replyTo`)
  accepts a `review:` remark id in `replyTo`, and two new optional comment
  fields: `link` and `resolve` (true / false sets the thread's state after
  the reply). One batch, the existing rollback (a failed resolve undoes the
  batch's replies).
- MCP: `gg_note_reply` (`id`, `summary`, `rationale`, `link`) and
  `gg_note_resolve` (`id`, `resolved`); `gg_review_show` carries the replies
  and states; `gg_notes_apply` takes the reply entries.
- Skills: `reviewing-with-gg` gains "Answering another agent's review"
  (`gg review show <link>` → `gg note reply` per remark → `gg note resolve`
  what is settled, or one `gg note apply` batch); `using-gg` lists the verbs.
  Bump both versions, `gg init --update` after merge.

## 5. Errors

| Case | Result |
|---|---|
| Reply / resolve on a `forge:` id | refused: "forge threads are read-only in gg" / "resolved on GitHub" |
| Reply to `review:<id>:<n>` whose review is gone | "no such review <id>" |
| `n` out of range | "review <id> has no remark <n>" |
| Edit / remove the remark itself | the existing read-only error |
| `--link` that parses as neither a `gg://` link nor a resolvable commit | refused before writing |
| Unresolve an open thread | `ErrNotFound` → CLI "thread is not resolved", exit 1 |

## 6. Stage-1 minors folded in

- `reviewHintMsg` carries the generation it was issued under; a result
  arriving after a repo switch is dropped.
- `gg link --review` refuses `--no-fingerprint` (it has no line to print).

The rest of stage 1's deferred minors stay deferred.

## 7. Testing

- `notes`: `[[resolved]]` round trip, replace, unresolve, sweep of a gone
  root, quarantine of a corrupt table; `remark_fp` / `remark_summary` /
  `link` round trip.
- `domain`: reply to a remark lands in the review's part with the remark's
  anchor; join by index, by moved fingerprint, outdated; resolve on stored,
  reply, remark and forge ids; cascade on review delete (commit and working);
  `NoteCounts` ignores remark replies; preview/pair readers set `Resolved`.
- `cli` / `mcp`: verbs, `--json` shapes, `gg note apply` reply entries with
  rollback, errors from §5.
- `tui`: `R` on a remark, `x` toggle + fold, resolved start-folded once, the
  tally on the three rows (wide and narrow), link line enter; i18n gates.
- `web`: endpoints, wire fields, a JS pin for the Resolve button.
- e2e: A reviews → B replies + resolves via `gg note apply` →
  `gg review show --json` shows replies and states.

## Out of scope

- Posting replies or resolved state to GitHub (forge strategy, publish
  stage).
- Verdict labels (agree / disagree / fixed) — ruled out.
- Stage 3 (a second review that answers the first).
- Storing the collapsed state.
- Open-file notes (`agentdocs`, memory-only agent remarks): no stored
  thread, no Resolve.
