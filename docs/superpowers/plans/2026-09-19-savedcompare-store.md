# Saved-comparison store (plan 3a) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: this repository's `CLAUDE.md` says **NEVER USE SUB AGENTS** — execute this plan task-by-task in the one session that reads it. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land `internal/savedcompare` as the one store behind both merge previews and saved comparisons, converting the user's existing `previews.toml` losslessly, and generalize the migration framework so a migration names its own action instead of being hardcoded to ref deletion.

**Architecture:** A new DAG-leaf store owned by `domain`, modelled on `internal/preview`, holding `{ID, Left model.Link, Right *model.Link, Label, Created}` where `Right == nil` means a saved SET (a merge preview). `domain` keeps `PreviewAdd/List/Get/Rename/Remove` as a façade over set-shaped entries, so no frontend changes in this plan. The previews→savedcompare conversion runs once per machine as a lossless, consent-free migration through a generalized `engine.ApplyMigration` that now delegates its body to a `MigrationAction`.

**Tech Stack:** Go 1.26, `github.com/pelletier/go-toml/v2`, `internal/filelock`, `internal/preflight`, `internal/engine`, `internal/domain`, `internal/cli`.

**Spec:** `docs/superpowers/specs/2026-09-16-unified-links-design.md` — §4.3 (new packages), §4.5 + §4.5.1 (the conversion and the generalized framework), §5.3 (CLI), §8 (phasing row **3a**).

## Global Constraints

- **The one rule (§3.1):** a **pair is BOUNDED** (a finite enumerated path set); a **point is UNBOUNDED** (every file at some state). Compare is CLOSED — you can only scale DOWN.
- **Direction (§5.1):** `PreviewAdd(source, target)` renders `merge-base(target, source)..source`, spelled `@target...source`. The selected link is the **source**; the base is the **target**.
- **IDs derive from the link texts, one derivation for both shapes** — never branch the hash on shape. Converted entries carry their **old id verbatim**.
- **`Add` dedups on `(Left, Right)`, never on the id.**
- **Reuse the kind string `"previews"`** for `stateBaseDir`. `internal/domain/statebasedir_callers_test.go` pins the exact SET of kinds and must stay green **unchanged** — that is what proves the kind was reused rather than added.
- **A store is a DAG leaf owned by `domain`; frontends never import it** (the `notes`/`shelf`/`preview`/`bookmark` convention, enforced by `internal/archtest`).
- **Engine/CLI prose stays English.** No `i18n.T` in this plan — no TUI strings are added.
- **`internal/tui` and `internal/cli` never import `internal/git`.**
- **Tests use a real `git`** in a `t.TempDir()` (`newRepo`/`newTestRepo`) or `gitexec.FakeRunner` for argv assertions. New tests call `t.Parallel()` **except** tests that mutate a package global (`RepoStatePath`, `PreviewStatePath`, `PreviewsDisabled`) — those stay serial.
- **A gate pins the CONTRACT, never the spelling.** Assert membership and behaviour, never a verbatim source line.
- **"Watch it fail" means with the GUARD removed, not the feature.** Every new test must be run against a build where the thing it guards is deliberately broken, and must actually fail there.
- **Two arms that look alike and must behave differently** is this feature's signature defect. Tests for two such arms must DISAGREE on one shared fixture.
- Work in the worktree at `.claude/worktrees/savedcompare` on branch `feat/savedcompare`. Never commit in the main checkout. Run `./test.sh race` before proposing a merge; **ask before merging**.

---

## File Structure

| file | responsibility |
|---|---|
| `internal/model/link.go` (modify) | `Link` gains `MarshalText`/`UnmarshalText` over the existing `String`/`ParseLink` pair, so a link persists as readable text |
| `internal/savedcompare/store.go` (create) | `Entry`, `ID`, `ErrNotFound`, `ErrExists`, the `Store` interface — the package's contract, no I/O |
| `internal/savedcompare/file_store.go` (create) | `FileStore`: `savedcompare.toml` + `internal/filelock`, mutex → file lock → fresh read → apply → atomic rewrite |
| `internal/savedcompare/convert.go` (create) | The legacy `previews.toml` shape, FROZEN locally, plus `ConvertLegacy(dir)`: read → convert → merge → write → remove the old file |
| `internal/engine/apply_migration.go` (modify) | `MigrationAction` interface; `ApplyMigration.Refs` becomes `ApplyMigration.Action` |
| `internal/engine/migration_actions.go` (create) | `DiscardRefs` (today's body, extracted verbatim) and `ConvertPreviews` |
| `internal/preflight/preflight.go` (modify) | `Probes.Legacy`, the `LegacyStore` requirement, `Migration.Action` and `Migration.Lossless` |
| `internal/domain/features.go` (modify) | `FeaturePreviews` + `StorePreviews`, declaring the lossless conversion |
| `internal/domain/preflight.go` (modify) | probe the legacy file; build the action; `RunAutoMigrations`; `PendingMigrations` reports only consent-requiring migrations |
| `internal/domain/savedcomparestore.go` (create, replaces `previewstore.go`) | the lazy per-repo store resolver, keeping the `PreviewStatePath` / `PreviewsDisabled` / `UsePreviewsDir` seam names |
| `internal/domain/preview.go` (modify) | `PreviewAdd/List/Get/Rename/Remove` as a façade over set-shaped entries |
| `internal/domain/savedcompare.go` (create) | `SavedCompareAdd/List/Get/Remove` for pair-shaped entries |
| `internal/cli/compare.go` (modify) | `--save`, `--saved`, `--list`, and `compareTokenLink` |
| `internal/preview/` (delete) | absorbed by `savedcompare` |
| `internal/domain/previewstore.go` (delete) | replaced by `savedcomparestore.go` |

---

### Task 1: `model.Link` persists as text

**Files:**
- Modify: `internal/model/link.go`
- Test: `internal/model/link_text_test.go` (create)

**Interfaces:**
- Consumes: the existing `func ParseLink(s string) (Link, error)` and `func (l Link) String() string`.
- Produces: `func (l Link) MarshalText() ([]byte, error)` and `func (l *Link) UnmarshalText(b []byte) error`, so `savedcompare.Entry` can hold `model.Link` fields directly and go-toml v2 serialises them as link text.

- [ ] **Step 1: Write the failing test**

```go
package model

import "testing"

// A Link round-trips through TOML text as the link TEXT, not an exploded
// struct. The fixture is a THREE-DOT preview link on purpose: it is the shape
// the previews conversion writes, and it is the one shape whose halves are
// asymmetric, so a marshaller that dropped or swapped a half would show here.
func TestLinkMarshalTextRoundTrip(t *testing.T) {
	t.Parallel()
	const s = "gg://gigagit@main...feat/login"
	l, err := ParseLink(s)
	if err != nil {
		t.Fatalf("ParseLink(%q): %v", s, err)
	}
	b, err := l.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	if string(b) != s {
		t.Fatalf("MarshalText = %q, want %q", b, s)
	}
	var back Link
	if err := back.UnmarshalText(b); err != nil {
		t.Fatalf("UnmarshalText(%q): %v", b, err)
	}
	if back.String() != s {
		t.Fatalf("round trip = %q, want %q", back.String(), s)
	}
	if back.Target.Preview == nil {
		t.Fatal("round trip lost the preview target")
	}
	if got := [2]string{back.Target.Preview.Target, back.Target.Preview.Source}; got != [2]string{"main", "feat/login"} {
		t.Fatalf("round trip halves = %v, want [main feat/login]", got)
	}
}

// UnmarshalText refuses a malformed link rather than leaving a zero Link
// behind: a corrupt stored row must fail loudly, never read as "the working
// tree of some repo".
func TestLinkUnmarshalTextRefusesGarbage(t *testing.T) {
	t.Parallel()
	var l Link
	if err := l.UnmarshalText([]byte("not-a-link")); err == nil {
		t.Fatal("UnmarshalText accepted a non-link")
	}
}
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `go test ./internal/model/ -run 'TestLinkMarshalText|TestLinkUnmarshalText' -v`
Expected: FAIL to COMPILE — `l.MarshalText undefined (type Link has no field or method MarshalText)`.

- [ ] **Step 3: Implement**

Append to `internal/model/link.go`:

```go
// MarshalText renders the link as its own text, so a Link stored in TOML or
// JSON is the string a user could paste. Pairs with UnmarshalText; together
// they make any file holding a Link a standing String(Parse(s)) == s witness.
func (l Link) MarshalText() ([]byte, error) { return []byte(l.String()), nil }

// UnmarshalText parses the link text. A malformed value is an ERROR, never a
// zero Link: a corrupt stored row must fail loudly rather than read as the
// working tree of an unnamed repository.
func (l *Link) UnmarshalText(b []byte) error {
	parsed, err := ParseLink(string(b))
	if err != nil {
		return err
	}
	*l = parsed
	return nil
}
```

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `go test ./internal/model/ -run 'TestLink' -v`
Expected: PASS, including the pre-existing `String(Parse(s))==s` round-trip tests.

- [ ] **Step 5: Watch it fail with the GUARD removed**

Temporarily change `MarshalText` to `return []byte(l.Repo.Name), nil` and re-run Step 4. `TestLinkMarshalTextRoundTrip` MUST fail. Restore.

- [ ] **Step 6: Commit**

```bash
git add internal/model/link.go internal/model/link_text_test.go
git commit -m "feat(model): a Link marshals as its own text"
```

---

### Task 2: the `savedcompare` store

**Files:**
- Create: `internal/savedcompare/store.go`, `internal/savedcompare/file_store.go`
- Test: `internal/savedcompare/file_store_test.go` (create)
- Modify: `internal/archtest/` — add `savedcompare` to the DAG-leaf list, exactly as `linkhist` and `filelock` are listed

**Interfaces:**
- Consumes: `model.Link` with `MarshalText`/`UnmarshalText` (Task 1); `filelock.Acquire(path string) (func(), error)`.
- Produces:
  ```go
  type Entry struct {
      ID      string
      Left    model.Link
      Right   *model.Link // nil ⇒ a saved SET (a merge preview)
      Label   string
      Created time.Time
  }
  func ID(left, right string) string
  func (e Entry) IsSet() bool
  var ErrNotFound, ErrExists error
  type Store interface {
      Add(e Entry) (Entry, error)
      Get(id string) (Entry, error)
      List() ([]Entry, error)
      Rename(id, label string) error
      Remove(id string) error
  }
  func NewFileStore(root string) *FileStore
  ```

- [ ] **Step 1: Write the failing test**

```go
package savedcompare

import (
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func mustLink(t *testing.T, s string) model.Link {
	t.Helper()
	l, err := model.ParseLink(s)
	if err != nil {
		t.Fatalf("ParseLink(%q): %v", s, err)
	}
	return l
}

// ID is ONE derivation for both shapes. A set-shaped entry hashes its empty
// right half rather than taking a different code path: a hash that branches
// on shape is the two-arms defect this feature keeps producing.
func TestIDIsOneDerivationForBothShapes(t *testing.T) {
	t.Parallel()
	set := ID("gg://r@main...feat/x", "")
	pair := ID("gg://r@main...feat/x", "gg://r@abc1234")
	if set == "" || pair == "" {
		t.Fatal("ID returned empty")
	}
	if set == pair {
		t.Fatal("a set and a pair sharing a left half collided")
	}
	// Direction-sensitive, like preview.ID before it.
	if ID("gg://r@a", "gg://r@b") == ID("gg://r@b", "gg://r@a") {
		t.Fatal("ID is not direction-sensitive")
	}
}

// Add fills the id, defaults the label, dedups on the PAIR (not the id), and
// returns the existing record with ErrExists so a caller can focus it.
func TestAddDedupsOnThePairAndReturnsTheExisting(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	left, right := mustLink(t, "gg://r@main...feat/x"), mustLink(t, "gg://r@abc1234")

	first, err := fs.Add(Entry{Left: left, Right: &right, Label: "mine"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if first.ID == "" || first.Created.IsZero() {
		t.Fatalf("Add did not fill ID/Created: %+v", first)
	}

	again, err := fs.Add(Entry{Left: left, Right: &right, Label: "different label"})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("second Add err = %v, want ErrExists", err)
	}
	if again.ID != first.ID || again.Label != "mine" {
		t.Fatalf("ErrExists returned %+v, want the stored record %+v", again, first)
	}

	list, err := fs.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v (%d entries), want 1", err, len(list))
	}
}

// A set-shaped entry (Right nil) and a pair-shaped entry with the SAME left
// half are two different rows, and each reads back in its own shape. This is
// the shared fixture that makes the two arms disagree.
func TestSetAndPairWithTheSameLeftAreDistinctRows(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	left := mustLink(t, "gg://r@main...feat/x")
	right := mustLink(t, "gg://r@abc1234")

	set, err := fs.Add(Entry{Left: left, Label: "the preview"})
	if err != nil {
		t.Fatalf("Add set: %v", err)
	}
	pair, err := fs.Add(Entry{Left: left, Right: &right, Label: "the comparison"})
	if err != nil {
		t.Fatalf("Add pair: %v", err)
	}
	if set.ID == pair.ID {
		t.Fatal("the set and the pair share an id")
	}

	gotSet, err := fs.Get(set.ID)
	if err != nil {
		t.Fatalf("Get set: %v", err)
	}
	if !gotSet.IsSet() || gotSet.Right != nil {
		t.Fatalf("set read back as a pair: %+v", gotSet)
	}
	gotPair, err := fs.Get(pair.ID)
	if err != nil {
		t.Fatalf("Get pair: %v", err)
	}
	if gotPair.IsSet() || gotPair.Right == nil {
		t.Fatalf("pair read back as a set: %+v", gotPair)
	}
	if gotPair.Right.String() != right.String() {
		t.Fatalf("right half = %q, want %q", gotPair.Right.String(), right.String())
	}
}

// The file on disk holds LINK TEXT, so it stays readable and every stored row
// is a String(Parse(s)) == s witness.
func TestTheFileHoldsLinkText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := NewFileStore(dir)
	if _, err := fs.Add(Entry{Left: mustLink(t, "gg://r@main...feat/x"), Label: "x"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "savedcompare.toml"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "gg://r@main...feat/x") {
		t.Fatalf("stored file does not hold the link text:\n%s", data)
	}
}

// A corrupt file is an ERROR, never "empty" — swallowing it would let the
// next write destroy the store (the rule internal/preview already follows).
func TestACorruptFileIsAnError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "savedcompare.toml"), []byte("this is not toml ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(dir).List(); err == nil {
		t.Fatal("List read a corrupt file as empty")
	}
}

func TestRenameAndRemove(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	e, err := fs.Add(Entry{Left: mustLink(t, "gg://r@main...feat/x")})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if e.Label != "main...feat/x" {
		t.Fatalf("default label = %q, want %q", e.Label, "main...feat/x")
	}
	if err := fs.Rename(e.ID, "renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	got, err := fs.Get(e.ID)
	if err != nil || got.Label != "renamed" {
		t.Fatalf("after Rename: %+v, %v", got, err)
	}
	if err := fs.Remove(e.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := fs.Get(e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Remove = %v, want ErrNotFound", err)
	}
	if err := fs.Remove(e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Remove = %v, want ErrNotFound", err)
	}
}
```

Add the imports this file needs: `os`, `path/filepath`, `strings`.

- [ ] **Step 2: Run it to make sure it fails**

Run: `go test ./internal/savedcompare/ -v`
Expected: FAIL — the package does not exist.

- [ ] **Step 3: Write `internal/savedcompare/store.go`**

```go
// Package savedcompare is gigagit's machine-local registry of saved
// comparisons. One entry is either a PAIR (two links: "these two things,
// compared") or a SET (one link, Right nil: everything that link changes —
// which is what a saved merge preview is). Records only, no blobs. Owned by
// internal/domain — frontends never import it (like notes/bookmark/shelf).
package savedcompare

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

// ErrNotFound is returned by Get/Rename/Remove for an unknown id.
var ErrNotFound = errors.New("savedcompare: not found")

// ErrExists is returned by Add when the (Left, Right) pair is already stored;
// the existing record is returned alongside it.
var ErrExists = errors.New("savedcompare: already exists")

// Entry is one saved comparison.
//
// Right == nil means a saved SET, not a pair: a merge preview is one bounded
// set ("everything feat/x would bring into main"), and the store holds that
// shape explicitly rather than faking a second side (spec §4.5).
//
// The links are stored UNRESOLVED, exactly as produced. That is what makes a
// saved preview follow its branches as they move: `gg://r@main...feat/x`
// names two branches, and every open re-resolves their tips.
type Entry struct {
	ID      string      `toml:"id"`
	Left    model.Link  `toml:"left"`
	Right   *model.Link `toml:"right,omitempty"`
	Label   string      `toml:"label"`
	Created time.Time   `toml:"created"`
}

// IsSet reports whether this entry is a bounded SET rather than a pair.
func (e Entry) IsSet() bool { return e.Right == nil }

// ID derives a record's id from its two link TEXTS: sha256("left\x00right"),
// first 8 hex chars. Direction-sensitive, like preview.ID before it.
//
// ONE derivation serves both shapes — a set hashes an EMPTY right half rather
// than taking a second code path. A hash that branched on shape would be the
// signature two-arms defect of this feature: two arms that look alike and
// must behave differently, with the rule applied to one and skipped for the
// other.
//
// Entries converted from the legacy previews store keep their OLD id instead
// (see ConvertLegacy): ids are user-visible handles typed into `gg preview
// <id>`, so they are carried across rather than recomputed.
func ID(left, right string) string {
	sum := sha256.Sum256([]byte(left + "\x00" + right))
	return hex.EncodeToString(sum[:4])
}

// DefaultLabel is the label an entry gets when the caller gives none.
func (e Entry) DefaultLabel() string {
	left := e.Left.Target.String()
	if e.Right == nil {
		return left
	}
	return left + " ↔ " + e.Right.Target.String()
}

// Store persists saved comparisons. Every mutation re-reads under a
// cross-process lock, applies, and rewrites atomically.
type Store interface {
	Add(e Entry) (Entry, error) // fills ID (+ Label when empty); ErrExists with the stored record
	Get(id string) (Entry, error)
	List() ([]Entry, error) // insertion order
	Rename(id, label string) error
	Remove(id string) error
}

var _ Store = (*FileStore)(nil)
```

If `model.LinkTarget` has no `String()` method, use the link's own text minus the scheme and repo instead: replace `e.Left.Target.String()` with `strings.TrimPrefix(e.Left.String(), model.LinkScheme+e.Left.Repo.Name+"@")` and add a `strings` import — the label is cosmetic, and the test above pins the expected value `"main...feat/x"`, so make whichever spelling produces it.

- [ ] **Step 4: Write `internal/savedcompare/file_store.go`**

Copy `internal/preview/file_store.go` structurally — it is the precedent this package follows — changing the element type to `Entry`, the file name to `savedcompare.toml`, the temp pattern to `savedcompare-*.toml`, and the `index` struct field to `Entries []Entry \`toml:"entries"\``. Keep verbatim: the `mu sync.Mutex` + `filelock.Acquire` pairing, the `read` that treats a MISSING file as empty and anything else as an error, the temp-file + rename `write`, and the `mutate` shape (mutex → file lock → fresh read → apply → atomic rewrite).

`Add` differs from preview's in exactly two ways — dedup on the pair, and the shared id derivation:

```go
func (fs *FileStore) Add(e Entry) (Entry, error) {
	right := ""
	if e.Right != nil {
		right = e.Right.String()
	}
	if e.ID == "" {
		e.ID = ID(e.Left.String(), right)
	}
	if e.Label == "" {
		e.Label = e.DefaultLabel()
	}
	if e.Created.IsZero() {
		e.Created = time.Now().UTC()
	}
	var existing *Entry
	err := fs.mutate(func(es []Entry) ([]Entry, error) {
		for _, q := range es {
			// Dedup on the PAIR, never on the id: a converted entry carries a
			// legacy id, so two rows for the same comparison could hold
			// different ids and an id-keyed check would miss the duplicate.
			qr := ""
			if q.Right != nil {
				qr = q.Right.String()
			}
			if q.Left.String() == e.Left.String() && qr == right {
				c := q
				existing = &c
				return nil, ErrExists
			}
		}
		return append(es, e), nil
	})
	if errors.Is(err, ErrExists) {
		return *existing, err
	}
	if err != nil {
		return Entry{}, err
	}
	return e, nil
}
```

`Get`, `List`, `Rename` and `Remove` are preview's, with `Entry` for `model.MergePreview`.

- [ ] **Step 5: Run the tests and make sure they pass**

Run: `go test ./internal/savedcompare/ -v`
Expected: PASS, every test.

- [ ] **Step 6: Watch each guard fail**

One at a time, make the break, run Step 5, confirm the NAMED test fails, then restore:

| break | test that must fail |
|---|---|
| `ID` returns `hex...[:4]` of `left` alone (drops the right half) | `TestIDIsOneDerivationForBothShapes` |
| `Add` dedups on `q.ID == e.ID` | `TestAddDedupsOnThePairAndReturnsTheExisting` |
| `Entry.Right` loses its `omitempty` and `IsSet` returns `false` always | `TestSetAndPairWithTheSameLeftAreDistinctRows` |
| `read` returns `nil, nil` on a TOML parse error | `TestACorruptFileIsAnError` |

If any break leaves every test green, the test cannot see its own subject — fix the test before continuing.

- [ ] **Step 7: Add the archtest leaf gate**

Find where `internal/archtest` lists DAG leaves (`linkhist` and `filelock` are already there) and add `savedcompare` the same way: it may import `model`, `filelock` and the TOML library, and nothing else from `internal/`.

Run: `go test ./internal/archtest/ -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/savedcompare internal/archtest
git commit -m "feat(savedcompare): the store behind saved comparisons and merge previews"
```

---

### Task 3: the lossless conversion

**Files:**
- Create: `internal/savedcompare/convert.go`
- Test: `internal/savedcompare/convert_test.go` (create)

**Interfaces:**
- Consumes: `Entry`, `NewFileStore`, `ErrExists` (Task 2).
- Produces: `func ConvertLegacy(dir string, repo model.LinkRepo) (n int, err error)` — converts `dir/previews.toml` into `dir/savedcompare.toml` and removes the legacy file; returns how many entries were converted. A missing legacy file is `(0, nil)`.

**Why the legacy shape is frozen here:** a migration must keep reading the bytes that are actually on disk, whatever the live code later does to `model.MergePreview`. `convert.go` therefore declares its own copy of the old struct and does NOT import `internal/preview` — which is deleted in Task 7.

- [ ] **Step 1: Write the failing test**

```go
package savedcompare

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

const legacyTOML = `[[previews]]
id = "abcd1234"
source = "feat/login"
target = "main"
label = "login work"
created = 2026-09-01T10:00:00Z
`

func repoFixture() model.LinkRepo { return model.LinkRepo{Name: "gigagit"} }

// THE DIRECTION GATE. PreviewAdd(source, target) renders `@target...source`,
// so a preview of source=feat/login into target=main converts to
// `gg://gigagit@main...feat/login` — target FIRST. The fixture uses two
// different names for exactly this reason: a converter that swapped the
// halves would be invisible against a symmetric fixture, and the swap is the
// defect this feature produced over and over.
func TestConvertLegacyPutsTargetFirst(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "previews.toml"), []byte(legacyTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := ConvertLegacy(dir, repoFixture())
	if err != nil {
		t.Fatalf("ConvertLegacy: %v", err)
	}
	if n != 1 {
		t.Fatalf("converted %d entries, want 1", n)
	}
	list, err := NewFileStore(dir).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("stored %d entries, want 1", len(list))
	}
	got := list[0]
	if want := "gg://gigagit@main...feat/login"; got.Left.String() != want {
		t.Fatalf("Left = %q, want %q (target first)", got.Left.String(), want)
	}
	if !got.IsSet() {
		t.Fatal("a converted preview must be a SET, not a pair")
	}
	if got.ID != "abcd1234" {
		t.Fatalf("ID = %q, want the legacy id %q carried over verbatim", got.ID, "abcd1234")
	}
	if got.Label != "login work" {
		t.Fatalf("Label = %q, want %q", got.Label, "login work")
	}
	if got.Created.IsZero() {
		t.Fatal("Created was not carried over")
	}
}

// The legacy file is REMOVED, so the conversion does not run again and an
// old build's file cannot shadow the new store.
func TestConvertLegacyRemovesTheOldFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	legacy := filepath.Join(dir, "previews.toml")
	if err := os.WriteFile(legacy, []byte(legacyTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ConvertLegacy(dir, repoFixture()); err != nil {
		t.Fatalf("ConvertLegacy: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("previews.toml still present (stat err = %v)", err)
	}
}

// Nothing to convert is not an error, and writes no file: this runs at every
// startup and must be free when there is no legacy data.
func TestConvertLegacyWithNoLegacyFileIsANoOp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	n, err := ConvertLegacy(dir, repoFixture())
	if err != nil || n != 0 {
		t.Fatalf("ConvertLegacy = (%d, %v), want (0, nil)", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "savedcompare.toml")); !os.IsNotExist(err) {
		t.Fatalf("a no-op conversion wrote savedcompare.toml (stat err = %v)", err)
	}
}

// THE RESURRECTION CASE. An older gg still on PATH recreates previews.toml
// after the conversion ran. Converting again must NOT duplicate the row: the
// dedup is on the (Left, Right) pair, so the second pass absorbs it.
func TestConvertLegacyIsIdempotentAcrossAResurrectedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func() {
		if err := os.WriteFile(filepath.Join(dir, "previews.toml"), []byte(legacyTOML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write()
	if _, err := ConvertLegacy(dir, repoFixture()); err != nil {
		t.Fatalf("first ConvertLegacy: %v", err)
	}
	write() // an older build wrote it again
	if _, err := ConvertLegacy(dir, repoFixture()); err != nil {
		t.Fatalf("second ConvertLegacy: %v", err)
	}
	list, err := NewFileStore(dir).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("stored %d entries after a resurrected legacy file, want 1", len(list))
	}
}

// A corrupt legacy file fails loudly and leaves it in place, so the user's
// data is never removed on the strength of a parse this build got wrong.
func TestConvertLegacyRefusesACorruptFileAndKeepsIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	legacy := filepath.Join(dir, "previews.toml")
	if err := os.WriteFile(legacy, []byte("not toml ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ConvertLegacy(dir, repoFixture()); err == nil {
		t.Fatal("ConvertLegacy accepted a corrupt legacy file")
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("a corrupt legacy file was removed anyway: %v", err)
	}
}
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `go test ./internal/savedcompare/ -run TestConvertLegacy -v`
Expected: FAIL to COMPILE — `undefined: ConvertLegacy`.

- [ ] **Step 3: Implement `internal/savedcompare/convert.go`**

```go
package savedcompare

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/model"
)

// LegacyFile is the name of the merge-preview store savedcompare absorbs.
const LegacyFile = "previews.toml"

// legacyPreview is the on-disk shape of one internal/preview record, FROZEN.
// A migration reads the bytes that are actually on a user's disk, so it keeps
// its own copy rather than importing the live type: internal/preview is
// deleted by this plan, and a later change to model.MergePreview must not be
// able to alter what this reads.
type legacyPreview struct {
	ID      string    `toml:"id"`
	Source  string    `toml:"source"`
	Target  string    `toml:"target"`
	Label   string    `toml:"label"`
	Created time.Time `toml:"created"`
}

type legacyIndex struct {
	Previews []legacyPreview `toml:"previews"`
}

// linkText renders the set link for one legacy record.
//
// TARGET FIRST. PreviewAdd(source, target) renders
// merge-base(target, source)..source, spelled `@target...source` — the same
// order `git diff target...source` reads. Swapping these two halves compares
// the wrong direction and is invisible in any fixture whose two names are
// interchangeable (spec §5.1).
func (p legacyPreview) linkText(repo model.LinkRepo) string {
	return model.Link{Repo: repo, Target: model.LinkTarget{
		State:   model.StateCommitted,
		Preview: &model.LinkPreview{Source: p.Source, Target: p.Target},
	}}.String()
}

// ConvertLegacy folds dir/previews.toml into dir/savedcompare.toml and
// removes the legacy file, returning how many records it converted.
//
// Lossless by construction: the id is carried across VERBATIM (it is a
// user-visible handle typed into `gg preview <id>`), as are the label and the
// creation time. Preview notes need no migration at all — they key on the
// branch NAMES and are ordinary committed notes at the source tip.
//
// Idempotent: a record already present is skipped (Add dedups on the pair),
// so an older gg that recreates previews.toml is absorbed rather than
// duplicated. A corrupt legacy file is an error and the file is LEFT IN
// PLACE — data is never removed on the strength of a parse this build got
// wrong.
func ConvertLegacy(dir string, repo model.LinkRepo) (int, error) {
	path := filepath.Join(dir, LegacyFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var idx legacyIndex
	if err := toml.Unmarshal(data, &idx); err != nil {
		return 0, fmt.Errorf("savedcompare: %s is corrupt: %w", path, err)
	}
	st := NewFileStore(dir)
	n := 0
	for _, p := range idx.Previews {
		if p.Source == "" || p.Target == "" {
			continue // a half-written legacy row names no pair; drop it
		}
		l, err := model.ParseLink(p.linkText(repo))
		if err != nil {
			return n, fmt.Errorf("savedcompare: converting preview %q: %w", p.ID, err)
		}
		e := Entry{ID: p.ID, Left: l, Label: p.Label, Created: p.Created}
		if _, err := st.Add(e); err != nil && !errors.Is(err, ErrExists) {
			return n, err
		} else if err == nil {
			n++
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return n, err
	}
	return n, nil
}
```

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `go test ./internal/savedcompare/ -v`
Expected: PASS, every test.

- [ ] **Step 5: Watch each guard fail**

| break | test that must fail |
|---|---|
| `linkText` swaps to `{Source: p.Target, Target: p.Source}` | `TestConvertLegacyPutsTargetFirst` |
| `e := Entry{Left: l, ...}` drops `ID: p.ID` (so `Add` derives a fresh one) | `TestConvertLegacyPutsTargetFirst` |
| the `os.Remove(path)` call is deleted | `TestConvertLegacyRemovesTheOldFile` |
| the `toml.Unmarshal` error is swallowed (`idx = legacyIndex{}`) | `TestConvertLegacyRefusesACorruptFileAndKeepsIt` |

The swap break must fail on the LINK TEXT assertion specifically. If it only fails on the id, the direction gate is not actually watching direction.

- [ ] **Step 6: Commit**

```bash
git add internal/savedcompare/convert.go internal/savedcompare/convert_test.go
git commit -m "feat(savedcompare): convert the legacy previews store losslessly"
```

---

### Task 4: a migration names its own action

**Files:**
- Modify: `internal/engine/apply_migration.go`
- Create: `internal/engine/migration_actions.go`
- Modify: `internal/domain/preflight.go:292` (`RunMigration`)
- Test: `internal/engine/apply_migration_test.go` (modify — the existing tests move to the new shape), `internal/engine/migration_actions_test.go` (create)

**Interfaces:**
- Consumes: `engine.OpDeps`, `deps.Repo.DeleteRef`, `deps.Repo.StampStoreFormat`.
- Produces:
  ```go
  type MigrationAction interface {
      Describe() string                                       // English, for the summary
      Apply(ctx context.Context, deps OpDeps) (n int, err error)
  }
  type DiscardRefs struct{ Refs []string }
  type ApplyMigration struct {
      Feature string
      Store   string
      To      int
      Action  MigrationAction
  }
  ```

- [ ] **Step 1: Write the failing test**

```go
package engine

import (
	"context"
	"errors"
	"testing"
)

// countingAction stands in for any future action: ApplyMigration must run
// WHATEVER it was handed, not a body of its own.
type countingAction struct {
	n      int
	err    error
	called bool
}

func (a *countingAction) Describe() string { return "counted" }
func (a *countingAction) Apply(ctx context.Context, deps OpDeps) (int, error) {
	a.called = true
	return a.n, a.err
}

// ApplyMigration is a dumb executor: it runs the action it was handed and
// then stamps the marker. Nothing about WHICH store is being migrated may
// live in the op.
func TestApplyMigrationRunsWhateverActionItWasHanded(t *testing.T) {
	t.Parallel()
	act := &countingAction{n: 3}
	deps, repo := newMigrationDeps(t) // existing test helper; see Step 3
	op := ApplyMigration{Feature: "f", Store: "s", To: 2, Action: act}
	res, err := op.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !act.called {
		t.Fatal("ApplyMigration did not run its action")
	}
	if !repo.stamped("s", 2) {
		t.Fatal("ApplyMigration did not stamp the marker")
	}
	if !res.Changed {
		t.Fatal("Result.Changed is false")
	}
}

// A failing action aborts BEFORE the marker is stamped: a half-migrated store
// must not be labelled migrated.
func TestApplyMigrationDoesNotStampWhenTheActionFails(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	act := &countingAction{err: boom}
	deps, repo := newMigrationDeps(t)
	op := ApplyMigration{Feature: "f", Store: "s", To: 2, Action: act}
	if _, err := op.Run(context.Background(), deps); !errors.Is(err, boom) {
		t.Fatalf("Run err = %v, want boom", err)
	}
	if repo.stamped("s", 2) {
		t.Fatal("the marker was stamped after the action failed")
	}
}

// A nil action is a caller bug, refused like the other zero-value guards.
func TestApplyMigrationRefusesANilAction(t *testing.T) {
	t.Parallel()
	deps, _ := newMigrationDeps(t)
	op := ApplyMigration{Feature: "f", Store: "s", To: 2}
	if _, err := op.Run(context.Background(), deps); err == nil {
		t.Fatal("Run accepted a nil Action")
	}
}

// DiscardRefs is today's behaviour, extracted verbatim: it deletes exactly
// the refs it was handed, in order, and reports the count.
func TestDiscardRefsDeletesExactlyWhatItWasHanded(t *testing.T) {
	t.Parallel()
	deps, repo := newMigrationDeps(t)
	act := DiscardRefs{Refs: []string{"refs/gg/versions/a", "refs/gg/versions/b"}}
	n, err := act.Apply(context.Background(), deps)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}
	if got := repo.deletedRefs(); len(got) != 2 || got[0] != "refs/gg/versions/a" || got[1] != "refs/gg/versions/b" {
		t.Fatalf("deleted %v, want both refs in order", got)
	}
}
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `go test ./internal/engine/ -run 'TestApplyMigration|TestDiscardRefs' -v`
Expected: FAIL to COMPILE — `unknown field Action in struct literal of type ApplyMigration`.

- [ ] **Step 3: Provide the test helper**

`internal/engine/apply_migration_test.go` already builds `OpDeps` around a fake repo for the existing `ApplyMigration` tests. Extract that setup into `newMigrationDeps(t *testing.T) (OpDeps, *fakeMigrationRepo)` and give the fake two accessors used above: `stamped(store string, format int) bool` and `deletedRefs() []string`. Reuse the existing fake rather than writing a second one; if the existing tests assert on package-level slices, move those into the fake so both old and new tests read the same recorder.

- [ ] **Step 4: Implement `internal/engine/migration_actions.go`**

```go
package engine

import (
	"context"
	"fmt"
)

// MigrationAction is the BODY of a migration: what to do to the stale data
// before the new format marker is stamped.
//
// It exists because gg's stores are not all the same kind of thing. The first
// migration deleted git refs, so ApplyMigration deleted git refs; the next
// one converts a machine-local TOML file, which is not a ref and not a
// deletion. Rather than grow a field per store, the op takes the action it is
// to run — the caller (domain) still decides WHAT, and the op still only runs
// it under the reservation.
type MigrationAction interface {
	// Describe names the action in English for the operation summary.
	Describe() string
	// Apply performs the migration and reports how many entries it affected.
	Apply(ctx context.Context, deps OpDeps) (int, error)
}

// DiscardRefs deletes the listed refs. This is ApplyMigration's original
// hardcoded body, extracted unchanged: the branch-versions migration (format
// 1 → 2) is its caller, and a format-1 version ref can never be converted
// because it records no merge base.
type DiscardRefs struct {
	Refs []string
}

func (a DiscardRefs) Describe() string { return "discarding stale refs" }

func (a DiscardRefs) Apply(ctx context.Context, deps OpDeps) (int, error) {
	for _, ref := range a.Refs {
		if err := deps.Repo.DeleteRef(ctx, ref); err != nil {
			return 0, fmt.Errorf("deleting %s: %w", ref, err)
		}
	}
	return len(a.Refs), nil
}
```

- [ ] **Step 5: Rewrite `internal/engine/apply_migration.go`**

Replace the `Refs []string` field with `Action MigrationAction`, delete the ref loop, and call the action:

```go
// ApplyMigration runs a migration's action and stamps the new format marker.
// It is the ONLY write preflight ever performs, and it happens only after the
// migration's consent rule is satisfied — the frontends ask; this op does not.
//
// Action is constructed by the caller (domain) so the op stays a dumb
// executor: it never decides WHAT is stale, only runs what it was handed,
// under the reservation, and stamps the marker afterwards.
type ApplyMigration struct {
	Feature string          // English feature id, for the summary
	Store   string          // store whose marker is stamped
	To      int             // format written after the migration
	Action  MigrationAction // what to do to the stale data
}

var _ Operation = ApplyMigration{}

func (op ApplyMigration) LockMode() repogate.Mode { return repogate.RefWrite }

func (op ApplyMigration) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Store == "" {
		return Result{}, fmt.Errorf("apply migration: Store is required")
	}
	if op.To <= 0 {
		return Result{}, fmt.Errorf("apply migration: To must be a positive format number")
	}
	if op.Action == nil {
		return Result{}, fmt.Errorf("apply migration: Action is required")
	}

	deps.emit(ctx, Progress{Step: "migrating store", Detail: op.Store})

	n, err := op.Action.Apply(ctx, deps)
	if err != nil {
		return Result{}, fmt.Errorf("apply migration: %s: %w", op.Action.Describe(), err)
	}
	// The marker is stamped only after the action succeeded: a half-migrated
	// store must never be labelled migrated.
	if err := deps.Repo.StampStoreFormat(ctx, op.Store, op.To); err != nil {
		return Result{}, fmt.Errorf("apply migration: stamping %s format %d: %w", op.Store, op.To, err)
	}

	res := Result{Changed: true}.WithSummary("migrated %s to format %d (%d entries)", op.Store, op.To, n)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

- [ ] **Step 6: Update the one caller**

`internal/domain/preflight.go:292` currently reads:

```go
op := engine.ApplyMigration{Feature: m.Feature, Store: m.Store, To: m.To, Refs: m.Refs}
```

Change it to:

```go
op := engine.ApplyMigration{Feature: m.Feature, Store: m.Store, To: m.To, Action: engine.DiscardRefs{Refs: m.Refs}}
```

- [ ] **Step 7: Run the tests and make sure they pass**

Run: `go test ./internal/engine/ ./internal/domain/ -run 'Migration|Migrate|Preflight' -v`
Expected: PASS, including every pre-existing migration test.

- [ ] **Step 8: Watch the guards fail**

| break | test that must fail |
|---|---|
| move the `StampStoreFormat` call ABOVE the `Action.Apply` call | `TestApplyMigrationDoesNotStampWhenTheActionFails` |
| drop the `op.Action == nil` guard | `TestApplyMigrationRefusesANilAction` (panics — a panic is not a pass; the test must report failure, so assert with `err == nil` only after the call returns) |
| `DiscardRefs.Apply` returns `nil` without looping | `TestDiscardRefsDeletesExactlyWhatItWasHanded` |

- [ ] **Step 9: Commit**

```bash
git add internal/engine internal/domain/preflight.go
git commit -m "refactor(engine): a migration names its own action"
```

---

### Task 5: the machine-local probe and the consent rule

**Files:**
- Modify: `internal/preflight/preflight.go`
- Test: `internal/preflight/preflight_test.go` (modify), `internal/preflight/legacystore_test.go` (create)

**Interfaces:**
- Consumes: the existing `Requirement`, `Probes`, `Migration`, `Resolve`.
- Produces:
  ```go
  type LegacyProbe struct{ Present bool }
  // Probes gains: Legacy map[string]LegacyProbe
  type LegacyStore struct{ Store string }   // implements Requirement
  // Migration gains: Action string; Lossless bool  (NOT Consent: the zero value must be the safe one)
  ```

**Why a new requirement kind rather than `DataFormat`:** `DataFormat` compares a store's marker against a version range, and the marker is a git ref inside `.git`. `previews.toml` is machine-local, so a ref-backed marker would be stamped by one environment and read as authoritative by another that still holds the file. There are no format numbers in this migration at all: `previews.toml` and `savedcompare.toml` are different files, so the complete question is "is the legacy file still here, on this machine".

- [ ] **Step 1: Write the failing test**

```go
package preflight

import "testing"

// A legacy store still holding data is Repairable when the feature declares a
// migration, and Satisfied once the file is gone.
func TestLegacyStoreFitsOnPresence(t *testing.T) {
	t.Parallel()
	req := LegacyStore{Store: "previews"}
	present := Probes{Legacy: map[string]LegacyProbe{"previews": {Present: true}}}
	absent := Probes{Legacy: map[string]LegacyProbe{"previews": {Present: false}}}
	unprobed := Probes{}

	if got := req.Fit(present); got != FitTooOld {
		t.Fatalf("Fit(present) = %v, want FitTooOld", got)
	}
	if got := req.Fit(absent); got != FitOK {
		t.Fatalf("Fit(absent) = %v, want FitOK", got)
	}
	// An UNPROBED store must read as OK, never as present: a caller that
	// skipped the probe must not trigger a migration it never measured.
	if got := req.Fit(unprobed); got != FitOK {
		t.Fatalf("Fit(unprobed) = %v, want FitOK", got)
	}
	if req.RepairStore() != "previews" {
		t.Fatalf("RepairStore = %q, want %q", req.RepairStore(), "previews")
	}
	// It reads Probes.Legacy, NOT Probes.Stores, so a Required-only resolve
	// must not be told it needs the git-ref store probes on its account.
	if req.NeedsStoreProbes() {
		t.Fatal("LegacyStore asked for the git-ref store probes")
	}
}

// A feature with a legacy store and a declared migration resolves Repairable.
func TestLegacyStoreResolvesRepairableWithAMigration(t *testing.T) {
	t.Parallel()
	f := Feature{
		ID:          "previews",
		Criticality: Optional,
		Requires:    []Requirement{LegacyStore{Store: "previews"}},
		Migrate:     &Migration{Store: "previews", To: 2, Action: "convert-previews", Lossless: true},
	}
	vs := Resolve([]Feature{f}, Probes{Legacy: map[string]LegacyProbe{"previews": {Present: true}}})
	if len(vs) != 1 {
		t.Fatalf("Resolve returned %d verdicts, want 1", len(vs))
	}
	if vs[0].State != Repairable {
		t.Fatalf("State = %v, want Repairable", vs[0].State)
	}
}

// THE POLARITY GATE. Lossless is what lets a migration run unasked, and its
// zero value must be the SAFE one: a migration that forgot to declare has to
// fall towards the consent screen, never past it. The case that makes this
// matter is named in the test: the branch-versions migration, the one that
// really does destroy data, declares nothing in this field at all.
func TestAMigrationThatDeclaresNothingIsNotLossless(t *testing.T) {
	t.Parallel()
	var m Migration
	if m.Lossless {
		t.Fatal("an undeclared Migration reads as lossless — it would destroy data unasked")
	}
	versions := Migration{Store: "versions", From: 1, To: 2}
	if versions.Lossless {
		t.Fatal("the branch-versions discard reads as lossless")
	}
}
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `go test ./internal/preflight/ -run 'Legacy|Lossless' -v`
Expected: FAIL to COMPILE — `undefined: LegacyStore`, `unknown field Legacy in struct literal of type Probes`.

- [ ] **Step 3: Implement**

In `internal/preflight/preflight.go`, extend `Probes`:

```go
// LegacyProbe is what the resolver knows about one superseded store. Present
// means its data file is still on THIS machine.
//
// Deliberately not a StoreProbe: a StoreProbe's Format comes from a git-ref
// marker inside .git, which is the wrong scope for a machine-local file — one
// .git opened from two environments is one marker and two data directories,
// so a marker-based verdict would report one side's migration as the other
// side's fact.
type LegacyProbe struct{ Present bool }

// Probes is everything the resolver is allowed to look at.
type Probes struct {
	Stores     map[string]StoreProbe
	Legacy     map[string]LegacyProbe
	GitVersion [3]int
}
```

Add the requirement:

```go
// LegacyStore requires that a superseded store's data has been absorbed —
// i.e. that its file is gone from this machine. There are no format numbers:
// the old store and the new one are different files, so presence is the whole
// question.
type LegacyStore struct{ Store string }

func (l LegacyStore) Fit(p Probes) Fit {
	if p.Legacy[l.Store].Present {
		return FitTooOld
	}
	return FitOK
}

func (l LegacyStore) Reason(p Probes) Text {
	return Text{Format: "the %s store still holds data from an older layout", Args: []any{l.Store}}
}

func (l LegacyStore) Remedy(p Probes) Text {
	return Text{Format: "Upgrade gg to use this feature."}
}

func (l LegacyStore) RepairStore() string { return l.Store }

// NeedsStoreProbes is FALSE: this requirement reads Probes.Legacy, never
// Probes.Stores, so it never obliges a caller to run the git-ref probes.
func (l LegacyStore) NeedsStoreProbes() bool { return false }
```

Extend `Migration`:

```go
// Migration repairs one store. Describe returns the consequence prose shown
// before consent.
type Migration struct {
	Store    string
	From, To int
	// Action names the body domain will construct for this migration — an
	// English protocol value ("discard-refs", "convert-previews"), not code,
	// so this package stays a stdlib leaf whose whole decision table is
	// testable with plain values.
	Action string
	// Lossless says this migration destroys nothing, so it may run without
	// asking. The ZERO VALUE IS THE SAFE ONE: spelled "Consent bool", a
	// migration that forgot to declare would destroy data unasked, and the
	// branch-versions migration declares nothing here.
	Lossless bool
	Describe func() Text
}
```

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `go test ./internal/preflight/ -v`
Expected: PASS, including every pre-existing resolver test.

- [ ] **Step 5: Watch the guards fail**

| break | test that must fail |
|---|---|
| `LegacyStore.Fit` returns `FitTooOld` when the map is missing the key | `TestLegacyStoreFitsOnPresence` (the `unprobed` case) |
| `LegacyStore.NeedsStoreProbes` returns `true` | `TestLegacyStoreFitsOnPresence` |
| `LegacyStore.RepairStore` returns `""` | `TestLegacyStoreResolvesRepairableWithAMigration` (falls to Unsatisfiable) |

- [ ] **Step 6: Commit**

```bash
git add internal/preflight
git commit -m "feat(preflight): probe a machine-local legacy store; a migration declares its consent rule"
```

---

### Task 6: register the conversion and run it

**Files:**
- Modify: `internal/domain/features.go`, `internal/domain/preflight.go`
- Create: `internal/domain/savedcomparestore.go` (the directory accessor only; Task 7 grows it into the full store resolver)
- Create: `internal/engine/migration_actions.go` (append `ConvertPreviews`)
- Modify: `cmd/gg/main.go` (composition root)
- Test: `internal/domain/previewmigration_test.go` (create)

**Interfaces:**
- Consumes: `preflight.LegacyStore`, `preflight.Migration{Action, Lossless}` (Task 5); `engine.MigrationAction`, `engine.ApplyMigration{Action}` (Task 4); `savedcompare.ConvertLegacy(dir, repo)` and `savedcompare.LegacyFile` (Task 3).
- Produces:
  ```go
  const FeaturePreviews = "previews"
  const StorePreviews   = "previews"
  const PreviewsFormat  = 2
  func (s *Service) RunAutoMigrations(ctx context.Context) error
  func (s *Service) savedCompareDir(ctx context.Context) string
  // engine:
  type ConvertPreviews struct { Dir string; Repo model.LinkRepo }
  ```
- **`savedCompareDir` is created in THIS task**, in a new `internal/domain/savedcomparestore.go` holding nothing else yet. Task 7 adds the store resolver beside it and deletes `previewstore.go`. Its body is given verbatim in Task 7 Step 3 — write it here and leave it untouched there.

- [ ] **Step 1: Write the failing test**

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const legacyPreviewsTOML = `[[previews]]
id = "abcd1234"
source = "feat/login"
target = "main"
label = "login work"
created = 2026-09-01T10:00:00Z
`

// RunAutoMigrations converts a legacy previews file WITHOUT asking, because
// the conversion is lossless. After it runs, the preview is readable through
// the ordinary PreviewList façade and the legacy file is gone.
//
// This test mutates the PreviewStatePath package global, so it does NOT call
// t.Parallel().
func TestRunAutoMigrationsConvertsPreviewsWithoutConsent(t *testing.T) {
	svc, _ := newTestService(t) // existing helper: a real git repo in a TempDir
	dir := t.TempDir()
	old := PreviewStatePath
	PreviewStatePath = dir
	t.Cleanup(func() { PreviewStatePath = old })

	legacy := filepath.Join(dir, "previews.toml")
	if err := os.WriteFile(legacy, []byte(legacyPreviewsTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.RunAutoMigrations(context.Background()); err != nil {
		t.Fatalf("RunAutoMigrations: %v", err)
	}

	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("previews.toml survived the conversion (stat err = %v)", err)
	}
	ps, err := svc.PreviewList(context.Background())
	if err != nil {
		t.Fatalf("PreviewList: %v", err)
	}
	if len(ps) != 1 {
		t.Fatalf("PreviewList = %d entries, want 1", len(ps))
	}
	got := ps[0]
	if got.Source != "feat/login" || got.Target != "main" {
		t.Fatalf("converted preview = %+v, want source=feat/login target=main", got)
	}
	if got.ID != "abcd1234" || got.Label != "login work" {
		t.Fatalf("converted preview lost its id/label: %+v", got)
	}
}

// A LOSSLESS migration never appears in PendingMigrations: that list feeds the
// three consent screens, and asking about a conversion that loses nothing is
// a prompt with no decision in it.
func TestPendingMigrationsExcludesLosslessOnes(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	old := PreviewStatePath
	PreviewStatePath = dir
	t.Cleanup(func() { PreviewStatePath = old })
	if err := os.WriteFile(filepath.Join(dir, "previews.toml"), []byte(legacyPreviewsTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	pending, err := svc.PendingMigrations(context.Background())
	if err != nil {
		t.Fatalf("PendingMigrations: %v", err)
	}
	for _, m := range pending {
		if m.Store == StorePreviews {
			t.Fatalf("a lossless migration was offered for consent: %+v", m)
		}
	}
}

// Nothing to convert costs nothing and is not an error — this runs at every
// startup.
func TestRunAutoMigrationsWithNothingToDoIsANoOp(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	old := PreviewStatePath
	PreviewStatePath = dir
	t.Cleanup(func() { PreviewStatePath = old })
	if err := svc.RunAutoMigrations(context.Background()); err != nil {
		t.Fatalf("RunAutoMigrations: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "savedcompare.toml")); !os.IsNotExist(err) {
		t.Fatalf("a no-op run wrote savedcompare.toml (stat err = %v)", err)
	}
}
```

Use whichever service-construction helper `internal/domain`'s existing tests use (grep `func newTestService` / `func newService` in `internal/domain/*_test.go`) and match its signature; the only requirement is a `*Service` over a real repo in a `t.TempDir()`.

- [ ] **Step 2: Run it to make sure it fails**

Run: `go test ./internal/domain/ -run 'RunAutoMigrations|PendingMigrationsExcludes' -v`
Expected: FAIL to COMPILE — `svc.RunAutoMigrations undefined`.

- [ ] **Step 3: Add the `ConvertPreviews` action**

Append to `internal/engine/migration_actions.go`:

```go
// ConvertPreviews folds the legacy merge-preview store into savedcompare.
// Lossless: ids, labels and creation times are carried across, and preview
// notes need no migration because they key on the branch NAMES.
type ConvertPreviews struct {
	Dir  string         // the per-repo state directory holding both files
	Repo model.LinkRepo // this repository's link identity
}

func (a ConvertPreviews) Describe() string { return "converting saved merge previews" }

func (a ConvertPreviews) Apply(ctx context.Context, deps OpDeps) (int, error) {
	return savedcompare.ConvertLegacy(a.Dir, a.Repo)
}
```

Add the `model` and `savedcompare` imports. If `internal/archtest` forbids `engine` importing `savedcompare`, move `ConvertPreviews` to `internal/domain` instead and have `RunAutoMigrations` construct it there — the `MigrationAction` interface is satisfied from any package, and domain is already the layer that "decides WHAT". Check `internal/archtest` first and follow whichever it allows; prefer domain if both are legal, since domain already owns every store.

- [ ] **Step 4: Register the feature**

In `internal/domain/features.go`, add the ids and the feature:

```go
const (
	FeatureCore     = "core"
	FeatureVersions = "versions"
	FeaturePreviews = "previews"

	StoreVersions = "versions"
	StorePreviews = "previews"
)

// PreviewsFormat is the saved-comparison layout this build writes. Format 1
// was the standalone previews.toml merge-preview store; format 2 is the
// savedcompare store that absorbs it (spec §4.5).
const PreviewsFormat = 2
```

and inside `Features()`:

```go
{
	ID:          FeaturePreviews,
	Criticality: preflight.Optional,
	Requires: []preflight.Requirement{
		preflight.LegacyStore{Store: StorePreviews},
	},
	// Lossless: every saved preview is converted into
	// the savedcompare store with its id, label and creation time intact,
	// and preview notes need no migration at all — they key on the branch
	// NAMES. There is nothing to confess, so there is nothing to ask.
	Migrate: &preflight.Migration{
		Store: StorePreviews, From: 1, To: PreviewsFormat,
		Action: "convert-previews", Lossless: true,
		Describe: func() preflight.Text {
			return preflight.Text{
				Format: "Folds your saved merge previews into the saved-comparison store. Nothing is lost: ids, labels and creation times are kept, and preview notes are unaffected.",
			}
		},
	},
},
```

- [ ] **Step 5: Probe, filter and run**

In `internal/domain/preflight.go`:

1. Where `Probes` is built (around line 70), populate `Legacy`:

```go
Legacy: map[string]preflight.LegacyProbe{
	StorePreviews: {Present: s.legacyPreviewsPresent(ctx)},
},
```

with

```go
// legacyPreviewsPresent reports whether this machine still holds the
// superseded previews.toml for this repository. One os.Stat; a missing state
// directory reads as absent.
func (s *Service) legacyPreviewsPresent(ctx context.Context) bool {
	dir := s.savedCompareDir(ctx) // created in this task; see Interfaces
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, savedcompare.LegacyFile))
	return err == nil
}
```

2. In `PendingMigrations`, skip migrations that need no consent:

```go
mig := v.Feature.Migrate
if !mig.Lossless {
	continue // lossless: RunAutoMigrations handles it; never prompt
}
```

3. Add `RunAutoMigrations`:

```go
// RunAutoMigrations applies every pending migration that needs no consent,
// because it loses nothing. Called once per process from the composition
// root, before a surface starts. It costs one os.Stat per registered legacy
// store when there is nothing to do.
//
// Consent-requiring migrations are NOT run here — they stay in
// PendingMigrations for the CLI/TUI/web consent screens.
func (s *Service) RunAutoMigrations(ctx context.Context) error {
	vs, err := s.Preflight(ctx)
	if err != nil {
		return err
	}
	ran := false
	for _, v := range vs {
		if v.State != preflight.Repairable || v.Feature.Migrate == nil || !v.Feature.Migrate.Lossless {
			continue
		}
		mig := v.Feature.Migrate
		act, err := s.migrationAction(ctx, mig)
		if err != nil {
			return err
		}
		op := engine.ApplyMigration{Feature: v.Feature.ID, Store: mig.Store, To: mig.To, Action: act}
		if _, err := s.Execute(ctx, op, nil, nil); err != nil {
			return err
		}
		ran = true
	}
	if ran {
		s.preflightMu.Lock()
		s.preflightDone, s.preflightOut, s.preflightMarks = false, nil, nil
		s.preflightMu.Unlock()
	}
	return nil
}

// migrationAction builds the body for one declared migration. This is the one
// place an Action name becomes code, so preflight stays a stdlib leaf.
func (s *Service) migrationAction(ctx context.Context, m *preflight.Migration) (engine.MigrationAction, error) {
	switch m.Action {
	case "convert-previews":
		repo, err := s.LinkRepo(ctx) // this repository's link identity
		if err != nil {
			return nil, err
		}
		return engine.ConvertPreviews{Dir: s.savedCompareDir(ctx), Repo: repo}, nil
	case "discard-refs", "":
		refs, err := s.storeRefs(ctx, m.Store)
		if err != nil {
			return nil, err
		}
		return engine.DiscardRefs{Refs: refs}, nil
	default:
		return nil, fmt.Errorf("preflight: no action named %q", m.Action)
	}
}
```

4. Rewrite `RunMigration` to use the same builder, so the consent path and the automatic path can never construct different bodies:

```go
func (s *Service) RunMigration(ctx context.Context, m PendingMigration) error {
	act, err := s.migrationAction(ctx, &preflight.Migration{Store: m.Store, From: m.From, To: m.To, Action: m.Action})
	if err != nil {
		return err
	}
	op := engine.ApplyMigration{Feature: m.Feature, Store: m.Store, To: m.To, Action: act}
	if _, err := s.Execute(ctx, op, nil, nil); err != nil {
		return err
	}
	s.preflightMu.Lock()
	s.preflightDone, s.preflightOut, s.preflightMarks = false, nil, nil
	s.preflightMu.Unlock()
	return nil
}
```

Add `Action string` to `PendingMigration` and populate it from `mig.Action` where the struct is built, so the consent path carries the name through.

For `s.LinkRepo(ctx)`: if `domain` has no such method, find how `internal/cli/link.go`'s `buildLink` obtains the `model.LinkRepo` for the current repository and expose that same derivation as a `Service` method — do not duplicate the logic. Grep `LinkRepo{` in `internal/domain` and `internal/cli` first.

- [ ] **Step 6: Wire the composition root**

In `cmd/gg/main.go`, next to the existing `tui.Preflight(svc, os.Stdin, os.Stderr)` call (line ~183), call `RunAutoMigrations` BEFORE it, on every surface (TUI, CLI, web, MCP) — i.e. at the one place the `*domain.Service` is constructed and handed to a surface, not once per surface:

```go
// Lossless migrations run before any surface starts: they need no decision
// from the user, and a surface that read the store first would read the old
// layout. A failure here is reported and does NOT stop gg — a store that
// could not be converted is a degraded surface, not a broken repository.
if err := svc.RunAutoMigrations(context.Background()); err != nil {
	fmt.Fprintln(os.Stderr, "warning: migrating stores:", err)
}
```

- [ ] **Step 7: Run the tests and make sure they pass**

Run: `go test ./internal/domain/ ./internal/engine/ ./internal/preflight/ -v`
Expected: PASS.

- [ ] **Step 8: Watch the guards fail**

| break | test that must fail |
|---|---|
| `RunAutoMigrations` skips migrations with `Lossless == true` (inverted condition) | `TestRunAutoMigrationsConvertsPreviewsWithoutConsent` |
| `PendingMigrations` drops its `!mig.Lossless` skip | `TestPendingMigrationsExcludesLosslessOnes` |
| `legacyPreviewsPresent` returns `true` unconditionally | `TestRunAutoMigrationsWithNothingToDoIsANoOp` |

- [ ] **Step 9: Commit**

```bash
git add internal/domain internal/engine cmd/gg/main.go
git commit -m "feat(domain): convert saved merge previews on first run, losslessly"
```

---

### Task 7: the domain façade, and `internal/preview` goes away

**Files:**
- Create: `internal/domain/savedcomparestore.go`
- Delete: `internal/domain/previewstore.go`, the whole of `internal/preview/`
- Modify: `internal/domain/preview.go`
- Test: `internal/domain/preview_test.go` (modify — the existing tests must pass UNCHANGED wherever they assert behaviour rather than the store type), `internal/domain/previewfacade_test.go` (create)

**Interfaces:**
- Consumes: `savedcompare.Store`, `savedcompare.NewFileStore`, `savedcompare.Entry`, `savedcompare.ErrNotFound`, `savedcompare.ErrExists` (Task 2).
- Produces:
  - unchanged public signatures: `PreviewAdd(ctx, source, target, label) (model.MergePreview, error)`, `PreviewList(ctx) ([]model.MergePreview, error)`, `PreviewGet(ctx, idOrLabel) (model.MergePreview, error)`, `PreviewRename(ctx, id, label) error`, `PreviewRemove(ctx, id) error`
  - unchanged seams: `PreviewStatePath`, `PreviewsDisabled`, `UsePreviewsDir(dir)`, `SetPreviewStore` → renamed `SetSavedCompareStore(st savedcompare.Store)`; keep a `SetPreviewStore` wrapper if any test calls it
  - new: `func (s *Service) savedCompareDir(ctx context.Context) string` (used by Task 6), `func (s *Service) savedCompareStore(ctx context.Context) savedcompare.Store`

**The two arms.** `entryFromPreview` and `previewFromEntry` are the defining two-arms pair of this plan: they look alike, and one swaps the halves the other swaps back. They must be tested on ONE fixture whose source and target differ, asserting the LINK TEXT in between — not just that a round trip returns its input, which a doubly-swapped pair also satisfies.

- [ ] **Step 1: Write the failing test**

```go
package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/savedcompare"
)

// The façade's two conversions are two arms that look alike and must undo
// each other. A round trip alone cannot see a DOUBLE swap, so this asserts
// the link text in the middle: target first.
func TestPreviewEntryConversionPutsTargetFirst(t *testing.T) {
	t.Parallel()
	repo := model.LinkRepo{Name: "gigagit"}
	p := model.MergePreview{ID: "abcd1234", Source: "feat/login", Target: "main", Label: "login work"}

	e, err := entryFromPreview(repo, p)
	if err != nil {
		t.Fatalf("entryFromPreview: %v", err)
	}
	if want := "gg://gigagit@main...feat/login"; e.Left.String() != want {
		t.Fatalf("Left = %q, want %q (target first)", e.Left.String(), want)
	}
	if !e.IsSet() {
		t.Fatal("a preview must convert to a SET, not a pair")
	}

	back, ok := previewFromEntry(e)
	if !ok {
		t.Fatal("previewFromEntry refused a set-shaped entry")
	}
	if back.Source != "feat/login" || back.Target != "main" {
		t.Fatalf("round trip = %+v, want source=feat/login target=main", back)
	}
	if back.ID != p.ID || back.Label != p.Label {
		t.Fatalf("round trip lost id/label: %+v", back)
	}
}

// A PAIR-shaped entry is not a merge preview, and the façade must say so
// rather than invent one. This is the arm that has to DISAGREE.
func TestPreviewFromEntryRefusesAPairShapedEntry(t *testing.T) {
	t.Parallel()
	left, err := model.ParseLink("gg://gigagit@main...feat/login")
	if err != nil {
		t.Fatal(err)
	}
	right, err := model.ParseLink("gg://gigagit@abc1234")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := previewFromEntry(savedcompare.Entry{Left: left, Right: &right}); ok {
		t.Fatal("previewFromEntry accepted a pair-shaped entry as a merge preview")
	}
}

// A set-shaped entry whose left half is NOT a three-dot preview link (a saved
// bounded set from `gg compare --save`) is also not a merge preview.
func TestPreviewFromEntryRefusesANonPreviewSet(t *testing.T) {
	t.Parallel()
	left, err := model.ParseLink("gg://gigagit@abc1234..def5678")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := previewFromEntry(savedcompare.Entry{Left: left}); ok {
		t.Fatal("previewFromEntry accepted a change-set as a merge preview")
	}
}

// PreviewList shows merge previews and HIDES pair-shaped saved comparisons —
// the façade's whole job. Mutates PreviewStatePath, so no t.Parallel().
func TestPreviewListShowsOnlyMergePreviews(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	old := PreviewStatePath
	PreviewStatePath = dir
	t.Cleanup(func() { PreviewStatePath = old })

	ctx := context.Background()
	if _, err := svc.PreviewAdd(ctx, "feat/login", "main", "login work"); err != nil {
		t.Fatalf("PreviewAdd: %v", err)
	}
	if _, err := svc.SavedCompareAdd(ctx, "gg://gigagit@abc1234", "gg://gigagit@def5678", "a comparison"); err != nil {
		t.Fatalf("SavedCompareAdd: %v", err)
	}

	ps, err := svc.PreviewList(ctx)
	if err != nil {
		t.Fatalf("PreviewList: %v", err)
	}
	if len(ps) != 1 || ps[0].Source != "feat/login" {
		t.Fatalf("PreviewList = %+v, want exactly the merge preview", ps)
	}
	all, err := svc.SavedCompareList(ctx)
	if err != nil {
		t.Fatalf("SavedCompareList: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("SavedCompareList = %d entries, want 2 (both shapes)", len(all))
	}
}
```

`PreviewAdd` resolves both names against the repo, so the fixture repo needs branches `main` and `feat/login`. Create them in the test helper (`newTestService` already builds a real repo; add the branch with the same helper its neighbours use).

`SavedCompareAdd`/`SavedCompareList` arrive in Task 8 — write this test now, and expect the last case to stay red until Task 8 lands. If that is uncomfortable, split `TestPreviewListShowsOnlyMergePreviews` into Task 8 instead; the first three cases must land here.

- [ ] **Step 2: Run it to make sure it fails**

Run: `go test ./internal/domain/ -run 'TestPreviewEntryConversion|TestPreviewFromEntry' -v`
Expected: FAIL to COMPILE — `undefined: entryFromPreview`.

- [ ] **Step 3: Write `internal/domain/savedcomparestore.go`**

`savedCompareDir` already exists from Task 6. Copy `internal/domain/previewstore.go` into the same file and change four things: the import to `internal/savedcompare`, the field type to `savedcompare.Store`, `preview.NewFileStore` to `savedcompare.NewFileStore`, and the resolver's name to `savedCompareStore`. **Keep `PreviewStatePath`, `PreviewsDisabled` and `UsePreviewsDir` under their existing names** — they are the test seam for `internal/tui` and `internal/web`, which cannot import a store package, and renaming them here is churn that belongs to plan 3c when the Previews tab becomes the saved-comparisons tab. Keep the kind string `stateBaseDir("previews")` exactly.

For reference, the accessor Task 6 already wrote — `savedCompareStore` must call it rather than re-deriving the path:

```go
// savedCompareDir is the per-repo state directory, keyed by git common dir
// under <state>/gg/previews. "" = disabled (no state dir, or PreviewsDisabled).
//
// The kind string stays "previews" on purpose: the legacy previews.toml this
// store absorbs is a sibling in this very directory, so the conversion needs
// no path plumbing, and domain's stateBaseDir caller gate keeps its pinned
// set of kinds unchanged. A kind string is a DIRECTORY ON A USER'S DISK.
func (s *Service) savedCompareDir(ctx context.Context) string {
	if PreviewStatePath != "" {
		return PreviewStatePath
	}
	if PreviewsDisabled {
		return ""
	}
	base := stateBaseDir("previews")
	if base == "" {
		return ""
	}
	key := "unknown"
	if cd, err := s.GitCommonDir(ctx); err == nil {
		key = repoKey(strings.TrimSpace(cd))
	}
	return filepath.Join(base, key)
}
```

and have `savedCompareStore` call it.

- [ ] **Step 4: Write the two conversions in `internal/domain/preview.go`**

```go
// entryFromPreview renders a saved merge preview as a set-shaped entry.
//
// TARGET FIRST: PreviewAdd(source, target) means "what source brings into
// target", which renders merge-base(target, source)..source and is spelled
// `@target...source` — the order `git diff target...source` reads. This
// function and previewFromEntry are two arms that look alike and must undo
// each other exactly; a swap in either is invisible unless a test asserts the
// LINK TEXT between them on a fixture whose two names differ.
func entryFromPreview(repo model.LinkRepo, p model.MergePreview) (savedcompare.Entry, error) {
	l, err := model.ParseLink(model.Link{Repo: repo, Target: model.LinkTarget{
		State:   model.StateCommitted,
		Preview: &model.LinkPreview{Source: p.Source, Target: p.Target},
	}}.String())
	if err != nil {
		return savedcompare.Entry{}, err
	}
	return savedcompare.Entry{ID: p.ID, Left: l, Label: p.Label, Created: p.Created}, nil
}

// previewFromEntry reads a set-shaped entry back as a merge preview, and
// reports false for everything that is not one: a PAIR-shaped entry (a saved
// comparison), and a set whose left half is not a three-dot preview link (a
// saved change-set). Both are legitimate savedcompare rows that the preview
// surfaces must not show.
func previewFromEntry(e savedcompare.Entry) (model.MergePreview, bool) {
	if e.Right != nil {
		return model.MergePreview{}, false
	}
	pv := e.Left.Target.Preview
	if pv == nil {
		return model.MergePreview{}, false
	}
	return model.MergePreview{
		ID: e.ID, Source: pv.Source, Target: pv.Target,
		Label: e.Label, Created: e.Created,
	}, true
}
```

- [ ] **Step 5: Rewrite the five façade methods**

Keep every signature and every wrapped error identical. `PreviewList` filters through `previewFromEntry`; `PreviewAdd` keeps its two `ResolveRev` checks and its `source == target` refusal, then converts and calls `st.Add`, mapping `savedcompare.ErrExists` to `ErrPreviewExists` and returning the existing record converted back. `PreviewGet` keeps its id-then-label search over `PreviewList`. `PreviewRename`/`PreviewRemove` delegate, mapping `savedcompare.ErrNotFound` to `ErrPreviewNotFound`.

Redefine the wrapped sentinels against the new package:

```go
var (
	ErrPreviewNotFound = fmt.Errorf("%w", savedcompare.ErrNotFound)
	ErrPreviewExists   = fmt.Errorf("%w", savedcompare.ErrExists)
)
```

**`PreviewRemove` must refuse to remove a pair-shaped entry by id**, or a saved comparison could be deleted through a preview surface. Look the entry up first and return `ErrPreviewNotFound` when `previewFromEntry` reports false.

- [ ] **Step 6: Delete the old store**

```bash
git rm -r internal/preview
git rm internal/domain/previewstore.go
```

Fix every compile error the deletion surfaces. `internal/domain/evallink.go` and `internal/domain/previewnotes.go` both touch previews — they go through the façade, so they should need no change beyond imports; if either imports `internal/preview` directly, route it through the façade instead.

- [ ] **Step 7: Run the whole suite**

Run: `./test.sh unit`
Expected: PASS. The pre-existing `internal/domain/preview_test.go`, `internal/tui/preview_panel_test.go` and `internal/web/previews_test.go` must pass **unchanged** except for store-type references — that is the evidence the façade is faithful. If a behavioural test needed editing, stop and work out why the façade changed behaviour.

- [ ] **Step 8: Watch the guards fail**

| break | test that must fail |
|---|---|
| `entryFromPreview` builds `{Source: p.Target, Target: p.Source}` | `TestPreviewEntryConversionPutsTargetFirst` |
| **both** conversions swap (the double swap) | `TestPreviewEntryConversionPutsTargetFirst` — on the LINK TEXT assertion. If it passes, the test is round-tripping and cannot see its subject. |
| `previewFromEntry` drops its `e.Right != nil` check | `TestPreviewFromEntryRefusesAPairShapedEntry` |
| `previewFromEntry` drops its `pv == nil` check | `TestPreviewFromEntryRefusesANonPreviewSet` |

- [ ] **Step 9: Verify the kind gate is untouched**

Run: `go test ./internal/domain/ -run TestStateBaseDirCallersPassKnownKinds -v`
Expected: PASS, with `internal/domain/statebasedir_callers_test.go` **unmodified**. If it needed an edit, a kind was added or renamed — revert to `"previews"`.

- [ ] **Step 10: Commit**

```bash
git add -A internal/domain internal/preview
git commit -m "refactor(domain): previews become set-shaped saved comparisons"
```

---

### Task 8: the saved-comparison domain API

**Files:**
- Create: `internal/domain/savedcompare.go`
- Test: `internal/domain/savedcompare_test.go` (create)

**Interfaces:**
- Consumes: `savedCompareStore` (Task 7), `savedcompare.Entry`, `model.ParseLink`.
- Produces:
  ```go
  var ErrSavedCompareNotFound, ErrSavedCompareExists error
  type SavedCompare struct {
      ID, Left, Right, Label string // link TEXTS; Right == "" ⇒ a saved SET
      Created time.Time
  }
  func (s *Service) SavedCompareAdd(ctx context.Context, left, right, label string) (SavedCompare, error)
  func (s *Service) SavedCompareList(ctx context.Context) ([]SavedCompare, error)
  func (s *Service) SavedCompareGet(ctx context.Context, idOrLabel string) (SavedCompare, error)
  func (s *Service) SavedCompareRemove(ctx context.Context, id string) error
  ```

Frontends cannot import `internal/savedcompare`, so `SavedCompare` is the domain-level view: link TEXTS, which is exactly what a frontend pastes, prints and stores.

- [ ] **Step 1: Write the failing test**

```go
package domain

import (
	"context"
	"errors"
	"testing"
)

// Both shapes store and read back, and an empty right half means a SET.
// Mutates PreviewStatePath, so no t.Parallel().
func TestSavedCompareStoresBothShapes(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	old := PreviewStatePath
	PreviewStatePath = dir
	t.Cleanup(func() { PreviewStatePath = old })
	ctx := context.Background()

	pair, err := svc.SavedCompareAdd(ctx, "gg://gigagit@abc1234", "gg://gigagit@def5678", "a pair")
	if err != nil {
		t.Fatalf("SavedCompareAdd pair: %v", err)
	}
	set, err := svc.SavedCompareAdd(ctx, "gg://gigagit@main...feat/login", "", "a set")
	if err != nil {
		t.Fatalf("SavedCompareAdd set: %v", err)
	}
	if pair.ID == set.ID {
		t.Fatal("the pair and the set share an id")
	}

	got, err := svc.SavedCompareGet(ctx, set.ID)
	if err != nil {
		t.Fatalf("SavedCompareGet: %v", err)
	}
	if got.Right != "" {
		t.Fatalf("a set read back with a right half: %+v", got)
	}
	byLabel, err := svc.SavedCompareGet(ctx, "a pair")
	if err != nil || byLabel.ID != pair.ID {
		t.Fatalf("SavedCompareGet by label = %+v, %v", byLabel, err)
	}

	if err := svc.SavedCompareRemove(ctx, pair.ID); err != nil {
		t.Fatalf("SavedCompareRemove: %v", err)
	}
	if _, err := svc.SavedCompareGet(ctx, pair.ID); !errors.Is(err, ErrSavedCompareNotFound) {
		t.Fatalf("after Remove = %v, want ErrSavedCompareNotFound", err)
	}
}

// A malformed link is refused at the door, so the store never holds a row
// that cannot be read back.
func TestSavedCompareAddRefusesAMalformedLink(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	old := PreviewStatePath
	PreviewStatePath = dir
	t.Cleanup(func() { PreviewStatePath = old })
	if _, err := svc.SavedCompareAdd(context.Background(), "not-a-link", "", "x"); err == nil {
		t.Fatal("SavedCompareAdd accepted a non-link")
	}
}

// A duplicate returns the EXISTING record with ErrSavedCompareExists, so a
// frontend can focus it instead of failing.
func TestSavedCompareAddIsIdempotent(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	old := PreviewStatePath
	PreviewStatePath = dir
	t.Cleanup(func() { PreviewStatePath = old })
	ctx := context.Background()
	first, err := svc.SavedCompareAdd(ctx, "gg://gigagit@abc1234", "gg://gigagit@def5678", "mine")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	again, err := svc.SavedCompareAdd(ctx, "gg://gigagit@abc1234", "gg://gigagit@def5678", "other")
	if !errors.Is(err, ErrSavedCompareExists) {
		t.Fatalf("second err = %v, want ErrSavedCompareExists", err)
	}
	if again.ID != first.ID || again.Label != "mine" {
		t.Fatalf("ErrSavedCompareExists returned %+v, want the stored record", again)
	}
}
```

- [ ] **Step 2: Run it to make sure it fails**

Run: `go test ./internal/domain/ -run TestSavedCompare -v`
Expected: FAIL to COMPILE — `svc.SavedCompareAdd undefined`.

- [ ] **Step 3: Implement `internal/domain/savedcompare.go`**

```go
package domain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/savedcompare"
)

var ErrSavedComparesDisabled = errors.New("saved comparisons: no state directory available")

// ErrSavedCompareNotFound / ErrSavedCompareExists WRAP the store's errors so
// frontends (which cannot import internal/savedcompare) can errors.Is them.
var (
	ErrSavedCompareNotFound = fmt.Errorf("%w", savedcompare.ErrNotFound)
	ErrSavedCompareExists   = fmt.Errorf("%w", savedcompare.ErrExists)
)

// SavedCompare is one stored comparison as a frontend sees it: link TEXTS,
// which is what a frontend pastes, prints and hands back. Right == "" means a
// saved SET (a merge preview), not a pair.
type SavedCompare struct {
	ID      string
	Left    string
	Right   string
	Label   string
	Created time.Time
}

func fromEntry(e savedcompare.Entry) SavedCompare {
	out := SavedCompare{ID: e.ID, Left: e.Left.String(), Label: e.Label, Created: e.Created}
	if e.Right != nil {
		out.Right = e.Right.String()
	}
	return out
}

// SavedCompareAdd stores a comparison. right may be empty, which stores a
// bounded SET rather than a pair. Both halves are parsed here so the store
// never holds a row that cannot be read back.
func (s *Service) SavedCompareAdd(ctx context.Context, left, right, label string) (SavedCompare, error) {
	st := s.savedCompareStore(ctx)
	if st == nil {
		return SavedCompare{}, ErrSavedComparesDisabled
	}
	l, err := model.ParseLink(left)
	if err != nil {
		return SavedCompare{}, err
	}
	e := savedcompare.Entry{Left: l, Label: label}
	if right != "" {
		r, err := model.ParseLink(right)
		if err != nil {
			return SavedCompare{}, err
		}
		e.Right = &r
	}
	stored, err := st.Add(e)
	if errors.Is(err, savedcompare.ErrExists) {
		return fromEntry(stored), ErrSavedCompareExists
	}
	if err != nil {
		return SavedCompare{}, err
	}
	return fromEntry(stored), nil
}

// SavedCompareList returns BOTH shapes, in insertion order — unlike
// PreviewList, which shows only the merge previews among them.
func (s *Service) SavedCompareList(ctx context.Context) ([]SavedCompare, error) {
	st := s.savedCompareStore(ctx)
	if st == nil {
		return nil, ErrSavedComparesDisabled
	}
	es, err := st.List()
	if err != nil {
		return nil, err
	}
	out := make([]SavedCompare, 0, len(es))
	for _, e := range es {
		out = append(out, fromEntry(e))
	}
	return out, nil
}

// SavedCompareGet finds a record by id, else by exact label (first match) —
// the same order PreviewGet uses.
func (s *Service) SavedCompareGet(ctx context.Context, idOrLabel string) (SavedCompare, error) {
	cs, err := s.SavedCompareList(ctx)
	if err != nil {
		return SavedCompare{}, err
	}
	for _, c := range cs {
		if c.ID == idOrLabel {
			return c, nil
		}
	}
	for _, c := range cs {
		if c.Label == idOrLabel {
			return c, nil
		}
	}
	return SavedCompare{}, ErrSavedCompareNotFound
}

// SavedCompareRemove deletes by id. Unlike PreviewRemove it accepts BOTH
// shapes: this is the surface that owns every stored comparison.
func (s *Service) SavedCompareRemove(ctx context.Context, id string) error {
	st := s.savedCompareStore(ctx)
	if st == nil {
		return ErrSavedComparesDisabled
	}
	if err := st.Remove(id); errors.Is(err, savedcompare.ErrNotFound) {
		return ErrSavedCompareNotFound
	} else {
		return err
	}
}
```

- [ ] **Step 4: Run the tests and make sure they pass**

Run: `go test ./internal/domain/ -run 'TestSavedCompare|TestPreviewList' -v`
Expected: PASS, including `TestPreviewListShowsOnlyMergePreviews` from Task 7.

- [ ] **Step 5: Watch the guards fail**

| break | test that must fail |
|---|---|
| `SavedCompareAdd` skips `model.ParseLink` and stores a zero Link | `TestSavedCompareAddRefusesAMalformedLink` |
| `fromEntry` sets `Right` from `Left` when `e.Right == nil` | `TestSavedCompareStoresBothShapes` |
| `SavedCompareAdd` returns `SavedCompare{}` on `ErrExists` instead of the stored record | `TestSavedCompareAddIsIdempotent` |

- [ ] **Step 6: Commit**

```bash
git add internal/domain/savedcompare.go internal/domain/savedcompare_test.go
git commit -m "feat(domain): the saved-comparison API"
```

---

### Task 9: `gg compare --save`, `--saved`, `--list`

**Files:**
- Modify: `internal/cli/compare.go`
- Test: `internal/cli/comparesave_test.go` (create)

**Interfaces:**
- Consumes: `SavedCompareAdd/List/Get` (Task 8); `buildLink(ctx, svc, workdir, pathArg string, o linkOpts) (model.Link, error)` and `linkOpts{Cached, Rev, Ref, Pair, Preview, Hint}` from `internal/cli/link.go`; `resolveCompareSpec`, `isLinkArg`, `compareUsage`.
- Produces: `func compareTokenLink(ctx context.Context, svc *domain.Service, tok string) (model.Link, error)` — maps every token `gg compare` accepts onto a link.

**Why a token→link map:** §5.3 says links are the primary spelling and today's `bookmark:<id>` / `shelf:<id>` / `@staged` / `@worktree` "keep working, mapped onto links". A saved comparison holds links, so `--save` is where that mapping becomes real. `buildLink` is the one producer — never assemble a link string by hand.

- [ ] **Step 1: Write the failing test**

```go
package cli

import (
	"bytes"
	"strings"
	"testing"
)

// --save stores the comparison as two LINKS and prints the id. The test uses
// a real repo through the existing CLI test harness; follow the pattern in
// internal/cli/compare_test.go for constructing svc and the repo fixture.
//
// Mutates RepoStatePath, so it does NOT call t.Parallel() (see repo_test.go's
// convention note and linksession_serial_test.go).
func TestCompareSaveStoresBothSidesAsLinks(t *testing.T) {
	svc, statePath, repo := newCompareFixture(t) // see Step 2
	var out, errb bytes.Buffer

	code := cmdCompare(statePath, svc, []string{"--save", "my comparison", repo.headSha, "@worktree"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}

	cs, err := svc.SavedCompareList(t.Context())
	if err != nil {
		t.Fatalf("SavedCompareList: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("stored %d comparisons, want 1", len(cs))
	}
	got := cs[0]
	if got.Label != "my comparison" {
		t.Fatalf("Label = %q", got.Label)
	}
	if !strings.HasPrefix(got.Left, "gg://") || !strings.HasPrefix(got.Right, "gg://") {
		t.Fatalf("sides are not links: %+v", got)
	}
	// @worktree maps to a link with NO target, never to a commit: the two
	// sides are different kinds and the mapping must not flatten them.
	if strings.Contains(got.Right, "@") && !strings.HasSuffix(got.Right, "gg://"+linkRepoName(t, svc)) {
		t.Fatalf("@worktree mapped to a pinned target: %q", got.Right)
	}
	if !strings.Contains(out.String(), got.ID) {
		t.Fatalf("stdout did not name the id %q: %s", got.ID, out.String())
	}
}

// --list prints one "<id>\t<label>\t<left>\t<right>" line per stored
// comparison, and prints NOTHING at exit 0 when there are none — the same
// empty-output convention `gg links` follows.
func TestCompareListIsEmptyAndSilentWithNothingStored(t *testing.T) {
	svc, statePath, _ := newCompareFixture(t)
	var out, errb bytes.Buffer
	if code := cmdCompare(statePath, svc, []string{"--list"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if out.String() != "" {
		t.Fatalf("--list printed %q on an empty store, want nothing", out.String())
	}
}

// --saved re-runs a stored comparison by id or label and prints the same
// changed-file list the original invocation did.
func TestCompareSavedReRunsByIDAndLabel(t *testing.T) {
	svc, statePath, repo := newCompareFixture(t)
	var out, errb bytes.Buffer
	if code := cmdCompare(statePath, svc, []string{"--save", "mine", repo.parentSha, repo.headSha}, &out, &errb); code != 0 {
		t.Fatalf("save: exit %d, stderr: %s", code, errb.String())
	}
	direct := out.String()

	for _, spec := range []string{"mine", storedID(t, svc)} {
		out.Reset()
		errb.Reset()
		if code := cmdCompare(statePath, svc, []string{"--saved", spec}, &out, &errb); code != 0 {
			t.Fatalf("--saved %q: exit %d, stderr: %s", spec, code, errb.String())
		}
		if out.String() != direct {
			t.Fatalf("--saved %q printed:\n%s\nwant the original:\n%s", spec, out.String(), direct)
		}
	}
}

// A token that cannot be expressed as a link is refused at exit 2 rather than
// stored as something else. --save is the honest boundary.
func TestCompareSaveRefusesATokenItCannotLink(t *testing.T) {
	svc, statePath, _ := newCompareFixture(t)
	var out, errb bytes.Buffer
	code := cmdCompare(statePath, svc, []string{"--save", "x", "bookmark:nosuchid", "@worktree"}, &out, &errb)
	if code != 2 {
		t.Fatalf("exit %d, want 2; stderr: %s", code, errb.String())
	}
}

// --save and --saved together is a usage error: one writes, the other reads.
func TestCompareSaveAndSavedTogetherIsUsage(t *testing.T) {
	svc, statePath, _ := newCompareFixture(t)
	var out, errb bytes.Buffer
	if code := cmdCompare(statePath, svc, []string{"--save", "x", "--saved", "y"}, &out, &errb); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}
```

- [ ] **Step 2: Build the fixture helper**

`internal/cli` already has a compare test file; grep `func TestCompare` in `internal/cli/compare_test.go` and reuse its repo construction. Write `newCompareFixture(t *testing.T) (*domain.Service, string, fixtureRepo)` returning a service over a real repo with at least two commits, the `RepoStatePath` seam pointed at a `t.TempDir()`, `PreviewStatePath` likewise, and a `fixtureRepo{headSha, parentSha string}`. Add `storedID` and `linkRepoName` as small helpers over `svc.SavedCompareList` / the link a `gg link` call produces.

- [ ] **Step 3: Run it to make sure it fails**

Run: `go test ./internal/cli/ -run TestCompareSave -v`
Expected: FAIL — `flag provided but not defined: -save`, exit 2.

- [ ] **Step 4: Implement the token→link mapping**

Add to `internal/cli/compare.go`:

```go
// compareTokenLink maps one `gg compare` token onto the link that names the
// same place. Links are the primary spelling (spec §5.3) and the older
// vocabulary maps onto them, which is what lets a saved comparison hold two
// links whatever the user typed.
//
// buildLink is the ONE producer — a link is never assembled by string
// concatenation here, or `gg compare --save` could emit a spelling
// `gg link` would not.
func compareTokenLink(ctx context.Context, svc *domain.Service, tok string) (model.Link, error) {
	switch {
	case isLinkArg(tok):
		return model.ParseLink(tok)
	case tok == "@worktree":
		return buildLink(ctx, svc, ".", "", linkOpts{})
	case tok == "@staged", tok == "@index":
		return buildLink(ctx, svc, ".", "", linkOpts{Cached: true})
	case strings.HasPrefix(tok, "bookmark:"):
		id := strings.TrimPrefix(tok, "bookmark:")
		b, err := svc.BookmarkGet(ctx, id)
		if err != nil {
			return model.Link{}, fmt.Errorf("bookmark %q: %w", id, err)
		}
		if !b.IsCommit() {
			return model.Link{}, fmt.Errorf("bookmark %q is a file bookmark, not a commit", id)
		}
		return buildLink(ctx, svc, ".", "", linkOpts{Rev: b.Commit, Hint: model.LinkHint{Kind: "bookmark", ID: id}})
	case strings.HasPrefix(tok, "shelf:"):
		id := strings.TrimPrefix(tok, "shelf:")
		e, err := svc.ShelfFind(ctx, id)
		if err != nil {
			return model.Link{}, fmt.Errorf("shelf %q: %w", id, err)
		}
		if !e.IsCommit() {
			return model.Link{}, fmt.Errorf("shelf entry %q is a file entry, not a commit", id)
		}
		return buildLink(ctx, svc, ".", "", linkOpts{Rev: e.Origin.Commit, Hint: model.LinkHint{Kind: "shelf", ID: id}})
	default:
		return buildLink(ctx, svc, ".", "", linkOpts{Rev: tok})
	}
}
```

- [ ] **Step 5: Add the three flags to `cmdCompare`**

```go
save := fs.String("save", "", "store this comparison under `<label>`")
saved := fs.String("saved", "", "re-run the stored comparison named by `<id|label>`")
list := fs.Bool("list", false, "print the stored comparisons and exit")
```

Then, after `fs.Parse`:

```go
if *list && (*save != "" || *saved != "") {
	fmt.Fprintln(stderr, compareUsage)
	return 2
}
if *save != "" && *saved != "" {
	fmt.Fprintln(stderr, "compare: --save writes and --saved reads; use one")
	return 2
}
if *list {
	return cmdCompareList(svc, stdout, stderr)
}
```

`--saved <spec>` resolves the record and substitutes its two link texts for the positional tokens (a set-shaped record supplies `args[0]` only, and `rightTok` keeps its `@worktree` default), then falls through to the existing comparison path unchanged — so a saved comparison and its direct invocation print byte-identical output, which `TestCompareSavedReRunsByIDAndLabel` asserts.

`--save <label>` runs after the comparison SUCCEEDS, beside the existing `recordCompareLinks` calls (ruling R8: never from a failure path), maps both tokens through `compareTokenLink`, stores, and prints the id:

```go
// saveComparison stores the just-completed comparison. It runs only from
// cmdCompare's success returns, beside recordCompareLinks — a comparison that
// failed is not one the user asked to keep.
//
// Unlike recordCompareLinks this is NOT best-effort: the user asked for it in
// so many words, so a token that cannot be expressed as a link is a usage
// error (exit 2) rather than a silent skip.
func saveComparison(ctx context.Context, svc *domain.Service, label, leftTok, rightTok string, stdout, stderr io.Writer) int {
	left, err := compareTokenLink(ctx, svc, leftTok)
	if err != nil {
		fmt.Fprintf(stderr, "compare --save: %s: %v\n", leftTok, err)
		return 2
	}
	right, err := compareTokenLink(ctx, svc, rightTok)
	if err != nil {
		fmt.Fprintf(stderr, "compare --save: %s: %v\n", rightTok, err)
		return 2
	}
	c, err := svc.SavedCompareAdd(ctx, left.String(), right.String(), label)
	if errors.Is(err, domain.ErrSavedCompareExists) {
		fmt.Fprintf(stdout, "%s\t%s (already saved)\n", c.ID, c.Label)
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "compare --save:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s\t%s\n", c.ID, c.Label)
	return 0
}
```

and `cmdCompareList`:

```go
// cmdCompareList prints "<id>\t<label>\t<left>\t<right>" per stored
// comparison. A set-shaped record has an EMPTY right field, which keeps the
// column count fixed for `cut`. Nothing is printed when the store is empty —
// the convention `gg links` already follows.
func cmdCompareList(svc *domain.Service, stdout, stderr io.Writer) int {
	cs, err := svc.SavedCompareList(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, c := range cs {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", c.ID, c.Label, c.Left, c.Right)
	}
	return 0
}
```

Extend `compareUsage` to name the three flags.

- [ ] **Step 6: Run the tests and make sure they pass**

Run: `go test ./internal/cli/ -run TestCompare -v`
Expected: PASS, including every pre-existing `gg compare` test.

- [ ] **Step 7: Watch the guards fail**

| break | test that must fail |
|---|---|
| `saveComparison` is called before `CompareSets` rather than after | `TestCompareSaveRefusesATokenItCannotLink` (the store gains a row on a failed compare — add that assertion if it does not already fail) |
| `compareTokenLink`'s `@worktree` arm falls through to `linkOpts{Rev: tok}` | `TestCompareSaveStoresBothSidesAsLinks` |
| `cmdCompareList` prints a header line on an empty store | `TestCompareListIsEmptyAndSilentWithNothingStored` |
| `--saved` looks the record up but ignores its right half | `TestCompareSavedReRunsByIDAndLabel` |

- [ ] **Step 8: Verify the serial-test convention**

Run: `go test ./internal/cli/ -race -count=3`
Expected: PASS, 0 races. The new tests mutate `RepoStatePath` and `PreviewStatePath`, so they must NOT call `t.Parallel()` — plan 2 shipped an intermittent race exactly here, and one clean run is not evidence.

- [ ] **Step 9: Commit**

```bash
git add internal/cli
git commit -m "feat(cli): gg compare --save/--saved/--list"
```

---

### Task 10: e2e scenario, docs and the agent skill

**Files:**
- Create: `e2e/scenarios/s93_saved_compare.toml`
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`, `internal/agentskill/using-gg.md`, `internal/agentskill/skill.go` (the `Version` constant)

**Interfaces:**
- Consumes: the CLI surface from Task 9.
- Produces: nothing other code depends on.

- [ ] **Step 1: Write the e2e scenario**

Follow the `writing-e2e-scenarios` project skill and the shape of an existing scenario (`e2e/scenarios/s92_preview_links.toml` is the nearest neighbour). Cover, as semantic state assertions:

1. `gg compare --save "my work" <parentSha> <headSha>` exits 0 and prints an id.
2. `gg compare --list` prints exactly one line whose first field is that id and whose third and fourth fields both start with `gg://`.
3. `gg compare --saved "my work"` prints the same changed-file list as the direct invocation.
4. `gg compare --save x bookmark:nosuchid @worktree` exits **2**.
5. With a `previews.toml` seeded into the state directory before the run, `gg preview list` shows the converted preview with its original id and label, and `previews.toml` no longer exists.

Note the harness convention already documented at `e2e/scenarios/s92_preview_links.toml:69`: the harness runs in-process with `cli.LaunchTUI` nil, so a verb that would launch the TUI exits 1 there. None of the five cases above launches the TUI.

- [ ] **Step 2: Run the e2e stage**

Run: `./test.sh e2e`
Expected: PASS.

- [ ] **Step 3: Update `CHANGELOG.md`**

Add an entry describing: the `savedcompare` store; the lossless conversion of saved merge previews (naming that ids, labels and creation times are kept and preview notes are unaffected); `gg compare --save/--saved/--list`; and the migration framework generalization (a migration now names its own action, and a lossless one runs without a consent prompt).

- [ ] **Step 4: Update `README.md`**

The CLI surface changed, so document the three new `gg compare` flags wherever `gg compare` is documented.

- [ ] **Step 5: Update `internal/agentskill/using-gg.md` and bump `Version`**

Add `gg compare --save <label>`, `--saved <id|label>` and `--list` to the compare section, with the `--list` output columns (`<id>\t<label>\t<left>\t<right>`, right empty for a set) and the empty-store-prints-nothing rule. Bump `agentskill.Version` by one.

**Verify every claim against the built binary before writing it.** Plan 2 shipped two stale claims in this file that had been false since Task 2 of an earlier plan. Build (`go build ./cmd/gg`) and run each documented invocation.

- [ ] **Step 6: Update `CLAUDE.md`**

Replace the `preview` row in the package map with a `savedcompare` row — **one line**, per that file's own rule:

```
| `savedcompare`| Machine-local registry of saved comparisons: an entry is a PAIR of `gg://` links or a SET (one link — a saved merge preview). Records only, TOML + the shared file lock under XDG state; absorbs the former `preview` store, converting it losslessly on first run. Owned by `domain`; frontends never import it. |
```

Also update the `engine` row if it names `ApplyMigration`'s ref-deletion behaviour, and the `preflight` row to mention the machine-local legacy probe.

- [ ] **Step 7: Run the full gate**

Run: `./test.sh race`
Expected: PASS, 0 races, every stage.

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "docs: saved comparisons, the previews conversion, and the generalized migration"
```

- [ ] **Step 9: Report, and ASK before merging**

Report: the branch, the commit range, the full-gate result, and any finding parked rather than fixed. **Do not merge.** The user merges; the user pushes.

---

## Self-Review

**Spec coverage.**

| spec requirement | task |
|---|---|
| §4.3 `internal/savedcompare` as a DAG leaf owned by domain | 2 |
| §4.5 `Entry{ID, Left, Right, Label, Created}`, `Right == nil` ⇒ a set | 2 |
| §4.5 one ID derivation for both shapes; `Add` dedups on the pair | 2 |
| §4.5 converted entries keep their old id verbatim | 3 |
| §4.5 `model.Link` gains `MarshalText`/`UnmarshalText` | 1 |
| §4.5 the store reuses `stateBaseDir("previews")` | 7 (gate at 7 Step 9) |
| §4.5 CONVERT, not discard; lossless; preview notes unaffected | 3, 6 |
| §4.5.1 pluggable check — machine-local probe + `LegacyStore` | 5 |
| §4.5.1 pluggable action — `MigrationAction`, `DiscardRefs`, `ConvertPreviews` | 4, 6 |
| §4.5.1 `Migration.Lossless`; lossless runs without asking | 5, 6 |
| §4.5.1 `RunAutoMigrations` from the composition root | 6 |
| §5.3 `gg compare --save <label>` | 9 |
| §5.3 the old vocabulary maps onto links | 9 (`compareTokenLink`) |
| §8 row 3a: a domain façade keeps every frontend compiling untouched | 7 |

**Not in this plan, by design (and not gaps):** the TUI copy rows, the `#` history picker, the compare palette and the shared base picker (§5.1) are plan **3b**; the web dialog and the Previews tab listing saved comparisons are plan **3c**. The Previews tab keeps showing merge previews only, through the unchanged façade, until 3c.

**Placeholder scan.** No "TBD"/"TODO"/"handle edge cases" steps. Three steps direct the implementer to read an existing file before copying its pattern (Task 2 Step 4, Task 7 Step 3, Task 9 Step 2) rather than reproducing a hundred lines the repository already holds — each names the exact file and the exact properties to preserve.

**Type consistency.** `savedcompare.Entry` / `ID(left, right string)` / `IsSet()` are defined in Task 2 and used under those names in 3, 6, 7 and 8. `MigrationAction` / `DiscardRefs` / `ConvertPreviews` are defined in 4 and 6 and used in 6. `LegacyProbe` / `LegacyStore` / `Migration.Action` / `Migration.Lossless` are defined in 5 and used in 6. `savedCompareDir` is defined in Task 6 and reused unchanged by Task 7's `savedCompareStore`, so the tasks run in order with no forward reference. `SavedCompareAdd/List/Get/Remove` are defined in 8 and used in 9 and in one Task 7 test.
