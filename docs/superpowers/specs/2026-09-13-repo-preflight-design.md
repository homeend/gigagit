# Repo preflight & feature requirements — design

*Agreed 2026-09-13. Spec 1 of 2; spec 2 is "frozen preview versions + drift
detection", which is this subsystem's first real consumer.*

## Problem

gg owns a growing amount of per-repo and machine-local state: branch-version
refs, notes, shelves, bookmarks, previews, promptstate, prefixes, profiles, the
repos registry. Nothing in the codebase records *what format* any of it is in.
When a build changes a store's layout it has no way to tell old data from new,
so it either misreads it silently or the feature behaves as if the data were
absent.

The trigger is spec 2: branch versions are about to change from "a ref pointing
at the old tip" to "a ref pointing at a synthetic snapshot commit". Existing
version refs become unreadable. The user's requirement is that gg must *say so*
— describe what will be discarded and why — and ask, before the interface
appears, rather than quietly dropping data.

Generalised (the user's framing): a feature should **declare what it needs**,
and the runtime should decide whether that feature — or the whole app — can
start.

## Goals

- A feature declares its requirements statically: accepted data-format ranges,
  minimum git version, and whether it is critical.
- At service construction the runtime resolves every feature to a verdict and
  either initialises it, offers a migration, disables it, or refuses to start.
- A destructive migration is never applied without explicit consent, in any
  frontend.
- Declining a migration never locks the user out of a repo (unless the feature
  is `Required`).

## Non-goals

- Health, hints and informational notices. Those stay with the notification
  center. Preflight reports only **migrations** and **blocking conditions**.
- Downgrade migrations. Data written by a newer gg is never rewritten.
- The 1 → 2 versions migration itself. That is spec 2.
- Remembering a declined migration. There is deliberately no suppression.

## The contract

`internal/preflight` is a DAG leaf. It holds the registry, the requirement
kinds and the resolver, and it never runs git — it receives probe *results* and
returns verdicts, so the whole decision table is testable with plain values.

```go
type Criticality int // Required | Optional

type Feature struct {
    ID          string        // English protocol value: "versions", "core"
    Criticality Criticality
    Requires    []Requirement
    Migrate     *Migration    // nil when nothing is repairable
}

type Requirement interface {
    Check(Probes) State
}

type DataFormat struct { Store string; Min, Max int } // inclusive range
type GitVersion struct { Min [3]int }

type Migration struct {
    From, To int
    Describe func() Text  // consequence prose; see "Prose" below
    Apply    func(...)    // runs as an engine Operation, not here
}

func Resolve(features []Feature, p Probes) []Verdict
```

### Verdicts

| State | Condition | Outcome |
|---|---|---|
| `Satisfied` | found format within `[Min,Max]`, or the store holds no data at all | initialise |
| `Repairable` | found format below `Min` **and** a `Migration` covers it | describe consequences, ask consent |
| `Unsatisfiable` | found format above `Max`, or below `Min` with no migration, or git too old | `Optional` → disable; `Required` → explain and exit |
| — | a feature with several requirements | takes the **worst** verdict among them |

The range — rather than a single expected number — exists for the
above-`Max` case: an older gg opening a repo a newer gg has written cannot
downgrade the data, so it must disable the feature, never touch it.

### Legacy detection (the case that is easy to get wrong)

Every repo in the wild today has version refs and **no marker**. "No marker →
stamp the current format" would declare that old data current and hand the new
parser garbage. So a marker's absence is resolved against whether the store
holds anything:

- marker absent, **store has data** → the store is at **format 1** by
  definition (the pre-marker era); compare to the range like any other value.
- marker absent, **store empty** → `Satisfied`; there is nothing to migrate.

Each requirement kind therefore carries a cheap "does this store hold any
data?" probe alongside its format probe.

## Markers

**Repo-scoped stores** stamp a ref under `refs/gg/meta/<store>/<N>` pointing at
the empty tree. The format number lives in the *ref name*, so a single
`for-each-ref refs/gg/meta/` reads every marker in one git invocation, with no
blob writes and no `cat-file`. Verified against real git: a ref may point at a
tree object, and `for-each-ref` reports it. `refs/gg/meta/` sits outside
`refs/heads|tags|remotes`, so it is never pushed or fetched and is shared by all
worktrees through the common dir — the same properties `refs/gg/versions/`
already relies on.

**Machine-local TOML stores** carry a top-level `format = N` field. Their probe
must resolve the store path exactly the way the store itself does —
`XDG_STATE_HOME` wins everywhere in this codebase, and a probe that resolves
differently will report a phantom empty store.

## Startup is read-only

Nothing stamps a marker during resolution. Each store's own **writer** stamps
on first write — for versions, `snapshotBranchTip` writes
`refs/gg/meta/versions/<N>` alongside the snapshot.

Two reasons. It removes the compare-and-swap race between concurrently starting
`gg` processes entirely. And on a ~100GB monorepo shared with agents, a ref
write on every single `gg` invocation is exactly the cost this project avoids.

The only write preflight ever performs is a consented migration, and that runs
as an engine `Operation` through `domain.Execute` under `RefWrite`/`TreeWrite`
— it destroys refs and must hold the repo gate.

## Where resolution happens

Per `Service` construction **and on reRoot**. "At startup" is wrong: the TUI
repo switcher and the web both change repos inside a live process, and a new
repo must be re-resolved. Verdicts are cached on the `Service`; `gg batch`
resolves once for the whole batch.

Domain exposes:

- `Service.Preflight(ctx) []Verdict` — runs the probes (one `for-each-ref
  refs/gg/meta/`, one existence probe per store, one `git --version`) and calls
  `preflight.Resolve`.
- `Service.FeatureEnabled(id) bool` — the single gate.
- the migration `Operation`, applied through `Execute`.

## Which surface a check belongs on (user ruling, 2026-09-14)

The boot gate exists **only for decisions that must be made before the engine
starts** — today that means consent for a destructive migration, because the
data it rewrites is data the running app would otherwise read.

Everything else belongs in the **ordinary notification panel**, after start.
In particular, a condition the user cannot act on from inside gg — git is too
old, or the store was written by a newer gg — is not a boot-time decision. It
is information, and it surfaces like any other notice, with ordinary
dismissal.

A notice that only *diagnoses* is noise. Every non-`Satisfied` verdict states
its remedy: `Repairable` names `gg migrate`; `Unsatisfiable` names the upgrade
(`Requirement.Remedy`). If a check can offer neither a decision before start
nor a remedy after it, it should not be a notice at all.

## The disabled gate

`FeatureEnabled` is checked **inside the owning domain queries and ops**, which
return a typed `ErrFeatureDisabled{ID, Reason}`. Frontends never re-derive the
decision; they only render it. Scattering the check across four frontends is
the failure mode this avoids.

| Frontend | `Repairable` (consent pending) | Disabled | `Required` + unsatisfiable |
|---|---|---|---|
| TUI | pre-UI consent screen: **Migrate / Skip / Quit** | feature's keys inert; standing notification-center notice | friendly message, exit 1 |
| CLI | affected commands fail naming `gg migrate`; unaffected commands work | same failure, reason "disabled" | message, exit 1 |
| MCP | treated as Disabled — never prompts | tools omitted from the surface | startup error |
| web | consent panel before the app renders | feature's group hidden | error page |

**Skip is per-session and never remembered.** While a migration is pending the
consent screen appears on every launch; there is no "don't ask again" anywhere.
A permanently-off feature the user has forgotten about is the outcome this
design exists to prevent.

**Quit is always offered.** A pre-UI gate is the one place where trapping the
user would be unforgivable.

`gg migrate` is the single consent path for headless use: with no arguments it
lists pending migrations and their consequences and changes nothing; `--yes`
applies them.

## Prose

`Migration.Describe()` returns a plain `preflight.Text{Format string; Args
[]any}` — **not** `engine.Msg`. `Msg` lives in `internal/engine`, and a leaf
package cannot import it without breaking the DAG (`archtest` would fail).
`domain` converts `Text` to `engine.Msg` at the boundary; the two structs are
deliberately identical in shape so the conversion is mechanical.

The `Format` string must be a literal key present in all four bundles — the
consent text renders in the TUI, and a bare string fails `engine_prose_test`.
Feature IDs, op tokens and verdict names stay English protocol values, like
decision options.

## v1 consumers

Named explicitly so the implementation plan does not invent any:

- **`core`** — `Required`, `GitVersion{Min}`. Gives the explain-and-exit path a
  live consumer. There is no git-version probe in the tree today: it is one new
  verb plus parsing.
- **`versions`** — `Optional`, `DataFormat{Store: "versions", Min: 1, Max: 1}`,
  **no migration**.

`Repairable` therefore has only a *test* consumer in this spec. Spec 2 bumps
`versions` to format 2 and supplies the first real migration. This is stated
plainly rather than fabricating a migration to have one.

## Testing

**`internal/preflight`** (pure; the bulk of the coverage). A fake registry of
synthetic features drives the decision table with plain values: each of the four
states; marker-absent-with-data → legacy 1; marker-absent-without-data →
`Satisfied`; found above `Max` → `Unsatisfiable` and never `Repairable`;
`Required` + unsatisfiable → fatal while `Optional` + unsatisfiable → disabled;
a multi-requirement feature taking the worst verdict. Table-driven, `t.Parallel()`.

**`internal/domain`** (real git in `t.TempDir()`). Marker refs written and read
back through `for-each-ref`; a repo with version refs and no marker resolving as
legacy; verdicts re-resolved on reRoot; an owning query returning
`ErrFeatureDisabled`; the migration op taking the gate through `Execute`.

**Git-version probe.** `FakeRunner` argv assertions, plus parser cases against
the shapes git actually emits — `2.43.0`, `2.39.3 (Apple Git-145)`,
`2.43.0.windows.1`. That parsing is where this quietly breaks.

**e2e.** `gg migrate` with no arguments lists pending work and mutates nothing
(assert refs unchanged); `--yes` applies, and a second run reports nothing
pending. A CLI command against a disabled feature exits with the message naming
`gg migrate`.

**Gates.** `archtest` keeps `preflight` a leaf and the frontends off
`internal/git`; the i18n AST gates catch consent prose that is not a literal key
in all four bundles.

## Rulings already made — do not re-ask

- Scope: migrations **and** blocking conditions; health/hints stay with the
  notification center.
- Declining a migration runs gg with that feature disabled (not an exit).
- No suppression of the consent screen, ever; Skip stays and is per-session.
- MCP maps `Repairable` → Disabled unconditionally; it never prompts.
- CLI gets `gg migrate`; affected commands fail pointing at it.
- Requirement kinds in v1: data format and git version only. External-tool
  presence is deliberately excluded — it would compete with the existing
  `exttool` detection.
- Backward compatibility with existing on-disk data is **not** a constraint: old
  data may be discarded, provided the user is told what and why.
- All four frontends in the first cut.
