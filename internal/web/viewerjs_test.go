package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const viewerPureStart = "// --- viewer model (pure; guarded against Go) ---"
const viewerPureEnd = "// --- end viewer model ---"

// runPureJS runs driver under node with the guarded section [start, end) of
// static/<file> in scope, and returns its trimmed stdout. Skips without node.
func runPureJS(t *testing.T, file, start, end, driver string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", file))
	if err != nil {
		t.Fatal(err)
	}
	i, j := strings.Index(string(src), start), strings.Index(string(src), end)
	if i < 0 || j < i {
		t.Fatalf("%s: the guarded section markers are gone (%q / %q)", file, start, end)
	}
	script := filepath.Join(t.TempDir(), "run.mjs")
	if err := os.WriteFile(script, []byte(string(src)[i:j]+"\n"+driver), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, script).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestViewerModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", viewerPureStart, viewerPureEnd, `
const r = [];
r.push(clampLine(5, 0), clampLine(0, 3), clampLine(9, 3), clampLine(2, 3));
r.push(JSON.stringify(landLine(0, 3, "a.txt")), JSON.stringify(landLine(7, 3, "a.txt")), JSON.stringify(landLine(2, 3, "a.txt")));
r.push(sameLines([{text:"a"}],[{text:"a"}]), sameLines([{text:"a"}],[{text:"b"}]), sameLines([],[{text:""}]));
r.push(versionLabel("worktree",""), versionLabel("commit","0123456789"), versionLabel("shelf","x"));
console.log(r.join("|"));
`)
	want := `0|1|3|2|{"line":1,"notice":""}|{"line":3,"notice":"line 7 is past the end of a.txt (3 lines)"}|{"line":2,"notice":""}|true|false|false|working tree|@ 0123456|shelf`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestViewerJSIsWired(t *testing.T) {
	t.Parallel()
	read := func(n string) string {
		b, err := os.ReadFile(filepath.Join("static", n))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, c := range viewerWiring {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}

// viewerWiring grows task by task: each entry pins code that exists only once
// the step it names is done.
var viewerWiring = []struct{ file, want, why string }{
	{"app.js", "./viewer.js", "the module must be imported at boot"},
	{"viewer.js", `mountOverlay("viewer")`, "the overlay is a mounted layer"},
	{"viewer.js", `pushLayer("viewer"`, "it rides the layer stack"},
	{"viewer.js", "/api/file-content", "it reads through the content endpoint"},
	{"viewer.js", "bindSearchBar(", "it has the in-view search"},
	{"viewer.js", "renderCell(", "lines paint through the shared cell renderer"},
	{"viewer.js", `$("foot")`, "the keys go in the bottom bar"},
	{"viewer.js", "elidePath(", "the title cuts the path in the middle"},
	{"style.css", "#viewer", "the overlay is styled"},
}
