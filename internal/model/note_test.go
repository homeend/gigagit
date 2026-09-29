package model

import (
	"strings"
	"testing"
)

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

func TestIsReviewNote(t *testing.T) {
	t.Parallel()
	c := FileAddress{State: StateCommitted, Commit: strings.Repeat("a", 40)}
	cases := []struct {
		n    Note
		want bool
	}{
		{Note{Address: c, Tags: []string{ReviewTag}}, true},
		{Note{Address: c}, false}, // commit-level, not a review
		{Note{Address: FileAddress{State: StateCommitted, Commit: c.Commit, Path: "x"}, Tags: []string{ReviewTag}}, false},
		{Note{Address: FileAddress{State: StateUnstaged, Branch: "main", Path: "x"}, Tags: []string{ReviewTag}}, false},
	}
	for i, tc := range cases {
		if got := tc.n.IsReviewNote(); got != tc.want {
			t.Errorf("case %d: IsReviewNote = %v, want %v", i, got, tc.want)
		}
	}
}

func TestShelfLevelNote(t *testing.T) {
	n := Note{Address: FileAddress{State: StateShelf, ShelfID: "e1"}}
	if !n.IsShelfLevel() || !n.IsEntryLevel() {
		t.Fatal("a note on a whole shelf entry must be shelf-level and entry-level")
	}
	n.Address.Path = "a.go"
	if n.IsShelfLevel() || n.IsEntryLevel() {
		t.Fatal("a file note on a shelf entry is not entry-level")
	}
	c := Note{Address: FileAddress{State: StateCommitted, Commit: "abc"}}
	if !c.IsEntryLevel() || c.IsShelfLevel() {
		t.Fatal("a commit-level note is entry-level, not shelf-level")
	}
}
