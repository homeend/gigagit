---
name: writing-e2e-scenarios
description: Use when adding or modifying e2e test scenarios in e2e/scenarios/ — gigagit's declarative TOML tests that build a real repo, run gg commands, and assert user-visible state — including TUI scenarios that drive the real TUI and compare golden screens. Covers the schema, the operation contracts, the TUI golden-screen workflow, and the mistakes that produce wrong expectations.
---

# Writing gg e2e scenarios

One scenario = one `e2e/scenarios/<name>.toml`. The harness builds a real
git repo from `[input]`, runs each `[[run]]` via the in-process CLI, and
verifies `[expect]`. Run one: `go test ./e2e -run 'TestScenarios/<name>' -v`.

**The golden rule: expectations are derived from the operation's contract
below — NEVER from running the scenario and copying what happened.** If the
result surprises you, either your mental model or the engine is wrong;
investigate before adjusting the expectation. (The corpus has already caught
one real engine bug this way — worktree remove's relative-path matching.)

## Schema

(full design: `docs/superpowers/specs/2026-06-12-e2e-harness-design.md`)

| Section | Purpose |
|---|---|
| `[input] steps` | build the local repo: `{write,content}`, `{rm}`, `{commit}` (= `git add -A` + commit), `{branch}` (create, stay), `{switch}`, `{stash}` (setup stash, includes untracked via -u), `{worktree,branch}` (branch must exist), `{git_config,value}` (runs `git config <git_config> <value>` in the step's cwd — e.g. narrowing `remote.origin.fetch` to simulate a single-branch clone); any step takes `cwd` (sandbox-root-relative) |
| `[input.origin]` | upstream repo: `steps` (pre-clone; needs ≥1 commit), `after` (post-clone divergence), `transport` http (default, real HTTP server) / path. Local repo = clone of origin |
| `[[run]]` | `cmd` (gg argv, FLAGS BEFORE POSITIONALS), `exit` (required), `cwd`, `stdin` (multi-line TOML string fed to the command's stdin; default `""` = the empty reader; primarily for `gg batch`'s script-on-stdin); `{{cwd}}` in any `cmd` argument expands to that run's own working directory in git slash form (the only substitution the harness performs — for scenarios that must name an absolute path, e.g. a `gg://` link's local form) |
| `[expect]` | `branch`, `branches` (exact set), `clean`, `ahead`/`behind` (need origin), `in_progress` none/rebase/merge, `stashes` (count), `worktrees` (sandbox-root-relative), `[expect.files]`, `[expect.status]` staged/unstaged/untracked/conflicted, `[[expect.stash]]` `contains` (newest first), `[[expect.log]]` subjects newest-first (string or `{matches="re"}`), optional `branch` (default HEAD), `[expect.worktree."path"]` files/status, `[expect.origin]` branches/log |

File expectation forms: `"literal\n"` · `{ sha256 = "…" }` · `{ unchanged = true }` (vs end-of-input snapshot; main repo scope only) · `{ absent = true }`.

Harness facts: `.gg.toml` is injected before your steps (worktree templates:
branch `wt-<parent-branch>`, path `../wt/<branch>`) and is committed by the
first `commit` step — at least one commit step is required (in `origin.steps`
when an origin exists, else in `input.steps`). Identity and dates are frozen;
`origin.after` commits get LATER dates than `input.steps` commits (matters
for `git log` order after merges). stdin is not a TTY.

## Operation contracts (where you end up, what gets stashed)

| Command | Contract |
|---|---|
| `switch <b>` | Autostash fires only when TRACKED changes exist (untracked never stash — they travel natively). On success the stash is POPPED: `stashes = 0`, changes present on `<b>`. Pop conflict: exit 1, still ON `<b>`, stash retained, files conflicted. |
| `pull` (current branch) | fetch → ff-only. Divergence → decision `non-fast-forward`, answer with `--on-conflict=rebase|merge|abort`. rebase: linear, local on top, `ahead=1`. merge: `{matches="^Merge"}` first, then upstream-then-local (later dates sort first), `ahead=2`. abort: EXIT 0, history untouched, `ahead=1 behind=1`. No flag: exit 1, repo untouched. Mid-rebase conflict: exit 1, `in_progress="rebase"`, don't assert `branch` (detached HEAD during rebase). |
| `pull <other-branch>` | ENDS ON `<other-branch>` (PullAndStay) — the #1 authoring mistake. Dirty tracked files are stashed and popped on the target: make the dirty file disjoint from upstream's changes or you authored a pop-conflict scenario. If `<other-branch>` lives in a linked worktree, the pull happens THERE and you stay on your branch. |
| `pull --background <b>` | Updates the local ref only; current branch and dirty files untouched. Combining with `--on-conflict` is a usage error (exit 2). Not-ff-able → decision `not-fast-forwardable`. |
| `push` | Pushes the CURRENT branch to origin with `-u`. Assert via `[expect.origin]` log + `ahead = 0`. |
| `commit -m … --all` | `-a` stages tracked modifications only; untracked files stay. Nothing to commit → exit 1. |
| `stash -m …` | NO `-u`: tracked changes only; untracked files remain in the tree. |
| `undo` | Soft-resets the last commit; its changes end up STAGED; file contents keep the newer version. |
| `worktree add [start]` | Positional = START POINT (not the branch name!). Branch/path come from the pinned templates: `worktree add main` → branch `wt-main` at sandbox path `wt/wt-main`. |
| `worktree add --branch <b>` | Checks out the EXISTING branch `<b>` verbatim (no new branch, branch template bypassed); path still templated: sandbox path `wt/<b>`. You STAY on your current branch. Missing local branch (no remote-DWIM) or branch checked out anywhere → exit 1; `--branch` + positional start-point → exit 2. |
| `branch create <name> [<start>]` | Creates a local branch, NEVER switches. Start defaults to HEAD. Existing name (git refuses) or illegal name (fast-fail validation) → exit 1. No decisions. |
| `branch delete [--force] <name>` | The confirm decision is pre-answered by the CLI — a merged branch deletes with no flags and no TTY. Unmerged → `branch-unmerged` fork: unanswered (no `--force`, no TTY) → exit 1 and the branch SURVIVES; `--force` pre-answers force-delete. Guards (exit 1, nothing happens): the checked-out branch, a branch checked out in any worktree. |
| `merge [--into <t>] [--on-conflict=keep\|abort] <s>` | Merges `<s>` into `<t>` (default: current). Ends on `<t>` ONLY when `<t>` wasn't checked out anywhere (rung 3); if `<t>` lives in a linked worktree the merge lands THERE and you stay. Conflict → `merge-conflict` fork `["keep-conflicts","abort"]`: `abort` = exit 0 + tree restored; `keep` = exit 1 + `in_progress="merge"` + conflicted files; unanswered (no flag, no TTY) = exit 1 with the conflict LEFT IN PLACE (the decision fires after the conflict exists). Guards (same branch, missing branch, detached HEAD without `--into`) → exit 1, nothing happens. |
| `worktree remove <path>` | Path matched literally, cwd-absolute, or repo-top-relative; flags first: `--with-branch` deletes the branch (`remove-scope`), `--force` answers `worktree-dirty` + `branch-unmerged`. Clean+merged needs no `--force`. |

Exit codes: success/chosen-abort = 0 · failed op / unanswered decision = 1 ·
usage error = 2.

## Checklist before committing a scenario

1. Flags before positionals in every `cmd`.
2. Origin scenarios: divergence comes from `origin.after`; at least one commit in `origin.steps`.
3. Every decision the run will hit is answered by a flag — there is no TTY.
4. Re-read the contract row for your command: where does it END? what is
   stashed? what is the exit code for *your* path through the decision tree?
5. `go test ./e2e -run 'TestScenarios/<your-file>' -v` passes.

## TUI scenarios (golden screens)

(design: `docs/superpowers/specs/2026-10-01-tui-e2e-golden-screens-design.md`)

A `[tui]` block drives the real TUI (`tui.Headless`: the real `Model` in a
deterministic in-process loop, drawn through an `x/vt` terminal) after the
`[[run]]`s, and compares screens with golden files. `[[run]]` is optional
in a TUI scenario; `exit` stays required on every run.

```toml
[tui]
size = "160x40"                  # COLSxROWS, default 160x40

[[tui.step]]
name = "commits"                 # checkpoint → <scenario>.screens/01-commits.txt
keys = ["right"]                 # keyscript tokens (gg --record writes these)
wait = false                     # true: fire the parked timers once (a "pause")
screen_contains = ["✎"]          # checked on every OS
screen_excludes = ["@notes/"]
```

- **Keys** are the recorder's tokens: `enter esc space tab up down left
  right bspace delete home end pgup pgdown`, `C-x`, `M-x` (alt), or a
  literal (`.`, `foo` = one press per rune). A step without `name` only
  moves; a named step's whole screen is compared.
- **Settling has no clock.** Each key's commands run to a fixed point;
  timers (notice expiry, debounces, the heartbeat) are PARKED, so a plain
  step shows the screen right after the keys (a sticky notice is still
  there). Use `wait = true` for a screen that appears after a debounce.
- **Non-git state** comes from `[[run]]`s through the real CLI: `gg note
  add …`, and `gg review --tool fake …` with the fake agent declared in the
  scenario's own `.gg.toml` (an `[input]` write, as in `tui_review_mixed`):
  `command = "{{ggfake}} review"` prints `e2e/fixtures/review.json`.
- **Write goldens with** `go test ./e2e -run 'TestScenarios/<name>' -update`,
  then **READ every new or changed golden in full** before committing — a
  golden is an expectation; copying a wrong screen pins the bug. A mismatch
  writes `<golden>.actual` beside it (gitignored); a golden no checkpoint
  produces fails as stale (`-update` deletes it).
- **Determinism is the harness's job:** frozen `internal/clock` (ages,
  dates), one fixed date for gg-made commits, `time.Local = UTC`, a fixed
  sandbox root shown as `{{root}}`, the fake agent on PATH. If two
  `-update` runs give different goldens, find the varying input — never
  loosen the comparison.
- **Prove it bites:** with the fix under test removed (a reversible edit —
  never `git checkout` a file holding uncommitted work), the scenario must
  fail.
- **Cannot be checkpointed:** AI-task surfaces (shared task history), the
  tool-update notice (agents are never probed), agent consoles, terminal
  handovers (an editor or merge tool — the step fails), mouse input.
- Golden bytes are not compared on Windows (paths differ);
  `screen_contains`/`screen_excludes` run everywhere.

**When to write one**
- **A bug seen in a real repo** (e.g. test-1): find a scenario with that
  repo's shape, or extend the closest one, or add one; reproduce the bug as
  a failing screen first; then fix.
- **A feature with a TUI surface** ships at least one scenario with golden
  screens of that surface.
- **Mix features the way users do:** features that meet on one screen
  (reviews, notes, branch markers on one Commits list) belong in ONE
  scenario, so their interaction is pinned. Separate repo definitions are
  the default; share one only when a shape is copied a third time.

