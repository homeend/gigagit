# Post-worktree-create hook — design

**Date:** 2026-06-30
**Branch:** `feat/post-worktree-hook`
**Status:** approved, pre-implementation

## Goal

After gg creates a new worktree, run a user-configured shell script inside that
new worktree. The motivating use case: copy gitignored files that live only in
the main checkout's working tree — `.env`, local config, build tooling,
`node_modules` symlinks — into the fresh worktree, which starts without them
because they are not in git.

The script is configured **per repository**, edited in the TUI through a wide
multi-line editor, and runs **automatically** on every worktree creation (TUI
and CLI alike), with a per-create opt-out.

## Decisions (locked)

| Decision | Choice |
|----------|--------|
| When it runs | Automatically after every successful worktree creation, with a per-create skip toggle. |
| Where it runs | Inside the **engine operation**, so TUI, CLI, and future MCP all get it. Non-interactive: output streams as `GitLine` events. |
| Execution model | Run via the user's `$SHELL -c <script>` (fallback `/bin/sh`; `cmd /C` on Windows). One script, full shell features (pipes, `&&`, env expansion). |
| Run context | `cwd` = the new worktree; injected `GG_*` env vars (see below). |
| Storage | Multi-line **literal** TOML string (`'''…'''`) under `[worktree] post_create_hook` in the **repo** `.gg.toml`. Literal (not `"""`) so backslashes/quotes in scripts survive verbatim. Limitation: the script cannot contain `'''` (documented). |
| Editor | New 80%-screen-width scrolling multi-line popup in the TUI. `Enter` inserts a newline; `Ctrl+S` saves; `Esc` cancels. |
| Hook failure | Non-fatal. The worktree already exists, so the op still returns success (`Changed:true`); a clear `exit N` line is streamed and noted in `Result.Summary`. Never rolls back. |

## Injected environment

The script can rely on these (in addition to the inherited process environment):

| Var | Value |
|-----|-------|
| `GG_MAIN_WORKTREE` | absolute path of the main checkout — the copy **source** |
| `GG_WORKTREE_PATH` | the new worktree — also `cwd`, so the copy **destination** is `.` |
| `GG_BRANCH` | the new branch name |
| `GG_REPO` | the repo name |

Example hook:

```sh
cp "$GG_MAIN_WORKTREE/.env" .
cp "$GG_MAIN_WORKTREE/config/local.json" config/
ln -s "$GG_MAIN_WORKTREE/node_modules" node_modules
```

## Why a new runner seam

`internal/gitexec`'s `Runner` is **git-locked**: `ExecRunner` hardcodes the
`git` binary (`exec.CommandContext(ctx, r.gitPath, argv...)`), pins `cmd.Dir` to
the repo working dir, and `Stream` accepts neither a custom cwd nor env. It
therefore cannot run the user's `$SHELL`. The hook needs its own small,
fakeable process-runner seam.

## Component design

### 1. `internal/config`

- Add `WorktreeConfig.PostCreateHook string` with tag `toml:"post_create_hook"`
  (`config.go`, after `BranchTemplates`). Default empty = disabled.
- `go-toml/v2` parses multi-line literal strings natively — no decode changes
  beyond the new field.
- Overlay: standard string zero-is-unset block in `overlayWorktree`
  (`if src.PostCreateHook != "" { dst.PostCreateHook = src.PostCreateHook }`).
- New writer `SetWorktreePostCreateHook(path, script string)` in `write.go` —
  the first **multi-line** config writer. It locates an existing
  `post_create_hook = '''` … closing `'''` span under `[worktree]` (single- or
  multi-line form) and replaces it; otherwise inserts after the section header,
  or appends a new `[worktree]` section. Writes the block form
  (`post_create_hook = '''\n<script>\n'''`) and reuses the existing atomic
  temp-file rewrite. Empty script → remove the key.
- One `settingDocs` entry (comment-only, `value: nil`) in `template.go` so
  `gg config init`/`populate` document it and `TestSettingDocsCoverAllFields`
  passes. The comment states the env vars and the `'''` limitation.

### 2. `internal/engine`

- Add `PostCreateHook string` to **both** `CreateWorktree` and
  `CreateWorktreeForBranch`. Empty = no hook. The engine stays config-agnostic;
  the frontend decides whether to populate it.
- New `OpDeps.HookRunner` seam:
  ```go
  type HookSpec struct { Dir string; Env []string; Script string }
  type HookRunner interface {
      Run(ctx context.Context, spec HookSpec, onLine func(string)) (exitCode int, err error)
  }
  ```
  Real impl `ShellHookRunner` resolves `$SHELL -c` (fallback `/bin/sh -c`;
  `cmd /C` on Windows), sets `cmd.Dir`/`cmd.Env`, and streams stdout+stderr
  lines to `onLine`. Faked in tests.
- Shared helper `runPostCreateHook(ctx, deps, absWorktree, mainWorktree, branch, repo, script)`
  in a new `post_create_hook.go`, called by both ops after a successful
  `AddWorktree`:
  - emit `Progress{Step: "running post-create hook"}`
  - build the env (inherited + the four `GG_*` vars)
  - stream each output line as a `GitLine`
  - on non-zero exit / error: emit a clear `GitLine` and append a note to
    `Result.Summary`; **do not** return an error (non-fatal).
- The main-worktree root is already computed by `resolveNewWorktreePath`; reuse
  that to populate `GG_MAIN_WORKTREE`.
- `domain.Execute` injects the real `ShellHookRunner` into `OpDeps`.

### 3. `internal/tui`

- New `hookEditorPopup` (`hook_editor.go`): an 80%-width multi-line editor built
  on the existing `textfield` (already supports `\n`, Up/Down, home/end,
  word-jump) plus a vertical scrolling viewport keyed to the cursor line; long
  lines clipped with horizontal scroll on the cursor row. Keys: printable +
  `Enter` → insert newline; arrows/home/end via `textfield`; `Ctrl+S` save;
  `Esc` cancel. Help line shows the keys.
- Settings (`,`) menu gains a **"Worktree post-create hook"** entry that opens
  the editor seeded with `cfg.Worktree.PostCreateHook`. On save: write to the
  **repo** `.gg.toml` via `SetWorktreePostCreateHook`, update in-memory
  `m.cfg`, close.
- Per-create skip: when a hook is configured, the worktree popup's action view
  shows `[x] run post-create hook` (key `h` toggles a `runHook` bool, default
  on). `createOp()` sets `PostCreateHook` only when enabled.

### 4. `internal/cli`

- The worktree-create command sets `PostCreateHook` from
  `cfg.Worktree.PostCreateHook`; new `--no-hook` flag skips it. CLI already
  prints streamed `GitLine` output.

### 5. Docs

- `CHANGELOG.md` (always), `README.md` (new TUI surface + config key),
  `CLAUDE.md` (new `[worktree]` field, the `HookRunner` engine seam, the new TUI
  popup, env-var contract).
- `internal/agentskill/using-gg.md` for the `--no-hook` flag; bump
  `agentskill.Version`; `gg init --update`.
- Follow the `adding-config-entries` and `adding-tui-windows` skills.

## Testing (TDD)

- **config**: parse a `'''…'''` multi-line value into `PostCreateHook`; overlay
  (repo over global over default); `SetWorktreePostCreateHook` round-trip
  (write → re-read), replace an existing block, insert into a file with/without
  the section, empty-script removal; `settingDocs` coverage test stays green.
- **engine**: with a fake `HookRunner` — `CreateWorktree` and
  `CreateWorktreeForBranch` run the hook with the correct `Dir`, the four
  `GG_*` env vars, and `Script`, streaming lines as `GitLine`; hook non-zero
  exit is non-fatal and noted in `Summary`; empty hook → runner not called.
- **ShellHookRunner**: run a tiny `echo`/`printenv` script in a temp dir,
  assert captured lines, cwd, and env (`GG_MAIN_WORKTREE` etc.); guard for
  Windows shell.
- **TUI**: editor popup edits + `Ctrl+S` writes config; worktree popup `h`
  toggle yields an op with/without `PostCreateHook`. Follow existing popup test
  patterns.

## Out of scope (YAGNI)

- Interactive hooks (TTY/password prompts) — non-interactive streaming only.
- Pre-create or post-remove hooks; other lifecycle events.
- A separate hook-script file or a `.git/hooks`-style directory.
- Per-template or per-branch hook variants.
