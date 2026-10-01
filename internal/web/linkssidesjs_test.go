package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A compare addressed by two side SPECS (the entry-diff lane: worktree |
// staged | commit:<hex> | bookmark:<id> | shelf:<id>) links a line as the TUI
// does: two commits are the pair, any other compare names the clicked side as
// the version it shows, and a stored entry has no link.
func TestLinkForJSRendersACompareBySideSpecs(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS port guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "links.js"))
	if err != nil {
		t.Fatal(err)
	}
	i, j := strings.Index(string(src), linksPureStart), strings.Index(string(src), linksPureEnd)
	if i < 0 || j < i {
		t.Fatal("links.js: the guarded section markers are gone")
	}
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	type tcase struct {
		Name    string `json:"name"`
		Left    string `json:"left"`
		Right   string `json:"right"`
		Path    string `json:"path"`
		OldPath string `json:"oldPath"`
		Side    string `json:"side"`
		No      int    `json:"no"`
		want    string
	}
	cases := []tcase{
		{Name: "commit ↔ working tree, new side", Left: "commit:" + a, Right: "worktree", Path: "x.go", Side: "new", No: 7, want: "gg://gigagit/x.go:7"},
		{Name: "commit ↔ working tree, old side", Left: "commit:" + a, Right: "worktree", Path: "x.go", Side: "old", No: 7, want: "gg://gigagit/x.go@" + a + ":7"},
		{Name: "the old side of a rename has its own path", Left: "commit:" + a, Right: "worktree", Path: "x.go", OldPath: "w.go", Side: "old", No: 7, want: "gg://gigagit/w.go@" + a + ":7"},
		{Name: "commit ↔ index, new side", Left: "commit:" + a, Right: "staged", Path: "x.go", Side: "new", No: 3, want: "gg://gigagit/x.go@staged:3"},
		{Name: "working tree on the left", Left: "worktree", Right: "commit:" + b, Path: "x.go", Side: "old", No: 3, want: "gg://gigagit/x.go:3"},
		{Name: "two commits are the pair", Left: "commit:" + a, Right: "commit:" + b, Path: "x.go", Side: "old", No: 9, want: "gg://gigagit/x.go@" + a + ".." + b + ":old:9"},
		{Name: "the file, no line", Left: "commit:" + a, Right: "worktree", Path: "x.go", want: "gg://gigagit/x.go"},
		{Name: "a shelf side has no link", Left: "shelf:s1", Right: "worktree", Path: "x.go", Side: "old", No: 2, want: ""},
		{Name: "…but the other side still has one", Left: "shelf:s1", Right: "worktree", Path: "x.go", Side: "new", No: 2, want: "gg://gigagit/x.go:2"},
		{Name: "a bookmark side has no link", Left: "commit:" + a, Right: "bookmark:b1", Path: "x.go", Side: "new", No: 2, want: ""},
		{Name: "a short sha refuses", Left: "commit:" + a[:12], Right: "worktree", Path: "x.go", Side: "old", No: 2, want: ""},
	}
	dir := t.TempDir()
	blob, _ := json.Marshal(cases)
	script := `
import { readFileSync } from "node:fs";
const pure = readFileSync(process.argv[2], "utf8");
const cases = JSON.parse(readFileSync(process.argv[3], "utf8"));
const linkFor = new Function(pure + "; return linkFor;")();
console.log(JSON.stringify(cases.map((c) => linkFor({ link_repo: "gigagit" }, "", {
  path: c.path, oldPath: c.oldPath || "", compare: true, notes: false, cmpSides: { left: c.left, right: c.right },
}, c.side, c.no))));
`
	for name, body := range map[string][]byte{"pure.js": src[i:j], "cases.json": blob, "check.mjs": []byte(script)} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command(node, filepath.Join(dir, "check.mjs"), filepath.Join(dir, "pure.js"), filepath.Join(dir, "cases.json")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for n, c := range cases {
		if got[n] != c.want {
			t.Errorf("%s: linkFor = %q, want %q", c.Name, got[n], c.want)
			continue
		}
		if c.want != "" {
			if _, err := model.ParseLink(got[n]); err != nil {
				t.Errorf("%s: ParseLink(%q) = %v", c.Name, got[n], err)
			}
		}
	}
}
