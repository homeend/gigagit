# Permanent overviews — design

Date: 2026-10-12 · Branch `feat/permanent-overviews`

## 1. Goal

An agent's overview (the gg-overview document: markdown whose links are
anchors into the code, walked beside the files in the overview split) lives
today only in a running gg's memory. Closing it or quitting gg loses it, and
the user must ask the agent again.

A **permanent overview** is the same document stored on a **commit**: an
explanation of the source code as it is at that commit, about the topic the
user asked the agent to investigate. It is kept like a commit review, shown
like an overview, and opens again any time.

## 2. User rulings (2026-10-12, do not re-ask)

- R1 **Permanent is the default.** Every overview an agent makes is
  permanent. It is temporary (in memory, today's flow) only when the user
  explicitly says so ("in-memory", "temporary").
- R2 **No promotion.** gg offers no key or menu entry that turns a temporary
  overview into a permanent one. The user asks the agent for a new one.
- R3 **Remarks are kept.** The agent's line notes are stored as remarks in
  the document (the review document's `files[].annotations`) and show as
  read-only boxes beside the file, like review remarks. The document links
  to lines, never to `note:t<n>`.
- R4 **Where it shows:** a `≡` mark on the commit in Commits; a
  `└ Overview: <title> <date> <agent>` row under the branch in Branches; an
  "Open overview: <title>" entry in the commit's `.` menu; View all notes;
  `gg overview list/show`; `?overview=<id>` links. Several overviews per
  commit (one per topic); the same commit + the same title replaces.
- R5 **Visibility follows where the note was created.** Made on a commit →
  shown wherever that commit appears, from any branch. Made on a branch tip →
  also a row under that branch while the branch still points at the commit;
  once the branch moves on, it shows on the commit only.
- R6 **gg enforces permanence.** `gg overview save` refuses — stores nothing,
  lists every problem — when anything the document anchors could not be
  shown again exactly: a missing path, lines past the end, a `note:` link, or
  an anchored file that differs on disk from the commit (uncommitted,
  staged, deleted, untracked). No `--force`.
- R7 **On an uncommitted-changes refusal the agent asks the user**: commit
  the listed files and save permanently, or keep the overview in memory only.
- R8 Storage approach A: the same notes store as a commit review, a new tag.

## 3. Storage

A permanent overview is a **commit-level note** in the existing notes store
(`notes` package, part `commits.toml` — the same file a commit review lives
in):

| Field | Value |
|---|---|
| `Address` | `{State: committed, Commit: <full sha>, Branch: <branch or "">}` |
| `Tags` | `["overview"]` (`model.OverviewTag`) — never `review` |
| `Source` / `Author` | `agent` / the agent's name |
| `Summary` | the title |
| `Rationale` | the document JSON (§4) |
| `Scope`, `Preview` | empty |

- `model.Note.IsOverviewNote()` = `IsCommitLevel() && NoteHasTag(n, OverviewTag)`.
  It is the ONLY test for "this is a permanent overview". `IsReviewNote`
  stays false for it, so every review reader skips it unchanged.
- `Branch` is the checked-out branch only when HEAD is that branch's tip and
  the overview is saved on HEAD (the `reviewBranchName` rule): an explicit
  commit link saves with `Branch: ""`.
- Its state is `ReviewKindOf(branch, commit, branchTip)` — on the branch /
  was the tip / on a commit — exactly a review's three states.
- **Replace:** a save whose commit and title (trimmed, exact) match an
  existing overview rewrites that note and keeps its id and `Created`.
- Never sent: the forge send path and its ledger only look at review notes;
  overviews carry no `Send`/`RemarkSends`.
- Never swept: a commit-level note never expires (`notes_sweep.go` already
  skips `IsCommitLevel`).

## 4. The document

The review document's JSON shape (`notebatch`), overview-only:

```json
{
  "version": 1,
  "title": "<short title>",
  "overview": "<markdown: the gg-overview kind>",
  "files": [
    { "path": "<repo-relative, slash form>",
      "annotations": [
        { "newRange": [40, 60], "summary": "<one line>", "rationale": "<why>" } ] } ]
}
```

- `title` and `overview` are required (`--title` fills a missing `title`).
- `summary`, `meta` and file `summary`s are refused: an overview is not a
  review (`"an overview has no summary — that is a review"`).
- `newRange` names lines of the file **at the commit**; `oldRange` is
  refused (there is no old side).
- A new parser `notebatch.ParseOverview` shares the review parser's field
  decoding; limits are the overview's: ≤ 64 KiB markdown, ≤ 100 anchors
  (`agentdocs.MaxOverviewBytes`/anchor cap), title ≤ `agentdocs.MaxOverviewTitle`.

## 5. Save check (R6)

`domain.SaveOverview(ctx, SaveOverviewCmd{Link, Title, Agent, Doc, DryRun})`
resolves the commit (HEAD when no link; a `gg://` link must name ONE commit —
a working-tree, staged, pair, preview or PR link is refused: "a permanent
overview needs a commit"), then checks, collecting **every** problem:

1. each overview anchor (`agentdocs.ParseOverview`) and each annotation:
   - `note:` anchor → `<dest>: a note link — link the lines instead`;
   - path not in the commit's tree → `<path>: not in <sha7>`;
   - lines past the end → `<path>:<a>-<b>: past the end (<n> lines at <sha7>)`;
2. the anchored + annotated paths against the disk of the checkout the
   command runs in:
   - `git status --porcelain -- <paths>` (one invocation): any entry
     (modified, staged, deleted, untracked, renamed) →
     `<path>: uncommitted changes`;
   - when the commit is not HEAD, also `git diff --name-only <sha> HEAD --
     <paths>` (one more invocation): a listed path →
     `<path>: differs from <sha7> on disk` — the file the agent read on disk
     is not the file at the commit.

Any problem → `ErrOverviewRefused{Problems []string}`; nothing is stored.
The CLI prints one problem per line and exits 1. `--dry-run` runs the check
and prints the commit, branch and title without storing.

The lines are read through `ResolveBytes` at the commit (the same reader
`overviewFileLines` uses); paths are matched against `git ls-tree -r
--name-only <sha> -- <paths>` (one invocation).

## 6. Domain read model

- `domain.Overview{ID, Commit, Branch, Kind ReviewNoteKind, Agent, Title,
  Created, Updated, Doc}` and `OverviewHead` (no `Doc`) for list rows.
- `NoteCounts.Overviews []OverviewHead` — filled by the same pass that fills
  `Reviews` (`notes.go:667`), so the TUI's `≡` and Branches rows cost no
  extra read.
- `Overviews(ctx)`, `Overview(ctx, id)`, `RemoveOverview(ctx, id)`,
  `OverviewsFor(ctx, commit)`.
- `OverviewDocAt(ctx, id) (OverviewDoc, error)` — the existing
  `OverviewDoc` (anchors with OK verdicts) resolved against the **whole tree
  at the commit** (any path that exists there; lines within the file). It
  shares the resolver with `ReviewOverview` through a `scope` func
  (`inReview` for reviews, `inTree` for permanent overviews).
- `OverviewFileNotes(ctx, id, path)` → the annotations on `path` as
  read-only remark boxes (the shape `ReviewFileNotes` returns).
- `OverviewLink(ctx, id)` → `gg://…@<sha>?overview=<id>`; `ResolveLink`
  handles the new hint kind `overview` (`model.OverviewHintKind`, added to
  `linkHintKinds`); a mismatched commit is `ErrReviewLinkMismatch`'s twin
  `ErrOverviewLinkMismatch`.

### Readers that must skip or label overviews

A note tagged `overview` is commit-level but not a review; every reader of
commit-level / plain notes must treat it explicitly:

| Site | Treatment |
|---|---|
| `notes.go:490` (line notes on a commit) | already skips (`Path == ""`) — test only |
| `notes.go:667` note counts | `IsOverviewNote` → `c.Overviews`; never in the ◆N badge or `Reviews` |
| `notes.go:870` entry-level resolve | treat like a review note (no line to re-anchor) — verify |
| `notes.go:1215` `storedGroup` | `"overview:" + id` (never "my draft review") |
| `notes_overview.go:112` View all notes | its own `Overviews` group under the commit, labelled Overview |
| `previewnotes.go:255`, PR view scope | never shown in a preview/PR (no `Preview`) — test |
| `forge_send_ledger.go:214` | skipped (not a review) — test |
| `review_threads.go:287`, `review_notes.go:333/433` | skipped (not a review) — test |
| `cli/note.go:708` `gg note` printing | `gg note list/show` prints it as `overview <id>: <title>`, not as a plain note |
| `notes/part_file.go:410` | entry-level part routing — commit part, verify |

## 7. CLI

```
gg overview save [<gg-link>] [--title <t>] --agent <name> --stdin [--json] [--dry-run]
gg overview list [--all] [--json]
gg overview show <id|link> [--json]
gg overview rm <id>
```

- `save` prints the overview's link on success (`--json`: `{id, link,
  commit, branch, title, replaced}`); on refusal one problem per line on
  stderr, exit 1.
- `list`: HEAD's overviews by default, `--all` the repo's; a row is `<id>
  <sha7> <on-branch|was-tip|commit> <date> <agent> <title>`.
- `show`: the title, commit, then the markdown; `--json` the stored document
  plus `id`, `link`, `commit`, `branch`, `kind`.
- `rm`: deletes the note.
- `save`/`rm` post a steer reload (like `gg note add`), so a running TUI or
  gg web page updates its note counts at once.
- `gg session navigate <overview link>` and `gg open <link>` open it
  (`linknav` carries the `overview` hint).
- No new MCP tools.

## 8. Skills

**gg-overview** (version bump):
- Permanent is the default. The recipe: investigate; write the §4 JSON
  (remarks as `annotations`, links as `path:N-M`, never `note:`); `gg
  overview save --agent "<name>" --stdin [--title …]`; fix and re-save on
  path/line problems; on `uncommitted changes` STOP and ask the user (commit
  them and save permanently, or keep it in memory only) — R7; on success run
  `gg session navigate <link>` when a gg window is open (`no gg session for
  this worktree` → skip), reply with the link, the title and a three-line
  summary. A permanent overview never starts `gg web`.
- Lines are the file **at the commit**.
- Temporary overview = today's recipe (`gg session note` + `gg session
  overview add`, starting gg web when nothing is open), used only when the
  user explicitly asks for an in-memory / temporary one.
- Worker agents: unchanged (the final `agent_report` tour).

**using-gg**: a "Permanent overviews" section with the four commands and
the refusal reasons. **reviewing-with-gg**: one line pointing at it.
`agentskill.Version` bumped; `gg init --update` after merge.

## 9. TUI

- **Commits**: `≡` beside the commit (and beside `✎` when both), from
  `noteCounts.Overviews`, on any branch (no `ReviewShownOn`).
- **Branches**: `└ Overview: <title> <date> <agent>` sub-rows (brEntry rows
  like review rows), only while the branch's tip is the overview's commit and
  its `Branch` is that branch; after the review rows.
- **Commit `.` menu**: one "Open overview: <title>" row per overview of the
  selected commit (stacked-diff parity test entry if the menu has a stacked
  twin).
- **View all notes**: an "Overview" group under its commit; `.` on the row =
  Open + Delete only.
- **Opening** = the existing overview split through `showOverview` with a
  stored-overview source: left the document (anchors OK per §6), right the
  file **at the commit** (read-only `FileRef{SourceCommit, sha, path}`) with
  `OverviewFileNotes` boxes; ↑/↓ links, n/p bands (W5: highlight only), `S`
  stacks the linked files in first-anchored order, `D` flips to the commit's
  own change to the file (`<sha>^..<sha>`; "this commit does not change
  <path>" on the status line when untouched — a root commit diffs against
  the empty tree), `y` copies the overview link, esc closes. Narrow
  (< 60 cols) → full-screen viewer as today.
- A steer reload / `r` refreshes note counts; a deleted overview that is
  open closes with a status line.
- Every new string through `i18n.T`, all four bundles; new ops mapped in
  `opAffectedSources`; help + footer advertise the `.` entry.

## 10. Web

Mirrors §9 in `gg web` and the hosted page: `≡` badge on commit rows; branch
sidebar sub-row; commit context menu "Open overview: <title>"; View all notes
group; the existing web overview split with the file at the commit + remark
boxes, `S`, `D`; `?overview=` links; delete from the row menu. New routes:
`GET /api/overviews?commit=`, `GET /api/overview/{id}` (document + resolved
anchors), file content via `/api/file-content?overview=<id>` (remarks ride
it, like `?review=`), `DELETE /api/overview/{id}`; wire values resolved
through the allowlist; loopback/Origin guards as every route.

## 11. Plans

1. **Domain + CLI + skills** (§3–§8) with an e2e scenario (save, refusal on
   a dirty file, list after the branch moved, show, rm).
2. **TUI** (§9) with golden screens.
3. **Web** (§10) with a Playwright check.

Each plan runs in this session, on its own branch off `main`, merged by the
user before the next starts.

## 12. Testing

- Domain (real repo): each refusal reason (missing path, past the end,
  `note:` link, modified / staged / deleted / untracked anchored file, an
  older commit whose file differs from HEAD); every problem listed, nothing
  stored; replace by title keeps the id; the three branch states; visible
  from a branch the commit was merged into; every §6 reader leaves it out of
  reviews / plain notes / forge sends / previews / PR view.
- CLI: output, `--json`, exit codes, `--dry-run` stores nothing.
- TUI / web: each surface; opening after the branch moved shows the file as
  it was at the commit; remark boxes; D on an untouched file.

## 13. Out of scope

MCP tools; promoting a temporary overview (R2); overviews on a range, a
merge preview, a PR or the working tree; sending overviews to a forge;
editing an overview in gg (re-save by title replaces it).
