package domain

import "sync"

// flightGroup coalesces concurrent calls sharing a key: the first caller
// (the leader) runs fn; callers that arrive while it is in flight share its
// result without running fn again. Once the leader returns, the key is freed
// — there is no caching across completed calls. The zero value is ready.
//
// The leader's call governs the work; a leader whose context is cancelled
// affects its followers (same semantics as golang.org/x/sync/singleflight).
// That is a non-issue for the single-caller TUI and acceptable for a shared
// MCP service.
type flightGroup struct {
	mu sync.Mutex
	m  map[string]*flightCall
}

type flightCall struct {
	wg  sync.WaitGroup
	val any
	err error
}

// Do runs fn under key, coalescing concurrent callers.
func (g *flightGroup) Do(key string, fn func() (any, error)) (any, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*flightCall)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := &flightCall{}
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	c.val, c.err = fn()
	c.wg.Done()

	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()

	return c.val, c.err
}
