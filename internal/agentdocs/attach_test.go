package agentdocs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func within(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func attached(t *testing.T) (*Store, string, string) {
	t.Helper()
	wt := t.TempDir()
	dir := filepath.Join(t.TempDir(), "docs")
	s := New()
	if err := s.Attach(root, wt, dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, wt, dir
}

func TestAttachLoadsWhatIsThereAndIgnoresTheRest(t *testing.T) {
	t.Parallel()
	wt := t.TempDir()
	dir := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRaw(t, dir, "ov-1.md", "+++\ntitle = \"One\"\ncreated = 2026-10-18T10:00:00Z\n+++\n[a](a.go:1)\n")
	writeRaw(t, dir, "ov-0.md", "+++\ntitle = \"Zero\"\ncreated = 2026-10-18T09:00:00Z\n+++\nbody\n")
	writeRaw(t, dir, "n-1.toml", "path = \"a.go\"\nstart = 2\nend = 2\nsummary = \"s\"\nlines = [\"b\"]\n")
	writeRaw(t, dir, "junk.txt", "x")
	s := New()
	if err := s.Attach(root, wt, dir); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Attached(root) != dir || s.Attached("/other") != "" {
		t.Fatal("Attached")
	}
	ovs := s.Overviews(root)
	if len(ovs) != 2 || ovs[0].ID != "ov-0" || ovs[1].ID != "ov-1" || ovs[1].Title != "One" || len(ovs[1].Anchors) != 1 {
		t.Fatalf("overviews = %+v", ovs)
	}
	n, ok := s.FindNote("n-1")
	if !ok || n.Path != "a.go" || n.Start != 2 || n.End != 2 || n.Root != root {
		t.Fatalf("note = %+v %v", n, ok)
	}
	if p := s.Problems(root); len(p) != 1 || p[0].Name != "junk.txt" {
		t.Fatalf("problems = %+v", p)
	}
}

func TestColdNoteIsPlacedOnFirstAlign(t *testing.T) {
	t.Parallel()
	s, _, dir := attached(t)
	writeRaw(t, dir, "n-1.toml", "path = \"a.go\"\nstart = 2\nend = 3\nsummary = \"s\"\nlines = [\"b\", \"c\"]\n")
	s.Reload(root)
	// The file gained a line above: the note follows its text.
	notes := s.AlignedNotes(root, "a.go", []string{"x", "a", "b", "c"})
	if len(notes) != 1 || notes[0].Start != 3 || notes[0].End != 4 || notes[0].Outdated {
		t.Fatalf("placed = %+v", notes)
	}
	// From here on the usual re-anchoring runs.
	notes = s.AlignedNotes(root, "a.go", []string{"x", "a", "b", "C"})
	if len(notes) != 1 || !notes[0].Outdated || notes[0].Start != 3 || notes[0].End != 4 {
		t.Fatalf("after edit = %+v", notes)
	}
	// A note loaded for a path whose content the store already holds is
	// placed at load.
	writeRaw(t, dir, "n-2.toml", "path = \"a.go\"\nstart = 1\nend = 1\nsummary = \"t\"\nlines = [\"a\"]\n")
	s.Reload(root)
	n, _ := s.FindNote("n-2")
	if n.Start != 2 || n.Outdated {
		t.Fatalf("n-2 = %+v", n)
	}
}

func TestAddNoteOnAnAttachedRootWritesAFile(t *testing.T) {
	t.Parallel()
	s, _, dir := attached(t)
	n, err := s.AddNoteMeta(root, "a.go", []string{"a", "b", "c"}, 2, 3, NoteText{Summary: "look", Rationale: "why", Author: "ada"})
	if err != nil {
		t.Fatal(err)
	}
	if !ValidID(n.ID) || len(n.ID) < 10 {
		t.Fatalf("id = %q", n.ID)
	}
	d, err := ReadDoc(dir, n.ID, KindNote)
	if err != nil || d.Note.Summary != "look" || d.Note.Author != "ada" || strings.Join(d.Note.Lines, "|") != "b|c" || d.Note.Created.IsZero() {
		t.Fatalf("file = %+v %v", d, err)
	}
	if got, _ := s.FindNote(n.ID); got.Start != 2 || got.End != 3 || got.Outdated {
		t.Fatalf("loaded back = %+v", got)
	}
	if !s.RemoveNote(n.ID) {
		t.Fatal("remove")
	}
	if _, err := ReadDoc(dir, n.ID, KindNote); err == nil {
		t.Fatal("remove must unlink the file")
	}
}

func TestAddOverviewOnAnAttachedRootHasNoCapAndSetRewrites(t *testing.T) {
	t.Parallel()
	s, wt, dir := attached(t)
	var last Overview
	for i := 0; i < MaxOverviewsPerRoot+1; i++ {
		o, err := s.AddOverview(root, wt, "T", "[a](a.go:1)\n")
		if err != nil {
			t.Fatalf("overview %d: %v", i, err)
		}
		last = o
	}
	if len(s.Overviews(root)) != MaxOverviewsPerRoot+1 {
		t.Fatal("an attached root has no overview cap")
	}
	before, _ := ReadDoc(dir, last.ID, KindOverview)
	if _, err := s.SetOverview(last.ID, "", "new text\n"); err != nil {
		t.Fatal(err)
	}
	after, _ := ReadDoc(dir, last.ID, KindOverview)
	if after.Over.Text != "new text\n" || after.Over.Title != "T" || !after.Over.Created.Equal(before.Over.Created) {
		t.Fatalf("set = %+v", after.Over)
	}
	if !s.RemoveOverview(last.ID) {
		t.Fatal("remove")
	}
	if _, err := ReadDoc(dir, last.ID, KindOverview); err == nil {
		t.Fatal("remove must unlink")
	}
}

func TestExternalWritesAppearAndRemovalsDisappear(t *testing.T) {
	t.Parallel()
	s, _, dir := attached(t)
	ch, cancel := s.Subscribe()
	defer cancel()
	// A writer that is not this process: temp + rename.
	writeRaw(t, dir, ".ov-9.ab.tmp", "+++\ntitle = \"Nine\"\n+++\nbody\n")
	if err := os.Rename(filepath.Join(dir, ".ov-9.ab.tmp"), filepath.Join(dir, "ov-9.md")); err != nil {
		t.Fatal(err)
	}
	within(t, 5*time.Second, func() bool { _, ok := s.Overview("ov-9"); return ok })
	select {
	case <-ch:
	default:
		t.Fatal("a reload that changed something must signal")
	}
	// Replaced in place: same id, new text.
	writeRaw(t, dir, ".ov-9.cd.tmp", "+++\ntitle = \"Nine\"\n+++\nnew body\n")
	if err := os.Rename(filepath.Join(dir, ".ov-9.cd.tmp"), filepath.Join(dir, "ov-9.md")); err != nil {
		t.Fatal(err)
	}
	within(t, 5*time.Second, func() bool { o, _ := s.Overview("ov-9"); return o.Text == "new body\n" })
	if err := os.Remove(filepath.Join(dir, "ov-9.md")); err != nil {
		t.Fatal(err)
	}
	within(t, 5*time.Second, func() bool { _, ok := s.Overview("ov-9"); return !ok })
}

func TestPollFallbackFindsChangesWithoutAWatch(t *testing.T) {
	// Not parallel: it shortens the package-wide poll interval.
	old := pollEvery
	pollEvery = 50 * time.Millisecond
	t.Cleanup(func() { pollEvery = old })
	wt := t.TempDir()
	dir := filepath.Join(t.TempDir(), "docs")
	s := New()
	s.noWatch = true // test hook: the poll alone
	if err := s.Attach(root, wt, dir); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	writeRaw(t, dir, "ov-1.md", okOverview)
	within(t, 3*time.Second, func() bool { _, ok := s.Overview("ov-1"); return ok })
	// The whole directory replaced underneath: no crash, no ghosts.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	within(t, 3*time.Second, func() bool { _, ok := s.Overview("ov-1"); return !ok })
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRaw(t, dir, "ov-2.md", okOverview)
	within(t, 3*time.Second, func() bool { _, ok := s.Overview("ov-2"); return ok })
}

func TestFileTourUsesAFixedNameAndReplaces(t *testing.T) {
	t.Parallel()
	s, wt, dir := attached(t)
	o, added, err := s.FileTour("report:gg-12345/7", root, wt, "Report", "first\n")
	if err != nil || !added || o.ID != "tour-report-gg-12345-7" {
		t.Fatalf("%+v %v %v", o, added, err)
	}
	o2, added, err := s.FileTour("report:gg-12345/7", root, wt, "Report", "second\n")
	if err != nil || added || o2.ID != o.ID {
		t.Fatalf("%+v %v %v", o2, added, err)
	}
	if d, _ := ReadDoc(dir, o.ID, KindOverview); d.Over.Text != "second\n" {
		t.Fatalf("file = %+v", d.Over)
	}
	if id, ok := s.TourID("report:gg-12345/7"); !ok || id != o.ID {
		t.Fatal("TourID")
	}
	s.RemoveOverview(o.ID)
	if _, ok := s.TourID("report:gg-12345/7"); ok {
		t.Fatal("a deleted tour has no id")
	}
}

func TestDetachDropsTheRootAndUnattachedRootsKeepMemoryIDs(t *testing.T) {
	t.Parallel()
	s, wt, dir := attached(t)
	writeRaw(t, dir, "ov-1.md", okOverview)
	s.Reload(root)
	s.Detach(root)
	if len(s.Overviews(root)) != 0 || s.Attached(root) != "" {
		t.Fatal("detach must drop the root's documents")
	}
	o, err := s.AddOverview("/memory", wt, "T", "body\n")
	if err != nil || o.ID != "f1" {
		t.Fatalf("an unattached root numbers as before: %+v %v", o, err)
	}
	n, err := s.AddNote("/memory", "a.go", []string{"a"}, 1, 1, "s", "", "")
	if err != nil || n.ID != "t1" {
		t.Fatalf("%+v %v", n, err)
	}
}

func TestOverviewsOrderByCreatedThenID(t *testing.T) {
	t.Parallel()
	s, _, dir := attached(t)
	writeRaw(t, dir, "b.md", "+++\ntitle = \"B\"\ncreated = 2026-10-18T10:00:00Z\n+++\nx\n")
	writeRaw(t, dir, "a.md", "+++\ntitle = \"A\"\ncreated = 2026-10-18T10:00:00Z\n+++\nx\n")
	writeRaw(t, dir, "c.md", "+++\ntitle = \"C\"\ncreated = 2026-10-18T09:00:00+02:00\n+++\nx\n") // 07:00Z
	writeRaw(t, dir, "d.md", okOverview)                                                          // no created: mtime (now) → last
	s.Reload(root)
	var ids []string
	for _, o := range s.Overviews(root) {
		ids = append(ids, o.ID)
	}
	if strings.Join(ids, ",") != "c,a,b,d" {
		t.Fatalf("order = %v", ids)
	}
}

func TestAttachedWorktree(t *testing.T) {
	t.Parallel()
	s, wt, _ := attached(t)
	if got := s.AttachedWorktree(root); got != wt {
		t.Fatalf("AttachedWorktree = %q, want %q", got, wt)
	}
	if got := s.AttachedWorktree("other"); got != "" {
		t.Fatalf("unknown root = %q", got)
	}
	s.Detach(root)
	if got := s.AttachedWorktree(root); got != "" {
		t.Fatalf("after Detach = %q", got)
	}
}
