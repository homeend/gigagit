# Preview reviews: AI review with an overview on a merge preview, `gg review save`, `/gg-review`

Status: agreed in brainstorming 2026-10-07, awaiting written-spec review.

## 1. Why

A review of a merge preview today is notes only. An agent handed a preview
`gg://` link can only `gg note add --preview …`: per-file notes, no overview.
The one path that produces a review DOCUMENT (summary + verdict + per-file
remarks, shown in the review view) is the headless review lane (`gg review`,
the TUI and web "Review …" rows), and:

- the Previews list has no "Review" row in either frontend (only open,
  rename, swap, copy link, remove);
- `gg review --preview` stores its review like a commit review on the source
  tip, so it puts ✎ on that commit on every branch and never shows in the
  preview;
- an agent the user runs interactively has no verb to store a document at all.

The user wants:

1. a **Review (AI)** row in a preview's menu (TUI `.`, web right-click);
2. a **`/gg-review <gg-link> [focus]`** slash command, so a review needs no
   "use the gg skills" preamble;
3. both producing a **proper review**: one document — overview first, then the
   files with their remarks — that shows **in the preview**.

Ad-hoc questions about one file or line keep producing ordinary notes (the
existing `gg note` lane); both kinds show in the preview.

## 2. Rulings (from the brainstorm)

| # | Ruling |
|---|--------|
| R1 | A review — from the menu or from `/gg-review` — is ONE review document (overview + per-file remarks), stored as one review note and opened in the existing review view. Single-file questions stay ordinary notes. |
| R2 | A preview review is shown while gg can show EXACTLY what was reviewed: both reviewed commits still exist. A review of an older tip is shown marked "older tip" and opens its reviewed range; one whose commits are gone is hidden from the preview (still in View all notes). Never synthesised from current files. |
| R3 | The Previews list shows a preview's reviews as SUB-ROWS under it (like the Branches tab's review sub-rows), TUI and web. |
| R4 | `/gg-review` and `gg review save` accept ANY gg:// link that names a change (preview, pair, commit, branch, working/staged). |
| R5 | A preview review belongs to its preview: it never marks a commit (✎) and never shows in a commit's or branch's Reviews heading. |
| R6 | Note rows' menus stay Open + Delete only (ruling 2026-10-05) — that includes the new sub-rows. |

## 3. Domain

### 3.1 Target and tag

`domain.ReviewTarget` gains:

```go
// Preview is the scope a review belongs to when it reviewed a merge preview
// ("<target>...<source>", branch NAMES) or a commit pair ("<a7>..<b7>").
// Data only — never spliced into a command (Range stays the hex pair).
Preview string
```

One constructor builds the target for a scope, replacing the CLI's inline
literal (`internal/cli/review.go`, the `pf.set()` arm):

```go
// ScopeReviewTarget is the review of a merge preview or commit pair: Range =
// the scope's hex pair (merge base..source tip, or a..b), Label = the human
// pair, Commit = the tip, Preview = set.Pair().
func ScopeReviewTarget(set PreviewNoteSet) ReviewTarget
```

`gg review --preview`, the TUI row, the web route and `gg review save` all
call it. A preview that is not `PreviewOK` (merged, missing side, no base)
is refused by the caller exactly as `resolvePreviewTarget` refuses it today.

`SaveReview` copies `Target.Preview` onto `Note.Preview`. `notes.PartOf`
already routes a note whose `Preview` holds `...` to `previews.toml`; a pair
review (`a7..b7`) stays in `commits.toml` — the same parts the scopes' line
notes use. The review note's `Scope` is the full hex range it read, so the
review view already opens exactly that range (`reviewRevs`).

### 3.2 Reading

- `Review` and `ReviewHead` gain `Preview string` and `Older bool`.
- **Belongs-to-preview filter (R5):** every commit-facing read —
  `commitReviewed` (✎), a commit's Reviews heading (`commitReviewsMsg`), the
  Branches review sub-rows (`branchReviewHeads`) and their web twins — skips
  a review whose `Preview != ""`. View all notes keeps listing it, its
  "where" column naming the preview.
- **`PreviewReviews(ctx, set PreviewNoteSet) ([]ReviewHead, error)`** — the
  reviews of one scope, newest first, each classified (R2):
  - *current*: its tip is the scope's tip now (merge preview: the source's
    current tip; a pair's tip never moves, so a pair review is current while
    both its commits exist);
  - *older*: the tip moved but BOTH the scope's base and tip still resolve as
    commits (`cat-file -e <sha>^{commit}`) — `Older = true`;
  - *gone*: either commit is missing — omitted.
  Matching is by `Preview` == `set.Pair()` (branch names), so it survives the
  saved row being removed and re-added and works for a preview opened only
  from a link.
- **Cost:** the Previews list needs the heads for every row. One
  `PreviewReviewHeads(ctx) map[string][]ReviewHead` keyed by scope name is
  read with the note counts (one store read) and the "do the commits exist"
  checks run once per refresh, batched (`git cat-file --batch-check`), never
  per rendered row.

### 3.3 Branch rename / delete

`reviewsFollowBranchOp` also loads `PartPreviews`: a rename rewrites the
branch name inside `Note.Preview` (either side of `...`). A delete leaves
preview reviews alone — they are kept while their commits exist (R2).

### 3.4 Existing data

Reviews stored by `gg review --preview` before this change carry no
`Preview`: they stay where they show today. No migration.

## 4. CLI: `gg review save`

```
gg review save <gg-link> --agent <name> (--stdin | --file <path>) [--json]
```

The link is resolved by `domain.ResolveLink` (the `gg open` resolver); its
CHANGE decides the target; a path/line part is ignored:

| Link names | Target |
|---|---|
| a merge preview (`<target>...<source>`, with or without `?preview=<id>`) | `ScopeReviewTarget` of the preview (§3.1) |
| a pair `@<a>..<b>` | `ScopeReviewTarget` of the pair |
| one commit | that commit's change (`sha^..sha`; a root commit alone) — the TUI's `reviewTargetForCommit` moved to domain |
| `@ref:<name>` | `BranchReviewTarget(name)` (base = the trunk) |
| the working tree or `@staged` | `WorkingReviewTarget()` in the link's checkout |

- **Input** must be the review document (`notebatch.ParseReview`); anything
  else exits 1 with the parse error and nothing is stored (unlike the lane,
  prose is not accepted: the caller is an agent that can fix its JSON).
- **Refusals:** a link with no change (a bare repo link, a hint-only link
  whose entry does not name one, a two-link comparison) — "names no change
  to review; give a commit, a pair, a merge preview, a branch or the working
  tree"; a link that resolves to another repository than the current one —
  refused.
- **Store:** `SaveReview` (same retry on a held lock, same quarantine warning).
- **Output:** `review: <id>` and the `gg://…?review=<id>` link
  (`Service.ReviewLink`); `--json` → `{"id","link","warn"}`.
- No MCP tool in this spec.

## 5. Skills

- **New embedded skill `gg-review`** (`internal/agentskill/gg-review.md`),
  installed by `gg init` / `gg init --update` for every `ModeSkillFile` agent
  (Claude Code project+global, Junie, Kimi, Antigravity). Frontmatter adds
  `argument-hint: <gg-link> [what to focus on]` and
  `disable-model-invocation: true` (user-invoked only; agents that ignore the
  keys simply load it as a skill). Body:
  1. `$ARGUMENTS`: the first word is the link, the rest the user's focus.
  2. Read the change: `gg diff --stat <link>`, `gg diff --hunks --json <link>`,
     `gg diff <link> -- <file>`.
  3. Write the review document (the reviewing-with-gg "Review document"
     shape: `## Summary`, `## Findings`, `## Verdict` in `summary`; remarks
     per file).
  4. `gg review save <link> --agent "<your name>" --stdin` with the document.
  5. If a gg window is live on this worktree (`gg session list`), open it there:
     `gg session navigate <review-link>`.
  6. Reply: the review link and a three-line summary.
- **reviewing-with-gg:** a "Writing a full review yourself" section pointing
  to `gg review save`; single-file questions keep using `gg note add`.
- **using-gg:** `gg review save` in the CLI reference; bump
  `agentskill.Version`.
- Codex (`ModeBlock`, AGENTS.md only, no slash commands) gets the
  reviewing-with-gg paragraph, no `/gg-review`.

## 6. TUI

- **Previews list rows:** the list becomes entries (a preview row or a
  review sub-row), the Branches tab's `brEntry` pattern: sorting and the
  filter keep a sub-row with its preview; `backingIndex`/`selectedPreview`
  map through it. Sub-row text: `└ Review: 10-07 14:02 Claude Code 3/5`
  (`reviewStampAt`, `withTally`), plus ` · older tip` when `Older`. Merge
  preview and pair rows only; a comparison row has none.
- **Enter on a sub-row** opens the review view (`openReview`). Its `.` menu:
  Open + Delete (R6, the existing review row menu).
- **Preview row `.` menu** gains:
  - **Review (AI)** (`preview-review`): gated like the other review rows —
    `opsIdle`, `hasReviewTool`, a merge preview in `PreviewOK` or a pair with
    both commits. The target needs a ctx (merge base) → the
    `reviewTargetReadyMsg` async hop, then `startReview`.
  - **Show review** (`preview-show-review`): the newest current review; only
    when one exists.
- **Opened merge preview / pair:** a "Reviews" heading at the top of its file
  list, one row per `PreviewReviews` head; enter opens the review view, esc
  returns to the preview (window-return convention).
- **Review view:** header names the preview (`main ... feat/x`) and says
  "older tip" when `Older`. Nothing else changes.
- **Refresh:** a lane result reloads `srcNotes` (as today). A steer navigate
  with a `review` hint also reloads `srcNotes` first, so a review an agent
  just saved shows on arrival. Otherwise an external save shows on the next
  refresh (`r`) — there is no note-store watcher, and this spec adds none.
- New ops need no `opAffectedSources` entry (the review lane is already
  mapped). All strings through `i18n.T` in all four bundles; help + footer
  mention Review (AI) on Previews.

## 7. Web

- `GET /api/preview` entries and the pair entries of `GET /api/saved-compares` carry
  `reviews: [{id, agent, created, remarks, resolved, older}]`; the list
  renders the same sub-rows; click opens the review (`openReview`), right-click
  Open + Delete.
- `showPreviewMenu` and `showPairMenu` gain **review (AI)…** →
  `startReview("preview", …)` and **show review**.
- `reviewStartRequest` gains `preview` — the saved row's ID, looked up
  server-side (`NoteScopeResolve`); no ref name is taken off the wire.
  `reviewTarget(kind="preview")` builds `ScopeReviewTarget`.
- The opened preview's file list gets the same "Reviews" block on top.
- Commit-facing review markers skip preview reviews (R5).

## 8. Errors

- Review (AI) hidden for a merged / missing-side / no-base preview (the CLI
  refuses the same).
- A preview whose source branch was deleted: its reviews are still classified
  by commit (R2) — shown "older tip" while the commits exist.
- `gg review save`: §4 refusals; a held note-store lock is retried within the
  existing budget.

## 9. Testing (TDD, real git in temp repos)

- **domain:** `ScopeReviewTarget` for a preview and a pair; `SaveReview`
  tags `Preview`; `PreviewReviews` current / older / gone (gone = a
  force-moved branch + an unreachable tip removed with `git gc --prune=now`);
  R5 filters (no ✎, not in a commit's or branch's heads); rename rewrites
  `Preview`.
- **cli:** `gg review save` per link kind, refusals, non-document input,
  `--json`; `gg review --preview` now tags its review.
- **tui:** sub-row render/sort/filter/enter/menu gating; Review (AI) gating;
  the opened preview's Reviews heading; older-tip header; steer navigate to a
  freshly saved review shows it.
- **web:** handler tests for `target=preview` (good id, unknown id, merged)
  and the entries' `reviews`; a playwright check that a sub-row and the
  heading are VISIBLE.
- **e2e:** a scenario — `gg review save` on a preview link, then the TUI
  Previews tab shows the sub-row (golden screen).
- **agentskill / agentinit:** the new skill embedded, versioned, installed
  for the SKILL.md agents and not for Codex.

## 10. Delivery

One spec, three plans, each its own branch merged in order:

1. **Domain + CLI + skills** — §3, §4, §5. `/gg-review` works end to end;
   the review opens via its link.
2. **TUI** — §6.
3. **Web** — §7.

Docs per stage: CHANGELOG (always), README (user-facing surface),
`docs/CLAUDE-details.md` (the preview-review tag and R2 classification).
