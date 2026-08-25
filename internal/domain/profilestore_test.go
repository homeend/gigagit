package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/profile"
)

func TestProfilesMergeBothScopes(t *testing.T) {
	t.Parallel()
	g := profile.NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	r := profile.NewFileStore(t.TempDir(), model.ProfileScopeRepo)
	svc := New(nil) // no git ops needed for profile CRUD
	svc.SetProfileStores(g, r)
	ctx := context.Background()

	if _, err := svc.AddProfile(ctx, model.Profile{Name: "Work", GitName: "A", GitEmail: "a@x", Scope: model.ProfileScopeGlobal}); err != nil {
		t.Fatalf("add global: %v", err)
	}
	if _, err := svc.AddProfile(ctx, model.Profile{Name: "OSS", GitName: "B", GitEmail: "b@x", Scope: model.ProfileScopeRepo}); err != nil {
		t.Fatalf("add repo: %v", err)
	}

	all, err := svc.Profiles(ctx)
	if err != nil {
		t.Fatalf("profiles: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len = %d, want 2: %#v", len(all), all)
	}
	// Global rows come first, tagged correctly.
	if all[0].Scope != model.ProfileScopeGlobal || all[1].Scope != model.ProfileScopeRepo {
		t.Fatalf("scopes = %v,%v", all[0].Scope, all[1].Scope)
	}

	if err := svc.RemoveProfile(ctx, model.ProfileScopeRepo, "oss"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if all, _ := svc.Profiles(ctx); len(all) != 1 || all[0].Name != "Work" {
		t.Fatalf("after remove = %#v", all)
	}
}
