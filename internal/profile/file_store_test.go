package profile

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestFileStoreAddListRemove(t *testing.T) {
	fs := NewFileStore(t.TempDir(), model.ProfileScopeRepo)

	got, err := fs.Add(model.Profile{Name: "Work", GitName: "Ada", GitEmail: "ada@work.example"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if got.ID != "work" {
		t.Fatalf("id = %q, want work", got.ID)
	}
	if got.Scope != model.ProfileScopeRepo {
		t.Fatalf("scope = %v, want repo", got.Scope)
	}
	if got.Created.IsZero() {
		t.Fatal("created not stamped")
	}

	list, err := fs.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Work" || list[0].Scope != model.ProfileScopeRepo {
		t.Fatalf("list = %#v", list)
	}

	if err := fs.Remove("work"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if list, _ := fs.List(); len(list) != 0 {
		t.Fatalf("after remove list = %#v", list)
	}
	if err := fs.Remove("work"); err != ErrNotFound {
		t.Fatalf("remove missing = %v, want ErrNotFound", err)
	}
}

func TestFileStoreAddIsIdempotentBySlug(t *testing.T) {
	fs := NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	_, _ = fs.Add(model.Profile{Name: "Work", GitEmail: "a@x"})
	_, _ = fs.Add(model.Profile{Name: "work", GitEmail: "b@x"}) // same slug
	list, _ := fs.List()
	if len(list) != 1 || list[0].GitEmail != "b@x" {
		t.Fatalf("expected idempotent replace, got %#v", list)
	}
}
