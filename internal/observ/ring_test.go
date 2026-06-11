package observ

import (
	"testing"
	"time"
)

func span(name string) Span {
	return Span{Name: name, Start: time.Now(), Duration: time.Millisecond}
}

func TestRingRecordsAndSnapshots(t *testing.T) {
	r := NewRing(3)
	r.Record(span("a"))
	r.Record(span("b"))
	got := r.Snapshot()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("order wrong: %q, %q", got[0].Name, got[1].Name)
	}
}

func TestRingEvictsOldestBeyondCapacity(t *testing.T) {
	r := NewRing(2)
	r.Record(span("a"))
	r.Record(span("b"))
	r.Record(span("c"))
	got := r.Snapshot()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (capped)", len(got))
	}
	if got[0].Name != "b" || got[1].Name != "c" {
		t.Fatalf("eviction wrong: %q, %q (want b, c)", got[0].Name, got[1].Name)
	}
}

func TestRingAssignsIncrementingIDs(t *testing.T) {
	r := NewRing(10)
	r.Record(span("a"))
	r.Record(span("b"))
	got := r.Snapshot()
	if got[0].ID == 0 || got[1].ID == 0 {
		t.Fatal("IDs should be non-zero")
	}
	if got[1].ID <= got[0].ID {
		t.Fatalf("IDs should increment: %d then %d", got[0].ID, got[1].ID)
	}
}
