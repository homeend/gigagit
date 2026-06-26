# Branch prefixes — design

**Date:** 2026-06-26
**Status:** approved (pending written-spec review)
**Feature branch:** `feat/branch-prefixes`

## Summary

Add a registry of reusable **branch-name prefixes** that a user can select when
creating a branch or a worktree. A prefix is a short, possibly-templated string
(e.g. `feat/`, `jdoe/`, `wt/<date:yyyy-MM-dd>/`). Selecting one **populates the
branch-name field** with the resolved prefix and leaves the cursor at the end so
the user types the remaining name.

Prefixes live in a **writable two-scope store** (global + per-repo) that mirrors
`internal/profile` exactly. They are **managed in Settings** (add/edit/remove,
Global|Repo) and **consumed read-only** at create time via a select-only picker
popup. A `gg prefix` CLI provides scriptable list/add/rm.

## Motivation

Teams use branch-name conventions (`feat/…`, `bugfix/…`, `<user>/…`,
`jira/<date>/…`). Today both create-branch (`b`/`B`) and create-worktree popups
require typing the full name (worktrees offer `[e] edit name`; create-branch is a
bare text field). Prefixes turn a convention into one keystroke + the variable
tail.

## Decisions (locked)

| Question | Decision |
|----------|----------|
| Prefix content | **Template strings** using gg's native `<…>` tokens, **non-interactive subset only** |
| Storage | **Writable two-scope store** (global + per-repo), mirroring `internal/profile`; **not** `.gg.toml` |
| Management UI | **Settings sub-screen** (like Identity & profiles); the create-time picker is **select-only** |
| CLI | **Included now**: `gg prefix ls | add <value> [--global] | rm <value>` |
| Path-leaf edit | **Out of scope** — separate spec next |

### Token scope (load-bearing constraint)

A prefix resolves through the existing `internal/template` engine with an
**empty `inputs` map**. Allowed tokens (all non-interactive):

- `<date:FMT>`, `<parent-branch>`, `<repo>`, `<seq:NAME>`,
  `<random-alpha:N>`, `<random-num:N>`

**Disallowed**: `<user:LABEL>` (free-input) and `<branch>`. Rationale: the whole
point of a prefix is that *the user types the tail*, so an interactive label
would be redundant; `<branch>` is path-only and meaningless in a branch name. A
prefix containing a disallowed token is rejected at **add time** (validation in
the store/domain layer) and, defensively, fails resolution (empty `inputs` makes
`<user:>` error) so a bad row can never silently produce a broken name.

## Architecture

A frontend-agnostic store + domain query/command layer drives both frontends,
exactly like profiles/bookmarks/shelf.

```
   TUI picker (select) + Settings (manage)      CLI: gg prefix ls|add|rm
                       \                          /
                        \                        /
                         internal/domain  ← owns the two stores, merges +
                                              tags scope; validates tokens
                                |
                         internal/prefix  ← Store interface + file impl
                                              (prefixes.toml, atomic rewrite)
   resolution: internal/template.Resolve (non-interactive ctx) — reused
```

### 1. `internal/prefix` (new package — mirrors `internal/profile`)

- `store.go`: `Store` interface — `Add(model.Prefix) (model.Prefix, error)`,
  `Get(id) (model.Prefix, error)`, `List() ([]model.Prefix, error)`,
  `Remove(id) error`. `ErrNotFound` for unknown id.
- `file_store.go`: `FileStore{root, scope}` with `path()` →
  `filepath.Join(root, "prefixes.toml")`; `read`/`write` (atomic rewrite);
  `PrefixID(value)` slug. Scope is `toml:"-"`, set on `List`/`Add` from the
  store's scope (same pattern as `profile.FileStore`).
- File format: a TOML `index` with `[[prefixes]]` rows `{ value = "feat/" }`.

### 2. `internal/model` (new type)

```go
// model/prefix.go
type Prefix struct {
    ID    string       `toml:"id"`    // slug derived from Value
    Value string       `toml:"value"` // the (template) prefix string
    Scope ProfileScope `toml:"-"`     // reuse ProfileScope (Global|Repo)
}
```

Reuse the existing `model.ProfileScope` (Global|Repo) rather than minting a
parallel scope enum — the two-scope semantics are identical. (Rename is out of
scope; a prefix's identity is its value, like a bookmark's is its address.)

### 3. `internal/domain` (new file `prefixstore.go`, mirrors `profilestore.go`)

- `PrefixStatePath` override var; `prefixBaseDir()` → `<state>/gg/prefix`
  cross-platform (copy of `profileBaseDir`).
- `prefixStores(ctx)` → `(global, repo prefix.Store)` keyed by git common dir;
  `SetPrefixStores(global, repo)` for tests.
- `Prefixes(ctx) ([]model.Prefix, error)` — global rows then repo rows, scope-tagged.
- `AddPrefix(ctx, model.Prefix) (model.Prefix, error)` — **validates tokens**
  (reject disallowed) before routing to the scope's store.
- `RemovePrefix(ctx, scope, id) error`.
- `service.go`: add `prefixGlobal`, `prefixRepo prefix.Store` fields.

Token validation helper (domain): parse `Value` with `template.Resolve(value,
map[string]string{}, neutralCtx)` where `neutralCtx` supplies `Now`/`Rand`/an
empty `Seqs` — a successful resolve with empty inputs proves no interactive
`<user:>` token; additionally scan for `<branch>` and reject. Return a clear
`invalid prefix: …` error surfaced to both Settings and CLI.

### 4. Resolution at create time (reuse)

Resolution stays in the TUI popups, reusing `template.Resolve` and the popup's
existing `tctx()` (which already supplies `Now`/`Rand`/`Seqs`/`ParentBranch`/
`Repo`). The resolved prefix string is inserted into the branch-name buffer.

**`<seq>` in a prefix**: peeked for the preview, the counter is bumped only on
actual create. The worktree popup already does this via `pendingSeqBump`; the
create-branch popup gains the same peek + bump-on-success plumbing. A prefix's
`<seq>` names are unioned into the consumed-seq set when that prefix is used.

### 5. TUI — select-only picker popup

A new popup `prefixPickerPopup` styled like the bookmark `g` / shelf `G`
switchers (single-column list, `/` filter, `enter` select, `esc` cancel). Rows
are grouped/tagged **[Global]** / **[Repo]**, each showing the raw template and a
live-resolved preview (resolved against the opener's ctx). Empty store → a hint
row pointing at Settings.

Entry points:

- **Create-branch popup** (`branch_popup.go`): new key **`p`** ("use a prefix").
  On select → resolve → set the `name` field value to the resolved prefix, cursor
  at end → user continues typing. (This popup gains a `tctx()`/seq-peek helper so
  templated prefixes resolve; today it has none.)
- **Create-worktree popup** (`worktree_popup.go`): in `stAction`, add
  **`[p] use a prefix & edit`** next to `[e] edit name`. On select → resolve →
  enter the existing `stEdit` state with `editBuf` seeded to the resolved prefix
  (cursor at end). Reuses the `branchOverride` machinery — **no new edit state**.
  Available in new-branch and `fromCommit` modes; suppressed in `existing` mode
  (no new branch name there), consistent with `[e]`.

### 6. TUI — Settings management surface

Add a **"Branch prefixes"** entry to the Settings (`,`) surface, modeled on
`identity_popup.go`'s browse/form pattern:

- Browse list of prefixes (scope-tagged), `a` add, `e` edit, `d` remove.
- Add/edit form: a value text field + a Global|Repo scope toggle (reuse the
  identity form's scope-toggle idiom). Submit → `AddProfile`-equivalent
  (`AddPrefix`); invalid-token errors shown inline.
- Reuse the `loadIdentityDataCmd`/`addProfileCmd`/`removeProfileCmd` command
  shape (`loadPrefixDataCmd` / `addPrefixCmd` / `removePrefixCmd`).

### 7. CLI — `gg prefix`

New `internal/cli/prefix.go` with `cmdPrefix`, routed by a `case "prefix"` in
`cli.go` (mirrors `cmdBookmark`):

- `gg prefix ls` — list global + repo prefixes (scope-tagged; `--json` optional,
  match existing CLI conventions).
- `gg prefix add <value> [--global]` — default scope is repo; `--global` targets
  the global store. Validates tokens; prints the stored value.
- `gg prefix rm <value>` — remove by value (slug id) from whichever scope holds
  it (or `--global` to disambiguate).

## Data flow (select → create)

1. User opens create-branch or create-worktree, presses `p`.
2. Picker loads `domain.Prefixes(ctx)`; user filters/selects a row.
3. Popup resolves the selected `Value` with its `tctx()` (peeking `<seq>`).
4. Resolved string lands in the branch-name buffer, cursor at end; user types
   the tail and creates as normal.
5. On successful create, used `<seq>` counters (template + prefix) are bumped.

## Error handling

- Invalid prefix (disallowed/malformed token): rejected at `AddPrefix` (Settings
  inline error; CLI non-zero exit + message). Never stored.
- No state dir (stores disabled): `Prefixes` returns empty; picker shows the
  empty hint; `gg prefix add` errors clearly.
- Empty branch name after prefix: the existing create-time validation
  (`branchPopup` enter-guard / worktree `branchOverride==""` guard) still applies
  — a prefix alone with no tail is allowed only if it is itself a valid ref name
  (git validates via `CheckRefFormatBranch`).

## Testing

- `internal/prefix`: file-store round-trip (add/list/remove, scope tagging,
  atomic rewrite), `PrefixID` slugging — mirror `profile/file_store_test.go`.
- `internal/domain`: `Prefixes` merge+tag order; `AddPrefix` token validation
  (accept `<date>`/`<seq>`/`<random>`/`<parent-branch>`/`<repo>`; reject
  `<user:…>`, `<branch>`, malformed); scope routing — with injected temp stores.
- `internal/tui`: picker open/filter/select inserts resolved prefix into the
  field (both popups); worktree `p` seeds `stEdit`; Settings add/edit/remove
  round-trip through injected stores; `<seq>` peek-vs-bump.
- `internal/cli`: `gg prefix add/ls/rm` happy paths + `--global` scope +
  invalid-token rejection (FakeRunner / temp state dir).
- e2e: a scenario creating a prefix then a branch/worktree using it (asserts the
  resulting branch name), if the harness can drive the picker (else CLI-only).

## Wiring checklist (adding-features)

- [ ] `internal/prefix` store + file impl + tests
- [ ] `model.Prefix`
- [ ] `domain`: stores, `Prefixes`/`AddPrefix`/`RemovePrefix`, token validation
- [ ] TUI picker popup + key in both create popups
- [ ] TUI Settings "Branch prefixes" surface
- [ ] CLI `gg prefix` + dispatch
- [ ] Docs: `CHANGELOG.md`, `README.md` (keybinding + CLI), `CLAUDE.md` package
      map (`internal/prefix`), `internal/agentskill/using-gg.md` + bump
      `agentskill.Version` (CLI surface changed) → `gg init --update`
- [ ] Help/footer: advertise the `p` keybinding in `help.go` AND the footer

## Out of scope

- Worktree path-leaf editing (next spec).
- Repurposing the unused `config.BranchTemplates` field (left as-is).
- Editing a prefix's scope in place / renaming (remove + re-add instead).
- Interactive `<user:LABEL>` tokens in prefixes.
```
