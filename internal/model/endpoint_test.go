package model

import (
	"errors"
	"strings"
	"testing"
)

// mustCommit and mustShelf build a valid Endpoint for fixtures that have no
// *testing.T in scope (endpointCases in endpoint_exhaustive_test.go builds
// its table before any t.Run). A validation failure here is a fixture bug,
// so they panic rather than silently returning the zero Endpoint.
func mustCommit(hash string) Endpoint {
	e, err := CommitEndpoint(hash)
	if err != nil {
		panic(err)
	}
	return e
}

func mustShelf(id string) Endpoint {
	e, err := ShelfEndpoint(id)
	if err != nil {
		panic(err)
	}
	return e
}

func TestEndpointDisplay(t *testing.T) {
	cases := []struct {
		e    Endpoint
		want string
	}{
		{WorkTreeEndpoint(), "Working Tree"},
		{IndexEndpoint(), "Staged"},
		{mustCommit("0123456789abcdef"), "0123456"},
		// "abc" is shorter than CommitEndpoint's 7-char minimum: this probes
		// Display's truncation on a hash the constructor would refuse, so it
		// stays a raw struct literal deliberately.
		{Endpoint{kind: EndpointCommit, hash: "abc"}, "abc"},
	}
	for _, c := range cases {
		if got := c.e.Display(); got != c.want {
			t.Errorf("Display(%+v) = %q, want %q", c.e, got, c.want)
		}
	}
}

func TestEndpointFileRef(t *testing.T) {
	if got := (WorkTreeEndpoint()).FileRef("a.go"); got != (FileRef{Source: SourceUnstaged, Path: "a.go"}) {
		t.Errorf("worktree FileRef = %+v", got)
	}
	if got := (IndexEndpoint()).FileRef("a.go"); got != (FileRef{Source: SourceStaged, Path: "a.go"}) {
		t.Errorf("index FileRef = %+v", got)
	}
	if got := (mustCommit("deadbeef")).FileRef("a.go"); got != (FileRef{Source: SourceCommit, Locator: "deadbeef", Path: "a.go"}) {
		t.Errorf("commit FileRef = %+v", got)
	}
}

func TestEndpointIsLiveAndCacheTag(t *testing.T) {
	if !(WorkTreeEndpoint()).IsLive() || !(IndexEndpoint()).IsLive() {
		t.Error("worktree/index must be live")
	}
	// "x" is shorter than CommitEndpoint's 7-char minimum: these two probe
	// IsLive/CacheTag on a hash the constructor would refuse, so they stay
	// raw struct literals deliberately.
	if (Endpoint{kind: EndpointCommit, hash: "x"}).IsLive() {
		t.Error("commit must not be live")
	}
	if got := (Endpoint{kind: EndpointCommit, hash: "x"}).CacheTag(); got != "x" {
		t.Errorf("commit CacheTag = %q", got)
	}
	if got := (WorkTreeEndpoint()).CacheTag(); got != "worktree" {
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
		if e.Kind() != EndpointCommit {
			t.Errorf("CommitEndpoint(%q).Kind = %v, want EndpointCommit", h, e.Kind())
		}
		if e.Hash() != h {
			t.Errorf("CommitEndpoint(%q).Hash = %q, want %q", h, e.Hash(), h)
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
	if e.Kind() != EndpointShelf || e.ShelfID() != "wt-parser-9f3a1" {
		t.Errorf("ShelfEndpoint = %+v, want kind shelf and the id", e)
	}
}

func TestRefEndpointAcceptsAName(t *testing.T) {
	t.Parallel()
	e, err := RefEndpoint("feat/unified-links")
	if err != nil {
		t.Fatalf("RefEndpoint: %v", err)
	}
	if e.Kind() != EndpointRef || e.Ref() != "feat/unified-links" {
		t.Errorf("RefEndpoint = %+v, want kind ref and the name", e)
	}
	if e.Hash() != "" || e.ShelfID() != "" || e.PairA() != "" || e.PairB() != "" {
		t.Errorf("RefEndpoint's other accessors must be \"\": %+v", e)
	}
}

func TestPairEndpointStoresBothSides(t *testing.T) {
	t.Parallel()
	e, err := PairEndpoint("abc1234def5678", "abc9999fff0000")
	if err != nil {
		t.Fatalf("PairEndpoint: %v", err)
	}
	if e.Kind() != EndpointPair || e.PairA() != "abc1234def5678" || e.PairB() != "abc9999fff0000" {
		t.Errorf("PairEndpoint = %+v, want kind pair and both shas", e)
	}
	if e.Hash() != "" || e.ShelfID() != "" || e.Ref() != "" {
		t.Errorf("PairEndpoint's other accessors must be \"\": %+v", e)
	}
}

func TestLiveEndpointConstructors(t *testing.T) {
	t.Parallel()
	if got := WorkTreeEndpoint(); got.Kind() != EndpointWorkTree {
		t.Errorf("WorkTreeEndpoint().Kind = %v, want EndpointWorkTree", got.Kind())
	}
	if got := IndexEndpoint(); got.Kind() != EndpointIndex {
		t.Errorf("IndexEndpoint().Kind = %v, want EndpointIndex", got.Kind())
	}
}

// TestBoundedMatchesTheSpecRule pins section 3.1: a shelf entry and a
// resolved pair are finite, enumerated sets of paths; a tree, the index and a
// ref tip are not. The want value comes from the table's own bounded column
// (endpoint_exhaustive_test.go), which is the one place this rule is spelled
// out per kind.
func TestBoundedMatchesTheSpecRule(t *testing.T) {
	t.Parallel()
	for _, c := range endpointCases() {
		if got := c.build(t).Bounded(); got != c.bounded {
			t.Errorf("%s: Bounded() = %v, want %v", c.name, got, c.bounded)
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
