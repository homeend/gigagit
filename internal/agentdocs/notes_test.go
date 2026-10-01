package agentdocs

import (
	"strings"
	"testing"
)

const root = "/r"

func addOn(t *testing.T, s *Store, lines []string, start, end int) Note {
	t.Helper()
	n, err := s.AddNote(root, "f.go", lines, start, end, "s", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func noteOf(t *testing.T, s *Store, id string) Note {
	t.Helper()
	n, ok := s.FindNote(id)
	if !ok {
		t.Fatalf("note %s is gone", id)
	}
	return n
}

func abc() []string { return []string{"a", "b", "c", "d", "e"} }

func TestAddNoteKeepsTheRangeAndDefaultsTheAuthor(t *testing.T) {
	t.Parallel()
	s := New()
	n, err := s.AddNote(root, "f.go", abc(), 3, 4, "  look here  ", "because", "")
	if err != nil {
		t.Fatal(err)
	}
	if n.ID != "t1" || n.Start != 3 || n.End != 4 || n.Summary != "look here" || n.Author != "agent" || n.Root != root || n.Path != "f.go" {
		t.Fatalf("note = %+v", n)
	}
	if got := s.NoteText(n.ID); strings.Join(got, ",") != "c,d" {
		t.Fatalf("text = %q", got)
	}
}

func TestAddNoteRefusals(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", MaxNoteSummary+1)
	for _, tc := range []struct {
		name       string
		lines      []string
		start, end int
		summary    string
		rationale  string
		want       string
	}{
		{"no lines", nil, 1, 1, "s", "", "f.go has no lines to note"},
		{"zero line", abc(), 0, 1, "s", "", "a line number is 1-based"},
		{"backwards", abc(), 4, 3, "s", "", "the range ends before it starts"},
		{"past the end", abc(), 4, 6, "s", "", "line 6 is past the end of f.go (5 lines)"},
		{"no summary", abc(), 1, 1, "   ", "", "a note needs a summary"},
		{"long summary", abc(), 1, 1, long, "", "the summary is longer than 500 characters"},
		{"long rationale", abc(), 1, 1, "s", strings.Repeat("y", MaxNoteRationale+1), "the rationale is longer than 4000 characters"},
	} {
		s := New()
		if _, err := s.AddNote(root, "f.go", tc.lines, tc.start, tc.end, tc.summary, tc.rationale, ""); err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
		if s.NoteCount(root, "f.go") != 0 {
			t.Errorf("%s: a refused add left a note", tc.name)
		}
	}
}

func TestNoteCapPerFile(t *testing.T) {
	t.Parallel()
	s := New()
	for i := 0; i < MaxNotesPerFile; i++ {
		addOn(t, s, abc(), 1, 1)
	}
	if _, err := s.AddNote(root, "f.go", abc(), 1, 1, "s", "", ""); err == nil || err.Error() != "f.go already carries 50 notes" {
		t.Fatalf("err = %v", err)
	}
}

func TestNotesStayOrderedByLineThenAge(t *testing.T) {
	t.Parallel()
	s := New()
	a := addOn(t, s, abc(), 5, 5)
	b := addOn(t, s, abc(), 2, 3)
	c := addOn(t, s, abc(), 2, 2)
	ns, _ := s.Notes(root, "f.go")
	if len(ns) != 3 || ns[0].ID != b.ID || ns[1].ID != c.ID || ns[2].ID != a.ID {
		t.Fatalf("order = %+v", ns)
	}
	if !s.RemoveNote(b.ID) || s.RemoveNote(b.ID) || s.NoteCount(root, "f.go") != 2 {
		t.Fatal("RemoveNote must remove exactly once")
	}
	if n := s.ClearPath(root, "f.go"); n != 2 || s.NoteCount(root, "f.go") != 0 {
		t.Fatalf("ClearPath = %d", n)
	}
	if len(s.NotedPaths(root)) != 0 {
		t.Fatal("a cleared path is still listed")
	}
}

// The alignment rule (moved from tui/open_file_notes_test.go).

func TestNoteMovesWhenLinesAreInsertedAbove(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	s.Align(root, "f.go", []string{"NEW", "a", "b", "c", "d", "e"})
	if g := noteOf(t, s, n.ID); g.Start != 4 || g.End != 5 || g.Outdated {
		t.Fatalf("note = %+v, want 4-5 live", g)
	}
}

func TestNoteMovesWhenLinesAreDeletedAbove(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	s.Align(root, "f.go", []string{"b", "c", "d", "e"})
	if g := noteOf(t, s, n.ID); g.Start != 2 || g.End != 3 || g.Outdated {
		t.Fatalf("note = %+v, want 2-3 live", g)
	}
}

func TestNoteGoesOutdatedWhenEditedAndRecoversOnRevert(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	s.Align(root, "f.go", []string{"a", "b", "C-EDITED", "d", "e"})
	if g := noteOf(t, s, n.ID); !g.Outdated || g.Start != 3 || g.End != 4 {
		t.Fatalf("note = %+v, want 3-4 outdated", g)
	}
	s.Align(root, "f.go", abc())
	if g := noteOf(t, s, n.ID); g.Outdated || g.Start != 3 || g.End != 4 {
		t.Fatalf("after the revert: %+v, want 3-4 live", g)
	}
}

func TestOutdatedNoteIsClampedWhenTheFileShrinks(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	s.Align(root, "f.go", []string{"a"})
	if g := noteOf(t, s, n.ID); !g.Outdated || g.Start != 1 || g.End != 1 {
		t.Fatalf("note = %+v, want 1-1 outdated", g)
	}
}

func TestOutdatedNoteDoesNotJumpToAnAmbiguousCopy(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, []string{"x", "}", "y", "}", "z"}, 2, 2)
	s.Align(root, "f.go", []string{"x", "}!", "y", "}", "z"})
	if g := noteOf(t, s, n.ID); !g.Outdated || g.Start != 2 {
		t.Fatalf("note = %+v, want outdated on line 2", g)
	}
}

func TestNoteFollowsAUniqueMovedBlockAfterGoingOutdated(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	s.Align(root, "f.go", []string{"a", "b", "e"})
	s.Align(root, "f.go", []string{"a", "b", "e", "c", "d"})
	if g := noteOf(t, s, n.ID); g.Outdated || g.Start != 4 || g.End != 5 {
		t.Fatalf("note = %+v, want 4-5 live", g)
	}
}

// A full rewrite (far past any alignment guard) leaves the note outdated
// and inside the file.
func TestNoteSurvivesAFullRewrite(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	big := make([]string, 6000)
	for i := range big {
		big[i] = "q" + strings.Repeat("z", i%7)
	}
	s.Align(root, "f.go", big)
	if g := noteOf(t, s, n.ID); !g.Outdated || g.Start < 1 || g.End > len(big) {
		t.Fatalf("after a full rewrite: %+v", g)
	}
}

func TestAlignWithNoLinesTouchesNothing(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	if s.Align(root, "f.go", nil) {
		t.Fatal("an empty read (a placeholder) must not align")
	}
	if g := noteOf(t, s, n.ID); g.Start != 3 || g.Outdated {
		t.Fatalf("note = %+v", g)
	}
}

func TestAlignIsIdempotentAndReportsTheFingerprint(t *testing.T) {
	t.Parallel()
	s := New()
	addOn(t, s, abc(), 3, 4)
	next := []string{"NEW", "a", "b", "c", "d", "e"}
	if !s.Align(root, "f.go", next) {
		t.Fatal("new content must align")
	}
	if s.Align(root, "f.go", next) {
		t.Fatal("the same content a second time must be a no-op")
	}
	if _, fp := s.Notes(root, "f.go"); fp != Print(next) {
		t.Fatal("Notes must report the fingerprint of the content they sit on")
	}
}

// Review Focus 1: an old read aligning after a newer one is undone by the
// next read of the newest content.
func TestOutOfOrderAlignsConverge(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	newer := []string{"NEW", "a", "b", "c", "d", "e"}
	s.Align(root, "f.go", newer)
	s.Align(root, "f.go", abc()) // a stale read lands late
	s.Align(root, "f.go", newer) // the side that saw the mismatch re-reads
	if g := noteOf(t, s, n.ID); g.Start != 4 || g.End != 5 || g.Outdated {
		t.Fatalf("note = %+v, want 4-5 live", g)
	}
}

func TestLinesIsTheTUIsSplit(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"a\nb\n":     "a|b",
		"a\r\nb\r\n": "a|b",
		"a\rb":       "a|b",
		"a\n\n\n":    "a",
		"":           "",
		"\n\n":       "",
		"x\n\ny\n":   "x||y",
	} {
		if got := strings.Join(Lines([]byte(in)), "|"); got != want {
			t.Errorf("Lines(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNotedPathsAndRootsAreSeparate(t *testing.T) {
	t.Parallel()
	s := New()
	if _, err := s.AddNote("/r", "b.go", abc(), 1, 1, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNote("/r", "a.go", abc(), 1, 1, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNote("/other", "c.go", abc(), 1, 1, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	// In the order their first notes were placed (the TUI's } order: by seq).
	if got := strings.Join(s.NotedPaths("/r"), ","); got != "b.go,a.go" {
		t.Fatalf("NotedPaths = %s", got)
	}
}

// AlignedNotes aligns and reads under one lock: the positions it returns
// are always for the lines it was handed, whatever else aligns meanwhile.
func TestAlignedNotesReturnsPositionsForTheLinesGiven(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	newer := []string{"NEW", "a", "b", "c", "d", "e"}
	ns := s.AlignedNotes(root, "f.go", newer)
	if len(ns) != 1 || ns[0].ID != n.ID || ns[0].Start != 4 {
		t.Fatalf("notes = %+v, want 4-5 for the newer lines", ns)
	}
	if ns := s.AlignedNotes(root, "f.go", nil); ns != nil {
		t.Fatalf("no lines (a placeholder) must answer no notes, got %+v", ns)
	}
	if ns := s.AlignedNotes(root, "other.go", abc()); ns != nil {
		t.Fatalf("a path without notes: %+v", ns)
	}
}
