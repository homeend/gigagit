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

// A commit pair's diff is addressable: `/path@<a>..<b>[:N]`. linkFor takes the
// pair as ctx.preview.pair — a preview with NO names — so the pair arm has to
// be reached before the names are screened, the JS twin of "ask IsPair()
// first". An old-side line degrades to the file form, as a preview's does: a
// pair link has no old side either.
func TestLinkForJSRendersACommitPair(t *testing.T) {
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
		Name   string `json:"name"`
		A      string `json:"a"`
		B      string `json:"b"`
		Source string `json:"source"`
		Target string `json:"target"`
		Path   string `json:"path"`
		Side   string `json:"side"`
		No     int    `json:"no"`
		want   string
	}
	link := func(path string, line int) string {
		l := model.Link{Repo: model.LinkRepo{Name: "gigagit"}, Path: path, Side: model.NoteSideNew, Line: line,
			Target: model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: a, B: b}}}
		return l.String()
	}
	cases := []tcase{
		{Name: "a file", A: a, B: b, Path: "a/b.go", want: link("a/b.go", 0)},
		{Name: "a new-side line", A: a, B: b, Path: "a/b.go", Side: "new", No: 42, want: link("a/b.go", 42)},
		{Name: "an old-side line degrades to the file", A: a, B: b, Path: "a/b.go", Side: "old", No: 17, want: link("a/b.go", 0)},
		{Name: "a short a refuses", A: a[:39], B: b, Path: "a/b.go", want: ""},
		{Name: "a short b refuses", A: a, B: b[:39], Path: "a/b.go", want: ""},
		// The ordering: a ctx carrying BOTH is the pair's. Were the names read
		// first this would come back as main...feat/x.
		{Name: "a pair wins over names", A: a, B: b, Source: "feat/x", Target: "main", Path: "a/b.go", want: link("a/b.go", 0)},
	}
	if want := "gg://gigagit/a/b.go@" + a + ".." + b + ":42"; cases[1].want != want {
		t.Fatalf("pinned: model renders %q, want %q", cases[1].want, want)
	}
	dir := t.TempDir()
	blob, _ := json.Marshal(cases)
	script := `
import { readFileSync } from "node:fs";
const pure = readFileSync(process.argv[2], "utf8");
const cases = JSON.parse(readFileSync(process.argv[3], "utf8"));
const linkFor = new Function(pure + "; return linkFor;")();
console.log(JSON.stringify(cases.map((c) => linkFor({ link_repo: "gigagit" }, "", {
  path: c.path, rev: c.b, state: "commit", compare: true,
  preview: { pair: { a: c.a, b: c.b }, source: c.source || "", target: c.target || "", pr: 0 },
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
		if c.want == "" {
			continue
		}
		l, err := model.ParseLink(got[n])
		if err != nil || l.Target.Pair == nil || l.Target.Pair.A != a || l.Target.Pair.B != b || l.Path != c.Path {
			t.Errorf("%s: %q does not read back as the pair: %+v, %v", c.Name, got[n], l, err)
		}
	}
}
