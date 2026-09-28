package web

import (
	"strings"
	"testing"
)

func TestWorktreeSessionRowsJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "sidebar.js", "// --- sidebar model (pure; guarded against Go) ---", "// --- end sidebar model ---", `
const now = Date.now();
const sess = [
  { id: "s1", label: "claude", worktree: "/x/gg", state: "running", started: new Date(now - 125000).toISOString() },
  { id: "s2", label: "codex", worktree: "/x/other", state: "running", started: new Date(now).toISOString() },
  { id: "s3", label: "Claude · commit message — main @ abc", worktree: "/x/gg", state: "exited", exit_code: 2, started: new Date(now).toISOString(), task: "t1" },
];
console.log(JSON.stringify(worktreeSessionRows(sess, "/x/gg", now)));
`)
	want := `[{"id":"s1","glyph":"●","label":"claude","meta":"running 2m","task":false},{"id":"s3","glyph":"○","label":"Claude · commit message — main @ abc","meta":"exited (2)","task":true}]`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

var sidebarSessionWiring = []struct{ file, want, why string }{
	{"sidebar.js", `class="wsess`, "session sub-rows render under their worktree"},
	{"sidebar.js", "openConsole(", "a click on a sub-row opens the console"},
	{"sidebar.js", "takeSessions", "the sessions event refreshes the rows"},
	{"live.js", "takeSessions(", "live.js forwards the sessions list to the sidebar"},
	{"style.css", ".wsess", "sub-rows are styled"},
}

func TestSidebarSessionsWired(t *testing.T) {
	t.Parallel()
	for _, c := range sidebarSessionWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}
