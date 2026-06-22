# User identity & app profiles — design

**Date:** 2026-06-22
**Status:** Approved (design); implementation plan pending

## Summary

Add the ability to view and edit the git **user identity** (`user.name` /
`user.email`) that lands in commits — distinguished by scope (git **global**
`~/.gitconfig` vs repo-**local** `.git/config`) — and a list of named **app
profiles** (`{name, email}` presets) the user can **apply** onto either the
global or repo-local git identity with one keystroke.

This is the first feature in gg that **writes git config**. (The TOML config
under `internal/config` remains read-only at runtime; only git config and the
new profile side-store are written.)

## Concepts

- **Git identity** — the real `user.name` / `user.email` git records in commits,
  living at git's **global** or repo-**local** scope. gg reads and writes these
  via thin `git config` verbs.
- **App profile** — a named `{name, email}` preset the user defines in gg (e.g.
  "Work", "Personal", "OSS-acme"), stored in a new writable side-store. Applying
  a profile writes its values into the chosen git scope.

Concepts A (edit live identity) and B (apply a named profile) share **one write
path**: applying a profile and editing the identity inline both feed the same
engine op with different value sources.

## Decisions (from brainstorming)

| Decision | Choice |
|----------|--------|
| Scope of feature | Both: view/edit live identity **and** named profiles |
| Profile storage | In-app, writable side-store (not TOML config) |
| Apply target | **Ask each time** — "this repo (local) or globally?" |
| Profile scope | **Two scopes** — global profiles (everywhere) + project-specific (this repo only) |
| Frontend scope | **Engine + TUI now**; `gg identity` / `gg profile` CLI + e2e deferred |

## Entry point & surface

Lives in the **Settings popup** (`,`) — its existing menu/picker split was built
for exactly this kind of future option. A new menu row **"Identity & profiles"**
opens an identity sub-surface:

```
Identity & profiles

Current identity
  Global   Ada Lovelace <ada@global.example>
  Repo     (not set — commits here use global)
  Effective Ada Lovelace <ada@global.example>

Profiles
  ● Work       Ada Lovelace <ada@work.example>      [global]
    Personal   Ada L. <ada@home.example>            [global]
    OSS-acme   ada-oss <ada@acme.dev>               [this repo]

[e]dit identity  [n]ew  [r]ename  [d]elete  [enter] apply  [esc]
```

- **Current identity** block — read-only display. Global / repo-local / effective
  lines are genuinely distinguished (see Reads below): "not set locally" reads
  differently from "inherits global X".
- **Profiles** — named presets, grouped/tagged by scope (`[global]` vs
  `[this repo]`).
- `enter` on a profile → prompts **"Apply to: [r] this repo / [g] globally"** →
  writes git config at the chosen scope.
- `e` → inline edit of the live identity (name/email text fields) → same apply
  prompt.
- `n` / `r` / `d` → create / rename / delete a profile (in the side-store). New
  profiles prompt for scope (global vs this repo).

The sub-surface is a real TUI surface (list + create/edit/delete + local/global
prompt); it will be designed against the `adding-tui-windows` skill and reuse
`internal/tui/textfield.go` for the name/email inputs.

## Architecture

### New package: `internal/profile`

Mirrors `internal/bookmark`'s shape, but **two-scoped**.

- `model.Profile{ ID, Name, GitName, GitEmail, Scope }`, `Scope ∈ {Global, Repo}`.
- `profile.Store` interface: `Add`, `Get`, `List`, `Remove`.
- `FileStore` — `profiles.toml` written via atomic rewrite (same as bookmark's
  `file_store.go`).
- **Two FileStore instances** (the key difference from bookmark, which is
  single-scope per-repo):
  - Global → `<state>/gg/profile/global/profiles.toml`
  - Per-repo → `<state>/gg/profile/<repoKey>/profiles.toml` (keyed by git common
    dir, reusing `repoKey`).
- `domain` owns both stores, **lists/merges** them (tagging each row with its
  scope), and routes `Add`/`Remove` to the correct store by the profile's
  `Scope`. Frontends never import `profile` (archtest-guarded, like
  bookmark/shelf).

### Git identity verbs: `internal/git`

One git invocation each:

- `ConfigGet(ctx, scope, key) (value string, set bool, err error)` —
  `git config --local --get <key>` or `git config --global --get <key>`.
  **Exit code 1 = unset → returns `("", false, nil)`**, not an error. Use
  explicit `--local` / `--global`, never plain `--get` (plain `--get` returns the
  effective merged value and would make an unset local identity falsely echo the
  global one).
- `ConfigSet(ctx, scope, key, value) error` —
  `git config --local|--global <key> <value>`.

### Domain query: `Identity`

`Identity(ctx) (model.Identity, error)` reads under a Read reservation:

```
model.Identity{
  GlobalName, GlobalEmail string; GlobalSet bool
  LocalName,  LocalEmail  string; LocalSet  bool
  EffectiveName, EffectiveEmail string   // plain --get, "what commits will use"
}
```

Renders the "Current identity" block, distinguishing local from global from
effective.

### Engine op: `SetIdentity` (the one write path)

`SetIdentity{ Name, Email string; Global bool }` — decision-free (like
`CreateTag`; the scope is known before any work, so no `Decider` fork):

1. emit `Progress`,
2. `ConfigSet` name then email at the chosen scope,
3. return `Result{ Changed: true }`, emit `Done`.

Fed from two sources, no parallel paths:
- **Edit identity** → typed text.
- **Apply profile** → the profile's saved values.

**Locking & refresh:** a config write touches neither refs nor tree, and a
*global* write isn't repo-scoped at all — so the op declares the lightest
`LockMode` (Read-level). On completion the TUI does a **targeted identity
re-read only** (re-run `Identity`), **not** a full `Snapshot` — applying changes
no status/refs (the partial-refresh lesson from worktree-create).

## Testing

- **Git verbs** — real-git tests in `t.TempDir()`: set name/email locally and
  globally, assert `ConfigGet` distinguishes scopes and that unset returns
  `(_, false, nil)` not an error.
- **Global-write safety** — any test writing *global* config first sets
  `GIT_CONFIG_GLOBAL` to a temp file (and `GIT_CONFIG_SYSTEM=/dev/null`) so it can
  never touch the developer's real `~/.gitconfig`. Verify whether
  `newRepo`/`newTestRepo` already isolates this before writing the first
  global-write test; add isolation if not.
- **Profile store** — unit tests on the two-scope FileStore: add/list/remove per
  scope, atomic rewrite, merge tagging.
- **Engine op** — real-git test that `SetIdentity` writes the right scope;
  integration test proving "apply profile" and "edit identity" hit the same op.
- **TUI** — render-path test for the identity sub-surface (the
  green-unit/broken-render lesson from the Reflog panel); designed against
  `adding-tui-windows`.

## Out of scope (deferred follow-up)

- `gg identity` (show/set) and `gg profile` (list/add/rm/apply) CLI commands.
- agentskill bump + e2e scenarios.

The engine op + domain queries are built CLI-ready, so the follow-up is thin
wiring (a `cliDecider`/flag-policy maps the local-vs-global choice; the op and
queries are reused as-is).

## Docs to update on completion

`CHANGELOG.md` (always); `README.md` (new user-facing surface);
`CLAUDE.md` package map (new `internal/profile` package + first git-config
write); a `[ui]`/config note only if a config key is added (none planned).
