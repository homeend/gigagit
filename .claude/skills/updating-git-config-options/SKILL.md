---
name: updating-git-config-options
description: Use when a gigagit feature needs to READ or WRITE a git config option (git config --local/--global) — the verbs, the SetGitConfig engine op, why writes are ops, and how the TUI/notice/Settings surfaces share one path.
---

# Updating git config options

## The stack, bottom to top

1. **Verbs** (`internal/git/config.go`):
   `ConfigGet(ctx, scope, key) (value string, set bool, err error)` — exit 1
   = unset, not an error; `ConfigSet(ctx, scope, key, value) error`.
   Scopes: `git.ConfigLocal` / `git.ConfigGlobal` / `git.ConfigEffective`
   (merged; get-only). One verb = one `git config` invocation.
2. **The op** (`internal/engine/set_git_config.go`):
   `engine.SetGitConfig{Key, Value string, Global bool}` — decision-free,
   `LockMode() = repogate.Read`. **Why an op and not a direct verb call:**
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
- Stage 3's git-config explorer will add set/unset on curated keys through
  the same op (extend with `Unset bool` + a `git.ConfigUnset` verb there —
  do NOT add a second write op).

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
