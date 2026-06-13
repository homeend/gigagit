package domain

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestFlightCoalesces: while a leader holds an in-flight call, every other
// caller of the same key shares its result without re-running fn.
func TestFlightCoalesces(t *testing.T) {
	var g flightGroup
	var calls int32
	entered := make(chan struct{})
	release := make(chan struct{})

	leader := func() (any, error) {
		atomic.AddInt32(&calls, 1)
		close(entered)
		<-release
		return 42, nil
	}
	const n = 4
	got := make(chan int, n)
	go func() { v, _ := g.Do("k", leader); got <- v.(int) }()
	<-entered // leader is now inside fn, holding the slot

	for i := 0; i < n-1; i++ {
		go func() {
			v, _ := g.Do("k", func() (any, error) {
				atomic.AddInt32(&calls, 1) // must NOT run for followers
				return -1, nil
			})
			got <- v.(int)
		}()
	}
	// Followers have entered Do and found the held key (the leader will not
	// release the slot until we close release).
	time.Sleep(20 * time.Millisecond)
	close(release)

	for i := 0; i < n; i++ {
		if v := <-got; v != 42 {
			t.Fatalf("caller got %d, want 42 (coalesced)", v)
		}
	}
	if c := atomic.LoadInt32(&calls); c != 1 {
		t.Fatalf("fn ran %d times, want 1", c)
	}
}

// TestFlightReleasesKey: after a call completes the key is freed, so a later
// call runs fn again (no permanent caching).
func TestFlightReleasesKey(t *testing.T) {
	var g flightGroup
	var calls int32
	fn := func() (any, error) { atomic.AddInt32(&calls, 1); return nil, nil }
	g.Do("k", fn)
	g.Do("k", fn)
	if c := atomic.LoadInt32(&calls); c != 2 {
		t.Fatalf("fn ran %d times across two sequential Do calls, want 2", c)
	}
}

// TestFlightPropagatesError: the leader's error reaches every follower.
func TestFlightPropagatesError(t *testing.T) {
	var g flightGroup
	boom := errors.New("boom")
	entered := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	errc := make(chan error, 3)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := g.Do("k", func() (any, error) {
			close(entered)
			<-release
			return nil, boom
		})
		errc <- err
	}()
	<-entered
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := g.Do("k", func() (any, error) { return nil, nil }); errc <- err }()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errc)
	for err := range errc {
		if !errors.Is(err, boom) {
			t.Fatalf("caller got %v, want boom", err)
		}
	}
}
