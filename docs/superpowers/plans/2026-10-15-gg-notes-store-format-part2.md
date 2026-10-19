# gg notes store format part 2 — conformance material Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the gg-side conformance material part 2 of the spec promises — review-document schemas, fixture records of every new kind written by gg's own encoder, portable test vectors for the two fingerprints and the relocation rule — and hand the plugin repository its copy.

**Architecture:** No change to how gg stores anything. New data files under `docs/specs/gg-notes-store/`; a golden generator test in `internal/domain` that WRITES the fixture through `notes.FileStore` (so the TOML spelling of `meta`/`display`/`added`/`[[resolved]]`/worktree parts is gg's, not hand-typed) and fails when the committed fixture drifts; read-through tests via `Reviews`/`NotesAt`/`Overviews`; vector/case files in JSON that both gg's tests and the plugin's can load.

**Tech Stack:** Go 1.26, `github.com/google/jsonschema-go v0.4.3` (direct, MIT), `github.com/pelletier/go-toml/v2` (direct), real `git` in `t.TempDir()`.

**Spec:** `docs/superpowers/specs/2026-10-15-gg-notes-store-format-part2-design.md` (§9 conformance material), extending `2026-10-15-gg-notes-store-format-design.md` (part 1).

## Global Constraints

- Worktree `/work/gigagit/.claude/worktrees/gg-notes-store-format`, branch `spec/gg-notes-store-format` (part 1 is already on it, unmerged); every path is relative to it.
- No new Go dependency. `internal/notebatch` production code stays stdlib + notemeta; its tests may import go-toml/jsonschema (archtest guards production imports only).
- Tests never write into `docs/` except the golden generator run with `-update`, which the implementer runs on purpose and whose output they read before committing. Every new test calls `t.Parallel()` unless it takes `-update` or `t.Setenv`.
- Fixture records use fixed ids, fixed timestamps (UTC), fixed shas; the golden test must be byte-deterministic.
- `gg add` + raw `git commit -F <msgfile>`; never `git add -A`; attribution lines per the session reminder.
- `./test.sh race` from the worktree before declaring done; "all green" in the log is the only pass signal.
- Docs after: `CHANGELOG.md`, `docs/specs/gg-notes-store/README.md`, `docs/CLAUDE-details.md` one line. No CLI change → no skill bump, no README.md change.

## Review Focus

1. A review document whose remark has BOTH `newRange` and `oldRange`, or neither: the strict and lenient schemas must both reject it (gg refuses it), else a reader accepts what gg never stores. → Task 1 (`TestReviewSchemaStrictRejectsLenientAccepts`).
2. A `display` declaration with an unknown colour or a two-character glyph: the schemas must REJECT (the vocabularies are closed), while gg's parser drops the entry and keeps the rest — the spec says so; the test pins both behaviours side by side so a reader knows a schema-invalid stored document can exist. → Task 1 (`TestDisplayVocabularyIsClosed`).
3. The fixture's worktree part: `wkey` must equal `sha256(Worktree)[:8]` of the path stored in the record — the self-check the spec tells readers to run. → Task 2 (`TestFixtureWorktreeKeyMatchesRecord`).
4. A reply whose root is in another part (a reply to the PR review, stored in `previews.toml`): `NotesAt`/`Reviews` must thread it under its root, not list it alone. → Task 3 (`TestFixtureReplyThreadsUnderItsRoot`).
5. Relocation when the anchored lines appear TWICE in the file, once above and once below the stored start at the same distance: the LOWER window wins (spec §5.2 "the lower candidate checked before the higher"). → Task 3 (a case in `relocation-cases.json`, `TestRelocationCases`).

---

### Task 1: Review-document schemas + notebatch conformance test

**Files:**
- Create: `docs/specs/gg-notes-store/review-document.schema.json`
- Create: `docs/specs/gg-notes-store/review-document.lenient.schema.json`
- Test: `internal/notebatch/review_schema_test.go`

**Interfaces:**
- Consumes: `notebatch.ParseReview([]byte) (ReviewDoc, error)`, `ReviewDoc.Canonical() []byte`, test helpers `loadSchema`, `validateJSON` from `internal/notebatch/schema_test.go` (Task 1 of part 1).
- Produces: the two schema files; Task 2's fixture review documents validate against the strict one.

- [ ] **Step 1: Write the failing test**

`internal/notebatch/review_schema_test.go`:

```go
package notebatch

import "testing"

// Every canonical review document gg writes validates against the strict
// review schema — part 2 §9's drift guard.
func TestCanonicalReviewMatchesStrictSchema(t *testing.T) {
	t.Parallel()
	strict := loadSchema(t, "review-document.schema.json")
	lenient := loadSchema(t, "review-document.lenient.schema.json")
	docs := []string{
		`{"version":1,"summary":"## Fine\nNothing to say."}`,
		`{"version":1,"summary":"text","overview":"[gate](internal/x/gate.go:3-4)","meta":{"verdict":"approve","n":3},
		  "display":{"severity":{"show":["badge","count"],"label":"Severity","order":1,"style":"filled","as":"level",
		    "values":{"high":{"color":"red","glyph":"!","level":"high"},"low":{"color":"yellow","level":"low"}},"other":{"color":"grey"}}},
		  "files":[{"path":"internal/x/gate.go","summary":"the gate","meta":{"touched":"yes"},
		    "annotations":[{"newRange":[3,4],"summary":"The refusal.","rationale":"why","meta":{"severity":"high"},"display":{"severity":{"show":["frame"]}}},
		                   {"oldRange":[2,2],"summary":"was open"}]}]}`,
		`{"version":1,"summary":"legacy fields fold into meta","files":[{"path":"a.go","annotations":[{"newRange":[1,1],"summary":"s","tags":["kind: question"],"confidence":"high"}]}]}`,
	}
	for _, in := range docs {
		d, err := ParseReview([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		canon := d.Canonical()
		if err := validateJSON(strict, canon); err != nil {
			t.Errorf("strict review schema rejects gg's canonical form:\n%s\n%v", canon, err)
		}
		if err := validateJSON(lenient, canon); err != nil {
			t.Errorf("lenient review schema rejects gg's canonical form:\n%s\n%v", canon, err)
		}
	}
}

// Review focus 1 + the lenient/strict split: unknown keys pass only the
// lenient schema; the shapes gg refuses pass neither.
func TestReviewSchemaStrictRejectsLenientAccepts(t *testing.T) {
	t.Parallel()
	strict := loadSchema(t, "review-document.schema.json")
	lenient := loadSchema(t, "review-document.lenient.schema.json")
	extra := []byte(`{"version":1,"summary":"s","future":1,"files":[{"path":"a","extra":true,"annotations":[{"newRange":[1,1],"summary":"s","confidence":0.5}]}]}`)
	if err := validateJSON(strict, extra); err == nil {
		t.Fatal("strict schema accepted unknown keys")
	}
	if err := validateJSON(lenient, extra); err != nil {
		t.Fatalf("lenient schema rejected unknown keys: %v", err)
	}
	for _, bad := range []string{
		`{"version":1,"summary":""}`,
		`{"version":2,"summary":"s"}`,
		`{"version":1,"summary":"s","files":[{"path":"a","annotations":[{"newRange":[1,1],"oldRange":[1,1],"summary":"s"}]}]}`,
		`{"version":1,"summary":"s","files":[{"path":"a","annotations":[{"summary":"s"}]}]}`,
		`{"version":1,"summary":"s","files":[{"path":"a","annotations":[{"newRange":[2,1],"summary":"s"}]}]}`,
		`{"version":1,"summary":"s","files":[{"path":"","annotations":[]}]}`,
		`{"version":1,"summary":"s","meta":"not an object"}`,
	} {
		if err := validateJSON(strict, []byte(bad)); err == nil {
			t.Errorf("strict schema accepted %s", bad)
		}
		if err := validateJSON(lenient, []byte(bad)); err == nil {
			t.Errorf("lenient schema accepted %s", bad)
		}
	}
}

// Review focus 2: the display vocabularies are closed in the schemas, while
// gg's parser keeps the document and drops the bad entry — a reader must
// expect a stored document its schema rejects on display alone.
func TestDisplayVocabularyIsClosed(t *testing.T) {
	t.Parallel()
	strict := loadSchema(t, "review-document.schema.json")
	bad := `{"version":1,"summary":"s","display":{"k":{"show":["badge"],"values":{"v":{"color":"crimson","glyph":"ab"}}}}}`
	if err := validateJSON(strict, []byte(bad)); err == nil {
		t.Fatal("schema accepted colour crimson / glyph ab")
	}
	d, err := ParseReview([]byte(bad))
	if err != nil {
		t.Fatalf("gg refused the document outright: %v", err)
	}
	// ParseReview checks only the declaration's SHAPE (unknown fields); the
	// vocabularies are checked when the note is displayed (notemeta.Resolve).
	if r := notemeta.Resolve(nil, d.Display, nil); len(r.Problems) == 0 {
		t.Fatal("gg displayed the bad colour/glyph without naming a problem")
	}
}
```

Add `"github.com/homeend/gigagit/internal/notemeta"` to the imports.

- [ ] **Step 2: Run to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && go test ./internal/notebatch -run 'TestCanonicalReviewMatchesStrictSchema|TestReviewSchemaStrictRejectsLenientAccepts|TestDisplayVocabularyIsClosed' 2>&1 | tail -6`
Expected: FAIL — `open ../../docs/specs/gg-notes-store/review-document.schema.json: no such file`.

- [ ] **Step 3: Write the strict schema**

`docs/specs/gg-notes-store/review-document.schema.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://gigagit.dev/specs/gg-notes-store/review-document.schema.json",
  "title": "gg review document (stored canonical form)",
  "description": "The JSON text gg stores in a review record's `rationale` (spec part 2 §3.2). STRICT: gg's own drift guard; a reader validates leniently (review-document.lenient.schema.json). Length limits here are code points; gg enforces BYTES after trimming. The display vocabularies are closed here; gg's parser drops an invalid display entry and keeps the document.",
  "$defs": {
    "meta": { "type": "object", "additionalProperties": { "type": "string" } },
    "displayValue": {
      "type": "object", "additionalProperties": false,
      "properties": {
        "color": { "enum": ["red", "orange", "yellow", "green", "blue", "purple", "grey"] },
        "glyph": { "type": "string", "pattern": "^([!-~]|[◆·→»•])$" },
        "level": { "enum": ["high", "medium", "low", "info"] },
        "audience": { "enum": ["author", "reviewer"] }
      }
    },
    "display": {
      "type": "object", "maxProperties": 8,
      "additionalProperties": {
        "type": "object", "additionalProperties": false,
        "properties": {
          "show": { "type": "array", "items": { "enum": ["badge", "frame", "count", "line"] } },
          "label": { "type": "string" },
          "order": { "type": "integer" },
          "style": { "enum": ["filled", "text"] },
          "as": { "enum": ["level", "audience"] },
          "values": { "type": "object", "maxProperties": 16, "additionalProperties": { "$ref": "#/$defs/displayValue" } },
          "other": { "$ref": "#/$defs/displayValue" }
        }
      }
    },
    "range": { "type": "array", "items": { "type": "integer", "minimum": 1 }, "minItems": 2, "maxItems": 2 },
    "annotation": {
      "type": "object", "additionalProperties": false,
      "required": ["summary"],
      "oneOf": [ { "required": ["newRange"] }, { "required": ["oldRange"] } ],
      "properties": {
        "newRange": { "$ref": "#/$defs/range" },
        "oldRange": { "$ref": "#/$defs/range" },
        "summary": { "type": "string", "minLength": 1 },
        "rationale": { "type": "string" },
        "meta": { "$ref": "#/$defs/meta" },
        "display": { "$ref": "#/$defs/display" }
      }
    },
    "file": {
      "type": "object", "additionalProperties": false,
      "required": ["path"],
      "properties": {
        "path": { "type": "string", "minLength": 1 },
        "summary": { "type": "string" },
        "meta": { "$ref": "#/$defs/meta" },
        "display": { "$ref": "#/$defs/display" },
        "annotations": { "type": "array", "items": { "$ref": "#/$defs/annotation" } }
      }
    }
  },
  "type": "object",
  "additionalProperties": false,
  "required": ["version", "summary"],
  "properties": {
    "version": { "const": 1 },
    "summary": { "type": "string", "minLength": 1 },
    "overview": { "type": "string", "minLength": 1, "maxLength": 65536 },
    "meta": { "$ref": "#/$defs/meta" },
    "display": { "$ref": "#/$defs/display" },
    "files": { "type": "array", "items": { "$ref": "#/$defs/file" } }
  }
}
```

Note the `oneOf` on `annotation`: exactly one of the two ranges (both → fails both branches; neither → fails both).

- [ ] **Step 4: Write the lenient schema**

Same file with: `$id` ending `review-document.lenient.schema.json`; `title` `"gg review document (reader's lenient form)"`; `description` `"The shape a READER validates with: required fields, types and the closed display vocabularies as the strict schema, unknown keys tolerated (spec part 1 §8.3)."`; every `"additionalProperties": false` → `true` EXCEPT the three places where `additionalProperties` is a schema (`meta`, `display`, `values`) — those stay as they are, since they define the map's values, not unknown keys.

- [ ] **Step 5: Run to verify it passes**

Run: the Step 2 command. Expected: PASS ×3.

- [ ] **Step 6: Commit**

```bash
cd /work/gigagit/.claude/worktrees/gg-notes-store-format
gg add docs/specs/gg-notes-store/review-document.schema.json docs/specs/gg-notes-store/review-document.lenient.schema.json internal/notebatch/review_schema_test.go
git commit -F <msgfile>   # "notebatch: JSON Schemas for the stored review document + conformance test"
```

---

### Task 2: Fixture records of every new kind, written by gg's own encoder

**Files:**
- Modify: `docs/specs/gg-notes-store/fixture/commits.toml` (regenerated; part-1 records kept)
- Modify: `docs/specs/gg-notes-store/fixture/previews.toml` (regenerated; + a PR review and a reply to it)
- Create: `docs/specs/gg-notes-store/fixture/worktrees/<wkey>.toml` (generated)
- Create: `docs/specs/gg-notes-store/fixture/README.md`
- Create: `internal/domain/fixture_golden_test.go`

**Interfaces:**
- Consumes: `notes.NewFileStore(root) *FileStore`, `(*FileStore).Put(model.Note) error`, `(*FileStore).Resolve(model.ThreadResolution) error`, `notes.WorktreePart(top) Part`, `notebatch.ParseReview` + `ReviewDoc.Canonical()`, `notebatch.ParseOverview` + `OverviewDoc.Canonical()`, `model.NoteContextHash([]string) string`, `remarkFP(path, side, rng, summary)` (domain, unexported — the test is in package `domain`).
- Produces: the fixture files + `fixtureNotes()` (the Go list of every fixture record, reused by Task 3) and the constants `fixtureWorktree = "/home/dev/src/gate"`, `fixtureCommit = "d5efa035df1f3c4c949040bebb924a35d0ea61a0"`, `fixturePRHead = "e4a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3"`, `fixturePRBase = "0a0b0c0d0e0f1a1b2c3d4e5f6a7b8c9d0e1f2a3b"`.

- [ ] **Step 1: Write the golden generator test (fails: fixture differs)**

`internal/domain/fixture_golden_test.go`:

```go
package domain

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/notemeta"
	"github.com/homeend/gigagit/internal/notes"
)

var updateFixture = flag.Bool("update-fixture", false, "rewrite docs/specs/gg-notes-store/fixture/ from fixtureNotes() (read the diff before committing)")

const (
	fixtureWorktree = "/home/dev/src/gate"
	fixtureCommit   = "d5efa035df1f3c4c949040bebb924a35d0ea61a0"
	fixturePRHead   = "e4a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3"
	fixturePRBase   = "0a0b0c0d0e0f1a1b2c3d4e5f6a7b8c9d0e1f2a3b"
)

func fixtureDir() string { return filepath.Join("..", "..", "docs", "specs", "gg-notes-store", "fixture") }

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

func canonOverview(t *testing.T, in string) string {
	t.Helper()
	d, err := notebatch.ParseOverview([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	return string(d.Canonical())
}

func canonReview(t *testing.T, in string) string {
	t.Helper()
	d, err := notebatch.ParseReview([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	return string(d.Canonical())
}

// fixtureGateLines are internal/x/gate.go's lines 1–5 at fixtureCommit —
// what the line notes' context_hash values fingerprint (fixture README).
var fixtureGateLines = []string{"func gate(closed bool) bool {", "\topen := !closed", "\tif closed {", "\t\treturn false", "\t}", "\treturn open", "}"}

// fixtureNotes is every record of the spec's fixture store, in the order
// gg writes them (by created). Part 1's records come first, unchanged.
func fixtureNotes(t *testing.T) ([]model.Note, []model.ThreadResolution) {
	t.Helper()
	committed := func(path string) model.FileAddress {
		return model.FileAddress{State: model.StateCommitted, Commit: fixtureCommit, Path: path}
	}
	live := func(path string) model.FileAddress {
		return model.FileAddress{State: model.StateUnstaged, Worktree: fixtureWorktree, Path: path}
	}
	overview := canonOverview(t, `{"version":1,"title":"How the gate works","overview":"The gate is one function.\n\n## 1. The check\n\n- [`+"`gate`"+`](internal/x/gate.go:3-4) refuses a closed door.\n- [this line](internal/x/gate.go:2) opens it.\n\n## 2. The caller\n\n- [the call](internal/y/call.go:1) runs the gate first. See [the config](config.toml).\n\nSkipped: nothing. Next: none.","files":[{"path":"internal/x/gate.go","annotations":[{"newRange":[3,4],"summary":"The refusal.","rationale":"A closed door must not pass."}]},{"path":"internal/y/call.go","annotations":[{"newRange":[1,1],"summary":"The only caller."}]}]}`)
	prOverview := canonOverview(t, `{"version":1,"title":"PR 7: the gate, reviewed","overview":"The pull request adds [the gate](internal/x/gate.go:1-4)."}`)
	commitReview := canonReview(t, `{"version":1,"summary":"## Summary\nThe gate refuses a closed door.\n\n## Findings\n- one high finding","overview":"Start at [the refusal](internal/x/gate.go:3-4).","meta":{"verdict":"approve"},"display":{"severity":{"show":["badge","count"],"label":"Severity","order":1,"style":"filled","as":"level","values":{"high":{"color":"red","glyph":"!","level":"high"},"low":{"color":"yellow","level":"low"}},"other":{"color":"grey"}}},"files":[{"path":"internal/x/gate.go","summary":"the gate itself","annotations":[{"newRange":[3,4],"summary":"The refusal returns false.","rationale":"A closed door must not pass.","meta":{"severity":"high"}},{"oldRange":[2,2],"summary":"The old open flag is gone.","meta":{"severity":"low"}}]}]}`)
	prReview := canonReview(t, `{"version":1,"summary":"## Summary\nPR 7 adds the gate. Fine.","files":[{"path":"internal/x/gate.go","annotations":[{"newRange":[1,1],"summary":"Name the parameter.","meta":{"severity":"low"}}]}]}`)
	workingReview := canonReview(t, `{"version":1,"summary":"## Summary\nThe uncommitted change renames open.","files":[{"path":"internal/x/gate.go","annotations":[{"newRange":[2,2],"summary":"Rename it back.","meta":{"severity":"medium"}}]}]}`)
	_ = notemeta.KV{}
	ns := []model.Note{
		// ---- part 1 (unchanged) ----
		{ID: "7cf0812f", Source: model.NoteSourceAgent, Author: "Claude Opus 5.5", Side: model.NoteSideNew, Tags: []string{"overview"},
			Address: model.FileAddress{State: model.StateCommitted, Commit: fixtureCommit, Branch: "main"},
			Summary: "How the gate works", Rationale: overview, Created: at("2026-10-13T23:53:56.742993129Z"), Updated: at("2026-10-13T23:53:56.742993129Z")},
		{ID: "3a9e1c20", Source: model.NoteSourceAgent, Author: "ada", Side: model.NoteSideNew, Tags: []string{"overview"}, Preview: "main...refs/gg/pr/7",
			Address: model.FileAddress{State: model.StateCommitted, Commit: fixturePRHead},
			Summary: "PR 7: the gate, reviewed", Rationale: prOverview, Created: at("2026-10-14T08:00:00Z"), Updated: at("2026-10-14T08:00:00Z")},
		{ID: "91f0aa07", Source: model.NoteSourceAgent, Author: "ada", Side: model.NoteSideNew, Range: [2]int{3, 4}, Tags: []string{"review"}, Scope: "d5efa035..e4a1b2c3",
			Address: model.FileAddress{State: model.StateCommitted, Commit: fixturePRHead, Path: "internal/x/gate.go"},
			Summary: "A review remark, not an overview", Rationale: "A reader of overview records skips this record: it has a Path and no overview tag.",
			Created: at("2026-10-14T08:05:00Z"), Updated: at("2026-10-14T08:05:00Z")},
		{ID: "b2c3d4e5", Source: model.NoteSourceUser, Side: model.NoteSideNew, Range: [2]int{2, 2}, Tags: []string{"overview"},
			Address: committed("internal/x/gate.go"),
			Summary: "Tagged overview but a FILE note", Rationale: "A reader skips this record: it carries the overview tag but has a Path (not commit-level).",
			Created: at("2026-10-14T08:06:00Z"), Updated: at("2026-10-14T08:06:00Z")},
		{ID: "c3d4e5f6", ParentID: "7cf0812f", Source: model.NoteSourceUser, Side: model.NoteSideNew, Tags: []string{"overview"},
			Address: model.FileAddress{State: model.StateCommitted, Commit: fixtureCommit},
			Summary: "A reply under the overview", Rationale: "A reader skips this record: it has a parent_id (a reply), whatever its tags say.",
			Created: at("2026-10-14T08:07:00Z"), Updated: at("2026-10-14T08:07:00Z")},
		// ---- part 2: reviews ----
		{ID: "c0ffee01", Source: model.NoteSourceAgent, Author: "ada", Side: model.NoteSideNew, Tags: []string{"review"},
			Scope: fixtureCommit + "^.." + fixtureCommit, Address: model.FileAddress{State: model.StateCommitted, Commit: fixtureCommit, Branch: "main"},
			Summary: "Review: d5efa035 Add the gate (d5efa03^..d5efa03)", Rationale: commitReview,
			Created: at("2026-10-14T09:00:00Z"), Updated: at("2026-10-14T09:00:00Z")},
		{ID: "c0ffee02", Source: model.NoteSourceAgent, Author: "ada", Side: model.NoteSideNew, Tags: []string{"review"},
			Scope: fixturePRBase + ".." + fixturePRHead, Preview: "main...refs/gg/pr/7",
			Address: model.FileAddress{State: model.StateCommitted, Commit: fixturePRHead},
			Summary: "Review: PR #7 (0a0b0c0..e4a1b2c)", Rationale: prReview,
			Created: at("2026-10-14T09:10:00Z"), Updated: at("2026-10-14T09:10:00Z")},
		// a reply to remark 0 of the PR review (lives in the review's part: previews.toml)
		{ID: "c0ffee03", ParentID: "c0ffee02", Source: model.NoteSourceUser, Author: "me", Side: model.NoteSideNew,
			Address: model.FileAddress{State: model.StateCommitted, Commit: fixturePRHead},
			Summary: "Done in the next push.", Remark: "review:c0ffee02:0",
			RemarkFP: remarkFP("internal/x/gate.go", model.NoteSideNew, [2]int{1, 1}, "Name the parameter."), RemarkSummary: "Name the parameter.",
			Created: at("2026-10-14T09:20:00Z"), Updated: at("2026-10-14T09:20:00Z")},
		// ---- part 2: a committed line note, its reply ----
		{ID: "aa110001", Source: model.NoteSourceUser, Author: "me", Side: model.NoteSideNew, Range: [2]int{3, 4},
			ContextHash: model.NoteContextHash(fixtureGateLines[2:4]), Address: committed("internal/x/gate.go"),
			Summary: "Why not return early?", Rationale: "The open flag is computed and then ignored on this path.",
			Meta: []notemeta.KV{{Key: "kind", Value: "question"}},
			Display: notemeta.Display{"kind": {Show: []string{"badge"}, Values: map[string]notemeta.Value{"question": {Color: "blue", Glyph: "?"}}}},
			Added: []notemeta.Added{{Key: "status", Value: "agreed", By: "ada", At: at("2026-10-14T10:05:00Z")}},
			Created: at("2026-10-14T10:00:00Z"), Updated: at("2026-10-14T10:00:00Z")},
		{ID: "aa110002", ParentID: "aa110001", Source: model.NoteSourceAgent, Author: "ada", Side: model.NoteSideNew, Range: [2]int{3, 4},
			ContextHash: model.NoteContextHash(fixtureGateLines[2:4]), Address: committed("internal/x/gate.go"),
			Summary: "Agreed; an early return reads better.", Link: "gg://gate/internal/x/gate.go@" + fixtureCommit + ":3",
			Created: at("2026-10-14T10:04:00Z"), Updated: at("2026-10-14T10:04:00Z")},
		// ---- part 2: live notes in the worktree part ----
		{ID: "bb220001", Source: model.NoteSourceUser, Author: "me", Side: model.NoteSideNew, Range: [2]int{6, 6},
			ContextHash: model.NoteContextHash(fixtureGateLines[5:6]), Address: live("internal/x/gate.go"),
			Summary: "Still returns the flag.", Created: at("2026-10-14T11:00:00Z"), Updated: at("2026-10-14T11:00:00Z")},
		{ID: "bb220002", Source: model.NoteSourceAgent, Author: "ada", Side: model.NoteSideNew, Tags: []string{"review"},
			Address: model.FileAddress{State: model.StateUnstaged, Worktree: fixtureWorktree},
			Files:   []model.NoteFile{{Path: "internal/x/gate.go", Blob: "5d2a7c1e9b3f4a6d8c0e2f1a3b5c7d9e1f3a5b7c"}},
			Summary: "Review: working changes", Rationale: workingReview,
			Created: at("2026-10-14T11:30:00Z"), Updated: at("2026-10-14T11:30:00Z")},
	}
	res := []model.ThreadResolution{{Root: "aa110001", By: "me", At: at("2026-10-14T10:06:00Z")}}
	return ns, res
}

// writeFixture writes every record through gg's own store into dir.
func writeFixture(t *testing.T, dir string) {
	t.Helper()
	ns, res := fixtureNotes(t)
	st := notes.NewFileStore(dir)
	for _, n := range ns {
		if err := st.Put(n); err != nil {
			t.Fatalf("put %s: %v", n.ID, err)
		}
	}
	for _, r := range res {
		if err := st.Resolve(r); err != nil {
			t.Fatalf("resolve %s: %v", r.Root, err)
		}
	}
}

func fixtureFiles() []string {
	wkey := strings.TrimPrefix(string(notes.WorktreePart(fixtureWorktree)), "wt-")
	return []string{"commits.toml", "previews.toml", filepath.Join("worktrees", wkey+".toml")}
}

// The committed fixture is byte-for-byte what gg's store writes for
// fixtureNotes (spec part 2 §9). -update-fixture regenerates it.
func TestFixtureIsWhatGGWrites(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir)
	for _, name := range fixtureFiles() {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("gg wrote no %s: %v", name, err)
		}
		target := filepath.Join(fixtureDir(), name)
		if *updateFixture {
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, got, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("%s: %v (run with -update-fixture)", name, err)
		}
		if string(want) != string(got) {
			t.Errorf("%s differs from what gg writes (run with -update-fixture and read the diff)", name)
		}
	}
}

// Review focus 3: the worktree part's key is the hash of the path stored in
// its records — the reader's self-check (spec part 2 §5.1).
func TestFixtureWorktreeKeyMatchesRecord(t *testing.T) {
	t.Parallel()
	wkey := strings.TrimPrefix(string(notes.WorktreePart(fixtureWorktree)), "wt-")
	if len(wkey) != 16 || wkey != repoKey(fixtureWorktree) {
		t.Fatalf("wkey %q is not sha256(Clean(%q))[:8] = %q", wkey, fixtureWorktree, repoKey(fixtureWorktree))
	}
	data, err := os.ReadFile(filepath.Join(fixtureDir(), "worktrees", wkey+".toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Worktree = '"+fixtureWorktree+"'") {
		t.Fatalf("worktree part does not carry its path:\n%s", data)
	}
}
```

(`repoKey` is `shelfstore.go`'s; `notes.WorktreePart` uses the same formula, so both must agree — that is the assertion.)

- [ ] **Step 2: Run to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && go test ./internal/domain -run 'TestFixtureIsWhatGGWrites|TestFixtureWorktreeKeyMatchesRecord' 2>&1 | tail -6`
Expected: FAIL — `commits.toml differs`, `previews.toml differs`, `worktrees/<wkey>.toml: no such file`. If `Put` rejects a record (e.g. a reply whose root is not yet stored — Put files a reply in its root's part, so roots must be Put BEFORE their replies; the list above keeps that order), fix the ORDER, not the test.

- [ ] **Step 3: Regenerate the fixture and read the diff**

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && go test ./internal/domain -run TestFixtureIsWhatGGWrites -update-fixture && git diff --stat docs/specs/gg-notes-store/fixture && git diff docs/specs/gg-notes-store/fixture/commits.toml | head -80`
Expected: the three files written; the part-1 records in `commits.toml` keep their bytes except for ordering/spacing gg's encoder chooses (read the diff: the two overview documents must be unchanged — the schema pin of part 1 will say so); `previews.toml` gains the PR review and its reply; the worktree file exists. Then run WITHOUT the flag: PASS.

- [ ] **Step 4: Part-1 tests still pass on the regenerated fixture**

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && go test ./internal/domain -run 'TestFixture' && go test ./internal/notebatch -run 'TestFixtureDocumentsAreCanonical'`
Expected: PASS. `TestFixtureDocumentsAreCanonical` counts overview records with the §4.3 predicate — still 2. If `TestFixtureStoreCannotRenderRowSurvives`'s string replace no longer matches (the PR overview's rationale line moved), update its `strings.Replace` needle to the regenerated bytes — the needle is the only thing allowed to change there.

- [ ] **Step 5: Fixture README**

`docs/specs/gg-notes-store/fixture/README.md`:

```markdown
# fixture — a notes store written by gg itself

Generated by `go test ./internal/domain -run TestFixtureIsWhatGGWrites -update-fixture`
from `fixtureNotes()` in `internal/domain/fixture_golden_test.go`; the test
fails when these files drift from what gg's store writes.

| file | records |
|------|---------|
| `commits.toml` | `7cf0812f` commit overview (part 1) · `91f0aa07` review REMARK-shaped note with a path (skip as overview) · `b2c3d4e5` overview-tagged file note (skip) · `c3d4e5f6` reply to the overview (skip) · `c0ffee01` commit review (new- and old-side remarks, `meta`, `display`) · `aa110001` line note on `internal/x/gate.go:3-4` with record-level `meta`/`display`/`added` · `aa110002` its reply (with `link`) · `[[resolved]]` for `aa110001` |
| `previews.toml` | `3a9e1c20` PR overview (part 1) · `c0ffee02` PR review (`scope`, `preview`) · `c0ffee03` reply to its remark 0 (`remark`, `remark_fp`) |
| `worktrees/<wkey>.toml` | `bb220001` live line note (`State 3`) · `bb220002` working review with `files[].blob` |

Constants: commit `d5efa035df1f3c4c949040bebb924a35d0ea61a0`, PR head
`e4a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3`, PR base
`0a0b0c0d0e0f1a1b2c3d4e5f6a7b8c9d0e1f2a3b`, worktree `/home/dev/src/gate`
(`<wkey>` = `sha256("/home/dev/src/gate")[:8]`, the file's name).

The `context_hash` values fingerprint these lines of `internal/x/gate.go`
(1-based):

    1 func gate(closed bool) bool {
    2 	open := !closed
    3 	if closed {
    4 		return false
    5 	}
    6 	return open
    7 }

`aa110001`/`aa110002` hash lines 3–4; `bb220001` hashes line 6. The vectors
in `../vectors.json` are computed from the same lines.
```

- [ ] **Step 6: Commit**

```bash
cd /work/gigagit/.claude/worktrees/gg-notes-store-format
gg add internal/domain/fixture_golden_test.go docs/specs/gg-notes-store/fixture
git commit -F <msgfile>   # "spec fixture: every part-2 record kind, written through gg's own store"
```

---

### Task 3: Read-through tests, portable vectors, relocation cases

**Files:**
- Create: `docs/specs/gg-notes-store/vectors.json`
- Create: `docs/specs/gg-notes-store/relocation-cases.json`
- Create: `internal/domain/fixture_part2_test.go`
- Modify: `internal/domain/overview_fixture_test.go:14-27` (`fixtureStoreDir` copies the worktree part too)

**Interfaces:**
- Consumes: `fixtureNotes`, `fixtureStoreDir`, `permOvRepo`, `(*Service).Reviews(ctx)`, `(*Service).NotesAt(ctx, model.FileAddress)`, `resolveOne(model.Note, []string) (model.NoteStatus, [2]int)`, `remarkFP`, `model.NoteContextHash`.
- Produces: the two JSON files a reader loads in its own tests.

- [ ] **Step 1: Extend `fixtureStoreDir` to copy every part**

Replace the `for _, name := range []string{"commits.toml", "previews.toml"}` loop body with one over `fixtureFiles()` that `os.MkdirAll(filepath.Dir(dst))` before writing. Run `go test ./internal/domain -run TestFixtureStore` — still PASS.

- [ ] **Step 2: Write the vectors and cases files**

`docs/specs/gg-notes-store/vectors.json`:

```json
{
  "context_hash": [
    { "lines": ["\tif closed {", "\t\treturn false"], "hash": "FILL" },
    { "lines": ["   return open   "], "hash": "FILL", "note": "trimmed before hashing: equals hash of [\"return open\"]" },
    { "lines": ["return open"], "hash": "FILL" }
  ],
  "remark_fp": [
    { "path": "internal/x/gate.go", "side": "new", "range": [1, 1], "summary": "Name the parameter.", "fp": "FILL" },
    { "path": "./internal/x/gate.go", "side": "old", "range": [2, 2], "summary": "The old open flag is gone.", "fp": "FILL", "note": "a leading ./ is stripped before hashing" }
  ],
  "repo_key": [
    { "common_dir": "/work/gigagit/.git", "key": "d10200a3a219735f" },
    { "common_dir": "/home/homeend/git-focus/.git", "key": "4acf7db7161a42e0" }
  ],
  "worktree_key": [
    { "top": "/home/dev/src/gate", "key": "FILL" }
  ]
}
```

`docs/specs/gg-notes-store/relocation-cases.json` (lines are the CURRENT file; the note is `range` + the hash of `anchored`, the lines it was written on):

```json
[
  { "name": "entry-level note is where it is", "lines": ["a", "b"], "range": [0, 0], "anchored": [], "status": "active", "at": [0, 0] },
  { "name": "unchanged: found where it was", "lines": ["x", "if closed {", "return false", "y"], "range": [2, 3], "anchored": ["if closed {", "return false"], "status": "active", "at": [2, 3] },
  { "name": "re-indented only: still where it was", "lines": ["x", "    if closed {", "        return false", "y"], "range": [2, 3], "anchored": ["if closed {", "return false"], "status": "active", "at": [2, 3] },
  { "name": "moved down two lines", "lines": ["p", "q", "x", "if closed {", "return false", "y"], "range": [2, 3], "anchored": ["if closed {", "return false"], "status": "active", "at": [4, 5] },
  { "name": "moved up one line", "lines": ["if closed {", "return false", "y"], "range": [2, 3], "anchored": ["if closed {", "return false"], "status": "active", "at": [1, 2] },
  { "name": "appears twice at equal distance: the lower wins", "lines": ["t", "t", "m", "u", "u"], "range": [3, 3], "anchored": ["t"], "status": "active", "at": [2, 2], "note": "distance 1: line 2 (lower) checked before line 4" },
  { "name": "text gone, file present: stale, clamped", "lines": ["a", "b"], "range": [5, 6], "anchored": ["gone"], "status": "stale", "at": [2, 2] },
  { "name": "text gone, range inside file: stale at the stored range", "lines": ["a", "b", "c", "d"], "range": [2, 3], "anchored": ["gone"], "status": "stale", "at": [2, 3] },
  { "name": "empty file: stale at 0,0", "lines": [], "range": [1, 1], "anchored": ["a"], "status": "stale", "at": [0, 0] },
  { "name": "side absent: orphaned", "lines": null, "range": [1, 1], "anchored": ["a"], "status": "orphaned", "at": [1, 1] }
]
```

(The equal-distance case: `"t"` at lines 1 and 2, `"u"` at 4 and 5; from 3, distance 1 checks line 2 then line 4 — line 2 matches `"t"`. The test proves the order.)

- [ ] **Step 3: Write the failing tests**

`internal/domain/fixture_part2_test.go`:

```go
package domain

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

func readJSON(t *testing.T, name string, into any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "specs", "gg-notes-store", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// The fixture's reviews read through gg: the commit review and the PR
// review, each with its parsed document; the working review belongs to
// another checkout and is not listed here.
func TestFixtureReviewsReadThroughGG(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	svc.UseNotesDir(fixtureStoreDir(t))
	rs, err := svc.Reviews(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Review{}
	for _, r := range rs {
		byID[r.ID] = r
	}
	c, ok := byID["c0ffee01"]
	if !ok || c.Doc == nil || c.Scope != fixtureCommit+"^.."+fixtureCommit || c.Branch != "main" || c.Preview != "" {
		t.Fatalf("commit review = %+v (found %v)", c, ok)
	}
	if n, f := c.Doc.NoteCount(); n != 2 || f != 1 {
		t.Fatalf("commit review remarks = %d on %d files, want 2 on 1", n, f)
	}
	if c.Doc.Files[0].Notes[1].Side != "old" || c.Doc.Files[0].Notes[1].Range != [2]int{2, 2} {
		t.Fatalf("old-side remark = %+v", c.Doc.Files[0].Notes[1])
	}
	p, ok := byID["c0ffee02"]
	if !ok || p.Doc == nil || p.Preview != "main...refs/gg/pr/7" || p.Scope != fixturePRBase+".."+fixturePRHead || p.Commit != fixturePRHead {
		t.Fatalf("PR review = %+v (found %v)", p, ok)
	}
	if _, ok := byID["bb220002"]; ok {
		t.Fatal("another checkout's working review was listed")
	}
}

// Review focus 4: the reply stored in previews.toml threads under the PR
// review's remark 0 — never as a note of its own.
func TestFixtureReplyThreadsUnderItsRoot(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	svc.UseNotesDir(fixtureStoreDir(t))
	ctx := context.Background()
	r, err := svc.Review(ctx, "c0ffee02")
	if err != nil {
		t.Fatal(err)
	}
	// Review.Replies holds the review's stored replies (remarkThreadSet.attach);
	// a remark reply names its remark.
	if len(r.Replies) != 1 || r.Replies[0].ID != "c0ffee03" || r.Replies[0].Remark != "review:c0ffee02:0" ||
		r.Replies[0].RemarkFP != remarkFP("internal/x/gate.go", model.NoteSideNew, [2]int{1, 1}, "Name the parameter.") {
		t.Fatalf("replies = %+v, want c0ffee03 on remark review:c0ffee02:0", r.Replies)
	}
	// And NotesAt on the PR head's file lists no root named c0ffee03.
	rs, err := svc.NotesAt(ctx, model.FileAddress{State: model.StateCommitted, Commit: fixturePRHead, Path: "internal/x/gate.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range rs {
		if n.Note.ID == "c0ffee03" {
			t.Fatal("the remark reply surfaced as a root note")
		}
	}
}

// The committed line note, its reply and its resolution read through gg.
func TestFixtureLineNoteThreadReadThroughGG(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	svc.UseNotesDir(fixtureStoreDir(t))
	rs, err := svc.NotesAt(context.Background(), model.FileAddress{State: model.StateCommitted, Commit: fixtureCommit, Path: "internal/x/gate.go"})
	if err != nil {
		t.Fatal(err)
	}
	var root *ResolvedNote
	for i := range rs {
		if rs[i].Note.ID == "aa110001" {
			root = &rs[i]
		}
	}
	if root == nil {
		t.Fatalf("line note aa110001 not found in %+v", rs)
	}
	if len(root.Replies) != 1 || root.Replies[0].Note.ID != "aa110002" || root.Replies[0].Note.Link == "" {
		t.Fatalf("thread = %+v", root.Replies)
	}
	if root.Resolution == nil || root.Resolution.By != "me" {
		t.Fatalf("resolution = %+v, want resolved by me", root.Resolution)
	}
	if len(root.Note.Meta) != 1 || root.Note.Meta[0].Key != "kind" || len(root.Note.Added) != 1 || root.Note.Added[0].Value != "agreed" {
		t.Fatalf("meta/added = %+v / %+v", root.Note.Meta, root.Note.Added)
	}
}

// vectors.json: the two fingerprints and the keys, computed by gg's own
// functions — a reader ports them and must get the same strings.
func TestFixtureVectors(t *testing.T) {
	t.Parallel()
	var v struct {
		ContextHash []struct {
			Lines []string `json:"lines"`
			Hash  string   `json:"hash"`
		} `json:"context_hash"`
		RemarkFP []struct {
			Path    string `json:"path"`
			Side    string `json:"side"`
			Range   [2]int `json:"range"`
			Summary string `json:"summary"`
			FP      string `json:"fp"`
		} `json:"remark_fp"`
		WorktreeKey []struct {
			Top string `json:"top"`
			Key string `json:"key"`
		} `json:"worktree_key"`
	}
	readJSON(t, "vectors.json", &v)
	for _, c := range v.ContextHash {
		if got := model.NoteContextHash(c.Lines); got != c.Hash {
			t.Errorf("context_hash(%q) = %s, want %s", c.Lines, got, c.Hash)
		}
	}
	for _, c := range v.RemarkFP {
		if got := remarkFP(c.Path, model.NoteSide(c.Side), c.Range, c.Summary); got != c.FP {
			t.Errorf("remark_fp(%q,%s,%v,%q) = %s, want %s", c.Path, c.Side, c.Range, c.Summary, got, c.FP)
		}
	}
	for _, c := range v.WorktreeKey {
		if got := strings.TrimPrefix(string(notes.WorktreePart(c.Top)), "wt-"); got != c.Key {
			t.Errorf("worktree_key(%q) = %s, want %s", c.Top, got, c.Key)
		}
	}
}

// relocation-cases.json: gg's resolver on each case (spec part 2 §5.2);
// a reader runs the same file against its port.
func TestRelocationCases(t *testing.T) {
	t.Parallel()
	var cases []struct {
		Name     string   `json:"name"`
		Lines    []string `json:"lines"`
		Range    [2]int   `json:"range"`
		Anchored []string `json:"anchored"`
		Status   string   `json:"status"`
		At       [2]int   `json:"at"`
	}
	readJSON(t, "relocation-cases.json", &cases)
	for _, c := range cases {
		n := model.Note{Range: c.Range, Address: model.FileAddress{State: model.StateCommitted, Commit: fixtureCommit, Path: "f"}}
		if c.Range == [2]int{0, 0} {
			n.Address.Path = "" // entry-level
		}
		if len(c.Anchored) > 0 {
			n.ContextHash = model.NoteContextHash(c.Anchored)
		}
		st, at := resolveOne(n, c.Lines)
		if string(st) != c.Status || at != c.At {
			t.Errorf("%s: got %s at %v, want %s at %v", c.Name, st, at, c.Status, c.At)
		}
	}
}
```

Before running: open `internal/domain/review_threads.go` and confirm how a `Review` exposes remark threads (`RemarkThread`, attached by `th.attach`); use the real field name in `TestFixtureReplyThreadsUnderItsRoot` (the plan's `r.Threads` is a placeholder for whatever `attach` fills — a plan defect to resolve on reading, ledger it).

- [ ] **Step 4: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && go test ./internal/domain -run 'TestFixtureReviews|TestFixtureReply|TestFixtureLineNote|TestFixtureVectors|TestRelocationCases' 2>&1 | tail -12`
Expected: `TestFixtureVectors` FAILS on every `"FILL"` (prints the real values); `TestRelocationCases` passes or fails per case — read each failure: a failing case means EITHER the case's expectation is wrong (fix the JSON to what gg does and re-check the spec text) OR the spec's §5.2 wording is wrong (fix the spec). Never change `resolveOne`.

- [ ] **Step 5: Fill the vectors, make everything green**

Copy the printed values into `vectors.json` (`FILL` → the hex). Run the Step 4 command again. Expected: PASS ×5.

- [ ] **Step 6: Commit**

```bash
cd /work/gigagit/.claude/worktrees/gg-notes-store-format
gg add docs/specs/gg-notes-store/vectors.json docs/specs/gg-notes-store/relocation-cases.json internal/domain/fixture_part2_test.go internal/domain/overview_fixture_test.go
git commit -F <msgfile>   # "spec part 2: fixture read-through tests, fingerprint vectors, relocation cases"
```

---

### Task 4: Docs, race gate, the gg-notes copy

**Files:**
- Modify: `docs/specs/gg-notes-store/README.md`
- Modify: `CHANGELOG.md` (the existing part-1 section)
- Modify: `docs/CLAUDE-details.md` (the overview CLI line's neighbourhood: one sentence)
- Outside gg (not committed by us): `/work/gg-notes/docs/gg-notes-store/` + `/work/gg-notes/AGENTS.md`

- [ ] **Step 1: specs README**

Append to `docs/specs/gg-notes-store/README.md`:

```markdown

## Part 2 (reviews, line notes, live notes, meta)

Spec: `../../superpowers/specs/2026-10-15-gg-notes-store-format-part2-design.md`.

- `review-document.schema.json` / `review-document.lenient.schema.json` —
  the stored review document (strict = gg's drift guard, lenient = a
  reader's).
- `fixture/` now holds every record kind (see `fixture/README.md`), written
  by gg's own store (`go test ./internal/domain -run TestFixtureIsWhatGGWrites`).
- `vectors.json` — `context_hash`, `remark_fp`, repository and worktree key
  test vectors.
- `relocation-cases.json` — the relocation rule (part 2 §5.2) as cases a
  reader runs against its own implementation; gg's resolver passes them
  (`TestRelocationCases`).
```

- [ ] **Step 2: CHANGELOG**

In the `## gg notes store format: a spec other tools can read overviews with` section, add under `### Added`:

```markdown
- Part 2 of the spec
  (`docs/superpowers/specs/2026-10-15-gg-notes-store-format-part2-design.md`):
  review records and their document (both diff sides, scope, PR fields,
  remark identity), committed line notes and replies with thread
  resolutions, live notes in per-worktree parts with gg's relocation rule
  spelled out, the meta/display declaration, and what is not in the store.
  Conformance material: review-document schemas, a fixture of every record
  kind written by gg's own store (`-update-fixture`), fingerprint vectors
  and relocation cases as JSON a reader can run.
```

- [ ] **Step 3: CLAUDE-details**

After the sentence added in part 1 (`… the oracle of the store-format spec (docs/specs/gg-notes-store/)`), add: `Part 2 (reviews/notes/live notes/meta) ships schemas, a gg-written fixture (\`TestFixtureIsWhatGGWrites -update-fixture\`), vectors.json and relocation-cases.json there.`

- [ ] **Step 4: Race gate**

Run: `cd /work/gigagit/.claude/worktrees/gg-notes-store-format && ./test.sh race > <scratch>/race3.log 2>&1; grep -n 'all green\|^FAIL\|--- FAIL' <scratch>/race3.log | tail -5`
Expected: `all green`, no FAIL lines.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/gg-notes-store-format
gg add docs/specs/gg-notes-store/README.md CHANGELOG.md docs/CLAUDE-details.md
git commit -F <msgfile>   # "changelog: gg notes store format part 2"
```

- [ ] **Step 6: Hand the plugin repo its copy (no commit there)**

```bash
W=/work/gigagit/.claude/worktrees/gg-notes-store-format; G=/work/gg-notes/docs/gg-notes-store
cp $W/docs/superpowers/specs/2026-10-15-gg-notes-store-format-part2-design.md $G/SPEC-part2.md
cp $W/docs/superpowers/specs/2026-10-15-gg-notes-store-format-design.md $G/SPEC.md   # part 1 changed (§1 pointer, §3.3 SHOULD)
cp $W/docs/specs/gg-notes-store/review-document*.json $W/docs/specs/gg-notes-store/vectors.json $W/docs/specs/gg-notes-store/relocation-cases.json $G/
rm -rf $G/fixture && cp -r $W/docs/specs/gg-notes-store/fixture $G/fixture
cp $W/docs/specs/gg-notes-store/fixture/*.toml /work/gg-notes/examples/ && mkdir -p /work/gg-notes/examples/worktrees && cp $W/docs/specs/gg-notes-store/fixture/worktrees/*.toml /work/gg-notes/examples/worktrees/
```

Prepend the same provenance header to `SPEC-part2.md` as `SPEC.md` has (branch + commit). In `/work/gg-notes/AGENTS.md` add a section `## 6. Part 2 — reviews, line notes, live notes` pointing at `SPEC-part2.md`, the new fixture records (by id, from `fixture/README.md`), `vectors.json` and `relocation-cases.json` as the unit-test inputs, and the v2 scope in the spec's §8 table; in `examples/README.md` add a short list of the new records. Report the paths; the user commits that repo.
