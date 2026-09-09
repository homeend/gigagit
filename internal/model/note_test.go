package model

import "testing"

func TestNoteContextHashTrimsAndJoins(t *testing.T) {
	t.Parallel()
	a := NoteContextHash([]string{"  foo(bar)  ", "\tbaz"})
	b := NoteContextHash([]string{"foo(bar)", "baz"})
	if a != b {
		t.Fatalf("leading/trailing whitespace must not change the hash: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("hash = %q, want 64 hex chars (sha256)", a)
	}
	if NoteContextHash([]string{"foo", "bar"}) == NoteContextHash([]string{"bar", "foo"}) {
		t.Fatal("line order must matter")
	}
	if NoteContextHash(nil) != NoteContextHash([]string{}) {
		t.Fatal("nil and empty must hash alike")
	}
	// A single line is the phase-1 shape and must not carry a trailing newline
	// into the digest — phase 2's multi-line hunk anchors reuse this exact rule.
	if NoteContextHash([]string{"x"}) == NoteContextHash([]string{"x", ""}) {
		t.Fatal("a trailing empty line must change the hash")
	}
}

func TestNoteIsReply(t *testing.T) {
	t.Parallel()
	if (Note{}).IsReply() {
		t.Fatal("a root note has no ParentID")
	}
	if !(Note{ParentID: "abc"}).IsReply() {
		t.Fatal("a note with a ParentID is a reply")
	}
}
