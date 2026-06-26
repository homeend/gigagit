package prefix

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestFileStoreRoundTrip(t *testing.T) {
	fs := NewFileStore(t.TempDir(), model.ProfileScopeRepo)

	added, err := fs.Add(model.Prefix{Value: "feat/"})
	if err != nil {
		t.Fatal(err)
	}
	if added.ID == "" {
		t.Fatal("want non-empty ID")
	}
	if added.Scope != model.ProfileScopeRepo {
		t.Fatalf("scope = %v, want repo", added.Scope)
	}

	list, err := fs.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Value != "feat/" {
		t.Fatalf("list = %+v", list)
	}
	if list[0].Scope != model.ProfileScopeRepo {
		t.Fatalf("listed scope = %v", list[0].Scope)
	}

	if err := fs.Remove(added.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := fs.List(); len(list) != 0 {
		t.Fatalf("after remove, list = %+v", list)
	}
}

func TestFileStoreAddIsIdempotentBySlug(t *testing.T) {
	fs := NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	_, _ = fs.Add(model.Prefix{Value: "feat/"})
	_, _ = fs.Add(model.Prefix{Value: "feat/"})
	if list, _ := fs.List(); len(list) != 1 {
		t.Fatalf("want 1 entry, got %d", len(list))
	}
}

func TestRemoveUnknownIsErrNotFound(t *testing.T) {
	fs := NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	if err := fs.Remove("nope"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
