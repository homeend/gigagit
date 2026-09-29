package web

import (
	"strings"
	"testing"
)

// The picker lists every worktree but the one the page is on and bare ones;
// a detached one says so; a worktree with a RUNNING agent session is marked
// (an exited session is history, not a reason to ask).
func TestRecycleCandidatesJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "sidebar.js", "// --- sidebar model (pure; guarded against Go) ---", "// --- end sidebar model ---", `
const wts = [
  { path: "/r", branch: "main" },
  { path: "/r.bare", bare: true },
  { path: "/w/a", branch: "a" },
  { path: "/w/det", branch: "", detached: true },
  { path: "/w/old", branch: "old" },
];
const sess = [
  { worktree: "/w/a", state: "running" },
  { worktree: "/w/old", state: "exited" },
];
console.log(JSON.stringify(recycleCandidates(wts, "/r", sess)));
`)
	want := `[{"path":"/w/a","branch":"a","live":true},{"path":"/w/det","branch":"detached","live":false},{"path":"/w/old","branch":"old","live":false}]`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

var recycleWiring = []struct{ file, want, why string }{
	{"sidebar.js", `"recycle a worktree…"`, "the branch and remote menus offer the row"},
	{"sidebar.js", "function openRecyclePicker(", "the row opens the worktree picker"},
	{"sidebar.js", `op: "recycle-worktree"`, "a pick starts the op"},
	{"sidebar.js", "elidePath(", "picker paths are cut in the middle"},
	{"sidebar.js", "An agent session is running in ", "a live session asks first"},
}

func TestRecycleWired(t *testing.T) {
	t.Parallel()
	for _, c := range recycleWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
	// Both menus carry the row: the branch menu and the remote menu.
	if n := strings.Count(readStatic(t, "sidebar.js"), `"recycle a worktree…"`); n != 2 {
		t.Errorf("recycle row appears %d times in sidebar.js, want 2 (branch + remote menu)", n)
	}
}
