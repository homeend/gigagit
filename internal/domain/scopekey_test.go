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
}
