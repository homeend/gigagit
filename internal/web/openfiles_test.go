package web

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
)

func wtKey(p string) ofKey { return ofKey{Src: "worktree", Path: p} }

func ids(r *openFiles, wt string) string {
	s := ""
	for _, f := range r.list(wt) {
		s += f.ID + ":" + f.State + " "
	}
	return s
}

func TestOpenFilesOrderReuseAndState(t *testing.T) {
	t.Parallel()
	r := newOpenFiles(agentdocs.New().NextFileSeq)
	a, _ := r.open("/wt", wtKey("a.txt"), "t1", 0)
	b, _ := r.open("/wt", wtKey("b.txt"), "t1", 7) // t1 now shows b; a is background
	if a.ID != "f1" || b.ID != "f2" || b.Line != 7 {
		t.Fatalf("ids/line: %+v %+v", a, b)
	}
	if got := ids(r, "/wt"); got != "f2:shown f1:background " {
		t.Fatalf("order/state: %q", got)
	}
	again, _ := r.open("/wt", wtKey("a.txt"), "t1", 0) // reuse, move first
	if again.ID != "f1" || ids(r, "/wt") != "f1:shown f2:background " {
		t.Fatalf("reuse: %+v %q", again, ids(r, "/wt"))
	}
	if len(r.list("/other")) != 0 {
		t.Fatal("lists are per worktree")
	}
	c, _ := r.open("/wt", ofKey{Src: "commit", Rev: "abc", Path: "a.txt"}, "", 0)
	if c.ID != "f3" || c.State != "background" || c.Source != "commit" || c.Rev != "abc" {
		t.Fatalf("a version is its own entry; tab \"\" opens in the background: %+v", c)
	}
}

func TestOpenFilesReuseAcrossTabs(t *testing.T) {
	t.Parallel()
	r := newOpenFiles(agentdocs.New().NextFileSeq)
	x, _ := r.open("/wt", wtKey("a.txt"), "t1", 0)
	y, _ := r.open("/wt", wtKey("a.txt"), "t2", 0)
	if x.ID != y.ID || len(r.list("/wt")) != 1 {
		t.Fatalf("one entry per (src,rev,path): %+v %+v", x, y)
	}
	// t1 lets go (esc): t2 still shows it, so it stays.
	if !r.close("/wt", x.ID, "t1", false) || ids(r, "/wt") != "f1:shown " {
		t.Fatalf("close by one tab while another shows: %q", ids(r, "/wt"))
	}
	// t2 lets go too: removed.
	r.close("/wt", x.ID, "t2", false)
	if len(r.list("/wt")) != 0 {
		t.Fatalf("last tab's close removes: %q", ids(r, "/wt"))
	}
}

func TestOpenFilesCloseEverywhereAndUnknown(t *testing.T) {
	t.Parallel()
	r := newOpenFiles(agentdocs.New().NextFileSeq)
	f, _ := r.open("/wt", wtKey("a.txt"), "t1", 0)
	if !r.close("/wt", f.ID, "t2", true) || len(r.list("/wt")) != 0 {
		t.Fatal("everywhere removes even while another tab shows it")
	}
	if r.close("/wt", "f99", "t1", false) || r.background("/wt", "f99", "t1", 1) || r.cursor("/wt", "f99", 1) || r.setShown("/wt", "t1", "f99") {
		t.Fatal("an unknown id is refused")
	}
	if _, ok := r.focus("/wt", "f99", "t1"); ok {
		t.Fatal("focus on an unknown id")
	}
}

func TestOpenFilesBackgroundFocusCursorShown(t *testing.T) {
	t.Parallel()
	r := newOpenFiles(agentdocs.New().NextFileSeq)
	a, _ := r.open("/wt", wtKey("a.txt"), "t1", 0)
	r.open("/wt", wtKey("b.txt"), "", 0)
	if !r.background("/wt", a.ID, "t1", 12) || ids(r, "/wt") != "f2:background f1:background " {
		t.Fatalf("background keeps the order: %q", ids(r, "/wt"))
	}
	if r.list("/wt")[1].Line != 12 {
		t.Fatal("background records the line")
	}
	r.cursor("/wt", a.ID, 30)
	f, ok := r.focus("/wt", a.ID, "t1")
	if !ok || f.Line != 30 || ids(r, "/wt") != "f1:shown f2:background " {
		t.Fatalf("focus: %+v %q", f, ids(r, "/wt"))
	}
	if !r.setShown("/wt", "t1", "") || ids(r, "/wt") != "f1:background f2:background " {
		t.Fatalf("shown \"\" = the tab shows nothing: %q", ids(r, "/wt"))
	}
	r.setShown("/wt", "t1", "f2")
	if ids(r, "/wt") != "f1:background f2:shown " {
		t.Fatalf("shown does not reorder: %q", ids(r, "/wt"))
	}
}

func TestOpenFilesStreamDropsShown(t *testing.T) {
	t.Parallel()
	r := newOpenFiles(agentdocs.New().NextFileSeq)
	r.streamOpened("t1")
	r.streamOpened("t1") // a reconnect racing the old stream's end
	r.open("/wt", wtKey("a.txt"), "t1", 0)
	if r.streamClosed("t1") || ids(r, "/wt") != "f1:shown " {
		t.Fatalf("one of two streams ending keeps the tab: %q", ids(r, "/wt"))
	}
	if !r.streamClosed("t1") || ids(r, "/wt") != "f1:background " {
		t.Fatalf("the last stream ending drops what the tab showed: %q", ids(r, "/wt"))
	}
	if r.streamClosed("t1") {
		t.Fatal("an extra close is a no-op")
	}
}

func TestOpenFilesCapEvictsLeastRecentNotShown(t *testing.T) {
	t.Parallel()
	r := newOpenFiles(agentdocs.New().NextFileSeq)
	first, _ := r.open("/wt", wtKey("p0.txt"), "t9", 0) // t9 keeps showing p0
	for i := 1; i < maxOpenFiles; i++ {
		r.open("/wt", wtKey(fmt.Sprintf("p%d.txt", i)), "", 0)
	}
	f, ev := r.open("/wt", wtKey("new.txt"), "", 0)
	if ev != "p1.txt" || len(r.list("/wt")) != maxOpenFiles || f.Path != "new.txt" {
		t.Fatalf("evicted %q (want p1.txt: p0 is shown), len %d", ev, len(r.list("/wt")))
	}
	if _, ok := r.focus("/wt", first.ID, "t9"); !ok {
		t.Fatal("the shown file survived")
	}
	// Every entry shown: nothing can go, the list grows past the cap.
	r2 := newOpenFiles(agentdocs.New().NextFileSeq)
	for i := 0; i <= maxOpenFiles; i++ {
		_, ev := r2.open("/wt", wtKey(fmt.Sprintf("q%d.txt", i)), fmt.Sprintf("tab%d", i), 0)
		if ev != "" {
			t.Fatalf("evicted a shown file %q", ev)
		}
	}
	if len(r2.list("/wt")) != maxOpenFiles+1 {
		t.Fatalf("len %d", len(r2.list("/wt")))
	}
}

func TestOpenFilesDueAndApplyStat(t *testing.T) {
	t.Parallel()
	r := newOpenFiles(agentdocs.New().NextFileSeq)
	now := time.Unix(1000, 0)
	shown, _ := r.open("/wt", wtKey("s.txt"), "t1", 0)
	bg, _ := r.open("/wt", wtKey("b.txt"), "", 0)
	r.open("/wt", ofKey{Src: "commit", Rev: "abc", Path: "c.txt"}, "", 0) // never polled
	if d := r.due("/wt", now, false, 5*time.Second); len(d) != 2 {
		t.Fatalf("first round: every working-tree entry: %+v", d)
	}
	if d := r.due("/wt", now.Add(time.Second), false, 5*time.Second); len(d) != 1 || d[0].ID != shown.ID {
		t.Fatalf("1 s later only the shown one: %+v", d)
	}
	if d := r.due("/wt", now.Add(2*time.Second), true, 5*time.Second); len(d) != 2 {
		t.Fatalf("a wake polls all: %+v", d)
	}
	if d := r.due("/wt", now.Add(8*time.Second), false, 5*time.Second); len(d) != 2 {
		t.Fatalf("5 s after the last check the background one is due: %+v", d)
	}
	st := diskStat{size: 1, mod: now, known: true}
	if r.applyStat("/wt", bg.ID, st) {
		t.Fatal("the first stat only records")
	}
	if r.applyStat("/wt", bg.ID, st) || !r.applyStat("/wt", bg.ID, diskStat{missing: true, known: true}) {
		t.Fatal("same = no change; missing = change")
	}
	if !r.applyStat("/wt", bg.ID, st) {
		t.Fatal("back from missing = change")
	}
	if r.applyStat("/wt", bg.ID, diskStat{}) {
		t.Fatal("an unknown stat never reads as a change")
	}
	r.close("/wt", bg.ID, "", true)
	if r.applyStat("/wt", bg.ID, diskStat{size: 9, known: true}) || len(r.list("/wt")) != 2 {
		t.Fatal("a stat for a closed entry is dropped, never resurrects it")
	}
}

func TestStatDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	if st := statDisk(p); !st.known || !st.missing {
		t.Fatalf("absent: %+v", st)
	}
	os.WriteFile(p, []byte("abc"), 0o644)
	st := statDisk(p)
	if !st.known || st.missing || st.size != 3 {
		t.Fatalf("present: %+v", st)
	}
	if !st.same(statDisk(p)) || st.same(diskStat{missing: true, known: true}) {
		t.Fatal("same")
	}
}

func TestOpenFilesLookup(t *testing.T) {
	t.Parallel()
	r := newOpenFiles(agentdocs.New().NextFileSeq)
	k := ofKey{Src: "worktree", Path: "a.txt"}
	if _, ok := r.lookup("/w", k); ok {
		t.Fatal("lookup found a file never opened")
	}
	r.open("/w", k, "t1", 0)
	f, ok := r.lookup("/w", k)
	if !ok || f.ID != "f1" || f.State != "shown" {
		t.Fatalf("lookup = %+v, %v", f, ok)
	}
	if _, ok := r.lookup("/w", ofKey{Src: "commit", Rev: "abc", Path: "a.txt"}); ok {
		t.Fatal("another version of the path matched")
	}
}

func TestOpenFilesResolveAndLiveTabs(t *testing.T) {
	t.Parallel()
	r := newOpenFiles(agentdocs.New().NextFileSeq)
	r.open("/w", ofKey{Src: "worktree", Path: "a.txt"}, "", 0)
	r.open("/w", ofKey{Src: "worktree", Path: "b.txt"}, "", 0)
	for _, tc := range []struct{ id, path, want string }{
		{"f1", "", "f1"}, {"", "b.txt", "f2"}, {"b.txt", "", "f2"}, {"f9", "", ""},
	} {
		f, ok := r.resolve("/w", tc.id, tc.path)
		if ok != (tc.want != "") || f.ID != tc.want {
			t.Errorf("resolve(%q,%q) = %+v,%v; want %q", tc.id, tc.path, f, ok, tc.want)
		}
	}
	if r.liveTabs() != 0 {
		t.Fatal("no stream is open")
	}
	r.streamOpened("t1")
	if r.liveTabs() != 1 {
		t.Fatal("liveTabs misses t1")
	}
}

// Twenty overviews (pinned, never evicted) leave the user's own files room:
// the cap is 100 (user ruling 2026-10-01), not a squeeze to one free slot.
func TestOpenFilesCapLeavesRoomBeside20Overviews(t *testing.T) {
	t.Parallel()
	r := newOpenFiles(agentdocs.New().NextFileSeq)
	for i := 0; i < 20; i++ {
		r.ensureOpenID("/wt", ofKey{Src: "overview", Path: fmt.Sprintf("overview-%d.md", i)}, fmt.Sprintf("f%d", 900+i), "T")
	}
	for i := 0; i < 30; i++ {
		if _, ev := r.open("/wt", wtKey(fmt.Sprintf("u%d.txt", i)), "", 0); ev != "" {
			t.Fatalf("file %d pushed %s out with 20 overviews open", i, ev)
		}
	}
}
