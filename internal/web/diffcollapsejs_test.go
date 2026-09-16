package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const collapsePureStart = "// --- diff collapse (pure; guarded against Go) ---"
const collapsePureEnd = "// --- end diff collapse ---"

// The browser's "changed lines only" diff view is the TUI's f toggle: every
// change plus three equal rows each side stays, every other run of equal rows
// folds to one row. The section is pure, so node pins it against the rules
// textdiff.Collapse follows — a drift here shows a change with no context, or
// hides one behind a fold.
func TestDiffCollapseJSMirrorsTextdiff(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "files.js"))
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), collapsePureStart)
	j := strings.Index(string(src), collapsePureEnd)
	if i < 0 || j < i {
		t.Fatalf("files.js: the guarded section markers are gone (%q / %q)", collapsePureStart, collapsePureEnd)
	}
	pure := string(src)[i:j]

	// Each case is a row-kind string ("s" same, "a" add, "d" del, "c" change)
	// plus which rows to force-keep and which folds to open; the expected
	// stream is the kinds of kept rows with "F<n>@<start>" for a fold.
	type tc struct {
		name string
		rows string
		keep []int  // row indexes the keep predicate forces visible
		open []int  // fold start indexes to emit expanded
		want string // "" = null (no change rows at all)
	}
	cases := []tc{
		{name: "no changes", rows: "ssssss", want: ""},
		{name: "one change mid-file keeps 3 each side", rows: "ssssssscssssss", want: "F4@0 s s s c s s s F3@11"},
		{name: "change at the top has no leading fold", rows: "csssss", want: "c s s s F2@4"},
		{name: "change at the bottom has no trailing fold", rows: "sssssa", want: "F2@0 s s s a"},
		{name: "two changes 6 apart share context, no fold", rows: "cssssssd", want: "c s s s s s s d"},
		{name: "a trailing equal run past the context folds", rows: "cssssssss", want: "c s s s F5@4"},
		{name: "adjacent change kinds are one block", rows: "sssdacsss", want: "s s s d a c s s s"},
		{name: "a kept row splits the fold", rows: "cssssssssssss", keep: []int{8}, want: "c s s s F4@4 s F4@9"},
		{name: "an open fold emits its rows", rows: "ssssssc", open: []int{0}, want: "s s s s s s c"},
		{name: "opening one fold leaves the other folded", rows: "ssssssssscsssssssss", open: []int{13}, want: "F6@0 s s s c s s s s s s s s s"},
	}

	type wire struct {
		Rows []map[string]any `json:"rows"`
		Keep []int            `json:"keep"`
		Open []int            `json:"open"`
	}
	kinds := map[byte]string{'s': "same", 'a': "add", 'd': "del", 'c': "change"}
	ins := make([]wire, len(cases))
	for n, c := range cases {
		for k := 0; k < len(c.rows); k++ {
			ins[n].Rows = append(ins[n].Rows, map[string]any{"kind": kinds[c.rows[k]], "left_no": k + 1, "right_no": k + 1})
		}
		ins[n].Keep, ins[n].Open = c.keep, c.open
		if ins[n].Keep == nil {
			ins[n].Keep = []int{}
		}
		if ins[n].Open == nil {
			ins[n].Open = []int{}
		}
	}
	dir := t.TempDir()
	casesPath := filepath.Join(dir, "cases.json")
	blob, _ := json.Marshal(ins)
	if err := os.WriteFile(casesPath, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	script := `
import { readFileSync } from "node:fs";
const pure = readFileSync(process.argv[2], "utf8");
const cases = JSON.parse(readFileSync(process.argv[3], "utf8"));
const collapse = new Function(pure + "; return collapseDiffRows;")();
const out = cases.map((c) => {
  const keep = new Set(c.keep);
  const items = collapse(c.rows, new Set(c.open), (r, i) => keep.has(i));
  if (items === null) return "";
  return items.map((it) => it.fold ? "F" + it.fold + "@" + it.start : it.kind[0]).join(" ");
});
console.log(JSON.stringify(out));
`
	purePath := filepath.Join(dir, "pure.js")
	scriptPath := filepath.Join(dir, "check.mjs")
	if err := os.WriteFile(purePath, []byte(pure), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, scriptPath, purePath, casesPath).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("node output %q: %v", out, err)
	}
	if len(got) != len(cases) {
		t.Fatalf("node returned %d results, want %d", len(got), len(cases))
	}
	for n, c := range cases {
		if got[n] != c.want {
			t.Errorf("%s: rows %q keep %v open %v\n got %q\nwant %q", c.name, c.rows, c.keep, c.open, got[n], c.want)
		}
	}
}
