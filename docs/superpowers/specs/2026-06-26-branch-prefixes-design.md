# Branch prefixes — design

**Date:** 2026-06-26
**Status:** approved (pending written-spec review)
**Feature branch:** `feat/branch-prefixes`

## Summary

Add a registry of reusable **branch-name prefixes** (really branch-name
*skeletons*) that a user can select when creating a branch or a worktree. A
prefix is a short, possibly-templated string — from a literal `feat/` up to a
company schema like `john_smith/ISSUE-<user:issue-id>` or
`john_smith/sandbox-<seq:sandbox_seq:4>`. Selecting one **fills any interactive
`<user:…>` labels**, **resolves** the template, **populates the branch-name
field** with the result, and leaves the cursor at the end so the user appends the
remaining free text (e.g. `_some_text_description`).

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
| Prefix content | **Template strings** using gg's native `<…>` tokens — **all tokens including interactive `<user:LABEL>`** (only `<branch>` disallowed) |
| Storage | **Writable two-scope store** (global + per-repo), mirroring `internal/profile`; **not** `.gg.toml` |
| Management UI | **Settings sub-screen** (like Identity & profiles); the create-time picker is **select-only** |
| CLI | **Included now**: `gg prefix ls | add <value> [--global] | rm <value>` |
| Path-leaf edit | **Out of scope** — separate spec next |

### Token scope (load-bearing constraint)

A prefix resolves through the existing `internal/template` engine. **All tokens
are allowed except `<branch>`**:

- `<user:LABEL>` — **interactive**; collected at select time (see picker flow).
  Example: `john_smith/ISSUE-<user:issue-id>`.
- `<seq:NAME>` and `<seq:NAME:N>` (zero-padded width N). Example:
  `john_smith/sandbox-<seq:sandbox_seq:4>` → `…sandbox-0007`.
- `<date:FMT>`, `<parent-branch>`, `<repo>`, `<random-alpha:N>`, `<random-num:N>`.

**Disallowed**: `<branch>` — it is path-only / self-referential and the branch
name doesn't exist yet at create time. A prefix containing `<branch>`, an unknown
token, or a malformed token is rejected at **add time** (domain validation) so a
bad row can never reach the create flow.

> Note on token names: gg's engine uses `<user:…>`, `<seq:NAME:N>`,
> `<random-num:N>` (lowercase). A "numbered counter padded to N" is
> `<seq:NAME:N>` (the user's `<num:sandbox_seq:4>` notation maps to
> `<seq:sandbox_seq:4>`). We keep the engine's existing token vocabulary; no new
> aliases are introduced.

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
   resolution: internal/template.Resolve (collected inputs + tctx) — reused
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

Token validation helper (domain): well-formedness is proven by a **dry resolve
with placeholder inputs** — build `inputs` from `template.UserLabels(value)` (each
label → a placeholder), a neutral `Ctx` (`Now`/`Rand`/`Repo`/`ParentBranch` set,
`Seqs` = each `template.SeqNames(value)` → 0), then `template.Resolve`. A resolve
error means an unknown/malformed token → reject. Additionally scan tokens and
**reject `<branch>`** explicitly (it resolves to empty rather than erroring).
Return a clear `invalid prefix: …` error surfaced to both Settings and CLI.

### 4. Resolution at create time (reuse)

Resolution stays in the TUI, reusing `template.Resolve` and the popup's `tctx()`
(`Now`/`Rand`/`Seqs`/`ParentBranch`/`Repo`). Because a prefix may contain
interactive `<user:LABEL>` tokens, selecting it runs through a small **fill
step** (see §5) that collects those labels into an `inputs` map before
`template.Resolve`. The resolved string is inserted into the branch-name buffer.

This "collect a template's `<user>` labels + peek its `<seq>` + resolve" flow is
the same one the worktree popup already implements (`stInput` → `worktree.Resolve`
+ `PeekSeqs`). It is factored into a small reusable TUI helper
(`templateFill` — a textfield-per-label collector returning the resolved string)
consumed by both the prefix picker and the worktree popup, so the logic is not
duplicated.

**`<seq>` in a prefix**: the prefix's `<seq>` names are peeked (via
`worktree.PeekSeqs` against the same fixed `seqs` snapshot used for any preview)
and the counters are bumped only on actual create. The worktree popup already
does this via `pendingSeqBump`; the create-branch popup gains the same peek +
bump-on-success plumbing. A used prefix's `<seq>` names are unioned into the
consumed-seq set.

### 5. TUI — select-only picker popup

A new popup `prefixPickerPopup` styled like the bookmark `g` / shelf `G`
switchers (single-column list, `/` filter, `enter` select, `esc` cancel). Rows
are grouped/tagged **[Global]** / **[Repo]**, each showing the raw template and a
live-resolved preview (resolved against the opener's ctx). Empty store → a hint
row pointing at Settings.

**Select → fill → insert.** On `enter` over a prefix row:

1. If the prefix has interactive `<user:LABEL>` tokens
   (`template.UserLabels(value)` non-empty), push the `templateFill` step: one
   focused textfield per label (`tab`/`enter` advances, `esc` backs out to the
   picker). Otherwise skip straight to resolve.
2. Resolve the prefix with the collected `inputs` + the opener's `tctx()`
   (peeking the prefix's `<seq>` names).
3. Insert the resolved string into the branch-name buffer, cursor at end.

Entry points:

- **Create-branch popup** (`branch_popup.go`): new key **`p`** ("use a prefix").
  After fill+resolve → set the `name` field value to the resolved string, cursor
  at end → user continues typing. (This popup gains a `tctx()`/seq-peek helper and
  the `templateFill` step; today it has none.)
- **Create-worktree popup** (`worktree_popup.go`): in `stAction`, add
  **`[p] use a prefix & edit`** next to `[e] edit name`. After fill+resolve →
  enter the existing `stEdit` state with `editBuf` seeded to the resolved string
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
3. If the prefix has `<user:…>` labels, the `templateFill` step collects them.
4. Popup resolves the selected `Value` with the collected `inputs` + its
   `tctx()` (peeking `<seq>`).
5. Resolved string lands in the branch-name buffer, cursor at end; user appends
   the free-text tail and creates as normal.
6. On successful create, used `<seq>` counters (worktree template + prefix) are
   bumped.

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
  (accept `<date>`/`<seq:NAME:N>`/`<random>`/`<parent-branch>`/`<repo>`/
  `<user:LABEL>`; reject `<branch>`, unknown/malformed tokens); scope routing —
  with injected temp stores.
- `internal/tui`: `templateFill` collects `<user>` labels then resolves
  (table-driven: no-label fast path, multi-label, `<seq:NAME:N>` padding,
  `<user>` + tail append); picker open/filter/select inserts the resolved string
  into the field (both popups); worktree `p` seeds `stEdit`; Settings
  add/edit/remove round-trip through injected stores; `<seq>` peek-vs-bump.
- `internal/cli`: `gg prefix add/ls/rm` happy paths + `--global` scope +
  invalid-token rejection (`<branch>`/malformed) (FakeRunner / temp state dir).
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
```
