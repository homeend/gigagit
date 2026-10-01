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

// The browser builds links itself, so it fingerprints lines itself:
// lineFingerprint must be model.LineFingerprint byte for byte, and linkFor
// must put it on exactly the links the Go producers put it on.
func TestLineFingerprintJSMatchesGo(t *testing.T) {
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
	lines := []string{"alpha", "  alpha\t", "alpha\r", "x := 1", "", " \t ", "héllo — ünï", "\tif err != nil {", "日本語", "😀 emoji", "a\u00a0b", "\ufeffalpha", "\u0085alpha\u3000", "\u00a0alpha"}
	a40 := strings.Repeat("a", 40)
	fp := model.LineFingerprint("x := 1")
	type lcase struct {
		Name string         `json:"name"`
		Ctx  map[string]any `json:"ctx"`
		Side string         `json:"side"`
		No   int            `json:"no"`
		Text string         `json:"text"`
		want string
	}
	cases := []lcase{
		{Name: "unstaged new", Ctx: map[string]any{"path": "a.go", "state": "unstaged"}, Side: "new", No: 7, Text: "x := 1", want: "gg://gigagit/a.go:7~" + fp},
		{Name: "unstaged old", Ctx: map[string]any{"path": "a.go", "state": "unstaged"}, Side: "old", No: 7, Text: "  x := 1", want: "gg://gigagit/a.go:old:7~" + fp},
		{Name: "untracked", Ctx: map[string]any{"path": "a.go", "state": "untracked"}, Side: "new", No: 7, Text: "x := 1", want: "gg://gigagit/a.go:7~" + fp},
		{Name: "staged", Ctx: map[string]any{"path": "a.go", "state": "staged"}, Side: "new", No: 7, Text: "x := 1", want: "gg://gigagit/a.go@staged:7~" + fp},
		{Name: "a blank line is plain", Ctx: map[string]any{"path": "a.go", "state": "unstaged"}, Side: "new", No: 7, Text: "   ", want: "gg://gigagit/a.go:7"},
		{Name: "no line is plain", Ctx: map[string]any{"path": "a.go", "state": "unstaged"}, Side: "new", No: 0, Text: "x := 1", want: "gg://gigagit/a.go"},
		{Name: "no text is plain", Ctx: map[string]any{"path": "a.go", "state": "unstaged"}, Side: "new", No: 7, want: "gg://gigagit/a.go:7"},
		{Name: "a commit never", Ctx: map[string]any{"path": "a.go", "state": "commit", "rev": a40}, Side: "new", No: 7, Text: "x := 1", want: "gg://gigagit/a.go@" + a40 + ":7"},
		{Name: "a pair never", Ctx: map[string]any{"path": "a.go", "compare": true, "cmpPair": map[string]any{"a": a40, "b": a40}}, Side: "new", No: 7, Text: "x := 1", want: "gg://gigagit/a.go@" + a40 + ".." + a40 + ":7"},
		{Name: "sides: the working side", Ctx: map[string]any{"path": "a.go", "compare": true, "cmpSides": map[string]any{"left": "commit:" + a40, "right": "worktree"}}, Side: "new", No: 7, Text: "x := 1", want: "gg://gigagit/a.go:7~" + fp},
		{Name: "sides: the commit side", Ctx: map[string]any{"path": "a.go", "compare": true, "cmpSides": map[string]any{"left": "commit:" + a40, "right": "worktree"}}, Side: "old", No: 7, Text: "x := 1", want: "gg://gigagit/a.go@" + a40 + ":7"},
		{Name: "sides: the index side", Ctx: map[string]any{"path": "a.go", "compare": true, "cmpSides": map[string]any{"left": "commit:" + a40, "right": "staged"}}, Side: "new", No: 7, Text: "x := 1", want: "gg://gigagit/a.go@staged:7~" + fp},
		// A line that was not valid UTF-8 reaches the page with U+FFFD in place
		// of its bytes: its fingerprint would be of different bytes than the
		// file's, so the link stays plain.
		{Name: "a line holding U+FFFD is plain", Ctx: map[string]any{"path": "a.go", "state": "unstaged"}, Side: "new", No: 7, Text: "caf\ufffd := 1", want: "gg://gigagit/a.go:7"},
		{Name: "content", Ctx: map[string]any{"path": "a.go", "state": "unstaged", "hint": map[string]any{"kind": "view", "id": "content"}}, Side: "new", No: 3, Text: "alpha", want: "gg://gigagit/a.go:3~5d8b6dab?view=content"},
	}
	dir := t.TempDir()
	lb, _ := json.Marshal(lines)
	cb, _ := json.Marshal(cases)
	script := `
import { readFileSync } from "node:fs";
const pure = readFileSync(process.argv[2], "utf8");
const lines = JSON.parse(readFileSync(process.argv[3], "utf8"));
const cases = JSON.parse(readFileSync(process.argv[4], "utf8"));
const { linkFor, lineFingerprint } = new Function(pure + "; return { linkFor, lineFingerprint };")();
console.log(JSON.stringify({
  fps: lines.map(lineFingerprint),
  links: cases.map((c) => linkFor({ link_repo: "gigagit" }, "", c.ctx, c.side, c.no, c.text)),
}));
`
	for name, body := range map[string][]byte{"pure.js": src[i:j], "lines.json": lb, "cases.json": cb, "check.mjs": []byte(script)} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command(node, filepath.Join(dir, "check.mjs"), filepath.Join(dir, "pure.js"), filepath.Join(dir, "lines.json"), filepath.Join(dir, "cases.json")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct {
		FPs   []string `json:"fps"`
		Links []string `json:"links"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for n, l := range lines {
		if want := model.LineFingerprint(l); got.FPs[n] != want {
			t.Errorf("lineFingerprint(%q) = %q, Go says %q", l, got.FPs[n], want)
		}
	}
	for n, c := range cases {
		if got.Links[n] != c.want {
			t.Errorf("%s: linkFor = %q, want %q", c.Name, got.Links[n], c.want)
			continue
		}
		if _, err := model.ParseLink(got.Links[n]); err != nil {
			t.Errorf("%s: ParseLink(%q) = %v", c.Name, got.Links[n], err)
		}
	}
}
