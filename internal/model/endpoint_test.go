package model

import (
	"errors"
	"strings"
	"testing"
)

func TestEndpointDisplay(t *testing.T) {
	cases := []struct {
		e    Endpoint
		want string
	}{
		{Endpoint{Kind: EndpointWorkTree}, "Working Tree"},
		{Endpoint{Kind: EndpointIndex}, "Staged"},
		{Endpoint{Kind: EndpointCommit, Hash: "0123456789abcdef"}, "0123456"},
		{Endpoint{Kind: EndpointCommit, Hash: "abc"}, "abc"},
	}
	for _, c := range cases {
		if got := c.e.Display(); got != c.want {
			t.Errorf("Display(%+v) = %q, want %q", c.e, got, c.want)
		}
	}
}

func TestEndpointFileRef(t *testing.T) {
	if got := (Endpoint{Kind: EndpointWorkTree}).FileRef("a.go"); got != (FileRef{Source: SourceUnstaged, Path: "a.go"}) {
		t.Errorf("worktree FileRef = %+v", got)
	}
	if got := (Endpoint{Kind: EndpointIndex}).FileRef("a.go"); got != (FileRef{Source: SourceStaged, Path: "a.go"}) {
		t.Errorf("index FileRef = %+v", got)
	}
	if got := (Endpoint{Kind: EndpointCommit, Hash: "deadbeef"}).FileRef("a.go"); got != (FileRef{Source: SourceCommit, Locator: "deadbeef", Path: "a.go"}) {
		t.Errorf("commit FileRef = %+v", got)
	}
}

func TestEndpointIsLiveAndCacheTag(t *testing.T) {
	if !(Endpoint{Kind: EndpointWorkTree}).IsLive() || !(Endpoint{Kind: EndpointIndex}).IsLive() {
		t.Error("worktree/index must be live")
	}
	if (Endpoint{Kind: EndpointCommit, Hash: "x"}).IsLive() {
		t.Error("commit must not be live")
	}
	if got := (Endpoint{Kind: EndpointCommit, Hash: "x"}).CacheTag(); got != "x" {
		t.Errorf("commit CacheTag = %q", got)
	}
	if got := (Endpoint{Kind: EndpointWorkTree}).CacheTag(); got != "worktree" {
		t.Errorf("worktree CacheTag = %q", got)
	}
}

func TestCommitEndpointRejectsBadHashes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		hash string
	}{
		{"empty", ""},
		{"too short", "abc123"}, // 6 < 7
		{"not hex", "zzzzzzz"},
		{"too long", strings.Repeat("a", 65)}, // 65 > 64
		{"has whitespace", "abc1234 "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := CommitEndpoint(tc.hash); !errors.Is(err, ErrEndpoint) {
				t.Errorf("CommitEndpoint(%q) error = %v, want one wrapping ErrEndpoint", tc.hash, err)
			}
		})
	}
}

func TestCommitEndpointAcceptsValidHashes(t *testing.T) {
	t.Parallel()
	for _, h := range []string{
		"abc1234", // 7, the minimum
		"e3b815eb61c32228c9963e667c0076d6d42308c3", // 40, sha-1
		strings.Repeat("a", 64),                    // 64, sha-256
		"ABC1234",                                  // upper-case hex is still hex
	} {
		e, err := CommitEndpoint(h)
		if err != nil {
			t.Fatalf("CommitEndpoint(%q): %v", h, err)
		}
		if e.Kind != EndpointCommit {
			t.Errorf("CommitEndpoint(%q).Kind = %v, want EndpointCommit", h, e.Kind)
		}
		if e.Hash != h {
			t.Errorf("CommitEndpoint(%q).Hash = %q, want %q", h, e.Hash, h)
		}
	}
}

func TestShelfEndpointRejectsAnEmptyID(t *testing.T) {
	t.Parallel()
	if _, err := ShelfEndpoint(""); !errors.Is(err, ErrEndpoint) {
		t.Errorf("ShelfEndpoint(\"\") error = %v, want one wrapping ErrEndpoint", err)
	}
}

func TestShelfEndpointAcceptsAnID(t *testing.T) {
	t.Parallel()
	e, err := ShelfEndpoint("wt-parser-9f3a1")
	if err != nil {
		t.Fatalf("ShelfEndpoint: %v", err)
	}
	if e.Kind != EndpointShelf || e.ShelfID != "wt-parser-9f3a1" {
		t.Errorf("ShelfEndpoint = %+v, want kind shelf and the id", e)
	}
}

func TestLiveEndpointConstructors(t *testing.T) {
	t.Parallel()
	if got := WorkTreeEndpoint(); got.Kind != EndpointWorkTree {
		t.Errorf("WorkTreeEndpoint().Kind = %v, want EndpointWorkTree", got.Kind)
	}
	if got := IndexEndpoint(); got.Kind != EndpointIndex {
		t.Errorf("IndexEndpoint().Kind = %v, want EndpointIndex", got.Kind)
	}
}

// TestBoundedMatchesTheSpecRule pins section 3.1: a shelf entry is a finite,
// enumerated set of paths; a tree, a tip and the index are not.
func TestBoundedMatchesTheSpecRule(t *testing.T) {
	t.Parallel()
	for _, c := range endpointCases() {
		want := c.kind == EndpointShelf
		if got := c.build().Bounded(); got != want {
			t.Errorf("%s: Bounded() = %v, want %v", c.name, got, want)
		}
	}
}

func TestBoundedPanicsOnAnUnsetEndpoint(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("Bounded() on a zero Endpoint did not panic")
		}
	}()
	_ = Endpoint{}.Bounded()
}
