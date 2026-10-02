package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const activityPureStart = "// --- session activity (pure; guarded against Go) ---"
const activityPureEnd = "// --- end session activity ---"

// activitySection is core.js's activity model, prepended to the pure
// sections that call it (they import it from core.js at runtime).
func activitySection(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("static", "core.js"))
	if err != nil {
		t.Fatal(err)
	}
	i, j := strings.Index(string(src), activityPureStart), strings.Index(string(src), activityPureEnd)
	if i < 0 || j < i {
		t.Fatal("core.js: the activity section markers are gone")
	}
	return string(src)[i:j] + "\n"
}

func TestActivityModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "core.js", activityPureStart, activityPureEnd, `
const now = Date.now();
const since = new Date(now - 7 * 60000).toISOString();
const r = [];
r.push(activityLabel({ state: "running", agent_state: "working", since }, now));
r.push(activityLabel({ state: "running", agent_state: "idle", since }, now));
r.push(activityLabel({ state: "running", agent_state: "question", since }, now));
r.push(activityLabel({ state: "running", agent_state: "working", since, stalled: true }, now));
r.push(activityLabel({ state: "running", stalled: true }, now));
r.push(activityLabel({ state: "running" }, now) === "");
r.push(activityLabel({ state: "exited", agent_state: "idle", since }, now) === "");
r.push([{ agent_state: "question" }, { agent_state: "idle", stalled: true }, { agent_state: "idle" }, {}].map((s) => activityAttn(s)).join(","));
r.push(noticeText({ kind: "question", label: "claude", worktree: "/a/b/wt" }));
r.push(noticeText({ kind: "idle", label: "claude", worktree: "C:\\x\\wt2" }));
r.push(noticeText({ kind: "stalled", label: "codex", worktree: "/a/wt", quiet_s: 125 }));
console.log(r.join("|"));
`)
	want := `working 7m|idle 7m|needs input|stalled · working 7m|stalled · no output|true|true|true,true,false,false|` +
		`claude in wt needs your input|claude in wt2 finished its turn — idle|codex in wt has printed nothing for 2m — stalled?`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// The three row models show a classified session's activity; an
// unclassified one keeps today's text.
func TestActivityOnTheRowsJS(t *testing.T) {
	t.Parallel()
	pre := activitySection(t)
	sidebar := runPureJS(t, "sidebar.js", "// --- sidebar model (pure; guarded against Go) ---", "// --- end sidebar model ---", pre+`
const now = Date.now();
const since = new Date(now - 3 * 60000).toISOString();
const sess = [
  { id: "s1", label: "claude", worktree: "/x/gg", state: "running", started: new Date(now - 125000).toISOString(), agent_state: "idle", since },
  { id: "s2", label: "codex", worktree: "/x/gg", state: "running", started: new Date(now).toISOString(), agent_state: "question", since },
  { id: "s3", label: "sh", worktree: "/x/gg", state: "running", started: new Date(now - 125000).toISOString() },
];
console.log(worktreeSessionRows(sess, "/x/gg", now).map((r) => r.meta + "/" + r.attn).join("|"));
`)
	if want := "idle 3m/false|needs input/true|running 2m/false"; sidebar != want {
		t.Fatalf("sidebar got %s want %s", sidebar, want)
	}
	switcher := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, pre+`
const now = Date.now();
const since = new Date(now - 3 * 60000).toISOString();
const rows = sessionRows([
  { id: "s1", label: "claude", repo: "r", worktree: "/x/gg", state: "running", started: new Date(now - 125000).toISOString(), agent_state: "working", since, stalled: true },
  { id: "s3", label: "sh", repo: "r", worktree: "/x/gg", state: "running", started: new Date(now - 125000).toISOString() },
], "", now).filter((r) => r.id);
console.log(rows.map((r) => r.meta + "/" + r.attn).join("|"));
`)
	if want := "2m · stalled · working 3m/true|2m/false"; switcher != want {
		t.Fatalf("switcher got %s want %s", switcher, want)
	}
	console := runPureJS(t, "console.js", consolePureStart, consolePureEnd, pre+`
const now = Date.now();
const since = new Date(now - 3 * 60000).toISOString();
const started = new Date(now - 125000).toISOString();
console.log(consoleTitle({ label: "claude", worktree: "/a/b/wt", state: "running", started, agent_state: "question", since }, now, (p) => p) + "|" +
  consoleTitle({ label: "sh", worktree: "/a/b/wt", state: "running", started }, now, (p) => p));
`)
	if want := "claude · /a/b/wt · running 2m · needs input|sh · /a/b/wt · running 2m"; console != want {
		t.Fatalf("console got %s want %s", console, want)
	}
}

var activityWiring = []struct{ file, want, why string }{
	{"live.js", "noticeText(", "a sessions event's notices become toasts"},
	{"live.js", "consoleFocusedId()", "no toast for the session the user is typing into"},
	{"console.js", "consoleFocusedId", "the console says which session has the keyboard"},
	{"sidebar.js", `r.attn ? " attn"`, "a sub-row needing attention wears the attention class"},
	{"openfiles.js", `r.attn ? " attn"`, "a switcher row needing attention wears the attention class"},
	{"console.js", `class="act`, "the console title marks the activity"},
	{"sessions.js", "resp.warning", "a start's rule warning is shown"},
	{"style.css", ".wsess.attn", "attention sub-rows are coloured"},
	{"style.css", ".ofrow.attn", "attention switcher rows are coloured"},
	{"style.css", "--act-attn", "one attention colour token"},
}

func TestActivityWired(t *testing.T) {
	t.Parallel()
	for _, c := range activityWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}
