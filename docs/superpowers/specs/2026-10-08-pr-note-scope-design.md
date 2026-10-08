# A pull request shows only its own notes — design

**Date:** 2026-10-08 · **Status:** approved in chat, awaiting spec review
**Supersedes:** `2026-10-07-github-write-design.md` §1.4 ("Which local
notes show in a PR": keep every note on the PR's commits + carried notes).

## 0. The rule (user ruling 2026-10-08, verbatim)

> "PR should only show things that were added to this particular PR.
> Review of the commit should show only notes added for review of the
> commit. Review of the branch should show only notes for the review of
> the branch. … no notes that are not attached to given thing should be
> shown on the given thing. So if there is a need to extend addressing so
> the note had explicitly address that it is used for PR, then add it."

Trigger: a PR opened the same day showed an agent's note written two days
earlier on something else (a *carried* note, §1.4 of the old spec).

Commit and branch reviews already keep to the rule: a review is opened by
its id and shows its own remarks, and a commit's own view shows only plain
notes (`domain.PlainNotes`, `NoteCounts.ScopesShownOn` — a preview-scoped
note is never a commit's). The pull request's view is the one exception,
and this design closes it.

## 1. What a pull request's view shows

Exactly these, and nothing else:

1. **GitHub's review threads** of that PR (forge data) and the local draft
   replies to them (`IsForgeReply`) — unchanged.
2. **Notes written for that PR** (§2): a root note whose scope names that
   PR, anchored on the new side of a commit in the PR's range
   (`merge-base..head`, the existing preview rule), plus its replies.
3. **AI reviews run on that PR**: a review note whose scope names that PR,
   and its remarks (`prReviewNotes`, unchanged otherwise).

Removed from the PR's view:

- **Carried notes** — notes from anywhere whose anchored lines reappear in
  the PR's head (`domain/forge_carried.go`). The feature is deleted:
  `carriedNotes`, `carriedCache`, `OriginWorkingTree`,
  `ResolvedNote.Origin` and its display (TUI `noteOrigin`, web
  `notebox.js` "· from …", CLI `gg pr notes` "(from <origin>)").
- **Every other note on the PR's commits** — plain commit notes, and notes
  of another review that happen to sit on a PR commit.
- **Reviews of a PR commit or branch** — `prReviewHeads` loses its
  `NoteCounts.Reviews` commit-membership half; only `PreviewReviews` whose
  scope names the PR remain.

Everything that reads the PR's notes goes through the same functions
(`PreviewNotesFor/At/All`, `PreviewNoteCounts`, `PreviewNoteGroups`,
`prReviews`), so the narrowing reaches every surface at once: the diff's
note boxes (TUI, web), the file list's ◆ badges and group stripes, the PR
list's note counts, `}`/`{` stepping, `gg pr notes`, the MCP reads, and the
send planner — *Send review…* / *my draft review* send only that PR's own
notes, and a send's id check accepts only them.

## 2. The PR address on a note

**Shape.** A PR note records its scope in `model.Note.Preview` as
`<base>...refs/gg/pr/<n>` — the name `PreviewNoteSet.Pair()` already gives
a PR's set, and the name AI reviews saved on a PR already carry.

- `<base>` is whatever the writer had: the TUI and web build a PR's set from
  `PRPair{Base: <base sha>, Head: refs/gg/pr/<n>}`; an agent types e.g.
  `origin/main...refs/gg/pr/7`.
- **Matching is by PR number, never by the whole string**: a note belongs
  to PR *n* when the part after `...` is exactly `refs/gg/pr/<n>`. The base
  moves when the PR's base branch moves or the PR is rebased; the note
  stays the PR's.
- New domain helper: `PRScopeNumber(scope string) (int, bool)` — the one
  parser (uses `git.ParsePRRef` on the source half). `prReviewHeads`'
  existing `strings.HasSuffix(sc, "..."+set.Source)` becomes this helper.
- The shape contains `...`, so storage routing is unchanged: `notes.PartOf`
  sends it to `previews.toml`, `scopeParts`/`IsPreviewScope` read that part.
  A reply carries no scope and follows its root (unchanged).

**`PreviewNoteSet.scope()`** stops returning `""` for a PR set and returns
`Pair()`. The filter in `loadPreviewNotes` (and its reply rule) compares
through a set method `owns(preview string) bool`: a PR set by
`PRScopeNumber`, every other set by equality (today's rule). The count
cache key (`previewStoreCounts`) keeps `Tip:Base:scope()`.

**A force-push** that drops the commit a PR note sits on hides that note
from the PR — the existing preview rule (`commitSet` membership), stated
here, not re-decided.

## 3. Every writer stamps it

| Writer | Today | Change |
|---|---|---|
| TUI note form (`note_popup.go:122`) | skips the stamp when `previewOpen.prNumber != 0` | stamp `set.Pair()` for a PR too |
| AI review on a PR — `gg review --link <PR link>` / `/gg-review`, cross-review, a saved preview of the PR pair (`ScopeReviewTarget`, `review.go:99`; the TUI has no Review (AI) on an opened PR) | `Preview: set.scope()` = `""` → stored as a plain commit review | gets the PR stamp through `scope()`; nothing else |
| gg web note (`files.js` `noteScopeSpec`, `/api/notes/add`) | `noteScopeSpec` returns `""` for `pv.pr` | the request carries the PR number (`pr: n`, a new field — the page never sends a PR's names back as refs, `previews.js`); the server builds the PR's set from its cached pair (`PRPair`) and stamps `set.Pair()` only when the set's tip is the note's commit (the guard `notePreview` already applies) |
| `gg note add/apply --preview <base>...refs/gg/pr/<n>`, MCP `gg_notes_add/apply` `preview`, agent JSON batches, `gg review save --preview` | already stamp `set.Pair()` | none — pinned by tests |
| Replies (TUI, web, CLI, MCP) | follow their root | none |

## 4. Outside the PR

- A PR note never shows on its commit: the commit's diff, file badges and
  Range-review rows already leave out preview-scoped notes
  (`PlainNotes`, `ScopesShownOn`). (This corrects the chat design, which
  promised a "PR #7" row on the commit: merge-preview notes get no such row
  today, and a PR note follows them.)
- **View all notes** (TUI, web) lists PR notes like other preview notes and
  opens them through the existing range path (`ScopeAtCommit`).
  `NoteScopeLabel` names a PR scope **`PR #<n>`** instead of
  `refs/gg/pr/<n> → <base>`; the TUI title becomes "Preview review: PR #7".
  No new i18n key (the label is data inside an existing format).

## 5. Existing data (accepted consequences, no conversion)

- Notes written in a PR view before this change carry no stamp and are
  indistinguishable from plain commit notes: they stay visible on their
  commit and leave the PR.
- AI reviews run on a PR from the TUI were stored untagged (as commit
  reviews): they stay on their commit and leave the PR.
- Notes and reviews already stamped `…refs/gg/pr/<n>` (agent batches, web
  and CLI `gg review save --preview`) keep showing in their PR.

## 6. Out of scope

- A shorter PR spelling for agents (`--preview pr:7`). The existing
  `<base>...refs/gg/pr/<n>` spelling works; the skill text says so.
- Opening a PR note from View all notes into the PR's own view (it opens
  the frozen range, as every preview note does).

## 7. Docs

- This spec; a "superseded by" line under the old §1.4.
- `CHANGELOG.md` (a PR shows only its own notes; carried notes removed).
- `README.md` `gg pr notes` line (no "carried ones").
- `internal/agentskill/using-gg.md`: `gg pr notes` drops "(from
  <origin>)"; one line: a note or review for a PR is written with
  `--preview <base>...refs/gg/pr/<n>` and shows only in that PR. Bump
  `agentskill.Version`, then `gg init --update` after merge.
- `docs/CLAUDE-details.md`: the PR note rule and `PRScopeNumber`.

## 8. Testing

- Domain: a PR set shows a note stamped for it (any base spelling, and
  after the base moved), and hides: a plain note on a PR commit, a note of
  another review on a PR commit, a note stamped for another PR, a note that
  would have been carried, a commit review whose tip is a PR commit. Counts,
  groups and `PreviewNotesAll` agree with the boxes. A TUI-path review
  (`ScopeReviewTarget` on a PR set) is stored with the PR stamp and shows.
- Send: the planner's "mine" group and the id check exclude a plain note on
  a PR commit.
- TUI: a note written in a PR's diff is stored with the PR stamp and
  appears in that PR's view, not in the commit's own diff.
- Web: a note added with `pr: n` is stamped with that PR's scope, and one
  whose commit is not the PR's tip is not; a PR note's box is drawn in the PR's diff
  (browser probe, run on the old build first: there the stray note shows).
- `NoteScopeLabel("<sha>...refs/gg/pr/7") == "PR #7"`.
- Existing carried-note and plan-3 T1/T2 tests that assert the old rule are
  rewritten to the new one (not deleted).
