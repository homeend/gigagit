package agentdocs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/homeend/gigagit/internal/markdown"
)

func TestParseAnchorDest(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		dest string
		want Anchor
		ok   bool
	}{
		{"a/b.go", Anchor{Path: "a/b.go"}, true},
		{"./a.go:12", Anchor{Path: "a.go", Start: 12, End: 12}, true},
		{"a.go:3-9", Anchor{Path: "a.go", Start: 3, End: 9}, true},
		{" a.go:3 ", Anchor{Path: "a.go", Start: 3, End: 3}, true},
		{"a:b.go", Anchor{Path: "a:b.go"}, true},
		{"Makefile:4", Anchor{Path: "Makefile", Start: 4, End: 4}, true},
		{"note:t7", Anchor{Note: "t7"}, true},
		{"a.go:9-3", Anchor{}, false},
		{"a.go:0", Anchor{}, false},
		{"note:x", Anchor{}, false},
		{"/etc/passwd", Anchor{}, false},
		{"../x", Anchor{}, false},
		{"a/../b", Anchor{}, false},
		{"a\\b", Anchor{}, false},
		{"C:/x", Anchor{}, false},
		{"http://x", Anchor{}, false},
		{"ftp://x/a", Anchor{}, false},
		{"a b.go", Anchor{}, false},
		{"", Anchor{}, false},
		{"./", Anchor{}, false},
	} {
		got, ok := ParseAnchorDest(tc.dest)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseAnchorDest(%q) = %+v, %v; want %+v, %v", tc.dest, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseOverviewNumbersAnchorsAndCapsThem(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i <= MaxAnchors; i++ {
		fmt.Fprintf(&b, "- [label%d](f%d.go)\n", i, i)
	}
	doc, as := ParseOverview(b.String())
	if len(as) != MaxAnchors || as[0].Dest != "f0.go" || as[0].Path != "f0.go" {
		t.Fatalf("anchors = %d, first %+v", len(as), as[0])
	}
	var kinds []string
	var walk func([]markdown.Inline)
	walk = func(in []markdown.Inline) {
		for _, n := range in {
			if n.Kind == markdown.InAnchor {
				kinds = append(kinds, n.Text)
			} else if n.Kind == markdown.InText && strings.Contains(n.Text, fmt.Sprint("label", MaxAnchors)) {
				kinds = append(kinds, "text")
			}
			walk(n.In)
		}
	}
	var blocks func([]markdown.Block)
	blocks = func(bs []markdown.Block) {
		for _, bl := range bs {
			walk(bl.Inline)
			blocks(bl.Blocks)
			for _, it := range bl.Items {
				blocks(it.Blocks)
			}
		}
	}
	blocks(doc.Blocks)
	if len(kinds) != MaxAnchors+1 || kinds[0] != "0" || kinds[MaxAnchors-1] != fmt.Sprint(MaxAnchors-1) || kinds[MaxAnchors] != "text" {
		t.Fatalf("inline numbering = %v", kinds)
	}
}

func TestAddOverviewNumbersFromTheStoreAndKeepsAnchorsInOrder(t *testing.T) {
	t.Parallel()
	s := New()
	s.NextFileSeq() // a plain file took f1
	o, err := s.AddOverview("/r", "/r", "Tour", "# T\n\n[a](a.go) then [b](b.go:3-4) and [n](note:t9) and [web](https://x.y)\n")
	if err != nil {
		t.Fatal(err)
	}
	if o.ID != "f2" || o.Seq != 2 || o.Title != "Tour" || len(o.Anchors) != 3 {
		t.Fatalf("overview = %+v", o)
	}
	if a := o.Anchors[1]; a.Dest != "b.go:3-4" || a.Path != "b.go" || a.Start != 3 || a.End != 4 {
		t.Fatalf("anchor 1 = %+v", a)
	}
	if o.Anchors[2].Note != "t9" {
		t.Fatalf("anchor 2 = %+v", o.Anchors[2])
	}
	if got := s.Overviews("/r"); len(got) != 1 || got[0].ID != "f2" {
		t.Fatalf("overviews = %+v", got)
	}
	if got := s.Overviews("/other"); len(got) != 0 {
		t.Fatalf("another root sees %+v", got)
	}
}

func TestOverviewLimits(t *testing.T) {
	t.Parallel()
	s := New()
	for _, tc := range []struct{ title, text, want string }{
		{"T", "  ", "an overview needs text"},
		{"T", strings.Repeat("x", MaxOverviewBytes+1), "the text is over 64 KiB"},
		{" ", "x", "an overview needs a title"},
		{"a\nb", "x", "the title must be one line of at most 200 characters"},
		{strings.Repeat("é", MaxOverviewTitle+1), "x", "the title must be one line of at most 200 characters"},
	} {
		if _, err := s.AddOverview("/r", "/r", tc.title, tc.text); err == nil || err.Error() != tc.want {
			t.Errorf("%q/%d: err = %v, want %q", tc.title, len(tc.text), err, tc.want)
		}
	}
	for i := 0; i < MaxOverviewsPerRoot; i++ {
		if _, err := s.AddOverview("/r", "/r", "T", "x"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddOverview("/r", "/r", "T", "x"); err == nil || err.Error() != "20 overviews are open; remove one first" {
		t.Fatalf("cap: %v", err)
	}
	if _, err := s.AddOverview("/other", "/other", "T", "x"); err != nil {
		t.Fatal("the cap is per root")
	}
}

func TestSetKeepsTheIdAndRemoveSignals(t *testing.T) {
	t.Parallel()
	s := New()
	o, _ := s.AddOverview("/r", "/r", "T", "[a](a.go)")
	ch, cancel := s.Subscribe()
	defer cancel()
	set, err := s.SetOverview(o.ID, "", "[b](b.go) [c](c.go)")
	if err != nil || set.ID != o.ID || set.Title != "T" || len(set.Anchors) != 2 {
		t.Fatalf("set = %+v %v", set, err)
	}
	<-ch
	if set, err = s.SetOverview(o.ID, " T2 ", "x"); err != nil || set.Title != "T2" {
		t.Fatalf("set title = %+v %v", set, err)
	}
	<-ch
	if _, err := s.SetOverview(o.ID, "", " "); err == nil || err.Error() != "an overview needs text" {
		t.Fatalf("set empty: %v", err)
	}
	if _, err := s.SetOverview("f99", "", "x"); err == nil || err.Error() != "no overview f99" {
		t.Fatalf("set unknown: %v", err)
	}
	if !s.RemoveOverview(o.ID) || s.RemoveOverview(o.ID) {
		t.Fatal("remove exactly once")
	}
	<-ch
	if _, ok := s.Overview(o.ID); ok {
		t.Fatal("a removed overview is still there")
	}
}

func TestOverviewCopiesDoNotAlias(t *testing.T) {
	t.Parallel()
	s := New()
	o, _ := s.AddOverview("/r", "/r", "T", "[a](a.go)")
	o.Anchors[0].Missing = true
	if got, _ := s.Overview(o.ID); got.Anchors[0].Missing {
		t.Fatal("a caller's copy wrote into the store")
	}
}

func TestCheckAnchorsStatsFilesAndLooksUpNotesInTheRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "here.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := New()
	root := "KEY" // the caller's key; files are stat'ed under dir
	n, _ := s.AddNote(root, "here.go", []string{"x"}, 1, 1, "s", "", "")
	other, _ := s.AddNote("/elsewhere", "o.go", []string{"x"}, 1, 1, "s", "", "")
	o, _ := s.AddOverview(root, dir, "T", "[h](here.go) [g](gone.go) [n](note:"+n.ID+") [o](note:"+other.ID+") [d](sub)")
	if !s.CheckAnchors(o.ID) {
		t.Fatal("the first check found missing anchors: want changed")
	}
	got, _ := s.Overview(o.ID)
	var miss []bool
	for _, a := range got.Anchors {
		miss = append(miss, a.Missing)
	}
	if fmt.Sprint(miss) != "[false true false true true]" {
		t.Fatalf("missing = %v", miss)
	}
	if s.CheckAnchors(o.ID) {
		t.Fatal("an unchanged check reported a change")
	}
	s.RemoveNote(n.ID)
	if !s.CheckAnchors(o.ID) {
		t.Fatal("a removed note did not change the check")
	}
	if s.CheckAnchors("f99") {
		t.Fatal("an unknown id changed something")
	}
}

func TestSetAnchorMissing(t *testing.T) {
	t.Parallel()
	s := New()
	o, _ := s.AddOverview("/r", "/r", "T", "[a](a.go) [b](b.go)")
	if !s.SetAnchorMissing(o.ID, 1, true) || s.SetAnchorMissing(o.ID, 1, true) {
		t.Fatal("set once changes, twice does not")
	}
	if s.SetAnchorMissing(o.ID, 5, true) || s.SetAnchorMissing("f99", 0, true) {
		t.Fatal("out of range changed something")
	}
	if got, _ := s.Overview(o.ID); got.Anchors[0].Missing || !got.Anchors[1].Missing {
		t.Fatalf("anchors = %+v", got.Anchors)
	}
}

func TestOverviewReplyProse(t *testing.T) {
	t.Parallel()
	o := Overview{ID: "f7", Title: `A "tour"`, Text: "txt", Anchors: []Anchor{{Dest: "a.go:3"}, {Dest: "note:t2", Note: "t2", Missing: true}}}
	if got := AnchorReference(o, o.Anchors[0]); got != `gg overview f7 "A \"tour\"" → a.go:3` {
		t.Fatalf("reference = %q", got)
	}
	w := OverviewWire(o, "shown", false)
	if w.ID != "f7" || w.State != "shown" || w.Anchors != 2 || fmt.Sprint(w.Unresolved) != "[note:t2]" || w.Text != "" {
		t.Fatalf("wire = %+v", w)
	}
	if w = OverviewWire(o, "background", true); w.Text != "txt" {
		t.Fatalf("wire with text = %+v", w)
	}
}

// The stamp changes exactly when what a tab draws of the overview does: its
// title or text, an anchor's missing flag, or where an anchored note sits —
// and not on a note no anchor names.
func TestOverviewStampFollowsWhatATabDraws(t *testing.T) {
	t.Parallel()
	s := New()
	n, _ := s.AddNote("/r", "a.go", []string{"x", "y"}, 1, 1, "s", "", "")
	o, _ := s.AddOverview("/r", "/r", "T", "[a](a.go) [n](note:"+n.ID+")")
	st := s.OverviewStamp(o.ID)
	step := func(what string, changes bool, do func()) {
		t.Helper()
		do()
		got := s.OverviewStamp(o.ID)
		if (got != st) != changes {
			t.Fatalf("%s: stamp changed = %v, want %v", what, got != st, changes)
		}
		st = got
	}
	if st == "" || s.OverviewStamp("f99") != "" {
		t.Fatalf("stamp %q, unknown %q", st, s.OverviewStamp("f99"))
	}
	step("an unrelated note", false, func() { s.AddNote("/r", "b.go", []string{"x"}, 1, 1, "s", "", "") })
	step("an unrelated note in another root", false, func() { s.AddNote("/o", "a.go", []string{"x"}, 1, 1, "s", "", "") })
	step("a missing flag", true, func() { s.SetAnchorMissing(o.ID, 0, true) })
	step("the anchored note moves", true, func() { s.Align("/r", "a.go", []string{"new", "x", "y"}) })
	step("the anchored note goes", true, func() { s.RemoveNote(n.ID) })
	step("the title", true, func() { s.SetOverview(o.ID, "U", "[a](a.go) [n](note:"+n.ID+")") })
	step("the text", true, func() { s.SetOverview(o.ID, "", "[a](a.go)") })
}

// A link past the cap turns into its label as plain text — a label over a
// line break keeps the space between its words.
func TestParseOverviewPlainLabelPastTheCapKeepsTheBreakSpace(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("[x](a.go) ", MaxAnchors) + "[two\nwords](a.go)"
	doc, as := ParseOverview(text)
	if len(as) != MaxAnchors {
		t.Fatalf("%d anchors", len(as))
	}
	in := doc.Blocks[0].Inline
	if last := in[len(in)-1]; last.Kind != markdown.InText || !strings.HasSuffix(last.Text, "two words") {
		t.Fatalf("last inline = %+v", last)
	}
}

func TestFileTourIsKeyed(t *testing.T) {
	s := New()
	o, added, err := s.FileTour("brief:p/s1", "/r", "/r", "Brief — a", "one")
	if err != nil || !added || o.Title != "Brief — a" {
		t.Fatalf("first file: %+v %v %v", o, added, err)
	}
	o2, added, err := s.FileTour("brief:p/s1", "/r", "/r", "Brief — b", "two")
	if err != nil || added || o2.ID != o.ID || o2.Text != "two" || o2.Title != "Brief — b" {
		t.Fatalf("refile must replace in place: %+v %v %v", o2, added, err)
	}
	if id, ok := s.TourID("brief:p/s1"); !ok || id != o.ID {
		t.Fatalf("TourID = %q %v", id, ok)
	}
	s.RemoveOverview(o.ID) // the user closed it (X)
	if _, ok := s.TourID("brief:p/s1"); ok {
		t.Fatal("a closed tour has no id")
	}
	o3, added, err := s.FileTour("brief:p/s1", "/r", "/r", "Brief — c", "three")
	if err != nil || !added || o3.ID == o.ID {
		t.Fatalf("a closed tour is filed anew: %+v %v %v", o3, added, err)
	}
	if got := len(s.Overviews("/r")); got != 1 {
		t.Fatalf("overviews = %d, want 1", got)
	}
}

func TestFileTourAtTheCap(t *testing.T) {
	s := New()
	for i := 0; i < MaxOverviewsPerRoot; i++ {
		if _, err := s.AddOverview("/r", "/r", "o", "x"); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.FileTour("report:p/s1", "/r", "/r", "Report", "x"); err == nil || !strings.Contains(err.Error(), "overviews are open") {
		t.Fatalf("cap: %v", err)
	}
}

// Two callers filing one key at once (the TUI's wake and a web click) file
// one overview: the second replaces the first, never orphans a twin that
// counts against the cap.
func TestFileTourConcurrentFilesOne(t *testing.T) {
	for round := 0; round < 50; round++ {
		s := New()
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, _, err := s.FileTour("report:p/s1", "/r", "/r", "Report", "x"); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		if n := len(s.Overviews("/r")); n != 1 {
			t.Fatalf("round %d: %d overviews for one key", round, n)
		}
	}
}

func TestParseOverviewKeepsTheLabel(t *testing.T) {
	t.Parallel()
	_, as := ParseOverview("see [the **lock** order](a.go:5-8) and [x](note:t3)")
	if len(as) != 2 || as[0].Label != "the lock order" || as[1].Label != "x" {
		t.Fatalf("anchors = %+v", as)
	}
}
