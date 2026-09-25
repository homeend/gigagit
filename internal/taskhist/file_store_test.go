package taskhist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rec(id string) Record {
	return Record{ID: id, Key: "review — " + id, Kind: "review", State: "done", Started: time.Unix(1, 0).UTC(), Ended: time.Unix(2, 0).UTC()}
}

func stores(t *testing.T) map[string]Store {
	return map[string]Store{"file": NewFileStore(t.TempDir()), "mem": NewMemStore()}
}

func TestAddListResultTail(t *testing.T) {
	t.Parallel()
	for name, st := range stores(t) {
		if err := st.Add(rec("a"), "result a", "tail a"); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := st.Add(rec("b"), "", ""); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		list, err := st.List()
		if err != nil || len(list) != 2 || list[0].ID != "b" || list[1].ID != "a" {
			t.Fatalf("%s: List = %+v, %v (want newest first)", name, list, err)
		}
		if list[1].Key != "review — a" || !list[1].Started.Equal(time.Unix(1, 0)) {
			t.Fatalf("%s: record did not round-trip: %+v", name, list[1])
		}
		if r, err := st.Result("a"); err != nil || r != "result a" {
			t.Fatalf("%s: Result = %q, %v", name, r, err)
		}
		if r, err := st.Tail("a"); err != nil || r != "tail a" {
			t.Fatalf("%s: Tail = %q, %v", name, r, err)
		}
		if r, err := st.Result("b"); err != nil || r != "" {
			t.Fatalf("%s: a record with no result reads %q, %v", name, r, err)
		}
	}
}

func TestPruneDropsOldestAndItsFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := NewFileStore(root)
	for i := 0; i < Max+1; i++ {
		if err := st.Add(rec(string(rune('A'+i%26))+strings.Repeat("x", i/26)), "r", "t"); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := st.List()
	if len(list) != Max {
		t.Fatalf("kept %d records, want %d", len(list), Max)
	}
	if _, err := os.Stat(filepath.Join(root, "A.result")); !os.IsNotExist(err) {
		t.Fatal("the pruned record's result file survived")
	}
	if _, err := os.Stat(filepath.Join(root, "A.tail")); !os.IsNotExist(err) {
		t.Fatal("the pruned record's tail file survived")
	}
}

func TestRemoveDeletesRecordAndFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	st := NewFileStore(root)
	_ = st.Add(rec("a"), "r", "t")
	if err := st.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if list, _ := st.List(); len(list) != 0 {
		t.Fatalf("List = %+v after Remove", list)
	}
	if _, err := os.Stat(filepath.Join(root, "a.result")); !os.IsNotExist(err) {
		t.Fatal("result file survived Remove")
	}
}

func TestCorruptIndexIsAnErrorNotAWipe(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tasks.toml"), []byte("not = [toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := NewFileStore(root)
	if _, err := st.List(); err == nil {
		t.Fatal("List of a corrupt index must fail")
	}
	if err := st.Add(rec("a"), "", ""); err == nil {
		t.Fatal("Add over a corrupt index must fail, not rewrite it from empty")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "tasks.toml")); string(b) != "not = [toml" {
		t.Fatal("the corrupt index was overwritten")
	}
}

func TestUnwritableRootFails(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewFileStore(filepath.Join(file, "sub")).Add(rec("a"), "r", ""); err == nil {
		t.Fatal("Add under a regular file must fail")
	}
}

func TestTrimTailKeepsTheEnd(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("é", MaxTail) // 2 bytes per rune
	got := TrimTail(s)
	if len(got) > MaxTail || !strings.HasSuffix(s, got) || !strings.HasPrefix(got, "é") {
		t.Fatalf("TrimTail kept %d bytes, prefix %q", len(got), got[:4])
	}
}

func TestNewIDIsUniqueAndSafe(t *testing.T) {
	t.Parallel()
	now := time.Now()
	a, b := NewID(now), NewID(now)
	if a == b || strings.ContainsAny(a, `/\: `) {
		t.Fatalf("NewID = %q, %q", a, b)
	}
}
