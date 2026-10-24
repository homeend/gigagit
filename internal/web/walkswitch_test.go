package web

import (
	"strings"
	"testing"
)

// A console shows in its own worktree (the TUI's showConsoleBy) and comes
// back on its close (returnView); alt+w steps the worktrees (cycleWorktrees).
func TestWalkSwitchJS(t *testing.T) {
	t.Parallel()
	var mod strings.Builder
	for _, fn := range []string{"wtBase", "pathKey", "switchTarget", "returnPlan", "wtCycle"} {
		mod.WriteString(jsFunc(t, "walk.js", fn) + "\n")
	}
	src := mod.String() + `
const order = ["/r", "/r/wt-a", "/r/wt-b"];
console.log(JSON.stringify([
  switchTarget("/r/wt-b", "/r", order),       // another listed worktree: go there
  switchTarget("/r", "/r", order),            // already here
  switchTarget("/other", "/r", order),        // another repository's: stay
  switchTarget("", "/r", order),
  switchTarget("/r/wt-b/", "/r/wt-b", order), // a spelling, not another worktree
  switchTarget("c:\\R\\wt-b", "C:/R", ["C:/R", "C:/R/wt-b"]), // a drive path: the list's spelling
  returnPlan("/r", "/r/wt-b", order),
  returnPlan("/r/wt-b", "/r/wt-b", order),
  returnPlan("", "/r/wt-b", order),
  returnPlan("/r/gone", "/r/wt-b", order),
  wtCycle(order, "/r", true, true),           // a console covers: hidden first
  wtCycle(["/r"], "/r", true, true),
  wtCycle(order, "/r/wt-a", true, false),     // a window covers
  wtCycle(["/r"], "/r", true, false),
  wtCycle(["/r"], "/r", false, false),        // one worktree
  wtCycle(order, "/r", false, false),         // the next
  wtCycle(order, "/r/wt-b", false, false),    // around again
  wtCycle(order, "/elsewhere", false, false), // unlisted: the first
]));
`
	want := `["/r/wt-b","","","","","C:/R/wt-b",` +
		`{"path":"/r"},{},{},{"msg":"gone is gone — showing wt-b"},` +
		`{"close":true,"msg":"console hidden — alt+w again for the next worktree"},` +
		`{"close":true,"msg":"console hidden — this repository has one worktree, alt+w cycles them once there are more"},` +
		`{"close":true,"msg":"wt-a — alt+w again for the next worktree"},` +
		`{"close":true,"msg":"this repository has one worktree — alt+w cycles them once there are more"},` +
		`{"msg":"this repository has one worktree — alt+w cycles them once there are more"},` +
		`{"path":"/r/wt-a","msg":"wt-a — 2 of 3 worktrees"},` +
		`{"path":"/r","msg":"r — 1 of 3 worktrees"},` +
		`{"path":"/r","msg":"r — 1 of 3 worktrees"}]`
	if got := nodeRun(t, src); got != want {
		t.Errorf("walk switch =\n%s\nwant\n%s", got, want)
	}
}

func TestWalkSwitchWired(t *testing.T) {
	t.Parallel()
	c := readStaticSrc(t, "console.js")
	for _, want := range []string{
		"switchTarget(info.worktree, state.worktree, wtOrder())",
		"doReroot(dir, {",
		`"an operation is running — switch once it has finished"`,
		"function leaveConsole(",
		`case "Escape": leaveConsole(); break;`,
		"toast(con.info.label + \" was removed\");\n    leaveConsole();",
		"returnPlan(",
		`e.code === "KeyW"`,
		"wtCycle(",
		"function openPendingConsole(",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("console.js lacks %s", want)
		}
	}
	if !strings.Contains(readStaticSrc(t, "ops.js"), `"gg-open-console"`) {
		t.Error("ops.js: an object carry is written for boot")
	}
	if !strings.Contains(readStaticSrc(t, "app.js"), "openPendingConsole()") {
		t.Error("app.js: boot reopens the carried console")
	}
	if !strings.Contains(c, "<b>alt+w</b> closes the console") {
		t.Error("console.js: the help names alt+w")
	}
}
