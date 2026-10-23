package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func tmpFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "state", "workspaces.toml")
}

func TestLoadMissingIsNil(t *testing.T) {
	t.Parallel()
	if got := Load(tmpFile(t)); got != nil {
		t.Fatalf("Load(missing) = %v, want nil", got)
	}
	if got := Load(""); got != nil {
		t.Fatalf("Load(\"\") = %v, want nil", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	p := tmpFile(t)
	ws := []Entry{
		{Name: "shop", Repos: []string{"/work/shop-api", "/work/shop-web"}, SettingsRepo: "/work/shop-web"},
		{Name: "infra", Repos: []string{"/work/infra"}},
	}
	if err := Save(p, ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "format = 1\n") {
		t.Fatalf("file does not start with the format marker:\n%s", data)
	}
	got := Load(p)
	if len(got) != 2 || got[0].Name != "shop" || got[1].Name != "infra" {
		t.Fatalf("Load = %+v", got)
	}
	if got[0].SettingsRepo != "/work/shop-web" || len(got[0].Repos) != 2 || got[0].Repos[1] != "/work/shop-web" {
		t.Fatalf("entry 0 = %+v", got[0])
	}
}

func TestLoadCorruptIsNil(t *testing.T) {
	t.Parallel()
	p := tmpFile(t)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("format = 1\n[[workspace]\nname = "), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Load(p); got != nil {
		t.Fatalf("Load(corrupt) = %v, want nil", got)
	}
}

func TestLoadNewerFormatIsNil(t *testing.T) {
	t.Parallel()
	p := tmpFile(t)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("format = 2\n[[workspace]]\nname = \"x\"\nrepos = [\"/a\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Load(p); got != nil {
		t.Fatalf("Load(newer) = %v, want nil", got)
	}
}

func TestValidName(t *testing.T) {
	t.Parallel()
	bad := []string{"", strings.Repeat("x", 41), "a/b", "a?b", "a\x01b", "  "}
	for _, n := range bad {
		if err := ValidName(n); err == nil {
			t.Errorf("ValidName(%q) = nil, want error", n)
		}
	}
	good := []string{"shop", "Shop 2", strings.Repeat("x", 40), "日本語"}
	for _, n := range good {
		if err := ValidName(n); err != nil {
			t.Errorf("ValidName(%q) = %v, want nil", n, err)
		}
	}
}

func TestAddCapAndDuplicate(t *testing.T) {
	t.Parallel()
	var e Entry
	for i := range MaxRepos {
		if err := e.Add(filepath.Join("/work", string(rune('a'+i)))); err != nil {
			t.Fatalf("Add #%d: %v", i, err)
		}
	}
	if err := e.Add("/work/z"); !errors.Is(err, ErrFull) {
		t.Fatalf("6th Add = %v, want ErrFull", err)
	}
	if err := e.Add("/work/a"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate Add = %v, want ErrDuplicate (duplicate beats full)", err)
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		if err := e.Add("/work/A"); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("case-folded duplicate = %v, want ErrDuplicate", err)
		}
	}
	if !e.Has("/work/a/") || e.Has("/work/q") {
		t.Fatalf("Has: %+v", e)
	}
}

func TestRemoveClearsSettingsRepo(t *testing.T) {
	t.Parallel()
	e := Entry{Repos: []string{"/a", "/b"}, SettingsRepo: "/b"}
	e.Remove("/b")
	if len(e.Repos) != 1 || e.Repos[0] != "/a" || e.SettingsRepo != "" {
		t.Fatalf("after Remove: %+v", e)
	}
	e.Remove("/nope")
	if len(e.Repos) != 1 {
		t.Fatalf("Remove of a non-member changed the list: %+v", e)
	}
}

func TestMoveClamps(t *testing.T) {
	t.Parallel()
	e := Entry{Repos: []string{"/a", "/b", "/c"}}
	e.Move(0, -1)
	if e.Repos[0] != "/a" {
		t.Fatalf("Move up from the top moved: %v", e.Repos)
	}
	e.Move(2, +5)
	if e.Repos[2] != "/c" {
		t.Fatalf("Move down past the end moved: %v", e.Repos)
	}
	e.Move(0, +1)
	if e.Repos[0] != "/b" || e.Repos[1] != "/a" {
		t.Fatalf("Move(0,+1) = %v", e.Repos)
	}
	e.Move(2, -2)
	if e.Repos[0] != "/c" {
		t.Fatalf("Move(2,-2) = %v", e.Repos)
	}
	e.Move(9, 1) // out of range: no-op, no panic
}

func TestContainingByKey(t *testing.T) {
	t.Parallel()
	ws := []Entry{
		{Name: "one", Repos: []string{"/work/a", "/work/b"}},
		{Name: "two", Repos: []string{"/work/b"}},
		{Name: "three", Repos: []string{"/work/c"}},
	}
	got := Containing(ws, model.KeyOf("/work/b/"))
	if len(got) != 2 || got[0].Name != "one" || got[1].Name != "two" {
		t.Fatalf("Containing(b) = %+v", got)
	}
	if got := Containing(ws, model.KeyOf("/work/zz")); got != nil {
		t.Fatalf("Containing(zz) = %+v, want nil", got)
	}
}

func TestFindUpsertDeleteCaseInsensitive(t *testing.T) {
	t.Parallel()
	ws := Upsert(nil, Entry{Name: "Shop", Repos: []string{"/a"}})
	ws = Upsert(ws, Entry{Name: "infra", Repos: []string{"/i"}})
	ws = Upsert(ws, Entry{Name: "shop", Repos: []string{"/a", "/b"}}) // replaces, keeps position
	if len(ws) != 2 || ws[0].Name != "shop" || len(ws[0].Repos) != 2 {
		t.Fatalf("Upsert = %+v", ws)
	}
	e, ok := Find(ws, "SHOP")
	if !ok || e.Name != "shop" {
		t.Fatalf("Find = %+v %v", e, ok)
	}
	if _, ok := Find(ws, "nope"); ok {
		t.Fatal("Find(nope) found something")
	}
	ws = Delete(ws, "INFRA")
	if len(ws) != 1 || ws[0].Name != "shop" {
		t.Fatalf("Delete = %+v", ws)
	}
}

func TestSaveLeavesNoTempLitter(t *testing.T) {
	t.Parallel()
	p := tmpFile(t)
	if err := Save(p, []Entry{{Name: "x", Repos: []string{"/x"}}}); err != nil {
		t.Fatal(err)
	}
	if err := Save(p, nil); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.Name() != filepath.Base(p) {
			t.Errorf("litter left behind: %s", e.Name())
		}
	}
	if got := Load(p); len(got) != 0 {
		t.Fatalf("Load after Save(nil) = %+v", got)
	}
}

// Update applies an edit to the file as it is at that moment, under the
// lock: an entry another writer added since this process last loaded the
// file survives the edit.
func TestUpdateMergesUnderLock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "workspaces.toml")
	if err := Save(path, []Entry{{Name: "shop"}}); err != nil {
		t.Fatal(err)
	}
	stale := Load(path)
	if err := Save(path, []Entry{{Name: "shop"}, {Name: "other"}}); err != nil {
		t.Fatal(err)
	}
	got, err := Update(path, func(ws []Entry) []Entry { return Upsert(ws, Entry{Name: "shop", Repos: []string{"/r/1"}}) })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Repos[0] != "/r/1" || got[1].Name != "other" || len(stale) != 1 {
		t.Fatalf("got %+v", got)
	}
	if on := Load(path); len(on) != 2 {
		t.Fatalf("file = %+v", on)
	}
}
