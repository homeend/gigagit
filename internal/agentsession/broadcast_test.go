package agentsession

import (
	"testing"
	"time"
)

func recv(t *testing.T, ch <-chan struct{}) bool {
	t.Helper()
	select {
	case <-ch:
		return true
	case <-time.After(200 * time.Millisecond):
		return false
	}
}

func TestBroadcasterWakesEverySubscriber(t *testing.T) {
	t.Parallel()
	var b Broadcaster
	a, cancelA := b.Subscribe()
	defer cancelA()
	c, cancelC := b.Subscribe()
	defer cancelC()
	b.Signal()
	if !recv(t, a) || !recv(t, c) {
		t.Fatal("both subscribers must wake on one Signal")
	}
}

func TestBroadcasterCoalescesPerSubscriber(t *testing.T) {
	t.Parallel()
	var b Broadcaster
	a, cancel := b.Subscribe()
	defer cancel()
	b.Signal()
	b.Signal()
	b.Signal()
	if !recv(t, a) {
		t.Fatal("first wakeup")
	}
	if recv(t, a) {
		t.Fatal("a burst before the read must coalesce into ONE wakeup")
	}
}

func TestBroadcasterCancelDropsTheSubscriber(t *testing.T) {
	t.Parallel()
	var b Broadcaster
	a, cancel := b.Subscribe()
	cancel()
	cancel() // idempotent
	b.Signal()
	if recv(t, a) {
		t.Fatal("a cancelled subscriber receives nothing")
	}
	if n := b.count(); n != 0 {
		t.Fatalf("subscribers after cancel = %d, want 0", n)
	}
}
