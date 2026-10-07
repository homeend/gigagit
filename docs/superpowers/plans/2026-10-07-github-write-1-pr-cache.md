# GitHub write-back — Plan 1: PR cache (stale-first, background refresh)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
> **gigagit rule (CLAUDE.md): NO implementer subagents** — this plan is executed by the session that wrote it (executing-plans); only the final whole-branch review may be a read-only subagent.

**Goal:** A pull request seen in the last hours opens at once — from a disk cache that survives restarts — and is refreshed in the background with one forge call; the open view updates in place when the refresh finds changes.

**Architecture:** A new DAG-leaf package `internal/prcache` stores per-repo JSON entries (the PR list; one entry per recently opened PR with its comments and the derived git data of its base/head pair) under the XDG state dir, guarded by the shared `filelock`. `domain` layers it under today's in-memory forge cache (expiry by read time replaces the 5-minute idle eviction), adds one combined GraphQL read (`forge.Snapshotter`) for revalidation, and one bundled open (`PRPreview`) that serves the pair's summary, commit list and file list from the cache. The TUI and web keep their existing stale-while-revalidate shapes and switch to the new domain calls.

**Tech Stack:** Go 1.26, Bubble Tea, `gh` CLI (GraphQL via `gh api graphql`), stdlib `encoding/json`, `internal/filelock`.

**Spec:** `docs/superpowers/specs/2026-10-07-github-write-design.md` (§2 is this plan; §1.3/§3.1 add read fields this plan already fetches).

## Global Constraints

- Expiry: `[forge] cache_hours`, default **8**; `0` = always read from GitHub first. An entry older than the limit is discarded, never drawn.
- Bound: **50** PR entries per repository; opening a 51st drops the least recently OPENED entry.
- Prefetch: `[forge] prefetch`, default **5**, `0` = off; only PRs whose head moved; low priority, under `LimitRunner`.
- One combined GraphQL query per refresh (head sha, state, body, threads with ids + review ids, reviews, `viewerDidAuthor`, `viewerLatestReview { id state }`, PR node id).
- Cache location: `stateBaseDir("prcache")/<repoKey(git common dir)>/`; files `list.json`, `pr-<n>.json`, lock `cache.lock`; temp + rename writes; a corrupt file is renamed `<name>.corrupt-<unix>` and treated as absent.
- Stored/drawn times use `internal/clock` (`clock.Now()`); timing code keeps `time.Now`.
- `internal/tui`, `internal/cli`, `internal/web`, `internal/mcp` never import `prcache` or `internal/git` (archtest); `prcache` imports only stdlib, `filelock`, `model`.
- Every user-visible TUI string through `i18n.T` with a literal key present in all four bundles (ja/ko/zh/ru).
- Tests use real git in `t.TempDir()` or `FakeRunner`; new tests call `t.Parallel()`; every TestMain that can reach the cache pins `XDG_STATE_HOME`/`XDG_CONFIG_HOME` to temp dirs.
- Nothing in this plan writes to GitHub.

## Review Focus

1. **Two gg processes on one repo** (TUI + `gg web`, or two TUIs) write the same `pr-<n>.json` at once → no torn file, no error surfaced, last writer wins. (Task 1 test.)
2. **A read time in the future** (system clock moved back, file copied from another machine) → the entry counts as expired, never as fresh forever. (Task 1 test.)
3. **A force-pushed PR head** (new sha, unrelated history) → after the refresh the diff is recomputed for the new pair; the old pair's derived data is never served for the new head. (Task 6 test.)
4. **`cache_hours = 0` / `prefetch = 0`** → no stale draw at all / no background fetch at all. (Task 4 and Task 7 tests.)
5. **A repo switch while a refresh is in flight** → its answer is dropped, never applied to the new repository's PR with the same number. (Task 8 test.)

---

### Task 1: `internal/prcache` — the on-disk PR cache store

**Files:**
- Create: `internal/prcache/prcache.go`
- Create: `internal/prcache/prcache_test.go`

**Interfaces:**
- Consumes: `filelock.Acquire(path string) (release func(), err error)`; `model.PullRequest`, `model.ForgeComment`, `model.CommitFile`.
- Produces:
  ```go
  type Derived struct {
      Source, Target string            // full shas the data belongs to (PR head, base tip)
      State          string            // "ok" | "merged" | "no-base"
      MergeBase      string
      Ahead, Files   int
      Commits        []string          // rev-list mergeBase..Source
      FileList       []model.CommitFile // diff --name-status mergeBase Source
  }
  type Entry struct {
      Number       int
      PR           model.PullRequest
      Full         bool                 // read on its own (carries the body)
      Comments     []model.ForgeComment
      Truncated    bool
      HasComments  bool
      ReadAt       time.Time            // last forge read — the expiry clock
      OpenedAt     time.Time            // last open — the LRU clock
      Derived      []Derived            // newest first, at most 2
  }
  type List struct {
      ReadAt   time.Time
      Provider string                   // the provider name that answered ("github")
      PRs      []model.PullRequest
  }
  func New(root string, max int) *Store
  func (s *Store) LoadList() (List, bool)
  func (s *Store) SaveList(l List) error
  func (s *Store) Load(n int) (Entry, bool)
  func (s *Store) Save(e Entry) error        // then trims to max by OpenedAt
  func (s *Store) Remove(n int) error
  func (e *Entry) PutDerived(d Derived)      // replaces same (Source,Target), keeps newest 2
  func (e Entry) DerivedFor(source, target string) (Derived, bool)
  func Fresh(readAt, now time.Time, maxAge time.Duration) bool
  const DefaultMax = 50
  ```

- [ ] **Step 1: Write the failing tests**

```go
package prcache

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

var t0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	s := New(t.TempDir(), DefaultMax)
	e := Entry{Number: 7, PR: model.PullRequest{Number: 7, Title: "x", HeadSHA: "abc"}, Full: true,
		Comments: []model.ForgeComment{{ID: "c1", Body: "hi"}}, HasComments: true, ReadAt: t0, OpenedAt: t0}
	e.PutDerived(Derived{Source: "s1", Target: "t1", State: "ok", Ahead: 2, Files: 1,
		FileList: []model.CommitFile{{Status: "M", Path: "a.go"}}})
	if err := s.Save(e); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Load(7)
	if !ok || got.PR.Title != "x" || len(got.Comments) != 1 || !got.ReadAt.Equal(t0) {
		t.Fatalf("Load = %+v, %v", got, ok)
	}
	d, ok := got.DerivedFor("s1", "t1")
	if !ok || d.FileList[0].Path != "a.go" {
		t.Fatalf("DerivedFor = %+v, %v", d, ok)
	}
	if _, ok := got.DerivedFor("s2", "t1"); ok {
		t.Fatal("a different pair must miss")
	}
}

func TestPutDerivedKeepsNewestTwo(t *testing.T) {
	t.Parallel()
	var e Entry
	for _, s := range []string{"a", "b", "c"} {
		e.PutDerived(Derived{Source: s, Target: "t"})
	}
	if len(e.Derived) != 2 || e.Derived[0].Source != "c" || e.Derived[1].Source != "b" {
		t.Fatalf("Derived = %+v", e.Derived)
	}
	e.PutDerived(Derived{Source: "b", Target: "t", Ahead: 9}) // same pair: replaced, moved to front
	if len(e.Derived) != 2 || e.Derived[0].Source != "b" || e.Derived[0].Ahead != 9 {
		t.Fatalf("Derived after replace = %+v", e.Derived)
	}
}

func TestSaveTrimsLeastRecentlyOpened(t *testing.T) {
	t.Parallel()
	s := New(t.TempDir(), 3)
	for n := 1; n <= 4; n++ {
		if err := s.Save(Entry{Number: n, OpenedAt: t0.Add(time.Duration(n) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := s.Load(1); ok {
		t.Error("the least recently opened entry must be dropped")
	}
	for n := 2; n <= 4; n++ {
		if _, ok := s.Load(n); !ok {
			t.Errorf("entry %d dropped", n)
		}
	}
}

func TestCorruptFileIsQuarantined(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := New(dir, DefaultMax)
	if err := os.WriteFile(filepath.Join(dir, "pr-9.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Load(9); ok {
		t.Fatal("a corrupt entry must read as absent")
	}
	m, _ := filepath.Glob(filepath.Join(dir, "pr-9.json.corrupt-*"))
	if len(m) != 1 {
		t.Fatalf("quarantine files = %v", m)
	}
	if err := os.WriteFile(filepath.Join(dir, "list.json"), []byte("]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LoadList(); ok {
		t.Fatal("a corrupt list must read as absent")
	}
}

func TestListRoundTripAndRemove(t *testing.T) {
	t.Parallel()
	s := New(t.TempDir(), DefaultMax)
	if err := s.SaveList(List{ReadAt: t0, Provider: "github", PRs: []model.PullRequest{{Number: 3}}}); err != nil {
		t.Fatal(err)
	}
	l, ok := s.LoadList()
	if !ok || l.Provider != "github" || len(l.PRs) != 1 {
		t.Fatalf("LoadList = %+v, %v", l, ok)
	}
	_ = s.Save(Entry{Number: 3})
	if err := s.Remove(3); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Load(3); ok {
		t.Fatal("removed entry still loads")
	}
	if err := s.Remove(3); err != nil {
		t.Fatalf("removing an absent entry: %v", err)
	}
}

// Review focus 2: a read time in the future is never fresh.
func TestFresh(t *testing.T) {
	t.Parallel()
	h8 := 8 * time.Hour
	cases := []struct {
		name   string
		readAt time.Time
		maxAge time.Duration
		want   bool
	}{
		{"just read", t0, h8, true},
		{"7h59m old", t0.Add(-h8 + time.Minute), h8, true},
		{"8h01m old", t0.Add(-h8 - time.Minute), h8, false},
		{"zero time", time.Time{}, h8, false},
		{"limit 0", t0, 0, false},
		{"read in the future", t0.Add(2 * time.Hour), h8, false},
		{"a minute ahead (clock jitter)", t0.Add(30 * time.Second), h8, true},
	}
	for _, c := range cases {
		if got := Fresh(c.readAt, t0, c.maxAge); got != c.want {
			t.Errorf("%s: Fresh = %v, want %v", c.name, got, c.want)
		}
	}
}

// Review focus 1: concurrent writers never leave a torn file.
func TestConcurrentSavesNeverTear(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, b := New(dir, DefaultMax), New(dir, DefaultMax) // two "processes"
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := a
			if i%2 == 1 {
				s = b
			}
			_ = s.Save(Entry{Number: 5, PR: model.PullRequest{Title: strings.Repeat("x", i*100)}, OpenedAt: t0})
		}()
	}
	wg.Wait()
	if _, ok := a.Load(5); !ok {
		t.Fatal("entry unreadable after concurrent saves")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.corrupt-*")); len(m) != 0 {
		t.Fatalf("a concurrent save tore the file: %v", m)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/prcache/`
Expected: FAIL — `undefined: New` (package has no implementation).

- [ ] **Step 3: Implement**

```go
// Package prcache is the on-disk pull-request cache: per repository, the PR
// list and one entry per recently opened PR (its forge data, its comments and
// the derived git data of its base/head pair). Records only — domain decides
// what is fresh and what to draw. JSON files under one directory, written
// temp + rename under the shared file lock, so several gg processes on one
// repository never tear a file. DAG leaf: stdlib, filelock, model.
package prcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/filelock"
	"github.com/homeend/gigagit/internal/model"
)

// DefaultMax bounds the PR entries kept per repository (spec §2.1).
const DefaultMax = 50

// futureSlack is how far ahead of now a read time may be and still count:
// two processes' clocks and a file system's mtime granularity disagree by
// seconds, never by hours.
const futureSlack = time.Minute

// Derived is the git data of one (head, base) pair. A new push is a new
// pair, so an entry never serves another pair's data.
type Derived struct {
	Source    string             `json:"source"`
	Target    string             `json:"target"`
	State     string             `json:"state"`
	MergeBase string             `json:"merge_base,omitempty"`
	Ahead     int                `json:"ahead,omitempty"`
	Files     int                `json:"files,omitempty"`
	Commits   []string           `json:"commits,omitempty"`
	FileList  []model.CommitFile `json:"file_list,omitempty"`
}

// Entry is one cached pull request.
type Entry struct {
	Number      int                  `json:"number"`
	PR          model.PullRequest    `json:"pr"`
	Full        bool                 `json:"full,omitempty"`
	Comments    []model.ForgeComment `json:"comments,omitempty"`
	Truncated   bool                 `json:"truncated,omitempty"`
	HasComments bool                 `json:"has_comments,omitempty"`
	ReadAt      time.Time            `json:"read_at"`
	OpenedAt    time.Time            `json:"opened_at"`
	Derived     []Derived            `json:"derived,omitempty"`
}

// List is the cached open-PR listing.
type List struct {
	ReadAt   time.Time           `json:"read_at"`
	Provider string              `json:"provider"`
	PRs      []model.PullRequest `json:"prs"`
}

// Store is one repository's cache directory.
type Store struct {
	root string
	max  int
}

// New opens the cache rooted at root (created on first write), keeping at
// most max PR entries (<= 0 = DefaultMax).
func New(root string, max int) *Store {
	if max <= 0 {
		max = DefaultMax
	}
	return &Store{root: root, max: max}
}

// Fresh reports whether a read at readAt is still within maxAge of now. A
// zero time, a zero limit, or a read time more than a minute in the future
// (a clock moved back) is never fresh.
func Fresh(readAt, now time.Time, maxAge time.Duration) bool {
	if readAt.IsZero() || maxAge <= 0 || readAt.After(now.Add(futureSlack)) {
		return false
	}
	return now.Sub(readAt) < maxAge
}

// PutDerived records d, replacing the same pair and keeping the newest two.
func (e *Entry) PutDerived(d Derived) {
	out := []Derived{d}
	for _, x := range e.Derived {
		if x.Source != d.Source || x.Target != d.Target {
			out = append(out, x)
		}
	}
	if len(out) > 2 {
		out = out[:2]
	}
	e.Derived = out
}

// DerivedFor is the pair's data, if the entry has it.
func (e Entry) DerivedFor(source, target string) (Derived, bool) {
	for _, d := range e.Derived {
		if d.Source == source && d.Target == target {
			return d, true
		}
	}
	return Derived{}, false
}

func (s *Store) entryPath(n int) string { return filepath.Join(s.root, "pr-"+strconv.Itoa(n)+".json") }
func (s *Store) listPath() string       { return filepath.Join(s.root, "list.json") }

// locked runs f under the directory's lock.
func (s *Store) locked(f func() error) error {
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return err
	}
	release, err := filelock.Acquire(filepath.Join(s.root, "cache.lock"))
	if err != nil {
		return err
	}
	defer release()
	return f()
}

// readJSON decodes path into v; a corrupt file is quarantined and reads as
// absent, a missing one as absent.
func readJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if err := json.Unmarshal(b, v); err != nil {
		_ = os.Rename(path, path+".corrupt-"+strconv.FormatInt(time.Now().Unix(), 10))
		return false
	}
	return true
}

// writeJSON writes v to path atomically (temp + rename in the same dir).
func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// LoadList reads the cached listing.
func (s *Store) LoadList() (List, bool) {
	var l List
	return l, readJSON(s.listPath(), &l)
}

// SaveList replaces the cached listing.
func (s *Store) SaveList(l List) error {
	return s.locked(func() error { return writeJSON(s.listPath(), l) })
}

// Load reads PR n's entry.
func (s *Store) Load(n int) (Entry, bool) {
	var e Entry
	return e, readJSON(s.entryPath(n), &e)
}

// Save writes e and trims the directory to max entries, least recently
// opened first.
func (s *Store) Save(e Entry) error {
	return s.locked(func() error {
		if err := writeJSON(s.entryPath(e.Number), e); err != nil {
			return err
		}
		return s.trimLocked()
	})
}

// Remove deletes PR n's entry; an absent one is not an error.
func (s *Store) Remove(n int) error {
	return s.locked(func() error {
		if err := os.Remove(s.entryPath(n)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	})
}

func (s *Store) trimLocked() error {
	names, err := filepath.Glob(filepath.Join(s.root, "pr-*.json"))
	if err != nil || len(names) <= s.max {
		return err
	}
	type aged struct {
		path   string
		opened time.Time
	}
	var all []aged
	for _, p := range names {
		var e Entry
		if readJSON(p, &e) {
			all = append(all, aged{p, e.OpenedAt})
		}
	}
	slices.SortFunc(all, func(a, b aged) int { return b.opened.Compare(a.opened) })
	for _, a := range all[min(len(all), s.max):] {
		if err := os.Remove(a.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("prcache: trim %s: %w", filepath.Base(a.path), err)
		}
	}
	return nil
}

// numberOf parses "pr-<n>.json" (used by callers listing entries).
func numberOf(name string) (int, bool) {
	s, ok := strings.CutPrefix(filepath.Base(name), "pr-")
	if !ok {
		return 0, false
	}
	s, ok = strings.CutSuffix(s, ".json")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// Entries lists every cached entry, most recently opened first (prefetch
// walks it).
func (s *Store) Entries() []Entry {
	names, _ := filepath.Glob(filepath.Join(s.root, "pr-*.json"))
	var out []Entry
	for _, p := range names {
		if _, ok := numberOf(p); !ok {
			continue
		}
		var e Entry
		if readJSON(p, &e) {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b Entry) int { return b.OpenedAt.Compare(a.OpenedAt) })
	return out
}
```

Add `Entries()` to the Interfaces block above (it is used by Task 7). Add one test:

```go
func TestEntriesNewestOpenedFirst(t *testing.T) {
	t.Parallel()
	s := New(t.TempDir(), DefaultMax)
	_ = s.Save(Entry{Number: 1, OpenedAt: t0})
	_ = s.Save(Entry{Number: 2, OpenedAt: t0.Add(time.Hour)})
	es := s.Entries()
	if len(es) != 2 || es[0].Number != 2 || es[1].Number != 1 {
		t.Fatalf("Entries = %+v", es)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/prcache/`
Expected: PASS (all 8 tests).

- [ ] **Step 5: Add the package to the archtest DAG-leaf list**

Run: `grep -n 'filelock' internal/archtest/*.go` to find the leaf table; add `"prcache"` beside `"linkhist"` with allowed imports `filelock`, `model`. Run `go test ./internal/archtest/` → PASS.

- [ ] **Step 6: Commit**

```bash
gg add internal/prcache internal/archtest
git commit -m "feat(prcache): on-disk pull-request cache store"
```

---

### Task 2: one combined GraphQL read (`forge.Snapshotter`) + the new read fields

**Files:**
- Modify: `internal/model/forge.go` (fields)
- Modify: `internal/forge/forge.go` (interface)
- Modify: `internal/forge/gh.go` (query + method)
- Modify: `internal/forge/gh_parse.go` (parser)
- Modify: `internal/forge/testdata/fakegh/main.go` (route)
- Create: `internal/forge/testdata/snapshot-7.json`
- Test: `internal/forge/gh_test.go`, `internal/forge/gh_parse_test.go`

**Interfaces:**
- Produces:
  ```go
  // model
  type PullRequest struct { /* … existing … */
      NodeID              string `json:"node_id,omitempty"`              // the forge's id; write calls target it (plan 2)
      ViewerDidAuthor     bool   `json:"viewer_did_author,omitempty"`
      ViewerPendingReview string `json:"viewer_pending_review,omitempty"` // id of the viewer's PENDING review, "" = none
  }
  type ForgeComment struct { /* … existing … */
      ThreadID string `json:"thread_id,omitempty"` // review-thread id (inline/file comments)
      ReviewID string `json:"review_id,omitempty"` // the review the comment was posted in
  }
  // forge
  type Snapshot struct {
      PR        model.PullRequest
      Comments  []model.ForgeComment
      Truncated bool
  }
  type Snapshotter interface {
      Snapshot(ctx context.Context, n int) (Snapshot, error)
  }
  func (g *GH) Snapshot(ctx context.Context, n int) (Snapshot, error)
  ```

- [ ] **Step 1: Write the fixture** `internal/forge/testdata/snapshot-7.json`:

```json
{"data":{"repository":{"pullRequest":{
 "id":"PR_kw7","number":7,"title":"Add x","body":"Body\r\ntext","author":{"login":"ann"},
 "state":"OPEN","isDraft":false,"reviewDecision":"REVIEW_REQUIRED","headRefName":"feat/x",
 "isCrossRepository":false,"headRepositoryOwner":{"login":"ann"},"headRepository":{"name":"r"},
 "baseRefName":"main","baseRefOid":"b0b0","headRefOid":"h1h1","url":"https://github.com/o/r/pull/7",
 "createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-06T10:00:00Z",
 "viewerDidAuthor":true,"viewerLatestReview":{"id":"PRR_p1","state":"PENDING"},
 "reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[
  {"id":"PRRT_t1","path":"a.go","line":4,"startLine":null,"originalLine":4,"originalStartLine":null,
   "diffSide":"RIGHT","subjectType":"LINE","isResolved":false,"isOutdated":false,
   "comments":{"pageInfo":{"hasNextPage":false},"nodes":[
    {"id":"PRRC_c1","replyTo":null,"author":{"login":"bob"},"body":"why?","diffHunk":"@@ -1 +1 @@",
     "createdAt":"2026-10-05T10:00:00Z","updatedAt":"2026-10-05T10:00:00Z","pullRequestReview":{"id":"PRR_r1"}}]}}]},
 "comments":{"pageInfo":{"hasNextPage":false},"nodes":[]},
 "reviews":{"pageInfo":{"hasNextPage":false},"nodes":[
  {"id":"PRR_r1","author":{"login":"bob"},"body":"","state":"COMMENTED","submittedAt":"2026-10-05T10:00:00Z"}]}
}}}}
```

- [ ] **Step 2: Write the failing parser test** (in `gh_parse_test.go`):

```go
func TestParseSnapshot(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("testdata/snapshot-7.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := parseSnapshot(b)
	if err != nil {
		t.Fatal(err)
	}
	p := s.PR
	if p.Number != 7 || p.NodeID != "PR_kw7" || p.HeadSHA != "h1h1" || p.Body != "Body\ntext" ||
		p.State != "open" || !p.ViewerDidAuthor || p.ViewerPendingReview != "PRR_p1" {
		t.Fatalf("PR = %+v", p)
	}
	var inline *model.ForgeComment
	for i := range s.Comments {
		if s.Comments[i].ID == "PRRC_c1" {
			inline = &s.Comments[i]
		}
	}
	if inline == nil || inline.ThreadID != "PRRT_t1" || inline.ReviewID != "PRR_r1" || inline.Line != 4 {
		t.Fatalf("inline comment = %+v", inline)
	}
}

// A submitted (non-pending) latest review is not a pending one.
func TestParseSnapshotIgnoresSubmittedLatestReview(t *testing.T) {
	t.Parallel()
	b, _ := os.ReadFile("testdata/snapshot-7.json")
	b = bytes.Replace(b, []byte(`"state":"PENDING"`), []byte(`"state":"COMMENTED"`), 1)
	s, err := parseSnapshot(b)
	if err != nil || s.PR.ViewerPendingReview != "" {
		t.Fatalf("pending = %q, err %v", s.PR.ViewerPendingReview, err)
	}
}
```

Also extend the existing threads parser test (find it with `grep -n 'func TestParseThreads' internal/forge/gh_parse_test.go`) to assert that a thread node carrying `"id":"PRRT_x"` and a comment carrying `"pullRequestReview":{"id":"PRR_y"}` yields `ThreadID "PRRT_x"` / `ReviewID "PRR_y"`.

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/forge/ -run 'Snapshot|Threads'`
Expected: FAIL — `undefined: parseSnapshot`.

- [ ] **Step 4: Implement**

In `internal/model/forge.go`, add the three `PullRequest` fields and the two `ForgeComment` fields exactly as in Interfaces.

In `internal/forge/gh_parse.go`:
- add `ID string \`json:"id"\`` to `ghThread` and `PullRequestReview *struct{ ID string \`json:"id"\` } \`json:"pullRequestReview"\`` to `ghThreadComment`;
- in `parseThreads`' comment loop set `fc.ThreadID = th.ID` and, when `c.PullRequestReview != nil`, `fc.ReviewID = c.PullRequestReview.ID`;
- add:

```go
// ghSnapshot is the combined read: the PR's own fields (ghPR's names are
// GraphQL's, so it decodes the same node) plus the viewer fields; the
// threads/comments/reviews of the same node go through parseThreads.
type ghSnapshot struct {
	Data struct {
		Repository struct {
			PullRequest *struct {
				ghPR
				ID                 string `json:"id"`
				ViewerDidAuthor    bool   `json:"viewerDidAuthor"`
				ViewerLatestReview *struct {
					ID    string `json:"id"`
					State string `json:"state"`
				} `json:"viewerLatestReview"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

func parseSnapshot(b []byte) (Snapshot, error) {
	var raw ghSnapshot
	if err := json.Unmarshal(b, &raw); err != nil {
		return Snapshot{}, err
	}
	n := raw.Data.Repository.PullRequest
	if n == nil {
		return Snapshot{}, ErrNotFound
	}
	pr := n.ghPR.model()
	pr.NodeID, pr.ViewerDidAuthor = n.ID, n.ViewerDidAuthor
	if r := n.ViewerLatestReview; r != nil && r.State == "PENDING" {
		pr.ViewerPendingReview = r.ID
	}
	cs, truncated, err := parseThreads(b)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{PR: pr, Comments: cs, Truncated: truncated}, nil
}
```

In `internal/forge/forge.go`, add the `Snapshot` struct and `Snapshotter` interface (doc: "optional; a Provider that implements it answers a revalidation with ONE call. Read-only.").

In `internal/forge/gh.go`:
- extend `threadsQuery`: thread nodes gain `id`; comment nodes gain `pullRequestReview{id}`;
- add the combined query and method:

```go
// snapshotQuery is threadsQuery's pullRequest node plus the PR's own fields
// and the viewer fields — one round trip for a whole revalidation. The
// operation name lets the fake gh tell it from the threads read.
const snapshotQuery = `query PRSnapshot($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){
id number title body author{login} state isDraft reviewDecision headRefName isCrossRepository headRepositoryOwner{login} headRepository{name}
baseRefName baseRefOid headRefOid url createdAt updatedAt viewerDidAuthor viewerLatestReview{id state}
reviewThreads(first:100){pageInfo{hasNextPage} nodes{id path line startLine originalLine originalStartLine diffSide subjectType isResolved isOutdated
comments(first:50){pageInfo{hasNextPage} nodes{id replyTo{id} author{login} body diffHunk createdAt updatedAt pullRequestReview{id}}}}}
comments(first:100){pageInfo{hasNextPage} nodes{id author{login} body createdAt updatedAt}}
reviews(first:100){pageInfo{hasNextPage} nodes{id author{login} body state submittedAt}}}}}`

func (g *GH) Snapshot(ctx context.Context, n int) (Snapshot, error) {
	res, err := g.run(ctx, "gh api graphql (snapshot)", "api", "graphql",
		"-F", "owner={owner}", "-F", "name={repo}", "-F", "number="+strconv.Itoa(n),
		"-f", "query="+snapshotQuery)
	if err != nil {
		return Snapshot{}, err
	}
	return parseSnapshot([]byte(res.Stdout))
}
```

In `internal/forge/testdata/fakegh/main.go`, inside the `api graphql` case, pick `snapshot-<n>.json` when any arg starts with `query=query PRSnapshot`:

```go
	case len(a) >= 2 && a[0] == "api" && a[1] == "graphql":
		prefix := "threads-"
		for _, s := range a {
			if strings.HasPrefix(s, "query=query PRSnapshot") {
				prefix = "snapshot-"
			}
		}
		for _, s := range a {
			if n, ok := strings.CutPrefix(s, "number="); ok {
				name = prefix + n + ".json"
			}
		}
```

Add an argv test in `gh_test.go` (FakeRunner) asserting `Snapshot(ctx, 7)` runs `api graphql -F owner={owner} -F name={repo} -F number=7 -f query=query PRSnapshot…`, with the runner returning the fixture bytes and the result's `PR.NodeID == "PR_kw7"`. Follow the existing `TestGHComments…` argv test in that file for the FakeRunner wiring.

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/forge/... ./internal/model/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gg add internal/model/forge.go internal/forge
git commit -m "feat(forge): one-call PR snapshot read with thread, review and node ids"
```

---

### Task 3: `[forge]` config — `cache_hours`, `prefetch`

**Files:**
- Modify: `internal/config/config.go` (struct, Defaults, accessors)
- Modify: `internal/config/template.go` (`settingDocs`)
- Modify: the overlay/populate code the adding-config-entries skill names
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  ```go
  type ForgeConfig struct {
      CacheHours *int `toml:"cache_hours"` // nil = 8; 0 = always read from the forge first
      Prefetch   *int `toml:"prefetch"`    // nil = 5; 0 = off
  }
  func (f ForgeConfig) CacheMaxAge() time.Duration // nil → 8h; <0 → 8h; 0 → 0
  func (f ForgeConfig) PrefetchCount() int         // nil → 5; <0 → 0; >50 → 50
  // Config gains: Forge ForgeConfig `toml:"forge"`
  ```
  Pointers because an explicit 0 must overlay a non-zero default (the `RefreshConfig.PRs` precedent).

- [ ] **Step 1: Load the `adding-config-entries` skill** (`Skill adding-config-entries`) and follow its checklist for every file it names (struct, Defaults, overlay, template doc, Settings registry if it applies, docs).

- [ ] **Step 2: Write the failing test**

```go
func TestForgeConfigDefaultsAndOverlay(t *testing.T) {
	t.Parallel()
	var f ForgeConfig
	if f.CacheMaxAge() != 8*time.Hour || f.PrefetchCount() != 5 {
		t.Fatalf("defaults = %v, %d", f.CacheMaxAge(), f.PrefetchCount())
	}
	zero, big, neg := 0, 99, -3
	f = ForgeConfig{CacheHours: &zero, Prefetch: &zero}
	if f.CacheMaxAge() != 0 || f.PrefetchCount() != 0 {
		t.Fatalf("explicit 0 = %v, %d", f.CacheMaxAge(), f.PrefetchCount())
	}
	f = ForgeConfig{CacheHours: &neg, Prefetch: &big}
	if f.CacheMaxAge() != 8*time.Hour || f.PrefetchCount() != 50 {
		t.Fatalf("clamped = %v, %d", f.CacheMaxAge(), f.PrefetchCount())
	}
	// repo layer `[forge] cache_hours = 0` overlays the global 12
	dir := t.TempDir()
	global, repo := filepath.Join(dir, "g.toml"), filepath.Join(dir, "r.toml")
	os.WriteFile(global, []byte("[forge]\ncache_hours = 12\n"), 0o600)
	os.WriteFile(repo, []byte("[forge]\ncache_hours = 0\n"), 0o600)
	cfg, err := Load(global, repo)
	if err != nil || cfg.Forge.CacheMaxAge() != 0 {
		t.Fatalf("overlay = %v, %v", cfg.Forge.CacheMaxAge(), err)
	}
}
```

(If `Load`'s second parameter is a repo DIRECTORY rather than a file, use the form the neighbouring `RefreshConfig.PRs` overlay test uses — `grep -n 'prs' internal/config/*_test.go`.)

- [ ] **Step 3: Run to verify failure** — `go test ./internal/config/ -run Forge` → FAIL `undefined: ForgeConfig`.

- [ ] **Step 4: Implement** the struct, the two accessors, the `Forge` field on `Config`, the overlay arm (pointer fields: non-nil wins, as for `Refresh.PRs`), and two `settingDocs` entries:

```go
{Section: "forge", Key: "cache_hours", Doc: "Hours a pull request read from the forge stays fresh. A fresher one opens at once from the cache and refreshes in the background; an older one is read from the forge first. 0 = always read first. Default 8."},
{Section: "forge", Key: "prefetch", Doc: "After the pull-request list refreshes, fetch and prepare this many recently opened pull requests whose head moved, in the background. 0 = off. Default 5."},
```

(Match the real `settingDoc` field names — read the first entries of `settingDocs` in `internal/config/template.go`.)

- [ ] **Step 5: Run** `go test ./internal/config/` → PASS (the template staleness test included).

- [ ] **Step 6: Commit**

```bash
gg add internal/config
git commit -m "feat(config): [forge] cache_hours and prefetch"
```

---

### Task 4: domain — disk-backed PR, comment and list cache with expiry

**Files:**
- Create: `internal/domain/prcachestore.go`
- Modify: `internal/domain/forge_cache.go`
- Modify: `internal/domain/forge.go` (`pullRequests`, `PullRequest`, `PRComments` bucketing, `PRForgetOp`)
- Modify: `internal/domain/forge_notes.go` (`PRCommentsRefresh`, `PRCommentsCached`)
- Modify: `internal/domain/service.go` (fields)
- Test: `internal/domain/prcache_test.go`; rewrite `TestPRCacheEvictsIdleEntries` in `internal/domain/forge_test.go`

**Interfaces:**
- Consumes: `prcache.New/Load/Save/Remove/LoadList/SaveList/Fresh`, `config.ForgeConfig`.
- Produces:
  ```go
  func (s *Service) SetPRCachePolicy(maxAge time.Duration, prefetch int) // frontends call it on config load
  func (s *Service) SetPRCacheStore(st *prcache.Store)                   // tests
  func (s *Service) PullRequestsCached() ([]model.PullRequest, bool)     // the fresh cached listing; never calls the forge
  func (s *Service) PRCacheReadAt(n int) (time.Time, bool)               // when PR n was last read from the forge
  func bucketComments(cs []model.ForgeComment, truncated bool) PRComments // extracted from PRComments
  ```
  Behaviour change: `prCacheIdle` and idle eviction are removed; an in-memory or disk entry is served while `prcache.Fresh(ReadAt, s.forgeClock(), maxAge)`; `OpenedAt` is bumped (and persisted) on every `cachedPR` use.

- [ ] **Step 1: Write the failing tests** (`internal/domain/prcache_test.go`; reuse `fakeForge`, `newForgeSvc`, `pr(n, state, x)` from `forge_test.go`):

```go
func newCachedForgeSvc(t *testing.T, ff *fakeForge, dir string) *Service {
	t.Helper()
	svc := newForgeSvc(t, ff)
	svc.SetPRCacheStore(prcache.New(dir, prcache.DefaultMax))
	return svc
}

// A PR read in one session opens in the next with no forge call.
func TestPRCacheSurvivesANewSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ff := &fakeForge{url: "u", byNum: map[int]model.PullRequest{7: pr(7, "open", 1)}}
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	a := newCachedForgeSvc(t, ff, dir)
	a.forgeNow = func() time.Time { return now }
	if _, err := a.PRFetchOp(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	b := newCachedForgeSvc(t, ff, dir) // a restart
	now = now.Add(7 * time.Hour)
	b.forgeNow = func() time.Time { return now }
	if _, err := b.PRFetchOp(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if n, _ := ff.calls(7); n != 1 {
		t.Fatalf("PR() called %d times across two sessions within 8h, want 1", n)
	}
	now = now.Add(2 * time.Hour) // 9h after the read
	c := newCachedForgeSvc(t, ff, dir)
	c.forgeNow = func() time.Time { return now }
	c.PRFetchOp(context.Background(), 7)
	if n, _ := ff.calls(7); n != 2 {
		t.Fatalf("an expired entry must be read again: PR() called %d times, want 2", n)
	}
}

// Review focus 4: cache_hours = 0 never serves a cached entry.
func TestPRCacheZeroHoursAlwaysReads(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{url: "u", byNum: map[int]model.PullRequest{7: pr(7, "open", 1)}}
	svc := newCachedForgeSvc(t, ff, t.TempDir())
	svc.SetPRCachePolicy(0, 0)
	for range 2 {
		svc.PRFetchOp(context.Background(), 7)
	}
	if n, _ := ff.calls(7); n != 2 {
		t.Fatalf("cache_hours=0: PR() called %d times, want 2", n)
	}
	if _, ok := svc.PullRequestsCached(); ok {
		t.Fatal("cache_hours=0: no cached listing may be served")
	}
}

// Comments read in one session are drawn in the next before any forge call.
func TestPRCommentsServedFromDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ff := &fakeForge{url: "u", byNum: map[int]model.PullRequest{7: pr(7, "open", 1)},
		comments: map[int][]model.ForgeComment{7: {{ID: "c1", Kind: model.ForgeCommentInline, Path: "a.go", Line: 2, Body: "x"}}}}
	a := newCachedForgeSvc(t, ff, dir)
	if _, err := a.PRCommentsRefresh(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	b := newCachedForgeSvc(t, ff, dir)
	c, ok := b.PRCommentsCached(7)
	if !ok || len(c.Inline) != 1 || c.Inline[0].ID != "c1" {
		t.Fatalf("PRCommentsCached in a new session = %+v, %v", c, ok)
	}
}

// The listing is cached; a new session draws it with no forge call.
func TestPullRequestsCached(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ff := &fakeForge{url: "u", open: []model.PullRequest{pr(3, "open", 1)}}
	a := newCachedForgeSvc(t, ff, dir)
	if _, err := a.PullRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	b := newCachedForgeSvc(t, ff, dir)
	got, ok := b.PullRequestsCached()
	if !ok || len(got) != 1 || got[0].Number != 3 {
		t.Fatalf("PullRequestsCached = %+v, %v", got, ok)
	}
}

// Forget drops the disk entry too.
func TestPRForgetRemovesTheDiskEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ff := &fakeForge{url: "u", byNum: map[int]model.PullRequest{7: pr(7, "closed", 1)}}
	svc := newCachedForgeSvc(t, ff, dir)
	svc.PRFetchOp(context.Background(), 7)
	svc.PRForgetOp(7)
	if _, ok := prcache.New(dir, 0).Load(7); ok {
		t.Fatal("PRForgetOp left the disk entry")
	}
}
```

If `fakeForge` lacks `comments`/`open` fields, add them in `forge_test.go` (its `Comments` and `ListOpen` methods return them) — read the type first: `grep -n 'type fakeForge' -A30 internal/domain/forge_test.go`.

Rewrite `TestPRCacheEvictsIdleEntries` → `TestPRCacheExpiresByReadTime`: within 8h of the READ two opens make one `PR()` call even with no use in between for 7h; at 8h+1s the next open calls again (use is no longer what keeps an entry).

- [ ] **Step 2: Run to verify failure** — `go test ./internal/domain/ -run 'PRCache|PRComments|PullRequestsCached|PRForget'` → FAIL (`SetPRCacheStore` undefined).

- [ ] **Step 3: Implement**

`internal/domain/service.go` — add beside the forge fields:

```go
	// prStore is the on-disk PR cache (prcachestore.go); nil until first use,
	// and stays nil when no state dir resolves (the cache is then memory-only).
	prStore       *prcache.Store
	prStoreReady  bool
	prMaxAge      time.Duration // [forge] cache_hours; set by SetPRCachePolicy
	prPrefetch    int           // [forge] prefetch
	prPolicySet   bool
```

`internal/domain/prcachestore.go`:

```go
package domain

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/prcache"
)

// Defaults when no frontend set a policy (CLI one-shots, tests).
const (
	defaultPRMaxAge   = 8 * time.Hour
	defaultPRPrefetch = 5
)

// SetPRCachePolicy applies [forge] cache_hours / prefetch.
func (s *Service) SetPRCachePolicy(maxAge time.Duration, prefetch int) {
	s.forgeMu.Lock()
	s.prMaxAge, s.prPrefetch, s.prPolicySet = maxAge, prefetch, true
	s.forgeMu.Unlock()
}

// SetPRCacheStore injects the store (tests).
func (s *Service) SetPRCacheStore(st *prcache.Store) {
	s.forgeMu.Lock()
	s.prStore, s.prStoreReady = st, true
	s.forgeMu.Unlock()
}

// prPolicyLocked is the effective policy. Callers hold forgeMu.
func (s *Service) prPolicyLocked() (time.Duration, int) {
	if !s.prPolicySet {
		return defaultPRMaxAge, defaultPRPrefetch
	}
	return s.prMaxAge, s.prPrefetch
}

// prCacheStore resolves (once) the per-repo store keyed by git common dir.
// Called OUTSIDE forgeMu (GitCommonDir runs git).
func (s *Service) prCacheStore(ctx context.Context) *prcache.Store {
	s.forgeMu.Lock()
	if s.prStoreReady {
		st := s.prStore
		s.forgeMu.Unlock()
		return st
	}
	s.forgeMu.Unlock()
	var st *prcache.Store
	if base := stateBaseDir("prcache"); base != "" {
		if cd, err := s.GitCommonDir(ctx); err == nil {
			st = prcache.New(filepath.Join(base, repoKey(strings.TrimSpace(cd))), prcache.DefaultMax)
		}
	}
	s.forgeMu.Lock()
	if !s.prStoreReady {
		s.prStore, s.prStoreReady = st, true
	}
	st = s.prStore
	s.forgeMu.Unlock()
	return st
}

// fresh reports whether a read at t is still fresh under the policy.
func (s *Service) fresh(t time.Time) bool {
	s.forgeMu.Lock()
	maxAge, _ := s.prPolicyLocked()
	s.forgeMu.Unlock()
	return prcache.Fresh(t, s.forgeClock(), maxAge)
}

// diskEntry is PR n's fresh disk entry.
func (s *Service) diskEntry(ctx context.Context, n int) (prcache.Entry, bool) {
	st := s.prCacheStore(ctx)
	if st == nil {
		return prcache.Entry{}, false
	}
	e, ok := st.Load(n)
	if !ok || !s.fresh(e.ReadAt) {
		return prcache.Entry{}, false
	}
	return e, true
}

// persistPR merges what the caller learned into PR n's disk entry. Best
// effort: a cache that cannot be written never fails the read it follows.
func (s *Service) persistPR(ctx context.Context, n int, edit func(e *prcache.Entry)) {
	st := s.prCacheStore(ctx)
	if st == nil {
		return
	}
	e, ok := st.Load(n)
	if !ok {
		e = prcache.Entry{Number: n}
	}
	edit(&e)
	_ = st.Save(e)
}

// PullRequestsCached is the fresh cached listing (never a forge call).
func (s *Service) PullRequestsCached() ([]model.PullRequest, bool) {
	st := s.prCacheStore(context.Background())
	if st == nil {
		return nil, false
	}
	l, ok := st.LoadList()
	if !ok || !s.fresh(l.ReadAt) {
		return nil, false
	}
	return l.PRs, true
}

// PRCacheReadAt is when PR n was last read from the forge.
func (s *Service) PRCacheReadAt(n int) (time.Time, bool) {
	s.forgeMu.Lock()
	e, ok := s.forgePRCache[n]
	s.forgeMu.Unlock()
	if ok {
		return e.readAt, true
	}
	if d, ok := s.diskEntry(context.Background(), n); ok {
		return d.ReadAt, true
	}
	return time.Time{}, false
}
```

`internal/domain/forge_cache.go` — replace `used` with `readAt`, drop `prCacheIdle` and the idle loop:

```go
type forgePREntry struct {
	pr     model.PullRequest
	full   bool
	readAt time.Time // when the forge answered; expiry counts from here
}

func (s *Service) putPRLocked(pr model.PullRequest, full bool) {
	if s.forgePRCache == nil {
		s.forgePRCache = map[int]forgePREntry{}
	}
	if old, ok := s.forgePRCache[pr.Number]; ok && old.full && !full {
		pr.Body, full = old.pr.Body, true
	}
	s.forgePRCache[pr.Number] = forgePREntry{pr: pr, full: full, readAt: s.forgeClock()}
}

// takePRLocked returns PR n when its read is still fresh. Callers hold forgeMu.
func (s *Service) takePRLocked(n int) (forgePREntry, bool) {
	e, ok := s.forgePRCache[n]
	maxAge, _ := s.prPolicyLocked()
	if !ok || !prcache.Fresh(e.readAt, s.forgeClock(), maxAge) {
		delete(s.forgePRCache, n)
		return forgePREntry{}, false
	}
	return e, true
}
```

`cachedPR` becomes memory → disk → forge, persisting and bumping `OpenedAt`:

```go
func (s *Service) cachedPR(ctx context.Context, p forge.Provider, n int) (model.PullRequest, error) {
	s.forgeMu.Lock()
	e, ok := s.takePRLocked(n)
	s.forgeMu.Unlock()
	if !ok {
		if d, dok := s.diskEntry(ctx, n); dok {
			s.forgeMu.Lock()
			if s.forgePRCache == nil {
				s.forgePRCache = map[int]forgePREntry{}
			}
			s.forgePRCache[n] = forgePREntry{pr: d.PR, full: d.Full, readAt: d.ReadAt}
			s.forgeMu.Unlock()
			e, ok = forgePREntry{pr: d.PR, full: d.Full, readAt: d.ReadAt}, true
		}
	}
	if ok {
		now := s.forgeClock()
		s.persistPR(ctx, n, func(x *prcache.Entry) { x.OpenedAt = now })
		return e.pr, nil
	}
	pr, err := p.PR(ctx, n)
	if err != nil {
		return model.PullRequest{}, err
	}
	s.rememberPR(ctx, pr, true)
	return pr, nil
}

// rememberPR stores pr in memory and on disk, stamping the read time.
func (s *Service) rememberPR(ctx context.Context, pr model.PullRequest, full bool) {
	s.forgeMu.Lock()
	s.putPRLocked(pr, full)
	e := s.forgePRCache[pr.Number]
	s.forgeMu.Unlock()
	now := s.forgeClock()
	s.persistPR(ctx, pr.Number, func(x *prcache.Entry) {
		x.PR, x.Full, x.ReadAt, x.OpenedAt = e.pr, e.full, e.readAt, now
	})
}
```

Replace the other `putPRLocked(pr, true)` call sites outside `pullRequests` (`PullRequest`, `PRRevalidate`) with `s.rememberPR(ctx, pr, true)` (in `PRRevalidate` keep the `forgeTerminal` bookkeeping under its own lock).

`PRDetailsCached` / `PRCommentsCached`: on a memory miss, load the fresh disk entry (`diskEntry`), seed `forgePRCache` / `forgeComments` (comments through `bucketComments(e.Comments, e.Truncated)`, `sig: prCommentsSig(...)`) and answer from it.

`internal/domain/forge.go`:
- extract `bucketComments(cs, truncated) PRComments` from `PRComments` (body unchanged) and call it there;
- `PRComments` gains nothing else; `PRCommentsRefresh` (forge_notes.go) after storing in memory also persists: `s.persistPR(ctx, n, func(x *prcache.Entry){ x.Comments, x.Truncated, x.HasComments = raw, truncated, true })` — so `PRComments` must hand back the raw slice too: add an unexported `prCommentsRaw(ctx, n) ([]model.ForgeComment, bool, error)` that `PRComments` and `PRCommentsRefresh` share;
- `pullRequests`: after a successful `ListOpen`, `st.SaveList(prcache.List{ReadAt: s.forgeClock(), Provider: p.Name(), PRs: <the open+rest result>})` (best effort, store resolved via `prCacheStore`);
- `PRForgetOp`: also `if st := s.prCacheStore(context.Background()); st != nil { _ = st.Remove(n) }` (outside forgeMu).

Lock rule (CLAUDE.md "forgeMu"): never call `prCacheStore`, `persistPR`, `diskEntry` or `fresh` while holding `forgeMu` — each takes it internally.

- [ ] **Step 4: Run** `go test -race ./internal/domain/ -run 'PR|Forge|Preview'` → PASS.

- [ ] **Step 5: Pin the state dir in TestMains** that can now write the cache: `internal/domain`, `internal/tui`, `internal/web`, `internal/cli`, `internal/mcp`, `e2e` — check each TestMain with `grep -n 'XDG_STATE_HOME' internal/*/main_test.go e2e/*_test.go`; add `os.Setenv("XDG_STATE_HOME", <tempdir>)` where missing (memory: test-xdg-config-isolation).

- [ ] **Step 6: Commit**

```bash
gg add internal/domain
git commit -m "feat(domain): disk-backed PR cache with read-time expiry"
```

---

### Task 5: domain — revalidation in one call, reporting comment changes

**Files:**
- Modify: `internal/domain/forge_cache.go` (`PRRevalidate`, `PRRevalidation`)
- Test: `internal/domain/prcache_test.go`

**Interfaces:**
- Consumes: `forge.Snapshotter`, `bucketComments`, `prCommentsSig`, `rememberPR`, `persistPR`.
- Produces:
  ```go
  type PRRevalidation struct {
      PR              model.PullRequest
      Moved           bool // local refs/gg/pr/<n> missing or behind the forge's head
      CommentsChanged bool // the cached comments differ from the forge's now
      ReadAt          time.Time
  }
  ```
  `PRRevalidate` uses `Snapshot` when the provider implements `forge.Snapshotter` (ONE forge call: PR + comments), else `PR()` + `PRCommentsRefresh` (today's two).

- [ ] **Step 1: Write the failing test** — add a `snapFake` in the test file that embeds `*fakeForge` and implements `Snapshot` by counting calls and returning `forge.Snapshot{PR: byNum[n], Comments: comments[n]}`:

```go
type snapFake struct {
	*fakeForge
	snaps int
}

func (f *snapFake) Snapshot(_ context.Context, n int) (forge.Snapshot, error) {
	f.mu.Lock()
	f.snaps++
	f.mu.Unlock()
	return forge.Snapshot{PR: f.byNum[n], Comments: f.comments[n]}, nil
}

func TestPRRevalidateIsOneCallAndReportsComments(t *testing.T) {
	t.Parallel()
	ff := &snapFake{fakeForge: &fakeForge{url: "u", byNum: map[int]model.PullRequest{7: pr(7, "open", 1)},
		comments: map[int][]model.ForgeComment{7: {{ID: "c1", Kind: model.ForgeCommentInline, Path: "a", Line: 1}}}}}
	svc := newCachedForgeSvc(t, ff, t.TempDir()) // newForgeSvc must accept a forge.Provider
	rv, err := svc.PRRevalidate(context.Background(), 7)
	if err != nil || !rv.CommentsChanged {
		t.Fatalf("first revalidate = %+v, %v", rv, err)
	}
	rv, _ = svc.PRRevalidate(context.Background(), 7)
	if rv.CommentsChanged {
		t.Fatal("unchanged comments reported as changed")
	}
	if ff.snaps != 2 {
		t.Fatalf("Snapshot calls = %d, want 2", ff.snaps)
	}
	if n, _ := ff.calls(7); n != 0 {
		t.Fatalf("PR() called %d times beside Snapshot, want 0", n)
	}
	if c, ok := svc.PRCommentsCached(7); !ok || len(c.Inline) != 1 {
		t.Fatalf("revalidate did not fill the comment cache: %+v %v", c, ok)
	}
}
```

(If `newForgeSvc` takes `*fakeForge`, widen its parameter to `forge.Provider`; `fakeForge` methods must have pointer receivers so `snapFake` embedding works — check, and adapt the embed if they are value receivers.)

- [ ] **Step 2: Run** — `go test ./internal/domain/ -run PRRevalidate` → FAIL (`CommentsChanged` undefined).

- [ ] **Step 3: Implement**

```go
func (s *Service) PRRevalidate(ctx context.Context, n int) (PRRevalidation, error) {
	p, err := s.provider(ctx)
	if err != nil {
		return PRRevalidation{}, err
	}
	var pr model.PullRequest
	changed := false
	if sp, ok := p.(forge.Snapshotter); ok {
		snap, err := sp.Snapshot(ctx, n)
		if err != nil {
			return PRRevalidation{}, err
		}
		pr = snap.PR
		changed = s.storeComments(ctx, n, snap.Comments, snap.Truncated)
	} else {
		if pr, err = p.PR(ctx, n); err != nil {
			return PRRevalidation{}, err
		}
		if changed, err = s.PRCommentsRefresh(ctx, n); err != nil {
			return PRRevalidation{}, err
		}
	}
	s.rememberPR(ctx, pr, true)
	if !pr.IsOpen() {
		s.forgeMu.Lock()
		if s.forgeTerminal == nil {
			s.forgeTerminal = map[int]model.PullRequest{}
		}
		s.forgeTerminal[n] = pr
		s.forgeMu.Unlock()
	}
	readAt, _ := s.PRCacheReadAt(n)
	local, rerr := s.repo.ResolveCommit(ctx, git.PRRef(n))
	return PRRevalidation{PR: pr, Moved: rerr != nil || (pr.HeadSHA != "" && local != pr.HeadSHA),
		CommentsChanged: changed, ReadAt: readAt}, nil
}
```

Extract `storeComments(ctx, n, raw, truncated) (changed bool)` from `PRCommentsRefresh` (bucket → sig → memory → persist) and make `PRCommentsRefresh` call it.

- [ ] **Step 4: Run** `go test -race ./internal/domain/ -run 'PR|Forge'` → PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/domain
git commit -m "feat(domain): revalidate a PR with one forge call, reporting comment changes"
```

---

### Task 6: domain — `PRPreview`: the PR open served from cached derived data

**Files:**
- Modify: `internal/domain/preview.go` (split hash variants)
- Modify: `internal/domain/previewnotes.go` (split hash variant)
- Modify: `internal/domain/query.go` (`CompareFiles` caches commit↔commit pairs)
- Create: `internal/domain/prpreview.go`
- Test: `internal/domain/prpreview_test.go`

**Interfaces:**
- Consumes: `prcache.Derived`, `Entry.PutDerived/DerivedFor`, `persistPR`, `diskEntry`.
- Produces:
  ```go
  type PRPreviewResult struct {
      Pair      PRPair
      Endpoints PreviewEndpoints
      Set       PreviewNoteSet      // zero unless Endpoints.Summary.State == PreviewOK
      Files     []model.CommitFile  // the compare file list (nil unless PreviewOK)
      Cached    bool                // served from the disk entry (no git recompute)
  }
  func (s *Service) PRPreview(ctx context.Context, pr model.PullRequest) (PRPreviewResult, error)
  func (s *Service) previewSummaryHashes(ctx context.Context, srcHash, tgtHash string) (PreviewSummary, error)
  func (s *Service) previewNoteSetFor(ctx context.Context, source, target string, sum PreviewSummary) (PreviewNoteSet, error)
  ```
  `CompareFiles(left, right)` with both endpoints commits is now cached in `s.factory.Cache("preview")` under `"compare-files:"+tags` (immutable pair; a LRU, not disk).

- [ ] **Step 1: Write the failing tests** (real git; build a repo with `newTestRepo`-style helpers already used in `preview_test.go`: a `main` with one commit, a branch `feat` with two commits touching `a.go`; create `refs/gg/pr/7` pointing at `feat`'s tip with `git update-ref`). Use a counting runner wrapper — the domain tests already have one (`grep -n 'countingRunner\|recordRunner' internal/domain/*_test.go`); if none exists, wrap the repo's `gitexec.Runner` in a struct that counts `Run` calls by name.

```go
func TestPRPreviewServesASecondSessionFromTheCache(t *testing.T) {
	t.Parallel()
	dir, head := prPreviewRepo(t) // returns repo dir + feat tip sha; refs/gg/pr/7 = head
	cache := t.TempDir()
	p := model.PullRequest{Number: 7, State: "open", Target: "main", HeadSHA: head}

	a, _ := countingService(t, dir)
	a.SetPRCacheStore(prcache.New(cache, 0))
	first, err := a.PRPreview(context.Background(), p)
	if err != nil || first.Endpoints.Summary.State != PreviewOK || len(first.Files) != 1 || first.Cached {
		t.Fatalf("first open = %+v, %v", first, err)
	}

	b, calls := countingService(t, dir) // a restart: empty memory caches
	b.SetPRCacheStore(prcache.New(cache, 0))
	second, err := b.PRPreview(context.Background(), p)
	if err != nil || !second.Cached || len(second.Files) != 1 || len(second.Set.Commits) != 2 {
		t.Fatalf("second open = %+v, %v", second, err)
	}
	for _, name := range []string{"git merge-base", "git rev-list --left-right --count",
		"git diff --name-only (range)", "git rev-list (range)", "git diff (compare files)"} {
		if calls(name) != 0 {
			t.Errorf("%s ran %d times on a cached open, want 0", name, calls(name))
		}
	}
	// The compare view's own file read hits the seeded memory cache.
	if _, err := b.CompareFiles(context.Background(), second.Endpoints.Left, second.Endpoints.Right); err != nil {
		t.Fatal(err)
	}
	if calls("git diff (compare files)") != 0 {
		t.Error("CompareFiles re-ran git after PRPreview seeded it")
	}
}

// Review focus 3: a force-push is a new pair; the old pair's data is never served.
func TestPRPreviewRecomputesForANewHead(t *testing.T) {
	t.Parallel()
	dir, head := prPreviewRepo(t)
	cache := t.TempDir()
	svc, _ := countingService(t, dir)
	svc.SetPRCacheStore(prcache.New(cache, 0))
	p := model.PullRequest{Number: 7, State: "open", Target: "main", HeadSHA: head}
	if _, err := svc.PRPreview(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	newHead := forcePushPR(t, dir, 7) // a new commit on an orphan-ish base touching b.go; update-ref refs/gg/pr/7
	svc2, _ := countingService(t, dir)
	svc2.SetPRCacheStore(prcache.New(cache, 0))
	got, err := svc2.PRPreview(context.Background(), model.PullRequest{Number: 7, State: "open", Target: "main", HeadSHA: newHead})
	if err != nil || got.Cached {
		t.Fatalf("new head served from cache: %+v, %v", got, err)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "b.go" {
		t.Fatalf("files for the new head = %+v", got.Files)
	}
}
```

Write the helpers `prPreviewRepo`, `forcePushPR`, `countingService` in the test file with real git commands (`git init -b main`, commits via `git commit --allow-empty`/files, `git update-ref refs/gg/pr/7 <sha>`), following `preview_test.go`'s repo helper.

- [ ] **Step 2: Run** — `go test ./internal/domain/ -run PRPreview` → FAIL (`PRPreview` undefined).

- [ ] **Step 3: Implement**

`preview.go`: move the cached body of `PreviewSummary` (from `key := "preview-summary:"…` to the return) into `previewSummaryHashes(ctx, srcHash, tgtHash)`; `PreviewSummary` keeps the two `ResolveRev` calls then calls it. Add an unexported seeding helper:

```go
// seedPreviewSummary puts a summary computed elsewhere (the PR cache) under
// the same key previewSummaryHashes would fill.
func (s *Service) seedPreviewSummary(sum PreviewSummary) {
	s.factory.Cache("preview").Put("preview-summary:"+sum.SourceHash+":"+sum.TargetHash, sum)
}
```

(Check the cache type's insert method name: `grep -n 'func (c \*.*) \(Put\|Set\|Add\)' internal/cache/*.go`; use it.)

`previewnotes.go`: split `PreviewNotes` into resolve + `previewNoteSetFor(ctx, source, target, sum)` (the rev-list part); add `seedPreviewRevList(src, tgt string, commits []string)` with key `"preview-revlist:"+src+":"+tgt`.

`query.go` `CompareFiles`: when both endpoints are `model.EndpointCommit`, wrap the existing `query(...)` in `s.factory.Cache("preview").GetOrLoad("compare-files:"+tags, …)`; add `seedCompareFiles(left, right model.Endpoint, files []model.CommitFile)`.

`prpreview.go`:

```go
package domain

import (
	"context"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/prcache"
)

// PRPreview is a pull request's whole open: its pair, the endpoints, the
// note set and the changed files. The pair's derived git data (merge base,
// counts, commit list, file list) is read from the disk cache when it holds
// THIS (head, base) pair, and seeded into the memory caches the compare view
// reads next; otherwise it is computed once and saved. Each ref resolves
// once (spec §2.2).
func (s *Service) PRPreview(ctx context.Context, pr model.PullRequest) (PRPreviewResult, error) {
	pair := s.PRPair(ctx, pr)
	src, okS, err := s.ResolveRev(ctx, pair.Head)
	if err != nil {
		return PRPreviewResult{}, err
	}
	tgt, okT, err := s.ResolveRev(ctx, pair.Base)
	if err != nil {
		return PRPreviewResult{}, err
	}
	out := PRPreviewResult{Pair: pair}
	switch {
	case !okS:
		out.Endpoints.Summary = PreviewSummary{State: PreviewMissingSource}
		return out, nil
	case !okT:
		out.Endpoints.Summary = PreviewSummary{State: PreviewMissingTarget, SourceHash: src}
		return out, nil
	}
	if e, ok := s.diskEntry(ctx, pr.Number); ok {
		if d, ok := e.DerivedFor(src, tgt); ok {
			return s.fromDerived(pair, d)
		}
	}
	sum, err := s.previewSummaryHashes(ctx, src, tgt)
	if err != nil {
		return PRPreviewResult{}, err
	}
	out.Endpoints, err = endpointsFor(sum)
	if err != nil || sum.State != PreviewOK {
		s.saveDerived(ctx, pr.Number, sum, nil, nil)
		return out, err
	}
	if out.Set, err = s.previewNoteSetFor(ctx, pair.Head, pair.Base, sum); err != nil {
		return PRPreviewResult{}, err
	}
	if out.Files, err = s.CompareFiles(ctx, out.Endpoints.Left, out.Endpoints.Right); err != nil {
		return PRPreviewResult{}, err
	}
	s.saveDerived(ctx, pr.Number, sum, out.Set.Commits, out.Files)
	return out, nil
}

func (s *Service) fromDerived(pair PRPair, d prcache.Derived) (PRPreviewResult, error) {
	sum := PreviewSummary{State: previewStateOf(d.State), SourceHash: d.Source, TargetHash: d.Target,
		Files: d.Files, Ahead: d.Ahead, base: d.MergeBase}
	s.seedPreviewSummary(sum)
	out := PRPreviewResult{Pair: pair, Cached: true}
	var err error
	if out.Endpoints, err = endpointsFor(sum); err != nil || sum.State != PreviewOK {
		return out, err
	}
	s.seedPreviewRevList(d.Source, d.Target, d.Commits)
	s.seedCompareFiles(out.Endpoints.Left, out.Endpoints.Right, d.FileList)
	out.Set = PreviewNoteSet{Source: pair.Head, Target: pair.Base, Tip: d.Source, Base: d.MergeBase, Commits: d.Commits}
	out.Files = d.FileList
	return out, nil
}

func (s *Service) saveDerived(ctx context.Context, n int, sum PreviewSummary, commits []string, files []model.CommitFile) {
	d := prcache.Derived{Source: sum.SourceHash, Target: sum.TargetHash, State: sum.State.String(),
		MergeBase: sum.base, Ahead: sum.Ahead, Files: sum.Files, Commits: commits, FileList: files}
	s.persistPR(ctx, n, func(e *prcache.Entry) { e.PutDerived(d) })
}
```

Factor `endpointsFor(sum PreviewSummary) (PreviewEndpoints, error)` out of `PreviewOpen` (the two `CommitEndpoint` calls) and have `PreviewOpen` use it. Add `previewStateOf(string) PreviewState` as the inverse of the existing `PreviewState.String()` (states: ok, merged, no-base, missing-source, missing-target). `persistPR` must not create an entry with a zero `ReadAt` that later reads as "fresh": it never does (`Fresh` rejects a zero time), and `diskEntry` refuses it — note that a PR opened from the list (row data only) gets its `ReadAt` from `rememberPR` in `cachedPR`.

- [ ] **Step 4: Run** `go test -race ./internal/domain/` → PASS (whole package: the preview tests must not regress).

- [ ] **Step 5: Commit**

```bash
gg add internal/domain
git commit -m "feat(domain): PRPreview opens a PR from cached derived git data"
```

---

### Task 7: domain — skip the detect call, and prefetch

**Files:**
- Modify: `internal/domain/forge.go` (`PullRequests`)
- Create: `internal/domain/prprefetch.go`
- Test: `internal/domain/prcache_test.go`

**Interfaces:**
- Produces:
  ```go
  func (s *Service) PRPrefetch(ctx context.Context) int // PRs warmed; 0 when prefetch is off
  ```
  `PullRequests`: when detection has not run yet and the fresh cached listing names a provider, that provider's `ListOpen` is tried FIRST; success marks it the active provider (no `Detect` call); failure falls back to `ForgeStatus` (today's path).

- [ ] **Step 1: Write the failing tests**

```go
// A fresh cached listing from "fake" lets the next session list without Detect.
func TestPullRequestsSkipsDetectAfterAGoodSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ff := &fakeForge{url: "u", open: []model.PullRequest{pr(3, "open", 1)}}
	a := newCachedForgeSvc(t, ff, dir)
	a.PullRequests(context.Background())
	before := ff.detects
	b := newCachedForgeSvc(t, ff, dir)
	if _, err := b.PullRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ff.detects != before {
		t.Fatalf("Detect ran %d more times", ff.detects-before)
	}
	if st := b.ForgeStatus(context.Background()); !st.Available() {
		t.Fatal("a successful optimistic list must leave the forge available")
	}
}

// A failing optimistic list falls back to detection.
func TestPullRequestsFallsBackToDetect(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ok := &fakeForge{url: "u", open: []model.PullRequest{pr(3, "open", 1)}}
	newCachedForgeSvc(t, ok, dir).PullRequests(context.Background())
	broken := &fakeForge{url: "u", listErr: errors.New("401"), detectErr: errors.New("logged out")}
	b := newCachedForgeSvc(t, broken, dir)
	if _, err := b.PullRequests(context.Background()); err == nil {
		t.Fatal("want an error")
	}
	if broken.detects != 1 {
		t.Fatalf("Detect calls = %d, want 1", broken.detects)
	}
}

// Review focus 4: prefetch = 0 does nothing; otherwise only moved heads, newest opened first, at most N.
func TestPRPrefetch(t *testing.T) {
	t.Parallel()
	dir, head := prPreviewRepo(t) // refs/gg/pr/7 = head
	svc, _ := countingService(t, dir)
	svc.SetPRCacheStore(prcache.New(t.TempDir(), 0))
	ff := &fakeForge{url: dir, open: []model.PullRequest{{Number: 7, State: "open", Target: "main", HeadSHA: head}}}
	svc.SetForgeProviders([]forge.Provider{ff})
	svc.PRPreview(context.Background(), ff.open[0]) // opened once → an entry
	svc.SetPRCachePolicy(8*time.Hour, 0)
	if n := svc.PRPrefetch(context.Background()); n != 0 {
		t.Fatalf("prefetch=0 warmed %d", n)
	}
	svc.SetPRCachePolicy(8*time.Hour, 5)
	if n := svc.PRPrefetch(context.Background()); n != 0 {
		t.Fatalf("an unmoved head was prefetched (%d)", n)
	}
}
```

(Add `detects`, `detectErr`, `listErr` to `fakeForge` if missing.)

- [ ] **Step 2: Run** → FAIL (`PRPrefetch` undefined; the skip test fails on detects).

- [ ] **Step 3: Implement**

In `PullRequests`, before `s.provider(ctx)`:

```go
	if p := s.optimisticProvider(); p != nil {
		v, err := s.flight.Do("forge-prs", func() (any, error) { return s.pullRequests(ctx, p) })
		if err == nil {
			s.forgeMu.Lock()
			if !s.forgeProbed {
				s.forgeActive, s.forgeErr, s.forgeProbed = p, nil, true
			}
			s.forgeMu.Unlock()
			s.invalidatePreflight()
			return slices.Clone(v.([]model.PullRequest)), nil
		}
		// fall through: detection decides, as before
	}
```

```go
// optimisticProvider is the provider the fresh cached listing names, when
// detection has not run yet this session; nil otherwise.
func (s *Service) optimisticProvider() forge.Provider {
	s.forgeMu.Lock()
	probed, ps, rec := s.forgeProbed || s.forgeProbing != nil, s.forgeProviders, s.forgeRec
	s.forgeMu.Unlock()
	if probed {
		return nil
	}
	st := s.prCacheStore(context.Background())
	if st == nil {
		return nil
	}
	l, ok := st.LoadList()
	if !ok || l.Provider == "" || !s.fresh(l.ReadAt) {
		return nil
	}
	if ps == nil && !ForgeDisabled {
		ps = forge.Default(s.workdir, rec)
	}
	for _, p := range ps {
		if p.Name() == l.Provider {
			return p
		}
	}
	return nil
}
```

`prprefetch.go`:

```go
// PRPrefetch fetches and prepares, in the background, the most recently
// opened PRs whose forge head moved past the local ref (spec §2.5). It runs
// each fetch through Execute (the repo gate) and then PRPreview, so the next
// open is a cache hit. Returns how many PRs it warmed.
func (s *Service) PRPrefetch(ctx context.Context) int {
	s.forgeMu.Lock()
	_, limit := s.prPolicyLocked()
	s.forgeMu.Unlock()
	st := s.prCacheStore(ctx)
	if limit <= 0 || st == nil {
		return 0
	}
	listed := map[int]model.PullRequest{}
	if prs, ok := s.PullRequestsCached(); ok {
		for _, p := range prs {
			listed[p.Number] = p
		}
	}
	warmed := 0
	for _, e := range st.Entries() {
		if warmed >= limit || ctx.Err() != nil {
			break
		}
		p, ok := listed[e.Number]
		if !ok || !p.IsOpen() || p.HeadSHA == "" {
			continue
		}
		if local, err := s.repo.ResolveCommit(ctx, git.PRRef(p.Number)); err == nil && local == p.HeadSHA {
			continue // unmoved: nothing to warm
		}
		op, err := s.PRFetchOp(ctx, p.Number)
		if err != nil {
			continue
		}
		if _, err := s.Execute(ctx, op, nil, nil); err != nil {
			continue
		}
		if _, err := s.PRPreview(ctx, p); err == nil {
			warmed++
		}
	}
	return warmed
}
```

(Check `Execute`'s exact signature: `grep -n 'func (s \*Service) Execute' internal/domain/*.go` and pass nil channels the way `runHeadless` in tasks.go does if nil is not accepted.)

- [ ] **Step 4: Run** `go test -race ./internal/domain/` → PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/domain
git commit -m "feat(domain): list without a detect call after a good session; PR prefetch"
```

---

### Task 8: TUI — cached list at startup, cached open, one-call refresh, freshness mark

**Files:**
- Modify: `internal/tui/pr_panel.go` (`kickForgeProbe`, a cached-list message)
- Modify: `internal/tui/pr_actions.go` (`openPRPreviewCmd` → `PRPreview`)
- Modify: `internal/tui/pr_revalidate.go` (comments + freshness)
- Modify: `internal/tui/pr_comments.go` (the tick runs the revalidate)
- Modify: `internal/tui/preview_open.go` (title freshness suffix)
- Modify: `internal/tui/model.go` (fields, config hook calling `SetPRCachePolicy`)
- Modify: `internal/i18n/*.toml` (four bundles)
- Test: `internal/tui/pr_cache_test.go`

**Interfaces:**
- Consumes: `svc.PullRequestsCached()`, `svc.PRPreview(ctx, pr)`, `svc.PRRevalidate` (`CommentsChanged`, `ReadAt`), `svc.PRCacheReadAt(n)`, `svc.SetPRCachePolicy`.
- Produces: `prFresh` state on the Model: `prRefreshing bool`, `prOfflineSince time.Time` (zero = online), drawn as a title suffix by `prFreshnessSuffix() string`.

- [ ] **Step 1: Write the failing tests** (`internal/tui/pr_cache_test.go`; build the Model the way `pr_revalidate_test.go` / `pr_panel_test.go` do — `grep -n 'func Test.*Revalidat' internal/tui/*_test.go` and copy its setup, including the fake forge provider injection):

```go
// The PR tab appears from the cached listing before the forge answers.
func TestPRTabDrawsTheCachedListFirst(t *testing.T) {
	t.Parallel()
	m := newPRTestModel(t)              // the existing helper used by PR panel tests
	seedPRListCache(t, m, 3)            // svc.SetPRCacheStore + SaveList{PRs: #3, ReadAt: clock.Now()}
	m, cmd := m.kickForgeProbe()
	msg := firstMsgOf(t, cmd, func(x tea.Msg) bool { _, ok := x.(prsCachedMsg); return ok })
	m2, _ := m.Update(msg)
	if got := m2.(Model).prs; len(got) != 1 || got[0].Number != 3 {
		t.Fatalf("prs after the cached message = %+v", got)
	}
}

// A refresh that reports changed comments reloads the notes; the title shows
// "refreshing…" while it runs and an offline age when it fails.
func TestPRRefreshDrivesCommentsAndFreshness(t *testing.T) {
	t.Parallel()
	m := openPRDiffModel(t, 7) // existing helper or: newPRTestModel + previewOpen{prNumber:7} + filesView
	m, _ = m.prRevalidateCmd(7)
	if !strings.Contains(m.prFreshnessSuffix(), i18n.T("refreshing…")) {
		t.Fatalf("suffix while in flight = %q", m.prFreshnessSuffix())
	}
	m2, cmd := m.handlePRRevalidatedMsg(prRevalidatedMsg{n: 7, pr: model.PullRequest{Number: 7, State: "open"}, commentsChanged: true})
	if cmd == nil {
		t.Fatal("changed comments must schedule a notes/counts reload")
	}
	if m2.prFreshnessSuffix() != "" {
		t.Fatalf("suffix after a good refresh = %q", m2.prFreshnessSuffix())
	}
	m3, _ := m2.handlePRRevalidatedMsg(prRevalidatedMsg{n: 7, err: errors.New("offline"), readAt: clock.Now().Add(-3 * time.Hour)})
	if !strings.Contains(m3.prFreshnessSuffix(), "3") {
		t.Fatalf("offline suffix = %q", m3.prFreshnessSuffix())
	}
}

// Review focus 5: an answer for the previous repository is dropped.
func TestPRRefreshAfterARepoSwitchIsDropped(t *testing.T) {
	t.Parallel()
	m := openPRDiffModel(t, 7)
	m, _ = m.prRevalidateCmd(7)
	gen := m.prsGen
	m.prsGen++ // what reRoot does
	m2, cmd := m.handlePRRevalidatedMsg(prRevalidatedMsg{n: 7, gen: gen, moved: true, pr: model.PullRequest{Number: 7}})
	if cmd != nil || m2.prRevalidateInflight {
		t.Fatal("a stale-generation refresh must be dropped and must free the in-flight flag")
	}
}
```

- [ ] **Step 2: Run** — `go test ./internal/tui/ -run 'PRTab|PRRefresh'` → FAIL.

- [ ] **Step 3: Implement**

1. `model.go`: where the TUI applies config (search `applyTasksConfig` call sites), also call `m.svc.SetPRCachePolicy(m.cfg.Forge.CacheMaxAge(), m.cfg.Forge.PrefetchCount())` (and on reRoot, for the new Service). Add Model fields `prRefreshing bool`, `prOfflineSince time.Time`, `prReadAt time.Time`.
2. `pr_panel.go`: `kickForgeProbe` returns `tea.Batch(cachedListCmd, readPRsCmd…)` where

```go
type prsCachedMsg struct {
	gen int
	prs []model.PullRequest
}

func (m Model) cachedPRsCmd() tea.Cmd {
	svc, gen := m.svc, m.prsGen
	return func() tea.Msg {
		prs, ok := svc.PullRequestsCached()
		if !ok {
			return nil
		}
		return prsCachedMsg{gen: gen, prs: prs}
	}
}
```

and the `prsCachedMsg` arm (in `model.go`'s Update switch) applies `msg.prs` only when `msg.gen == m.prsGen` AND no live read landed yet (`len(m.prs) == 0`), showing the tab exactly as `handlePRsLoaded` does for a usable status (reuse its row-applying code via a small extracted helper `applyPRRows(prs)`).
3. `pr_actions.go` `openPRPreviewCmd`: replace `PRPair` + `PreviewOpen` + `PreviewNotes` with one `svc.PRPreview(ctx, p)`; fill `previewOpenMsg{source: r.Pair.Head, target: r.Pair.Base, eps: r.Endpoints, set: r.Set, …}` (only when `r.Set.OK()`) and keep the counts call. `handlePreviewOpenMsg` is unchanged: its `openCompareFiles` → `CompareFiles` read now hits the cache `PRPreview` seeded.
4. `pr_revalidate.go`: `prRevalidatedMsg` gains `commentsChanged bool`, `readAt time.Time`, `gen int` (set from `m.prsGen` when the cmd is built; the handler drops a mismatched gen after clearing `prRevalidateInflight`). `prRevalidateCmd` sets `m.prRefreshing = true`. The handler clears it; on error sets `m.prOfflineSince = msg.readAt` (fallback `svc.PRCacheReadAt`), on success zeroes it; on `commentsChanged` returns the same cmds `handlePRCommentsMsg` returns for a change (extract `m.reloadPRNotesCmds()` from there and call it from both).
5. `pr_comments.go`: `prCommentsTick` calls `m.prRevalidateCmd(n)` instead of `m.prCommentsCmd(false)`; `r` in a PR diff (manual) keeps `prCommentsCmd(true)` for its status-line answers but ALSO goes through revalidate — simplest: make `prCommentsCmd(manual)` build the revalidate cmd and map its result to both messages. Keep the `prRevalidateSkip` loop guard intact.
6. Title: `prTitle(p)` callers append `m.prFreshnessSuffix()`:

```go
func (m Model) prFreshnessSuffix() string {
	switch {
	case m.prRefreshing:
		return " · " + i18n.T("refreshing…")
	case !m.prOfflineSince.IsZero():
		return " · " + i18n.T("offline · read %s ago", humanAge(clock.Now().Sub(m.prOfflineSince)))
	}
	return ""
}
```

(`humanAge`: reuse the existing age formatter — `grep -n 'func .*[aA]ge(' internal/tui/*.go`.) Add the keys `refreshing…` and `offline · read %s ago` to all four bundles (load the `adding-translations` skill and follow it).

- [ ] **Step 4: Run** `go test -race ./internal/tui/` → PASS (whole package; the i18n AST gates included).

- [ ] **Step 5: Commit**

```bash
gg add internal/tui internal/i18n
git commit -m "feat(tui): PRs draw from the cache, refresh in one call, show freshness"
```

---

### Task 9: TUI — a moved head updates the open diff in place

**Files:**
- Modify: `internal/tui/pr_revalidate.go`
- Modify: `internal/tui/model.go` (`compareFilesMsg` arm: reland)
- Test: `internal/tui/pr_cache_test.go`

**Interfaces:**
- Produces: `type prReland struct{ n int; path string; line int; diff bool }`, Model field `prReland *prReland`, set before the moved-head reopen and consumed by the next `compareFilesMsg` of that PR.

- [ ] **Step 1: Write the failing test**

```go
// After a moved-head reopen the files cursor is back on the same path, and an
// open diff reopens on the same file.
func TestMovedHeadKeepsTheFileAndLine(t *testing.T) {
	t.Parallel()
	m := openPRDiffModelWithFiles(t, 7, []string{"a.go", "b.go", "c.go"}) // cursor on b.go, a diff layer open at line 12
	m2, _ := m.handlePRRevalidatedMsg(prRevalidatedMsg{n: 7, gen: m.prsGen, moved: true, pr: model.PullRequest{Number: 7, State: "open"}})
	if m2.prReland == nil || m2.prReland.path != "b.go" || m2.prReland.line != 12 || !m2.prReland.diff {
		t.Fatalf("reland = %+v", m2.prReland)
	}
	m3, _ := m2.Update(compareFilesMsg{tag: m2.compareTag, files: []model.CommitFile{{Path: "a.go"}, {Path: "b.go"}, {Path: "d.go"}}})
	mm := m3.(Model)
	if sel := mm.selectedFilePath(); sel != "b.go" {
		t.Fatalf("cursor on %q after the reopen, want b.go", sel)
	}
	if mm.prReland != nil {
		t.Fatal("reland must be consumed once")
	}
}
```

(`selectedFilePath`: use the files view's existing "path under cursor" helper — `grep -n 'func (m Model) .*[Ss]elected.*Path' internal/tui/files_view.go`.)

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** — in `handlePRRevalidatedMsg`'s moved branch, before `m.openPRCmd(msg.pr)`: record `m.prReland = &prReland{n: msg.n, path: <files cursor path>, line: <diff cursor new-side line, 0 if no diff>, diff: m.diffLayer() != nil}`. In the `compareFilesMsg` arm, after `m.filesView.lines` is set: if `r := m.prReland; r != nil && m.openPRNumber() == r.n`, move the files cursor to the row whose path is `r.path` (the row stays unselected if the path left the PR), clear `m.prReland`, and if `r.diff` and the path is present, open the diff the way enter does on that row (`openDiffForFileLine(row)`) with the line parked as the diff's pending line (the `pendingLine` mechanism `open_file.go` uses for links; if the diff view has no pending-line field, add `pendingNewLine int` to `diffView` and land it in the diff's load handler, mirroring `landPendingLine`). The notice "PR #%d has new commits — updating…" stays.

- [ ] **Step 4: Run** `go test -race ./internal/tui/` → PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui
git commit -m "feat(tui): a PR head that moved updates the diff in place, keeping file and line"
```

---

### Task 10: TUI — prefetch after the list refresh

**Files:**
- Modify: `internal/tui/pr_panel.go` (`handlePRsLoaded`)
- Test: `internal/tui/pr_cache_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPRListRefreshSchedulesPrefetch(t *testing.T) {
	t.Parallel()
	m := newPRTestModel(t)
	m.cfg.Forge.Prefetch = intPtr(5)
	_, cmd := m.handlePRsLoaded(prsLoadedMsg{gen: m.prsGen, bg: true, status: domain.ForgeStatus{Provider: "github"}})
	if !cmdProduces[prPrefetchedMsg](t, cmd) {
		t.Fatal("a successful list refresh must schedule a prefetch")
	}
	zero := 0
	m.cfg.Forge.Prefetch = &zero
	_, cmd = m.handlePRsLoaded(prsLoadedMsg{gen: m.prsGen, bg: true, status: domain.ForgeStatus{Provider: "github"}})
	if cmdProduces[prPrefetchedMsg](t, cmd) {
		t.Fatal("prefetch = 0 must schedule nothing")
	}
}
```

(`cmdProduces` / `intPtr`: add small helpers if the test package lacks them; `cmdProduces` runs the cmd tree — batch-aware — and reports whether a message of type T comes out. Look for an existing batch-walking helper first: `grep -n 'func .*tea.BatchMsg' internal/tui/*_test.go`.)

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement** — at the end of `handlePRsLoaded`'s success path, when `m.cfg.Forge.PrefetchCount() > 0` and not `m.quiet` (memory: never-ending TUI cmds gated by quiet), add:

```go
type prPrefetchedMsg struct{ gen, n int }

func (m Model) prPrefetchCmd() tea.Cmd {
	svc, gen := m.svc, m.prsGen
	return func() tea.Msg {
		return prPrefetchedMsg{gen: gen, n: svc.PRPrefetch(context.Background())}
	}
}
```

The `prPrefetchedMsg` arm does nothing visible (the next open is simply a cache hit); it must not touch `m.loading`. Map nothing new in `opAffectedSources` (prefetch runs its fetch through domain, not `startOp`) — but confirm a prefetch fetch does not trip the "op running" UI: `Execute` from domain does not go through the TUI's op runner, so the PR list does not reload.

- [ ] **Step 4: Run** `go test -race ./internal/tui/` → PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui
git commit -m "feat(tui): prefetch recently opened PRs after the list refreshes"
```

---

### Task 11: web — cached list, cached open, one-call refresh, freshness mark

**Files:**
- Modify: `internal/web/prs.go` (list seed, `handlePROpen`, `handlePRRevalidate`, prefetch)
- Modify: `internal/web/prnotes.go` (comments refresh → revalidate)
- Modify: `internal/web/server.go` or wherever the web reads config (call `SetPRCachePolicy`)
- Modify: `internal/web/static/prs.js` (or the file holding the PR open / revalidate client code — `grep -ln 'pr/revalidate' internal/web/static/*.js`)
- Test: `internal/web/prs_test.go`; browser probe (scratchpad)

**Interfaces:**
- `POST /api/pr/revalidate` response gains `comments_changed` (bool) and `read_at` (RFC3339 string).
- `GET /api/prs` serves the cached listing at once when the in-memory list is empty and `svc.PullRequestsCached()` is fresh, with `"cached": true`; the background refresh replaces it and emits the existing `prs` live message.

- [ ] **Step 1: Write the failing Go tests** — follow the existing handler tests in `internal/web/prs_test.go` (`grep -n 'func Test' internal/web/prs_test.go`):
  - `TestPRsServesTheCachedListing`: seed a `prcache` store with a listing; a GET before any forge read returns the cached rows with `cached: true`.
  - `TestPRRevalidateReportsCommentChanges`: with a snapshot-capable fake provider, the first POST returns `comments_changed: true`, the second `false`, both with a `read_at`.
  - `TestPROpenUsesTheCache`: two Services over one cache dir; the second `/api/pr/open` response carries the same file count with zero `git diff (compare files)` runs (counting runner as in Task 6).

- [ ] **Step 2: Run** → FAIL.

- [ ] **Step 3: Implement**
  - `handlePROpen`: replace `PRPair` + `PreviewOpen` with `svc.PRPreview(ctx, pr)`; the response body is unchanged plus `"cached": r.Cached`.
  - `handlePRRevalidate`: add `comments_changed` and `read_at` to the JSON.
  - `handlePRCommentsRefresh` keeps its route (the page's existing re-poll calls it) but calls `PRRevalidate` and maps `CommentsChanged` → its existing `changed` field, so a poll is one forge call.
  - The listing loader (`refreshPRs`, the function around line 135): before the forge read, if `c.prs` is empty and `svc.PullRequestsCached()` is ok, publish those rows with state `ready` and `cached: true`, emit `prs`; after a successful read, run `go svc.PRPrefetch(context.Background())` when the policy allows (domain returns 0 when off).
  - Config: where the web Server loads `config` (search `config.Load` in `internal/web`), call `svc.SetPRCachePolicy(cfg.Forge.CacheMaxAge(), cfg.Forge.PrefetchCount())` for every Service it builds (standalone and hosted).
  - JS: after a revalidate answer, `comments_changed` triggers the same notes reload the comment poll triggers today; the PR view header shows "refreshing…" while the POST is in flight and "offline · read <age> ago" on failure (`read_at` → age), cleared on success. Use the existing escaping helper for any text.

- [ ] **Step 4: Run** `go test -race ./internal/web/` → PASS.

- [ ] **Step 5: Browser probe** (playwright, scratchpad; memory: playwright-web-verification, browser-check-must-assert-visibility): with the slow-gh wrapper (1 s per call) and a fixture PR, assert (a) a second `gg web` start shows the PR row before the first gh call returns, (b) opening the PR shows its files before the revalidate POST answers, (c) the header text "refreshing…" is VISIBLE during the POST and gone after. Run once against the unfixed build to see it fail.

- [ ] **Step 6: Commit**

```bash
gg add internal/web
git commit -m "feat(web): PRs draw from the cache, refresh in one call, show freshness"
```

---

### Task 12: docs, gates, review

**Files:**
- Modify: `CHANGELOG.md`, `README.md` (the `[forge]` settings + the faster PR opens), `CLAUDE.md` (one package-map row for `prcache`), `docs/CLAUDE-details.md` (the PR cache section: files, expiry, derived pairs, optimistic list, prefetch, the forgeMu rule)

- [ ] **Step 1: Write the docs.** CLAUDE.md row:

```
| `prcache`    | On-disk per-repo pull-request cache: the PR list + one JSON entry per recently opened PR (forge data, comments, derived git data per head/base pair), 50 entries by last open, `Fresh` = read-time expiry; temp+rename under the shared file lock, corrupt files quarantined. Owned by `domain`; frontends never import it. DAG leaf. |
```

- [ ] **Step 2: Run the gates**

Run: `./test.sh` then `./test.sh race` (memory: race gate is green ONLY when the log says "all green").
Expected: all green.

- [ ] **Step 3: Commit**

```bash
gg add CHANGELOG.md README.md CLAUDE.md docs/CLAUDE-details.md
git commit -m "docs: the PR cache"
```

- [ ] **Step 4: Final whole-branch review** — one read-only review subagent on the most capable model (CLAUDE.md allows review subagents), given the spec §2 and this plan; fix its findings in this session; re-run `./test.sh race`.

- [ ] **Step 5: Build a verify binary** (`go build -o bin/gg ./cmd/gg` in the worktree; memory: build-gg-in-worktree-for-testing) and give the user its absolute path for a manual check in a repo with PRs (babel): first open of a PR, quit, reopen within 8 h → instant; ask before merging.
