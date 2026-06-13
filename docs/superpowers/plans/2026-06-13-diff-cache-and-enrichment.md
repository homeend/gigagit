# Diff cache + intraline enrichment — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a generic injected in-memory LRU cache and use it to serve enriched (GitHub-style intraline) side-by-side diffs for commits without re-reading immutable blobs or recomputing.

**Architecture:** Three layers. `internal/cache` is a reusable LRU factory vended by injection. `internal/textdiff` stays pure and gains an `Enhanced` option that adds word-level intraline spans to `Changed` rows. `internal/domain` adds a `Differ` abstraction (plain + cached decorator over lazy byte-sources) returning a cacheable `Diff` outcome; the TUI loaders call it. A cache hit skips both the git reads and the alignment.

**Tech Stack:** Go 1.26, stdlib only (`container/list`, `sync`, `unicode`); lipgloss for render-time emphasis. Tests use a real `git` in `t.TempDir()` (`newRepoDir`) or `gitexec.FakeRunner`.

**Reference:** spec at `docs/superpowers/specs/2026-06-13-diff-cache-and-enrichment-design.md`. Build/test: `go build ./cmd/gg`, `./test.sh` (vet+gofmt → unit → e2e), `./test.sh race` before merge. Sibling Spec B (word-wrap) is out of scope.

**Task order & dependencies:** 1 (cache) → 2 (textdiff) → 3 (domain Differ, needs 1+2) → 4 (TUI loaders, needs 3) → 5 (TUI render, needs 2) → 6 (docs).

---

### Task 1: `internal/cache` — generic injected LRU factory

**Files:**
- Create: `internal/cache/cache.go`
- Test: `internal/cache/cache_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cache/cache_test.go`:

```go
package cache

import (
	"errors"
	"sync"
	"testing"
)

// sized is a test value reporting a fixed byte weight via cache.Sized.
type sized struct {
	id string
	n  int
}

func (s sized) Size() int { return s.n }

func TestGetOrLoadMissThenHit(t *testing.T) {
	c := NewFactory(0, 0).Cache("t")
	calls := 0
	load := func() (any, error) { calls++; return 42, nil }
	if v, _ := c.GetOrLoad("k", load); v.(int) != 42 {
		t.Fatalf("got %v", v)
	}
	if v, _ := c.GetOrLoad("k", load); v.(int) != 42 {
		t.Fatalf("got %v", v)
	}
	if calls != 1 {
		t.Fatalf("load called %d times, want 1 (second is a hit)", calls)
	}
}

func TestGetOrLoadErrorNotStored(t *testing.T) {
	c := NewFactory(0, 0).Cache("t")
	_, err := c.GetOrLoad("k", func() (any, error) { return nil, errors.New("boom") })
	if err == nil {
		t.Fatal("want error")
	}
	if _, ok := c.Get("k"); ok {
		t.Fatal("a failed load must not be cached")
	}
}

func TestEvictsLeastRecentlyUsedByCount(t *testing.T) {
	c := NewFactory(2, 0).Cache("t") // entry cap 2, default byte budget
	put := func(k string) { c.GetOrLoad(k, func() (any, error) { return k, nil }) }
	put("a")
	put("b")
	c.Get("a")       // a is now most-recently used; b is the LRU
	put("c")         // evicts b
	if _, ok := c.Get("b"); ok {
		t.Fatal("b should have been evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a should survive (it was used after insertion)")
	}
	if _, ok := c.Get("c"); !ok {
		t.Fatal("c should be present")
	}
	if c.Len() != 2 {
		t.Fatalf("Len = %d, want 2", c.Len())
	}
}

func TestEvictsOnByteBudget(t *testing.T) {
	// High entry cap, tight byte budget: weight, not count, drives eviction.
	c := NewFactory(1000, 100).Cache("t") // 100-byte budget
	put := func(id string, n int) {
		c.GetOrLoad(id, func() (any, error) { return sized{id: id, n: n}, nil })
	}
	put("a", 60)
	put("b", 60) // total would be 120 > 100 → evict a (the LRU)
	if _, ok := c.Get("a"); ok {
		t.Fatal("a should have been evicted by the byte budget")
	}
	if _, ok := c.Get("b"); !ok {
		t.Fatal("b should be present")
	}
}

func TestUnsizedValuesWeighOne(t *testing.T) {
	// Values without Size() weigh 1, so under a normal byte budget they're
	// bounded by entry count alone — no spurious byte-budget eviction.
	c := NewFactory(1000, 0).Cache("t") // default 64 MiB budget
	for _, k := range []string{"a", "b", "c"} {
		c.GetOrLoad(k, func() (any, error) { return k, nil })
	}
	if c.Len() != 3 {
		t.Fatalf("unsized values must not be byte-evicted under a normal budget; Len=%d want 3", c.Len())
	}
}

func TestFactoryNamesAreIsolated(t *testing.T) {
	f := NewFactory(0, 0)
	if f.Cache("x") != f.Cache("x") {
		t.Fatal("same name must return the same instance")
	}
	a, b := f.Cache("a"), f.Cache("b")
	a.GetOrLoad("k", func() (any, error) { return 1, nil })
	if _, ok := b.Get("k"); ok {
		t.Fatal("different names must not share entries")
	}
}

func TestLoadTypedRoundTrip(t *testing.T) {
	c := NewFactory(0, 0).Cache("t")
	v, err := Load[string](c, "k", func() (string, error) { return "hi", nil })
	if err != nil || v != "hi" {
		t.Fatalf("got %q, %v", v, err)
	}
}

func TestLoadTypeMismatchPanics(t *testing.T) {
	c := NewFactory(0, 0).Cache("t")
	c.GetOrLoad("k", func() (any, error) { return 7, nil }) // store an int
	defer func() {
		if recover() == nil {
			t.Fatal("Load[string] over an int entry must panic")
		}
	}()
	_, _ = Load[string](c, "k", func() (string, error) { return "x", nil })
}

func TestConcurrentAccess(t *testing.T) {
	c := NewFactory(8, 0).Cache("t")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := string(rune('a' + n%8))
			c.GetOrLoad(key, func() (any, error) { return n, nil })
			c.Get(key)
		}(i)
	}
	wg.Wait()
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cache/`
Expected: FAIL — build error, `cache` package has no `NewFactory`/`Load`.

- [ ] **Step 3: Implement the cache**

Create `internal/cache/cache.go`:

```go
// Package cache is a generic, injected, in-memory LRU cache. It lets
// expensive, content-addressable computations (currently side-by-side diffs;
// later commit lists, files-in-commits) be served without recomputation in
// very large repositories. Keys are caller-chosen strings (typically content
// hashes) — providing a correct, stable key is the caller's responsibility.
//
// Caches are vended by an injected Factory rather than a package singleton, so
// tests get a fresh, isolated cache and production wires exactly one Factory
// at the construction point.
package cache

import (
	"container/list"
	"fmt"
	"sync"
)

// Default per-cache bounds used when NewFactory is given non-positive values.
// The LRU is two-bound: it evicts when EITHER the entry count or the total
// byte weight is exceeded. Entry count alone does not bound memory when values
// vary widely in size; the byte budget is the hard memory ceiling.
const (
	defaultCapacity   = 1024
	defaultByteBudget = 64 << 20 // 64 MiB
)

// Cache is a concurrency-safe, bounded key→value store. A given cache holds one
// value type (Load panics on a type mismatch).
type Cache interface {
	// GetOrLoad returns the value for key, computing it via load on a miss and
	// storing the result. load runs OUTSIDE the lock, so two concurrent misses
	// on the same key may both compute (harmless for pure loads); the cache
	// never blocks unrelated keys behind one slow load. A load error is
	// returned and nothing is stored.
	GetOrLoad(key string, load func() (any, error)) (any, error)
	// Get peeks without loading; ok is false on a miss. A hit refreshes recency.
	Get(key string) (val any, ok bool)
	// Len reports the current entry count.
	Len() int
}

// Sized lets a cached value report its approximate heap weight in bytes for the
// byte budget. A value not implementing Sized weighs 1 (bounded by count only).
type Sized interface {
	Size() int
}

// Factory vends named caches; the same name returns the same instance, so
// independent consumers each get their own bounded LRU.
type Factory interface {
	Cache(name string) Cache
}

// NewFactory builds an in-memory Factory whose caches are two-bound: at most
// capacity entries AND at most byteBudget total bytes. Non-positive values use
// the defaults (defaultCapacity, defaultByteBudget).
func NewFactory(capacity, byteBudget int) Factory {
	if capacity <= 0 {
		capacity = defaultCapacity
	}
	if byteBudget <= 0 {
		byteBudget = defaultByteBudget
	}
	return &memFactory{capacity: capacity, byteBudget: byteBudget, caches: map[string]Cache{}}
}

type memFactory struct {
	capacity   int
	byteBudget int
	mu         sync.Mutex
	caches     map[string]Cache
}

func (f *memFactory) Cache(name string) Cache {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.caches[name]
	if !ok {
		c = newLRU(f.capacity, f.byteBudget)
		f.caches[name] = c
	}
	return c
}

// entry is one cached key/value held in the recency list, with its byte weight.
type entry struct {
	key    string
	val    any
	weight int
}

// weigh reports a value's byte weight: its Size() if it implements Sized, else 1.
func weigh(val any) int {
	if s, ok := val.(Sized); ok {
		if n := s.Size(); n > 0 {
			return n
		}
		return 1
	}
	return 1
}

type lru struct {
	capacity   int
	byteBudget int
	mu         sync.Mutex
	ll         *list.List               // front = most recently used
	items      map[string]*list.Element // key → element holding *entry
	bytes      int                      // sum of entry weights
}

func newLRU(capacity, byteBudget int) *lru {
	return &lru{capacity: capacity, byteBudget: byteBudget, ll: list.New(), items: map[string]*list.Element{}}
}

func (c *lru) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*entry).val, true
	}
	return nil, false
}

func (c *lru) GetOrLoad(key string, load func() (any, error)) (any, error) {
	if v, ok := c.Get(key); ok {
		return v, nil
	}
	v, err := load()
	if err != nil {
		return nil, err
	}
	c.store(key, v)
	return v, nil
}

func (c *lru) store(key string, val any) {
	w := weigh(val)
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok { // a concurrent miss already stored: refresh
		e := el.Value.(*entry)
		c.bytes += w - e.weight
		e.val, e.weight = val, w
		c.ll.MoveToFront(el)
		c.evict()
		return
	}
	c.items[key] = c.ll.PushFront(&entry{key: key, val: val, weight: w})
	c.bytes += w
	c.evict()
}

// evict drops the LRU tail while either bound is exceeded, always keeping at
// least one entry (so a lone over-budget value is still served).
func (c *lru) evict() {
	for c.ll.Len() > 1 && (c.ll.Len() > c.capacity || c.bytes > c.byteBudget) {
		back := c.ll.Back()
		if back == nil {
			return
		}
		e := back.Value.(*entry)
		c.ll.Remove(back)
		delete(c.items, e.key)
		c.bytes -= e.weight
	}
}

func (c *lru) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// Load is a typed view over a Cache for value type V. A cached value of a
// different concrete type is a programming error and panics.
func Load[V any](c Cache, key string, load func() (V, error)) (V, error) {
	v, err := c.GetOrLoad(key, func() (any, error) {
		val, e := load()
		return val, e
	})
	if err != nil {
		var zero V
		return zero, err
	}
	tv, ok := v.(V)
	if !ok {
		panic(fmt.Sprintf("cache: key %q holds %T, want %T", key, v, tv))
	}
	return tv, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cache/` then `go test -race ./internal/cache/`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add internal/cache/
git commit -m "feat(cache): generic injected in-memory LRU factory

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: `internal/textdiff` — intraline word enrichment

**Files:**
- Modify: `internal/textdiff/textdiff.go` (add `Options`, `Span`, `Row` span fields, change `Compare` signature, add enrichment)
- Modify: `internal/textdiff/textdiff_test.go` (update every `Compare` call; add span tests)
- Modify: `internal/tui/diff_view.go:200` (the one production `Compare` caller — pass `Options{}` to keep the build green; Task 4 rewrites this fully)

- [ ] **Step 1: Write the failing tests**

Add to `internal/textdiff/textdiff_test.go` (and, in this same step, update **every existing** `Compare(a, b)` call in this file to `Compare(a, b, Options{})` so the file compiles):

```go
func TestEnhancedSingleChangedWord(t *testing.T) {
	// "foo(a, b)" vs "foo(a, c)" → one Changed row; only b/c differ.
	res := Compare([]byte("foo(a, b)\n"), []byte("foo(a, c)\n"), Options{Enhanced: true})
	var row *Row
	for i := range res.Rows {
		if res.Rows[i].Kind == Changed {
			row = &res.Rows[i]
		}
	}
	if row == nil {
		t.Fatal("expected a Changed row")
	}
	// 'b' and 'c' are at rune index 7 in "foo(a, b)" / "foo(a, c)".
	wantL := []Span{{Start: 7, End: 8}}
	wantR := []Span{{Start: 7, End: 8}}
	if !equalSpans(row.LeftSpans, wantL) {
		t.Fatalf("LeftSpans = %v, want %v", row.LeftSpans, wantL)
	}
	if !equalSpans(row.RightSpans, wantR) {
		t.Fatalf("RightSpans = %v, want %v", row.RightSpans, wantR)
	}
}

func TestEnhancedMergesAdjacentChangedTokens(t *testing.T) {
	// "a+b" vs "a" → tokens [a][+][b] vs [a]; "+" and "b" both deleted and
	// touch, so they merge into one left span [1,3); right has none.
	res := Compare([]byte("a+b\n"), []byte("a\n"), Options{Enhanced: true})
	row := changedRow(t, res)
	wantL := []Span{{Start: 1, End: 3}}
	if !equalSpans(row.LeftSpans, wantL) {
		t.Fatalf("LeftSpans = %v, want %v", row.LeftSpans, wantL)
	}
	if len(row.RightSpans) != 0 {
		t.Fatalf("RightSpans = %v, want none", row.RightSpans)
	}
}

func TestEnhancedOnlyMarksChangedRows(t *testing.T) {
	// A pure add and a pure delete must carry no spans.
	res := Compare([]byte("keep\nold\n"), []byte("keep\n"), Options{Enhanced: true})
	for _, r := range res.Rows {
		if r.Kind != Changed && (len(r.LeftSpans) > 0 || len(r.RightSpans) > 0) {
			t.Fatalf("non-Changed row %+v carries spans", r)
		}
	}
}

func TestNotEnhancedHasNoSpans(t *testing.T) {
	plain := Compare([]byte("foo(a, b)\n"), []byte("foo(a, c)\n"), Options{})
	for _, r := range plain.Rows {
		if len(r.LeftSpans) > 0 || len(r.RightSpans) > 0 {
			t.Fatalf("plain Compare must not produce spans: %+v", r)
		}
	}
	// And the rest of the Result matches the enhanced run row-for-row in Kind.
	enh := Compare([]byte("foo(a, b)\n"), []byte("foo(a, c)\n"), Options{Enhanced: true})
	if len(plain.Rows) != len(enh.Rows) {
		t.Fatalf("row count differs: %d vs %d", len(plain.Rows), len(enh.Rows))
	}
	for i := range plain.Rows {
		if plain.Rows[i].Kind != enh.Rows[i].Kind {
			t.Fatalf("row %d Kind differs", i)
		}
	}
}

// helpers
func equalSpans(a, b []Span) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func changedRow(t *testing.T, res Result) *Row {
	t.Helper()
	for i := range res.Rows {
		if res.Rows[i].Kind == Changed {
			return &res.Rows[i]
		}
	}
	t.Fatal("expected a Changed row")
	return nil
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/textdiff/`
Expected: FAIL — `Compare` takes 2 args (signature mismatch), `Options`/`Span`/`LeftSpans` undefined.

- [ ] **Step 3: Implement the option, spans, and enrichment**

In `internal/textdiff/textdiff.go`:

a) Add `"unicode"` to the imports (keep `bytes`, `strings`).

b) Add the `Span` type and span fields to `Row`. Replace the existing `Row` struct (lines ~22-29) with:

```go
// Row is one display row of the side-by-side view. Line numbers are 1-based;
// 0 means "no line on that side" (the gap cell of a Del/Add row). LeftSpans
// and RightSpans mark intraline differences; they are populated only on
// Changed rows under Options.Enhanced, nil otherwise.
type Row struct {
	Kind       Kind
	Left       string
	Right      string
	LeftNo     int
	RightNo    int
	LeftSpans  []Span
	RightSpans []Span
}

// Span is a half-open rune range [Start, End) into a row's raw Left/Right
// string, marking text that differs from the other side. Rune offsets, not
// bytes — the renderer maps them to display columns.
type Span struct {
	Start, End int
}

// Options tunes a comparison. The zero value is the plain line-level diff;
// Enhanced additionally computes intraline word-level spans on Changed rows.
type Options struct {
	Enhanced bool
}
```

c) Change `Compare`'s signature and add the enrichment call. The signature line (~134) becomes:

```go
func Compare(old, newB []byte, opts Options) Result {
```

and just before the final `return Result{Rows: rows, Blocks: blocks, Truncated: truncated}` (~196), insert:

```go
	if opts.Enhanced {
		enrich(rows)
	}
```

d) Append the enrichment engine to the file:

```go
// enrich fills intraline spans on every Changed row by word-diffing its two
// sides. Other row kinds are untouched. A myers give-up leaves spans nil — the
// row still renders, just without emphasis.
func enrich(rows []Row) {
	for i := range rows {
		if rows[i].Kind != Changed {
			continue
		}
		rows[i].LeftSpans, rows[i].RightSpans = wordSpans(rows[i].Left, rows[i].Right)
	}
}

// token is a maximal run of word runes, or a maximal run of non-word runes,
// tagged with its rune offset range [start,end) in the source line.
type token struct {
	text       string
	start, end int
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func tokenize(s string) []token {
	var toks []token
	runes := []rune(s)
	for i := 0; i < len(runes); {
		w := isWordRune(runes[i])
		j := i + 1
		for j < len(runes) && isWordRune(runes[j]) == w {
			j++
		}
		toks = append(toks, token{text: string(runes[i:j]), start: i, end: j})
		i = j
	}
	return toks
}

// wordSpans word-diffs left vs right and returns the differing rune ranges on
// each side, adjacent differing tokens merged into one span.
func wordSpans(left, right string) (leftSpans, rightSpans []Span) {
	lt, rt := tokenize(left), tokenize(right)
	la := make([]string, len(lt))
	for i, t := range lt {
		la[i] = t.text
	}
	ra := make([]string, len(rt))
	for i, t := range rt {
		ra[i] = t.text
	}
	script, ok := myers(la, ra)
	if !ok {
		return nil, nil
	}
	li, ri := 0, 0
	for _, op := range script {
		switch op {
		case opEq:
			li++
			ri++
		case opDel:
			leftSpans = appendSpan(leftSpans, lt[li].start, lt[li].end)
			li++
		case opAdd:
			rightSpans = appendSpan(rightSpans, rt[ri].start, rt[ri].end)
			ri++
		}
	}
	return leftSpans, rightSpans
}

// appendSpan adds [start,end), merging with the previous span when they touch.
func appendSpan(spans []Span, start, end int) []Span {
	if n := len(spans); n > 0 && spans[n-1].End == start {
		spans[n-1].End = end
		return spans
	}
	return append(spans, Span{Start: start, End: end})
}
```

e) Update the one production caller so the package still builds: in `internal/tui/diff_view.go`, the line `res := textdiff.Compare(oldB, newB)` becomes `res := textdiff.Compare(oldB, newB, textdiff.Options{})`. (Task 4 replaces this whole function; this is a temporary keep-it-green edit.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/textdiff/ ./internal/tui/` and `gofmt -l internal/textdiff/textdiff.go`
Expected: PASS; gofmt prints nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/textdiff/ internal/tui/diff_view.go
git commit -m "feat(textdiff): word-level intraline enrichment behind Options.Enhanced

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: `internal/domain` — the Differ abstraction

**Files:**
- Create: `internal/domain/differ.go`
- Test: `internal/domain/differ_test.go`
- Modify: `internal/domain/service.go` (add `factory`/`differ` fields, set factory in `New`, add `Differ()` accessor)

- [ ] **Step 1: Write the failing tests**

Create `internal/domain/differ_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/gigagit/gg/internal/cache"
	"github.com/gigagit/gg/internal/textdiff"
)

func src(b []byte) ByteSource {
	return func(context.Context) ([]byte, error) { return b, nil }
}

func TestPlainDifferComputes(t *testing.T) {
	d := NewDiffer(DifferOptions{Enhanced: true}, nil)
	out, err := d.Diff(context.Background(), Request{Old: src([]byte("a\n")), New: src([]byte("b\n"))})
	if err != nil {
		t.Fatal(err)
	}
	if out.Binary || out.TooLarge {
		t.Fatalf("unexpected flags: %+v", out)
	}
	if len(out.Result.Blocks) != 1 {
		t.Fatalf("blocks = %v, want one change", out.Result.Blocks)
	}
}

func TestPlainDifferNilSourceIsAbsentSide(t *testing.T) {
	d := NewDiffer(DifferOptions{}, nil)
	out, err := d.Diff(context.Background(), Request{Old: nil, New: src([]byte("x\ny\n"))})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Result.Rows) != 2 {
		t.Fatalf("nil old side should diff as all-add, got %d rows", len(out.Result.Rows))
	}
	for _, r := range out.Result.Rows {
		if r.Kind != textdiff.Add {
			t.Fatalf("nil old side: every row should be Add, got %+v", r)
		}
	}
}

func TestPlainDifferBinary(t *testing.T) {
	d := NewDiffer(DifferOptions{}, nil)
	out, _ := d.Diff(context.Background(), Request{Old: src([]byte("\x00\x01")), New: src([]byte("ok\n"))})
	if !out.Binary {
		t.Fatal("a NUL byte must yield Binary")
	}
}

func TestPlainDifferTooLarge(t *testing.T) {
	big := make([]byte, MaxDiffBytes+1)
	d := NewDiffer(DifferOptions{}, nil)
	out, _ := d.Diff(context.Background(), Request{Old: src(nil), New: src(big)})
	if !out.TooLarge {
		t.Fatal("an oversized side must yield TooLarge")
	}
}

func TestDiffSizeCountsRowText(t *testing.T) {
	d := NewDiffer(DifferOptions{}, nil)
	out, _ := d.Diff(context.Background(), Request{Old: src([]byte("abc\n")), New: src([]byte("abxy\n"))})
	if out.Size() <= 0 {
		t.Fatalf("a non-empty diff must report positive Size, got %d", out.Size())
	}
	// Binary/too-large outcomes hold no rows → near-zero weight.
	bin := Diff{Binary: true}
	if bin.Size() != 0 {
		t.Fatalf("binary outcome Size = %d, want 0", bin.Size())
	}
}

func TestPlainDifferSourceErrorPropagates(t *testing.T) {
	d := NewDiffer(DifferOptions{}, nil)
	fail := func(context.Context) ([]byte, error) { return nil, errors.New("boom") }
	if _, err := d.Diff(context.Background(), Request{Old: fail, New: src(nil)}); err == nil {
		t.Fatal("source error must propagate")
	}
}

func TestCachedServesWithoutReinvokingSources(t *testing.T) {
	c := cache.NewFactory(0, 0).Cache("diff")
	d := NewDiffer(DifferOptions{Enhanced: true, Cached: true}, c)
	calls := 0
	counting := func(context.Context) ([]byte, error) { calls++; return []byte("a\n"), nil }
	req := Request{Key: "k", Old: counting, New: src([]byte("b\n"))}
	d.Diff(context.Background(), req)
	d.Diff(context.Background(), req)
	if calls != 1 {
		t.Fatalf("source invoked %d times, want 1 (second served from cache)", calls)
	}
}

func TestCachedEmptyKeyNeverCaches(t *testing.T) {
	c := cache.NewFactory(0, 0).Cache("diff")
	d := NewDiffer(DifferOptions{Cached: true}, c)
	calls := 0
	counting := func(context.Context) ([]byte, error) { calls++; return []byte("a\n"), nil }
	req := Request{Key: "", Old: counting, New: src([]byte("b\n"))}
	d.Diff(context.Background(), req)
	d.Diff(context.Background(), req)
	if calls != 2 {
		t.Fatalf("Key=='' must not cache; source invoked %d times, want 2", calls)
	}
}

func TestCachedQualityNamespacing(t *testing.T) {
	c := cache.NewFactory(0, 0).Cache("diff")
	enh := NewDiffer(DifferOptions{Enhanced: true, Cached: true}, c)
	plain := NewDiffer(DifferOptions{Enhanced: false, Cached: true}, c)
	calls := 0
	counting := func(context.Context) ([]byte, error) { calls++; return []byte("a\n"), nil }
	enh.Diff(context.Background(), Request{Key: "k", Old: counting, New: src([]byte("b\n"))})
	plain.Diff(context.Background(), Request{Key: "k", Old: counting, New: src([]byte("b\n"))})
	if calls != 2 {
		t.Fatalf("enhanced and plain must not collide on the same key; calls=%d want 2", calls)
	}
}

func TestServiceDifferShareCache(t *testing.T) {
	// Differ() never touches the repo, so a nil repo is fine here. Two calls
	// must share one cache, so a diff cached via the first is served via the
	// second without re-invoking the source.
	s := New(nil)
	calls := 0
	counting := func(context.Context) ([]byte, error) { calls++; return []byte("a\n"), nil }
	req := Request{Key: "k", Old: counting, New: src([]byte("b\n"))}
	if _, err := s.Differ().Diff(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Differ().Diff(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("the Service's Differ must share one cache; source called %d times, want 1", calls)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/domain/`
Expected: FAIL — `NewDiffer`, `Request`, `ByteSource`, `MaxDiffBytes`, `Service.Differ` undefined.

- [ ] **Step 3: Implement the Differ**

Create `internal/domain/differ.go`:

```go
package domain

import (
	"context"

	"github.com/gigagit/gg/internal/cache"
	"github.com/gigagit/gg/internal/textdiff"
)

// MaxDiffBytes caps each side fed to the comparison engine; a larger side is
// reported as TooLarge instead of being aligned.
const MaxDiffBytes = 10 << 20

// ByteSource lazily yields one side's content; invoked only on a cache miss. A
// nil ByteSource means an absent side (new or deleted file), matching
// textdiff.Compare(nil, x) / Compare(x, nil).
type ByteSource func(context.Context) ([]byte, error)

// Request is one diff to compute. Key is the cache key; "" disables caching
// for this call (e.g. working-tree diffs).
type Request struct {
	Key      string
	Old, New ByteSource
}

// Diff is the cacheable outcome of one comparison. For a commit diff every
// field is immutable, so the whole struct is cached — including the
// binary/too-large verdicts, which avoids re-reading the blob to re-detect
// them on a later open.
type Diff struct {
	Result   textdiff.Result // valid unless Binary or TooLarge
	Binary   bool
	TooLarge bool
}

// Size implements cache.Sized: the diff's approximate heap weight in bytes for
// the cache byte budget — the row text on both sides (the dominant cost) plus
// a small per-row overhead. Binary/too-large outcomes hold no rows.
//
// The cached rows are shared across every cache hit (the loader aliases them
// into the view); treat them as READ-ONLY — an in-place mutation of a cached
// Row would corrupt the cache for all later opens.
func (d Diff) Size() int {
	n := 0
	for _, r := range d.Result.Rows {
		n += len(r.Left) + len(r.Right) + 48 // 48 ≈ Row + slice-header overhead
	}
	return n
}

// Differ computes an aligned diff, possibly from cache.
type Differ interface {
	Diff(ctx context.Context, req Request) (Diff, error)
}

// DifferOptions selects quality and caching at construction.
type DifferOptions struct {
	Enhanced bool // produce intraline spans
	Cached   bool // wrap in the caching decorator
}

// NewDiffer composes a Differ: a plainDiffer(Enhanced) optionally wrapped by a
// caching decorator over c (c may be nil when Cached is false).
func NewDiffer(opts DifferOptions, c cache.Cache) Differ {
	var d Differ = plainDiffer{enhanced: opts.Enhanced}
	if opts.Cached {
		d = cachedDiffer{inner: d, cache: c, enhanced: opts.Enhanced}
	}
	return d
}

type plainDiffer struct{ enhanced bool }

func (d plainDiffer) Diff(ctx context.Context, req Request) (Diff, error) {
	old, err := readSource(ctx, req.Old)
	if err != nil {
		return Diff{}, err
	}
	newB, err := readSource(ctx, req.New)
	if err != nil {
		return Diff{}, err
	}
	if len(old) > MaxDiffBytes || len(newB) > MaxDiffBytes {
		return Diff{TooLarge: true}, nil
	}
	if textdiff.IsBinary(old) || textdiff.IsBinary(newB) {
		return Diff{Binary: true}, nil
	}
	return Diff{Result: textdiff.Compare(old, newB, textdiff.Options{Enhanced: d.enhanced})}, nil
}

func readSource(ctx context.Context, s ByteSource) ([]byte, error) {
	if s == nil {
		return nil, nil
	}
	return s(ctx)
}

type cachedDiffer struct {
	inner    Differ
	cache    cache.Cache
	enhanced bool
}

func (d cachedDiffer) Diff(ctx context.Context, req Request) (Diff, error) {
	if req.Key == "" { // uncacheable (e.g. working-tree diff): compute directly
		return d.inner.Diff(ctx, req)
	}
	qkey := "p:" + req.Key
	if d.enhanced {
		qkey = "e:" + req.Key
	}
	return cache.Load[Diff](d.cache, qkey, func() (Diff, error) {
		return d.inner.Diff(ctx, req)
	})
}
```

- [ ] **Step 4: Wire the Service**

In `internal/domain/service.go`:

a) Add `"github.com/gigagit/gg/internal/cache"` to imports.

b) Add two fields to the `Service` struct (after `flight flightGroup`):

```go
	factory cache.Factory // vends the diff (and future) caches
	differ  Differ        // memoized production diff engine
```

c) In `New`, construct the factory:

```go
func New(repo *git.Repo) *Service {
	return &Service{repo: repo, factory: cache.NewFactory(0, 0)}
}
```

(`Open` already calls `New`, so it inherits the factory — no change there.)

d) Add the accessor after `Repo()`:

```go
// Differ returns this Service's diff engine: enhanced (intraline) and cached,
// over the Service's "diff" cache. Built once, lazily, under the Service lock.
func (s *Service) Differ() Differ {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.differ == nil {
		s.differ = NewDiffer(DifferOptions{Enhanced: true, Cached: true}, s.factory.Cache("diff"))
	}
	return s.differ
}
```

> If any code constructs `&Service{...}` as a struct literal (not via `New`),
> it would have a nil `factory`. Verify with `grep -rn "Service{" internal/`;
> the only constructor should be `New`. If a literal exists, route it through
> `New` or set `factory: cache.NewFactory(0, 0)`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/domain/ ./internal/cache/ ./internal/textdiff/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/
git commit -m "feat(domain): Differ abstraction (plain/cached) over lazy byte-sources

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: TUI loaders call the Differ

**Files:**
- Modify: `internal/tui/diff_view.go` (rewrite both loaders to build `domain.Request` and call the Differ; replace `fillDiff` with `applyDiff`; add `diffDiffer` helper; drop the local `maxDiffBytes` const in favor of `domain.MaxDiffBytes`)
- Test: `internal/tui/diff_view_test.go` (add the whole-stack caching test; existing loader tests keep passing via the nil-`svc` fallback)

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/diff_view_test.go`:

```go
func TestCommitDiffSecondOpenServedFromCache(t *testing.T) {
	dir, repo := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "f.txt")
	gitIn(t, dir, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\nTWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-am", "edit")
	hash := gitOut(t, dir, "rev-parse", "HEAD")

	m := diffModel()
	m.repo = repo
	m.svc = domain.New(repo) // a real Service so both opens share one cache
	line := contentLine{path: "f.txt", status: "M"}

	first := m.loadCommitDiffCmd(hash, line)().(diffMsg)
	if first.view.err != nil {
		t.Fatalf("first open: %v", first.view.err)
	}
	wantBlocks := len(first.view.blocks)

	// Break git: a bare FakeRunner has no responses configured, so any
	// ShowFile now errors. If the second open hits the cache, the broken repo
	// is never consulted and the result still arrives.
	m.repo = &git.Repo{Runner: gitexec.NewFakeRunner()}

	second := m.loadCommitDiffCmd(hash, line)().(diffMsg)
	if second.view.err != nil {
		t.Fatalf("second open should be served from cache, got err: %v", second.view.err)
	}
	if len(second.view.blocks) != wantBlocks {
		t.Fatalf("cached result differs: blocks %d vs %d", len(second.view.blocks), wantBlocks)
	}
}
```

> Imports this test needs (add if missing to the file's import block):
> `"github.com/gigagit/gg/internal/domain"`, `"github.com/gigagit/gg/internal/gitexec"`,
> `"github.com/gigagit/gg/internal/git"`.
> `newRepoDir`, `gitIn`, `gitOut`, `diffModel`, `contentLine` already exist in the tui test package.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestCommitDiffSecondOpenServedFromCache`
Expected: FAIL — second open errors (no caching yet; the broken repo is consulted).

- [ ] **Step 3: Rewrite the loaders and helpers**

In `internal/tui/diff_view.go`:

a) Update the imports: add `"github.com/gigagit/gg/internal/domain"`. Keep `context`, `errors`, `io/fs`, `os`, `path/filepath`, `strings`, `model`, `textdiff`, `tea`.

b) **Delete** the `maxDiffBytes` const block (lines ~17-19) — its single use moves to `domain.MaxDiffBytes`.

c) Add the differ accessor (mirrors `op.go`'s nil-`svc` fallback for test fixtures):

```go
// diffDiffer returns the Service's diff engine, falling back to a fresh one
// for models built directly in tests (svc nil). The fallback's cache is
// per-call, so only a shared Service caches across opens — which is exactly
// the production path.
func (m Model) diffDiffer() domain.Differ {
	svc := m.svc
	if svc == nil {
		svc = domain.New(m.repo)
	}
	return svc.Differ()
}
```

d) Replace `loadStatusDiffCmd` (the whole function) with:

```go
func (m Model) loadStatusDiffCmd(f model.FileStatus) tea.Cmd {
	repo := m.repo
	differ := m.diffDiffer()
	root := m.currentWorktree
	body := m.diffBodyRows()
	tag := "status:" + f.Path
	v := &diffView{title: f.Path, context: "HEAD → working tree", partial: m.diffPartial}

	// Old side: absent when the file isn't in HEAD (untracked, or staged-new
	// 'A'). Renames fetch the old name.
	var oldSrc domain.ByteSource
	if f.Kind != model.KindUntracked && f.Staged != 'A' {
		p := f.Path
		if f.OrigPath != "" {
			p = f.OrigPath
		}
		oldSrc = func(ctx context.Context) ([]byte, error) { return repo.ShowFile(ctx, "HEAD", p) }
	}
	full := filepath.Join(root, f.Path)

	return func() tea.Msg {
		// New side: the working file. Stat first to size-guard without reading
		// a giant file into memory; not-exists means deleted (absorbs the
		// delete/re-create porcelain combinations and races).
		var newSrc domain.ByteSource
		switch st, err := os.Stat(full); {
		case err == nil && st.Size() > domain.MaxDiffBytes:
			v.tooLarge = true
			return diffMsg{tag: tag, view: v}
		case err == nil:
			newSrc = func(ctx context.Context) ([]byte, error) {
				b, rerr := os.ReadFile(full)
				if rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
					return nil, rerr
				}
				return b, nil // ErrNotExist ⇒ nil ⇒ deleted
			}
		case !errors.Is(err, fs.ErrNotExist):
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		// Working-tree diffs are never cached (Key: "").
		out, err := differ.Diff(context.Background(), domain.Request{Key: "", Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
}
```

e) Replace `fillDiff` (the whole function) with `applyDiff`:

```go
// applyDiff maps a domain.Diff outcome onto the view: size/binary state, or
// the aligned rows plus the open-at-first-difference jump.
func applyDiff(v *diffView, out domain.Diff, body int) {
	switch {
	case out.TooLarge:
		v.tooLarge = true
	case out.Binary:
		v.binary = true
	default:
		v.full = out.Result.Rows
		v.fullBlocks = out.Result.Blocks
		v.truncated = out.Result.Truncated
		v.rebuild()
		if len(v.blocks) > 0 {
			v.jumpTo(v.blocks[0], body)
		}
	}
}
```

f) Replace `loadCommitDiffCmd` (the whole function) with:

```go
func (m Model) loadCommitDiffCmd(hash string, line contentLine) tea.Cmd {
	repo := m.repo
	differ := m.diffDiffer()
	body := m.diffBodyRows()
	tag := "commit:" + hash + ":" + line.path
	v := &diffView{title: line.path, context: "@ " + strings.TrimPrefix(m.filesTitle, "Files "), partial: m.diffPartial}
	// Immutable: parent(hash)→hash for a path always yields the same bytes.
	key := hash + "^.." + hash + ":" + line.path

	var oldSrc, newSrc domain.ByteSource
	if line.status != "A" {
		p := line.path
		if line.oldPath != "" {
			p = line.oldPath
		}
		oldSrc = func(ctx context.Context) ([]byte, error) { return repo.ShowFile(ctx, hash+"^", p) }
	}
	if line.status != "D" {
		newSrc = func(ctx context.Context) ([]byte, error) { return repo.ShowFile(ctx, hash, line.path) }
	}
	return func() tea.Msg {
		out, err := differ.Diff(context.Background(), domain.Request{Key: key, Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
}
```

> The `import "github.com/gigagit/gg/internal/textdiff"` in this file is no
> longer used by the loaders directly — but leave it only if something else in
> the file references it. After this edit, run `goimports`/`gofmt`; if vet flags
> an unused import, remove `textdiff` from `diff_view.go`'s imports. (`diff_render.go`
> still imports it.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/` then `go build ./cmd/gg`
Expected: PASS, build clean. The existing loader tests pass via the nil-`svc` fallback; the new caching test passes.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/diff_view.go internal/tui/diff_view_test.go
git commit -m "feat(tui): diff loaders serve commit diffs through the cached Differ

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: TUI render — intraline emphasis

**Files:**
- Modify: `internal/tui/diff_render.go` (add the emphasis style, a span-aware cell body, and thread row spans into `diffCell`)
- Test: `internal/tui/diff_render_test.go` (raw→display mapping helpers; enriched-render smoke test; existing render tests must pass unchanged — that proves plain-mode byte-identity)

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/diff_render_test.go`:

```go
func TestSanitizeSpansMapsThroughTabExpansion(t *testing.T) {
	// "\tx" — a leading tab expands to 4 spaces; the span over 'x' (raw rune
	// index 1) must land on display column 4, not column 1.
	disp, emph := sanitizeSpans("\tx", []textdiff.Span{{Start: 1, End: 2}})
	if string(disp) != "    x" {
		t.Fatalf("disp = %q, want %q", string(disp), "    x")
	}
	want := []bool{false, false, false, false, true}
	for i := range want {
		if emph[i] != want[i] {
			t.Fatalf("emph = %v, want %v", emph, want)
		}
	}
}

func TestSanitizeSpansControlCharBecomesDot(t *testing.T) {
	disp, _ := sanitizeSpans("a\x01b", nil)
	if string(disp) != "a·b" {
		t.Fatalf("disp = %q, want %q", string(disp), "a·b")
	}
}

func TestCoverMaskClampsEnds(t *testing.T) {
	m := coverMask(3, []textdiff.Span{{Start: 1, End: 99}})
	want := []bool{false, true, true}
	for i := range want {
		if m[i] != want[i] {
			t.Fatalf("mask = %v, want %v", m, want)
		}
	}
}

func TestEmphasisActuallyChangesOutput(t *testing.T) {
	// A cheap check that the emphasis style lands: the same hot cell rendered
	// with a span differs from the same cell with no span (which takes the
	// original, byte-identical path).
	emph := diffCell(1, "foobar", 3, 20, false, true, diffDelCell, []textdiff.Span{{Start: 0, End: 3}})
	plain := diffCell(1, "foobar", 3, 20, false, true, diffDelCell, nil)
	if emph == plain {
		t.Fatal("an emphasized render must differ from the plain hot render")
	}
	if lipgloss.Width(emph) != lipgloss.Width(plain) {
		t.Fatalf("emphasis must not change visible width: %d vs %d", lipgloss.Width(emph), lipgloss.Width(plain))
	}
}

func TestEnrichedRowRendersWithoutBreakingWidth(t *testing.T) {
	// A Changed row carrying spans must still render as left│right at full
	// width and not panic.
	v := &diffView{
		full: []textdiff.Row{{
			Kind: textdiff.Changed, Left: "foo a", Right: "foo b",
			LeftNo: 1, RightNo: 1,
			LeftSpans: []textdiff.Span{{Start: 4, End: 5}}, RightSpans: []textdiff.Span{{Start: 4, End: 5}},
		}},
		fullBlocks: []int{0},
	}
	v.rebuild()
	m := footerModel()
	// A row renders as left│right = 2*((w-1)/2)+1 columns wide. For w=41 that
	// is 41 (paneW=20 each side + the separator).
	const w = 41
	lines := m.diffPaneLines(v, w, 1)
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d", len(lines))
	}
	if lipgloss.Width(lines[0]) != w {
		t.Fatalf("enriched row width = %d, want %d", lipgloss.Width(lines[0]), w)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'SanitizeSpans|CoverMask|EnrichedRow'`
Expected: FAIL — `sanitizeSpans`, `coverMask` undefined.

- [ ] **Step 3: Implement the span-aware renderer**

In `internal/tui/diff_render.go`:

a) Add the emphasis style to the `var (...)` block:

```go
	diffEmph = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")) // bright fg over the hot cell bg
```

b) Thread spans into the two `diffCell` calls in `diffPaneLines`. Replace the existing `left :=`/`right :=` assignments with:

```go
		left := diffCell(r.LeftNo, r.Left, gut, paneW,
			r.Kind == textdiff.Add,
			r.Kind == textdiff.Del || r.Kind == textdiff.Changed, diffDelCell, r.LeftSpans)
		right := diffCell(r.RightNo, r.Right, gut, paneW,
			r.Kind == textdiff.Del,
			r.Kind == textdiff.Add || r.Kind == textdiff.Changed, diffAddCell, r.RightSpans)
```

c) Replace `diffCell` (the whole function) with the span-aware version plus its helpers:

```go
// diffCell renders one pane cell: gutter + text, or the dim gap filler. With
// no spans (plain mode, non-Changed rows, or enrichment give-up) it is
// byte-identical to the pre-enrichment renderer; with spans it layers
// intraline emphasis over the hot cell background.
func diffCell(no int, text string, gut, width int, gap, hot bool, hotStyle lipgloss.Style, spans []textdiff.Span) string {
	if gap {
		return diffGapCell.Render(strings.Repeat("·", width))
	}
	if gut > width-2 { // degenerate pane: keep the cell inside its width
		gut = width - 2
		if gut < 1 {
			gut = 1
		}
	}
	num := fmt.Sprintf("%*d ", gut, no)
	tw := width - gut - 1
	if tw < 1 {
		tw = 1
	}
	var bodyTxt string
	if hot && len(spans) > 0 {
		bodyTxt = hotEmphBody(text, spans, tw, hotStyle)
	} else {
		bodyTxt = padRight(truncate(sanitizeLine(text), tw), tw)
		if hot {
			bodyTxt = hotStyle.Render(bodyTxt)
		}
	}
	return diffGutter.Render(truncate(num, gut+1)) + bodyTxt
}

// hotEmphBody renders a Changed cell's text into a tw-column body: sanitized
// like sanitizeLine, the whole cell carrying hotStyle, with the runes whose
// raw index falls in a span additionally wearing diffEmph. Truncation mirrors
// truncate()'s trailing ellipsis.
func hotEmphBody(text string, spans []textdiff.Span, tw int, hotStyle lipgloss.Style) string {
	disp, emph := sanitizeSpans(text, spans)
	if lipgloss.Width(string(disp)) <= tw {
		body := styledRuns(disp, emph, hotStyle)
		if pad := tw - lipgloss.Width(string(disp)); pad > 0 {
			body += hotStyle.Render(strings.Repeat(" ", pad))
		}
		return body
	}
	if tw == 1 {
		return hotStyle.Render("…")
	}
	w, cut := 0, len(disp)
	for i, r := range disp {
		rw := lipgloss.Width(string(r))
		if w+rw+1 > tw { // reserve one column for the ellipsis
			cut = i
			break
		}
		w += rw
	}
	return styledRuns(disp[:cut], emph[:cut], hotStyle) + hotStyle.Render("…")
}

// sanitizeSpans expands text exactly as sanitizeLine and returns the display
// runes with a parallel mask marking those whose source raw rune is covered by
// a span. Raw indices are counted over the \r-trimmed text (matching
// sanitizeLine); span ends are clamped to that length.
func sanitizeSpans(s string, spans []textdiff.Span) (disp []rune, emph []bool) {
	s = strings.TrimSuffix(s, "\r")
	runes := []rune(s)
	cover := coverMask(len(runes), spans)
	col := 0
	for raw, r := range runes {
		on := cover[raw]
		switch {
		case r == '\t':
			n := 4 - col%4
			for k := 0; k < n; k++ {
				disp = append(disp, ' ')
				emph = append(emph, on)
			}
			col += n
		case r < 0x20 || r == 0x7f:
			disp = append(disp, '·')
			emph = append(emph, on)
			col++
		default:
			disp = append(disp, r)
			emph = append(emph, on)
			col++
		}
	}
	return disp, emph
}

// coverMask marks raw rune indices [0,n) covered by any span (ends clamped).
func coverMask(n int, spans []textdiff.Span) []bool {
	mask := make([]bool, n)
	for _, sp := range spans {
		lo, hi := sp.Start, sp.End
		if lo < 0 {
			lo = 0
		}
		if hi > n {
			hi = n
		}
		for i := lo; i < hi; i++ {
			mask[i] = true
		}
	}
	return mask
}

// styledRuns renders disp grouping consecutive runes by emph flag: emphasized
// runs wear diffEmph inherited over base (so the cell background shows through),
// the rest just base.
func styledRuns(disp []rune, emph []bool, base lipgloss.Style) string {
	var b strings.Builder
	for i := 0; i < len(disp); {
		j := i + 1
		for j < len(disp) && emph[j] == emph[i] {
			j++
		}
		seg := string(disp[i:j])
		if emph[i] {
			b.WriteString(base.Inherit(diffEmph).Render(seg))
		} else {
			b.WriteString(base.Render(seg))
		}
		i = j
	}
	return b.String()
}
```

> `lipgloss.Style.Inherit(diffEmph)` copies diffEmph's Bold+Foreground onto a
> copy of `base` only where base hasn't set them; base keeps its Background. So
> emphasis is a bright bold foreground over the add/del cell background.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/` and `gofmt -l internal/tui/diff_render.go`
Expected: PASS — including **all pre-existing** `diff_render_test.go` tests unchanged (proof that plain-mode rendering is byte-identical). gofmt prints nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/diff_render.go internal/tui/diff_render_test.go
git commit -m "feat(tui): intraline word emphasis in the side-by-side diff

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: Docs

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `CLAUDE.md` (package map)
- Modify: `README.md` (only the diff-view feature note)

- [ ] **Step 1: CHANGELOG**

Add an entry under the current in-progress/unreleased section of `CHANGELOG.md` (match the file's existing heading style):

```markdown
- Side-by-side diffs now show GitHub-style **intraline word emphasis** — the
  exact words that changed are highlighted within a changed line.
- Commit diffs are served from an in-memory LRU cache: re-opening the same
  file in a commit issues no further `git show` and skips re-alignment.
- New `internal/cache` package: a generic, injected LRU cache factory
  (`Factory.Cache(name)`), reusable for future heavy reads.
```

- [ ] **Step 2: CLAUDE.md package map**

In the `internal/` package table in `CLAUDE.md`, add a `cache` row (keep the table's column style) and amend the `domain` row to mention the Differ:

```markdown
| `cache`      | Generic injected in-memory LRU cache factory (`Factory.Cache(name) Cache`); keys are caller-chosen hashes. First consumer: the commit-diff cache. |
```

Amend the `domain` row's description to end with: `… Queries (snapshot, commit feed) and the cached **Differ** (plain/enhanced, commit-diff cache) land here too.`

- [ ] **Step 3: README**

In `README.md`, find the diff-view feature description (the side-by-side viewer section). Add one sentence: changed lines now show intraline word emphasis, and commit diffs are cached for instant re-open. Keep it to the existing prose style; do not invent new key bindings (none were added).

- [ ] **Step 4: Verify build/docs**

Run: `go build ./cmd/gg` and confirm the three docs render (no broken markdown tables).
Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md CLAUDE.md README.md
git commit -m "docs: diff cache + intraline emphasis (changelog, package map, readme)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Final verification (after all tasks)

- [ ] Run `./test.sh race` — all stages green (vet+gofmt → unit → e2e).
- [ ] Manual smoke (optional): `go build ./cmd/gg && ./gg`, open a commit's file
      diff — changed words are highlighted; re-open the same file (it's instant /
      cached); a working-tree diff still reflects live edits.
- [ ] Dispatch the final holistic code reviewer over the whole branch.

## Notes for the executor

- **No CLI surface changed** ⇒ do **not** bump `agentskill.Version` or run
  `gg init --update`.
- **No new key bindings** ⇒ the footer-binding registry, `avail.go`, and the
  `TestHelpFooterCoverage` guard are untouched. (The diff view draws its own
  hint line, and we added no diff keys.)
- The cache is **injected**, never a package singleton: tests build their own
  `cache.NewFactory` or pass a `cache.Cache` straight into `NewDiffer`.
- Keep `internal/textdiff` free of git/TUI imports — enrichment is pure.
- Commit after every task; never commit in the shared checkout (work stays in
  this worktree on `feat/diff-cache`).
