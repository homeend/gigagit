package web

import (
	"strings"
	"testing"
)

func TestSessionsModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "sessions.js", "// --- sessions model (pure; guarded against Go) ---", "// --- end sessions model ---", `
const r = [];
r.push(commandRowState({ approved: true, found: true }), commandRowState({ approved: false, found: true }), commandRowState({ approved: true, found: false }));
const cmds = [{ name: "Claude", approved: true, found: true }, { name: "Codex", approved: false, found: true }, { name: "Junie", approved: true, found: false }];
const d = { phase: "choose", cmds, sel: 0 };
r.push(JSON.stringify(dialogStep(d, "ArrowDown")), JSON.stringify(dialogStep({ ...d, sel: 2 }, "ArrowDown")), JSON.stringify(dialogStep(d, "ArrowUp")));
r.push(JSON.stringify(dialogStep(d, "Enter")));                    // approved → the name step
r.push(JSON.stringify(dialogStep(d, "2")));                        // unapproved → approve phase
r.push(JSON.stringify(dialogStep(d, "9")));                        // no such row → nothing
r.push(JSON.stringify(dialogStep({ ...d, sel: 2 }, "Enter")));     // not found still goes on: the server reports the failure
r.push(JSON.stringify(dialogStep({ phase: "approve", cmds, sel: 1 }, "Enter")));   // approve → the name step, approved
r.push(JSON.stringify(dialogStep({ phase: "approve", cmds, sel: 1 }, "Escape")));  // back to the list
r.push(JSON.stringify(dialogStep({ phase: "approve", cmds: [cmds[1]], sel: 0 }, "Escape"))); // one command: esc closes
r.push(JSON.stringify(dialogStep(d, "Escape")));
r.push(JSON.stringify(dialogStep({ phase: "starting", cmds, sel: 0 }, "Enter")));  // a held enter starts nothing twice
r.push(JSON.stringify(dialogStep({ phase: "starting", cmds, sel: 0 }, "Escape"))); // …and esc cannot orphan a start in flight
r.push(JSON.stringify(dialogStep({ phase: "detecting", cmds: [], sel: 0 }, "Escape")));
r.push(JSON.stringify(dialogStep(d, "Enter", true)), JSON.stringify(dialogStep({ phase: "approve", cmds, sel: 1 }, "Enter", true))); // a HELD enter neither picks nor approves
r.push(JSON.stringify(dialogStep(d, "ArrowDown", true)));          // …while a held arrow still moves
const n = { phase: "name", cmds, sel: 0 };
r.push(JSON.stringify(dialogStep(n, "Enter")));                                // start, approved in the list
r.push(JSON.stringify(dialogStep({ ...n, sel: 1, approved: true }, "Enter")));  // approved in this dialog
r.push(JSON.stringify(dialogStep(n, "Escape")));                               // back to the list
r.push(JSON.stringify(dialogStep({ ...n, cmds: [cmds[0]] }, "Escape")));       // one command: close
r.push(JSON.stringify(dialogStep(n, "j")), JSON.stringify(dialogStep(n, "2")), JSON.stringify(dialogStep(n, "ArrowDown"))); // typing is the input's
r.push(JSON.stringify(dialogStep(n, "Enter", true)));                          // a held enter starts nothing
r.push(JSON.stringify(startRows("/a/b/wt").map((x) => x.label)));
r.push(JSON.stringify(sessionMenuRows({ id: "s1", state: "running" }).map((x) => x.label)), JSON.stringify(sessionMenuRows({ id: "s2", state: "exited" }).map((x) => x.label)));
r.push(sessionMenuRows({ id: "s3", state: "running", has_brief: true, has_report: true }).map((x) => x.id).join(","));
r.push(sessionMenuRows({ id: "s4", state: "exited", has_report: true }).map((x) => x.id).join(","));
console.log(r.join("|"));
`)
	want := `approved|approve on start|not found|` +
		`{"sel":1}|{"sel":2}|{"sel":0}|` +
		`{"sel":0,"phase":"name"}|{"sel":1,"phase":"approve"}|{}|{"sel":2,"phase":"name"}|` +
		`{"phase":"name","approved":true}|{"phase":"choose"}|{"close":true}|{"close":true}|{}|{}|{"close":true}|{}|{}|{"sel":1}|` +
		`{"start":0,"approve":false}|{"start":1,"approve":true}|{"phase":"choose"}|{"close":true}|{}|{}|{}|{}|` +
		`["Start agent in wt","Open terminal in wt"]|["Kill session","Kill and remove session"]|["Remove session"]|` +
		`brief,report,kill,killrm|report,remove`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

var sessionsWiring = []struct{ file, want, why string }{
	{"app.js", "./sessions.js", "the module must be imported at boot"},
	{"sessions.js", "/api/session-commands?worktree=", "the dialog lists the worktree's commands"},
	{"sessions.js", "/api/session-start", "starting goes to its endpoint"},
	{"sessions.js", "needs_approval", "a 403 falls back to the approval step"},
	{"sessions.js", "Detecting installed agents…", "the first run says what it is doing"},
	{"sessions.js", `registerRows("worktree"`, "worktree rows get Start agent / Open terminal"},
	{"sessions.js", `registerRows("branch"`, "branch rows with a worktree get them too"},
	{"sessions.js", `registerRows("session"`, "sub-rows get Kill / Remove"},
	{"sessions.js", "worktreePathForBranch(", "the branch gate is the sidebar's own lookup"},
	{"sessions.js", "dialogStep(dlg, e.key, e.repeat)", "key auto-repeat reaches the step: a held enter must not approve"},
	{"sessions.js", `<datalist id="sessstart-names">`, "the name input suggests the repo's remembered names"},
	{"sessions.js", "body.names", "the names come with the command list"},
	{"sessions.js", "name: d.name", "the start (and its re-post after approval) carries the typed name"},
	{"menus.js", `"session"`, "session is a registered menu key"},
	{"sidebar.js", `extraRows("session"`, "the sub-row menu collects the session rows"},
	{"style.css", "#sessstart.hidden", "hidden by id, never a global .hidden"},
	{"sessions.js", "/api/agent-tour", "the tour rows ask the server to file the tour"},
	{"sessions.js", "gg-open-tour", "a tour on another worktree opens after the switch's reload"},
	{"sessions.js", "doReroot(r.worktree, r.overview)", "a tour in another worktree switches the page there, carrying the tour"},
	{"app.js", "openPendingTour", "boot opens a tour a switch left pending"},
}

func TestSessionsJSIsWired(t *testing.T) {
	t.Parallel()
	for _, c := range sessionsWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
	if src := readStatic(t, "sessions.js"); strings.Contains(src, `"Start agent…`) || strings.Contains(src, "terminal…") {
		t.Error("menu labels carry no trailing … (user ruling)")
	}
}
