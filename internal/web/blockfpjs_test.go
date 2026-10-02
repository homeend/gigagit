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

// A range link's fingerprint covers the whole block: blockFingerprint must be
// model.BlockFingerprint byte for byte, and linkFor must write the range the
// way the Go producers do (`:a-b`, the fp only on uncommitted lines).
func TestBlockFingerprintJSMatchesGo(t *testing.T) {
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
	blocks := [][]string{{"a", "b"}, {"  a\t", "b\r"}, {"a", "", "b"}, {"", " \t"}, {"a\u00a0", "\ufeffb"}, {"x := 1"}, {"日本語", "😀"}}
	a40, b40 := strings.Repeat("a", 40), strings.Repeat("b", 40)
	blk := []string{"l5", "  l6", "l7"}
	fp := model.BlockFingerprint(blk)
	type lcase struct {
		Name  string         `json:"name"`
		Ctx   map[string]any `json:"ctx"`
		Side  string         `json:"side"`
		No    int            `json:"no"`
		End   int            `json:"end"`
		Block []string       `json:"block"`
		want  string
	}
	un := map[string]any{"path": "a.go", "state": "unstaged"}
	cases := []lcase{
		{Name: "unstaged new", Ctx: un, Side: "new", No: 5, End: 7, Block: blk, want: "gg://gigagit/a.go:5-7~" + fp},
		{Name: "unstaged old", Ctx: un, Side: "old", No: 5, End: 7, Block: blk, want: "gg://gigagit/a.go:old:5-7~" + fp},
		{Name: "staged", Ctx: map[string]any{"path": "a.go", "state": "staged"}, Side: "new", No: 5, End: 7, Block: blk, want: "gg://gigagit/a.go@staged:5-7~" + fp},
		{Name: "a commit never", Ctx: map[string]any{"path": "a.go", "state": "commit", "rev": a40}, Side: "new", No: 5, End: 7, Block: blk, want: "gg://gigagit/a.go@" + a40 + ":5-7"},
		{Name: "a pair's old side", Ctx: map[string]any{"path": "a.go", "compare": true, "cmpPair": map[string]any{"a": a40, "b": b40}}, Side: "old", No: 5, End: 7, Block: blk, want: "gg://gigagit/a.go@" + a40 + ".." + b40 + ":old:5-7"},
		{Name: "a preview's new side", Ctx: map[string]any{"path": "a.go", "preview": map[string]any{"source": "feat", "target": "main"}}, Side: "new", No: 5, End: 7, Block: blk, want: "gg://gigagit/a.go@main...feat:5-7"},
		{Name: "a preview's old side is the file", Ctx: map[string]any{"path": "a.go", "preview": map[string]any{"source": "feat", "target": "main"}}, Side: "old", No: 5, End: 7, Block: blk, want: "gg://gigagit/a.go@main...feat"},
		{Name: "content", Ctx: map[string]any{"path": "a.go", "state": "unstaged", "hint": map[string]any{"kind": "view", "id": "content"}}, Side: "new", No: 5, End: 7, Block: blk, want: "gg://gigagit/a.go:5-7~" + fp + "?view=content"},
		{Name: "sides: the working side", Ctx: map[string]any{"path": "a.go", "compare": true, "cmpSides": map[string]any{"left": "commit:" + a40, "right": "worktree"}}, Side: "new", No: 5, End: 7, Block: blk, want: "gg://gigagit/a.go:5-7~" + fp},
		{Name: "sides: the commit side", Ctx: map[string]any{"path": "a.go", "compare": true, "cmpSides": map[string]any{"left": "commit:" + a40, "right": "worktree"}}, Side: "old", No: 5, End: 7, Block: blk, want: "gg://gigagit/a.go@" + a40 + ":5-7"},
		{Name: "a short block is plain", Ctx: un, Side: "new", No: 5, End: 7, Block: blk[:2], want: "gg://gigagit/a.go:5-7"},
		{Name: "no block is plain", Ctx: un, Side: "new", No: 5, End: 7, want: "gg://gigagit/a.go:5-7"},
		{Name: "U+FFFD is plain", Ctx: un, Side: "new", No: 5, End: 7, Block: []string{"l5", "caf\ufffd", "l7"}, want: "gg://gigagit/a.go:5-7"},
		{Name: "an all-blank block is plain", Ctx: un, Side: "new", No: 5, End: 6, Block: []string{"", "  "}, want: "gg://gigagit/a.go:5-6"},
		{Name: "end == no is one line", Ctx: un, Side: "new", No: 5, End: 5, Block: []string{"l5"}, want: "gg://gigagit/a.go:5"},
	}
	dir := t.TempDir()
	bb, _ := json.Marshal(blocks)
	cb, _ := json.Marshal(cases)
	script := `
import { readFileSync } from "node:fs";
const pure = readFileSync(process.argv[2], "utf8");
const blocks = JSON.parse(readFileSync(process.argv[3], "utf8"));
const cases = JSON.parse(readFileSync(process.argv[4], "utf8"));
const { linkFor, blockFingerprint } = new Function(pure + "; return { linkFor, blockFingerprint };")();
console.log(JSON.stringify({
  fps: blocks.map(blockFingerprint),
  links: cases.map((c) => linkFor({ link_repo: "gigagit" }, "", c.ctx, c.side, c.no, "", c.end, c.block)),
}));
`
	for name, body := range map[string][]byte{"pure.js": src[i:j], "blocks.json": bb, "cases.json": cb, "check.mjs": []byte(script)} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command(node, filepath.Join(dir, "check.mjs"), filepath.Join(dir, "pure.js"), filepath.Join(dir, "blocks.json"), filepath.Join(dir, "cases.json")).CombinedOutput()
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
	for n, b := range blocks {
		if want := model.BlockFingerprint(b); got.FPs[n] != want {
			t.Errorf("blockFingerprint(%q) = %q, Go says %q", b, got.FPs[n], want)
		}
	}
	for n, c := range cases {
		if got.Links[n] != c.want {
			t.Errorf("%s: linkFor = %q, want %q", c.Name, got.Links[n], c.want)
			continue
		}
		l, err := model.ParseLink(got.Links[n])
		if err != nil {
			t.Errorf("%s: ParseLink(%q) = %v", c.Name, got.Links[n], err)
			continue
		}
		if strings.Contains(c.want, "-7") && l.End != 7 {
			t.Errorf("%s: parsed End = %d, want 7", c.Name, l.End)
		}
	}
}
