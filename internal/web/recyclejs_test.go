package web

import (
	"regexp"
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
	{"sidebar.js", `const NBSP = "\u00a0"`, "the picker's gaps and column padding survive HTML whitespace collapsing"},
}

func TestRecycleWired(t *testing.T) {
	t.Parallel()
	for _, c := range recycleWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
	// The picker measures with runes() and cuts with elidePath(); sidebar.js
	// is an ES module, so a helper it does not import is a ReferenceError the
	// moment the row is clicked (seen in the browser, invisible to the pure
	// section above).
	src := readStatic(t, "sidebar.js")
	for _, name := range []string{"runes", "elidePath"} {
		if !regexp.MustCompile(`import \{[^}]*\b` + name + `\b[^}]*\} from "\./core\.js"`).MatchString(src) {
			t.Errorf("sidebar.js does not import %s from core.js", name)
		}
	}
	// Both menus carry the row: the branch menu and the remote menu.
	if n := strings.Count(readStatic(t, "sidebar.js"), `"recycle a worktree…"`); n != 2 {
		t.Errorf("recycle row appears %d times in sidebar.js, want 2 (branch + remote menu)", n)
	}
}

// A live session (or a claim, a reserve, the main checkout) is the op's own
// recycle.blocked question, answered in the parking decision modal — the
// picker must not ask a second time first.
func TestRecyclePickerAsksNothingItself(t *testing.T) {
	t.Parallel()
	if strings.Contains(readStatic(t, "sidebar.js"), "An agent session is running in ") {
		t.Fatal("sidebar.js still confirms a live session itself; the op asks recycle.blocked")
	}
}
