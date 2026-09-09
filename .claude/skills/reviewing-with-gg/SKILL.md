---
name: reviewing-with-gg
description: Use when reviewing code changes in a repository where the gg CLI is available: inspect diffs and leave anchored review notes the user reads in gg.
---

<!-- gg:reviewing-with-gg:v1 -->

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
gg note reply <note-id> --summary "…" [--rationale "…"] [--author <name>] [--json]
gg note apply --stdin [--cached | --rev <c>] [--author <name>] [--json]
gg note rm    <note-id>
gg note clear (--file <path> | --all) [--type user|agent|all] --yes
```

- `add` and `reply` print the new note id; `--json` prints the note object.
- Line numbers are 1-based. `--new-line` is the line in the NEW version of the
  file, `--old-line` in the old one; `--hunk N` covers the hunk's whole span
  (its new-side span, or its old-side span when the new side is empty — a
  deleted file).
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

## Common errors

- `a note anchors to one commit; pass the tip commit` — you passed a range to
  `--rev`. Use the tip commit's sha.
- `--cached and --rev are mutually exclusive` — pick one target.
- `<file> has K hunks` — the `--hunk N` you passed is past the end; re-read
  `gg diff --hunks`.
- `notes: the new side of <path> does not exist` — the file is not in that
  target's new side (wrong `--cached`/`--rev`, or a deleted file — use
  `--old-line`).
- `note: notes: not found` — the note id you passed is gone: removed, swept,
  or mistyped; list again.
- `review tool wrote no notes` — `gg review --notes` found neither a sidecar
  file nor a JSON report.
