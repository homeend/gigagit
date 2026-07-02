---
name: updating-git-config-options
description: Use when a gigagit feature needs to READ or WRITE a git config option (git config --local/--global) — the verbs, the SetGitConfig engine op, why writes are ops, and how the TUI/notice/Settings surfaces share one path.
---

# Updating git config options

## The stack, bottom to top

1. **Verbs** (`internal/git/config.go`):
   `ConfigGet(ctx, scope, key) (value string, set bool, err error)` — exit 1
   = unset, not an error; `ConfigSet(ctx, scope, key, value) error`.
   Also `ConfigUnset(ctx, scope, key) error` — `git config --unset`; exit 5
   (key absent) is a no-op success, so unset is idempotent. Read-side:
   `ConfigKeys(ctx)` (the `git help -c` catalog) and `ConfigListScoped(ctx)`
   (`git config --list --show-scope -z`; local/global only; -z survives
   multiline values; git lowercases set keys — join against the camelCase
   catalog case-insensitively).
   Scopes: `git.ConfigLocal` / `git.ConfigGlobal` / `git.ConfigEffective`
   (merged; get-only). One verb = one `git config` invocation.
2. **The op** (`internal/engine/set_git_config.go`):
   `engine.SetGitConfig{Key, Value string, Global, Unset bool}` — decision-free,
   `LockMode() = repogate.Read`. `Unset: true` removes the key (Value ignored); 
   one op for set AND unset, per the spec's explicit decision. **Why an op and not a direct verb call:**
   frontends may not import `internal/git` (archtest), and running through
   `domain.Execute` buys the repo-gate reservation, op events (busy line,
   oplog spans), and error surfacing for free. `Global` is a bool, not
   `git.ConfigScope`, precisely so the TUI can construct it.
   (`SetIdentity` remains the dedicated user.name/email pair op.)
3. **Reads from a frontend** go through a domain query, never a raw verb:
   `domain.Identity` (both scopes distinct) and `domain.RepoHealth`
   (`fetch.writeCommitGraph`, local-then-global) are the exemplars.

## Choosing scope

- Repo-specific behavior (e.g. `fetch.writeCommitGraph` for one big repo):
  **local** (`Global: false`) — never surprise the user's other repos.
- User preference expressed as "always": **global**, and only when the user
  explicitly chose it.

## The surfaces that share this path

- Notice actions (`notify.go`): `m.startOp(engine.SetGitConfig{…})`, or
  chained after another op via `m.pendingNoticeConfig` (the
  `pendingPushTags` chain pattern in `opFinishedMsg`).
- Settings rows (`settings_popup.go`): the "Commit-graph" row calls the SAME
  shared func as the notice action (`startCommitGraphWriteAndEnable`) — one
  code path, two entrances.
- The git-config explorer (Settings → "Git config explorer",
  `internal/tui/gitconfig_popup.go`): browse every catalog key with
  local/global/default columns; curated rows (`internal/gitconfdocs`) edit
  via `l`/`g`/`u` → `gitConfigWriteCmd` → `domain.Execute(SetGitConfig)`
  (the stageCmd synchronous pattern — config writes are fast and
  decision-free), then re-read rows + repo health in one message.

## Maintaining the curated table (internal/gitconfdocs)

- One `Doc` per key: `Key` (camelCase, exactly as `git help -c` prints it),
  `Kind` (bool/enum/string/int — picks the editor), `Default` (git's real
  default, `"(none)"` when git has none — never gg's opinion), `Desc` (one
  line), `Options` (enums only).
- `TestCuratedKeysExistInGitCatalog` is the staleness gate: every curated
  key must exist in the local `git help -c` (skipped when git is absent).
  If it fails after a git upgrade, fix or remove the entry — do not skip.
- Lookup is case-insensitive (`byLower`) because git lowercases set keys.
- Curated ⇒ writable in the explorer; think before adding keys whose
  values are dangerous to flip blindly (e.g. `core.fileMode` on the wrong
  filesystem) — the Desc should carry the caveat.

## Tests

- Verb argv: `gitexec.FakeRunner` + `f.Calls[0].Argv` assertion.
- Op behavior: real git in `t.TempDir()` (`newRepo(t)` in engine tests) with
  `t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))`
  and `t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)` so the developer's real
  config is never read or written. Assert the OTHER scope untouched.
- TUI wiring: `driveOp(t, m, cmd)` then read the value back with raw
  `exec.Command("git", "-C", dir, "config", "--local", key)`.

## Rules

- `internal/config` (`.gg.toml`) stays read-only at runtime except its
  registered line-edit writers — git config and gg config are different
  systems; don't cross them.
- Never shell out to `git config` from the TUI/CLI directly — archtest will
  fail the build, and you'd skip the gate + oplog.
