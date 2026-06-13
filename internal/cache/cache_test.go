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
	c.Get("a") // a is now most-recently used; b is the LRU
	put("c")   // evicts b
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
