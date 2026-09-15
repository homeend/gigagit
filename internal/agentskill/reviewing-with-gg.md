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
7. summarise your findings in the chat reply
```

Inspect before you annotate. Read the whole change first, then comment on
intent, structure, risk and follow-ups — not on every hunk.

When you changed code on a branch, review it the way the user will: open a
merge preview onto its target (`gg preview add <source> <target>`), then
annotate every change you made with `gg note add --preview <id|label> --file
<path> --new-line <n> --summary "…" --rationale "…"`. The notes are stored on
the source tip and travel with it; when you push further commits the preview
keeps showing them, marking the ones whose lines moved `outdated`. Report the
preview's name back to the user so they can open it in their gg.

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
- `review tool wrote no notes` — `gg review --notes` found neither a sidecar
  file nor a JSON report.
