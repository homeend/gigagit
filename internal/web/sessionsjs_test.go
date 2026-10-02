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
r.push(JSON.stringify(dialogStep(d, "Enter")));                    // approved → start
r.push(JSON.stringify(dialogStep(d, "2")));                        // unapproved → approve phase
r.push(JSON.stringify(dialogStep(d, "9")));                        // no such row → nothing
r.push(JSON.stringify(dialogStep({ ...d, sel: 2 }, "Enter")));     // not found still starts: the server reports the failure
r.push(JSON.stringify(dialogStep({ phase: "approve", cmds, sel: 1 }, "Enter")));   // approve → start with approve
r.push(JSON.stringify(dialogStep({ phase: "approve", cmds, sel: 1 }, "Escape")));  // back to the list
r.push(JSON.stringify(dialogStep({ phase: "approve", cmds: [cmds[1]], sel: 0 }, "Escape"))); // one command: esc closes
r.push(JSON.stringify(dialogStep(d, "Escape")));
r.push(JSON.stringify(dialogStep({ phase: "starting", cmds, sel: 0 }, "Enter")));  // a held enter starts nothing twice
r.push(JSON.stringify(dialogStep({ phase: "starting", cmds, sel: 0 }, "Escape"))); // …and esc cannot orphan a start in flight
r.push(JSON.stringify(dialogStep({ phase: "detecting", cmds: [], sel: 0 }, "Escape")));
r.push(JSON.stringify(dialogStep(d, "Enter", true)), JSON.stringify(dialogStep({ phase: "approve", cmds, sel: 1 }, "Enter", true))); // a HELD enter neither picks nor approves
r.push(JSON.stringify(dialogStep(d, "ArrowDown", true)));          // …while a held arrow still moves
r.push(JSON.stringify(startRows("/a/b/wt").map((x) => x.label)));
r.push(JSON.stringify(sessionMenuRows({ id: "s1", state: "running" }).map((x) => x.label)), JSON.stringify(sessionMenuRows({ id: "s2", state: "exited" }).map((x) => x.label)));
console.log(r.join("|"));
`)
	want := `approved|approve on start|not found|` +
		`{"sel":1}|{"sel":2}|{"sel":0}|` +
		`{"start":0,"approve":false}|{"sel":1,"phase":"approve"}|{}|{"start":2,"approve":false}|` +
		`{"start":1,"approve":true}|{"phase":"choose"}|{"close":true}|{"close":true}|{}|{}|{"close":true}|{}|{}|{"sel":1}|` +
		`["Start agent in wt","Open terminal in wt"]|["Kill session","Kill and remove session"]|["Remove session"]`
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
	{"menus.js", `"session"`, "session is a registered menu key"},
	{"sidebar.js", `extraRows("session"`, "the sub-row menu collects the session rows"},
	{"style.css", "#sessstart.hidden", "hidden by id, never a global .hidden"},
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
