package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The diff header's path menu ("copy file name" / "copy parent dir") splits
// the open file's path in the BROWSER, so the split lives in files.js. What
// it must not do is hand out a name with a directory still glued to it, or an
// empty "parent dir" row for a file at the repo root — both are quiet
// failures: the copy succeeds and the pasted text is wrong.
//
// Only the PURE section of files.js is evaluated: the rest of the module
// touches the DOM at import time and cannot run under node.
const pathPartsStart = "// --- path parts (pure; guarded against Go) ---"
const pathPartsEnd = "// --- end path parts ---"

func TestPathPartsJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "files.js"))
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), pathPartsStart)
	j := strings.Index(string(src), pathPartsEnd)
	if i < 0 || j < i {
		t.Fatalf("files.js: the guarded section markers are gone (%q / %q)", pathPartsStart, pathPartsEnd)
	}
	pure := string(src)[i:j]

	cases := []struct {
		path string
		name string
		dir  string
	}{
		{"internal/web/static/files.js", "files.js", "internal/web/static"},
		{"README.md", "README.md", ""},                                // repo root: no parent row
		{"a/b", "b", "a"},                                             // the shallowest real nesting
		{`C:\repo\internal\files.go`, "files.go", `C:\repo\internal`}, // a Windows worktree address
		{"dir/sub/", "", "dir/sub"},                                   // a trailing separator is not a name
		{"", "", ""},                                                  // nothing open
		{"ünïcode/файл.txt", "файл.txt", "ünïcode"},                   // the split is by separator, not by byte
	}

	in, err := json.Marshal(cases2json(cases))
	if err != nil {
		t.Fatal(err)
	}
	script := pure + `
const cases = JSON.parse(process.argv[1]);
console.log(JSON.stringify(cases.map((c) => pathParts(c))));
`
	out, err := exec.Command(node, "-e", script, string(in)).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got []struct {
		Name string `json:"name"`
		Dir  string `json:"dir"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("node output %q: %v", out, err)
	}
	if len(got) != len(cases) {
		t.Fatalf("got %d results, want %d", len(got), len(cases))
	}
	for n, c := range cases {
		if got[n].Name != c.name || got[n].Dir != c.dir {
			t.Errorf("pathParts(%q) = {name:%q dir:%q}, want {name:%q dir:%q}",
				c.path, got[n].Name, got[n].Dir, c.name, c.dir)
		}
	}
}

func cases2json(cases []struct {
	path string
	name string
	dir  string
}) []string {
	out := make([]string, len(cases))
	for i, c := range cases {
		out[i] = c.path
	}
	return out
}
