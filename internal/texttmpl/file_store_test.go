package texttmpl

import (
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestFileStoreRoundTrip(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir(), model.ProfileScopeRepo)
	body := "## <user:title>\n\nline two\n"
	added, err := fs.Add(model.TextTemplate{Title: "PR description", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if added.ID != "pr-description" || added.Scope != model.ProfileScopeRepo || added.Created.IsZero() {
		t.Fatalf("added = %+v", added)
	}
	got, err := fs.Get("pr-description")
	if err != nil || got.Body != body || got.Scope != model.ProfileScopeRepo {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if err := fs.Remove(added.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := fs.List(); len(list) != 0 {
		t.Fatalf("after remove: %+v", list)
	}
	if err := fs.Remove(added.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second remove: %v", err)
	}
}

func TestFileStoreAddDuplicateTitle(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	if _, err := fs.Add(model.TextTemplate{Title: "Note", Body: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Add(model.TextTemplate{Title: "note", Body: "b"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("err = %v, want ErrDuplicate", err)
	}
}

func TestFileStoreListSortedByTitle(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	for _, title := range []string{"beta", "Alpha", "gamma"} {
		if _, err := fs.Add(model.TextTemplate{Title: title, Body: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := fs.List()
	if len(list) != 3 || list[0].Title != "Alpha" || list[1].Title != "beta" || list[2].Title != "gamma" {
		t.Fatalf("list = %+v", list)
	}
}

func TestFileStoreUpdate(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	a, _ := fs.Add(model.TextTemplate{Title: "One", Body: "a"})
	_, _ = fs.Add(model.TextTemplate{Title: "Two", Body: "b"})

	up, err := fs.Update(a.ID, model.TextTemplate{Title: "One", Body: "changed"})
	if err != nil || up.ID != "one" || up.Body != "changed" || !up.Created.Equal(a.Created) {
		t.Fatalf("body update = %+v, %v", up, err)
	}
	up, err = fs.Update("one", model.TextTemplate{Title: "Uno", Body: "changed"})
	if err != nil || up.ID != "uno" {
		t.Fatalf("rename = %+v, %v", up, err)
	}
	if _, err := fs.Get("one"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old id still there: %v", err)
	}
	// A rename onto another title is refused, nothing lost.
	if _, err := fs.Update("uno", model.TextTemplate{Title: "Two", Body: "x"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("rename onto Two: %v", err)
	}
	if list, _ := fs.List(); len(list) != 2 {
		t.Fatalf("rows lost: %+v", list)
	}
	if _, err := fs.Update("nope", model.TextTemplate{Title: "X", Body: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
}

// A non-ASCII title gets a usable id; symbols alone do not.
func TestIDUnicode(t *testing.T) {
	t.Parallel()
	for title, want := range map[string]string{
		"PR description!": "pr-description",
		"リリース告知":          "リリース告知",
		"Отчёт за день":   "отчёт-за-день",
		"  --  ":          "",
	} {
		if got := ID(title); got != want {
			t.Errorf("ID(%q) = %q, want %q", title, got, want)
		}
	}
	fs := NewFileStore(t.TempDir(), model.ProfileScopeGlobal)
	if _, err := fs.Add(model.TextTemplate{Title: "!!!", Body: "x"}); !errors.Is(err, ErrNoID) {
		t.Fatalf("symbol-only title: %v", err)
	}
}
