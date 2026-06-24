package domain

import "testing"

func TestScopeKeyFoldsUpstreams(t *testing.T) {
	a := scopeKey(LogScope{Branches: []string{"main"}})
	b := scopeKey(LogScope{Branches: []string{"main"}, Upstreams: []string{"origin/main"}})
	if a == b {
		t.Fatalf("scopeKey must differ when Upstreams differ: %q == %q", a, b)
	}
}

func TestScopeKeyFoldsFilterAxes(t *testing.T) {
	base := scopeKey(LogScope{Branches: []string{"main"}})
	grep := scopeKey(LogScope{Branches: []string{"main"}, Grep: "fix"})
	path := scopeKey(LogScope{Branches: []string{"main"}, Paths: []string{"a"}})
	if base == grep {
		t.Fatal("a message filter must change the scope key")
	}
	if base == path || grep == path {
		t.Fatal("a path filter must change the scope key")
	}
	if scopeKey(LogScope{Grep: "fix"}) != scopeKey(LogScope{Grep: "fix"}) {
		t.Fatal("scopeKey must be stable for equal scopes")
	}
	author := scopeKey(LogScope{Branches: []string{"main"}, Author: "alice"})
	since := scopeKey(LogScope{Branches: []string{"main"}, Since: "2024-01-01"})
	until := scopeKey(LogScope{Branches: []string{"main"}, Until: "2024-12-31"})
	if base == author {
		t.Fatal("author filter must change the scope key")
	}
	if base == since {
		t.Fatal("since filter must change the scope key")
	}
	if base == until {
		t.Fatal("until filter must change the scope key")
	}
	// A path containing the old comma/pipe separators must not collide.
	if scopeKey(LogScope{Paths: []string{"a,b"}}) == scopeKey(LogScope{Paths: []string{"a", "b"}}) {
		t.Fatal("paths must not collide regardless of separator chars in values")
	}
}
