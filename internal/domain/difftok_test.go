package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/cache"
	"github.com/homeend/gigagit/internal/syntax"
)

// countingLex is a tokStore lex seam: it counts calls, hands each call's
// context to the test, and blocks until release is closed (nil = no wait).
type countingLex struct {
	calls   atomic.Int32
	release chan struct{}
	started chan context.Context
}

func (c *countingLex) lex(ctx context.Context, lang string, src []byte) ([][]syntax.Tok, error) {
	c.calls.Add(1)
	if c.started != nil {
		c.started <- ctx
	}
	if c.release != nil {
		select {
		case <-c.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return [][]syntax.Tok{{{Start: 0, End: len(src), Class: syntax.Keyword}}}, nil
}

func newCountingStore(c *countingLex, withCache bool) *tokStore {
	var cc cache.Cache
	if withCache {
		cc = cache.NewFactory(0, 0).Cache("diff-tok")
	}
	s := newTokStore(cc)
	s.lex = c.lex
	return s
}

func TestTokStoreCachesBySum(t *testing.T) {
	t.Parallel()
	c := &countingLex{}
	s := newCountingStore(c, true)
	src := []byte("package p\n")
	sum := contentSum(src)
	for i := 0; i < 2; i++ {
		if _, err := s.Lex(context.Background(), "go", sum, src); err != nil {
			t.Fatal(err)
		}
	}
	if n := c.calls.Load(); n != 1 {
		t.Fatalf("lexed %d times; want 1 (the second Lex is a cache hit)", n)
	}
	if _, ok := s.Get("go", sum); !ok {
		t.Fatal("Get missed after a completed Lex")
	}
}

func TestTokStoreSharesRunningLex(t *testing.T) {
	t.Parallel()
	c := &countingLex{release: make(chan struct{}), started: make(chan context.Context, 2)}
	s := newCountingStore(c, true)
	src := []byte("x := 1\n")
	sum := contentSum(src)
	var wg sync.WaitGroup
	res := make([][][]syntax.Tok, 2)
	for i := range res {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res[i], _ = s.Lex(context.Background(), "go", sum, src)
		}()
	}
	<-c.started
	waitWaiters(t, s, "go", sum, 2)
	close(c.release)
	wg.Wait()
	if n := c.calls.Load(); n != 1 {
		t.Fatalf("lexed %d times; want 1 shared lex", n)
	}
	if res[0] == nil || &res[0][0] != &res[1][0] {
		t.Fatalf("waiters got different runs: %v / %v", res[0], res[1])
	}
}

func TestTokStoreLastWaiterCancels(t *testing.T) {
	t.Parallel()
	c := &countingLex{release: make(chan struct{}), started: make(chan context.Context, 2)}
	s := newCountingStore(c, true)
	src := []byte("x := 1\n")
	sum := contentSum(src)
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	errs := make(chan error, 2)
	go func() { _, err := s.Lex(ctx1, "go", sum, src); errs <- err }()
	lexCtx := <-c.started
	go func() { _, err := s.Lex(ctx2, "go", sum, src); errs <- err }()
	waitWaiters(t, s, "go", sum, 2)
	cancel1()
	if err := <-errs; !errors.Is(err, context.Canceled) {
		t.Fatalf("first waiter = %v; want context.Canceled", err)
	}
	select {
	case <-lexCtx.Done():
		t.Fatal("the lex was cancelled while a second waiter still wanted it")
	case <-time.After(20 * time.Millisecond):
	}
	cancel2()
	if err := <-errs; !errors.Is(err, context.Canceled) {
		t.Fatalf("second waiter = %v; want context.Canceled", err)
	}
	select {
	case <-lexCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the lex kept running after its last waiter left")
	}
	waitWaiters(t, s, "go", sum, 0)
	if _, ok := s.Get("go", sum); ok {
		t.Fatal("a cancelled lex was cached")
	}
	c.release = nil
	c.started = nil
	if _, err := s.Lex(context.Background(), "go", sum, src); err != nil {
		t.Fatal(err)
	}
	if n := c.calls.Load(); n != 2 {
		t.Fatalf("lexed %d times; want 2 (the cancelled lex is redone)", n)
	}
}

func TestTokStoreCancelledStoresNothing(t *testing.T) {
	t.Parallel()
	c := &countingLex{release: make(chan struct{}), started: make(chan context.Context, 1)}
	s := newCountingStore(c, true)
	src := []byte("x := 1\n")
	sum := contentSum(src)
	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	go func() { _, err := s.Lex(ctx, "go", sum, src); errs <- err }()
	<-c.started
	cancel()
	if err := <-errs; !errors.Is(err, context.Canceled) {
		t.Fatalf("Lex = %v; want context.Canceled", err)
	}
	waitWaiters(t, s, "go", sum, 0)
	if _, ok := s.Get("go", sum); ok {
		t.Fatal("a cancelled lex was cached")
	}
}

func TestContentSum(t *testing.T) {
	t.Parallel()
	if got := contentSum(nil); got != "" {
		t.Fatalf("contentSum(nil) = %q; want \"\" (an absent side)", got)
	}
	empty := sha256.Sum256(nil)
	if got := contentSum([]byte{}); got != hex.EncodeToString(empty[:]) {
		t.Fatalf("contentSum(empty) = %q; want the SHA-256 of nothing", got)
	}
	if contentSum([]byte("a")) != contentSum([]byte("a")) || contentSum([]byte("a")) == contentSum([]byte("b")) {
		t.Fatal("contentSum is not a function of the bytes")
	}
}

// waitWaiters polls until the store's running lex for lang/sum has n
// waiters (0 = no running lex at all).
func waitWaiters(t *testing.T, s *tokStore, lang, sum string, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.waiters(lang, sum) == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("waiters(%s) = %d; want %d", lang, s.waiters(lang, sum), n)
}
