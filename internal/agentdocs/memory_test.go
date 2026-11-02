package agentdocs

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

const memID = "20261102T031500-a1b2"

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestMemorySurvivesReload(t *testing.T) {
	t.Parallel()
	s, wt, dir := attached(t)
	o, err := s.AddMemoryOverview(root, wt, memID, "T", "text", "a", true)
	if err != nil {
		t.Fatal(err)
	}
	if !o.Memory || !o.Plain || o.ID != memID || o.Author != "a" || o.Created.IsZero() {
		t.Fatalf("overview = %+v", o)
	}
	if got := dirEntries(t, dir); len(got) != 0 {
		t.Fatalf("memory document on disk: %v", got)
	}
	s.Reload(root)
	if _, ok := s.OverviewIn(root, memID); !ok {
		t.Fatal("Reload dropped a memory document")
	}
}

func TestMemorySurvivesAttachDetach(t *testing.T) {
	t.Parallel()
	s, wt, dir := attached(t)
	if _, err := s.AddMemoryOverview(root, wt, memID, "T", "text", "", true); err != nil {
		t.Fatal(err)
	}
	s.Detach(root)
	if _, ok := s.OverviewIn(root, memID); !ok {
		t.Fatal("Detach dropped a memory document")
	}
	if err := s.Attach(root, wt, dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.OverviewIn(root, memID); !ok {
		t.Fatal("Attach dropped a memory document")
	}
	if got := s.MemoryWorktrees(); len(got) != 1 || got[0] != wt {
		t.Fatalf("MemoryWorktrees = %v", got)
	}
	if got := s.OverviewDir(root, memID); got != wt {
		t.Fatalf("OverviewDir = %q", got)
	}
}

func TestMemoryRemoveAndSetTouchNoFile(t *testing.T) {
	t.Parallel()
	s, wt, dir := attached(t)
	file, err := s.AddOverview(root, wt, "F", "file text")
	if err != nil {
		t.Fatal(err)
	}
	before := dirEntries(t, dir)
	if _, err := s.AddMemoryOverview(root, wt, memID, "T", "text", "", false); err != nil {
		t.Fatal(err)
	}
	o, err := s.SetOverviewIn(root, memID, "T2", "new [a](a.go:1)")
	if err != nil {
		t.Fatal(err)
	}
	if !o.Memory || o.Title != "T2" || len(o.Anchors) != 1 {
		t.Fatalf("set = %+v", o)
	}
	if !s.RemoveOverviewIn(root, memID) {
		t.Fatal("remove said none")
	}
	if got := dirEntries(t, dir); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Fatalf("directory changed: %v, was %v", got, before)
	}
	if _, ok := s.OverviewIn(root, file.ID); !ok {
		t.Fatal("the file document went")
	}
	if len(s.MemoryWorktrees()) != 0 {
		t.Fatal("MemoryWorktrees after the last document")
	}
}

func TestLegacyUnattachedStillDrops(t *testing.T) {
	t.Parallel()
	s := New()
	wt := t.TempDir()
	o, err := s.AddOverview(root, wt, "T", "text")
	if err != nil {
		t.Fatal(err)
	}
	if o.Memory {
		t.Fatal("a legacy document is not marked memory")
	}
	if err := s.Attach(root, wt, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, ok := s.OverviewIn(root, o.ID); ok {
		t.Fatal("Attach kept a legacy memory document")
	}
}

func TestMemoryCapPerRoot(t *testing.T) {
	t.Parallel()
	s, wt, _ := attached(t)
	if _, err := s.AddOverview(root, wt, "F", "a file does not count"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxOverviewsPerRoot; i++ {
		id := "m-" + strconv.Itoa(i)
		if _, err := s.AddMemoryOverview(root, wt, id, "T", "x", "", true); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	if _, err := s.AddMemoryOverview(root, wt, "m-over", "T", "x", "", true); err == nil {
		t.Fatal("the 21st was accepted")
	}
	if _, err := s.AddMemoryOverview(root, wt, "m-0", "T", "y", "", true); err != nil {
		t.Fatalf("replacing at the cap: %v", err)
	}
}

func TestMemorySameIDReplaces(t *testing.T) {
	t.Parallel()
	s, wt, _ := attached(t)
	a, err := s.AddMemoryOverview(root, wt, memID, "T", "one", "", true)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.AddMemoryOverview(root, wt, memID, "T", "two", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if b.Text != "two" || b.Seq != a.Seq || len(s.Overviews(root)) != 1 {
		t.Fatalf("replace = %+v, %d docs", b, len(s.Overviews(root)))
	}
}

func TestMemoryIDHeldByFileRefused(t *testing.T) {
	t.Parallel()
	s, wt, _ := attached(t)
	f, err := s.AddOverview(root, wt, "F", "file")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddMemoryOverview(root, wt, f.ID, "T", "x", "", true); err == nil {
		t.Fatal("took a file's id")
	}
	if _, err := s.AddMemoryOverview(root, wt, "bad id", "T", "x", "", true); err == nil {
		t.Fatal("accepted an invalid id")
	}
}

func TestPlainHasNoAnchors(t *testing.T) {
	t.Parallel()
	s, wt, _ := attached(t)
	const text = "see [a](x.go:1) and <b>\x1b[31m\n"
	o, err := s.AddMemoryOverview(root, wt, memID, "T", text, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Anchors) != 0 || o.Text != text {
		t.Fatalf("plain = %+v", o)
	}
	if o, err = s.SetOverviewIn(root, memID, "", text+"more"); err != nil || len(o.Anchors) != 0 {
		t.Fatalf("set plain = %+v, %v", o, err)
	}
}

func TestPlainFileRoundTrip(t *testing.T) {
	t.Parallel()
	s, wt, dir := attached(t)
	o, err := s.AddPlainOverview(root, wt, "T", "see [a](x.go:1)")
	if err != nil {
		t.Fatal(err)
	}
	if !o.Plain || o.Memory || len(o.Anchors) != 0 {
		t.Fatalf("file plain = %+v", o)
	}
	data, err := os.ReadFile(dir + "/" + o.ID + ".md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "plain = true") {
		t.Fatalf("no plain key: %s", data)
	}
	if o, err = s.SetOverviewIn(root, o.ID, "", "again [a](x.go:1)"); err != nil || !o.Plain || len(o.Anchors) != 0 {
		t.Fatalf("set kept plain: %+v, %v", o, err)
	}
	if err := s.SetOverviewPlain(root, o.ID, false); err != nil {
		t.Fatal(err)
	}
	if o, _ = s.OverviewIn(root, o.ID); o.Plain || len(o.Anchors) != 1 {
		t.Fatalf("unplain = %+v", o)
	}
	data, _ = os.ReadFile(dir + "/" + o.ID + ".md")
	if strings.Contains(string(data), "plain") {
		t.Fatalf("plain=false written: %s", data)
	}
}

func TestParseOverviewFileIgnoresUnknownKey(t *testing.T) {
	t.Parallel()
	o, err := ParseOverviewFile([]byte("+++\ntitle = \"T\"\nfuture = 1\n+++\nbody\n"))
	if err != nil || o.Plain {
		t.Fatalf("o = %+v, %v", o, err)
	}
}
