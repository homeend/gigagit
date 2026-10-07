package web

import (
	"strings"
	"testing"
)

func TestWorktreeSessionRowsJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "sidebar.js", "// --- sidebar model (pure; guarded against Go) ---", "// --- end sidebar model ---", activitySection(t)+`
const now = Date.now();
const sess = [
  { id: "s1", label: "claude", worktree: "/x/gg", state: "running", started: new Date(now - 125000).toISOString() },
  { id: "s2", label: "codex", worktree: "/x/other", state: "running", started: new Date(now).toISOString() },
  { id: "s3", label: "Claude · commit message — main @ abc", worktree: "/x/gg", state: "exited", exit_code: 2, started: new Date(now).toISOString(), task: "t1" },
];
console.log(JSON.stringify(worktreeSessionRows(sess, "/x/gg", now)));
`)
	want := `[{"id":"s1","glyph":"●","label":"claude","name":"","meta":"running 2m","task":false,"attn":false},{"id":"s3","glyph":"○","label":"Claude · commit message — main @ abc","name":"","meta":"exited (2)","task":true,"attn":false}]`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// A named session's sub-row keeps its [name] when the row is too narrow:
// the label gives way first, then the name is cut itself.
func TestSessionTitleFitJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "sidebar.js", "// --- sidebar model (pure; guarded against Go) ---", "// --- end sidebar model ---", activitySection(t)+`
const cut = (s, n) => (Array.from(s).length <= n ? s : Array.from(s).slice(0, n - 1).join("") + "…");
const r = [];
r.push(sessionTitleFit("Shell [j2viewer]", "j2viewer", 30, cut)); // room: as is
r.push(sessionTitleFit("Claude (yolo) [viewer]", "viewer", 16, cut)); // the label gives way
r.push(sessionTitleFit("Shell [j2viewer]", "j2viewer", 10, cut)); // no room for the label: the name alone
r.push(sessionTitleFit("Shell [a-very-long-name]", "a-very-long-name", 8, cut)); // the name itself is cut last
r.push(sessionTitleFit("Claude · commit message", "", 10, cut)); // unnamed: as before
console.log(r.join("|"));
`)
	want := `Shell [j2viewer]|Claude… [viewer]|[j2viewer]|[a-very…|Claude · …`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

var sidebarSessionWiring = []struct{ file, want, why string }{
	{"sidebar.js", `class="wsess`, "session sub-rows render under their worktree"},
	{"sidebar.js", "sessionTitleFit(r.label, r.name, room, elideNameMiddle)", "a narrow sub-row keeps the session's name"},
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
