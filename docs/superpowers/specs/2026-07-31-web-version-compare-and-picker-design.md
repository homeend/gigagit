# Web UI: version ↔ tip compare, and the all-branches versions picker

Date: 2026-07-31
Branch: `feat/web-version-compare` (off `web-dev`)

The two pieces deliberately left out of Part B item 15
(`2026-07-26-web-branch-dnd-and-parity-design.md`): comparing a recorded
branch version against the branch's current tip, and the picker over every
branch with recorded versions — the deleted-branch recovery path.

---

## 1 — Version ↔ tip compare

### Problem

The web versions popup (item 15) lists a branch's recorded snapshots and
offers restore / copy / delete. The TUI's `enter` — a whole-tree compare of
the selected version against the branch's current tip — has no web
equivalent, because `/api/compare` only accepts branch **names**, resolved
against the live branch list.

### The allowlist question

Does letting `/api/compare` take raw revs erode its allowlist? **No — the
allowlist's rationale is name-specific and does not transfer.**

The allowlist (compare.go's doc comment) exists because an unknown branch
*name* yields an empty compare indistinguishable from "these two branches
are identical" — a silent wrong answer. A commit **hash** has no such
failure mode: an unknown object makes `CompareFiles` fail loudly. And the
injection concern is *stronger* under the rev form, because the gate is
stricter than `isGitArgSafe`: a full 40-hex string can't be a flag, a
range, or anything but an object id (the TUI's marked-range-review rule —
"already hex, so `Range` stays injection-safe").

So the design is an **explicit second mode, not a widened first one**:

- `GET /api/compare?a=<name>&b=<name>` — unchanged. Names resolved against
  the live branch list, 404 on unknown, exactly as today.
- `GET /api/compare?a=<hex40>&b=<hex40>&revs=1` — both values must satisfy
  a new `isHexHash` (exactly 40 lowercase hex chars), else 400. No branch
  resolution; the hashes are the endpoints. Response shape identical
  (`a_hash`/`b_hash` echo the inputs), so the client's per-file diff and
  origin-filter paths need no changes.

No sniffing ("is this 40-hex value a branch name?") and no mixed mode: a
request is either names or revs. The tip hash comes from the client's
cached `state.branches` — the same standing as the TUI, whose
`branchTipHash` reads its cached `m.branches`.

### Riding along: enforce handleRevDiff's documented contract

`/api/diff?left=&right=` (handleRevDiff) documents "both revs are commit
HASHES resolved by /api/compare, never branch names" but only enforces
`isGitArgSafe`. Its sole client caller already passes hashes, so `left`/
`right` tighten to the same `isHexHash` — the doc comment becomes enforced
instead of aspirational.

### Client

`showVersionMenu` gains a first row, **"compare against current tip"**:

- Shown only when the branch has a live tip in `state.branches`. A deleted
  branch omits the row — the TUI's rule ("branch no longer exists — restore
  it to compare"): restore first, then compare.
- Opens the existing compare view via `openCompare`, generalized to accept
  `{revs: true, aLabel, bLabel}`. Query values are the version hash and the
  tip hash; display labels are `<branch>@<short>` and `<branch> (tip)`,
  used for the files header and the origin-filter buttons. The origin
  filter itself works unchanged — `CompareOrigins` on two commits is
  exactly what it already does.

## 2 — All-branches versions picker

### Problem

`openVersions(branch)` is only reachable from a live branch's sidebar menu.
A **deleted** branch's versions — the whole point of recording them on
`delete-branch` — are unreachable in the web, though the server-side read
(`/api/versions` deliberately skips branch-list resolution) and the write
(`restore-version` → `git.UpdateRef` recreates the ref) both already work.

### Server

`GET /api/version-branches` — no params — over `domain.AllVersionBranches`:

```json
{ "branches": [ { "branch": "feat/x", "deleted": true,
                  "count": 3, "latest_unix": 1753900000 } ] }
```

### Client

A new `vbranches` overlay layer (markup and interaction cloned from the
versions overlay: backdrop click and esc close it): one row per branch —
name, snapshot count, relative time of the latest, and a `deleted` tag on
branches that no longer exist. Clicking a row closes the picker and calls
`openVersions(branch, {deleted})`.

`openVersions` gains that optional `deleted` flag: the popup title appends
"(branch deleted)" so the restore row's meaning — recreate the branch — is
visible. Restore and delete-snapshot rows work unchanged; the compare row
is absent per §1.

Entry points, both one line: `paletteCommands()` row "branch versions…" and
an `openGlobalMenu()` row of the same label.

## Testing

- `compare_test.go`: revs-mode happy path (two known hashes → same file
  list as the name form), non-hex under `revs=1` → 400, and the existing
  name-mode tests stay green.
- Rev-diff tightening: a branch name in `left`/`right` now 400s.
- `versions_test.go` (or a sibling): `/api/version-branches` on a repo with
  snapshots on a live branch and on a deleted one — the deleted flag is the
  point of the endpoint.
- Client verified headlessly (rebuild, restart, hard-reload — the
  playwright/CDP notes): picker visible and populated, drill into a deleted
  branch, restore recreates it; compare opens with the right labels.

## Not doing

- Version ↔ version compare (TUI doesn't either; restore-then-compare
  covers it).
- A "new branch at version" lane (TUI has it; separate increment if wanted).
- Gating the picker rows on server-side tip re-resolution — the client's
  branch list is the compare gate, per the TUI precedent.
