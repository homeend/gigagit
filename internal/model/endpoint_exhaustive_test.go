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
	source   FileSource
	locator  string
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
			source:   SourceShelf,
			locator:  "wt-parser-9f3a1",
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
			if got := e.CacheTag(); got != c.cacheTag {
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
