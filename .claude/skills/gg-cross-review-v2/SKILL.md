---
name: gg-cross-review-v2
description: Experimental v2 of gg-cross-review — 2–3 copies of yourself on different models review a gg:// link, you settle their disagreements and store one merged review.
argument-hint: "<gg-link> [2|3] [what to focus on]"
disable-model-invocation: true
---

<!-- gg:gg-cross-review-v2:v1 -->

# /gg-cross-review-v2 — several models review, you settle it (experimental)

This is the EXPERIMENTAL second version of /gg-cross-review, installed by
`gg init --experimental` so the user can compare the two. Follow THIS file,
not gg-cross-review. It differs in step 6 (you read the intent, the before
and the callers when you rule, and you check the contracts yourself), in the
merged `## Summary` and in step 7 (the overview follows gg-overview-v2). The
reviewers get the same brief as in version 1.

The user typed `/gg-cross-review-v2 <gg-link> [2|3] [focus]`: the first word
after the command is a `gg://` link, then optionally how many reviewers (2
or 3), then optionally what they want looked at. Run that many copies of
yourself on DIFFERENT models over the change the link names, merge their
reviews, investigate every point they disagree on yourself, and store ONE
review in gg. Do NOT launch gg's TUI or `gg web`; they belong to the user.

**Wording.** Write every text the user reads — the merged summary, the
remarks, your rulings, your reply — in ASD-STE100 (80% compatibility when possible):
short sentences, active voice, one meaning per word, no idioms. Code,
paths, commands and identifiers stay exactly as they are. Tell each
reviewer the same rule in its prompt.

## Steps

1. **What is reviewed.** `gg review save <link> --dry-run --json` prints
   `kind`, `label`, `checkout`, `diff` and `hunks`. A refusal ("names no
   change") means the link is not a change — ask the user for a commit,
   pair, merge preview, branch or working-tree link. A file or line in the
   link narrows nothing: the whole change is reviewed.
2. **How many.** The number after the link. Missing, or not 2 or 3 → ask:
   "2 or 3 reviewers?" — accept only 2 or 3.
3. **Which models.** List the models you know are valid for YOUR OWN CLI —
   e.g. Claude Code: `opus`, `sonnet`, `haiku` (say which one you run on);
   Codex, Junie, Kimi, Antigravity: the models your configuration offers
   (Antigravity: `agy models` lists them). Ask the user to pick that many
   DIFFERENT ones; accept names they type. Never pick for them.
4. **Your own review tool.** `gg review --tools --json` lists the review
   tools: `name`, `agent` (`claude`, `codex`, `junie`, `kimi`,
   `antigravity`, `""` for a custom command), `mode`, `model`. Take the one
   with `mode: "capture"`, `agent` = you and `model: true`. More than one →
   ask the user which to use, naming them; every reviewer runs that one.
   None → tell the user to add your headless review tool (gg Settings →
   External tools, category review) and stop — or, when a tool of yours
   says `model: false`, that its command must take `<model:FLAG>` (its
   `gg review --model` error says where).
5. **Run the reviewers in parallel.** Make a scratch directory
   (`mktemp -d`) and start one command per model — reviewer 1, 2, 3 — each
   as a background task (Claude Code: run it in the background; a shell:
   `&`, then `wait`). Quote the model (names like `sonnet[1m]` are shell
   globs) and name the files by NUMBER, not by model:

       gg review --tool "<tool>" --model "<model>" --link "<link>" --no-save --json > <dir>/r1.json 2> <dir>/r1.err

   When the user gave a focus, every reviewer gets it: add
   `--focus '<the focus, word for word>'` before `--link` (in single quotes;
   a `'` inside becomes `'\''`). gg puts it in each reviewer's brief.
   One review takes minutes: never make a blocking call your client will
   time out. Wait until all have finished. A non-zero exit is a failed
   reviewer — keep its `.err` for the summary. Remember which number ran
   which model. None succeeded → report the
   errors and stop. Each `.json` is a review document (the shape is in the
   reviewing-with-gg skill's "Review document" section).
6. **Merge** them into ONE review document — see **Merging**.
7. **Overview (when asked).** Only when the user's request says
   "overview", "tour" or "walk me through": add an `"overview"` string to
   the MERGED document — written by YOU, never by the reviewers. Write it
   by the gg-overview-v2 skill's "What the overview says" — its sections,
   scaled to the change (`gg skill path gg-overview-v2` prints where the
   skill is). For a review: lines of the NEW version at the reviewed tip,
   paths repo-relative in slash form, no `note:` anchors, ≤ 100 anchors,
   ≤ 64 KiB. The summary stays the review — the verdict and the findings
   go there; the overview only explains. Otherwise leave the key out.
8. **Store it:** `gg review save <link> --agent "<you> — cross-review (<model1>, <model2>[, <model3>])" --stdin --json`
   with the merged document on stdin. A "not a gg review document" error
   names what is wrong — fix the JSON and run it again. `gg review save`
   prints `unresolved: <dest>` for every overview anchor whose file is not
   in the reviewed change or whose lines do not exist at the tip — fix the
   text and save again.
9. **Show it.** `gg session navigate <link from step 8>` opens the review in
   the gg window open on the link's worktree. Exit 1 with `no gg session for
   this worktree` means none is open: skip this step.
10. **Reply** with the review link, the verdict first, each reviewer and its
   verdict, and how many findings were agreed, how many disputed and how you
   ruled on them. Do not paste the whole review — it is in gg.

## Merging

Before you rule on anything, collect these facts in `checkout`:

- **The intent.** `gg log -n 50 <diff>` lists the commit subjects (not for
  `kind` = `working`); for a pull request, `gg pr view <n>` prints its
  description.
- **The before.** The removed lines are in `gg diff <diff> -- <file>`. For
  old lines outside a hunk: `gg link --pair <diff> <path>:old:<a>-<b>`
  (`working`: `gg link --ref HEAD <path>:<a>-<b>`), then `gg link text
  <that link>`.
- **The callers.** Search `checkout` for each changed name; find where
  execution enters the changed code from code the change did not touch.
- **Mechanical files.** `gg diff --stat <diff>` shows a rename as `old =>
  new`; note renames, formatting-only edits and generated files. Drop a
  reviewer's remark on them unless it names a real defect.

Read every reviewer's `summary` and `files[].annotations[]`. Two remarks are
the SAME issue when their lines overlap (or are a few lines apart in the same
function) and they describe the same problem.

- **Agreement** — reviewers raise the same issue: ONE remark. Its
  `meta.raised_by` lists the models (`"opus, sonnet"`), its `kind` and
  `severity` are the most serious any of them gave (issue > suggestion >
  nitpick; high > medium > low), its rationale the clearest one (combine
  when they add different evidence). Do not re-investigate agreed points.
- **Discrepancy** — any of:
  - an issue only ONE reviewer raised;
  - reviewers contradicting each other on the same code ("bug" vs "fine");
  - kinds that disagree (issue vs good) or severities more than one step
    apart (high vs low);
  - different verdicts.

  For each, **investigate it yourself**: read the code and its callers, run
  read-only checks (the tests, `gg diff`, grep) in `checkout`. Then rule:
  - **valid** → keep it as a remark; the rationale carries YOUR evidence;
    `meta.raised_by` = who raised it, `meta.disputed_by` = who disagreed
    (when someone did), `meta.ruling` = `"confirmed"`;
  - **not valid** → no remark; list it under `## Disagreements resolved`
    with why it does not hold;
  - **undecidable** (needs runtime data, the intent is unclear) → keep it as
    a `kind: question` remark, `meta.ruling` = `"open"`, the rationale saying
    what would settle it.

- **Contracts** — check them YOURSELF after the merge, whatever the
  reviewers said: every change to what other code, users or stored data
  depend on — a CLI flag, a config key, a stored format, the `gg://`
  grammar, a wire shape, a skill text — and what happens to old data and
  old callers. A breaking change without a migration is a finding; when no
  reviewer raised it, `meta.raised_by` = your own name.

  Every remark's `meta` uses reviewing-with-gg's "Meta and display"
  vocabulary (`kind`, `severity`, `intent`, `confidence`), and the merged
  document carries its default `"display"` block. `raised_by`,
  `disputed_by` and `ruling` are stored and listed, not shown, unless you
  declare them. An agent double-checking a stored review later writes
  `gg note meta <review:id:n> set status=agreed|disagreed|deferred` — a
  glance mark: it never resolves, sends or blocks anything.

The merged document's `summary` (markdown, line breaks as `\n`):

- `## Summary` — before → after in 1–3 sentences: what the code did, what
  it does now. When there are mechanical files, end with one line
  `Mechanical: <files>`.
- `## Findings` — one bullet per kept remark, most important first, the
  user's focus first when they gave one; `- None.` when nothing is left.
- `## Disagreements resolved` — each disputed point: what each model said,
  your ruling and its evidence; `- None.` when they agreed on everything.
- `## Reviewers` — each model and its verdict; a failed reviewer and why.
- `## Verdict` — YOURS (approve, comment or request changes), with a
  one-line reason; it may differ from every reviewer's.

The document's `meta`: `verdict`, `reviewers` (`"opus, sonnet"`), `mode` =
`"cross-review"`. Every `meta` value is a string.

A question about ONE file or line is not a review: answer it, and leave a
note with `gg note add` if a note helps (reviewing-with-gg skill).
