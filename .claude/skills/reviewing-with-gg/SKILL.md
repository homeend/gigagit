---
name: reviewing-with-gg
description: Use when reviewing code changes in a repository where the gg CLI is available — inspect diffs and leave anchored review notes with gg note.
---

<!-- gg:reviewing-with-gg:v16 -->

# Reviewing with gg

gg is a terminal git client with a persistent, machine-local store of anchored
review notes. The TUI (`gg`) and the browser UI (`gg web`) belong to the user —
do NOT launch either. Leave notes with the `gg note` CLI verbs; the user reads
them inline in their own diff view.

Notes survive quitting gg, so you can annotate a repository while nothing is
running. They are working material: gg drops them once they expire or their
anchored lines are gone.

## Workflow

```text
1. gg status                                  # what changed at all
2. gg diff --stat                             # which files, how much
3. gg diff --hunks --json                     # numbered hunks per file
4. gg diff -- <file>                          # read the actual change
5. gg note list --json                        # what has already been said
6. gg note apply --stdin                      # leave several notes in one call
   (or gg note add … for a single remark)
7. print `gg link --preview <P>` and hand that link back
```

Inspect before you annotate. Read the whole change first, then comment on
intent, structure, risk and follow-ups — not on every hunk.

When you changed code on a branch, review it the way the user will: open a
merge preview onto its target (`gg preview add <source> <target>`), then
annotate every change you made with `gg note add --preview <id|label> --file
<path> --new-line <n> --summary "…" --rationale "…"`. The notes are stored on
the source tip and travel with it; when you push further commits the preview
keeps showing them, marking the ones whose lines moved `outdated`. When you are
done, print `gg link --preview <id|label>` and hand THAT link back to the user
— they open it with `gg open <link>`, which steers their running gg straight
into the preview. A bare preview name only works on your machine; the link
works everywhere.

When the change to review is "what happened between two commits" rather than a
branch against its target — a previous state of the branch, another agent's
attempt — use a COMMIT PAIR the same way: `gg note add --preview <a>..<b>
--file <path> --new-line <n> …` (or `gg preview add <a>..<b>` first and use its
id). Notes land on `<b>`, new side only. Hand back `gg link --preview <a>..<b>`
— a `gg://<repo>@<a>..<b>` link the user opens with `gg open`.

## Checking another agent's review

When the human pastes a review link (`gg://…?review=<id>`) and asks you to
check it: `gg review show <link>` prints the other agent's overview and every
remark, numbered, each with its own line link, its thread id
(`review:<id>:<n>`), its replies and whether it is resolved. For each remark,
read the code it points at (`gg link text <remark-link>`, or `gg diff
<review-link>` for the whole reviewed change) and decide whether it holds. Do
not re-review from scratch unless asked; the job is to check the remarks you
were handed.

Answer IN the remark's thread — the human and the review's author read it
there (TUI, gg web, `gg review show`):

1. Reply: `gg note reply review:<id>:<n> --summary "…" [--rationale "…"]
   [--link <commit|gg://…>]` — `--link` points at your fix.
2. Settle what is settled: `gg note resolve review:<id>:<n>` (reopen with
   `gg note unresolve`). Leave a remark open when you disagree, and say why
   in the reply.
3. Or all at once: `gg note apply --stdin` with
   `{"comments":[{"replyTo":"review:<id>:<n>","summary":"…","link":"…","resolve":true}, …]}`
   — one batch, rolled back whole on a failure.

`review:latest:<n>` names a remark of the newest review. The review's author
reads your answers back with `gg review show`.

Findings the review MISSED are not answers: leave each as an ordinary note on
the change it reviewed — `gg note add --rev <tip> --file <path> --new-line N
--summary "…"` for a one-commit review, `--preview <base>..<tip>` for a range
review (`gg review show` prints both ends in its header). They show beside the
review, on the commit's Notes rows. Say in your report how many you added.

## Writing a full review yourself

When the user asks you to REVIEW a change (not one file — the whole change):
store one review document — the "Review document" shape below — with
`gg review save <gg-link> --agent "<your name>" --stdin`. Run
`gg review save <gg-link> --dry-run` first: it prints what is reviewed and
the `gg diff` argument that shows exactly that change. The review opens in
the user's review view (overview, files, your remarks at their lines); on a
merge preview or a commit pair it belongs to that preview. The user can also
start this with `/gg-review <gg-link> [focus]`, or with
`/gg-cross-review <gg-link> [2|3] [focus]` for a review by 2–3 copies of you
on different models, merged and with their disagreements settled. A question
about one file or line stays an ordinary note (`gg note add`).

## Reviewing a pull request

GitHub pull requests are read through `gg pr` (it needs `gh` logged in).
The user sends to GitHub, never you: nothing you run posts there.

```text
1. gg pr list                      # the open PRs (gg pr list --search <text> for others)
2. gg pr view <n>                  # description + conversation + verdicts
3. gg pr comments <n>              # inline threads already open, with [<thread id>]
4. gg pr fetch <n>                 # its head as refs/gg/pr/<n> (local only)
5. gg diff --stat <base>...refs/gg/pr/<n>   then   gg diff <base>...refs/gg/pr/<n> -- <file>
6. write: notes   gg note add --preview <base>...refs/gg/pr/<n> --file <path> --new-line <n> --summary "…"
   or one review  gg review save "$(gg link --pr <n>)" --agent "<your name>" --stdin
7. answer a thread: gg pr reply <n> <thread id> "<text>"   (a local DRAFT — no --send)
8. gg pr notes <n>                 # what the PR's view now holds
9. hand back `gg link --pr <n>`
```

`<base>` is the PR's target branch from `gg pr view`. A note on the PR's
commit written WITHOUT `--preview <base>...refs/gg/pr/<n>` is the commit's,
not the PR's: it does not show in the PR's view. Read what reviewers already
said (steps 2–3) before you write, and do not repeat it — reply to their
thread instead. Then tell the user what is ready: they send your notes,
review and drafts from gg's PR view, gg web, or `gg pr send` at their own
terminal (inside a gg session `gg pr send` refuses you). Never run `gh` to
comment, review or resolve.

## Choosing the target

A note anchors to ONE base and ONE result. The flags pick which:

| Flags | What the note anchors to | Old side | New side |
|---|---|---|---|
| (none) | the unstaged working tree | the index | the working file |
| `--cached` | the staged diff | HEAD | the index |
| `--rev <commit>` | that commit's own change | its parent | the commit |

`--rev` takes ONE commit; a range (`A..B`) is refused. `--cached` and `--rev`
cannot be combined. `gg diff --hunks` names the commit as a POSITIONAL argument
where `gg note` uses `--rev` — point both at the same target, so the hunk
number you read is the hunk number you pass back.

## Commands

### Inspect

```bash
gg status
gg diff --stat [--cached] [<commit>] [-- <paths>...]
gg diff --hunks [--json] [--cached] [<commit>] [-- <paths>...]
gg diff [--cached] [<commit>] [-- <paths>...]
gg show <commit> [--patch]
gg note list [--file <path>] [--type user|agent|all] [--cached | --rev <c>] [--json]
```

The positional commit does NOT mean the same thing everywhere: `gg diff
--hunks <commit>` shows that commit's own change (like `--rev`), but plain
`gg diff <commit>` compares the WORKING TREE against it — empty on a clean
checkout. To read a commit's own change, use `gg show <commit> --patch`.

`gg diff --hunks` also refuses `--cached` together with a single commit (exit
2): staged hunks are HEAD→index, a commit's hunks are its own change, and one
command cannot number both.

`gg diff --hunks` numbers each file's git `@@` hunks 1-based:

```text
src/search.ts
  1 @@ -15,7 +15,9 @@ export function score
  2 @@ -40,3 +42,8 @@
```

### Notes

```bash
gg note add   --file <path> (--hunk N | --new-line N | --old-line N) [--cached | --rev <c>] \
              --summary "…" [--rationale "…"] [--author <name>] [--source user|agent] [--json]
gg note reply [<repo-link>] <note-id> --summary "…" [--rationale "…"] [--author <name>] [--json]
gg note apply [<repo-link>] --stdin [--cached | --rev <c>] [--author <name>] [--json]
gg note rm    [<repo-link>] <note-id>
gg note clear [<link>] (--file <path> | --all) [--type user|agent|all] --yes
```

- `add` and `reply` print the new note id; `--json` prints the note object.
- Line numbers are 1-based. `--new-line` is the line in the NEW version of the
  file, `--old-line` in the old one; `--hunk N` covers the hunk's whole span
  (its new-side span, or its old-side span when the new side is empty — a
  deleted file, or a file whose whole content was removed).
- `rm` on a thread root takes its replies with it. `clear` prints
  `removed N notes`, counting records — roots AND their replies.
- CLI notes default to `--source agent` and to `$GG_AGENT` as the author.

`note apply --stdin` accepts either JSON shape, chosen by the top-level key:

```json
{"version":1,"summary":"optional overall note","files":[
  {"path":"src/search.ts","summary":"optional file note","annotations":[
    {"newRange":[15,23],"summary":"…","rationale":"…","tags":["perf"],"confidence":"high"}]}]}
```

```json
{"comments":[
  {"filePath":"src/search.ts","newLine":18,"summary":"…","rationale":"…"},
  {"filePath":"src/search.ts","hunk":2,"summary":"…"},
  {"replyTo":"a1b2c3d4","summary":"addressed in the latest revision"}]}
```

An annotation needs a `summary` plus at least one of `newRange` / `oldRange`
(`newRange` wins when both are present). A comment needs `summary` plus either
`replyTo` alone or `filePath` with exactly one of `hunk`, `hunkNumber`,
`newLine`, `oldLine`. The whole batch is validated before anything is stored,
so one bad item leaves the store untouched. Top-level and per-file `summary`
values have no anchor in gg: they are echoed to stderr as `context: …` lines,
not stored.

## Guiding a review

- Comment on what the reader would NOT spot: a broken invariant, a case the
  change forgets, a name that now lies, a risk the diff hides.
- One note per idea. Put the finding in `summary` and the reasoning in
  `rationale`.
- Anchor precisely: a line beats a hunk when you mean one line.
- Read `gg note list --json` first and reply to an existing thread instead of
  repeating it.
- Do not annotate every hunk, and never leave a note that just restates the
  diff.
- Say what you found in your chat reply too — the notes are for the code, the
  reply is for the person.
- **Quote a link for every finding** so the reader can jump to it:
  `gg link src/search.ts:42` prints a `gg://` address (add `--cached` or
  `--rev <sha>` to name the same target your note used). Paste it in the reply;
  `gg session navigate <link>` takes it straight back. Quote a link carrying
  `#<hunk>` — an unquoted `#` starts a shell comment.
- **Note ids are per repository.** To reply to, remove or import notes from
  a directory that is not that checkout, put the repository's link first —
  `gg note reply gg://<repo> <note-id> --summary "…"` (a `gg link` printed
  inside the repo, without its file part). A bare id the current repository
  does not hold exits 1 and names the store it searched.

## Steering the user's window

If the human has gg open on this worktree, you can put their window on the
line you are talking about. Check first:

```bash
gg session status          # exit 1 = nothing open; do nothing more
```

When a session is live, navigate to a hunk BEFORE you leave its note, and
again after `gg note apply` so the reader lands on the first finding:

```bash
gg session navigate --file src/search.ts --hunk 2
gg session navigate --file src/search.ts --new-line 42 --cached
gg session navigate --rev <sha>                     # reveal a commit
gg session navigate --next-comment                  # step the open diff
gg session navigate gg://gigagit/src/search.ts@abc1234:42   # a pasted link works everywhere a flag pair does
gg session reload notes                             # after writing notes
gg session focus files
gg session highlight add --file src/search.ts --start 40 --end 46 --tone warn
gg session highlight clear --file src/search.ts
```

- `--file` + one of `--hunk N` / `--new-line N` / `--old-line N`; the target
  flags are `gg note`'s (`--cached`, `--rev <sha>`, neither = the working tree).
- A command WAITS up to 2s for the window's answer and prints what happened.
  Use `--no-wait` for fire-and-forget; it prints the command id and exits 0.
- Exit 1 means either no session is live (`no gg session for this worktree`)
  or the window refused: an operation is running, a decision is
  waiting for the user, they are typing, or a picker owns the screen. That is
  not an error to work around — say what you found and move on.
- `gg note add|reply|apply|rm|clear` already posts a reload on its own; you only need
  `gg session reload` after changing notes some other way.
- NEVER launch the TUI or `gg web` yourself. Steering drives a window the human
  chose to open; if none is open, your notes are still waiting for them.

## Review document

When gg runs you as a review agent (`gg review`, the TUI's or the browser's
review lane), its brief at `$GG_CONTEXT_FILE` ends in a "Review output"
section. Reply with ONLY this JSON document, written to the file named by
`$GG_MESSAGE_FILE` (a fenced block or Claude's JSON envelope is unwrapped):

```json
{
  "version": 1,
  "summary": "<markdown: the overall review — what changed, what matters, the verdict>",
  "meta": { "verdict": "approve | comment | request changes" },
  "files": [
    { "path": "<repo-relative path>", "summary": "<optional one line about this file>",
      "annotations": [
        { "newRange": [12, 14], "summary": "<one line>", "rationale": "<why it matters>",
          "meta": { "severity": "bug | risk | design | nit", "confidence": "low | medium | high" } }
      ] }
  ]
}
```

`"summary"` is shown as rendered markdown in the review's overview, so give it
structure: `## Summary` (1–3 sentences on what the change does), `## Findings`
(one `- ` bullet per finding, most important first, with files and symbols in
`code` spans such as `src/app.go:42`; `- None.` when there is nothing), and
`## Verdict` (approve, comment or request changes, with a one-line reason).
It is a JSON string, so every line break is written as `\n`.

Lines are 1-based and inclusive; `"newRange"` names lines of the new version,
`"oldRange"` a removed line. `"meta"` is optional and free-form (string
values) on the document, a file and a note; gg shows every key as `key: value`.
gg stores the document as ONE review note on the reviewed commit and opens it
as a review view (the overview, the files, your notes at their lines). A reply
that is not the document is stored as text with a warning.

## Caveats

- If the same path carries both a STAGED and an UNSTAGED note, a bare
  `gg note list` (no `--file`) resolves both against their own diff and can
  print a misleading active/stale verdict for either — pass
  `gg note list --file <path>` with `--cached` (staged) or without (unstaged)
  to resolve one note against the diff it actually anchors to.
- In a batch error (`gg note apply --stdin`, `gg_notes_apply`), the item
  index in `item N` is 0-based: `item 0` is the first entry in the batch's
  array, not the first-numbered one.
- `gg note clear --all` removes every note this CHECKOUT can see: this
  worktree's own working-tree notes (unstaged/staged/untracked), PLUS every
  commit note in the whole store — commit notes are not worktree-scoped,
  since the commit's content is the same everywhere. A sibling worktree's
  working-tree notes are untouched; its commit notes are gone too.

## Common errors

- `a note anchors to one commit; pass the tip commit` — you passed a range to
  `--rev`. Use the tip commit's sha.
- `--cached and --rev are mutually exclusive` — pick one target.
- `<file> has K hunks` — the `--hunk N` you passed is past the end; re-read
  `gg diff --hunks`.
- `notes: the new side of <path> does not exist` — the file is not in that
  target's new side (wrong `--cached`/`--rev`, or a deleted file — use
  `--old-line`).
- `note reply: no note <id> in the store of <checkout>` — note ids are per
  repository: you are in the wrong checkout (put its repository link first),
  or the note is gone — removed, swept, or mistyped; list again.
- `the review is not a gg review document, so it has no notes to import` —
  `gg review --notes` got prose, not the review document.
