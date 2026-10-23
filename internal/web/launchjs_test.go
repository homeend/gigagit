package web

import "testing"

// The launch dialog's cursor model is the TUI's: the remembered pick
// preselects (agent, then mode + command, then mode alone), ←/→ keeps the
// mode when the next agent has it, ↑/↓ skips greyed rows and stops at the
// ends.
func TestLaunchModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "launch.js", "// --- launch model (pure; guarded against Go) ---", "// --- end launch model ---", `
const choices = [
  { identity: "name:A", rows: [{ mode: "background", none: true }, { mode: "foreground", none: true }, { mode: "headless", name: "A" }] },
  { identity: "id:claude", rows: [{ mode: "background", name: "Claude" }, { mode: "foreground", name: "Claude" }, { mode: "headless", name: "Claude" }, { mode: "headless", name: "Claude yolo" }] },
];
const r = [];
const p = (x) => x.agent + ":" + x.sel;
r.push(p(preselect(choices, null)));
r.push(p(preselect(choices, { identity: "id:claude", mode: "headless", name: "Claude yolo" })));
r.push(p(preselect(choices, { identity: "id:claude", mode: "headless", name: "gone" })));
r.push(p(preselect(choices, { identity: "name:nope", mode: "foreground", name: "x" })));
r.push(p(switchAgent(choices, { agent: 1, sel: 2 }, 1)));
r.push(p(switchAgent(choices, { agent: 1, sel: 0 }, -1)));
r.push(p(switchAgent(choices, { agent: 0, sel: 2 }, 1)));
r.push([moveSel(choices[0].rows, 2, -1), moveSel(choices[1].rows, 3, 1), moveSel(choices[1].rows, 1, 1)].join(","));
console.log(r.join("|"));
`)
	// 0:2 first runnable row; 1:3 the exact variant; 1:2 that mode's first
	// command; 0:2 an unknown agent falls back; 0:2 headless kept on A;
	// 0:2 A has no background — its first runnable row; 1:2 headless kept;
	// moves: greyed rows above stay put, the end stops, a step lands.
	if want := "0:2|1:3|1:2|0:2|0:2|0:2|1:2|2,3,2"; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
