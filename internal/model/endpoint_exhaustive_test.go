package model

import "testing"

// endpointCase is one row of the table that every EndpointKind must have.
// Adding a kind to the iota block without adding a row here fails
// TestEveryEndpointKindHasATableRow.
type endpointCase struct {
	kind     EndpointKind
	name     string
	build    func(*testing.T) Endpoint
	display  string
	live     bool
	cacheTag string
	// cacheTagPanics marks the one kind whose CacheTag has NO safe answer:
	// EndpointRef. Returning the ref name would put a MOVING value in the
	// session diff-cache key (plan 1a's headline bug), and returning "" would
	// make two different refs collide inside CompareFiles's singleflight key.
	// Holding an unresolved ref where a cache key is needed is a programming
	// error, so it panics — with its OWN message ("CacheTag on an unresolved
	// ref endpoint … resolve it to a commit first — see domain.EvalEndpoint"),
	// not endpointKindBug's "add a case arm": the arm and this row both exist,
	// and the panic is a deliberate refusal rather than a missing case. When
	// this is set, cacheTag is ignored.
	cacheTagPanics bool
	bounded        bool
	source         FileSource
	locator        string
}

func endpointCases() []endpointCase {
	return []endpointCase{
		{
			kind:     EndpointWorkTree,
			name:     "worktree",
			build:    func(*testing.T) Endpoint { return WorkTreeEndpoint() },
			display:  "Working Tree",
			live:     true,
			cacheTag: "worktree",
			bounded:  false,
			source:   SourceUnstaged,
			locator:  "",
		},
		{
			kind:     EndpointIndex,
			name:     "index",
			build:    func(*testing.T) Endpoint { return IndexEndpoint() },
			display:  "Staged",
			live:     true,
			cacheTag: "index",
			bounded:  false,
			source:   SourceStaged,
			locator:  "",
		},
		{
			kind: EndpointCommit,
			name: "commit",
			build: func(t *testing.T) Endpoint {
				t.Helper()
				e, err := CommitEndpoint("abc1234def5678")
				if err != nil {
					t.Fatalf("CommitEndpoint: %v", err)
				}
				return e
			},
			display:  "abc1234",
			live:     false,
			cacheTag: "abc1234def5678",
			bounded:  false,
			source:   SourceCommit,
			locator:  "abc1234def5678",
		},
		{
			kind: EndpointShelf,
			name: "shelf",
			build: func(t *testing.T) Endpoint {
				t.Helper()
				e, err := ShelfEndpoint("wt-parser-9f3a1")
				if err != nil {
					t.Fatalf("ShelfEndpoint: %v", err)
				}
				return e
			},
			display:  "shelf #wt-parser (frozen)",
			live:     false,
			cacheTag: "shelf:wt-parser-9f3a1",
			bounded:  true,
			source:   SourceShelf,
			locator:  "wt-parser-9f3a1",
		},
		{
			kind: EndpointRef,
			name: "ref",
			build: func(t *testing.T) Endpoint {
				t.Helper()
				e, err := RefEndpoint("feat/unified-links")
				if err != nil {
					t.Fatalf("RefEndpoint: %v", err)
				}
				return e
			},
			display:        "feat/unified-links",
			live:           true, // a tip MOVES: nothing may cache a diff against it
			cacheTagPanics: true,
			bounded:        false, // a point: the whole tree at that tip
			source:         SourceCommit,
			locator:        "feat/unified-links", // `git show <ref>:<path>` is correct
		},
		{
			kind: EndpointPair,
			name: "pair",
			build: func(t *testing.T) Endpoint {
				t.Helper()
				e, err := PairEndpoint("abc1234def5678", "abc9999fff0000")
				if err != nil {
					t.Fatalf("PairEndpoint: %v", err)
				}
				return e
			},
			display:  "abc1234..abc9999",
			live:     false,
			cacheTag: "pair:abc1234def5678..abc9999fff0000",
			bounded:  true,
			source:   SourceCommit,
			locator:  "abc9999fff0000", // the NEW side is what a file read means
		},
	}
}

// TestEveryEndpointKindHasATableRow is the guard: every kind between
// EndpointInvalid (exclusive) and endpointKindCount (exclusive) must appear in
// endpointCases exactly once.
func TestEveryEndpointKindHasATableRow(t *testing.T) {
	t.Parallel()
	seen := map[EndpointKind]int{}
	for _, c := range endpointCases() {
		seen[c.kind]++
	}
	for k := EndpointInvalid + 1; k < endpointKindCount; k++ {
		switch seen[k] {
		case 1:
		case 0:
			t.Errorf("EndpointKind %d has no row in endpointCases; add one", k)
		default:
			t.Errorf("EndpointKind %d has %d rows in endpointCases; want exactly 1", k, seen[k])
		}
	}
	if len(endpointCases()) != int(endpointKindCount)-1 {
		t.Errorf("endpointCases has %d rows, want %d (one per kind except Invalid)",
			len(endpointCases()), int(endpointKindCount)-1)
	}
}

func TestEndpointMethodsMatchTheTable(t *testing.T) {
	t.Parallel()
	for _, c := range endpointCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := c.build(t)
			if got := e.Display(); got != c.display {
				t.Errorf("Display() = %q, want %q", got, c.display)
			}
			if got := e.IsLive(); got != c.live {
				t.Errorf("IsLive() = %v, want %v", got, c.live)
			}
			if got := e.Bounded(); got != c.bounded {
				t.Errorf("Bounded() = %v, want %v", got, c.bounded)
			}
			if c.cacheTagPanics {
				func() {
					defer func() {
						if recover() == nil {
							t.Errorf("CacheTag() on kind %s must panic", c.name)
						}
					}()
					_ = e.CacheTag()
				}()
			} else if got := e.CacheTag(); got != c.cacheTag {
				t.Errorf("CacheTag() = %q, want %q", got, c.cacheTag)
			}
			ref := e.FileRef("some/path.go")
			if ref.Source != c.source {
				t.Errorf("FileRef().Source = %v, want %v", ref.Source, c.source)
			}
			if ref.Locator != c.locator {
				t.Errorf("FileRef().Locator = %q, want %q", ref.Locator, c.locator)
			}
			if ref.Path != "some/path.go" {
				t.Errorf("FileRef().Path = %q, want %q", ref.Path, "some/path.go")
			}
		})
	}
}

// TestInvalidEndpointPanics pins the new contract: an unset Endpoint is a
// programming error, loudly, rather than a silently empty commit.
func TestInvalidEndpointPanics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		call func(Endpoint)
	}{
		{"Display", func(e Endpoint) { _ = e.Display() }},
		{"FileRef", func(e Endpoint) { _ = e.FileRef("p") }},
		{"CacheTag", func(e Endpoint) { _ = e.CacheTag() }},
		{"IsLive", func(e Endpoint) { _ = e.IsLive() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Errorf("%s on a zero Endpoint did not panic", tc.name)
				}
			}()
			tc.call(Endpoint{})
		})
	}
}

// A pair of one commit is legal and means "nothing changed" (ruling R7).
func TestPairEndpointAcceptsIdenticalHalves(t *testing.T) {
	t.Parallel()
	if _, err := PairEndpoint("abc1234def5678", "abc1234def5678"); err != nil {
		t.Fatalf("PairEndpoint(x, x) must be legal: %v", err)
	}
}

func TestNewEndpointConstructorsReject(t *testing.T) {
	t.Parallel()
	if _, err := RefEndpoint(""); err == nil {
		t.Error("RefEndpoint(\"\") must fail")
	}
	if _, err := RefEndpoint("fe@at"); err == nil {
		t.Error("RefEndpoint must reject a name LinkRefOK refuses")
	}
	for _, tc := range [][2]string{
		{"", "abc1234def5678"},
		{"abc1234def5678", ""},
		{"abc1", "abc1234def5678"},           // too short
		{"zzzz123def5678", "abc1234def5678"}, // not hex
		// NOTE: PairEndpoint(x, x) is NOT rejected -- ruling R7. A fully
		// merged branch's three-dot pair legitimately has base == source,
		// and an empty bounded set is a result, not an error (spec section 6).
	} {
		if _, err := PairEndpoint(tc[0], tc[1]); err == nil {
			t.Errorf("PairEndpoint(%q, %q) must fail", tc[0], tc[1])
		}
	}
}
