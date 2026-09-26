package web

import "testing"

const ofPureStart = "// --- switcher model (pure; guarded against Go) ---"
const ofPureEnd = "// --- end switcher model ---"

func TestSwitcherModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const rows = switcherRows([
  {id: "f2", path: "a/b.go", line: 12, source: "worktree", state: "shown"},
  {id: "f1", path: "c.txt", line: 0, source: "commit", rev: "0123456789", state: "shown"},
], "f2");
console.log(JSON.stringify(rows) + "|" + [clampSel(5, 2), clampSel(-1, 2), clampSel(3, 0)].join(","));
`)
	want := `[{"id":"f2","mark":"●","path":"a/b.go","line":":12","source":"worktree","rev":""},{"id":"f1","mark":"○","path":"c.txt","line":"","source":"commit","rev":"0123456789"}]|1,0,0`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
