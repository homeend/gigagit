package prcache

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

var t0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	s := New(t.TempDir(), DefaultMax)
	e := Entry{Number: 7, PR: model.PullRequest{Number: 7, Title: "x", HeadSHA: "abc"}, Full: true,
		Comments: []model.ForgeComment{{ID: "c1", Body: "hi"}}, HasComments: true, ReadAt: t0, OpenedAt: t0}
	e.PutDerived(Derived{MergeBase: "m1", Source: "s1", Files: 1, ComputedAt: t0,
		FileList: []model.CommitFile{{Status: "M", Path: "a.go"}}})
	if err := s.Save(e); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Load(7)
	if !ok || got.PR.Title != "x" || len(got.Comments) != 1 || !got.ReadAt.Equal(t0) {
		t.Fatalf("Load = %+v, %v", got, ok)
	}
	d, ok := got.DerivedFor("m1", "s1")
	if !ok || d.FileList[0].Path != "a.go" {
		t.Fatalf("DerivedFor = %+v, %v", d, ok)
	}
	if _, ok := got.DerivedFor("m2", "s1"); ok { // the base branch moved past a new merge base
		t.Fatal("a different pair must miss")
	}
}

func TestPutDerivedKeepsNewestTwo(t *testing.T) {
	t.Parallel()
	var e Entry
	for _, s := range []string{"a", "b", "c"} {
		e.PutDerived(Derived{MergeBase: "m", Source: s})
	}
	if len(e.Derived) != 2 || e.Derived[0].Source != "c" || e.Derived[1].Source != "b" {
		t.Fatalf("Derived = %+v", e.Derived)
	}
	e.PutDerived(Derived{MergeBase: "m", Source: "b", Files: 9}) // same pair: replaced, moved to front
	if len(e.Derived) != 2 || e.Derived[0].Source != "b" || e.Derived[0].Files != 9 {
		t.Fatalf("Derived after replace = %+v", e.Derived)
	}
}

func TestSaveTrimsLeastRecentlyOpened(t *testing.T) {
	t.Parallel()
	s := New(t.TempDir(), 3)
	for n := 1; n <= 4; n++ {
		if err := s.Save(Entry{Number: n, OpenedAt: t0.Add(time.Duration(n) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := s.Load(1); ok {
		t.Error("the least recently opened entry must be dropped")
	}
	for n := 2; n <= 4; n++ {
		if _, ok := s.Load(n); !ok {
			t.Errorf("entry %d dropped", n)
		}
	}
}

func TestCorruptFileIsQuarantined(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := New(dir, DefaultMax)
	if err := os.WriteFile(filepath.Join(dir, "pr-9.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Load(9); ok {
		t.Fatal("a corrupt entry must read as absent")
	}
	m, _ := filepath.Glob(filepath.Join(dir, "pr-9.json.corrupt-*"))
	if len(m) != 1 {
		t.Fatalf("quarantine files = %v", m)
	}
	if err := os.WriteFile(filepath.Join(dir, "list.json"), []byte("]"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LoadList(); ok {
		t.Fatal("a corrupt list must read as absent")
	}
}

func TestRepoRoundTrip(t *testing.T) {
	t.Parallel()
	s := New(t.TempDir(), DefaultMax)
	if _, ok := s.LoadRepo(); ok {
		t.Fatal("an empty cache has no repo")
	}
	if err := s.SaveRepo(Repo{Slug: "o/r", URL: "https://github.com/o/r", ReadAt: t0}); err != nil {
		t.Fatal(err)
	}
	if r, ok := s.LoadRepo(); !ok || r.Slug != "o/r" {
		t.Fatalf("LoadRepo = %+v, %v", r, ok)
	}
}

func TestListRoundTripAndRemove(t *testing.T) {
	t.Parallel()
	s := New(t.TempDir(), DefaultMax)
	if err := s.SaveList(List{ReadAt: t0, Provider: "github", PRs: []model.PullRequest{{Number: 3}}}); err != nil {
		t.Fatal(err)
	}
	l, ok := s.LoadList()
	if !ok || l.Provider != "github" || len(l.PRs) != 1 {
		t.Fatalf("LoadList = %+v, %v", l, ok)
	}
	_ = s.Save(Entry{Number: 3})
	if err := s.Remove(3); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Load(3); ok {
		t.Fatal("removed entry still loads")
	}
	if err := s.Remove(3); err != nil {
		t.Fatalf("removing an absent entry: %v", err)
	}
}

// Review focus 2: a read time in the future is never fresh.
func TestFresh(t *testing.T) {
	t.Parallel()
	h8 := 8 * time.Hour
	cases := []struct {
		name   string
		readAt time.Time
		maxAge time.Duration
		want   bool
	}{
		{"just read", t0, h8, true},
		{"7h59m old", t0.Add(-h8 + time.Minute), h8, true},
		{"8h01m old", t0.Add(-h8 - time.Minute), h8, false},
		{"zero time", time.Time{}, h8, false},
		{"limit 0", t0, 0, false},
		{"read in the future", t0.Add(2 * time.Hour), h8, false},
		{"a minute ahead (clock jitter)", t0.Add(30 * time.Second), h8, true},
	}
	for _, c := range cases {
		if got := Fresh(c.readAt, t0, c.maxAge); got != c.want {
			t.Errorf("%s: Fresh = %v, want %v", c.name, got, c.want)
		}
	}
}

// Review focus 1: concurrent writers never leave a torn file.
func TestConcurrentSavesNeverTear(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, b := New(dir, DefaultMax), New(dir, DefaultMax) // two "processes"
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := a
			if i%2 == 1 {
				s = b
			}
			_ = s.Save(Entry{Number: 5, PR: model.PullRequest{Title: strings.Repeat("x", i*100)}, OpenedAt: t0})
		}()
	}
	wg.Wait()
	if _, ok := a.Load(5); !ok {
		t.Fatal("entry unreadable after concurrent saves")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.corrupt-*")); len(m) != 0 {
		t.Fatalf("a concurrent save tore the file: %v", m)
	}
}

func TestEntriesNewestOpenedFirst(t *testing.T) {
	t.Parallel()
	s := New(t.TempDir(), DefaultMax)
	_ = s.Save(Entry{Number: 1, OpenedAt: t0})
	_ = s.Save(Entry{Number: 2, OpenedAt: t0.Add(time.Hour)})
	es := s.Entries()
	if len(es) != 2 || es[0].Number != 2 || es[1].Number != 1 {
		t.Fatalf("Entries = %+v", es)
	}
}
