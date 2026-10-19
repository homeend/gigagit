---
name: gg-review-v2
description: Experimental v2 of gg-review — review the change a gg:// link names and store the review in gg with gg review save.
argument-hint: "<gg-link> [what to focus on]"
disable-model-invocation: true
---

<!-- gg:gg-review-v2:v1 -->

# /gg-review-v2 — a full review stored in gg (experimental)

This is the EXPERIMENTAL second version of /gg-review, installed by
`gg init --experimental` so the user can compare the two. Follow THIS file, not
gg-review. It differs in step 2 (what you read before you judge), step 3
(what the summary must say) and step 4 (the overview follows gg-overview-v2).

The user typed `/gg-review-v2 <gg-link> [focus]`: the first word after the
command is a `gg://` link, the rest (optional) is what they want you to look
at. Review the change the link names and store ONE review — a summary plus
per-file remarks — in gg, where the user reads it beside the code. Do NOT
launch gg's TUI or `gg web`; they belong to the user.

**Wording.** Write every text the user reads — the summary, the remarks,
the overview, your reply — in ASD-STE100 (80% compatibility when possible):
short sentences, active voice, one meaning per word, no idioms. Code,
paths, commands and identifiers stay exactly as they are.

## Steps

1. **What is reviewed.** `gg review save <link> --dry-run --json` prints
   `kind`, `label`, `checkout`, `diff` and `hunks`. A file or line in the
   link does not narrow the review: the whole change is reviewed. A refusal
   ("names no change") means the link is not a change — ask the user for a
   commit, pair, merge preview, pull request, branch or working-tree link.
   A pull request's link (`@<base>...refs/gg/pr/<n>`, `gg link --pr <n>`)
   reviews the whole PR, and the review shows in the PR's view.
2. **Read it** in `checkout` (`cd` there first — a working link may name
   another worktree) with those values — never guess the range. Read in
   this order:
   - **The intent first.** `gg log -n 50 <diff>` lists the commit subjects
     (not for `working`: there are no commits yet); for a pull request, `gg pr view <n>` prints its description. Judge the
     change against what it is FOR.
   - **The change.** `gg diff --stat <diff>` (a rename shows as `old =>
     new`), then `gg diff --hunks --json <hunks>`, then `gg diff <diff> --
     <file>` for each file that matters. When `hunks` is empty (`kind` =
     `working`) skip `--hunks`: read `gg diff HEAD -- <file>`, whose
     new-side line numbers are the working file's, and run `gg status` —
     untracked files are part of the review.
   - **The before.** The removed lines are in the patch. For old lines
     outside a hunk: `gg link --pair <diff> <path>:old:<a>-<b>` (`kind` =
     `working`: `gg link --ref HEAD <path>:<a>-<b>`), then `gg link text
     <that link>`. You cannot call a change wrong without the
     code it replaced.
   - **The callers.** Search `checkout` for each new or changed name. Find
     where execution enters the changed code from code the change did not
     touch — a bug often sits at that boundary.
   - **Mechanical files.** Note renames, formatting-only edits and
     generated files. Do not annotate them.
3. **Write the review document** — exactly the shape in the
   reviewing-with-gg skill's "Review document" section: `summary` is markdown
   with `## Summary`, `## Findings` (most important first), `## Verdict`;
   `files[].annotations[]` are the remarks, `newRange` lines of the new
   version (`oldRange` for a removed line), each remark's `meta` uses the
   suggested vocabulary (`kind`, `severity`, `intent`, `confidence`) and
   the document carries the default `"display"` block — both in
   reviewing-with-gg's "Meta and display" section. Put the user's focus
   first in Findings when they gave one. In addition:
   - `## Summary` says **before → after** in its 1–3 sentences: what the
     code did, what it does now. When there are mechanical files, end the
     section with one line `Mechanical: <files>`.
   - `## Findings` always covers **contracts**: check every change to what
     other code, users or stored data depend on — a CLI flag, a config key,
     a stored format, the `gg://` grammar, a wire shape, a skill text — and
     what happens to old data and old callers. A breaking change without a
     migration is a finding.
4. **Overview (when asked).** Only when the user's request says
   "overview", "tour" or "walk me through" (or `/gg-review-v2 <link>
   overview …`): add an `"overview"` string to the document. Write it by
   the gg-overview-v2 skill's "What the overview says" — its sections,
   scaled to the change (`gg skill path gg-overview-v2` prints where the
   skill is). For a review: lines of the NEW version at the reviewed tip,
   paths repo-relative in slash form, no `note:` anchors, ≤ 100 anchors,
   ≤ 64 KiB. The summary stays the review — the verdict and the findings
   go there; the overview only explains. Otherwise leave the key out.
5. **Store it:** `gg review save <link> --agent "<your name, e.g. Claude Code>" --stdin --json`
   with the document on stdin. A "not a gg review document" error names what
   is wrong — fix the JSON and run it again. `gg review save` prints
   `unresolved: <dest>` for every overview anchor whose file is not in the
   reviewed change or whose lines do not exist at the tip — fix the text and
   save again.
6. **Show it.** `gg session navigate <link from step 5>` opens the review in
   the gg window open on the link's worktree. Exit 1 with `no gg session for
   this worktree` means none is open: skip this step.
7. **Reply** with the review link and a three-line summary (verdict first).
   Do not paste the whole review into the chat — it is in gg.

A question about ONE file or line ("what does this function do?", "is this
safe?") is not a review: answer it, and leave a note with `gg note add` if a
note helps (reviewing-with-gg skill).
