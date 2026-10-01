package wtclaim

import (
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestCreateReadRemove(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	c := Claim{Session: "p/s1", Agent: "claude", Since: time.Unix(1700000000, 0).UTC(), Note: "https://x/issues/1"}
	if err := Create(d, c); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Read(d)
	if err != nil || !ok || got.Session != c.Session || got.Agent != c.Agent || got.Note != c.Note || !got.Since.Equal(c.Since) {
		t.Fatalf("Read = %+v %v %v", got, ok, err)
	}
	if a, ok := Age(d); !ok || a > time.Minute {
		t.Fatalf("Age = %v %v", a, ok)
	}
	if err := Create(d, c); !errors.Is(err, ErrClaimed) {
		t.Fatalf("second Create = %v, want ErrClaimed", err)
	}
	if err := Remove(d); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := Read(d); ok {
		t.Fatal("claim still present")
	}
	if err := Remove(d); err != nil {
		t.Fatalf("Remove absent = %v", err)
	}
}

func TestWithLockSerialises(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	var mu sync.Mutex
	inside, peak := 0, 0
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			WithLock(d, func() error {
				mu.Lock()
				inside++
				peak = max(peak, inside)
				mu.Unlock()
				time.Sleep(10 * time.Millisecond)
				mu.Lock()
				inside--
				mu.Unlock()
				return nil
			})
		}()
	}
	wg.Wait()
	if peak != 1 {
		t.Fatalf("max concurrent = %d, want 1", peak)
	}
}

func TestCreateRaceOneWinner(t *testing.T) {
	t.Parallel()
	d := t.TempDir()
	var wg sync.WaitGroup
	wins := make(chan int, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if Create(d, Claim{Session: "p/s" + strconv.Itoa(i)}) == nil {
				wins <- i
			}
		}()
	}
	wg.Wait()
	close(wins)
	if n := len(wins); n != 1 {
		t.Fatalf("winners = %d, want 1", n)
	}
}
