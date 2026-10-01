package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ofPureStart = "// --- switcher model (pure; guarded against Go) ---"
const ofPureEnd = "// --- end switcher model ---"

func TestSwitcherModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const rows = switcherRows([
  {id: "f2", path: "a/b.go", line: 12, source: "worktree", state: "shown", notes: 2},
  {id: "f1", path: "c.txt", line: 0, source: "commit", rev: "0123456789", state: "shown"},
], "f2");
console.log(JSON.stringify(rows) + "|" + [clampSel(5, 2), clampSel(-1, 2), clampSel(3, 0)].join(","));
`)
	want := `[{"id":"f2","mark":"●","path":"a/b.go","line":":12","source":"worktree","rev":"","notes":2},{"id":"f1","mark":"○","path":"c.txt","line":"","source":"commit","rev":"0123456789","notes":0}]|1,0,0`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// The viewer's footer is built in a template literal, where "\ " is an escape
// for a plain space: the chip must write "\\" or the page shows "ctrl+ open
// files" (found by the 5c browser check).
func TestViewerFootChipKeepsTheBackslash(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "viewer.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if strings.Contains(src, `>ctrl+\ open files<`) || !strings.Contains(src, `>ctrl+\\ open files<`) {
		t.Error(`viewer.js: the "ctrl+\ open files" chip loses its backslash in the template literal`)
	}
}
