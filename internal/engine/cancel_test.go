package engine

import (
	"context"
	"testing"
	"time"
)

// With an unbuffered, never-drained channel and a cancelled context, emit must
// return promptly instead of blocking forever.
func TestEmitUnblocksOnCancelledContext(t *testing.T) {
	t.Parallel()
	ch := make(chan Event) // unbuffered, no reader
	deps := OpDeps{Events: ch}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		deps.emit(ctx, Progress{Step: "x"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("emit blocked on a cancelled context with no reader")
	}
}
