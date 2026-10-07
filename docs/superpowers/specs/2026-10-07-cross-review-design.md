# Cross-review — design

Date: 2026-10-07 · Branch: `feat/cross-review`

## Problem

`/gg-review <link>` gives one agent's review. One model has blind spots;
the user wants the same change reviewed by 2–3 copies of the agent on
DIFFERENT models, and one final review in which the agent that ran them
has merged the results and settled where they disagree.

## Outcome (user's words, condensed)

- A skill, `/gg-cross-review <gg-link> [2|3] [focus]`, run by Claude Code,
  Junie, Codex, … The count is a parameter; when it is missing the agent
  asks (2 or 3).
- The agent proposes the models valid for ITS OWN CLI; the user picks.
- It starts that many reviewers — instances of itself, one per model — on
  the same link, collects their reviews, merges them, **investigates every
  discrepancy itself and rules on it**, and presents one final review.

## Rulings

| Question | Ruling |
|---|---|
| Who runs the reviewers | gg's headless review lane, extended with a model (`gg review --tool <own> --model <m>`): gg already knows each CLI's headless + permission flags. |
| Where the model list comes from | The parent agent proposes from its own knowledge; the user picks (or types). gg keeps no model list. |
| What is stored | Only the merged review. Reviewers run with `--no-save`. |
| Name | `/gg-cross-review <link> [2\|3] [focus]` — a new skill beside `/gg-review`. |
| Discrepancies | The parent investigates each itself (reads code, runs read-only checks) and gives a ruling with evidence. |

## Design

### 1. `gg review` additions (CLI, `internal/cli/review.go` + domain)

1. **`--model <m>`** — run the review tool on that model.
   - Built-in agents: gg knows each agent's model flag and adds it to the
     resolved command. Verified on 2026-10-07 against the installed CLIs:
     Claude `--model <m>`, Codex `exec -m <m>` (`--model`), Junie
     `--model=<m>`, Kimi `-m <m>` (`--model`), Antigravity `--model <m>`.
     The flag lives in the exttool catalog beside each agent's templates
     (one table, one value per agent id); the value is quoted for git's
     POSIX sh like every other token (`template` package rules).
   - Custom commands: a `<model>` token in the command is filled with the
     value (a new token kind in `internal/template`, quoted like
     `<prompt>`); a command holding `<model>` never gets the flag appended.
   - A tool with neither a known agent flag nor `<model>` refuses `--model`:
     `review tool "<name>" cannot take a model — add <model> to its command`
     (exit 2). An empty `--model` is the tool's default (no flag).
   - gg does not validate model names: the CLI rejects an unknown one, and
     its error reaches the caller as the review's failure (exit 1).
2. **`--link <gg-link>`** — review exactly what `gg review save <link>`
   would (merge preview, commit pair, commit, `@ref:` branch, working
   changes), through the same resolver (`domain` link → `ReviewTarget`).
   Mutually exclusive with `--preview`, `--working` and a positional.
3. **`--no-save`** — run and print, store nothing (no review note, no
   notes import). **`--json`** (new; only with `--no-save` — refused
   otherwise, exit 2) makes the output the review document itself
   (the JSON the tool produced, unwrapped from fences / Claude's envelope,
   as the store would have parsed it); a reply that is not a review
   document is an error (exit 1) carrying the raw text, so the caller can
   tell "failed" from "reviewed".
4. **`--tools [--json]`** — list the configured review tools: `name`,
   `agent` (exttool agent id, `""` for a custom command), `mode`
   (capture | interactive | …), `model` (`true` when `--model` works).
   Only capture-mode tools can run headless; the list says so.
5. Parallel runs: each `gg review` is its own process and capture (its own
   temp `$GG_MESSAGE_FILE` / `$GG_CONTEXT_FILE`). The plan verifies that
   two concurrent `--no-save` runs on the same link neither share a temp
   file nor wait on each other (no repo-gate reservation beyond a read).

### 2. The skill — `internal/agentskill/gg-cross-review.md`

Embedded like `gg-review` (user-invoked, argument hint
`<gg-link> [2|3] [focus]`, its own version counter), installed by
`gg init` / `gg init --update` for every agent that gets `gg-review`.

Steps the skill prescribes:

1. **What is reviewed** — `gg review save <link> --dry-run --json` (as
   `/gg-review`); a link that names no change → ask for one.
2. **How many** — the `[2|3]` argument; missing → ask "2 or 3 reviewers?".
   Anything else → ask again (2 or 3 only).
3. **Which models** — list the models you know are valid for YOUR OWN CLI
   (e.g. Claude Code: `opus`, `sonnet`, `haiku`; say which is the one you
   run on) and ask the user to pick that many distinct ones; accept names
   the user types. Never pick for the user.
4. **Your own tool** — `gg review --tools --json`: the capture-mode tool
   whose `agent` is you (`claude`, `junie`, `codex`, `kimi`, `agy`) with
   `model: true`. None → tell the user to add it (Settings → External
   tools, category review) and stop.
5. **Run the reviewers in parallel** — one background command per model:
   `gg review --tool <tool> --model <m> --link <link> --no-save --json >
   <tmp>/<m>.json 2> <tmp>/<m>.err`. Wait for all (they take minutes; use
   your background-task mechanism, never a blocking call your client will
   time out). A failed reviewer (non-zero exit) is reported with its error;
   the merge goes on with the others when at least one succeeded, else
   stop and report.
6. **Merge** (§3) into ONE review document.
7. **Store** — `gg review save <link> --agent "<you> — cross-review (<m1>,
   <m2>[, <m3>])" --stdin --json`.
8. **Show** — when `gg session list` shows a live gg window for this
   worktree: `gg session navigate <link from step 7>`.
9. **Reply** — the review link; the verdict; the reviewers and their
   verdicts; how many findings were agreed, how many disputed, and how the
   disputes were ruled. Not the whole review — it is in gg.

### 3. Merge rules (the skill's core)

- **Agreement** — reviewers flag the same issue (overlapping lines, same
  problem): ONE remark; `meta.raised_by` = the models (`"opus, sonnet"`),
  severity = the highest given. Not re-investigated.
- **Discrepancy** — any of: an issue only one reviewer raised; reviewers
  contradicting each other on the same code ("bug" vs "fine"); severities
  more than one step apart; different verdicts. For each, the parent
  **investigates on its own** — reads the code and its callers, runs
  read-only checks (tests, `gg diff`, grep) — and rules:
  - **valid** → kept as a remark; rationale carries the parent's evidence;
    `meta.raised_by` the model(s) that raised it, `meta.disputed_by` those
    that disagreed, `meta.ruling: "confirmed"`;
  - **not valid** → no remark; listed under `## Disagreements resolved`
    with why it does not hold;
  - **undecidable** (needs runtime data, intent unclear) → kept as a
    `risk` remark, `meta.ruling: "open"`, rationale saying what would
    settle it.
- **Summary** markdown (the review document's `summary`):
  `## Summary`, `## Findings` (most important first; the user's focus
  first when given), `## Disagreements resolved` (each disputed point:
  what each model said, the ruling, the evidence; `- None.` when none),
  `## Reviewers` (each model and its verdict; a failed reviewer and why),
  `## Verdict` (the parent's own, with its reason).
- `meta` on the document: `verdict`, `reviewers` (`"opus, sonnet"`),
  `mode: "cross-review"`. All values strings (the document's rule).

### 4. Docs

`using-gg.md`: the four `gg review` flags (skill version bump +
`gg init --update`); `reviewing-with-gg.md`: a pointer to
`/gg-cross-review` beside `/gg-review`; README (review section); CHANGELOG;
`docs/CLAUDE-details.md` (review lane: model flag table, `--link`,
`--no-save`).

## Error handling

- Refusals are exit 2 with the reason (bad flag mix, no model support);
  tool failures exit 1 with the tool's own error on stderr.
- The skill never stores a partial merge silently: a failed reviewer is
  named in `## Reviewers`.

## Testing

- `exttool`: every built-in agent with a review capture template has a
  model flag (table test); the golden template guard is untouched (no
  template text changes).
- `template`: `<model>` resolves and is quoted; absent value = empty.
- `cli`/`domain`: `--model` appends the agent flag / fills `<model>` /
  refuses (argv asserted with a fake review tool that echoes its argv into
  `$GG_MESSAGE_FILE`); `--link` targets match `review save --dry-run` for
  each link kind; `--no-save` stores nothing (note count unchanged) and
  `--json` prints the document; a non-document reply under `--no-save
  --json` exits 1; `--tools --json` shape; two concurrent `--no-save` runs
  on one link both succeed with their own outputs.
- `agentskill`: the new skill embeds, has a version marker, is installed
  by `gg init` beside gg-review (existing install tests extended).
- e2e: one scenario — a fake capture review tool with `<model>` echoing a
  canned document per model; `gg review --tool fake --model a --link …
  --no-save --json` prints it and stores nothing.

## Out of scope

Reviewers on different VENDORS in one run (the design allows it later:
`--tool` per reviewer); a gg-side model catalog; storing the individual
reviews; TUI/web launch surfaces for a cross-review (the skill is the
surface; the stored result shows in every frontend already).
