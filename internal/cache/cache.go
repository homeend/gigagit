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
