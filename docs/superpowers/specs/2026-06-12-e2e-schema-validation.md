# E2E scenario schema validation — coverage check against real gg features

Status: validation exercise (pre-spec). Companion to the upcoming e2e harness
design spec. Goal: express the most critical gg behaviors — current smart ops
plus future merge/rebase/SmartMerge — in the proposed scenario schema, and find
where the schema breaks.

Verdict up front: **the core shape holds** (declarative input steps → CLI
commands with exit codes → semantic state expectations), but the validation
found **7 required schema extensions**, all additive. They are folded into the
scenarios below and listed in the Gap Analysis at the end.

Ground-truth notes from the engine (these shaped the scenarios):

- `SmartSwitch` autostashes, switches, then **pops** the stash — on success the
  dirty files travel to the target branch and **no stash remains**. A stash
  persists only when the pop conflicts (decision `stash-pop-conflict`, the op
  returns an error → exit 1).
- `SmartPull` on the current branch: fetch → ff-only → on divergence decision
  `non-fast-forward` {rebase, merge, abort} (CLI `--on-conflict=`).
- `SmartPull` on another branch: if that branch is checked out in a worktree,
  pull happens **in that worktree**; otherwise autostash → switch → pull
  (→ switch back when `--background` chose `checkout-and-resolve`).
- **Abort options return a normal Result, not an error → exit 0.**
- Decisions are pre-answered by CLI flags; in tests stdin is not a TTY, so any
  unanswered decision fails deterministically — scenarios must answer every
  fork they trigger via flags.

---

## Schema recap (with extensions marked ★)

```toml
name = "…"

[input]                       # local repo built deterministically (frozen clock/identity)
steps = [ … ]                 # write|rm|commit|branch|switch|stash|worktree ★(worktree step)

[input.origin]                # ★ remote topology: presence ⇒ origin built first,
steps = [ … ]                 #   local = clone of origin, [input].steps run in the clone,
after = [ … ]                 #   origin.after runs upstream AFTER the clone (divergence)

[[run]]
cmd  = ["pull", "--on-conflict=rebase"]
cwd  = "wt/feature"           # ★ optional: run inside a linked worktree
exit = 0

[expect]
branch   = "main"             # current branch
branches = ["main", "feature"]# exactly these local branches exist
clean    = true               # no staged/unstaged/untracked/conflicted files
ahead    = 0                  # ★ vs upstream (user-visible ↑↓ sync state)
behind   = 0                  # ★
in_progress = "none"          # ★ none|rebase|merge — user-visible "rebase in progress"
worktrees = ["wt/feature"]    # ★ linked worktrees that exist (paths relative to sandbox)

[expect.files]                # working tree content (checksum-compared)
"a.txt" = "literal content\n"
"b.bin" = { sha256 = "…" }
"c.txt" = { unchanged = true }   # same as at end of input phase
"d.txt" = { absent = true }

[expect.status]               # ★ fine-grained when clean=false
staged    = ["a.txt"]
unstaged  = []
untracked = ["notes.txt"]
conflicted = []

[[expect.stash]]              # one entry per stash that must exist (order: newest first)
contains = { "notes.txt" = "draft\n" }   # name/date NOT asserted

stashes = 0                   # assert total stash count (omit = don't care)

[[expect.log]]                # ★ commit subjects, newest first — user-visible history shape
branch   = "main"             # default: current branch
subjects = ["local change", "upstream change", "initial"]
# non-deterministic subjects (merge messages embed paths) use a pattern:
# subjects = [{ matches = "^Merge branch" }, "local change", "initial"]

[expect.origin]               # ★ origin-side assertions (push scenarios)
[[expect.origin.log]]
branch = "main"
subjects = ["local change", "initial"]
```

---

## Scenarios

### S1 — SmartSwitch: dirty worktree travels to target branch (hero path)

```toml
name = "switch: dirty files autostash and restore on target branch"

[input]
steps = [
  { write = "README.md", content = "hello\n" },
  { commit = "initial" },
  { branch = "feature/login" },              # created, stay on main
  { write = "notes.txt", content = "draft\n" },   # untracked, uncommitted
]

[[run]]
cmd  = ["switch", "feature/login"]
exit = 0

[expect]
branch = "feature/login"
stashes = 0                        # autostash was popped — nothing left behind
[expect.files]
"README.md" = { unchanged = true }
"notes.txt" = "draft\n"            # restored on the new branch
[expect.status]
untracked = ["notes.txt"]
```

### S2 — SmartSwitch: stash-pop conflict preserves the stash

```toml
name = "switch: pop conflict keeps changes safe in stash"

[input]
steps = [
  { write = "shared.txt", content = "base\n" },
  { commit = "initial" },
  { branch = "feature/x" },
  { switch = "feature/x" },
  { write = "shared.txt", content = "feature version\n" },
  { commit = "feature change" },
  { switch = "main" },
  { write = "shared.txt", content = "my local edit\n" },   # dirty, conflicts with feature/x
]

[[run]]
cmd  = ["switch", "feature/x"]
exit = 1                            # pop conflict surfaces as error

[expect]
branch = "feature/x"                # switch itself succeeded
[expect.status]
conflicted = ["shared.txt"]
[[expect.stash]]
contains = { "shared.txt" = "my local edit\n" }   # never dropped
```

### S3 — SmartPull: clean fast-forward

```toml
name = "pull: fast-forward when only upstream moved"

[input.origin]
steps = [
  { write = "a.txt", content = "v1\n" },
  { commit = "initial" },
]
after = [
  { write = "a.txt", content = "v2\n" },
  { commit = "upstream change" },
]

[input]
steps = []                          # local clone stays at initial

[[run]]
cmd  = ["pull"]
exit = 0

[expect]
branch = "main"
clean  = true
ahead  = 0
behind = 0
[expect.files]
"a.txt" = "v2\n"
[[expect.log]]
subjects = ["upstream change", "initial"]
```

### S4 — SmartPull: diverged, policy=rebase (linear history)

```toml
name = "pull: divergence resolved by rebase"

[input.origin]
steps = [
  { write = "a.txt", content = "v1\n" },
  { commit = "initial" },
]
after = [
  { write = "upstream.txt", content = "u\n" },
  { commit = "upstream change" },
]

[input]
steps = [
  { write = "local.txt", content = "l\n" },
  { commit = "local change" },
]

[[run]]
cmd  = ["pull", "--on-conflict=rebase"]
exit = 0

[expect]
clean  = true
ahead  = 1                          # rebased commit not pushed yet
behind = 0
in_progress = "none"
[expect.files]
"upstream.txt" = "u\n"
"local.txt"    = "l\n"
[[expect.log]]
subjects = ["local change", "upstream change", "initial"]   # linear, local on top
```

### S5 — SmartPull: diverged, policy=merge (merge commit)

```toml
name = "pull: divergence resolved by merge"
# input identical to S4

[[run]]
cmd  = ["pull", "--on-conflict=merge"]
exit = 0

[expect]
clean  = true
ahead  = 2                          # merge commit + local commit
behind = 0
[[expect.log]]
subjects = [{ matches = "^Merge" }, "local change", "upstream change", "initial"]
```

### S6 — SmartPull: diverged, policy=abort leaves repo untouched, exit 0

```toml
name = "pull: abort on divergence is a clean no-op"
# input identical to S4

[[run]]
cmd  = ["pull", "--on-conflict=abort"]
exit = 0                            # abort is a chosen outcome, not an error

[expect]
ahead  = 1
behind = 1                          # still diverged (fetch updated tracking ref)
clean  = true
[[expect.log]]
subjects = ["local change", "initial"]   # nothing rewritten
```

### S7 — SmartPull: rebase hits content conflict → rebase left in progress

```toml
name = "pull: conflicting rebase stops with conflict state"

[input.origin]
steps = [
  { write = "shared.txt", content = "base\n" },
  { commit = "initial" },
]
after = [
  { write = "shared.txt", content = "upstream\n" },
  { commit = "upstream change" },
]

[input]
steps = [
  { write = "shared.txt", content = "local\n" },
  { commit = "local change" },
]

[[run]]
cmd  = ["pull", "--on-conflict=rebase"]
exit = 1

[expect]
in_progress = "rebase"              # user must resolve or abort — visible state
[expect.status]
conflicted = ["shared.txt"]
```

### S8 — SmartPull: unanswered decision fails deterministically (harness contract)

```toml
name = "pull: divergence with no policy errors out (no TTY)"
# input identical to S4

[[run]]
cmd  = ["pull"]                     # no --on-conflict, stdin is not a TTY
exit = 1

[expect]
clean = true
[[expect.log]]
subjects = ["local change", "initial"]   # repo untouched
```

### S9 — SmartPull: dirty worktree + pull of another branch (autostash round-trip)

```toml
name = "pull other branch: stash, switch, pull, restore"

[input.origin]
steps = [
  { write = "a.txt", content = "v1\n" },
  { commit = "initial" },
  { branch = "release" },
]
after = [
  { switch = "release" },
  { write = "a.txt", content = "release v2\n" },
  { commit = "release fix" },
]

[input]
steps = [
  { write = "wip.txt", content = "wip\n" },   # dirty on main
]

[[run]]
cmd  = ["pull", "release"]          # PullAndStay: ends on release
exit = 0

[expect]
branch  = "main"                    # ← intentionally wrong? NO: PullAndStay ends on target
```

**Caught during authoring:** `PullAndStay` ends on the *target* branch — the
correct expectation is `branch = "release"`, with `wip.txt` restored there.
Left here as written evidence that hand-authored expectations need the
operation contract documented in the authoring skill. Final form:

```toml
[expect]
branch = "release"
stashes = 0
[expect.files]
"a.txt"   = "release v2\n"
"wip.txt" = "wip\n"
[expect.status]
untracked = ["wip.txt"]
```

### S10 — SmartPull --background: ref updated without leaving the branch

```toml
name = "pull --background: target ref fast-forwards, current branch untouched"

[input.origin]
steps = [
  { write = "a.txt", content = "v1\n" },
  { commit = "initial" },
  { branch = "feature/x" },
]
after = [
  { switch = "feature/x" },
  { write = "f.txt", content = "f\n" },
  { commit = "feature work" },
]

[input]
steps = [
  { write = "wip.txt", content = "untouched\n" },   # stays dirty — proves no stash dance
]

[[run]]
cmd  = ["pull", "feature/x", "--background"]
exit = 0

[expect]
branch = "main"
[expect.status]
untracked = ["wip.txt"]             # worktree never touched
[[expect.log]]
branch   = "feature/x"              # ★ log assertion on a non-current branch
subjects = ["feature work", "initial"]
```

### S11 — SmartPull: target branch lives in a linked worktree

```toml
name = "pull other branch: pulls inside its worktree"

[input.origin]
steps = [
  { write = "a.txt", content = "v1\n" },
  { commit = "initial" },
  { branch = "feature/x" },
]
after = [
  { switch = "feature/x" },
  { write = "a.txt", content = "v2\n" },
  { commit = "feature update" },
]

[input]
steps = [
  { worktree = "wt-feature", branch = "feature/x" },   # ★ worktree setup step
]

[[run]]
cmd  = ["pull", "feature/x"]
exit = 0

[expect]
branch    = "main"                  # main worktree never switched
worktrees = ["wt-feature"]
[expect.worktree."wt-feature".files]   # ★ per-worktree file scope
"a.txt" = "v2\n"
```

### S12 — Stash + Commit + Push round-trip (basic verbs)

```toml
name = "stash, commit, push: bread-and-butter flow"

[input.origin]
steps = [
  { write = "a.txt", content = "v1\n" },
  { commit = "initial" },
]

[input]
steps = [
  { write = "a.txt", content = "v2\n" },        # tracked modification
  { write = "scratch.txt", content = "tmp\n" },  # untracked
]

[[run]]
cmd  = ["stash", "-m", "wip"]
exit = 0

[[run]]
cmd  = ["commit", "-m", "empty guard", "--all"]   # nothing to commit after stash
exit = 1

[[run]]
cmd  = ["push"]
exit = 0                            # nothing new — push of up-to-date is ok

[expect]
clean = false                       # scratch.txt: stash without -u keeps untracked? → see gap G7
[[expect.stash]]
contains = { "a.txt" = "v2\n" }
[expect.origin]
[[expect.origin.log]]
subjects = ["initial"]
```

**Note:** whether `scratch.txt` (untracked) enters the stash depends on the
Stash op's use of `-u`. The scenario forces us to pin that contract — another
authoring-skill item, and the reason `[expect.status]` exists.

### S13 — UndoLastCommit: changes return as staged

```toml
name = "undo: last commit unwound, changes kept staged"

[input]
steps = [
  { write = "a.txt", content = "v1\n" },
  { commit = "initial" },
  { write = "a.txt", content = "v2\n" },
  { commit = "second" },
]

[[run]]
cmd  = ["undo"]
exit = 0

[expect]
clean = false
[expect.files]
"a.txt" = "v2\n"                    # content survives
[expect.status]
staged = ["a.txt"]
[[expect.log]]
subjects = ["initial"]              # "second" is gone from history
```

### S14 — Worktree add + remove with dirty tree (decision via flag)

```toml
name = "worktree: add, dirty it, remove --force"

[input]
steps = [
  { write = "a.txt", content = "v1\n" },
  { commit = "initial" },
]

[[run]]
cmd  = ["worktree", "add", "feature/wt-test"]
exit = 0

[[run]]
cmd  = ["worktree", "remove", "feature/wt-test", "--force", "--with-branch"]
exit = 0

[expect]
worktrees = []                      # gone again
branches  = ["main"]                # branch deleted too
```

(Worktree paths come from the worktree template config; the harness pins a
deterministic `.gg.toml` template so the path — and thus `worktrees`
assertions — are stable. The dirty-worktree variant inserts a
`{ write = …, cwd = "…" }` step between the two runs.)

### S15 — FUTURE SmartMerge: clean merge of feature into main

```toml
name = "merge: feature merges clean into main"

[input]
steps = [
  { write = "a.txt", content = "base\n" },
  { commit = "initial" },
  { branch = "feature/x" },
  { switch = "feature/x" },
  { write = "f.txt", content = "f\n" },
  { commit = "feature work" },
  { switch = "main" },
  { write = "m.txt", content = "m\n" },
  { commit = "main work" },
]

[[run]]
cmd  = ["merge", "feature/x"]       # future command
exit = 0

[expect]
branch = "main"
clean  = true
in_progress = "none"
[expect.files]
"f.txt" = "f\n"
"m.txt" = "m\n"
[[expect.log]]
subjects = [{ matches = "^Merge" }, "main work", "feature work", "initial"]
```

### S16 — FUTURE SmartMerge: conflict → decision {resolve, abort}; abort restores

```toml
name = "merge: conflicting merge aborted cleanly"

[input]
steps = [
  { write = "shared.txt", content = "base\n" },
  { commit = "initial" },
  { branch = "feature/x" },
  { switch = "feature/x" },
  { write = "shared.txt", content = "feature\n" },
  { commit = "feature change" },
  { switch = "main" },
  { write = "shared.txt", content = "main\n" },
  { commit = "main change" },
]

[[run]]
cmd  = ["merge", "feature/x", "--on-conflict=abort"]   # future flag, same decider pattern
exit = 0

[expect]
branch = "main"
clean  = true
in_progress = "none"
[expect.files]
"shared.txt" = "main\n"             # exactly as before the attempt
[[expect.log]]
subjects = ["main change", "initial"]
```

### S17 — FUTURE rebase: branch onto moved main

```toml
name = "rebase: feature replayed onto advanced main"

[input]
steps = [
  { write = "a.txt", content = "base\n" },
  { commit = "initial" },
  { branch = "feature/x" },
  { switch = "feature/x" },
  { write = "f.txt", content = "f\n" },
  { commit = "feature work" },
  { switch = "main" },
  { write = "m.txt", content = "m\n" },
  { commit = "main advance" },
  { switch = "feature/x" },
]

[[run]]
cmd  = ["rebase", "main"]           # future command
exit = 0

[expect]
branch = "feature/x"
clean  = true
[expect.files]
"f.txt" = "f\n"
"m.txt" = "m\n"
[[expect.log]]
subjects = ["feature work", "main advance", "initial"]   # linearized
```

---

## Gap analysis — schema extensions the scenarios forced

| # | Gap | Extension (all additive) | Forced by |
|---|-----|--------------------------|-----------|
| G1 | No remote topology — pull/push/ff/divergence inexpressible | `[input.origin]` `steps` (pre-clone) + `after` (post-clone upstream changes); presence implies local = clone | S3–S12 |
| G2 | No history-shape assertions — rebase vs merge results indistinguishable | `[[expect.log]]`: ordered commit **subjects** (newest first), optional `branch`, `{ matches = "…" }` for non-deterministic subjects (merge messages embed sandbox paths) | S4, S5, S10, S15, S17 |
| G3 | No sync-state assertions | `expect.ahead` / `expect.behind` (the user-visible ↑↓ numbers) | S4–S6 |
| G4 | No conflict/in-progress state | `expect.in_progress = none\|rebase\|merge`, `expect.status.conflicted` | S2, S7, S16 |
| G5 | No fine-grained index state | `[expect.status]` staged/unstaged/untracked lists | S1, S12, S13 |
| G6 | Worktrees invisible | `{ worktree = path, branch = b }` setup step, `expect.worktrees`, `[expect.worktree."path".files]`, per-run/per-step `cwd` | S11, S14 |
| G7 | Push results / origin state unverifiable | `[expect.origin]` with its own `log` (and `branches`) assertions | S12 |

Non-gaps confirmed: stash content assertions (`git stash show` / `git show
'stash@{N}:path'` — no names or dates), exit-code-only run assertions
(abort = exit 0 verified against `finish()`), decisions answered purely by
existing CLI flags, and the no-TTY rule turning unanswered decisions into
deterministic failures (S8).

Two contract ambiguities surfaced that the **authoring skill must document**
(they are exactly the mistakes an agent would make):

1. Ops that end *somewhere else* than they started (`PullAndStay` lands on the
   target branch — see S9's preserved authoring error).
2. Whether autostash/`Stash` includes untracked files (`-u`) — defines S1/S12
   expectations.

## Conclusion

The declarative-steps → commands → semantic-expectations shape survived all 17
scenarios, including both future ops. Every gap was closed by adding vocabulary
(new step kinds, new expectation keys), never by changing the model. The
schema above (recap section) is the version that goes into the design spec.
