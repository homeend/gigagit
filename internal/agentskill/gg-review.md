# /gg-review — a full review stored in gg

The user typed `/gg-review <gg-link> [focus]`: the first word after the
command is a `gg://` link, the rest (optional) is what they want you to look
at. Review the change the link names and store ONE review — an overview plus
per-file remarks — in gg, where the user reads it beside the code. Do NOT
launch gg's TUI or `gg web`; they belong to the user.

## Steps

1. **What is reviewed.** `gg review save <link> --dry-run --json` prints
   `kind`, `label` and `diff`. A file or line in the link does not narrow the
   review: the whole change is reviewed. A refusal ("names no change") means
   the link is not a change — ask the user for a commit, pair, merge preview,
   branch or working-tree link.
2. **Read it** with the `diff` value — never guess the range:
   `gg diff --stat <diff>`, then `gg diff --hunks --json <diff>`, then
   `gg diff <diff> -- <file>` for each file that matters. For `kind` =
   `working`, also run `gg status`: untracked files are part of the review.
3. **Write the review document** — exactly the shape in the
   reviewing-with-gg skill's "Review document" section: `summary` is markdown
   with `## Summary`, `## Findings` (most important first), `## Verdict`;
   `files[].annotations[]` are the remarks, `newRange` lines of the new
   version (`oldRange` for a removed line), `meta.severity` = bug | risk |
   design | nit. Put the user's focus first in Findings when they gave one.
4. **Store it:** `gg review save <link> --agent "<your name, e.g. Claude Code>" --stdin --json`
   with the document on stdin. A "not a gg review document" error names what
   is wrong — fix the JSON and run it again.
5. **Show it.** If `gg session list` shows a live gg window for this
   worktree, open the review there: `gg session navigate <link from step 4>`.
6. **Reply** with the review link and a three-line summary (verdict first).
   Do not paste the whole review into the chat — it is in gg.

A question about ONE file or line ("what does this function do?", "is this
safe?") is not a review: answer it, and leave a note with `gg note add` if a
note helps (reviewing-with-gg skill).
