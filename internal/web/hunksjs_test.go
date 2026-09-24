package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Line staging in gg web follows GitKraken: select ROWS in a working-tree
// diff, then act on them from the right-click menu, at once. These guards pin
// the pieces a browser probe cannot see in isolation.

// The rows a table paints as selected arrive as a parameter — never a
// module-level "current file" (the 4a/4b rule: a stack paints one file's
// table while another file's selection is live).
func TestHunkContextIsExplicit(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")

	if !strings.Contains(files, "function hunkCls(r, kctx)") || !strings.Contains(files, "function hunkAttr(r, kctx)") {
		t.Fatal("files.js: hunkCls/hunkAttr must take the context")
	}
	for _, fn := range []string{"hunkCls", "hunkAttr"} {
		if body := jsFunc(t, "files.js", fn); strings.Contains(body, "diffHunks") {
			t.Fatalf("files.js: %s still reads the module-level diffHunks:\n%s", fn, body)
		}
	}
	if !strings.Contains(files, "function diffHTML(d, paneWidth, notesOn = false, open = state.diffFolds, nctx = null, hctx = null, kctx = null)") {
		t.Fatal("files.js: diffHTML must take the staging context as its own parameter")
	}
	if !strings.Contains(files, "diffHunks ? { sel: diffHunks.sel } : null") {
		t.Fatal("files.js: renderDiff must hand the single file's selection to diffHTML")
	}
}

// Each stacked file keeps its SHARE of the one selection, in its own lane,
// and a re-fetch keeps it only while the file's bytes are unchanged.
func TestSlotCarriesItsOwnHunks(t *testing.T) {
	t.Parallel()
	view := readStatic(t, "stackview.js")
	stack := readStatic(t, "stack.js")

	if !strings.Contains(stack, "hunks: null,") || !strings.Contains(stack, "hunksPrev: null,") {
		t.Fatal("stack.js: every slot needs its own staging state and the parked one")
	}
	if !strings.Contains(jsFunc(t, "stack.js", "reconcileSlots"), "o.hunksPrev = o.hunks;") {
		t.Fatal("stack.js: a re-fetch must park the selection for the re-arm to judge")
	}
	if !strings.Contains(view, "s.hunks = d.hunks && hunkEligible(s.f) ? hunkState(s.f.path, d.hunks) : null;") {
		t.Fatal("stackview.js: a slot arms its staging state from its own diff's tags")
	}
	if !strings.Contains(view, "s.hunksPrev.hash === s.hunks.hash") || !strings.Contains(view, "s.hunks.sel = s.hunksPrev.sel;") {
		t.Fatal("stackview.js: a selection survives a re-fetch only while the freshness hash is unchanged")
	}
	if !strings.Contains(view, "const kctx = s.hunks ? { sel: s.hunks.sel } : null;") {
		t.Fatal("stackview.js: a slot paints with its OWN selection")
	}
}

// GitKraken's gestures: click selects the row alone, ctrl/cmd-click toggles
// it, shift-click takes a range; right-click acts on the selection or the
// hunk, immediately, and a right-click on an unselected row selects it first.
func TestRowSelectionAndItsMenu(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")

	sel := jsFunc(t, "files.js", "selectStep")
	for _, want := range []string{"mods.shift && a >= 0", "if (mods.ctrl) {", "sel = new Set([hit.id]);"} {
		if !strings.Contains(sel, want) {
			t.Fatalf("files.js: selectStep lacks %q:\n%s", want, sel)
		}
	}
	menu := jsFunc(t, "files.js", "hunkMenuRows")
	for _, want := range []string{
		`const verb = v.lane === "staged" ? "Unstage" : "Stage";`,
		"`${verb} selected line",
		"`${verb} hunk`",
		"if (!v.sel.has(key)) {", // right-click on an unselected row selects it first
	} {
		if !strings.Contains(menu, want) {
			t.Fatalf("files.js: the staging menu lacks %q:\n%s", want, menu)
		}
	}
	if !strings.Contains(files, "if (hkRow) rows.unshift(...hunkMenuRows(hkRow));") {
		t.Fatal("files.js: the diff's right-click menu must lead with the staging rows")
	}
	// The screen moves FIRST: the prediction is painted before the POST, the
	// previous diff comes back on an error, and success re-reads ONE file
	// quietly (the user found the wait-then-flicker round trip odd).
	apply := jsFunc(t, "files.js", "stageJobs")
	paint := strings.Index(apply, "showFileDiff(j.scope, predicted, null);")
	post := strings.Index(apply, `postJSON("/api/stage-hunks", {`)
	if paint < 0 || post < 0 || paint > post {
		t.Fatalf("files.js: stageJobs must paint the prediction BEFORE it posts:\n%s", apply)
	}
	for _, want := range []string{"showFileDiff(scope, before, v);", "files: live.map(", "showFileDiff(scope, d, d.hunks ? hunkState(v.path, d.hunks) : null);", "await fetchStatus();"} {
		if !strings.Contains(apply, want) {
			t.Fatalf("files.js: stageJobs lacks %q", want)
		}
	}
	if strings.Contains(apply, "reopenAfterHunkStage(v.path") || strings.Contains(apply, "openStatusDiff(") {
		t.Fatal("files.js: a successful action must not re-open the file (that is the flicker)")
	}
	// double-click acts on that one row at once
	if !strings.Contains(files, `$("diff-body").addEventListener("dblclick", (e) => {`) || !strings.Contains(jsFunc(t, "files.js", "actOnRow"), "stageSelection(") {
		t.Fatal("files.js: a double-click must stage / unstage the row")
	}
	// the stack only repaints when a file entered or left it
	rec := jsFunc(t, "stackview.js", "reconcileStack")
	same := strings.Index(rec, "if (sameFiles) {")
	full := strings.Index(rec, "paintStack(st);")
	if same < 0 || full < 0 || same > full || !strings.Contains(rec, "quietReloadSlot(st, s)") {
		t.Fatal("stackview.js: a refresh with the same files must re-read slots quietly, not repaint the stack")
	}
	// the staged section is eligible now — it is where rows are unstaged
	if !strings.Contains(jsFunc(t, "files.js", "hunkEligible"), `f.section === "changes" || f.section === "staged"`) {
		t.Fatal("files.js: the staged diff must be selectable too")
	}
	// and the pick-then-apply bar is gone for good
	if strings.Contains(readStatic(t, "index.html"), `id="hunk-bar"`) {
		t.Fatal("index.html: the old stage-selected bar must not come back")
	}
}

// The classes a row gets say one thing: selected or not, the whole row.
const hunkPaintHarness = `
import * as H from "./hunks.mjs";
const rows = [
  { kind: "change", hunk: 0, hr: 0 },
  { kind: "same" },
  { kind: "del", hunk: 1, hr: 0 },
  { kind: "add", hunk: 1, hr: 1 },
];
const out = {};
out.selSecond = rows.map((r) => H.hunkCls(r, { sel: new Set(["1:1"]) })).join("|");
out.selFirst = rows.map((r) => H.hunkCls(r, { sel: new Set(["0:0"]) })).join("|");
out.none = rows.map((r) => H.hunkCls(r, { sel: new Set() })).join("|");
out.untagged = rows.map((r) => H.hunkCls(r, null)).join("|");
out.attr = rows.map((r) => H.hunkAttr(r, { sel: new Set() })).join("|");
console.log(JSON.stringify(out));
`

func TestHunkClassesFollowTheSelection(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	mod := jsFunc(t, "files.js", "hunkCls") + "\n" + jsFunc(t, "files.js", "hunkAttr") + "\nexport { hunkCls, hunkAttr };\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hunks.mjs"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(hunkPaintHarness), 0o644); err != nil {
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
		"selSecond": " hk|| hk| hk hk-sel",
		"selFirst":  " hk hk-sel|| hk| hk",
		"none":      " hk|| hk| hk",
		"untagged":  "|||",
		"attr":      ` data-hunk="0" data-hr="0"|| data-hunk="1" data-hr="0"| data-hunk="1" data-hr="1"`,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
}
