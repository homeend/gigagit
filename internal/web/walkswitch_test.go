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
	for _, fn := range []string{"wtBase", "pathKey", "listedPath", "switchTarget", "returnPlan", "wtCycle"} {
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
  wtCycle(order, "/r", false, false, ["/r/wt-a"]),           // not reachable from here: skipped and named
  wtCycle(order, "/r", false, false, ["/r/wt-a", "/r/wt-b"]), // none other reachable
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
		`{"path":"/r","msg":"r — 1 of 3 worktrees"},` +
		`{"path":"/r/wt-b","msg":"wt-b — 3 of 3 worktrees (skipped wt-a: not reachable from here)"},` +
		`{"msg":"no other worktree is reachable from here"}]`
	if got := nodeRun(t, src); got != want {
		t.Errorf("walk switch =\n%s\nwant\n%s", got, want)
	}
}

func TestWalkSwitchWired(t *testing.T) {
	t.Parallel()
	c := readStaticSrc(t, "console.js")
	for _, want := range []string{
		"switchTarget(info.worktree, state.worktree, wtOrder())",
		"await doReroot(dir, null, line);",
		`"an operation is running — switch once it has finished"`,
		"function leaveConsole(",
		`case "Escape": leaveConsole(); break;`,
		"toast(con.info.label + \" was removed\");\n    leaveConsole();",
		"returnPlan(",
		`e.code === "KeyW"`,
		"wtCycle(",
		"close: leaveConsole,",
		"<b>alt+w</b> closes the console",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("console.js lacks %s", want)
		}
	}
	// The in-place switch: nothing is carried through a reload any more.
	for _, f := range []string{"console.js", "ops.js", "app.js"} {
		src := readStaticSrc(t, f)
		for _, gone := range []string{"openPendingConsole", "gg-open-console", "carried"} {
			if strings.Contains(src, gone) {
				t.Errorf("%s still has %s", f, gone)
			}
		}
	}
	if !strings.Contains(readStaticSrc(t, "keys.js"), `WINDOW_LAYERS.includes(top.id) && e.altKey && e.code === "KeyW"`) {
		t.Error("keys.js: alt+w over the viewer / history / blame reaches walkKey")
	}
}

// The in-place worktree switch (no reload, no veil) and the lock bar's
// second look.
func TestSoftSwitchJS(t *testing.T) {
	t.Parallel()
	src := jsFunc(t, "locks.js", "confirmedLocks") + "\n" + jsFunc(t, "sidebar.js", "flipHead") + "\n" + jsFunc(t, "ops.js", "swapDraft") + `
const L = (path, age) => ({ path, name: path.split("/").pop(), age_seconds: age });
const drafts = new Map();
console.log(JSON.stringify([
  confirmedLocks([L("/r/.git/index.lock", 0)], [L("/r/.git/index.lock", 3.1)], 3),     // the same lock, older: stranded
  confirmedLocks([L("/r/.git/index.lock", 0)], [L("/r/.git/index.lock", 0.2)], 3),     // a new one in its place: a live git
  confirmedLocks([L("/r/.git/index.lock", 0)], [], 3),                                 // gone
  confirmedLocks([], [L("/r/.git/HEAD.lock", 50)], 3),                                 // not seen the first time
  confirmedLocks([L("/a.lock", 100), L("/b.lock", 1)], [L("/a.lock", 103), L("/b.lock", 1.5)], 3),
  flipHead([{ name: "main", is_head: true }, { name: "wtb", is_head: false }, { name: "x" }], "wtb").map((b) => b.name + ":" + !!b.is_head),
  flipHead([{ name: "main", is_head: true }], "").map((b) => b.name + ":" + !!b.is_head), // detached: no ✓
  swapDraft(drafts, "/r", "/r/wt-b", "half a message"),
  swapDraft(drafts, "/r/wt-b", "/r", "wt-b's"),
  swapDraft(drafts, "/r", "/r/wt-b", ""),
]));
`
	want := `[[{"path":"/r/.git/index.lock","name":"index.lock","age_seconds":3.1}],[],[],[],` +
		`[{"path":"/a.lock","name":"a.lock","age_seconds":103}],` +
		`["main:false","wtb:true","x:false"],["main:false"],"","half a message","wt-b's"]`
	if got := nodeRun(t, src); got != want {
		t.Errorf("soft switch =\n%s\nwant\n%s", got, want)
	}
}

func TestSoftSwitchWired(t *testing.T) {
	t.Parallel()
	ops := readStaticSrc(t, "ops.js")
	for _, want := range []string{
		"function softSwitch(path, line = \"\") {",
		"const listed = switchTarget(path, state.worktree, listedPaths());",
		"loadCommits(false, true)",
		"reconcileStatusView();",
		"fetchNotifications()",
		"fetchLocks()",
		"swapDraft(drafts, from, path,",
		"return false; // the switch did not happen",
	} {
		if !strings.Contains(ops, want) {
			t.Errorf("ops.js lacks %s", want)
		}
	}
	live := readStaticSrc(t, "live.js")
	for _, want := range []string{"if (inPlace(worktree)) {", "softSwitch(worktree);", "const reload = !inPlace(msg.worktree);"} {
		if !strings.Contains(live, want) {
			t.Errorf("live.js lacks %s", want)
		}
	}
	if !strings.Contains(readStaticSrc(t, "sidebar.js"), "async function fetchWorktreeSide(") {
		t.Error("sidebar.js: the worktree-side lists re-read on their own")
	}
	locks := readStaticSrc(t, "locks.js")
	if strings.Contains(locks, "\nfetchLocks();\n") {
		t.Error("locks.js: no probe at module load (it races boot's status scan)")
	}
	if !strings.Contains(locks, "confirmedLocks(first, second,") {
		t.Error("locks.js: the bar shows only a lock the second look still finds")
	}
}

// Review fixes of the in-place switch: the navigate a switch was asked for
// lands, the old worktree's working-tree view and marks go, and a slower
// switch's answers never paint over a later one.
func TestSoftSwitchReviewFixes(t *testing.T) {
	t.Parallel()
	ops := readStaticSrc(t, "ops.js")
	for _, want := range []string{
		"applyStartAt();",
		`if (state.filesMode === "status") exitStatusToList();`,
		"state.marked.clear();",
		`state.cmarks.delete("@worktree");`,
		`getJSON("/api/status").then((b) => { if (current()) applyStatus(b); })`,
		"fetchWorktreeSide(current)",
		"loadRepo(current)",
		"async function loadRepo(current = () => true) {",
	} {
		if !strings.Contains(ops, want) {
			t.Errorf("ops.js lacks %s", want)
		}
	}
	side := readStaticSrc(t, "sidebar.js")
	for _, want := range []string{"async function fetchWorktreeSide(current = () => true) {", "  if (!current()) return;"} {
		if !strings.Contains(side, want) {
			t.Errorf("sidebar.js lacks %s", want)
		}
	}
}
