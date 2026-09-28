package web

import (
	"strings"
	"testing"
)

func TestSwitcherTabsModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", "// --- switcher model (pure; guarded against Go) ---", "// --- end switcher model ---", `
const r = [];
const sess = [
  { id: "s1", label: "claude", repo: "gg", worktree: "/x/gg", state: "running", started: new Date(Date.now() - 65000).toISOString() },
  { id: "s2", label: "codex", repo: "gg", worktree: "/x/wt2", state: "exited", exit_code: 0, started: new Date().toISOString() },
  { id: "s3", label: "Terminal", repo: "lazygit", worktree: "/y/lg", state: "running", started: new Date().toISOString(), task: "" },
  { id: "s4", label: "Claude · commit message", repo: "gg", worktree: "/x/gg", state: "running", started: new Date().toISOString(), task: "t1" },
];
const rows = sessionRows(sess, "s1", Date.now());
r.push(rows.map((x) => x.h ? "H:" + x.h : x.h2 ? "h2:" + x.h2 : x.id + ":" + x.glyph + ":" + x.mark + ":" + x.meta).join(","));
r.push(JSON.stringify(taskRows([{ id: "t1", key: "commit message — main @ abc", agent: "claude", state: "running", session: "s4", started: new Date().toISOString() }], Date.now())[0]));
r.push(freshestTab([{ started: "2026-01-01T00:00:00Z" }], [], [1]), freshestTab([], [], [1]), freshestTab([], [], []));
console.log(r.join("|"));
`)
	want := "H:gg,h2:gg — /x/gg,s1:●:●:1m,s4:●:○:0s,h2:wt2 — /x/wt2,s2:○:○:exited (0),H:lazygit,h2:lg — /y/lg,s3:●:○:0s|" +
		`{"id":"t1","key":"commit message — main @ abc","agent":"claude","state":"running","session":"s4","age":"0s"}|` +
		"agents|files|agents"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

var switcherWiring = []struct{ file, want, why string }{
	{"openfiles.js", `data-tab="agents"`, "the Agents tab exists"},
	{"openfiles.js", `data-tab="tasks"`, "the AI tasks tab exists"},
	{"openfiles.js", `data-tab="files"`, "the Open files tab exists"},
	{"openfiles.js", "/api/sessions", "the Agents tab lists the server's sessions"},
	{"openfiles.js", "/api/tasks", "the AI tasks tab lists the server's tasks"},
	{"openfiles.js", "openConsole(", "enter on a session opens its console"},
	{"openfiles.js", `"gg:switcher"`, "the console's ctrl+\\ reaches the switcher"},
	{"index.html", `ctrl+\ sessions`, "the foot advertises the switcher's new role"},
	{"live.js", "switcherSessions(", "a sessions event refreshes an open switcher"},
	{"live.js", "consoleSessions(", "a sessions event retitles the open console"},
}

func TestSwitcherTabsWired(t *testing.T) {
	t.Parallel()
	for _, c := range switcherWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}
