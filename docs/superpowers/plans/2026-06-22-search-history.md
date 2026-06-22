# Search History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remember Enter-confirmed search phrases per window and let the user recall them while typing a new search via an Alt+↓/↑ scrollable dropdown (max 10 rows visible).

**Architecture:** A new domain-owned side store (`internal/searchhist`, the `bookmark` record-store mold) persists per-repo TOML rings keyed by scope. The `domain.Service` resolves it lazily per-repo and exposes `RecordSearch`/`SearchHistoryAll`. The TUI holds the rings in memory (loaded at startup), records on Enter, and drives a shared recall dropdown across the five search typing loops. No engine `Operation`, no CLI surface.

**Tech Stack:** Go 1.26, `github.com/pelletier/go-toml/v2`, Bubble Tea, lipgloss.

## Global Constraints

- A git verb is one invocation — but this feature adds **no** git verbs (side store only).
- `internal/tui` and `internal/cli` must **never** import `internal/searchhist` — reach it only through `internal/domain` (archtest-guarded).
- Per-repo state keyed by a **hash of the git common dir** (`repoKey` in `internal/domain/shelfstore.go`), under `<state>/gg/search/<key>`.
- Hard ceiling on per-ring entries: `searchhist.MaxSize = 1000`. Config default size = **20**. `<= 0` config → 20; `> 1000` → clamp to 1000.
- Record on **Enter only**, **non-empty only**, **dedup-to-top**.
- Scopes (ring keys): `panel` (main `/` filter + `@` highlight share it), `filetree` (files-view tree `/`), `bookmark` (`g` switcher `/`), `shelf` (`G` switcher `/`). The `.` action-menu `/` is **excluded**.
- TDD throughout; finish each task with `gofmt -l`, `go vet`, `go test`.
- Spec: `docs/superpowers/specs/2026-06-22-search-history-design.md`.

---

### Task 1: `internal/searchhist` store package

**Files:**
- Create: `internal/searchhist/store.go`
- Create: `internal/searchhist/file_store.go`
- Test: `internal/searchhist/file_store_test.go`

**Interfaces:**
- Produces:
  - `searchhist.MaxSize` (const `= 1000`)
  - `searchhist.Store` interface: `All() map[string][]string` and `Record(scope, phrase string, size int) error`
  - `searchhist.NewFileStore(root string) *FileStore`

- [ ] **Step 1: Write the failing test**

Create `internal/searchhist/file_store_test.go`:

```go
package searchhist

import (
	"path/filepath"
	"testing"
)

func newStore(t *testing.T) *FileStore {
	t.Helper()
	return NewFileStore(t.TempDir())
}

func TestRecordNewestFirstAndDedupToTop(t *testing.T) {
	s := newStore(t)
	for _, p := range []string{"alpha", "beta", "gamma"} {
		if err := s.Record("panel", p, 20); err != nil {
			t.Fatalf("Record(%q): %v", p, err)
		}
	}
	// Re-record an existing phrase: moves to top, no duplicate.
	if err := s.Record("panel", "alpha", 20); err != nil {
		t.Fatal(err)
	}
	got := s.All()["panel"]
	want := []string{"alpha", "gamma", "beta"}
	if len(got) != len(want) {
		t.Fatalf("ring = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ring = %v, want %v", got, want)
		}
	}
}

func TestRecordEmptyIsNoop(t *testing.T) {
	s := newStore(t)
	if err := s.Record("panel", "", 20); err != nil {
		t.Fatal(err)
	}
	if err := s.Record("panel", "   ", 20); err != nil {
		t.Fatal(err)
	}
	if got := s.All()["panel"]; len(got) != 0 {
		t.Fatalf("empty/blank phrase must not record, got %v", got)
	}
}

func TestRecordTrimsToSize(t *testing.T) {
	s := newStore(t)
	for _, p := range []string{"a", "b", "c", "d"} {
		if err := s.Record("panel", p, 2); err != nil {
			t.Fatal(err)
		}
	}
	got := s.All()["panel"]
	if len(got) != 2 || got[0] != "d" || got[1] != "c" {
		t.Fatalf("ring = %v, want [d c] (trimmed to 2, newest-first)", got)
	}
}

func TestRecordClampsSizeToMax(t *testing.T) {
	s := newStore(t)
	if err := s.Record("panel", "x", MaxSize+500); err != nil {
		t.Fatal(err)
	}
	// Can't easily fill 1000, but the size argument must not panic/overflow and
	// the single entry survives.
	if got := s.All()["panel"]; len(got) != 1 || got[0] != "x" {
		t.Fatalf("ring = %v, want [x]", got)
	}
}

func TestScopesAreIndependentAndPersist(t *testing.T) {
	root := t.TempDir()
	s1 := NewFileStore(root)
	if err := s1.Record("panel", "p1", 20); err != nil {
		t.Fatal(err)
	}
	if err := s1.Record("bookmark", "b1", 20); err != nil {
		t.Fatal(err)
	}
	// A fresh store over the same root reads what s1 wrote.
	s2 := NewFileStore(root)
	all := s2.All()
	if len(all["panel"]) != 1 || all["panel"][0] != "p1" {
		t.Fatalf("panel = %v, want [p1]", all["panel"])
	}
	if len(all["bookmark"]) != 1 || all["bookmark"][0] != "b1" {
		t.Fatalf("bookmark = %v, want [b1]", all["bookmark"])
	}
}

func TestRecordReadMergesConcurrentSibling(t *testing.T) {
	root := t.TempDir()
	a := NewFileStore(root)
	b := NewFileStore(root)
	if err := a.Record("panel", "from-a", 20); err != nil {
		t.Fatal(err)
	}
	// b records without having seen a's write in memory: read-merge must keep both.
	if err := b.Record("panel", "from-b", 20); err != nil {
		t.Fatal(err)
	}
	got := NewFileStore(root).All()["panel"]
	if len(got) != 2 || got[0] != "from-b" || got[1] != "from-a" {
		t.Fatalf("ring = %v, want [from-b from-a] (read-merge kept the sibling)", got)
	}
}

func TestAllOnMissingFileIsEmpty(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "nope"))
	if got := s.All(); len(got) != 0 {
		t.Fatalf("missing file should yield empty map, got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gg-searchhist && go test ./internal/searchhist/`
Expected: FAIL — package/types not defined.

- [ ] **Step 3: Write `store.go`**

Create `internal/searchhist/store.go`:

```go
// Package searchhist is gigagit's per-repo store of recent search phrases
// ("search history"): named rings of strings, newest-first. The Store interface
// is the fixed API; the file-backed implementation is swappable. It holds no git
// state — frontends reach it only through internal/domain.
package searchhist

// MaxSize is the hard ceiling on entries kept per ring, regardless of config.
const MaxSize = 1000

// Store persists per-scope history rings for one repo. Safe for sequential use
// by one process; cross-process writes read-merge then atomically rewrite, so
// the common interleaved case does not lose a sibling's entries.
type Store interface {
	// All returns every ring, newest-first, keyed by scope. Empty map when none.
	All() map[string][]string
	// Record prepends phrase to scope's ring (dedup-to-top), trims to size
	// (capped at MaxSize), and persists. No-op when phrase is empty/blank.
	Record(scope, phrase string, size int) error
}
```

- [ ] **Step 4: Write `file_store.go`**

Create `internal/searchhist/file_store.go`:

```go
package searchhist

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// FileStore keeps an atomic-rewrite TOML file under root/search.toml.
type FileStore struct{ root string }

// NewFileStore roots a store at the per-repo directory (caller-supplied).
func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

// rings is the on-disk shape: scope -> phrases (newest-first).
type rings struct {
	Rings map[string][]string `toml:"rings"`
}

func (fs *FileStore) path() string { return filepath.Join(fs.root, "search.toml") }

func (fs *FileStore) read() rings {
	var r rings
	data, err := os.ReadFile(fs.path())
	if err != nil {
		return rings{Rings: map[string][]string{}}
	}
	if err := toml.Unmarshal(data, &r); err != nil || r.Rings == nil {
		return rings{Rings: map[string][]string{}}
	}
	return r
}

// All returns a copy of every ring.
func (fs *FileStore) All() map[string][]string {
	src := fs.read().Rings
	out := make(map[string][]string, len(src))
	for k, v := range src {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// Record prepends phrase (dedup-to-top), trims to the effective size, and
// rewrites the file after read-merging the on-disk state.
func (fs *FileStore) Record(scope, phrase string, size int) error {
	phrase = strings.TrimSpace(phrase)
	if phrase == "" {
		return nil
	}
	if size <= 0 {
		size = 20
	}
	if size > MaxSize {
		size = MaxSize
	}
	r := fs.read() // read-merge: pick up any sibling writes first
	ring := r.Rings[scope]
	merged := make([]string, 0, len(ring)+1)
	merged = append(merged, phrase)
	for _, p := range ring {
		if p != phrase { // dedup-to-top
			merged = append(merged, p)
		}
	}
	if len(merged) > size {
		merged = merged[:size]
	}
	r.Rings[scope] = merged
	return fs.write(r)
}

// write persists r via temp-file + rename (the seq-state / bookmark pattern).
func (fs *FileStore) write(r rings) error {
	if err := os.MkdirAll(fs.root, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(r)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "search-*.toml")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, fs.path()); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /mnt/t/others/gg-searchhist && go test ./internal/searchhist/`
Expected: PASS (all tests).

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gg-searchhist
gofmt -w internal/searchhist/
git add internal/searchhist/
git commit -m "feat(searchhist): per-repo search-history store (TOML rings)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 2: config field + effective-size helper

**Files:**
- Modify: `internal/config/config.go` (UIConfig struct ~line 22, overlayUI ~line 107)
- Create: `internal/domain/searchhist_size.go`
- Test: `internal/config/config_test.go` (add cases), `internal/domain/searchhist_size_test.go`

**Interfaces:**
- Consumes: `searchhist.MaxSize` (Task 1).
- Produces:
  - `config.UIConfig.SearchHistorySize int` (`toml:"search_history_size"`)
  - `domain.DefaultSearchHistorySize` (const `= 20`)
  - `domain.EffectiveSearchHistorySize(raw int) int`

- [ ] **Step 1: Write the failing test (config overlay)**

Add to `internal/config/config_test.go` (new test func):

```go
func TestOverlaySearchHistorySize(t *testing.T) {
	dst := UIConfig{SearchHistorySize: 0}
	overlayUI(&dst, UIConfig{SearchHistorySize: 50})
	if dst.SearchHistorySize != 50 {
		t.Fatalf("SearchHistorySize = %d, want 50", dst.SearchHistorySize)
	}
	// <= 0 in src must not reset a set dst (unset rule).
	overlayUI(&dst, UIConfig{SearchHistorySize: 0})
	if dst.SearchHistorySize != 50 {
		t.Fatalf("zero src must not reset, got %d", dst.SearchHistorySize)
	}
}
```

- [ ] **Step 2: Write the failing test (effective size)**

Create `internal/domain/searchhist_size_test.go`:

```go
package domain

import "testing"

func TestEffectiveSearchHistorySize(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, DefaultSearchHistorySize},   // unset -> default 20
		{-3, DefaultSearchHistorySize},  // negative -> default
		{5, 5},                          // in range
		{1000, 1000},                    // at ceiling
		{5000, 1000},                    // above ceiling -> clamp
	}
	for _, c := range cases {
		if got := EffectiveSearchHistorySize(c.in); got != c.want {
			t.Fatalf("EffectiveSearchHistorySize(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /mnt/t/others/gg-searchhist && go test ./internal/config/ ./internal/domain/ 2>&1 | head -20`
Expected: FAIL — `SearchHistorySize` and `EffectiveSearchHistorySize` undefined.

- [ ] **Step 4: Add the config field**

In `internal/config/config.go`, add to `UIConfig` (after `MenuActions`, before the CommitGraph block):

```go
	SearchHistorySize int `toml:"search_history_size"` // entries kept per search-history ring; <=0 = unset (default 20), clamped to searchhist.MaxSize
```

And in `overlayUI`, add (alongside the other `> 0` guards):

```go
	if src.SearchHistorySize > 0 {
		dst.SearchHistorySize = src.SearchHistorySize
	}
```

- [ ] **Step 5: Add the effective-size helper**

Create `internal/domain/searchhist_size.go`:

```go
package domain

import "github.com/gigagit/gg/internal/searchhist"

// DefaultSearchHistorySize is the per-ring entry count when config leaves it unset.
const DefaultSearchHistorySize = 20

// EffectiveSearchHistorySize maps a raw config value to the size actually used:
// <=0 falls back to the default, anything above the hard ceiling clamps down.
func EffectiveSearchHistorySize(raw int) int {
	if raw <= 0 {
		return DefaultSearchHistorySize
	}
	if raw > searchhist.MaxSize {
		return searchhist.MaxSize
	}
	return raw
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd /mnt/t/others/gg-searchhist && go test ./internal/config/ ./internal/domain/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gg-searchhist
gofmt -w internal/config/ internal/domain/
git add internal/config/ internal/domain/searchhist_size.go internal/domain/searchhist_size_test.go
git commit -m "feat(config): search_history_size + EffectiveSearchHistorySize clamp

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 3: domain store resolution + commands + archtest guard

**Files:**
- Create: `internal/domain/searchstore.go`
- Test: `internal/domain/searchstore_test.go`
- Modify: `internal/domain/service.go` (add `searchhist` field ~line 36)
- Modify: `internal/archtest/import_guard_test.go` (~line 16)

**Interfaces:**
- Consumes: `searchhist.Store`/`NewFileStore` (Task 1), `EffectiveSearchHistorySize` (Task 2), `repoKey` (existing, `shelfstore.go`).
- Produces:
  - `(*Service).SetSearchStore(st searchhist.Store)`
  - `(*Service).RecordSearch(ctx, scope, phrase string, rawSize int)`
  - `(*Service).SearchHistoryAll(ctx) map[string][]string`
  - `domain.SearchStatePath` (override var)

- [ ] **Step 1: Add the Service field**

In `internal/domain/service.go`, in the `Service` struct (after the `bookmark` field at ~line 37):

```go
	searchhist searchhist.Store // lazily resolved; nil disables search history
```

Add the import `"github.com/gigagit/gg/internal/searchhist"` to `service.go`'s import block.

- [ ] **Step 2: Write the failing test**

Create `internal/domain/searchstore_test.go`:

```go
package domain

import (
	"context"
	"testing"

	"github.com/gigagit/gg/internal/searchhist"
)

func TestRecordSearchAndSearchHistoryAll(t *testing.T) {
	s := New(nil)
	s.SetSearchStore(searchhist.NewFileStore(t.TempDir()))
	ctx := context.Background()

	s.RecordSearch(ctx, "panel", "fix login", 20)
	s.RecordSearch(ctx, "panel", "TODO", 20)
	s.RecordSearch(ctx, "bookmark", "handler", 20)

	all := s.SearchHistoryAll(ctx)
	if got := all["panel"]; len(got) != 2 || got[0] != "TODO" || got[1] != "fix login" {
		t.Fatalf("panel ring = %v, want [TODO, fix login]", got)
	}
	if got := all["bookmark"]; len(got) != 1 || got[0] != "handler" {
		t.Fatalf("bookmark ring = %v, want [handler]", got)
	}
}

func TestRecordSearchClampsSize(t *testing.T) {
	s := New(nil)
	s.SetSearchStore(searchhist.NewFileStore(t.TempDir()))
	ctx := context.Background()
	// rawSize 5000 must clamp to 1000; record one phrase, no panic.
	s.RecordSearch(ctx, "panel", "x", 5000)
	if got := s.SearchHistoryAll(ctx)["panel"]; len(got) != 1 || got[0] != "x" {
		t.Fatalf("ring = %v, want [x]", got)
	}
}

func TestSearchHistoryAllNilStoreIsEmpty(t *testing.T) {
	s := New(nil)
	s.SetSearchStore(nil)
	// Force the nil branch: no state path resolvable in test → store stays nil.
	SearchStatePath = ""
	if got := s.SearchHistoryAll(context.Background()); got == nil {
		t.Fatal("SearchHistoryAll must return a non-nil (possibly empty) map")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd /mnt/t/others/gg-searchhist && go test ./internal/domain/ -run TestRecordSearch 2>&1 | head`
Expected: FAIL — `SetSearchStore`/`RecordSearch`/`SearchHistoryAll` undefined.

- [ ] **Step 4: Write `searchstore.go`**

Create `internal/domain/searchstore.go` (mirrors `bookmarkstore.go`):

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gigagit/gg/internal/searchhist"
)

// SearchStatePath overrides the search-history root dir. "" uses the default XDG
// location. cmd/gg leaves it ""; tests point it at a temp dir.
var SearchStatePath string

// SetSearchStore injects a store (tests).
func (s *Service) SetSearchStore(st searchhist.Store) {
	s.mu.Lock()
	s.searchhist = st
	s.mu.Unlock()
}

// searchStore resolves (once) the per-repo history store, keyed by git common
// dir under the XDG state dir. Returns nil (history disabled) when no state dir
// is resolvable — mirroring the shelf/bookmark posture.
func (s *Service) searchStore(ctx context.Context) searchhist.Store {
	s.mu.Lock()
	if s.searchhist != nil {
		st := s.searchhist
		s.mu.Unlock()
		return st
	}
	s.mu.Unlock()

	root := SearchStatePath
	if root == "" {
		base := searchBaseDir()
		if base == "" {
			return nil
		}
		key := "unknown"
		if cd, err := s.GitCommonDir(ctx); err == nil {
			key = repoKey(strings.TrimSpace(cd)) // reuse shelfstore.go's repoKey
		}
		root = filepath.Join(base, key)
	}
	st := searchhist.NewFileStore(root)
	s.mu.Lock()
	s.searchhist = st
	s.mu.Unlock()
	return st
}

// searchBaseDir resolves <state>/gg/search cross-platform (mirrors shelfBaseDir).
func searchBaseDir() string {
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "search")
		}
	}
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "search")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "search")
}

// RecordSearch appends an Enter-confirmed phrase to scope's ring. Best-effort:
// a nil store (history disabled) or a write error is silently ignored, like the
// other side stores. rawSize is the unclamped config value.
func (s *Service) RecordSearch(ctx context.Context, scope, phrase string, rawSize int) {
	st := s.searchStore(ctx)
	if st == nil {
		return
	}
	_ = st.Record(scope, phrase, EffectiveSearchHistorySize(rawSize))
}

// SearchHistoryAll returns every ring (newest-first), or an empty map when
// history is disabled.
func (s *Service) SearchHistoryAll(ctx context.Context) map[string][]string {
	st := s.searchStore(ctx)
	if st == nil {
		return map[string][]string{}
	}
	return st.All()
}
```

- [ ] **Step 5: Add the archtest guard**

In `internal/archtest/import_guard_test.go`, add to the `forbidden` map (after the bookmark line ~17):

```go
		"github.com/gigagit/gg/internal/searchhist": "frontends must reach the search-history store through internal/domain",
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd /mnt/t/others/gg-searchhist && go test ./internal/domain/ ./internal/archtest/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gg-searchhist
gofmt -w internal/domain/ internal/archtest/
git add internal/domain/searchstore.go internal/domain/searchstore_test.go internal/domain/service.go internal/archtest/import_guard_test.go
git commit -m "feat(domain): per-repo search-history resolve + RecordSearch/SearchHistoryAll

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 4: TUI startup load + record-on-Enter (all 5 sites)

This delivers persistence end-to-end: history is loaded at startup and every Enter-confirmed search is recorded. No recall navigation / dropdown yet.

**Files:**
- Modify: `internal/tui/model.go` (Model fields; load command/msg; record helper; filter+highlight Enter sites)
- Modify: `internal/tui/load.go` (dispatch the load command at startup)
- Modify: `internal/tui/content_popup.go` (files-view Enter)
- Modify: `internal/tui/bookmark_popup.go` (Enter)
- Modify: `internal/tui/shelf_popup.go` (Enter)
- Test: `internal/tui/search_history_test.go` (new)

**Interfaces:**
- Consumes: `domain.Service.RecordSearch`, `domain.Service.SearchHistoryAll`, `domain.EffectiveSearchHistorySize`, `m.cfg.UI.SearchHistorySize`.
- Produces (used by Task 5 & 6):
  - scope constants `scopePanel`, `scopeFiletree`, `scopeBookmark`, `scopeShelf` (string)
  - `Model.searchHist map[string][]string`
  - `(Model).searchHistorySize() int`
  - `(Model).recordSearch(scope, phrase string) (Model, tea.Cmd)` — updates the in-memory ring (dedup-to-top, trim) and returns a fire-and-forget persist cmd.
  - `searchHistLoadedMsg{ rings map[string][]string }`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/search_history_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/searchhist"
)

// withSearchStore wires a temp-dir store into a loaded model's service.
func withSearchStore(t *testing.T, m Model) Model {
	t.Helper()
	m.svc.SetSearchStore(searchhist.NewFileStore(t.TempDir()))
	return m
}

func TestPanelFilterEnterRecords(t *testing.T) {
	m := withSearchStore(t, loadedModel(t))
	m.focus = panelBranches
	m, _ = update(t, m, keyMsg("/")) // start filter
	m, _ = typeRunes(t, m, "feat")
	m, cmd := update(t, m, keyMsg("enter")) // commit -> record
	drainCmd(t, cmd)                        // run the persist cmd
	if got := m.searchHist[scopePanel]; len(got) == 0 || got[0] != "feat" {
		t.Fatalf("panel ring = %v, want newest 'feat'", got)
	}
}

func TestFilterEscDoesNotRecord(t *testing.T) {
	m := withSearchStore(t, loadedModel(t))
	m.focus = panelBranches
	m, _ = update(t, m, keyMsg("/"))
	m, _ = typeRunes(t, m, "junk")
	m, _ = update(t, m, keyMsg("esc")) // cancel -> no record
	if got := m.searchHist[scopePanel]; len(got) != 0 {
		t.Fatalf("esc must not record, got %v", got)
	}
}

func TestHighlightSharesPanelRing(t *testing.T) {
	m := withSearchStore(t, loadedModel(t))
	m.focus = panelCommits
	m, _ = update(t, m, keyMsg("@")) // start highlight
	m, _ = typeRunes(t, m, "bugfix")
	m, cmd := update(t, m, keyMsg("enter"))
	drainCmd(t, cmd)
	// @ records into the SAME ring as /.
	if got := m.searchHist[scopePanel]; len(got) == 0 || got[0] != "bugfix" {
		t.Fatalf("@ must share the panel ring, got %v", got)
	}
}

func TestStartupLoadPopulatesRings(t *testing.T) {
	store := searchhist.NewFileStore(t.TempDir())
	_ = store.Record(scopePanel, "preexisting", 20)
	m := loadedModel(t)
	m.svc.SetSearchStore(store)
	m, cmd := m.Update(loadSearchHistCmd(m.svc)())
	_ = cmd
	if got := m.searchHist[scopePanel]; len(got) != 1 || got[0] != "preexisting" {
		t.Fatalf("startup load ring = %v, want [preexisting]", got)
	}
}

var _ = domain.DefaultSearchHistorySize // keep import if unused elsewhere
```

> **Note for implementer:** check `internal/tui` for the existing test helpers `update`, `typeRunes`, `drainCmd`, `keyMsg`, `loadedModel`. If a helper with a different name exists (e.g. `pressRune`/`driveOp`), use that instead — match the established names in `filter_test.go` / `decision_integration_test.go`. The assertions above are the contract; adapt only the helper calls.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gg-searchhist && go test ./internal/tui/ -run TestPanelFilterEnterRecords 2>&1 | head`
Expected: FAIL — `searchHist`, `scopePanel`, `loadSearchHistCmd` undefined.

- [ ] **Step 3: Add Model fields + scope constants + helpers**

In `internal/tui/model.go`, add to the `Model` struct (near `filterPanel`/`filterQuery`):

```go
	searchHist map[string][]string // per-scope recall rings, newest-first (loaded at startup)
```

Add a new block (top of `model.go`, near other consts, or in a new `internal/tui/search_history.go` — implementer's choice; this plan assumes `search_history.go`):

Create `internal/tui/search_history.go`:

```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gigagit/gg/internal/domain"
)

// Search-history ring scopes. The panel filter and @ highlight share scopePanel.
const (
	scopePanel    = "panel"
	scopeFiletree = "filetree"
	scopeBookmark = "bookmark"
	scopeShelf    = "shelf"
)

// searchHistLoadedMsg carries the rings read once at startup.
type searchHistLoadedMsg struct{ rings map[string][]string }

// loadSearchHistCmd reads every ring from the per-repo store (off the UI goroutine).
func loadSearchHistCmd(svc *domain.Service) tea.Cmd {
	return func() tea.Msg {
		return searchHistLoadedMsg{rings: svc.SearchHistoryAll(context.Background())}
	}
}

// searchHistorySize is the effective per-ring cap from config (default 20, ≤1000).
func (m Model) searchHistorySize() int {
	return domain.EffectiveSearchHistorySize(m.cfg.UI.SearchHistorySize)
}

// recordSearch updates the in-memory ring (dedup-to-top, trim) and returns a
// fire-and-forget persist command. Empty/blank phrases are a no-op.
func (m Model) recordSearch(scope, phrase string) (Model, tea.Cmd) {
	if trimmed := trimSpace(phrase); trimmed == "" {
		return m, nil
	} else {
		phrase = trimmed
	}
	if m.searchHist == nil {
		m.searchHist = map[string][]string{}
	}
	ring := m.searchHist[scope]
	merged := make([]string, 0, len(ring)+1)
	merged = append(merged, phrase)
	for _, p := range ring {
		if p != phrase {
			merged = append(merged, p)
		}
	}
	if n := m.searchHistorySize(); len(merged) > n {
		merged = merged[:n]
	}
	// copy the map so the value-receiver Model mutation is visible to the caller
	next := make(map[string][]string, len(m.searchHist))
	for k, v := range m.searchHist {
		next[k] = v
	}
	next[scope] = merged
	m.searchHist = next

	svc := m.svc
	rawSize := m.cfg.UI.SearchHistorySize
	cmd := func() tea.Msg {
		svc.RecordSearch(context.Background(), scope, phrase, rawSize)
		return nil
	}
	return m, cmd
}
```

> **Implementer note:** `trimSpace` — use `strings.TrimSpace` directly (add the `strings` import) rather than a wrapper; the wrapper name above is illustrative. Adjust to: `if phrase = strings.TrimSpace(phrase); phrase == "" { return m, nil }`.

- [ ] **Step 4: Dispatch the load at startup**

In `internal/tui/load.go`, find where the initial data-load commands are batched (the `Init`/first-load `tea.Batch`). Add `loadSearchHistCmd(m.svc)` to that batch so the rings load alongside the snapshot. Then handle the message — in `model.go`'s `Update` (near the other `*LoadedMsg` cases):

```go
	case searchHistLoadedMsg:
		if msg.rings != nil {
			m.searchHist = msg.rings
		}
		return m, nil
```

- [ ] **Step 5: Hook record at the panel-filter Enter site**

In `internal/tui/model.go`, in the `filterTyping` switch, the `tea.KeyEnter` case (~line 455). Currently:

```go
			case tea.KeyEnter:
				m.filterTyping = false // commit: filter stays active
				if m.filesView != nil && m.filterPanel == panelCommits {
					return m.syncFilesViewToSelectedCommit()
				}
```

Change to capture and record the query, returning the persist cmd:

```go
			case tea.KeyEnter:
				m.filterTyping = false // commit: filter stays active
				m, recCmd := m.recordSearch(scopePanel, m.filterQuery)
				if m.filesView != nil && m.filterPanel == panelCommits {
					mm, cmd := m.syncFilesViewToSelectedCommit()
					return mm, tea.Batch(recCmd, cmd)
				}
				return m, recCmd
```

(The existing `return m, nil` at the end of the filter switch handles other keys; the explicit returns above replace the Enter fall-through.)

- [ ] **Step 6: Hook record at the @-highlight Enter site**

In `internal/tui/model.go`, in the `highlightTyping` switch, the `tea.KeyEnter` case (~line 507):

```go
			case tea.KeyEnter:
				m.highlightTyping = false // commit: highlight stays active
```

Change to:

```go
			case tea.KeyEnter:
				m.highlightTyping = false // commit: highlight stays active
				var recCmd tea.Cmd
				m, recCmd = m.recordSearch(scopePanel, m.highlightQuery)
				return m, recCmd
```

(Remove the bare `return m, nil` reliance for this case by returning here; keep the switch's trailing `return m, nil` for the other cases.)

- [ ] **Step 7: Hook record at the three popup Enter sites**

In `internal/tui/content_popup.go`, find the files-view search-commit handling (the Enter/commit of `p.typing`). At the point the query is committed, add — guarded so it only records for the files-view instance:

```go
	// (files-view search commit) record into the filetree ring
	if m.filesView == p {
		var recCmd tea.Cmd
		m, recCmd = m.recordSearch(scopeFiletree, p.query)
		// fold recCmd into the returned tea.Cmd (tea.Batch with any existing cmd)
		_ = recCmd
	}
```

> **Implementer note:** `content_popup.go`'s update is a method whose receiver may be the popup, not `Model`. Inspect the actual signature: the records must run where `Model` (`m`) and the committed `p.query` are both in scope — most likely the dispatch site in `files_view.go`/`model.go` that calls into the popup, OR thread the cmd back. Place the `recordSearch(scopeFiletree, query)` call at whichever site has `m` and the committed query, and `tea.Batch` its cmd into the return. The contract: committing a files-view tree search records into `scopeFiletree`.

In `internal/tui/bookmark_popup.go`, at the Enter/commit of the bookmark filter (where `p.query` is finalized as the active filter — NOT where an entry is selected), record into `scopeBookmark`:

```go
		var recCmd tea.Cmd
		m, recCmd = m.recordSearch(scopeBookmark, p.query)
		return m, tea.Batch(existingCmd, recCmd)
```

In `internal/tui/shelf_popup.go`, the same at its filter-commit site into `scopeShelf`.

> **Implementer note:** bookmark/shelf popups use `/` to *start* filtering and Enter may mean "open the selected entry" rather than "commit the filter". Record the phrase when the user confirms the filter text. If the popup has no distinct "commit filter" Enter (Enter always opens an entry), record `p.query` at that same Enter (the phrase they searched with), still gated on non-empty. Match the spec intent: a confirmed, non-empty search phrase is remembered.

- [ ] **Step 8: Run tests to verify they pass**

Run: `cd /mnt/t/others/gg-searchhist && go test ./internal/tui/ -run 'TestPanelFilterEnterRecords|TestFilterEscDoesNotRecord|TestHighlightSharesPanelRing|TestStartupLoadPopulatesRings' 2>&1 | tail -10`
Expected: PASS.

- [ ] **Step 9: Run the full tui suite (no regressions)**

Run: `cd /mnt/t/others/gg-searchhist && go test ./internal/tui/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
cd /mnt/t/others/gg-searchhist
gofmt -w internal/tui/
git add internal/tui/
git commit -m "feat(tui): record Enter-confirmed searches per scope + startup load

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 5: recall navigation engine (Alt+↓/↑, draft, preview) — no rendering

**Files:**
- Modify: `internal/tui/search_history.go` (recall state + `recallUpdate`)
- Modify: `internal/tui/model.go` (filter & highlight typing loops call `recallUpdate` first)
- Modify: `internal/tui/content_popup.go`, `bookmark_popup.go`, `shelf_popup.go` (call `recallUpdate`)
- Test: `internal/tui/search_history_test.go` (add cases)

**Interfaces:**
- Consumes: `Model.searchHist`, scope constants, `recordSearch` (Task 4).
- Produces:
  - `Model.recallScope string`, `Model.recallOpen bool`, `Model.recallIndex int`, `Model.recallDraft string`
  - `(Model).recallUpdate(scope string, msg tea.KeyMsg, curQuery string) (next Model, newQuery string, handled bool, commit bool)`
  - `(Model).recallReset() Model`

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/search_history_test.go`:

```go
func seedPanelRing(m Model, phrases ...string) Model {
	m.searchHist = map[string][]string{scopePanel: phrases} // newest-first
	return m
}

func TestRecallAltDownOpensNewest(t *testing.T) {
	m := seedPanelRing(loadedModel(t), "newest", "older", "oldest")
	m.focus = panelBranches
	m, _ = update(t, m, keyMsg("/"))
	m, _ = typeRunes(t, m, "dr")          // draft "dr"
	m, _ = update(t, m, keyMsg("alt+down")) // open on newest
	if !m.recallOpen || m.filterQuery != "newest" {
		t.Fatalf("alt+down should open & preview newest; open=%v q=%q", m.recallOpen, m.filterQuery)
	}
	m, _ = update(t, m, keyMsg("alt+down")) // -> older
	if m.filterQuery != "older" {
		t.Fatalf("second alt+down -> older, got %q", m.filterQuery)
	}
}

func TestRecallAltUpAboveNewestRestoresDraft(t *testing.T) {
	m := seedPanelRing(loadedModel(t), "newest", "older")
	m.focus = panelBranches
	m, _ = update(t, m, keyMsg("/"))
	m, _ = typeRunes(t, m, "dr")
	m, _ = update(t, m, keyMsg("alt+down")) // open, q="newest"
	m, _ = update(t, m, keyMsg("alt+up"))   // above newest -> close + restore draft
	if m.recallOpen || m.filterQuery != "dr" {
		t.Fatalf("alt+up above newest restores draft & closes; open=%v q=%q", m.recallOpen, m.filterQuery)
	}
}

func TestRecallEscRestoresDraftKeepsTyping(t *testing.T) {
	m := seedPanelRing(loadedModel(t), "newest")
	m.focus = panelBranches
	m, _ = update(t, m, keyMsg("/"))
	m, _ = typeRunes(t, m, "dr")
	m, _ = update(t, m, keyMsg("alt+down")) // open
	m, _ = update(t, m, keyMsg("esc"))      // close, restore draft, STILL typing
	if m.recallOpen || !m.filterTyping || m.filterQuery != "dr" {
		t.Fatalf("esc in dropdown: open=%v typing=%v q=%q", m.recallOpen, m.filterTyping, m.filterQuery)
	}
}

func TestRecallTypingClosesDropdown(t *testing.T) {
	m := seedPanelRing(loadedModel(t), "newest")
	m.focus = panelBranches
	m, _ = update(t, m, keyMsg("/"))
	m, _ = update(t, m, keyMsg("alt+down")) // open, q="newest"
	m, _ = typeRunes(t, m, "x")             // typing closes; appends to current query
	if m.recallOpen {
		t.Fatalf("typing must close the dropdown")
	}
	if m.filterQuery != "newestx" {
		t.Fatalf("typing appends to the previewed query, got %q", m.filterQuery)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gg-searchhist && go test ./internal/tui/ -run TestRecall 2>&1 | head`
Expected: FAIL — recall fields/`recallUpdate` undefined.

- [ ] **Step 3: Add recall state + `recallUpdate`**

In `internal/tui/search_history.go`, add the fields to a small struct or directly to `Model`. This plan adds them to `Model` (in `model.go`):

```go
	recallScope string // active recall ring; "" = none
	recallOpen  bool   // dropdown visible
	recallIndex int    // highlight into the ring; 0 = newest (meaningful when recallOpen)
	recallDraft string // text captured when the dropdown opened
```

Add to `search_history.go`:

```go
import tea "github.com/charmbracelet/bubbletea" // (merge into existing import block)

// recallReset clears recall state (called when a typing mode opens/closes).
func (m Model) recallReset() Model {
	m.recallScope = ""
	m.recallOpen = false
	m.recallIndex = 0
	m.recallDraft = ""
	return m
}

// recallUpdate processes a key for the search-history dropdown of scope.
//   - next:      updated model (recall state mutated)
//   - newQuery:  the query string the caller should now display (== curQuery when unchanged)
//   - handled:   true if the caller must NOT run its normal handling for this key
//   - commit:    true if Enter accepted an entry (caller runs its commit path on newQuery)
func (m Model) recallUpdate(scope string, msg tea.KeyMsg, curQuery string) (Model, string, bool, bool) {
	ring := m.searchHist[scope]
	altDown := msg.Alt && msg.Type == tea.KeyDown
	altUp := msg.Alt && msg.Type == tea.KeyUp

	if !m.recallOpen {
		if altDown && len(ring) > 0 {
			m.recallScope = scope
			m.recallOpen = true
			m.recallIndex = 0
			m.recallDraft = curQuery
			return m, ring[0], true, false
		}
		return m, curQuery, false, false // not our key
	}

	// Dropdown is open.
	switch {
	case altDown:
		if m.recallIndex < len(ring)-1 {
			m.recallIndex++
		}
		return m, ring[m.recallIndex], true, false
	case altUp:
		if m.recallIndex == 0 {
			draft := m.recallDraft
			m = m.recallReset()
			return m, draft, true, false // above newest -> close + restore draft
		}
		m.recallIndex--
		return m, ring[m.recallIndex], true, false
	case msg.Type == tea.KeyEnter:
		phrase := ring[m.recallIndex]
		m = m.recallReset()
		return m, phrase, true, true // accept -> caller commits on phrase
	case msg.Type == tea.KeyEsc:
		draft := m.recallDraft
		m = m.recallReset()
		return m, draft, true, false // close, restore draft, stay typing
	default:
		// Any other key (text/backspace/space) closes the dropdown and falls
		// through to normal handling with the currently-previewed query intact.
		m = m.recallReset()
		return m, curQuery, false, false
	}
}
```

- [ ] **Step 4: Call `recallUpdate` from the panel-filter loop**

In `internal/tui/model.go`, at the **top** of the `if m.filterTyping {` block (before the `switch msg.Type`), insert:

```go
			if nm, nq, handled, commit := m.recallUpdate(scopePanel, msg, m.filterQuery); handled {
				m = nm
				m.filterQuery = nq
				m.sel[m.filterPanel] = 0
				if commit {
					m.filterTyping = false
					var recCmd tea.Cmd
					m, recCmd = m.recordSearch(scopePanel, m.filterQuery)
					if m.filesView != nil && m.filterPanel == panelCommits {
						mm, c := m.syncFilesViewToSelectedCommit()
						return mm, tea.Batch(recCmd, c)
					}
					return m, recCmd
				}
				return m, nil
			} else {
				m = nm // recall may have closed on a fall-through key
			}
```

- [ ] **Step 5: Call `recallUpdate` from the @-highlight loop**

In `internal/tui/model.go`, at the top of `if m.highlightTyping {` (before its `switch`), insert the analogous block with `scopePanel` and `m.highlightQuery`, snapping the cursor after a preview:

```go
			if nm, nq, handled, commit := m.recallUpdate(scopePanel, msg, m.highlightQuery); handled {
				m = nm
				m.highlightQuery = nq
				m = m.snapToHighlightMatch()
				if commit {
					m.highlightTyping = false
					var recCmd tea.Cmd
					m, recCmd = m.recordSearch(scopePanel, m.highlightQuery)
					return m, recCmd
				}
				return m, nil
			} else {
				m = nm
			}
```

- [ ] **Step 6: Call `recallUpdate` from the three popups**

In each of `content_popup.go` (files-view), `bookmark_popup.go`, `shelf_popup.go`, at the top of their search-typing key handling, insert the analogous block with the scope (`scopeFiletree`/`scopeBookmark`/`scopeShelf`) and the popup's `p.query`, assigning `nq` back to `p.query`, resetting that popup's selection cursor, and on `commit` running the popup's normal commit path + `recordSearch`.

> **Implementer note:** these popups' update methods may carry the popup pointer; thread `m`/`p.query` so `recallUpdate` sees `Model`. The pattern is identical to Steps 4–5: preview replaces `p.query`, Enter commits + records, Esc/Alt-up-past-newest restores draft, typing falls through. Where the popup's update returns `(Model, tea.Cmd)`, `tea.Batch` the record cmd in.

- [ ] **Step 7: Reset recall when a typing mode opens**

Wherever a typing mode is *started* — panel `/` (`model.go` ~line 891 `m.filterTyping = true`), `@` (~line 898), and the three popups' `/` openers — add `m = m.recallReset()` so a stale dropdown never carries across sessions. Example at the panel `/` opener:

```go
				m.filterPanel = m.focus
				m.filterQuery = ""
				m.filterTyping = true
				m = m.recallReset()
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `cd /mnt/t/others/gg-searchhist && go test ./internal/tui/ -run 'TestRecall|TestPanelFilterEnter|TestHighlightShares' 2>&1 | tail -10`
Expected: PASS.

- [ ] **Step 9: Full tui suite + vet**

Run: `cd /mnt/t/others/gg-searchhist && go vet ./internal/tui/ && go test ./internal/tui/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
cd /mnt/t/others/gg-searchhist
gofmt -w internal/tui/
git add internal/tui/
git commit -m "feat(tui): Alt+down/up search-history recall (preview, draft restore)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 6: recall dropdown rendering (max 10 rows + scroll)

**Files:**
- Modify: `internal/tui/search_history.go` (a shared `recallBox` render helper + window math)
- Modify: the per-context view functions (panel render, `bookmark_popup.go`, `shelf_popup.go`, files-view render) to splice the box in when `recallOpen` and the scope matches.
- Test: `internal/tui/search_history_test.go` (render assertions via `View()`)

**Interfaces:**
- Consumes: `Model.recallOpen`, `recallScope`, `recallIndex`, `searchHist`.
- Produces: `(Model).recallBox(width int) string` — returns "" when closed; else a bordered list of up to 10 rows, highlighted current, with a `↑N`/`↓N` clipped affordance.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/search_history_test.go`:

```go
func TestRecallBoxShowsTenWithScroll(t *testing.T) {
	m := loadedModel(t)
	ring := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		ring = append(ring, "phrase"+string(rune('A'+i)))
	}
	m.searchHist = map[string][]string{scopePanel: ring}
	m.recallScope = scopePanel
	m.recallOpen = true
	m.recallIndex = 0
	box := m.recallBox(40)
	// Newest visible, 11th/12th initially hidden, clipped affordance present.
	if !contains(box, "phraseA") {
		t.Fatalf("box should show newest phraseA:\n%s", box)
	}
	if contains(box, "phraseL") {
		t.Fatalf("12th entry must be off-window at index 0:\n%s", box)
	}
	if !contains(box, "↓") {
		t.Fatalf("clipped-below affordance expected:\n%s", box)
	}
	// Move highlight to the oldest: window scrolls, oldest now visible.
	m.recallIndex = 11
	box = m.recallBox(40)
	if !contains(box, "phraseL") {
		t.Fatalf("oldest must be visible when highlighted:\n%s", box)
	}
}

func TestRecallBoxClosedIsEmpty(t *testing.T) {
	m := loadedModel(t)
	if got := m.recallBox(40); got != "" {
		t.Fatalf("closed dropdown renders nothing, got %q", got)
	}
}
```

> **Implementer note:** reuse the package's existing substring helper if one exists (`contains`/`strings.Contains`). If none, use `strings.Contains`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gg-searchhist && go test ./internal/tui/ -run TestRecallBox 2>&1 | head`
Expected: FAIL — `recallBox` undefined.

- [ ] **Step 3: Implement `recallBox`**

In `internal/tui/search_history.go`:

```go
import (
	"strings"

	"github.com/charmbracelet/lipgloss" // (merge into existing imports)
)

const recallVisibleRows = 10

// recallBox renders the history dropdown (≤10 rows, highlight, scroll markers).
// Returns "" when the dropdown is closed.
func (m Model) recallBox(width int) string {
	if !m.recallOpen {
		return ""
	}
	ring := m.searchHist[m.recallScope]
	if len(ring) == 0 {
		return ""
	}
	// Window: keep recallIndex visible, newest-first, at most recallVisibleRows.
	start := 0
	if m.recallIndex >= recallVisibleRows {
		start = m.recallIndex - recallVisibleRows + 1
	}
	end := start + recallVisibleRows
	if end > len(ring) {
		end = len(ring)
	}

	sel := lipgloss.NewStyle().Reverse(true)
	var b strings.Builder
	if start > 0 {
		b.WriteString("  ↑" + itoa(start) + " more\n")
	}
	for i := start; i < end; i++ {
		line := truncate(ring[i], width-2)
		if i == m.recallIndex {
			b.WriteString(sel.Render("▸ "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	if end < len(ring) {
		b.WriteString("  ↓" + itoa(len(ring)-end) + " more\n")
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Width(width)
	return box.Render(strings.TrimRight(b.String(), "\n"))
}
```

> **Implementer note:** `itoa` = `strconv.Itoa`; `truncate` — reuse the package's existing string-truncation helper (grep for `trunc`/`truncate`/`ellipsis` in `internal/tui`; one exists for the commit-id column). If none fits, inline a rune-safe truncation. Keep the affordance characters `↑`/`↓` — the test asserts `↓`.

- [ ] **Step 4: Splice the box into each context's view**

- **panel / @ highlight:** in the panel render path, when `m.recallOpen && m.recallScope == scopePanel`, render `m.recallBox(w)` near the focused panel's filter line (below it). Find the filter-line render in the panel/footer view and append the box.
- **bookmark / shelf popups:** in `bookmark_popup.go` / `shelf_popup.go` view, when `m.recallOpen && m.recallScope == scopeBookmark|scopeShelf`, render the box below the popup's filter row (inside the popup body).
- **files-view:** in the files-view render, when `m.recallOpen && m.recallScope == scopeFiletree`, render the box below the tree search line.

Each splice is: `if m.recallOpen && m.recallScope == <scope> { out += "\n" + m.recallBox(<width>) }` at the appropriate place in that view's string assembly.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /mnt/t/others/gg-searchhist && go test ./internal/tui/ -run TestRecallBox 2>&1 | tail`
Expected: PASS.

- [ ] **Step 6: Full tui suite**

Run: `cd /mnt/t/others/gg-searchhist && go test ./internal/tui/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gg-searchhist
gofmt -w internal/tui/
git add internal/tui/
git commit -m "feat(tui): render search-history dropdown (10 rows + scroll markers)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 7: docs + help note + final verification

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md` (config key table + a one-line keybinding note)
- Modify: TUI help/`?` text (grep `internal/tui/help.go` for where keys are listed) — add `alt+↑/↓  search history`.

- [ ] **Step 1: CHANGELOG entry**

Add under the current unreleased section of `CHANGELOG.md`:

```markdown
- **Search history.** Enter-confirmed searches are remembered per window
  (the panel `/` filter and `@` highlight share one ring; the bookmark/shelf
  switchers and the files-view tree search each keep their own). While typing a
  search, **Alt+↓** opens a scrollable dropdown of past phrases (max 10 rows;
  **Alt+↑** moves back up, **Esc** restores your draft). Stored per-repo; size is
  `[ui] search_history_size` (default 20, max 1000).
```

- [ ] **Step 2: README config + keybinding**

Add `search_history_size` to the README `[ui]` config table (default `20`, "entries kept per search-history ring; max 1000"), and a one-line note in the keybindings/search section: `alt+↑ / alt+↓ — recall previous searches`.

- [ ] **Step 3: TUI help note**

Run `grep -rn "alt+\|search\|filter" internal/tui/help.go` to find the help layout; add a line `alt+↑/↓  search history` near the existing `/`-filter help entry.

- [ ] **Step 4: Full suite + race**

Run:
```bash
cd /mnt/t/others/gg-searchhist
gofmt -l internal/ cmd/
go vet ./...
./test.sh race 2>&1 | tail -20
```
Expected: no gofmt output, vet clean, all tests PASS (unit + e2e).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-searchhist
git add CHANGELOG.md README.md internal/tui/
git commit -m "docs: search history (CHANGELOG, README config, help)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

## Self-Review notes (for the executor)

- **No CLI / no agentskill bump:** this feature has no `gg` subcommand and no change to the CLI surface, so `internal/agentskill/using-gg.md` and `agentskill.Version` are intentionally untouched (avoids the `TestDogfoodSkillCopyInSync` trap).
- **Scope `.` menu excluded:** do not wire `action_menu.go`'s `/` — verified excluded by design.
- **Value-receiver Model:** `recordSearch`/`recallUpdate`/`recallReset` return a new `Model`; always reassign (`m = ...`). The `searchHist` map is copied on write in `recordSearch` so the mutation survives the value copy.
- **Helper names:** TUI test helpers (`update`/`typeRunes`/`drainCmd`/`keyMsg`/`loadedModel`) are assumed from existing `*_test.go`; if names differ, adapt calls (not assertions).
