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

## Part 2 (reviews, line notes, live notes, meta)

Spec: `../../superpowers/specs/2026-10-15-gg-notes-store-format-part2-design.md`.

- `review-document.schema.json` / `review-document.lenient.schema.json` —
  the stored review document (strict = gg's drift guard, lenient = a
  reader's).
- `fixture/` now holds every record kind part 2 specifies except a forge
  draft reply and a shelf note (see `fixture/README.md`), written
  by gg's own store (`go test ./internal/domain -run TestFixtureIsWhatGGWrites`;
  `-update-fixture` regenerates it).
- `vectors.json` — `context_hash`, `remark_fp`, repository and worktree key
  test vectors.
- `relocation-cases.json` — the relocation rule (part 2 §5.2) as cases a
  reader runs against its own implementation; gg's resolver passes them
  (`TestRelocationCases`).
