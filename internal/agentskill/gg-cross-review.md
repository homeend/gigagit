# /gg-cross-review — several models review, you settle it

The user typed `/gg-cross-review <gg-link> [2|3] [focus]`: the first word
after the command is a `gg://` link, then optionally how many reviewers (2
or 3), then optionally what they want looked at. Run that many copies of
yourself on DIFFERENT models over the change the link names, merge their
reviews, investigate every point they disagree on yourself, and store ONE
review in gg. Do NOT launch gg's TUI or `gg web`; they belong to the user.

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
   with `mode: "capture"`, `agent` = you and `model: true`. None → tell the
   user to add your headless review tool (gg Settings → External tools,
   category review) and stop.
5. **Run the reviewers in parallel.** Make a scratch directory
   (`mktemp -d`) and start one command per model, each as a background task
   (Claude Code: run it in the background; a shell: `&`, then `wait`):

       gg review --tool <tool> --model <model> --link <link> --no-save --json > <dir>/<model>.json 2> <dir>/<model>.err

   One review takes minutes: never make a blocking call your client will
   time out. Wait until all have finished. A non-zero exit is a failed
   reviewer — keep its `.err` for the summary. None succeeded → report the
   errors and stop. Each `.json` is a review document (the shape is in the
   reviewing-with-gg skill's "Review document" section).
6. **Merge** them into ONE review document — see **Merging**.
7. **Store it:** `gg review save <link> --agent "<you> — cross-review (<model1>, <model2>[, <model3>])" --stdin --json`
   with the merged document on stdin. A "not a gg review document" error
   names what is wrong — fix the JSON and run it again.
8. **Show it.** If `gg session list` shows a live gg window for this
   worktree, open the review there: `gg session navigate <link from step 7>`.
9. **Reply** with the review link, the verdict first, each reviewer and its
   verdict, and how many findings were agreed, how many disputed and how you
   ruled on them. Do not paste the whole review — it is in gg.

## Merging

Read every reviewer's `summary` and `files[].annotations[]`. Two remarks are
the SAME issue when their lines overlap (or are a few lines apart in the same
function) and they describe the same problem.

- **Agreement** — reviewers raise the same issue: ONE remark. Its
  `meta.raised_by` lists the models (`"opus, sonnet"`), its severity is the
  highest any of them gave, its rationale the clearest one (combine when
  they add different evidence). Do not re-investigate agreed points.
- **Discrepancy** — any of:
  - an issue only ONE reviewer raised;
  - reviewers contradicting each other on the same code ("bug" vs "fine");
  - severities more than one step apart (bug > risk > design > nit);
  - different verdicts.

  For each, **investigate it yourself**: read the code and its callers, run
  read-only checks (the tests, `gg diff`, grep) in `checkout`. Then rule:
  - **valid** → keep it as a remark; the rationale carries YOUR evidence;
    `meta.raised_by` = who raised it, `meta.disputed_by` = who disagreed
    (when someone did), `meta.ruling` = `"confirmed"`;
  - **not valid** → no remark; list it under `## Disagreements resolved`
    with why it does not hold;
  - **undecidable** (needs runtime data, the intent is unclear) → keep it as
    a `risk` remark, `meta.ruling` = `"open"`, the rationale saying what
    would settle it.

The merged document's `summary` (markdown, line breaks as `\n`):

- `## Summary` — what the change does, 1–3 sentences.
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
