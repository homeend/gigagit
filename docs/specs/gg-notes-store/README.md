# gg notes store — conformance material

The specification is `../../superpowers/specs/2026-10-15-gg-notes-store-format-design.md`.

- `overview-document.schema.json` — the stored overview document, STRICT
  (`additionalProperties: false`). gg's own drift guard: a test in
  `internal/notebatch` validates gg's canonical output against it.
- `overview-document.lenient.schema.json` — the same shape, unknown keys
  tolerated. What a reader validates with.
- `fixture/commits.toml` + `fixture/previews.toml` — a store with a commit
  overview, a PR overview (in the previews part, where gg writes it) and
  three records a reader must skip. A test in `internal/domain` reads both
  files through gg's own store code.
- `gg overview store --json` prints the store directory, its key and the
  two files gg resolved for the current repository: the oracle for a
  reader's key derivation.
