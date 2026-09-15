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

// absPath joins a repo-relative path onto the checkout root the server
// reported. The row exists to be pasted into something else on that machine,
// so a Windows root must come back fully "\\"-separated — git hands the
// browser "/" no matter the platform, and a half-converted path is no use.
func TestAbsPathJS(t *testing.T) {
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
		Root string `json:"root"`
		Path string `json:"path"`
		Want string `json:"want"`
	}{
		{"/home/u/repo", "internal/web/files.go", "/home/u/repo/internal/web/files.go"},
		{"/home/u/repo/", "a.txt", "/home/u/repo/a.txt"}, // a trailing separator is not doubled
		{`T:\others\gigagit`, "ej-app/src/X.kt", `T:\others\gigagit\ej-app\src\X.kt`},
		{`T:\others\gigagit\`, "a.txt", `T:\others\gigagit\a.txt`},
		{`\\server\share\repo`, "a/b.txt", `\\server\share\repo\a\b.txt`}, // a UNC root is still Windows
		{"", "a.txt", ""},   // no root reported: the caller drops the row
		{"/home/u", "", ""}, // nothing open: never copy a bare root
	}
	in, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	script := pure + `
const cases = JSON.parse(process.argv[1]);
console.log(JSON.stringify(cases.map((c) => absPath(c.root, c.path))));
`
	out, err := exec.Command(node, "-e", script, string(in)).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("node output %q: %v", out, err)
	}
	if len(got) != len(cases) {
		t.Fatalf("got %d results, want %d", len(got), len(cases))
	}
	for n, c := range cases {
		if got[n] != c.Want {
			t.Errorf("absPath(%q, %q) = %q, want %q", c.Root, c.Path, got[n], c.Want)
		}
	}
}
