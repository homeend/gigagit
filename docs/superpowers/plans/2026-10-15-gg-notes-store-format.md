# gg notes store format — conformance material Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the gg-side material the spec promises so an external reader (the `gg-notes` IDEA plugin) can be built from the spec alone and gg cannot drift from it: two JSON Schemas, a fixture store, conformance tests, and a `gg overview store` oracle for the store path and key.

**Architecture:** Nothing in how gg stores overviews changes. New files under `docs/specs/gg-notes-store/` are pure data (schemas, a fixture `commits.toml`). Tests in `internal/notebatch` validate gg's own canonical document output against the strict schema (with `github.com/google/jsonschema-go`, already a direct dependency, MIT); a test in `internal/domain` reads the fixture through gg's own store code. One new domain query (`NotesStorePath`) exposes the resolved store directory + key, and one new CLI verb (`gg overview store`) prints it.

**Tech Stack:** Go 1.26, `github.com/google/jsonschema-go v0.4.3` (already in go.mod, used by `internal/mcp`), `github.com/pelletier/go-toml/v2` (already in go.mod, used by `internal/notes`), real `git` in `t.TempDir()`.

**Spec:** `docs/superpowers/specs/2026-10-15-gg-notes-store-format-design.md` (§9 conformance material, §9.3 oracle). One deviation, applied to the spec in Task 4: the oracle is a separate verb `gg overview store [--json]`, not fields on `gg overview list --json` — that command prints a JSON ARRAY of rows (a top-level field would break its shape) and prints no row at all in a repository with no overview yet, exactly where the plugin author needs the oracle most.

## Global Constraints

- Work in the worktree `/work/gigagit/.claude/worktrees/gg-notes-store-format` on branch `spec/gg-notes-store-format`; every path below is relative to it. Never touch the main checkout.
- No new Go dependency (licensing rule: permissive only; `jsonschema-go` is already direct, MIT).
- Tests use a real `git` in `t.TempDir()` or gg's existing test helpers; every new test calls `t.Parallel()`.
- Tests never write into `docs/`: copy the fixture into a temp dir before opening it as a store.
- `internal/cli` never imports `internal/git`/`internal/notes`; it reaches the store through `internal/domain` (archtest enforces).
- Engine/CLI prose stays English; no TUI strings are added (no i18n work).
- Commit with raw `git commit -F <msgfile>` after `gg add` (gg commit has no `-F`); never `git add -A`. End commit messages with the attribution lines the session reminder gives.
- Run `./test.sh race` from the worktree before declaring done; "all green" in the log is the only pass signal.
- Docs after the feature: `CHANGELOG.md` (always), `README.md` (CLI surface changed), `internal/agentskill/using-gg.md` + bump `agentskill.Version` (CLI surface changed), then `gg init --update` is the user's step, not ours.

## Review Focus

Inputs the spec implies that no task's tests would otherwise exercise, most likely to bite first; each line's test is added to the owning task.

1. A document carrying a field gg does not store (an agent's `meta`, a future gg's new key): the strict schema MUST reject it and the lenient one MUST accept it — otherwise the two schemas are the same file twice. → Task 1 (`TestSchemaStrictRejectsLenientAccepts`).
2. A `commits.toml` whose overview `rationale` is not JSON: `Overviews` still returns the row with `Doc == nil` (the "cannot render" row of spec §8.2), never an error, never a dropped row. → Task 2 (`TestFixtureStoreCannotRenderRowSurvives`).
3. The key rule on this platform: `NotesStorePath` must produce `d10200a3a219735f` for `/work/gigagit/.git` on Linux/macOS (the spec's test vector), and on every platform `hex(sha256(filepath.Clean(dir))[:8])`. → Task 3 (`TestNotesKeyVector`).
4. An injected or overridden store (tests, `NotesStatePath`): `NotesStorePath` must report the directory gg actually reads and an EMPTY key rather than a key for a directory gg is not using. → Task 3 (`TestNotesStorePathInjected`).
5. A repository with no commit and no overview: `gg overview store --json` must still print the store path and key (the oracle is for fresh setups), where `gg overview list` exits 1 with "HEAD has no commit". → Task 4 (`TestOverviewStoreOnEmptyRepo`).

---

### Task 1: JSON Schemas + notebatch conformance test

**Files:**
- Create: `docs/specs/gg-notes-store/overview-document.schema.json`
- Create: `docs/specs/gg-notes-store/overview-document.lenient.schema.json`
- Create: `docs/specs/gg-notes-store/README.md`
- Test: `internal/notebatch/schema_test.go`

**Interfaces:**
- Consumes: `notebatch.ParseOverview([]byte) (OverviewDoc, error)`, `OverviewDoc.Canonical() []byte` (existing).
- Produces: the two schema files at fixed paths; Task 2's fixture documents must validate against the strict one.

- [ ] **Step 1: Write the strict schema**

`docs/specs/gg-notes-store/overview-document.schema.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://gigagit.dev/specs/gg-notes-store/overview-document.schema.json",
  "title": "gg overview document (stored canonical form)",
  "description": "The JSON text gg stores in an overview record's `rationale` (spec 2026-10-15-gg-notes-store-format §5). STRICT: this is gg's own drift guard; a reader validates leniently (overview-document.lenient.schema.json).",
  "type": "object",
  "additionalProperties": false,
  "required": ["version", "title", "overview"],
  "properties": {
    "version": { "const": 1 },
    "title": { "type": "string", "minLength": 1, "maxLength": 200 },
    "overview": { "type": "string", "minLength": 1, "maxLength": 65536 },
    "files": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["path"],
        "properties": {
          "path": { "type": "string", "minLength": 1 },
          "annotations": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["newRange", "summary"],
              "properties": {
                "newRange": {
                  "type": "array",
                  "items": { "type": "integer", "minimum": 1 },
                  "minItems": 2,
                  "maxItems": 2
                },
                "summary": { "type": "string", "minLength": 1 },
                "rationale": { "type": "string" }
              }
            }
          }
        }
      }
    }
  }
}
```

- [ ] **Step 2: Write the lenient schema**

`docs/specs/gg-notes-store/overview-document.lenient.schema.json` — identical except every `"additionalProperties": false` becomes `"additionalProperties": true`, the `$id` ends in `overview-document.lenient.schema.json`, the `title` is `"gg overview document (reader's lenient form)"` and the description reads: `"The shape a READER validates with: the same required fields and types as the strict schema, unknown keys tolerated (spec §8.3)."`.

- [ ] **Step 3: Write the README**

`docs/specs/gg-notes-store/README.md`:

```markdown
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
```

- [ ] **Step 4: Write the failing conformance test**

`internal/notebatch/schema_test.go`:

```go
package notebatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// specDir is docs/specs/gg-notes-store relative to this package.
func specDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "docs", "specs", "gg-notes-store")
}

func loadSchema(t *testing.T, name string) *jsonschema.Resolved {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(specDir(t), name))
	if err != nil {
		t.Fatal(err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("%s: resolve: %v", name, err)
	}
	return rs
}

// validateJSON decodes raw as generic JSON (the schema validator's instance
// form) and validates it.
func validateJSON(rs *jsonschema.Resolved, raw []byte) error {
	var inst any
	if err := json.Unmarshal(raw, &inst); err != nil {
		return err
	}
	return rs.Validate(inst)
}

// Every canonical document gg writes validates against the strict schema —
// the drift guard of spec 2026-10-15-gg-notes-store-format §9.1.
func TestCanonicalOverviewMatchesStrictSchema(t *testing.T) {
	t.Parallel()
	strict := loadSchema(t, "overview-document.schema.json")
	lenient := loadSchema(t, "overview-document.lenient.schema.json")
	docs := []string{
		`{"version":1,"title":"Only text","overview":"No anchors here."}`,
		`{"version":1,"title":" Trimmed ","overview":"[gate](a.go:3)","files":[{"path":"a.go","annotations":[{"newRange":[3,4],"summary":"the gate","rationale":"why"}]}]}`,
		`{"version":1,"title":"No rationale","overview":"[x](b.go:1-2)","files":[{"path":"b.go","annotations":[{"newRange":[1,2],"summary":"s"}]}]}`,
		`{"version":1,"title":"Empty files","overview":"text","files":[]}`,
		// an agent's input form: a summary-less file with no annotations
		`{"version":1,"title":"Bare file","overview":"[f](c.go)","files":[{"path":"c.go"}]}`,
	}
	for _, in := range docs {
		d, err := ParseOverview([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		canon := d.Canonical()
		if err := validateJSON(strict, canon); err != nil {
			t.Errorf("strict schema rejects gg's own canonical form:\n%s\n%v", canon, err)
		}
		if err := validateJSON(lenient, canon); err != nil {
			t.Errorf("lenient schema rejects gg's own canonical form:\n%s\n%v", canon, err)
		}
	}
}

// Review focus 1: the strict schema rejects a key gg does not store; the
// lenient one tolerates it (spec §8.3 / §9.1).
func TestSchemaStrictRejectsLenientAccepts(t *testing.T) {
	t.Parallel()
	strict := loadSchema(t, "overview-document.schema.json")
	lenient := loadSchema(t, "overview-document.lenient.schema.json")
	extra := []byte(`{"version":1,"title":"t","overview":"o","future":true,"files":[{"path":"a.go","meta":{"k":"v"},"annotations":[{"newRange":[1,1],"summary":"s","confidence":0.5}]}]}`)
	if err := validateJSON(strict, extra); err == nil {
		t.Fatal("strict schema accepted unknown keys")
	}
	if err := validateJSON(lenient, extra); err != nil {
		t.Fatalf("lenient schema rejected unknown keys: %v", err)
	}
	// Both still refuse the shapes gg refuses.
	for _, bad := range []string{
		`{"version":2,"title":"t","overview":"o"}`,
		`{"version":1,"title":"","overview":"o"}`,
		`{"version":1,"title":"t","overview":""}`,
		`{"version":1,"title":"t","overview":"o","files":[{"path":""}]}`,
		`{"version":1,"title":"t","overview":"o","files":[{"path":"a","annotations":[{"newRange":[0,1],"summary":"s"}]}]}`,
		`{"version":1,"title":"t","overview":"o","files":[{"path":"a","annotations":[{"newRange":[1],"summary":"s"}]}]}`,
		`{"version":1,"title":"t","overview":"o","files":[{"path":"a","annotations":[{"newRange":[1,1]}]}]}`,
	} {
		if err := validateJSON(strict, []byte(bad)); err == nil {
			t.Errorf("strict schema accepted %s", bad)
		}
		if err := validateJSON(lenient, []byte(bad)); err == nil {
			t.Errorf("lenient schema accepted %s", bad)
		}
	}
}
```

- [ ] **Step 5: Run the test to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && go test ./internal/notebatch -run 'TestCanonicalOverviewMatchesStrictSchema|TestSchemaStrictRejectsLenientAccepts' -v`
Expected: FAIL — before Steps 1–2 are saved the files are missing ("no such file"); once they exist, PASS. Run it once with the schema files temporarily renamed if you wrote them first, so you have seen the failure mode (a missing schema must be a loud failure, not a skipped test).

- [ ] **Step 6: Run the test to verify it passes**

Run: the same command.
Expected: PASS. If `rs.Validate` reports `"minimum"` on `newRange` for a valid doc, check that `json.Unmarshal` into `any` produced `float64` — jsonschema-go treats integral floats as integers, so a failure there means the schema's `integer` keyword is misspelled.

- [ ] **Step 7: Commit**

```bash
cd /work/gigagit/.claude/worktrees/gg-notes-store-format
gg add docs/specs/gg-notes-store/overview-document.schema.json docs/specs/gg-notes-store/overview-document.lenient.schema.json docs/specs/gg-notes-store/README.md internal/notebatch/schema_test.go
git commit -F <msgfile>   # "notebatch: JSON Schemas for the stored overview document + conformance test"
```

---

### Task 2: Fixture store read through gg's own store code

**Files:**
- Create: `docs/specs/gg-notes-store/fixture/commits.toml`
- Test: `internal/domain/overview_fixture_test.go`

**Interfaces:**
- Consumes: `(*Service).UseNotesDir(dir string)`, `(*Service).Overviews(ctx) ([]Overview, error)`, `(*Service).OverviewsForCommit(ctx, sha)`, the test helper `permOvRepo(t) (string, *Service)` (in `overview_save_test.go`), `notebatch.ParseOverview`.
- Produces: the fixture file; Task 4's skill text names it.

- [ ] **Step 1: Write the fixture**

`docs/specs/gg-notes-store/fixture/commits.toml` — a store in the exact spelling gg writes (TOML 1.0, literal strings where possible, one basic string for the JSON document). Three records: a commit overview with two anchored files and remarks, a PR overview, and a review note a reader must skip.

```toml
format = 3

[[notes]]
id = '7cf0812f'
source = 'agent'
author = 'Claude Opus 5.5'
side = 'new'
range = [0, 0]
context_hash = ''
summary = 'How the gate works'
rationale = "{\n  \"version\": 1,\n  \"title\": \"How the gate works\",\n  \"overview\": \"The gate is one function.\\n\\n## 1. The check\\n\\n- [`gate`](internal/x/gate.go:3-4) refuses a closed door.\\n- [this line](internal/x/gate.go:2) opens it.\\n\\n## 2. The caller\\n\\n- [the call](internal/y/call.go:1) runs the gate first. See [the config](config.toml).\\n\\nSkipped: nothing. Next: none.\",\n  \"files\": [\n    {\n      \"path\": \"internal/x/gate.go\",\n      \"annotations\": [\n        {\n          \"newRange\": [3, 4],\n          \"summary\": \"The refusal.\",\n          \"rationale\": \"A closed door must not pass.\"\n        }\n      ]\n    },\n    {\n      \"path\": \"internal/y/call.go\",\n      \"annotations\": [\n        {\n          \"newRange\": [1, 1],\n          \"summary\": \"The only caller.\"\n        }\n      ]\n    }\n  ]\n}"
tags = ['overview']
created = 2026-10-13T23:53:56.742993129Z
updated = 2026-10-13T23:53:56.742993129Z

[notes.address]
Worktree = ''
Branch = 'main'
Commit = 'd5efa035df1f3c4c949040bebb924a35d0ea61a0'
ShelfID = ''
Path = ''
State = 0

[[notes]]
id = '3a9e1c20'
source = 'agent'
author = 'ada'
side = 'new'
range = [0, 0]
context_hash = ''
summary = 'PR 7: the gate, reviewed'
rationale = "{\n  \"version\": 1,\n  \"title\": \"PR 7: the gate, reviewed\",\n  \"overview\": \"The pull request adds [the gate](internal/x/gate.go:1-4).\"\n}"
tags = ['overview']
preview = 'main...refs/gg/pr/7'
created = 2026-10-14T08:00:00Z
updated = 2026-10-14T08:00:00Z

[notes.address]
Worktree = ''
Branch = ''
Commit = 'e4a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3'
ShelfID = ''
Path = ''
State = 0

[[notes]]
id = '91f0aa07'
source = 'agent'
author = 'ada'
side = 'new'
range = [3, 4]
context_hash = ''
summary = 'A review remark, not an overview'
rationale = 'A reader of overview records skips this record: it has a Path and no overview tag.'
tags = ['review']
scope = 'd5efa035..e4a1b2c3'
created = 2026-10-14T08:05:00Z
updated = 2026-10-14T08:05:00Z

[notes.address]
Worktree = ''
Branch = ''
Commit = 'e4a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3'
ShelfID = ''
Path = 'internal/x/gate.go'
State = 0
```

- [ ] **Step 2: Write the failing fixture test**

`internal/domain/overview_fixture_test.go`:

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureStoreDir copies docs/specs/gg-notes-store/fixture/commits.toml
// into a temp store dir (tests never write into docs/).
func fixtureStoreDir(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "..", "docs", "specs", "gg-notes-store", "fixture", "commits.toml")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "commits.toml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The spec's fixture store (2026-10-15-gg-notes-store-format §9.2) read
// through gg's own store: exactly the two overview records are found, with
// their documents parsed; the review note is skipped.
func TestFixtureStoreReadsThroughGG(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	svc.UseNotesDir(fixtureStoreDir(t))
	ctx := context.Background()
	all, err := svc.Overviews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("Overviews = %d rows, want 2: %+v", len(all), all)
	}
	// newest first: the PR overview (2026-10-14) before the commit one
	pr, commit := all[0], all[1]
	if pr.ID != "3a9e1c20" || pr.Scope != "main...refs/gg/pr/7" || pr.Title != "PR 7: the gate, reviewed" || pr.Doc == nil {
		t.Fatalf("PR overview = %+v", pr)
	}
	if n, ok := PRScopeNumber(pr.Scope); !ok || n != 7 {
		t.Fatalf("PRScopeNumber(%q) = %d %v", pr.Scope, n, ok)
	}
	if commit.ID != "7cf0812f" || commit.Commit != "d5efa035df1f3c4c949040bebb924a35d0ea61a0" || commit.Branch != "main" || commit.Agent != "Claude Opus 5.5" || commit.Doc == nil {
		t.Fatalf("commit overview = %+v", commit)
	}
	d := commit.Doc
	if d.Title != "How the gate works" || len(d.Files) != 2 || d.Files[0].Path != "internal/x/gate.go" ||
		d.Files[0].Notes[0].Range != [2]int{3, 4} || d.Files[0].Notes[0].Rationale != "A closed door must not pass." ||
		d.Files[1].Notes[0].Range != [2]int{1, 1} || d.Files[1].Notes[0].Rationale != "" {
		t.Fatalf("document = %+v", d)
	}
	if !strings.Contains(d.Overview, "[`gate`](internal/x/gate.go:3-4)") || !strings.Contains(d.Overview, "[the config](config.toml)") {
		t.Fatalf("overview text = %q", d.Overview)
	}
	// The commit's own list has only the commit overview (a PR's belongs to its PR).
	byCommit, err := svc.OverviewsForCommit(ctx, "d5efa035df1f3c4c949040bebb924a35d0ea61a0")
	if err != nil || len(byCommit) != 1 || byCommit[0].ID != "7cf0812f" {
		t.Fatalf("OverviewsForCommit = %+v %v", byCommit, err)
	}
}

// Review focus 2: a record whose document no longer parses is still a row
// (Doc nil — the reader's "cannot render" row, spec §8.2), never an error.
func TestFixtureStoreCannotRenderRowSurvives(t *testing.T) {
	t.Parallel()
	dir := fixtureStoreDir(t)
	p := filepath.Join(dir, "commits.toml")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// Break the PR overview's document only.
	broken := strings.Replace(string(data), `rationale = "{\n  \"version\": 1,\n  \"title\": \"PR 7`, `rationale = "not json {\n  \"version\": 1,\n  \"title\": \"PR 7`, 1)
	if broken == string(data) {
		t.Fatal("fixture text changed: the PR record's rationale was not found")
	}
	if err := os.WriteFile(p, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	_, svc := permOvRepo(t)
	svc.UseNotesDir(dir)
	all, err := svc.Overviews(context.Background())
	if err != nil || len(all) != 2 {
		t.Fatalf("Overviews = %+v %v, want 2 rows", all, err)
	}
	if all[0].ID != "3a9e1c20" || all[0].Doc != nil || all[0].Title != "PR 7: the gate, reviewed" {
		t.Fatalf("broken row = %+v, want Doc nil with its title kept", all[0])
	}
	if all[1].Doc == nil {
		t.Fatalf("the intact row lost its document: %+v", all[1])
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && go test ./internal/domain -run 'TestFixtureStore' -v`
Expected: FAIL with "no such file or directory" for the fixture until Step 1 is saved; with the fixture present, PASS. If `Overviews` returns 0 rows, the TOML did not parse — run `go run ./cmd/gg overview list --all` inside a repo with `XDG_STATE_HOME` pointed at a temp dir holding `notes/<key>/commits.toml` copied from the fixture to see gg's own error, and fix the FIXTURE (never the test's expectations) until gg reads it.

- [ ] **Step 4: Run the tests to verify they pass**

Run: the same command.
Expected: PASS (both tests).

- [ ] **Step 5: Validate the fixture's documents against the strict schema**

Append to `internal/notebatch/schema_test.go`:

```go
// The fixture store's documents are canonical: each validates strictly and
// round-trips through Canonical() unchanged.
func TestFixtureDocumentsAreCanonical(t *testing.T) {
	t.Parallel()
	strict := loadSchema(t, "overview-document.schema.json")
	data, err := os.ReadFile(filepath.Join(specDir(t), "fixture", "commits.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Notes []struct {
			Tags      []string `toml:"tags"`
			Rationale string   `toml:"rationale"`
		} `toml:"notes"`
	}
	if err := toml.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, n := range file.Notes {
		if !slices.Contains(n.Tags, "overview") {
			continue
		}
		seen++
		if err := validateJSON(strict, []byte(n.Rationale)); err != nil {
			t.Errorf("fixture document fails the strict schema: %v\n%s", err, n.Rationale)
		}
		d, err := ParseOverview([]byte(n.Rationale))
		if err != nil {
			t.Fatal(err)
		}
		if string(d.Canonical()) != n.Rationale {
			t.Errorf("fixture document is not in canonical form; gg would store:\n%s\ngot:\n%s", d.Canonical(), n.Rationale)
		}
	}
	if seen != 2 {
		t.Fatalf("fixture holds %d overview records, want 2", seen)
	}
}
```

Add `"slices"` and `"github.com/pelletier/go-toml/v2"` (as `toml`) to the test file's imports. `notebatch` production code stays stdlib-only; a `_test.go` import of go-toml is fine (the package's own test files decide what they import; the archtest guards production imports — confirm with `go vet ./internal/notebatch` and `go test ./internal/archtest`).

- [ ] **Step 6: Run it; fix the FIXTURE if the canonical bytes differ**

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && go test ./internal/notebatch -run TestFixtureDocumentsAreCanonical -v`
Expected: PASS. If the round-trip differs (indentation, key order, a trailing newline), replace the fixture's `rationale` strings with what the failure prints under "gg would store", JSON-escaped for a TOML basic string (`\n` for newlines, `\"` for quotes) — the fixture must be byte-for-byte what gg writes.

- [ ] **Step 7: Commit**

```bash
cd /work/gigagit/.claude/worktrees/gg-notes-store-format
gg add docs/specs/gg-notes-store/fixture/commits.toml internal/domain/overview_fixture_test.go internal/notebatch/schema_test.go
git commit -F <msgfile>   # "spec fixture: a notes store gg itself reads — two overviews + one record to skip"
```

---

### Task 3: `domain.NotesStorePath` — the resolved store directory and key

**Files:**
- Modify: `internal/domain/notesstore.go:84-132` (`notesStoreKeyed`)
- Modify: `internal/notes/file_store.go:30-33` (add `Root()`)
- Test: `internal/domain/notesstore_path_test.go`

**Interfaces:**
- Consumes: `repoKey(commonDir string) string` (`internal/domain/shelfstore.go:63`), `stateBaseDir(kind string) string` (`statebasedir.go`), `NotesStatePath`, `(*Service).UseNotesDir`.
- Produces:
  ```go
  // NotesStoreInfo is where this repository's notes live.
  type NotesStoreInfo struct {
      Dir string // the store directory gg reads (holds commits.toml); "" = notes disabled
      Key string // the directory key (spec §3.2); "" when the dir was injected or overridden
  }
  func (s *Service) NotesStorePath(ctx context.Context) NotesStoreInfo
  func (fs *notes.FileStore) Root() string
  ```

- [ ] **Step 1: Write the failing tests**

`internal/domain/notesstore_path_test.go`:

```go
package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Review focus 3: the key rule, with the spec's Linux test vector
// (2026-10-15-gg-notes-store-format §3.2).
func TestNotesKeyVector(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		if got := repoKey("/work/gigagit/.git"); got != "d10200a3a219735f" {
			t.Fatalf("repoKey(/work/gigagit/.git) = %q, want d10200a3a219735f", got)
		}
	}
	dir := filepath.Join(t.TempDir(), "repo", ".git")
	sum := sha256.Sum256([]byte(filepath.Clean(dir)))
	if got, want := repoKey(dir), hex.EncodeToString(sum[:8]); got != want {
		t.Fatalf("repoKey = %q, want %q", got, want)
	}
}

// A real repo resolves to <XDG_STATE_HOME>/gg/notes/<key> with the key of
// its common dir — the path and key gg overview store prints.
func TestNotesStorePathResolved(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // t.Setenv forbids t.Parallel
	dir, svc := newRealRepo(t)
	ctx := context.Background()
	cd, err := svc.GitCommonDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	info := svc.NotesStorePath(ctx)
	wantKey := repoKey(cd)
	if info.Key != wantKey || len(info.Key) != 16 {
		t.Fatalf("Key = %q, want %q", info.Key, wantKey)
	}
	wantDir := filepath.Join(stateBaseDir("notes"), wantKey)
	if info.Dir != wantDir {
		t.Fatalf("Dir = %q, want %q (repo %s)", info.Dir, wantDir, dir)
	}
	if !strings.HasPrefix(info.Dir, filepath.Join(os.Getenv("XDG_STATE_HOME"), "gg", "notes")) {
		t.Fatalf("Dir %q is not under the test's XDG_STATE_HOME", info.Dir)
	}
}

// Review focus 4: an injected store reports ITS directory and no key.
func TestNotesStorePathInjected(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	want := t.TempDir()
	svc.UseNotesDir(want)
	info := svc.NotesStorePath(context.Background())
	if info.Dir != want || info.Key != "" {
		t.Fatalf("NotesStorePath = %+v, want Dir %q and an empty key", info, want)
	}
}

// Notes disabled: nothing to report.
func TestNotesStorePathDisabled(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	svc.disableNotesForTest()
	if info := svc.NotesStorePath(context.Background()); info != (NotesStoreInfo{}) {
		t.Fatalf("NotesStorePath = %+v, want zero", info)
	}
}
```

Add `"os"` to the imports. `disableNotesForTest` is the existing method that sets `s.notesOff` (`notesstore.go:59`); the `Service` fields are `notes notes.Store` and `notesOff bool` (`service.go:55-56`) — add `notesKey string` beside them.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && go test ./internal/domain -run 'TestNotesKeyVector|TestNotesStorePath' -v`
Expected: FAIL to compile — `NotesStoreInfo` / `NotesStorePath` undefined.

- [ ] **Step 3: Add `Root()` to the file store**

In `internal/notes/file_store.go` after `NewFileStore`:

```go
// Root is the store directory (the one NewFileStore was given).
func (fs *FileStore) Root() string { return fs.root }
```

- [ ] **Step 4: Factor the root computation and add the query**

In `internal/domain/notesstore.go`, replace the `root := NotesStatePath … root = filepath.Join(base, key)` block of `notesStoreKeyed` with a call to a new helper, and record the key on the service:

```go
// notesRoot is the store directory gg resolves for this repository and its
// key (spec 2026-10-15-gg-notes-store-format §3): NotesStatePath when set
// (no key), else <state>/gg/notes/<repoKey(common dir)>. ok is false when
// no state dir is resolvable.
func notesRoot(commonDir func() (string, error)) (root, key string, ok bool) {
	if NotesStatePath != "" {
		return NotesStatePath, "", true
	}
	base := stateBaseDir("notes")
	if base == "" {
		return "", "", false
	}
	key = "unknown"
	if cd, err := commonDir(); err == nil {
		key = repoKey(strings.TrimSpace(cd)) // reuse shelfstore.go's repoKey
	}
	return filepath.Join(base, key), key, true
}
```

and in `notesStoreKeyed`:

```go
	root, key, ok := notesRoot(commonDir)
	if !ok {
		return nil
	}
	fs := openNotesStore(root)
	s.mu.Lock()
	fresh := s.notes == nil
	if fresh {
		s.notes = fs
		s.notesKey = key
	}
	st = s.notes
	…
```

Add `notesKey string` next to the `notes` field of `Service` (find it: `grep -n 'notes  *notes.Store' internal/domain/*.go`). `SetNotesStore` (an injected store) must clear it: add `s.notesKey = ""` where it assigns `s.notes = st`.

Then the query, in the same file:

```go
// NotesStoreInfo is where this repository's notes live — what `gg overview
// store` prints so a reader of the store format can check its own key
// derivation (spec 2026-10-15-gg-notes-store-format §9.3).
type NotesStoreInfo struct {
	Dir string // the store directory gg reads (holds commits.toml); "" = notes disabled
	Key string // the directory key (spec §3.2); "" when the dir was injected or overridden
}

// NotesStorePath resolves the store (as any read would) and reports it.
func (s *Service) NotesStorePath(ctx context.Context) NotesStoreInfo {
	st := s.notesStore(ctx)
	if st == nil {
		return NotesStoreInfo{}
	}
	fs, ok := st.(*notes.FileStore)
	if !ok {
		return NotesStoreInfo{}
	}
	s.mu.Lock()
	key := s.notesKey
	s.mu.Unlock()
	return NotesStoreInfo{Dir: fs.Root(), Key: key}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && go test ./internal/domain -run 'TestNotesKeyVector|TestNotesStorePath' -v && go test ./internal/domain ./internal/notes`
Expected: PASS, and the whole `domain` + `notes` packages still green (the refactor must not change where a store resolves — `TestNotesStorePathResolved` pins the path).

- [ ] **Step 6: Commit**

```bash
cd /work/gigagit/.claude/worktrees/gg-notes-store-format
gg add internal/domain/notesstore.go internal/notes/file_store.go internal/domain/notesstore_path_test.go
git commit -F <msgfile>   # "domain: NotesStorePath — the resolved notes store dir and key"
```

---

### Task 4: `gg overview store [--json]` + docs that name it

**Files:**
- Modify: `internal/cli/overview.go:19-45` (usage + dispatch), append `overviewStore`
- Modify: `e2e/scenarios/overview_permanent.toml` (one more `[[run]]`)
- Modify: `internal/agentskill/using-gg.md:143-146` (the overview verbs)
- Modify: `internal/agentskill/agentskill.go:38` (`Version = 173` → `174`)
- Modify: `README.md:179-183` (the overview block)
- Modify: `docs/superpowers/specs/2026-10-15-gg-notes-store-format-design.md` §9.3 and §10 step 1 (the oracle's spelling)
- Test: `internal/cli/overview_test.go`

**Interfaces:**
- Consumes: `(*domain.Service).NotesStorePath(ctx) domain.NotesStoreInfo` (Task 3), `runCLI(t, dir, args...) (int, string, string)` and `newRepoDir(t)` (cli test helpers), `openCLIService(t, dir)`.
- Produces: the verb. Text form: two lines `store <dir>/commits.toml` and `key <key>` (`key -` when empty). JSON: `{"dir": "<dir>", "store": "<dir>/commits.toml", "key": "<key>"}`. Exit 1 with `overview store: notes are disabled` when `Dir == ""`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/overview_test.go`:

```go
// `gg overview store` prints the store gg resolved for this repository:
// the oracle of spec 2026-10-15-gg-notes-store-format §9.3.
func TestOverviewStorePrintsDirAndKey(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	svc := openCLIService(t, dir)
	cd, err := svc.GitCommonDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := svc.NotesStorePath(context.Background())
	if want.Dir == "" || want.Key == "" || len(want.Key) != 16 {
		t.Fatalf("NotesStorePath = %+v (common dir %s)", want, cd)
	}
	code, out, errs := runCLI(t, dir, "overview", "store")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if !strings.Contains(out, "store "+filepath.Join(want.Dir, "commits.toml")+"\n") || !strings.Contains(out, "key "+want.Key+"\n") {
		t.Fatalf("text form:\n%s\nwant dir %s key %s", out, want.Dir, want.Key)
	}
	code, out, _ = runCLI(t, dir, "overview", "store", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var row struct{ Dir, Store, Key string }
	if err := json.Unmarshal([]byte(out), &row); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if row.Dir != want.Dir || row.Store != filepath.Join(want.Dir, "commits.toml") || row.Key != want.Key {
		t.Fatalf("json = %+v, want %+v", row, want)
	}
}

// Review focus 5: the oracle works in a repository with no commit, where
// `overview list` cannot (HEAD has no commit).
func TestOverviewStoreOnEmptyRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "-b", "main", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if code, _, _ := runCLI(t, dir, "overview", "list"); code != 1 {
		t.Fatalf("overview list on an empty repo: exit %d, want 1", code)
	}
	code, out, errs := runCLI(t, dir, "overview", "store", "--json")
	if code != 0 || !strings.Contains(out, `"key":"`) || !strings.Contains(out, "commits.toml") {
		t.Fatalf("exit %d out %s err %s", code, out, errs)
	}
}
```

Add `"context"`, `"encoding/json"`, `"os/exec"`, `"path/filepath"` to the file's imports as needed (check which are already there).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && go test ./internal/cli -run 'TestOverviewStore' -v`
Expected: FAIL — `overview store` prints the usage and exits 2.

- [ ] **Step 3: Implement the verb**

In `internal/cli/overview.go`: add the usage line and the dispatch case, then the function.

```go
const permOverviewUsage = `usage: gg overview save [<gg-link>] --agent <name> (--stdin | --file <path>) [--title <t>] [--json] [--dry-run]
       gg overview list [--all] [--json]
       gg overview show <id|gg-link> [--json]
       gg overview export [--out <path>|-] [--force] <id|gg-link>
       gg overview rm <id>
       gg overview store [--json]`
```

```go
	case "store":
		return overviewStore(svc, rest[1:], stdout, stderr)
```

```go
// overviewStore is `gg overview store`: where this repository's notes live
// (the store directory, its commits.toml, the directory key) — the oracle a
// reader of the store format checks its key derivation against (spec
// 2026-10-15-gg-notes-store-format §9.3). Works in a repository with no
// commit: the store is keyed by the git common dir, not by HEAD.
func overviewStore(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("overview store", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(stderr, permOverviewUsage)
		return 2
	}
	info := svc.NotesStorePath(context.Background())
	if info.Dir == "" {
		fmt.Fprintln(stderr, "overview store: notes are disabled")
		return 1
	}
	store := filepath.Join(info.Dir, "commits.toml")
	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(map[string]string{"dir": info.Dir, "store": store, "key": info.Key})
		return 0
	}
	key := info.Key
	if key == "" {
		key = "-"
	}
	fmt.Fprintf(stdout, "store %s\nkey %s\n", store, key)
	return 0
}
```

Add `"path/filepath"` to the file's imports if absent.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && go test ./internal/cli -run 'TestOverview' -v`
Expected: PASS (the new two and the existing overview tests).

- [ ] **Step 5: Add the e2e run**

In `e2e/scenarios/overview_permanent.toml`, after the `overview list --all --json` run and before `[expect]`:

```toml
[[run]]
cmd   = ["overview", "store", "--json"]
exit  = 0
stdout_contains = ['"key":"', 'commits.toml']
```

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && go test ./e2e -run 'TestScenarios/overview_permanent' -v` (if the harness names subtests differently, `go test ./e2e -run overview_permanent -v`).
Expected: PASS.

- [ ] **Step 6: Update the skill text and bump its version**

In `internal/agentskill/using-gg.md`, after the `gg overview rm <id>` line (`- \`gg overview rm <id>\` — delete one.`), add:

```markdown
- `gg overview store [--json]` — where this repository's overviews live on
  disk: `store <dir>/commits.toml` and `key <key>` (`--json` → `{dir, store,
  key}`). The oracle for a tool that reads gg's notes store directly (the
  spec: docs/specs/gg-notes-store/README.md in the gg source); works in a
  repository with no commit yet.
```

In `internal/agentskill/agentskill.go` change `const Version = 173` to `const Version = 174`.

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && go test ./internal/agentskill`
Expected: PASS (a golden/version guard, if any, now sees the bumped version).

- [ ] **Step 7: Update README and the spec's oracle spelling**

In `README.md`, after the `gg overview rm <id>` line in the overview block, add:

```
gg overview store [--json]           # where this repo's overviews live: the store file + its key (for other readers of the store)
```

In the spec `docs/superpowers/specs/2026-10-15-gg-notes-store-format-design.md`:
- §9.3 — replace the paragraph with:

  > `gg overview store [--json]` prints the store gg resolved for the current repository: `store <dir>/commits.toml` and `key <key>` (`--json` → `{"dir","store","key"}`). Running it in the repository the plugin targets proves the plugin's §3.2 derivation on any platform in one command, including a repository with no commit yet. (A separate verb, not fields on `gg overview list --json`: that command prints a JSON array of rows, and prints no row in a repository without an overview.)
- §3.2's "verify via the oracle in §9.3" and §10 step 1's "Confirm with `gg overview list --json` (§9.3)" — change the command name to `gg overview store --json`.
- §9.1 — the schema paths are as shipped (`docs/specs/gg-notes-store/…`); no change unless Task 1 moved them.

- [ ] **Step 8: Commit**

```bash
cd /work/gigagit/.claude/worktrees/gg-notes-store-format
gg add internal/cli/overview.go internal/cli/overview_test.go e2e/scenarios/overview_permanent.toml internal/agentskill/using-gg.md internal/agentskill/agentskill.go README.md docs/superpowers/specs/2026-10-15-gg-notes-store-format-design.md
git commit -F <msgfile>   # "cli: gg overview store — the notes store path and key (the store-format oracle); skill v174"
```

---

### Task 5: CHANGELOG, full gate, handoff note

**Files:**
- Modify: `CHANGELOG.md` (top of Unreleased)
- Modify: `docs/CLAUDE-details.md` (one line near `gg overview save|list|show|rm`, ~line 7277)

- [ ] **Step 1: CHANGELOG entry**

Insert directly under the `No tagged release has been cut yet; everything lives under **Unreleased**.` paragraph (above the current top section):

```markdown
## gg notes store format: a spec other tools can read overviews with

### Added
- `docs/superpowers/specs/2026-10-15-gg-notes-store-format-design.md`: the
  stored form of a permanent overview — where the store is (`<state>/gg/
  notes/<key>/commits.toml`, the key = the first 8 bytes of SHA-256 over the
  cleaned git common dir), the `[[notes]]` envelope, the canonical document,
  the anchor grammar, the markdown subset and the reader rules — written so
  a reader that is not gg (first: an IDEA plugin) can show the same overview
  from the same files. Conformance material ships in
  `docs/specs/gg-notes-store/`: a strict JSON Schema (gg's own drift guard,
  checked against gg's canonical output by a test), a lenient twin for
  readers, and a fixture store gg itself reads in a test.
- `gg overview store [--json]`: the store file and key gg resolved for the
  current repository — the oracle a reader checks its key derivation with;
  works in a repository with no commit yet.
```

- [ ] **Step 2: CLAUDE-details line**

In `docs/CLAUDE-details.md`, extend the line that reads `- **CLI:** \`gg overview save|list|show|rm\` (internal/cli/overview.go); save` so the verb list is `save|list|show|export|rm|store` and append one sentence: `\`store\` prints \`domain.NotesStorePath\` (dir + key) — the oracle of the store-format spec (docs/specs/gg-notes-store/).`

- [ ] **Step 3: Run the full race gate**

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && ./test.sh race 2>&1 | tee /tmp/claude-1000/-work-gigagit/3f942bc5-9389-49a0-a00f-fef0425e1a09/scratchpad/race.log; grep -n 'all green\|FAIL' /tmp/claude-1000/-work-gigagit/3f942bc5-9389-49a0-a00f-fef0425e1a09/scratchpad/race.log | tail -5`
Expected: the log ends with "all green"; no `FAIL` line. (The `| tee` exit status is tee's — read the log, do not trust `$?`.)

- [ ] **Step 4: Build the verify binary**

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && go build -o bin/gg ./cmd/gg && ./bin/gg overview store --json`
Expected: prints `{"dir":"…/notes/d10200a3a219735f","store":"…/commits.toml","key":"d10200a3a219735f"}` for this worktree (its common dir is `/work/gigagit/.git`) — the spec's Linux vector, end to end. `bin/` is gitignored; never `git add -A`.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/gg-notes-store-format
gg add CHANGELOG.md docs/CLAUDE-details.md
git commit -F <msgfile>   # "changelog: gg notes store format spec + gg overview store"
```

- [ ] **Step 6: Report**

Tell the user: the branch, the absolute path `/work/gigagit/.claude/worktrees/gg-notes-store-format/bin/gg`, the race-gate result, and that the plugin handoff is spec §10 plus `docs/specs/gg-notes-store/README.md`. Merging is the user's call (ask; never push).
