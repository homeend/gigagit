package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/prefix"
)

func TestPrefixesMergeGlobalThenRepoTagged(t *testing.T) {
	s := &Service{}
	g := prefix.NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	r := prefix.NewFileStore(t.TempDir(), model.ProfileScopeRepo)
	_, _ = g.Add(model.Prefix{Value: "feat/"})
	_, _ = r.Add(model.Prefix{Value: "jira/"})
	s.SetPrefixStores(g, r)

	got, err := s.Prefixes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Scope != model.ProfileScopeGlobal || got[1].Scope != model.ProfileScopeRepo {
		t.Fatalf("scopes = %v, %v", got[0].Scope, got[1].Scope)
	}
}

func TestAddPrefixRoutesByScopeAndValidates(t *testing.T) {
	s := &Service{}
	g := prefix.NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	r := prefix.NewFileStore(t.TempDir(), model.ProfileScopeRepo)
	s.SetPrefixStores(g, r)

	if _, err := s.AddPrefix(context.Background(), model.Prefix{Value: "feat/", Scope: model.ProfileScopeRepo}); err != nil {
		t.Fatal(err)
	}
	if list, _ := r.List(); len(list) != 1 {
		t.Fatalf("repo store len = %d, want 1", len(list))
	}
	if list, _ := g.List(); len(list) != 0 {
		t.Fatalf("global store len = %d, want 0", len(list))
	}

	if _, err := s.AddPrefix(context.Background(), model.Prefix{Value: "<branch>/x"}); err == nil {
		t.Fatal("want error for <branch> token")
	}
}

func TestValidatePrefixValue(t *testing.T) {
	ok := []string{
		"feat/",
		"john_smith/ISSUE-<user:issue-id>",
		"john_smith/sandbox-<seq:sandbox_seq:4>",
		"wt/<date:yyyy-MM-dd>/",
		"<parent-branch>/<random-alpha:4>",
	}
	for _, v := range ok {
		if err := ValidatePrefixValue(v); err != nil {
			t.Errorf("ValidatePrefixValue(%q) = %v, want nil", v, err)
		}
	}
	bad := []string{
		"",
		"<branch>",
		"x-<branch>-y",
		"<date>",    // missing format → engine errors
		"<bogus:1>", // unknown token
	}
	for _, v := range bad {
		if err := ValidatePrefixValue(v); err == nil {
			t.Errorf("ValidatePrefixValue(%q) = nil, want error", v)
		}
	}
}
