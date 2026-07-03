# Agent-friendly gg CLI: missing read/stage verbs + batch mode

**Date:** 2026-07-03
**Status:** approved design
**Arc:** 2 stages, one feature branch each
- Stage 1 (`feat/agent-cli-verbs`): `gg log`, `gg diff`, `gg show`, `gg add`/`gg unstage`,
  `gg branch current`, `gg branch ls`, `gg worktree prune`, commit-sha-in-summary.
- Stage 2 (`feat/gg-batch`, branched after stage 1 merges): `gg batch`.

## Motivation

Analysis of 1,430 real git invocations by agent sessions in this repo
(rtk history, 2026-06-24 → 2026-07-02) shows gg cannot yet replace raw git
for agent usage. The gaps, by observed volume:

| git usage | runs | gg today |
|---|---|---|
| `git log --oneline -N`, `-1 --format=%H %s`, ranges | ~260 | none |
| `git branch --show-current` | 172 | only via full `gg status` |
| `git add` (paths or `-A`) | 163 | none — `commit -a` stages tracked only, so gg **cannot commit a new file** |
| `git diff` (`--stat`, `--cached`, ranges, paths) | 146 | only endpoint-compare (`gg compare`) |
| `git show <sha> --stat`, `show <sha> -- <file>` | 45 | none |
| `git worktree prune` | 6 | none |
| `git branch` (list) | 9 | none (CLI has only create/rename/delete) |

Additionally, agents chain 4–6 commands per step with `echo '---'` separators;
each pays process start + repo discovery + config load. A batch mode removes
that overhead and gives sectioned, parseable output.

## Decisions

1. **Terse-first output** (user-approved): custom compact formats optimized for
   tokens, documented in `using-gg.md`, rather than mirroring git's padded
   output. gg output should be born-terse so no downstream filtering is needed.
2. **2-stage arc** (user-approved): verbs first, batch second. Batch is only
   useful once the commands it batches exist.
3. **Batch lives in the CLI layer** (approach A, user-approved): a loop over an
   extracted `runOne` dispatch sharing one `domain.Service`. Rejected: an
   engine-level composite op (ops have different lock modes; one reservation is
   either over-locked or wrong) and a daemon/serve mode (that is the future MCP
   milestone). Batch is sequential by design; parallelism stays where it
   already lives (domain's coalesced reads).
4. **Writes are allowed in batch** under the existing non-interactive decision
   contract (decision printed to stderr, exit 1, never blocks). The observed
   usage clusters (`add` → `commit` → `status` → `log -1`) are exactly
   read/write mixes.

## Stage 1 — command surface

All read verbs follow the house pattern: thin `internal/git` verb (one git
invocation via `gitcmd`) → `domain` query under a Read reservation with
`NoteFailure` on error → CLI subcommand. Writes go through engine ops via
`domain.Execute`. `internal/cli` never imports `internal/git` (archtest).

### `gg log [-n N] [<rev>|<A..B>]`

- New verb `git.LogLines(ctx, rev string, n int) ([]model.LogLine, error)`:
  `git log --format=%h%x00%s -n N <rev>` (NUL-separated to survive any
  subject). New domain query `Log(ctx, rev, n)`.
- Output: `<short-sha> <subject>` per line. Nothing else.
- `-n` defaults to 10. `<rev>` defaults to HEAD; a range string (`main..HEAD`)
  passes through verbatim.
- Out of scope: decorations, graph, `--all`, `--author`/path filters.

### `gg diff [--stat|--name-only] [--cached] [<A..B> | <commit>] [-- <paths>]`

- New verb `git.DiffRaw(ctx, opts DiffOpts) ([]byte, error)` where `DiffOpts`
  carries mode (patch | numstat | name-only), cached, rev/range, paths. One
  invocation per call; modes map to `git diff [--numstat|--name-only]`.
- Default mode = full patch (asking for a diff means wanting content).
- `--stat` renders numstat as `path +A -D` lines plus a `N files +A -D`
  trailer; binary files render `path bin`.
- `--name-only` prints bare paths.
- An empty diff prints nothing and exits 0 (no trailer) in every mode.
- With no rev: working-tree diff; `--cached`: index vs HEAD; one commit:
  that commit vs its parent is NOT implied — a single `<commit>` arg means
  `diff <commit>` against the working tree, exactly like git. Ranges
  (`A..B`, `A...B`) pass through.

### `gg show <commit> [--patch] [-- <file>]`

- Default: header line `<short-sha> <subject>` then the commit's terse stat
  (`path +A -D` lines, `N files +A -D` trailer) via
  `git show --format=%h%x00%s --numstat <commit>`.
- `--patch`: full patch via `git show --format= --patch` (not `format-patch`,
  so merge commits are safe — a merge shows its combined diff instead of
  triggering the format-patch wrong-commit hazard).
- `-- <file>` scopes stat or patch to that file.
- Out of scope: `git show sha:path` content-at-rev (compare/bookmark cover it).

### `gg add (-A | <path>...)` and `gg unstage <path>...`

- Wires the **existing** `engine.Stage{Paths, Unstage}` op via
  `domain.Execute`.
- `-A` adds `All bool` to the op, backed by a new one-line verb
  `git.StageAll(ctx)` (`git add -A`). Mutually exclusive with paths (usage
  error 2 if both or neither given).
- `gg unstage <path>...` is included because the op already implements it and
  it is the natural recovery from an over-eager `add -A`.
- This closes the functional hole: gg becomes able to commit new files.

### `gg branch current`

- Prints the branch name only (one line). Uses existing
  `domain.CurrentBranch`. On detached HEAD `CurrentBranch` returns "" — fall
  back to HEAD's short sha (`git rev-parse --short HEAD`, via a trivial verb
  or an option on the existing `RevParse`).

### `gg branch ls`

- Existing `domain.Branches` query. One line per branch:
  `* <name> ↑a ↓b` — star only on HEAD's branch, counts only when an
  upstream exists.
- Out of scope: `--merged` (3 observed uses).

### `gg worktree prune`

- New tiny engine op `PruneWorktrees` (`git worktree prune` via a new verb),
  admin cleanup of stale worktree records. LockMode: TreeWrite is safe and
  simple (default); it does not touch refs or tracked files but may delete
  `.git/worktrees/*` admin dirs.
- CLI: `gg worktree prune`, prints the op summary.

### Commit prints its sha

- `engine.Commit` reads HEAD's short sha + subject after a successful commit
  (one extra cheap invocation inside the op) and sets
  `Summary: "committed <short-sha> <subject>"` (amend:
  `"amended <short-sha> <subject>"`). Kills the ~70 reflexive
  `git log --oneline -1` follow-ups; the TUI status line benefits for free.

## Stage 2 — `gg batch`

### Input

- `gg batch [--keep-going]`; commands from stdin, one per line.
- Blank lines and lines starting with `#` are skipped.
- A small pure tokenizer (own file, table-driven tests) handles single and
  double quotes so `commit -m "multi word"` works. No pipes, env vars, globs,
  redirection, or line continuations — batch is not a shell.
- Each line is a gg command **without** the `gg` prefix (`status`, `log -3`).
  A leading `gg ` token is tolerated and stripped (agents will paste it).
- `batch` inside batch is rejected (usage error for that section).

### Execution

- Refactor: extract the body of `cli.Run`'s dispatch switch into
  `runOne(svc *domain.Service, cmd string, args []string, stdin io.Reader,
  stdout, stderr io.Writer) int`. `Run` becomes: build svc once, dispatch
  once. `batch` loops over parsed lines calling `runOne` with the shared svc —
  one process start, one repo discovery, one config load for N commands.
- Sub-commands receive an **empty stdin**, so anything that would prompt takes
  its documented non-TTY path (decision printed to stderr + exit 1, or refusal
  without `--yes`). Batch can never hang.
- Stop on first failure by default; `--keep-going` runs all lines.
- Each command still takes its own repo-gate reservation — correct ordering
  for read/write mixes, and reads keep domain's singleflight coalescing.

### Output framing

```
#1 ok status
on branch main (origin/main ↑0 ↓0)
working tree clean
#2 ok log -3
aa33389 Merge feat/git-config-explorer: ...
#3 !1 push
! error: decision needed: diverged (options: rebase, merge, abort)
#done 2 ok, 1 failed (stopped)
```

- Header per command: `#<idx> ok <cmdline>` or `#<idx> !<exit> <cmdline>`.
  The status token precedes the echoed command line so the grammar is
  unambiguous regardless of command text.
- A command's stdout prints verbatim inside its section; its stderr lines are
  prefixed `! `.
- Trailer: `#done <n> ok[, <m> failed[ (stopped)]]`.
- Exit code: 0 all ok; 1 any command failed; 2 usage/tokenizer error.
- A later `--json` flag has room in this design but is out of scope.

## Error handling

Nothing new is invented. Verbs surface git's stderr in errors; domain records
genuine failures to `errors.log` via the existing `observ` seam; the CLI
prints `error: …` and exits 1 (operation/decision) or 2 (usage). Batch adds
only the framing and the stop/continue policy.

## Testing (TDD, house rules)

- FakeRunner argv assertions for every new verb (`LogLines`, `DiffRaw`,
  `StageAll`, worktree-prune, the show invocation).
- Real-git `t.TempDir()` tests for domain queries and ops (including: `add`
  then commit of an untracked file; unstage keeps working-tree content;
  commit summary contains the new sha).
- In-process `cli.Run` tests asserting exact output bytes for each new
  subcommand (the formats are the contract).
- Table-driven tokenizer tests (quotes, empty, comment, unterminated quote →
  error) and framing-writer tests (ok/fail headers, stderr prefixing,
  trailer, stop-on-error vs keep-going).
- e2e scenarios: `log`, `diff` + `--stat`, `add` + `commit` of a new file,
  and a `batch` script mixing reads, one write, and one failing command.

## Docs (each stage before merge)

- `CHANGELOG.md`; `README.md` (CLI surface changed).
- `CLAUDE.md` package map: `cli` row (new verbs / batch), `engine` row
  (Stage.All, PruneWorktrees, Commit summary), `git` row (new verbs).
- `internal/agentskill/using-gg.md`: document every new verb and the batch
  format precisely — this is what future agent sessions run on. Bump
  `agentskill.Version`, then `gg init --update`.

## Non-goals

JSON output, parallel batch execution, daemon/serve mode (MCP milestone),
`git show sha:path`, log decorations/graph/filters, `branch --merged`,
shell features in batch scripts, unstaging hunks (TUI owns hunk-level ops).
