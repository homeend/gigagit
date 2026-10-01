package clock

import (
	"testing"
	"time"
)

// Not parallel: Freeze sets the package clock.
func TestFreezeAndRestore(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	restore := Freeze(at)
	if got := Now(); !got.Equal(at) {
		t.Fatalf("Now() = %v, want %v", got, at)
	}
	if got := Since(at.Add(-90 * time.Second)); got != 90*time.Second {
		t.Fatalf("Since = %v, want 90s", got)
	}
	restore()
	if d := time.Since(Now()); d < 0 || d > time.Minute {
		t.Fatalf("after restore Now() is not the real time: %v", Now())
	}
}
