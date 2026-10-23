package web

import (
	"regexp"
	"strings"
	"testing"
)

func TestSwitcherTabsModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", "// --- switcher model (pure; guarded against Go) ---", "// --- end switcher model ---", activitySection(t)+`
const r = [];
const sess = [
  { id: "s1", label: "claude", repo: "gg", worktree: "/x/gg", state: "running", started: new Date(Date.now() - 65000).toISOString() },
  { id: "s2", label: "codex", repo: "gg", worktree: "/x/wt2", state: "exited", exit_code: 0, started: new Date().toISOString() },
  { id: "s3", label: "Terminal", repo: "lazygit", worktree: "/y/lg", state: "running", started: new Date().toISOString(), task: "" },
  { id: "s4", label: "Claude · commit message", repo: "gg", worktree: "/x/gg", state: "running", started: new Date().toISOString(), task: "t1" },
];
const rows = sessionRows(sess, "s1", Date.now());
r.push(rows.map((x) => x.h ? "H:" + x.h : x.h2 ? "h2:" + x.h2 : x.id + ":" + x.glyph + ":" + x.mark + ":" + x.meta).join(","));
r.push(JSON.stringify(taskRows([{ id: "t1", key: "commit message — main @ abc", agent: "claude", state: "running", session: "s4", live: true, started: new Date().toISOString() }], Date.now())[0]));
r.push(freshestTab([{ started: "2026-01-01T00:00:00Z" }], [], [1]), freshestTab([], [], [1]), freshestTab([], [], []));
console.log(r.join("|"));
`)
	want := "H:gg,h2:gg — /x/gg,s1:●:●:1m,s4:●:○:0s,h2:wt2 — /x/wt2,s2:○:○:exited (0),H:lazygit,h2:lg — /y/lg,s3:●:○:0s|" +
		`{"id":"t1","key":"commit message — main @ abc","agent":"claude","state":"running","session":"s4","age":"0s","live":true,"kind":"","has_result":false,"note_id":"","save_err":"","worktree":""}|` +
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

var switcherLifecycleWiring = []struct{ file, want, why string }{
	{"openfiles.js", "killSession(", "k / X on the Agents tab kill"},
	{"style.css", "#openfiles-list .grp { color: var(--dim); font-size: 11px; padding: 3px 12px; }", "a repository group header shows the name as written, never uppercased"},
	{"openfiles.js", "removeSession(", "x on the Agents tab removes an exited session"},
	{"openfiles.js", `k kill`, "the Agents foot offers kill"},
	{"openfiles.js", `x remove`, "…and remove"},
}

func TestSwitcherLifecycleWired(t *testing.T) {
	t.Parallel()
	for _, c := range switcherLifecycleWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}

func TestSwitcherAgentKeyJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", "// --- switcher model (pure; guarded against Go) ---", "// --- end switcher model ---", activitySection(t)+`
console.log([agentKey("k", "running"), agentKey("X", "running"), agentKey("x", "exited"), agentKey("x", "running"), agentKey("k", "exited"), agentKey("X", "exited"), agentKey("j", "running")].join("|"));
`)
	if want := "kill|killrm|remove|refuse-remove|none|remove|none"; out != want {
		t.Fatalf("got %s want %s", out, want)
	}
}

// The AI tasks tab is the TUI's Headless tab: an ended row's age is its local
// date and time plus how long ago in its largest unit (a settled row does not
// tick); the filter matches key + agent; the hint offers only what applies;
// k arms then cancels a live task, x removes only an ended one, s retries
// only a failed save.
func TestSwitcherTaskTabJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", "// --- switcher model (pure; guarded against Go) ---", "// --- end switcher model ---", activitySection(t)+`
const now = Date.now();
const list = [
  { id: "a", key: "review — main", agent: "claude", state: "running", live: true, started: new Date(now - 125000).toISOString() },
  { id: "b", key: "commit message — main @ abc", agent: "codex", state: "done", live: false, has_result: true, ended: new Date(now - 3 * 3600000 - 900000).toISOString() },
  { id: "c", key: "review — feat", agent: "claude", state: "failed", live: false, ended: new Date(now - 50000).toISOString() },
  { id: "d", key: "review — x", agent: "claude", state: "done", live: true, save_err: "disk full", has_result: false, ended: new Date(now).toISOString() },
  { id: "e", key: "commit message — y", agent: "claude", state: "running", live: true, session: "s9", started: new Date(now).toISOString() },
];
const rows = taskRows(list, now, "");
const r = [];
r.push(rows.map((x) => x.age).join(";"));
r.push(taskRows(list, now, "codex").map((x) => x.id).join(","));
r.push(rows.map((x) => taskHint(x).join("+")).join(";"));
r.push([taskKey("k", rows[0], ""), taskKey("k", rows[0], "a"), taskKey("k", rows[1], ""), taskKey("x", rows[0], ""), taskKey("x", rows[1], ""), taskKey("s", rows[3], ""), taskKey("s", rows[1], "")].join(","));
console.log(r.join("|"));
`)
	parts := strings.Split(out, "|")
	if len(parts) != 4 {
		t.Fatalf("harness output %q", out)
	}
	ages := strings.Split(parts[0], ";")
	if ages[0] != "2m05s" {
		t.Errorf("a live row's age = %q, want the elapsed time 2m05s (the TUI's formatElapsed)", ages[0])
	}
	if !regexp.MustCompile(`^\d\d-\d\d \d\d:\d\d · 3h ago$`).MatchString(ages[1]) {
		t.Errorf("an ended row's age = %q, want MM-DD HH:MM · 3h ago", ages[1])
	}
	if !strings.HasSuffix(ages[2], " · 50s ago") {
		t.Errorf("a recent ended row's age = %q, want … · 50s ago", ages[2])
	}
	if parts[1] != "b" {
		t.Errorf("filter codex = %q, want only b", parts[1])
	}
	if want := "k k cancel;enter result+x remove;enter output+x remove;s retry save+k k cancel;enter console+k k cancel"; parts[2] != want {
		t.Errorf("hints = %q\nwant    %q", parts[2], want)
	}
	if want := "arm,cancel,none,refuse,remove,retry,none"; parts[3] != want {
		t.Errorf("keys = %q, want %q", parts[3], want)
	}
}
