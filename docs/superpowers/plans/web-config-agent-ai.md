# Task 6 — git config, agent setup, AI commit messages, amend

**Depends on task 0.** Read `README-web-parallel-tasks.md` first.

Four settings-shaped gaps that all live behind their own overlays, so they
share no surface with the other five tasks.

## Your files

| Server | Client |
|--------|--------|
| `internal/web/gitconfig.go` (new) | `static/gitconfig.js` (new) |
| `internal/web/agentsetup.go` (new) | `static/agentsetup.js` (new) |
| `internal/web/op_ai.go` (new) | |
| tests alongside each | |

Overlays mount themselves with `mountOverlay`; entries go in the ☰ menu with
`registerRows("menu", …)` (task 0 registers the ☰ groups the same way as the
context menus); help via `registerHelp`. Do not touch `ophttp.go`,
`server.go`, `settings.js`, `commits.js`, `files.js` or `index.html`.

## What already exists

- `internal/gitconfdocs` — a curated catalog of ~64 git config keys with
  defaults and kinds, already used by the TUI's config explorer, and kept
  honest by a staleness test against `git help -c`. It is a pure package with
  no git dependency: read it directly.
- `engine.SetGitConfig{Key, Value, Global bool}` — already wired in the web,
  but only ever for identity. The key/value must not come off the wire
  unchecked: resolve the key against the catalog and refuse anything else.
- `internal/agentinit` — the hardcoded agent registry plus detect / status /
  install behind `gg init` and the TUI's Settings popup. Same shape: read
  status, offer install, report what changed.
- `engine.GenerateMessage{Command, Dir, Env}` — runs a configured
  `commit_message` external tool headlessly and captures its output; the
  `$GG_MESSAGE_FILE`-wins-over-stdout contract belongs to `internal/exttool`.
  The web already runs the **review** and **conflict-complete** categories
  this way (`review.go`, `conflictai.go`) — copy that lane, including the
  first-run command approval (`promptstate.CommandHash`) and the parked run
  chip.
- `engine.Commit{Message, All, Amend bool}` — the web sends `Amend: false`
  always. The TUI's `C` opens the commit popup prefilled with HEAD's message.
- `GET /api/commit-message?rev=<hex>` already exists (it backs reword) and
  gives you HEAD's full message for the amend prefill.

## Work

1. **Git config explorer.** An overlay listing the catalog: key, current
   effective value, default, scope. Editing writes through `SetGitConfig` with
   the key resolved against the catalog (an unknown key is a 400, not a write).
   Show which scope a value came from, and let the user pick global or repo
   when writing — the identity view already models that choice.
2. **Agent-skill setup.** An overlay over `agentinit`: which agents are
   detected, which are installed, what installing writes. One action per agent,
   the result reported plainly. This is the web's `gg init`.
3. **AI commit message.** In the commit box, a *generate message (AI)* action
   that runs the configured `commit_message` tool over the staged diff and
   fills the message field for review. Nothing commits by itself. An
   unapproved command shows the approval step first; an existing draft asks
   before being replaced; cancelling stops the run.
4. **Amend.** An *amend the last commit* lane on the commit box: prefill from
   `/api/commit-message?rev=HEAD`, send `Amend: true`. It rewrites history, so
   it needs a confirm that says so, and it must be refused (with the engine's
   own words) when there is nothing to amend.

## Acceptance

- Go tests: the config list contains a known catalog key with its default;
  writing a catalog key changes `git config` in the chosen scope; a key
  outside the catalog is 400; agent status reflects a fixture install; a
  generate-message run with a fake capture tool returns its text (see
  `review_test.go` for the fake-tool pattern); amend replaces HEAD's message
  and keeps its parent.
- Browser, control run first: the config overlay opens, shows a value, and an
  edit persists across a reload; the agent overlay lists agents; generating a
  message fills the commit box without committing; amend rewrites the last
  commit and the feed shows the new subject at the same position.
- `./test.sh race` green. CHANGELOG bullet. `registerHelp` row.

## Notes

- **Never let a config key or a command line come off the wire.** The key is
  resolved against the catalog; the command comes from the user's own
  `[[tools.command]]` blocks and is approved by hash. This is the boundary
  that keeps a loopback server from being a remote-execution surface.
- The AI lanes park: a run continues while you look elsewhere and reattaches
  through the chip. Reuse `review.js`'s transport rather than inventing a
  second one — one lane at a time is a deliberate constraint, not a limitation
  to route around.
- Amend after a push means a force-push next. Say that in the confirm; the
  force-push lane already exists and its own modal handles the rest.
