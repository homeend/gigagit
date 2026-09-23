package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ONE selection spans the whole stack (user ruling 2026-09-24): a ctrl-click
// in another file adds to it, a shift-click range walks the rows in the order
// they are shown across files, and a selection holds one lane at a time. Row
// ids are "<file>\u0000<hunk>:<row>".
const rowSelHarness = `
import * as R from "./rowsel.mjs";
const order = [
  { id: "a\u00000:0", lane: "unstaged" },
  { id: "a\u00000:1", lane: "unstaged" },
  { id: "b\u00000:0", lane: "unstaged" },
  { id: "b\u00001:0", lane: "unstaged" },
  { id: "c\u00000:0", lane: "unstaged" },
  { id: "s\u00000:0", lane: "staged" },
];
const at = (id) => order.findIndex((r) => r.id === id);
const show = (s) => [...s.sel].sort().join(",").replace(/\u0000/g, "/") + " @" + String(s.anchor).replace(/\u0000/g, "/");
const empty = { sel: new Set(), anchor: null };
const none = { shift: false, ctrl: false };
const ctrl = { shift: false, ctrl: true };
const shift = { shift: true, ctrl: false };
const out = {};
let s = R.selectStep(order, empty, at("a\u00000:0"), ctrl);
s = R.selectStep(order, s, at("a\u00000:1"), ctrl);
s = R.selectStep(order, s, at("b\u00000:0"), ctrl);
s = R.selectStep(order, s, at("c\u00000:0"), ctrl);
out.ctrlAcross = show(s);
out.ctrlOff = show(R.selectStep(order, s, at("b\u00000:0"), ctrl));
out.plain = show(R.selectStep(order, s, at("b\u00001:0"), none));
s = R.selectStep(order, empty, at("a\u00000:1"), none);
out.shiftAcross = show(R.selectStep(order, s, at("c\u00000:0"), shift));
out.shiftUp = show(R.selectStep(order, R.selectStep(order, empty, at("c\u00000:0"), none), at("a\u00000:1"), shift));
out.shiftNoAnchor = show(R.selectStep(order, empty, at("b\u00000:0"), shift));
out.shiftCtrl = show(R.selectStep(order, { sel: new Set(["a\u00000:0", "c\u00000:0"]), anchor: "b\u00000:0" }, at("b\u00001:0"), { shift: true, ctrl: true }));
out.otherLane = show(R.selectStep(order, { sel: new Set(["a\u00000:0", "b\u00000:0"]), anchor: "b\u00000:0" }, at("s\u00000:0"), ctrl));
out.staleDropped = show(R.selectStep(order, { sel: new Set(["gone\u00000:0"]), anchor: "gone\u00000:0" }, at("a\u00000:0"), ctrl));
console.log(JSON.stringify(out));
`

func TestOneSelectionSpansTheStack(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	mod := jsFunc(t, "files.js", "selectStep") + "\nexport { selectStep };\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rowsel.mjs"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(rowSelHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
		t.Fatalf("not the harness JSON: %v\n%s", err, out)
	}
	want := map[string]string{
		"ctrlAcross":    "a/0:0,a/0:1,b/0:0,c/0:0 @c/0:0",
		"ctrlOff":       "a/0:0,a/0:1,c/0:0 @b/0:0",
		"plain":         "b/1:0 @b/1:0",
		"shiftAcross":   "a/0:1,b/0:0,b/1:0,c/0:0 @a/0:1",
		"shiftUp":       "a/0:1,b/0:0,b/1:0,c/0:0 @c/0:0",
		"shiftNoAnchor": "b/0:0 @b/0:0",
		"shiftCtrl":     "a/0:0,b/0:0,b/1:0,c/0:0 @b/0:0",
		"otherLane":     "s/0:0 @s/0:0",
		"staleDropped":  "a/0:0 @a/0:0",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: got %q, want %q", k, got[k], w)
		}
	}
}

// Every way out of a selection: a click that lands on no changed row, Esc,
// and the double-click rules (a marked row stages the whole selection as it
// stood BEFORE the double-click's first click; an unmarked row stages alone).
func TestSelectionClearsAndDoubleClickStagesIt(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	keys := readStatic(t, "keys.js")

	if !strings.Contains(files, `document.addEventListener("click", (e) => {`) || !strings.Contains(jsFunc(t, "files.js", "clearRowSelection"), "return true;") {
		t.Fatal("files.js: a click outside the changed rows must clear the selection")
	}
	esc := strings.Index(keys, `if (e.key === "Escape" && clearRowSelection()) return;`)
	search := strings.Index(keys, "if (diffSearchKey(e)) return;")
	if esc < 0 || search < 0 || esc > search {
		t.Fatal("keys.js: Esc must clear a selection before it clears a search or leaves the diff")
	}
	dbl := jsFunc(t, "files.js", "actOnRow")
	for _, want := range []string{"preClickSel", "stageSelection("} {
		if !strings.Contains(dbl, want) {
			t.Fatalf("files.js: actOnRow lacks %q:\n%s", want, dbl)
		}
	}
	menu := jsFunc(t, "files.js", "hunkMenuRows")
	if !strings.Contains(menu, "selectionSize()") {
		t.Fatalf("files.js: the menu must count the WHOLE selection:\n%s", menu)
	}
}
