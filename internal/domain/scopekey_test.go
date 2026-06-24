package domain

import "testing"

func TestScopeKeyFoldsUpstreams(t *testing.T) {
	a := scopeKey(LogScope{Branches: []string{"main"}})
	b := scopeKey(LogScope{Branches: []string{"main"}, Upstreams: []string{"origin/main"}})
	if a == b {
		t.Fatalf("scopeKey must differ when Upstreams differ: %q == %q", a, b)
	}
}
