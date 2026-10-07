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

// The working-review predicates: a live, path-less note with a worktree.
func TestWorkingReviewPredicates(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 40)
	review := []string{ReviewTag}
	cases := []struct {
		name               string
		n                  Note
		wtLevel, wr, entry bool
	}{
		{"working review", Note{Tags: review, Address: FileAddress{State: StateUnstaged, Worktree: "/r"}}, true, true, true},
		{"worktree-level untagged", Note{Address: FileAddress{State: StateUnstaged, Worktree: "/r"}}, true, false, true},
		{"staged state", Note{Tags: review, Address: FileAddress{State: StateStaged, Worktree: "/r"}}, true, true, true},
		{"no worktree", Note{Tags: review, Address: FileAddress{State: StateUnstaged}}, false, false, false},
		{"a file", Note{Tags: review, Address: FileAddress{State: StateUnstaged, Worktree: "/r", Path: "a.go"}}, false, false, false},
		{"commit review", Note{Tags: review, Address: FileAddress{State: StateCommitted, Commit: sha}}, false, false, true},
		{"shelf entry", Note{Address: FileAddress{State: StateShelf, ShelfID: "e1"}}, false, false, true},
	}
	for _, c := range cases {
		if got := c.n.IsWorktreeLevel(); got != c.wtLevel {
			t.Errorf("%s: IsWorktreeLevel = %v, want %v", c.name, got, c.wtLevel)
		}
		if got := c.n.IsWorkingReview(); got != c.wr {
			t.Errorf("%s: IsWorkingReview = %v, want %v", c.name, got, c.wr)
		}
		if got := c.n.IsEntryLevel(); got != c.entry {
			t.Errorf("%s: IsEntryLevel = %v, want %v", c.name, got, c.entry)
		}
		if c.n.IsWorkingReview() && c.n.IsReviewNote() {
			t.Errorf("%s: IsReviewNote must stay commit-only", c.name)
		}
	}
}

func TestStoredRootIDMapsARemarkToItsReview(t *testing.T) {
	cases := map[string]string{
		"review:ab12cd34:3": "ab12cd34",
		"review:ab12cd34:0": "ab12cd34",
		"ab12cd34":          "ab12cd34",
		"forge:991":         "forge:991",
		"review:bad":        "review:bad", // no index: not a remark id
		"":                  "",
	}
	for in, want := range cases {
		if got := StoredRootID(in); got != want {
			t.Errorf("StoredRootID(%q) = %q, want %q", in, got, want)
		}
	}
	// A remark reply hangs off the review note; Remark names the remark.
	r := Note{ID: "r1", ParentID: "ab12cd34", Remark: "review:ab12cd34:2"}
	if r.StoredParent() != "ab12cd34" || !r.IsRemarkReply() {
		t.Fatalf("remark reply: StoredParent=%q IsRemarkReply=%v", r.StoredParent(), r.IsRemarkReply())
	}
	p := Note{ID: "r2", ParentID: "11223344"}
	if p.StoredParent() != "11223344" || p.IsRemarkReply() {
		t.Fatalf("plain reply: StoredParent=%q IsRemarkReply=%v", p.StoredParent(), p.IsRemarkReply())
	}
	if id, n, ok := ParseReviewNoteID("review:ab12cd34:7"); !ok || id != "ab12cd34" || n != 7 {
		t.Fatalf("ParseReviewNoteID = %q %d %v", id, n, ok)
	}
	for _, bad := range []string{"review:ab12cd34", "review::3", "review:ab:-1", "review:ab:x", "ab:3"} {
		if _, _, ok := ParseReviewNoteID(bad); ok {
			t.Errorf("ParseReviewNoteID(%q) accepted", bad)
		}
	}
}
func TestNoteSendState(t *testing.T) {
	t.Parallel()
	var none *NoteSend
	if none.State() != SyncLocal {
		t.Errorf("nil send = %q", none.State())
	}
	if s := (&NoteSend{PR: 7, Review: "R"}); s.State() != SyncSending {
		t.Errorf("stamped = %q", s.State())
	}
	if s := (&NoteSend{PR: 7, Err: "403"}); s.State() != SyncFailed {
		t.Errorf("failed = %q", s.State())
	}
	if !(Note{ParentID: ForgeNoteIDPrefix + "PRRC_1"}).IsForgeReply() || (Note{ParentID: "n1"}).IsForgeReply() {
		t.Error("IsForgeReply")
	}
	n := Note{RemarkSends: []RemarkSend{{RemarkFP: "fp1", Moved: true}}}
	if rs, ok := n.RemarkSend("fp1"); !ok || !rs.Moved {
		t.Errorf("RemarkSend = %+v, %v", rs, ok)
	}
}
