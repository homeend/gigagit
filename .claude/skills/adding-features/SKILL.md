---
name: adding-features
description: Use when adding a new gigagit feature, smart operation, or command that must be wired across the engine, TUI, CLI, or future MCP — or when reviewing whether a feature is fully wired end-to-end.
---

# Adding a Feature to gigagit

## Overview

Every feature has the same spine: **git verb(s) → engine `Operation` → frontends**.
The engine owns all logic and all guards; frontends only collect inputs and
answer decisions. **Decision IDs + option strings are the cross-frontend API** —
the CLI policy map and any MCP `MapDecider` answer forks by exact string match.

Exemplar to copy (newest full feature): delete-worktree —
`internal/git/worktree.go` (verbs), `internal/engine/remove_worktree.go`,
TUI `case "d"` in `internal/tui/model.go`, `cmdWorktreeRemove` in
`internal/cli/worktree.go`, plus their `_test.go` files.

## Wiring checklist (do ALL of these)

| # | Where | What |
|---|-------|------|
| 1 | `internal/git/<area>.go` | One verb = one git invocation. `gitcmd.New("sub").Arg(...).ArgIf(cond,...)`, run via `r.Runner.Run` (or `.Stream` with `onLine` for long output). Doc comment names the backtick git command. |
| 2 | `internal/engine/<op>.go` | Struct with fully-resolved fields (frontends resolve templates/input). `Run(ctx, deps OpDeps)`: guards fail fast **before** any decision; `deps.emit` Progress/GitLine; forks via `deps.decide(DecisionRequest{ID, Prompt, Options})` — **option lists only, never free text**; include `"abort"` while nothing has been done yet; after a point of no return offer only forward options (e.g. `force-delete`/`keep`). `Result{Summary, Changed[, Path]}`; emit `Done` **on success only**. End with `var _ Operation = X{}`. |
| 3 | `internal/tui/model.go` | Key case in the normal-key `switch msg.String()`: guard `!m.running && !m.loading` (+ `m.focus == panelX` if panel-specific), then `return m.startOp(op)`. Decisions render in the existing modal automatically — zero UI work. Needs free-text input (a name, a message)? Don't dodge it with a synthetic default — collect it in a popup first (**REQUIRED SUB-SKILL:** adding-tui-windows), then pass fully-resolved fields to the op. |
| 4 | `internal/tui/view.go` (~line 99) | **Add the key to the footer hint string.** (Most-missed step.) |
| 5 | `internal/cli/<file>.go` | `flag.NewFlagSet(name, flag.ContinueOnError)` + `fs.SetOutput(stderr)`; map flags → `policy map[string]string` keyed by **decision IDs**; `cliDecider{policy, in, out, interactive: stdinIsTerminal()}`; `runOperation(...)`; `return finish(res, err, stdout, stderr)` (don't inline the ✓ print). |
| 6 | `internal/cli/cli.go` | Register: `commands` map entry + `case` in `Run`'s switch. |
| 7 | `cmd/gg/main.go` (~line 37) | **Update the unknown-command help string** listing commands. (It drifts — verify it's complete while you're there.) |
| 7b | `internal/agentskill/using-gg.md` | **CLI surface changed (commands/flags/decision IDs)?** Update the embedded using-gg skill, bump `agentskill.Version`, and re-run `gg init` (or `gg init --update`) wherever it's installed. |
| 8 | Docs | `CHANGELOG.md` always; `README.md` key table + CLI list if user-facing; `CLAUDE.md` if the package map changed. |

MCP (future): nothing to wire today — forks get pre-answered via
`engine.MapDecider`, which is why free-text decisions are forbidden.

## Tests per layer (TDD, real git)

- **git verb:** `newTestRepo(t)` in `internal/git` — assert real behavior, not argv.
- **engine:** `newRepo(t)` + `MapDecider{"<id>": "<option>"}` + `drain(ch)`; cover every decision branch, abort paths (`Changed:false`), and guards.
- **TUI:** `loadedModel(t)`/`newRepoDir(t)`, `keyMsg("x")`, `driveOp` to drain ops; for ops with decisions, use the modal-handshake loop (see `decision_integration_test.go` or `worktree_delete_test.go`).
- **CLI:** `newCLIRepo(t)` + `Run(dir, args, stdin, stdout, stderr, cwdFile)`; cover each flag→policy mapping and the non-interactive missing-decision error.
- Finish: `gofmt -l internal/ cmd/`, `go vet ./...`, `go test ./... -race`.

## Common mistakes

| Mistake | Fix |
|---------|-----|
| Footer hint forgotten | Step 4 — `view.go:99`. |
| `cmd/gg/main.go` help list forgotten | Step 7 — and check it for drift. |
| Decision ID/option typo between engine and CLI policy | grep both; exact string match is the contract. |
| Emitting `Done` on abort/cancel paths | `Done` is success-only; aborts just return `Result{Changed:false}`. |
| Prompting/blocking inside an operation | Never. Always `deps.decide` with an option list. |
| TUI test only asserts `m.running` | Ops with decisions need the modal-handshake loop test. |

Workflow: feature branch off `main` (never `master`), brainstorm → spec →
plan → execute; the human merges.
