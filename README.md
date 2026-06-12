# gigagit (`gg`)

A fast terminal git client for very large monorepos — GitKraken's one-key smart
operations with lazygit's keyboard-driven TUI. Cross-platform, shells out to the
system `git`.

> **Status:** early development. Core engine, smart operations, an interactive
> TUI, a scriptable CLI, and full worktree management are in place. See
> [`CHANGELOG.md`](CHANGELOG.md) for details and [`CLAUDE.md`](CLAUDE.md) for the
> architecture.

## Why

Huge repos make ordinary git slow and stateful operations error-prone. `gg`
turns multi-step flows (pull-with-divergence, switch-with-local-changes,
worktree create-and-cd) into single keystrokes that ask you a focused question
only when there's a real decision to make.

## Install

Requires Go 1.26 and a `git` binary on `PATH`.

```bash
# from a checkout
go build ./cmd/gg            # produces ./gg
# or cross-compile both targets
./build.sh all               # ./gg (linux) and ./gg.exe (windows)
```

## Usage

### TUI

Run `gg` with no arguments to open the interactive UI.

| Key | Action |
|-----|--------|
| `p` / `P` | pull / push |
| `s` | smart-switch to the selected branch |
| `S` | stash |
| `u` | undo last commit (ref-only, soft reset) |
| `w` | create a worktree (popup); `W` create **and** switch into it |
| `enter` | on the Worktrees panel: switch into the selected worktree |
| `d` | on the Worktrees panel: delete the selected worktree |
| `tab` | move focus between panels |
| `shift+tab` | move focus backwards |
| `j`/`k` or `↑`/`↓` | move selection |
| `pgup`/`pgdn` | move selection by 25% of the panel viewport |
| `o` | cycle the focused panel's sort order (name/date, asc/desc) |
| `/` | filter the focused panel (type, then `enter` to keep, `esc` to clear) |
| `r` / `q` | reload / quit |

When an operation hits a fork (e.g. a diverged branch, or a worktree with
uncommitted changes), a modal asks you to choose; `↑`/`↓` + `enter` to pick,
`esc` to take the safe default.

### CLI

Every smart operation is also scriptable:

```bash
gg status
gg commit -m "msg"            # add -a to stage tracked changes
gg pull [--background] [--on-conflict rebase|merge|abort]
gg push
gg switch <branch>
gg stash [-m msg]
gg undo
gg worktree list
gg worktree add [<start-point>]
gg worktree remove [--with-branch] [--force] <path>
gg inspect [--debug-dump <path>] [--trace]
```

Forks are answered by flags (e.g. `--on-conflict`, `--with-branch`/`--force`);
without a flag, an interactive terminal prompts, and a non-interactive run errors
asking for the flag.

### Shell integration (cd-on-switch)

So switching/creating a worktree can move your shell into it:

```bash
# bash/zsh
eval "$(gg shell-init bash)"
# fish
gg shell-init fish | source
```

## Configuration

Optional `.gg.toml` in the repo (overlaid on a global config) configures worktree
branch/path templates with tokens like `<parent-branch>`, `<repo>`,
`<date:YYYY-MM-DD>`, `<seq:NAME:N>`, and `<user:LABEL>`. Per-repo `<seq>` counters
live in `<git-common-dir>/gg/state.toml`.

## Development

```bash
go test ./...                # -race before merging
go vet ./... && gofmt -l internal/ cmd/
```

See [`CLAUDE.md`](CLAUDE.md) for architecture and contributor conventions.
