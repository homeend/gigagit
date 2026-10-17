# gg steer — conformance material for a steering consumer

Spec: `../../superpowers/specs/2026-10-15-gg-steer-ide-consumer-design.md`
(the IDE as a third `gg session` target). gg's own TUI is tested against
these files; a consumer that is not gg (the gg-notes IDEA plugin) runs them
against its handler.

- `command.schema.json`, `reply.schema.json`, `presence.schema.json` —
  the wire shapes (`cmd-<id>.json`, `reply-<id>.json`, `tui.json` /
  `web.json` / `ide.json`), lenient: unknown keys tolerated and the
  verb/state/side vocabularies left open (a consumer dispatches on them and
  refuses unknown values with prose — the schema never gates dispatch).
  `internal/steer` validates every shape gg writes against them
  (`TestWireShapesMatchSchemas`).
- `steer-cases.json` — a replay corpus: `sync` cases every consumer
  answers alike (gg's TUI runs them: `internal/tui` `TestSteerCases`), async
  landings carrying the TUI's exact `detail` prose (read from its handlers;
  the TUI answers them asynchronously so its test skips them), `tui_only`
  cases with an `ide_only` twin, and the IDE's own refusals. The plugin runs
  every case that is not `tui_only` against its handler.
- `reanchor-cases.json` — gg's exact-line re-anchoring of temporary notes
  (spec §5), eight cases (`internal/agentdocs` `TestReanchorCases`).
- `repokey-vectors.json` — `EncodeRepoKey` inputs/outputs for the inbox path
  (`internal/config` `TestRepoKeyVectors`); Windows rows are derived and run
  only on Windows.
