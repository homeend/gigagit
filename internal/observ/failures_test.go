package observ

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNoteFailureRecordsAndWrites(t *testing.T) {
	ResetFailures()
	var buf bytes.Buffer
	SetFailureSink(&buf)
	NoteFailure("query status", errors.New("git status failed (exit 128):\nfatal: boom"))

	fs := SessionFailures()
	if len(fs) != 1 {
		t.Fatalf("want 1 entry, got %d", len(fs))
	}
	if fs[0].Source != "query status" {
		t.Fatalf("source = %q", fs[0].Source)
	}
	// Detail is collapsed to one line.
	if strings.Contains(fs[0].Detail, "\n") {
		t.Fatalf("detail not collapsed: %q", fs[0].Detail)
	}
	if !strings.Contains(fs[0].Detail, "fatal: boom") {
		t.Fatalf("detail missing stderr: %q", fs[0].Detail)
	}
	line := buf.String()
	if !strings.Contains(line, "query status") || !strings.Contains(line, "fatal: boom") {
		t.Fatalf("sink line missing fields: %q", line)
	}
	if strings.Count(line, "\n") != 1 {
		t.Fatalf("want exactly one line written: %q", line)
	}
}

func TestNoteFailureIgnoresNilAndCancellation(t *testing.T) {
	ResetFailures()
	NoteFailure("x", nil)
	NoteFailure("x", context.Canceled)
	NoteFailure("x", fmt.Errorf("wrap: %w", context.DeadlineExceeded))
	if fs := SessionFailures(); len(fs) != 0 {
		t.Fatalf("want 0 entries, got %d: %+v", len(fs), fs)
	}
}

func TestSessionFailuresNewestFirst(t *testing.T) {
	ResetFailures()
	NoteFailure("a", errors.New("1"))
	NoteFailure("b", errors.New("2"))
	fs := SessionFailures()
	if len(fs) != 2 || fs[0].Source != "b" || fs[1].Source != "a" {
		t.Fatalf("want newest-first [b,a], got %+v", fs)
	}
}

func TestFailureRingEviction(t *testing.T) {
	ResetFailures()
	for i := 0; i < failureRingCap+50; i++ {
		NoteFailure(fmt.Sprintf("s%d", i), errors.New("e"))
	}
	fs := SessionFailures()
	if len(fs) != failureRingCap {
		t.Fatalf("want cap %d, got %d", failureRingCap, len(fs))
	}
	// Newest first: the last recorded source leads.
	if fs[0].Source != fmt.Sprintf("s%d", failureRingCap+49) {
		t.Fatalf("oldest not evicted; head = %q", fs[0].Source)
	}
}
