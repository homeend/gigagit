# gg session docs — conformance material for a reader or writer

Spec: `../../superpowers/specs/2026-10-18-gg-session-docs-design.md`
(files an agent writes into a worktree's `<inbox>/docs/`, shown by every
gg reader of that worktree: the TUI, gg web, the gg-notes IDEA plugin).
gigagit's `internal/agentdocs` is tested against these files; a reader that
is not gg runs them against its parser, placer and lister.

- `overview-frontmatter.schema.json`, `note.schema.json` — the PARSED shapes
  (TOML → JSON: tables are objects, arrays of tables are arrays, date-times
  are RFC 3339 strings), lenient: unknown keys allowed. What they cannot say
  — `lines.length == end - start + 1`, `start <= end`, the path segment
  rules — is in the spec §3.3 and in gg's parser
  (`TestSessionDocShapesMatchSchemas`, `TestNoteFileDefaultsAndRefusals`).
- `examples/` — a directory written by gg's own encoder plus the `tree/` it
  anchors; `ids.json` names what each document demonstrates and the
  placements a reader must reach (`TestExamplesAreWhatGGWrites`). gg's TOML
  encoder writes literal (single-quoted) strings and the empty parent
  headers `[display]` / `[display.<key>.values]`; any TOML parser accepts
  both, and a writer may spell strings with double quotes.
- `cold-anchor-cases.json` — first placement of a note from its `lines`
  alone (spec §4.3): `{name, file, note: {start, end, lines}, expect:
  {start, end, outdated}}` (`TestColdAnchorCases`). After that, re-anchoring
  on edits is `../gg-steer/reanchor-cases.json`.
- `id-vectors.json` — valid/invalid ids, file name ↔ (id, kind), and the
  entry names a reader ignores (`TestIDVectors`,
  `TestListDocsAcceptsDocumentsAndReportsTheRest`).
- The inbox path the directory lives under: `../gg-steer/repokey-vectors.json`.
