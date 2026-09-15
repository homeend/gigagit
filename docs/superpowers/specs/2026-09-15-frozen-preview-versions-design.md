# Frozen preview versions + rebase drift detection — design

*Agreed 2026-09-15. Spec 2 of 2; spec 1 is "repo preflight & feature
requirements" (`2026-09-13-repo-preflight-design.md`, merged as `e442279f`),
whose migration machinery this feature is the first real consumer of.*

## Problem

Before letting an AI rebase or merge a branch, the user hand-creates a backup
branch, because a bad conflict resolution can silently change what the branch
contributes — most sharply, a file upstream deleted can come back, so the
branch now reads as *adding* a file it never added.

gg already snapshots the branch tip before every history-rewriting op
(`refs/gg/versions/…`), so the raw material exists. But a version opens as *a
commit whose tree you go exploring*, which is useless for the question actually
being asked: **what did this branch contribute, and did the operation change
it?**

Two features, one record:

1. **A version opens as the frozen PR view** — the `merge-base(other, ours)...ours`
   diff as it stood immediately before the operation. The version list becomes a
   history of change sets, not of tips.
2. **Drift detection** — after the operation, gg compares the frozen change set
   against the current one and says so when the operation itself altered which
   files the branch touches.

Together they replace the manual backup branch.

## Goals

- Every snapshot records enough to re-render the PR-style preview later, at any
  time, without guessing.
- After a rebase/merge/pull, gg reports unprompted when the change set moved.
- The user never again hand-creates a backup branch before an AI-driven rewrite.

## Non-goals

- Content-level drift *inside* files both sides touch. A rebase across upstream
  edits legitimately changes a file's base-relative patch; flagging that is
  noise. `git range-diff` remains the manual tool for that question.
- Blocking or pausing an operation on drift. Detection reports; it never decides.
- Rename detection. Every changeset diff is taken with `--no-renames` (see below).

## The record

### Shape

A snapshot ref `refs/gg/versions/<branch>/<ts>-<op>` points at a **synthetic
commit** created with `git commit-tree`:

- **tree** = `<p1>^{tree}` — the snapshotted tip's own tree, so the synthetic
  commit is an empty commit on top of it and `git log --all -p` shows no diff.
  (Verified on git 2.43; `commit-tree` accepts the `^{tree}` peel.)
- **parents** = `[p1]`, plus `Ours` when it differs from `p1`.
- **message** = a one-line subject plus a single `Gg-Meta` trailer.

**The message is the truth; parents exist only for reachability.** Parents pin
objects against `gc`; every SHA is read from the trailer.

### The three endpoints

| Field | Meaning |
|---|---|
| **Ours** | the contribution being frozen — the branch's tip |
| **Other** | the tip it is landing on or against |
| **Base** | `merge-base(Ours, Other)` |

Per op, with the snapshotted branch's pre-op tip as `p1`:

| Op | p1 | Ours | Other |
|---|---|---|---|
| rebase / rebase-pull | B's old tip | B's old tip | the `onto` / new upstream |
| merge | target's old tip | the **source** tip | target's old tip |
| merge-pull | B's old tip | B's old tip | the new upstream |

`Base` cannot be recomputed later — after a merge, `merge-base(target, source)`
returns the source's tip, not the fork point — which is why it is recorded
rather than derived. `Ours` in the merge case is a live branch someone may
delete afterwards, which is why it is pinned as a parent.

### Metadata encoding

One git trailer, space-separated, fixed arity:

```
Gg-Meta: <op> <ours-sha> <other-sha> <base-sha> <source-name> <target-name>
```

Read back in the **same single `for-each-ref`** that lists versions today:

```
%(refname)%00%(parent)%00%(subject)%00%(trailers:key=Gg-Meta,valueonly,separator=%x20)
```

**Do not split this into one trailer per field.** Verified on git 2.43: two or
more `%(trailers:key=…)` atoms in one format string cross-contaminate — asking
for `Gg-Base` and `Gg-Ours` returns *both* values in *both* atoms. A single atom
is correct. This is the single most likely "improvement" a future reader will
try; it silently corrupts every version record.

Branch names may contain spaces? No — git refnames cannot. But they may contain
most other characters, so the two names go **last**, and parsing takes the first
four fields positionally and splits the remainder on the final space.

### Writing it

- `commit-tree` needs an identity and a fresh repo may have none. Invoke through
  `RunEnv` with fixed `GIT_AUTHOR_NAME/EMAIL`, `GIT_COMMITTER_NAME/EMAIL`, and
  both `_DATE`s set to the snapshot's `ts` — deterministic objects, no
  dependence on the user's config. (`internal/app/gitenv_test.go` is the
  precedent for pinning git env.)
- The message goes via `-F <tempfile>`, not `-m`: the `Runner` has no stdin,
  repeated `-m` makes each field its own paragraph and breaks the trailer block,
  and embedded newlines in argv are a Windows hazard. `(*Repo).EmptyTree`'s
  temp-file pattern is the precedent.

### One format, one shape

Under marker format **2**, *every* snapshot is a synthetic commit — including
the one-branch ops (`amend`, `reset`, `undo-commit`, `delete-branch`,
`restore`), which simply carry no `Ours`/`Other`/`Base` fields.

This supersedes the earlier "one-branch ops keep the plain tip" ruling, which
predates the format-2 decision. A plain-tip ref sitting beside synthetic commits
under one marker is exactly the mixed-shape data spec 1 exists to prevent, and
telling them apart would need a `cat-file -t` per ref — the content sniffing
that spec rejected. The versions popup opens a fieldless record as today's
commit view and a full one as the frozen preview.

## The frozen view

`domain.VersionPreview(ref) → PreviewEndpoints{Left: Base, Right: Ours}` feeds
the **existing** compare pipeline, which already takes
`model.Endpoint{Kind: EndpointCommit, Hash: …}` — arbitrary commits, not branch
names. So the frozen view is a *lens*, not a new renderer.

It shows what the Previews tab shows: files changed (the point), and the commit
list for `Base..Ours` (secondary — derivable from the same two SHAs via
`LogRangeMessages`).

## Drift detection

### The two change sets

```
before = git diff --name-status --no-renames <Base> <Ours>
after  = git diff --name-status --no-renames <Other> <newTip>
```

where `newTip` is the snapshotted branch's tip *after* the operation. One rule,
no per-op special-casing — it is correct for every path:

- rebase B onto O → `before = Base..B_old`, `after = O..B_new` ✓
- merge S into T → `before = Base..S`, `after = T_old..M` ✓
- pull-merge U into B → `before = Base..B_old`, `after = U..M` ✓

The naive `oldTargetTip..newTargetTip` formula is **wrong** for pull-merge: the
merge commit's first parent is the branch's own old tip, so that range is
*upstream's* changes and every file upstream touched would be flagged.

`--no-renames` on both sides is mandatory. With rename detection on, a `D`+`A`
pair on one side can be reported as a single `R` on the other, manufacturing
phantom differences.

### What is compared, and what warns

Compare as sets of `(status, path)`. **Alarm on `after − before`** — changes the
operation itself introduced to the branch's change set. The user's case lands as
`A path` in *after* that was `M path` in *before*: upstream deleted the file, the
branch had modified it, a modify/delete conflict was resolved the wrong way, and
the branch now reads as adding a file it never added.

`before − after` is reported quietly, not alarmed: it usually means upstream
absorbed the change (an identical fix, a cherry-pick), which is correct.

### When it runs, and when the summary appears

- Detection runs **after `Execute` releases the gate**, in `Read` mode. The two
  `name-status` diffs must not run under a `TreeWrite` reservation (spec 1 had
  exactly that bug and fixed it).
- **Skip when the before-set is empty.** That covers a fast-forward pull (`Base
  == Ours`) and a branch with nothing ahead, with no op-path special-casing. Skip
  a failed or aborted op too — there is no after side.
- The before→after summary is shown **when the change set drifted, or when the
  operation paused for conflicts at all** — i.e. when it completed via
  `resume-paused-op`. "Completed via resume" *is* the paused signal; no flag is
  persisted and nothing has to survive the pause.

  This supersedes "show it when an external/AI tool took part". gg cannot see an
  agent driving it over the CLI or MCP at all, only exttools it launched itself;
  and a hand-resolved conflict is exactly as worth reviewing. The paused signal
  is a superset with none of the tracking.

## Format 2 and the migration

`versions` moves to `DataFormat{Store: "versions", Min: 2, Max: 2}` and gains
the first real `preflight.Migration{Store: "versions", From: 1, To: 2}`.

Existing format-1 records are discarded — see "Migration: discard" below. No
new migration code is needed: spec 1's `ApplyMigration` already does exactly
this.

## Frontends

| Surface | Frozen view | Drift |
|---|---|---|
| TUI | versions popup opens the frozen preview | summary in the op result + a notice |
| CLI | `gg versions` / the version's preview | drift summary on `gg rebase` / `merge` / `pull` output |
| web | versions group opens the frozen preview | summary in the op result panel |
| MCP | — | — |

MCP stays out, consistent with spec 1: it exposes no versions surface and runs
no ops. Because the CLI's op output changes, `internal/agentskill/using-gg.md`
gains the drift summary and `agentskill.Version` is bumped.

## Code seams

- `snapshotBranchTip` gains `(ours, other)` parameters — ~10 call sites. In
  `smart_pull.go` the three snapshot sites must run **after** the fetch, so
  `Other` is the new upstream tip.
- New git verbs: `CommitTree` (the synthetic commit) and
  `DiffNameStatus(a, b)` with `--no-renames`.
- **Every reader must unwrap `p1`**: `BranchVersions` / `AllVersionBranches`
  (their `Hash` is `p1`, not the synthetic commit), `RestoreBranchVersion`, the
  TUI popup's subject/date, `internal/web/versions.go`, `gg versions`.
  `DeleteBranchVersion` is unchanged — it deletes the ref.
  `%(subject)` is now the synthetic commit's subject; where `p1`'s subject is
  wanted, batch-resolve all `p1` shas in **one** `git log --no-walk` rather than
  a call per ref.

## Testing

- **Pure/leaf:** the changeset set-difference — the `M`→`A` resurrection, a path
  only in *after*, a path only in *before*, and identical sets — driven with
  plain values.
- **`internal/git`:** `CommitTree` round-trip (tree is `p1`'s, parents correct,
  `Gg-Meta` reads back through one `for-each-ref`), and a guard test that the
  format string uses exactly **one** trailer atom.
- **`internal/engine` / `internal/domain`:** real git repos reproducing each op
  path and asserting the recorded `(Ours, Other, Base)`; a pull-merge fixture
  asserting no false alarm (the wrong formula's regression test); a rebase
  fixture where a modify/delete conflict is resolved the wrong way, asserting
  exactly one `A` finding.
- **Migration:** a format-1 repo resolving as `Repairable`, `gg migrate` listing
  the records it would discard without touching them, and `--yes` discarding them
  and stamping format 2 — the end-to-end apply path spec 1 could not cover.
- **e2e:** a scenario driving a rebase that resurrects a file and asserting the
  CLI reports it.

## Migration: discard (decided 2026-09-15)

Existing format-1 records are **discarded**, not converted. A format-1 ref is a
plain tip whose `Base` cannot be reconstructed, so it can never become a frozen
preview; and the user — the only person with such records — reports having made
little use of them precisely because the feature lacked what this spec adds.

**This requires no new migration code.** Spec 1's `engine.ApplyMigration` is
already a delete-the-listed-refs-and-stamp-the-marker executor, and
`domain.PendingMigrations` already computes the ref list via `storeRefs`. The
whole migration is therefore a declaration in `domain.Features()`:

```go
Migrate: &preflight.Migration{
    Store: StoreVersions, From: 1, To: 2,
    Describe: func() preflight.Text { … },
},
```

plus moving `DataFormat` to `{Min: 2, Max: 2}`. Everything downstream — the
consent gate, `gg migrate`, the web panel, the CLI refusal — already works and
gains its first real exercise. This also closes the coverage gap spec 1 shipped
with: `Repairable` had only test consumers, and the `--yes` apply loop was
untested end-to-end at CLI and web level.

The consent prose must state the consequence: because the versions *writer* is
gated on `FeatureEnabled`, **skipping the migration means no new versions are
recorded at all** until it is run — the user would be running rebases with no
safety net, the opposite of this feature's purpose.

## Rulings already made — do not re-ask

- Storage is the synthetic snapshot commit (option B), accepting that it appears
  under `git log --all`; today's version refs already put orphaned commits there.
- The frozen view is anchored at the recorded `Base`; the diff is the point and
  the commit list is secondary.
- Drift warns on the **file-set/status** signal only; content drift inside files
  both sides touch is expected and never warns.
- Detection is automatic, immediately after the op; it never blocks or pauses.
- The summary also appears whenever the op paused for conflicts.
- All frontends except MCP.
- Existing format-1 records are DISCARDED on migration, through spec 1's
  existing `ApplyMigration` — no wrap, no new migration op.
