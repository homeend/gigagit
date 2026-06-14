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

The footer is contextual: it lists only the keys that apply to the focused
panel and selected row right now; `?` opens the full searchable reference.

| Key | Action |
|-----|--------|
| `p` / `P` | pull / push |
| `s` | smart-switch to the selected branch |
| `b` | create a branch off the selected one (popup); `B` create **and** switch to it |
| `S` | stash |
| `u` | undo last commit (ref-only, soft reset) |
| `w` | create a worktree **for the selected branch** (popup); `W` worktree on a **new** templated branch. Inside the popup: `w`/`enter` create, `W` create **and** switch |
| `enter` | on the Worktrees panel: switch into the selected worktree; on the Status panel: full-screen side-by-side diff of the file (HEAD → working tree); on the files-view tree: diff of the file in the viewed commit. Inside the diff: `↑`/`↓` scroll, `pgup`/`pgdn` page, `n`/`p` (or `ctrl+↑`/`ctrl+↓`) jump between changes, `f` toggles full file ↔ changed-lines-only, `w` cycles long-line mode (scroll/wrap/truncate), `←`/`→`/`0` pan in scroll mode, `esc` closes. Changed lines highlight the exact words that differ; commit diffs are cached for instant re-open |
| `space` | on the Status panel: stage the selected file (`git add`), or unstage it (`git restore --staged`) when it is already fully staged; conflicted files are skipped |
| `d` | on the Worktrees panel: delete the selected worktree; on the Branches panel: delete the selected branch |
| `m` | mark the selected row; press `m` on a second row of the same panel to open the pair-operation picker (Branches: Merge; `esc` clears the mark before clearing the filter) |
| `l` | on the Commits panel: show the selected commit's files as a directory tree in the left column (`←`/`→`/`tab` switch focus between the tree and the commit list; movement keys act on the focused side — the commits side reloads the tree; `ctrl+↑`/`ctrl+↓` always scroll the tree; `/` searches paths; `esc`/`l` close) |
| `h` | file **history**: on a Status-panel file, a files-view tree row, or inside the diff view — opens the commits that touched the file (left, newest first) with the file's diff at the selected commit (right); `↑`/`↓` move between commits, `esc`/`h` go back |
| `b` | file **blame**: same entry points as `h` — opens the file with each line tagged by the commit that last changed it (consecutive same-commit lines grouped); `enter` opens that commit's history, `esc`/`b` go back |
| `tab` | move focus between panels |
| `shift+tab` | move focus backwards |
| `←`/`→` | focus the left column / the Commits panel (inside the files view: switch between the file tree and the commit list) |
| mouse | click focuses the window under the cursor and selects the clicked row; the wheel scrolls the hovered list (`[ui] wheel_step` rows per tick) |
| `j`/`k` or `↑`/`↓` | move selection |
| `pgup`/`pgdn` | move selection by 25% of the panel viewport |
| `o` | cycle the focused panel's sort order (name/date, asc/desc) |
| `/` | filter the focused panel (type, then `enter` to keep, `esc` to clear) |
| `R` | switch repository (popup: type to filter, `enter` to switch, `ctrl+d` to forget) |
| `,` | settings (set up agent skills) |
| `r` / `q` | reload / quit |
| `?` | help: searchable list of all key bindings (`/` to search; `↑`/`↓` or `j`/`k`, `ctrl+↑`/`ctrl+↓`, `pgup`/`pgdn`, mouse wheel to scroll; `q` closes) |

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
gg branch create <name> [<start-point>]
gg branch delete [--force] <name>
gg merge [--into <target>] [--on-conflict=keep|abort] <source>
gg rebase [--branch <b>] [--on-conflict=keep|abort] <newbase>
gg stash [-m msg]
gg undo
gg worktree list
gg worktree add [<start-point>]
gg worktree add --branch <name>
gg worktree remove [--with-branch] [--force] <path>
gg repo list
gg repo switch <query>
gg init [--all | --update | --agents <ids> | --list]
gg inspect [--debug-dump <path>] [--trace]
```

Forks are answered by flags (e.g. `--on-conflict`, `--with-branch`/`--force`);
without a flag, an interactive terminal prompts, and a non-interactive run errors
asking for the flag.

Every command (and the TUI) accepts a global `--time-track <file>` flag that
appends one JSON span per process start, git subprocess, and operation —
`jq . gg-perf.log` shows where the time went.

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

`[ui] wheel_step` sets the mouse-wheel scroll step in rows (default 3);
`[ui] hscroll_step` sets the diff scroll-mode pan step in columns (default 8);
like every entry, the repo's `.gg.toml` overrides the global config
per field.

## Development

```bash
go test ./...                # -race before merging
go vet ./... && gofmt -l internal/ cmd/
```

The `e2e/` directory contains a declarative scenario harness: TOML files in
`e2e/scenarios/` describe a starting repo state, a sequence of `gg` CLI
commands, and the expected user-visible outcome (files, branches, stashes, sync
state, history shape). Scenarios are run as standard Go tests and cover
SmartSwitch, SmartPull, stash, commit+push, undo, and worktree add/remove.

See [`CLAUDE.md`](CLAUDE.md) for architecture and contributor conventions.
