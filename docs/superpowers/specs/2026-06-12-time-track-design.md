# Performance Log (`--time-track <file>`) — Design Spec

**Date:** 2026-06-12
**Status:** Approved (approach A agreed in chat; this document records it)
**Scope:** A global `--time-track <path>` flag that appends one JSON line per
span — process start, every git subprocess, every engine operation — to a log
file, for both the TUI and every CLI subcommand. No TUI surface.

## Goal

`gg --time-track /tmp/gg.log` (or any `gg <cmd> --time-track …` run) produces
an analyzable record of where time goes: each user-level operation's total
duration with the git subprocess spans beneath it. Works across runs (append),
crash-safe (streamed, not dumped at exit), zero overhead when the flag is
absent.

## Format

One `observ.Span` per line, JSON-marshalled (the existing schema:
`id`, `name`, `args`, `exit_code`, `err`, `start`, `duration_ns`), **args
redacted** via `observ.Redact` — exactly the shape `gg inspect --trace` and
the panic dump already emit, so tooling is shared and `jq` works out of the
box.

Logged spans:

| Span | Name | Source |
|------|------|--------|
| Process start | `gg start` | `cmd/gg/main.go`, once, at t0; `Args` = redacted argv plus a final `version=<buildinfo version>` element; `Duration` 0. Delimits runs in the appended file. |
| Git subprocess | `git <verb>` (existing) | Already recorded by `gitexec.ExecRunner` into the `Ring`; mirrored to the sink by `Ring.Record`. |
| Engine operation | `op <Type>` (e.g. `op SmartPull`) | New: recorded around `op.Run` in `cli.runOperation` and `tui.startOp` via `observ.EmitSpan`. `ExitCode` 0 on success / 1 on error, `Err` = error text. |

No parent/child linking (`ParentID` stays 0): op spans and their git spans
correlate by time. The field exists if we ever want more.

## Mechanism (`internal/observ`)

The project-seam pattern (package var, zero value = disabled, wired only in
`cmd/gg/main.go` — the `cli.RepoStatePath` / `cli.InitHomeDir` precedent):

```go
// SetSpanSink routes a copy of every recorded span (ring and EmitSpan alike)
// to w as redacted JSON lines. nil disables (the default). Call once at
// startup; safe for concurrent recorders afterwards.
func SetSpanSink(w io.Writer)

// EmitSpan writes a synthetic span (process start, engine operation) to the
// sink. No-op when no sink is set. Does NOT enter any ring.
func EmitSpan(s Span)
```

- A package-level `sinkMu sync.Mutex` + `sink io.Writer` pair; one mutex
  serializes all writers (multiple rings + EmitSpan callers — TUI re-roots
  create new rings that all share the one sink).
- **`Ring.Record` mirrors to the sink** after assigning the span ID. This is
  deliberate (vs a wrapper Recorder): `cmd/gg/main.go` needs the concrete
  `*Ring` for the panic dump's `Snapshot`, and all four construction sites
  (`cli.openRepo`, `cmd/gg/main.go`, `tui.reRoot`, `app/inspect`) keep their
  `observ.NewRing(200)` calls untouched. Sink nil → exactly today's behavior.
- The marshal+redact+write logic is extracted into one unexported helper
  shared by the sink path and `TraceRecorder.Record` (which stays as is —
  `gg inspect --trace` is per-call tracing, unrelated to the global sink).
- Sync policy: plain buffered-by-OS `Write` per line, no fsync (a perf log
  must not distort the perf it measures). `O_APPEND` writes of single lines
  are atomic enough for this purpose.

## Flag plumbing (`cmd/gg/main.go`)

- `extractTimeTrack(args) (string, []string)` — identical shape to
  `extractCwdFile` (both `--time-track p` and `--time-track=p` forms), run
  right after it, BEFORE shell-init/inspect/CLI routing so the flag works for
  every surface.
- Non-empty path: `os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND,
  0o644)`. Open failure → message to stderr, **exit 2** (the user asked for
  measurements; a silent no-op log would lie). On success:
  `observ.SetSpanSink(f)` + `observ.EmitSpan` the `gg start` span
  (redacted argv + `version=` + `buildinfo` version). The file is never
  explicitly closed — process exit flushes OS buffers; each line is written
  unbuffered from Go's side.
- `gg shell-init` is exempt (routed before extraction is fine to keep, but
  extraction happens first — harmless: shell-init never records spans).

## Operation spans (frontends)

- **CLI** (`internal/cli/core.go`): `runOperation` wraps `op.Run` — capture
  `start := time.Now()`, after completion `observ.EmitSpan(observ.Span{Name:
  "op " + opName(op), Start: start, Duration: time.Since(start), ExitCode:
  0|1, Err: <error text>})`. `opName` derives `SmartPull` from
  `fmt.Sprintf("%T", op)` ( trims `engine.` prefix).
- **TUI** (`internal/tui/op.go`): same wrap inside `startOp`'s op goroutine
  (around `op.Run`, before the `opFinishedMsg` send). The span is emitted from
  the op goroutine — the sink mutex makes that safe.
- The name helper is **`engine.OpName(op Operation) string`** — the engine
  owns the Operation type, and both frontends share it (not duplicated per
  caller, not forced into observ which knows nothing about operations).

## Docs & skill

- README: flag documented next to `--cwd-file` (global flags section / CLI
  block) with a one-line jq example.
- `internal/agentskill/using-gg.md`: one bullet (`--time-track <file>` —
  append JSONL perf spans; works with every command). `agentskill.Version`
  3 → 4, `gg init --update`, commit the regenerated dogfood copy (the
  drift-guard test enforces it).
- CHANGELOG entry.

## Files touched

| File | Change |
|------|--------|
| `internal/observ/sink.go` (new) | `SetSpanSink`, `EmitSpan`, shared write helper; `Ring.Record` mirror (edit ring.go). |
| `internal/observ/trace.go` | `TraceRecorder.Record` reuses the shared helper (behavior unchanged). |
| `internal/engine/opname.go` (new) | `OpName(Operation) string`. |
| `internal/cli/core.go` | Op span around `op.Run` in `runOperation`. |
| `internal/tui/op.go` | Op span around `op.Run` in `startOp`. |
| `cmd/gg/main.go` | `extractTimeTrack`, file open, `SetSpanSink`, `gg start` span. |
| `internal/agentskill/{using-gg.md, agentskill.go}` | Flag bullet + `Version = 4`. |
| `.claude/skills/using-gg/SKILL.md` | Regenerated v4. |
| `README.md`, `CHANGELOG.md` | Docs. |

## Testing

- **observ:** sink mirroring — set a buffer sink, `Ring.Record` a span →
  buffer holds one valid JSON line with redacted args and the assigned ID;
  `EmitSpan` with no sink is a no-op; concurrent Record+EmitSpan under
  `-race`; `TraceRecorder` behavior unchanged (existing tests).
  **Hermeticity:** every test that sets the sink must
  `t.Cleanup(func() { observ.SetSpanSink(nil) })` — the sink is process-wide.
- **engine:** `OpName` for a few ops (`SmartPull`, `CreateWorktreeForBranch`).
- **cli:** run a `commit` through `runOperation` with a buffer sink → an
  `op Commit` span with plausible duration plus `git commit` span lines;
  failing op → `ExitCode` 1 + `Err` set.
- **tui:** drive a small op (`driveOp`) with a buffer sink → `op …` line
  appears; emitted from the op goroutine (covered by `-race`).
- **cmd/gg:** `extractTimeTrack` both forms + trailing-flag drop (table test
  mirroring `extractCwdFile`'s); open-failure exit 2 (point at an
  unwritable path).
- Append behavior: open the same temp file twice with the production
  open-flags helper, write a span through each → the file holds both lines
  (run 2 did not truncate run 1).

## Out of scope (YAGNI)

- Parent/child span linking; log rotation/size caps; human-prose format;
  a `--time-track` TUI status indicator; flushing/fsync policy knobs;
  routing `gg inspect --trace` through the sink (it keeps its own writer);
  op spans in the ring / panic dump (sink only).
