# Versioned external-tool templates with agent-version variants — design

Status: agreed 2026-09-30; open questions ruled (see end).

## Problem

Settings → External tools copies a catalog row (`exttool.Builtins`) into the
user's `config.toml` as a `[[tools.command]]` block. The copy is frozen: when
a later gg changes the catalog row, the user's block never follows, and the
wizard treats the row as already installed (it matches on category + name),
so re-adding does nothing.

Trigger case: `Claude — resolve & complete (yolo, headless)` was retagged
from `frontends = ["web"]` to `["tui", "web"]` in `32b01170`. A block written
before that stays web-only, so the TUI conflict menu never offers it.

A second axis the catalog cannot express today: an agent's CLI changes
between its own releases (a flag renamed, a permission mode added). The right
template depends on the installed agent's version, not only on gg's version.

The only upgrade path today is `exttool.UpgradeReviewCommand` — a hand-kept
list of superseded review commands behind one preflight migration. It covers
one category, one change, and command text only.

## Goals

- gg knows, per config block, which catalog template and template version it
  was written from.
- A catalog template can carry several variants, each for a range of the
  agent's own version.
- When a newer template (or a better-fitting variant) exists, gg offers it —
  including for blocks the user edited — and never overwrites a user edit
  without consent.
- Formatting-only edits (whitespace, key order, comments, quoting) do not
  count as edits.

Non-goals: changing how commands are resolved or run; updating
user-authored blocks that never came from the catalog; a three-way merge of
user edits with a new template.

## Catalog model (`internal/exttool`)

A catalog row becomes a **family** keyed by `(Category, Name)` — the same key
config blocks already use (`config.ToolCommand.Key`).

```go
type CommandTemplate struct {
    Category Category
    Name     string
    Version  int        // template revision; bumped on ANY change to the family
    OptIn    bool
    Variants []Variant  // exactly one when the family is not version-sensitive
}

type Variant struct {
    Range     string   // agent version range: "" (any), ">=2.1", ">=1.8 <2.1"
    Mode      Mode
    PerFile   bool
    WhenOp    string
    Frontends []string
    Command   string
}
```

`Tool` gains how to read the agent's version:

```go
VersionArgs []string       // default {"--version"}; nil + no ranged variants = never probed
VersionRe   *regexp.Regexp // first capture = "X.Y[.Z]"; default = first \d+\.\d+(\.\d+)?
```

**Range grammar:** space-separated AND of `>=X.Y[.Z]` and `<X.Y[.Z]`
comparators; `""` means any version. Missing components compare as 0.

**Catalog invariants (enforced by tests in `exttool`):**

1. Within a family, variant ranges are pairwise disjoint (user ruling
   2026-09-30: overlapping ranges are forbidden, not first-match). An
   `""` (any) variant must be the family's only variant.
2. Version bump guard: a golden file
   (`internal/exttool/testdata/catalog_versions.golden`) records each
   family's `Version` and the fingerprint of all its variants. The test
   fails when a family's fingerprint changed but its `Version` did not; the
   `-update` flag refuses to rewrite an entry whose version was not raised.
   Nobody can change a template and forget the bump.
3. `Version` only increases (the golden file holds the previous value).

## Config stamp (`internal/config`)

A block written from the catalog carries three extra keys:

```toml
[[tools.command]]
category = "conflict_complete"
name = "Claude — resolve & complete (yolo, headless)"
mode = "capture"
frontends = ["tui", "web"]
template_version = 3
agent_range = ">=2.1"          # the variant this block was rendered from ("" = any)
fingerprint = "sha256:…"       # normalised fingerprint of the block AS WRITTEN
command = '''…'''
```

`config.ToolCommand` gains `TemplateVersion int`, `AgentRange string`,
`Fingerprint string`. The TOML decoder already ignores unknown keys, so an
older gg reads a stamped config unchanged.

**Normalised fingerprint** (one function, `config.ToolFingerprint`), over the
parsed fields only — `mode`, `per_file`, `when_op`, sorted `frontends`,
`command` — never the raw file text:

- command: CRLF → LF, trim, strip trailing spaces per line, drop blank
  lines, collapse runs of spaces/tabs to one space;
- comments, key order, quoting style and indentation never reach it (they do
  not survive TOML parsing).

A whitespace change inside a quoted prompt therefore reads as "not edited".
Since no update is ever applied without consent, the fingerprint only
decides "customised" (silent) vs. "you changed this block" in the offer
reason; a normalisation miss costs at most a wrong word in that line.

## Agent version probe (`internal/domain`)

- For each detected tool whose family set has any ranged variant, run
  `<bin> <VersionArgs…>` off the UI thread with a 3 s timeout, stdin closed.
- Parse with `VersionRe`. Cache per session keyed on (resolved binary path,
  mtime), so an agent upgrade mid-session is picked up on the next check.
- Probe failure / unparsable output ⇒ version **unknown**.

Exposed as a domain query (`ToolTemplateStatus`) so the TUI, web and CLI see
one answer.

## Status of a block

For each config block whose key matches a catalog family, with the agent's
version `v` (possibly unknown):

1. **Target variant:** the variant whose range contains `v`. If `v` is
   unknown, the variant named by the block's `agent_range` (else the family's
   only variant). If no variant contains a known `v` ⇒ **unsupported**. If
   `v` is unknown and no variant can be chosen (an unstamped block of a
   multi-variant family) ⇒ no status, no offer.
2. **Edited?** `ToolFingerprint(block) != block.fingerprint`. An unstamped
   block counts as edited.
3. **Behind?** `block.template_version < family.Version`, or the target
   variant's range ≠ `block.agent_range`.

| Stamp | Edited? | Behind? | Status | Action |
|---|---|---|---|---|
| stamped | no | no | current | none |
| stamped | no | yes | update available | offer — never automatic (user ruling 2026-09-30) |
| stamped | yes | no | customised | none |
| stamped | yes | yes | update available | offer |
| none | — | — | if equal (normalised) to the target rendering: current; else update available | offer — user ruling 2026-09-30 |
| any | — | — | unsupported (known `v` outside every range) | notice: "Claude 1.2 is outside every range the <name> template supports"; block keeps running as is |

Blocks whose key matches no family (user-authored) are never touched.
No block — stamped or not, edited or not — is ever written without the
user's consent, not even to add a stamp. "Edited?" therefore only decides
whether a block is "customised" (current template, user changes: silent) —
it never unlocks an automatic write.

**The offer reason** is one line derived from the status, shared by the
notice and the review popup: "template updated (v2 → v3)", "Claude 2.3
detected — this block was written for `<2.1`", or both; plus "you changed
this block" when it is edited.

**The offered rendering** is the target variant generated with
`GenerateCommand` for the currently detected binary. A family whose tool is
not detected gets no offer.

## Surfaces

**Notice** (notification center, `adding-notifications`): one notice
"N external-tool templates can be updated" (and one per unsupported agent),
opening Settings → External tools.

**Settings → External tools (TUI):** an installed row shows its status
suffix (`update available` / `unsupported`). `enter` on an
update-available row opens a review popup (user ruling 2026-09-30): the
offer reason (the notification line above) and the **full text of the new
template** as it would be written — no side-by-side diff, no history of old
template texts — and three answers:

- **Take new** — replace the block in place with the new rendering + a fresh
  stamp;
- **Keep mine** — no change; remember the declined target (block key +
  target fingerprint) in `promptstate` so this exact update is not offered
  again; the next template change offers again;
- **Edit** — open the config file at the block in `$EDITOR` (via
  `handover()`).

**Web** (`internal/web/exttools.go`): the same status and review dialog,
through the same domain query and op.

**Writes** go through one engine op (`UpdateToolCommand`) using a new
scoped writer `config.ReplaceToolCommand(path, key, block)` that rewrites
exactly one `[[tools.command]]` block (located by category + name) in the
file that holds it — global or repo — leaving every other byte alone.
`AppendToolCommands` (the wizard) writes the stamp on every new block.

## Relation to the review-command migration

`UpgradeReviewCommand` and its preflight migration stay as they are for
blocks written before this feature. Their current review templates become
`Version: 1` families like any other; future review changes use the new
mechanism, and `SupersededReviewCommands` stops growing.

## Testing

- `exttool`: range parse/contains; disjoint-ranges invariant; golden
  version-bump guard (including a deliberately changed template in a test
  catalog failing without a bump).
- `config`: `ToolFingerprint` normalisation table (whitespace, CRLF,
  frontends order → equal; a flag change → different);
  `ReplaceToolCommand` round-trip leaving surrounding blocks, comments and
  repo/global files untouched; stamp keys decode and survive.
- `domain`: status table above as a table-driven test with a fake version
  probe (known / unknown / out of range / probe timeout).
- TUI: Settings row suffix and review popup via `tui-capture`; notice
  appears once and clears after Take new.
- e2e: a scenario with an unstamped web-only Claude headless block →
  status "update available" → take new → the TUI conflict picker offers it.

## User rulings (2026-09-30)

1. Unstamped blocks: offer the update (never stamp silently).
2. Overlapping variant ranges: forbidden (catalog test), not first-match.
3. A stamped, unedited block that falls behind: **always ask** — no
   automatic updates at all.
4. The review popup shows the offer reason (the notification line) and the
   new template's full text; no diff, no old template texts kept.
