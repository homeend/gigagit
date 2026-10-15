# gg notes store — conformance material

The specification is `../../superpowers/specs/2026-10-15-gg-notes-store-format-design.md`.

- `overview-document.schema.json` — the stored overview document, STRICT
  (`additionalProperties: false`). gg's own drift guard: a test in
  `internal/notebatch` validates gg's canonical output against it.
- `overview-document.lenient.schema.json` — the same shape, unknown keys
  tolerated. What a reader validates with.
- `fixture/commits.toml` — a store with two overview records (a commit
  overview and a PR overview) and one record a reader must skip. A test in
  `internal/domain` reads it through gg's own store code.
- `gg overview store --json` prints the store path and key gg resolved for
  the current repository: the oracle for a reader's key derivation.
