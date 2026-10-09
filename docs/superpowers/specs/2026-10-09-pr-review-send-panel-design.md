# PR review: a stored overview, PR Reviews rows, one send panel — design

Date: 2026-10-09. Status: design approved in brainstorm (rulings R1–R12 below);
this document is the spec for the user's review. Three plans follow (§9).

## 0. Vocabulary

An **overview** is one document kind in gg: markdown whose links are anchors
into the code (the gg-overview skill, `agentdocs.ParseOverview`). Today it is
temporary — added with `gg session overview`, kept in the running gg's
memory. This spec lets a review **store** one: the same document, saved in
the review's `"overview"` field, its anchors pointing at the reviewed
commits instead of the files on disk. Temporary or stored, it is the same
thing, rendered by the same code (R11).

The review's own text — the verdict and findings that GitHub gets as the
review body — is its **summary** (document key `"summary"`). Two renames keep
the words apart in code and on screen:

- `ReviewDoc.Overview` (the Go name of `"summary"`) becomes
  `ReviewDoc.Summary`; `ReviewShow.Overview`/`"overview"` becomes
  `Summary`/`"summary"`; the web wire's `overviewMd` becomes `summaryMd`.
  Mechanical, ten call sites.
- The review view's first row "≡ Overview" shows the summary, not an
  overview: it becomes **"≡ Summary"** in the TUI (`reviewTreeLines`, the
  popup title stays `Review: <label>`) and the web pane (`.review-ov` bar).
  A review that has a stored overview gets a second row, **"≡ Overview"**,
  under it (R12).

The new field is then `ReviewDoc.Overview` (JSON `"overview"`),
`ReviewShow.Overview`/`"overview"` and `overviewMd` on the web wire: the
one word, one meaning.

## 1. What is built

1. **An overview stored with a review** (§2, §4). `/gg-review` and
   `/gg-cross-review` write one only when the user asked for it (R1). It is
   an optional field of the review document, opened from its own
   "≡ Overview" row beside the "≡ Summary" row, in the popup a temporary
   overview uses, with tab/enter/backspace navigation (R5, R12). It never
   leaves the machine.
2. **A PR's stored reviews are visible from the PR** (§3): a Reviews block in
   the PR's file list, TUI and web, exactly as a saved preview draws its
   reviews; enter opens the existing review view (R4).
3. **One send panel** (§5): every unsent local comment of the PR, grouped by
   source, nothing ticked on open; the user ticks any mix, picks a body, and
   one GitHub review goes out through the existing confirm (R6, R7). It
   replaces "Send review…" everywhere; "Verdict…" stays. A note's own menu
   keeps only the one-note send, labelled "Send as GitHub comment" (R9, R10,
   the label is already merged).

The field is for every stored review — branch, commit, preview, working
changes, PR — not only PR reviews; the PR case is where the send panel sits
beside it.

Not built: attaching an overview to an already saved review (v1 has no edit
path; re-run the review), sending an overview to GitHub, turning a temporary
overview into a stored one (the gg-overview skill keeps saying "store a
review instead"), MCP tools beyond `review show` carrying it.

## 2. The stored overview

### 2.1 Storage

The review document (notebatch `rawReview`) gains an optional top-level
string `"overview"` beside `"summary"`. Absent or blank = none. Older
documents lack it: no migration, no format bump (`version` stays 1).

- `ReviewDoc.Overview string` (after the `Summary` rename of §0);
  `canonDoc.Overview string json:"overview,omitempty"` so `Canonical()`
  keeps it (a re-save through `Canonical` must not drop it — round-trip
  test).
- Limits, checked by `ParseReview`: an overview over 64 KiB
  (`agentdocs.MaxOverviewBytes`) fails the parse with an `ErrNotReviewDoc`
  error (`overview exceeds 64 KiB`), so `gg review save` refuses with a clear
  message instead of storing a truncated one. Links past the 100th anchor
  (`agentdocs.MaxAnchors`) stay plain text, as in a temporary overview.

### 2.2 Anchors (R3)

The same grammar through `agentdocs.ParseOverview` (no second parser):
`path`, `path:N`, `path:N-M`, paths repo-relative in slash form. Resolution
is against the **reviewed tip**:

- `path` must be one of the review's files (the diff the review read: base →
  tip; for a working review, the working set). `N`/`N-M` are new-side line
  numbers at the tip.
- `note:t<n>` and any other destination are parsed by the grammar but have no
  meaning in a stored review: drawn plain, not focusable.
- An anchor whose path is not in the review's file set, or whose range lies
  outside the file, is drawn plain.

`gg review save` reports every plain-drawn anchor as `unresolved: <dest>` on
stderr (text mode) / `"unresolved": ["<dest>", …]` (JSON), exit 0: the
agent sees it and may re-save with the text fixed. The resolution helper is
one domain function (`ReviewOverview(ctx, id) (OverviewDoc, error)`: the
markdown tree, the anchors, each with `OK bool` and the review file it names)
used by the save report, the TUI and the web alike.

### 2.3 Reading it elsewhere

- `gg review show <id|link|latest>` prints the overview after the summary
  under an `Overview` line; `--json` and the MCP stored-review show carry it
  as `"overview"` (and the summary as `"summary"`, §0).
- The review view's "≡ Overview" row opens it (§4).
- A send never includes it: `reviewSendBody`, `ReviewBodyText`, and the
  one-note bodies read the summary and remarks only (test: a review with an
  overview sends the same body as without).

### 2.4 Who writes it (R1)

Only the skills, only on request:

- `gg-review` and `gg-cross-review` gain a step **"Overview (when asked)"**
  right before "Store it": when the user's request says "overview", "tour",
  "walk me through" (or `/gg-review <link> overview …`), write the
  `"overview"` field following the gg-overview recipe — first line the result
  in one sentence, then anchored points in explaining order, one `path:N` /
  `path:N-M` per place, no `note:` anchors, ≤ 100 anchors, last the open
  questions. The summary stays the review: the verdict and the findings go
  there, the overview only walks. Otherwise the field is omitted.
- `gg-overview` gets one line: an overview is temporary when added with
  `gg session overview`, and kept when written into a review's `"overview"`
  field (reviewing-with-gg).
- `reviewing-with-gg` documents the field in "Review document" (optional,
  local only, the anchor grammar, limits, that `gg review save` lists
  unresolved anchors).

## 3. A PR's reviews from the PR view (R4)

### 3.1 Domain

`PreviewReviews(ctx, set)` stops returning nothing for a PR set. For a PR set
it classifies `prReviewHeads(ctx, set)` the way `classifyScopeReviews` does
for a preview (preview-reviews R2): **current** when the review's tip is the
PR head; **older** when the head moved on but both reviewed commits still
exist (`Older: true`); **gone** (a reviewed commit no longer in the
repository) omitted — View all notes still lists it. Matching stays by PR
number (`PRScopeNumber`). The doc comment on `PreviewReviews` and
`TestPreviewReviewsStayOffAPR` (becomes `TestPreviewReviewsOfAPR`) change.
`PRSendGroups`/remarks-in-the-diff are untouched.

### 3.2 TUI

The PR's file list (the preview the PR opens, `m.previewOpen.prNumber`) draws
the Reviews heading and one row per review, exactly as a saved preview does
(same row text, `(older)` suffix, ◆ counts, colour slot). Enter opens the
review view; esc from it returns to the PR file list (a window opened from a
view returns to it: the preview-review handoff is reused, not re-implemented).
The row menu is Open + Delete only (standing ruling). Delete removes the
review and its remarks from the PR diff as today.

### 3.3 Web

The PR view's file list gets the Reviews block `previews.js` draws for a
preview (`.brev` rows from `/api/pr/open`'s new `reviews` array, same wire
shape as a preview's). Click opens `openReview(id, {kind: "pr", pr: n})`;
Back/esc returns to the PR view. The right-click menu is Open + Delete.

## 4. The overview's own row (R5, R12)

### 4.1 TUI

`reviewTreeLines` draws "≡ Summary" first (today's row, renamed; its popup
unchanged), then — when the review has a stored overview — "≡ Overview".
Enter on it opens the **temporary overview's popup** (`overview_keys.go`)
over the review's `ReviewOverview` document: the same renderer, anchor
styling and keys, with the anchor target switched from the working tree to
the reviewed tip (plain for unresolved ones). No new popup type: the overview
popup takes an anchor opener, and the review view supplies one.

- `tab` / `shift+tab` move between resolved anchors (the current one
  highlighted; the first anchor is current on open), mouse click selects.
- `enter` opens the anchor's file **inside the review view** (the file at
  the reviewed tip, remarks visible), the cursor on line `N`, a band on
  `N-M`; every anchor of that file in the overview is drawn as a band and
  `n`/`p` step through them (overview anchor bands rules). The popup is
  hidden, not destroyed.
- `backspace` (and esc) in the opened file returns to the popup at the same
  anchor.
- The copy key of the overview popup copies the overview's markdown (as a
  temporary overview's does); the summary popup's copy key is unchanged.

A review without a stored overview draws exactly what it draws today, under
the renamed row.

### 4.2 Web

`reviews.js`'s file list draws "≡ Summary" (today's row, renamed; its
`.review-ov` pane unchanged, bar "Summary") and, when the review has a stored
overview, an "≡ Overview" row that opens the **temporary overview pane** over
`overviewMd`: the same rendering and anchor handling, the anchor target
switched to the reviewed tip — an anchor click opens the review's file at
the tip scrolled to the line (bands for every anchor of the file), Back
returns to the overview pane at the same scroll. Unresolved anchors are
plain spans.

## 5. The send panel (R6, R7, R9, R10)

### 5.1 Domain: candidates

`PRSendCandidates(ctx, n) (SendCandidates, error)` — the PR's diff must be
fetched (same precondition and error as `PRSendGroups`):

```
SendCandidates { PR int; Head string; Groups []SendCandidateGroup }
SendCandidateGroup {
    ID      string   // "review:<id>" | GroupMine | "replies"
    Kind    string   // review | mine | replies
    Agent, Title string; Created time.Time; Slot int   // a review's agent, first line of its summary, date, colour slot
    Rows    []SendCandidate
}
SendCandidate {
    ID        string   // note id | remark id review:<id>:<n> | draft reply id
    Kind      string   // note | remark | reply
    Severity  string   // remark meta "severity" ("" when none)
    Path      string; Range [2]int; Side string   // new | old (the wire note shape)
    Summary, Rationale string
    Sync      model.SyncState
    Code      []string // ≤ 4 source lines at the tip (old side: at the base), "" when the file is binary/missing
    Skip      string   // "" = sendable; else a SendSkip reason code (SkipLinesChanged, …): listed, not tickable
}
```

Order: each AI review newest first, then "My notes" (`GroupMine`), then
"Draft replies". Rows within a group: path, then line. A group with no rows
is omitted; no groups at all → the frontends say "nothing to send to #n"
without opening the panel. The filter is `PRSendGroups`'s (forge comments,
sending and already-on-GitHub rows are not candidates); `PRSendGroups` stays
as the CLI-facing summary (`gg pr notes`), the panel reads candidates.
`gg pr notes <n> --json` adds `group`, `severity` and `code` per row.

### 5.2 Domain: the request

`PRSendRequest` gains `BodyFrom string` (a review id). A `Notes` send may now
carry `Verdict` and a body:

- Body: `BodySet` → `Body` as is (an emptied box posts no body, standing
  ruling); else `BodyFrom` → that review's summary (`ReviewBodyText`), and
  the ledger stamps that review's summary as sent (`summaryKey`, as a
  `Review` send does) so a later send of its remaining remarks posts no
  second body; else no body.
- `Verdict` on a `Notes` send means the confirm offers comment / approve /
  request changes (as a `Review` send does); without it the confirm is
  send/abort and the review is a COMMENT. `Verdict` with no notes is still
  the verdict-only send ("Verdict…", R7).
- Remarks of several reviews and hand-written notes mix freely in one
  `Notes` send: one GitHub review (ruling A). Each remark is stamped under
  its own review's ledger as today.

**Draft replies in the same send (R6).** Today drafts with new comments is
`ErrMixedSend`. New: `engine.SendPlan` gains `Then *SendPlan` — a
`SendActions` plan the op runs **after** the review plan's writes succeeded.
One op, one reservation, one confirm: `describePlan` lists the review part,
then "and N replies" with their labels. If the review part aborts, fails or
is interrupted, `Then` does not run and the drafts stay drafts (the existing
finish/discard flow handles the interrupted review). A failure inside `Then`
is reported per reply as a `SendActions` failure is today; the review is
already posted and stays so. `planSend` builds `Then` when `req.Notes` holds
drafts **and** other notes (CLI `gg pr send --note <draft> --note <remark>`
gets it for free); `ErrMixedSend` remains for resolve/unresolve mixed with
new comments, and for drafts with `--review`/`--mine`.

### 5.3 TUI panel

A popup (`sendPanel`, embeds `popupMax`, scrolls when long) titled
`Send to GitHub — #<n>`:

```
Send to GitHub — #42                                  3 ticked
▌ Review (AI) · Claude Code · 2026-10-09 14:02        [a] all
  [x] high  internal/git/pull.go:120-134  lock released twice
  [ ] low   internal/git/pull.go:88       …
▌ My notes                                            [a] all
  [ ]       README.md:12                  …
▌ Draft replies                                       [a] all
  [x]       internal/git/pull.go:120      re: "…" → addressed
  [ ] ~     internal/tui/x.go:5           its lines changed

Body: review text (Claude Code · 14:02)          [b] change  [e] edit

space tick  a all/none in group  enter open  b body  e edit body  ctrl+s send  esc close
```

- Opens with nothing ticked (R7). `space` ticks the row (not a `Skip` row:
  the bottom bar says its reason); `a` on a group header or a row toggles all
  sendable rows of that group; `enter` opens the row's file at its line in the
  PR diff (the panel hidden, esc/backspace returns — `handOffToFilesView`).
- Body: `b` cycles **none → the summary of each ticked AI review (newest
  first, shown as "review text (<agent> · <time>)") → typed**; `e` opens the
  body box prefilled with the current choice (an edited box becomes "typed",
  kept per PR while the TUI runs — the existing kept-body memory, now per
  PR). With no AI review ticked the cycle is none → typed.
- `ctrl+s` with nothing ticked: bottom bar "tick something to send". Else
  builds `PRSendRequest{PR, Notes: ticked ids, Verdict: true, Body/BodySet or
  BodyFrom}` and hands it to `forgeSendCmd`: the existing confirm (comment /
  approve / request changes, or send/abort for the PR's own author) and the
  existing progress and outcome. On success the panel closes and the PR view
  refreshes as after any send; on abort the panel stays as it was.
- Rows show `Code` as a dim 1–4 line excerpt under the current row only
  (toggle with `c`), so the list stays dense.
- Entry points (R7): the PR `.` action-menu row `pr-send` "Send to GitHub…"
  replaces `pr-send-review` "Send review…"; PR details `s` opens the panel;
  help and the bottom-bar hints say "Send to GitHub…". "Verdict…" (`v`,
  `pr-verdict`) is unchanged.
- Removed: the `pr-send-group` decision, `sendReviewPopup`, `openSendReview`,
  `openSendReviewBody`, `sendGroupOptions`, `handleSendGroups`; the note-menu
  rows `note-send-review` ("Send this AI review…") and `note-send-drafts`
  ("Send my draft review…") (R9). Kept: `note-send` ("Send as GitHub
  comment" / "Retry sending as GitHub comment", R10), `note-reply-send`
  ("Reply & send…"), the verdict popup.
- New translated strings (four bundles): `Send to GitHub…`,
  `Send to GitHub — #%d`, `%d ticked`, `Review (AI)`, `My notes`,
  `Draft replies`, `Body: %s`, `none`, `review text (%s)`, `typed`,
  `tick something to send`, `nothing to send to #%d` (exists), the key-hint
  line, `Overview` (exists), `Summary` for the renamed row (exists), `its
  lines changed` (exists via the skip-reason map).
- Goldens: `e2e/scenarios/tui_pr_send.toml` — the note-menu screen loses the
  group rows, the PR action-menu screen says "Send to GitHub…", a new screen
  shows the panel with two rows ticked; every review-view golden with the
  "≡ Overview" row changes to "≡ Summary".

### 5.4 Web

- `GET /api/pr/send/candidates?pr=<n>` returns `SendCandidates` as JSON
  (markdown fields parsed to the wire tree like other note text; `code` as
  plain lines).
- `prsendpanel.js`: an overlay with the same layout — group headers with
  colour bar, agent, date and an all/none checkbox; rows with a checkbox,
  severity chip, `path:range`, summary, a ▸ that unfolds rationale + code;
  skip rows disabled with their reason; a body `<select>` (none / each ticked
  AI review's summary / typed) with a textarea for typed; a "Send N" button
  disabled at 0. Row building is pure (`prsendrows.js`, node-tested).
  Clicking a row's path opens the file at the line in the PR diff; the
  overlay closes and reopens with the ticks kept (page state).
- `POST /api/pr/send` kind `notes` accepts `verdict`, `body_from` (and the
  existing `body`/`body_set`); the server builds one request, the engine's
  `Then` handles drafts in the ids. Kind `group` is removed (only the old
  page flow used it); `verdict`, `resolve`, `unresolve`, `finish`, `discard`
  stay.
- Entry: the PR view's "Send review…" button/menu row becomes "Send to
  GitHub…" and opens the panel; "Verdict…" stays. Removed from `prsend.js`:
  `sendReviewPick`, `sendReviewBody`, `groupCount`. The note context menu
  (`sendRows`) keeps "Send as GitHub comment"/"Retry…" and "Reply & send…",
  drops the group rows (R9); `prsendjs_test.go` updated.
- The PR view's Reviews block, the "≡ Summary" rename and the "≡ Overview"
  row (§3.3, §4.2) land in the same plan.

### 5.5 CLI

`gg pr send <n> --note <id>… [--verdict] [--body <text> | --body-from
<review>]`. The "exactly one of" rule becomes: exactly one of `--note`,
`--review`, `--mine`, `--verdict`-alone, `--finish`, `--discard`, where
`--note` may add `--verdict` and one of `--body`/`--body-from`;
`--body-from` with anything but `--note` is a usage error. `--review` and
`--mine` are unchanged. A new e2e scenario beside `pr_readonly` (the fake
gh the e2e builder already provides) runs `gg pr send --note <remark> --note
<draft> --verdict --body-from <review>` with the confirm answered from
stdin: one review posted, then one reply, summary stamped.

## 6. Docs and skills

- Skills: gg-review (+ Overview step, `GGReviewVersion` 4), gg-cross-review
  (+ Overview step before Store it, `GGCrossReviewVersion` 5), gg-overview
  (the temporary/kept line, `GGOverviewVersion` 2), reviewing-with-gg
  (document field, `ReviewVersion` 17), using-gg (`gg pr send` flags, `gg pr
  notes --json` fields, `gg review show` overview and the `"summary"` JSON
  key, `Version` 161); then `gg init --update`.
- README: the PR section (send panel, PR reviews, stored overview), the
  review-view section (the "≡ Summary" and "≡ Overview" rows). CHANGELOG per
  plan. `docs/CLAUDE-details.md`: the candidates query, `Then`, the panel
  popup, the overview resolution, the `Summary` rename; CLAUDE.md map rows
  for `notebatch` (overview field) and `engine` (`Then`) only if a row
  changes meaning.

## 7. Tests

- notebatch: parse/canonical round-trip with and without `"overview"`; the
  64 KiB refusal; a document without the key canonicalises byte-identical to
  today.
- domain: `ReviewOverview` resolution (file in set / not, line in range /
  not, `note:` plain); `PreviewReviews` of a PR set (current / older /
  gone); `PRSendCandidates` (order, filter, skip rows, code excerpt, old
  side); `planSend` with mixed reviews + notes, with drafts → `Then`,
  `BodyFrom` stamp, `ErrMixedSend` still for resolve mixes; the send body
  ignores the overview.
- engine: `SendToForge` with `Then` — runs after success, not after
  abort/failure, confirm text lists both, a `Then` failure after a posted
  review.
- TUI: panel ticks/all/body cycle/ctrl+s request shape/nothing-ticked;
  enter-opens-and-returns; the PR Reviews rows and their handoff; the
  "≡ Overview" row (present only with a stored overview) and its popup's
  anchors (tab, enter opens at the tip, backspace); the "≡ Summary" row;
  menu rows (R9 gone, `pr-send` present); i18n gates; goldens.
- web: `prsendrows.js`/`prsendpanel` row builder node tests; the candidates
  handler; `/api/pr/send` with `verdict`/`body_from`; `/api/pr/open`
  reviews; `summaryMd`/`overviewMd` on the review wire.
- CLI: `parsePRSend` combinations; `gg review save` unresolved report; `gg
  review show` overview + `"summary"`; e2e fake-gh send.

## 8. Rulings

| # | ruling |
|---|---|
| R1 | The stored overview is opt-in: written only when the user asks (overview / tour / walk me through); not attachable later in v1. |
| R2 | It is an optional `"overview"` string in the review document; no migration; 64 KiB / 100 anchors. |
| R3 | Anchors `path`, `path:N`, `path:N-M` resolved against the reviewed tip; no `note:` anchors; unresolved drawn plain. |
| R4 | A PR's stored reviews are sub-rows in the PR file list, TUI and web; enter opens the review view; preview-reviews' outdated rule carries over. |
| R5 | The stored overview is navigated like a temporary one: tab / shift+tab / enter / backspace, anchors against the reviewed tip. |
| R6 | Draft replies ticked with new comments go as a second op after the review, under one confirm (`Then`). |
| R7 | The panel opens only from the PR level (PR `.` menu, PR details `s`), labelled "Send to GitHub…", replacing "Send review…"; "Verdict…" stays; nothing ticked on open. |
| R8 | Three plans after the spec (§9). |
| R9 | A note's own menu sends that one note only; the group rows leave the note menu in both frontends. |
| R10 | That row reads "Send as GitHub comment" (merged `0a1684ee`). |
| R11 | One word: an overview is the one document kind, temporary (`gg session overview`) or stored in a review; the review's text is its summary (`ReviewDoc.Summary`, `"summary"` everywhere). |
| R12 | The review view's first row is "≡ Summary"; a review with a stored overview gets a second row "≡ Overview" that opens the temporary overview's popup/pane over it (one word per row, one document per popup). |

## 9. Plans (R8)

1. **Core**: the `Summary` rename; notebatch overview field + limits; domain
   `ReviewOverview`, `PreviewReviews` for PRs, `PRSendCandidates`,
   `BodyFrom`, `planSend` mixes; engine `Then`; CLI (`gg pr send`, `gg pr
   notes --json`, `gg review save`/`show`); MCP review show; skills +
   versions + `gg init --update`; e2e CLI send.
2. **TUI**: the "≡ Summary" row; the "≡ Overview" row over the overview
   popup; PR Reviews rows; the send panel and the removals;
   menu/help/footer; i18n; goldens.
3. **Web**: `summaryMd`/`overviewMd`, the "≡ Summary" rename and the
   "≡ Overview" row over the overview pane; `/api/pr/open` reviews + Reviews
   block; candidates endpoint +
   the panel overlay; `/api/pr/send` changes and removals; node tests;
   README web notes.
