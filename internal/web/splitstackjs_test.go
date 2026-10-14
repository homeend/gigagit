package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// splitstack.js is the pure half of the overview split's stacked file
// column (spec 2026-10-11-web-overview-split §11): which files, the
// (file, line) cursor, header and band steps. Import-free: node runs it.
const splitStackHarness = `
import * as S from "./splitstack.mjs";
const out = {};
const anchors = [
  { dest: "a.txt:2", path: "a.txt", start: 2 },
  { dest: "b.txt:1", path: "b.txt", start: 1 },
  { dest: "a.txt:4", path: "a.txt", start: 4, end: 5 },
  { dest: "n1", note: "n1", path: "c.txt", start: 3 },
  { dest: "x", path: "x.txt", plain: true },
  { dest: "m", path: "m.txt", missing: true },
  { dest: "n2", note: "n2" },
];
// 1. first-linked order: duplicates once, a note link's file, plain / missing / pathless skipped
out.linked = S.stackFiles(null, anchors).map((f) => f.path).join(",");
// 2. the review's files in its order, D marked deleted
const rf = S.stackFiles([{ path: "z.txt", status: "M" }, { path: "gone.txt", status: "D" }, { path: "a.txt", status: "A" }], anchors);
out.review = rf.map((f) => f.path + ":" + f.status + ":" + f.deleted).join(",");
// 3. slots: a deleted file is never read and says why; over 100 files all fold but the target
const sl = S.newSlots(rf, 0);
out.deletedSlot = sl[1].load + "|" + sl[1].why;
out.smallOpen = sl.every((s) => !s.collapsed);
const many = S.newSlots(Array.from({ length: 101 }, (_, i) => ({ path: "f" + i, status: "", deleted: false })), 7);
out.manyFolded = many.filter((s) => s.collapsed).length + ":" + many[7].collapsed;
out.manyAuto = many.filter((s) => s.autoFold).length + ":" + many[7].autoFold + ":" + sl.some((s) => s.autoFold);
out.slotOf = S.slotOf(sl, "a.txt") + "," + S.slotOf(sl, "nope");
// 4. moves: header (0) then the shown lines; folded / unloaded files are their header only
const st = S.newSlots([{ path: "a" }, { path: "b" }, { path: "c" }], -1);
st[0].load = "ok"; st[0].lines = [{}, {}, {}];
st[2].load = "ok"; st[2].lines = [{}, {}];
const mv = (k, line, d) => { const c = S.stackMove(st, { k, line }, d); return c.k + ":" + c.line; };
out.moves = [mv(0, 0, 1), mv(0, 3, 1), mv(1, 0, 1), mv(2, 0, -1), mv(1, 0, -1), mv(0, 0, -1), mv(2, 2, 5), mv(0, 1, 6)].join(",");
out.end = JSON.stringify(S.stackEnd(st));
out.shows = [S.shows(st[0]), S.shows(st[1]), S.shows({ ...st[0], collapsed: true }), S.shows({ ...st[0], why: "(binary)" })].join(",");
// 5. N / P: the next header; back from inside a file stops on its own header first
const nh = (k, line, d) => JSON.stringify(S.nextHeader(st, { k, line }, d));
out.headers = [nh(0, 2, 1), nh(2, 0, 1), nh(2, 1, -1), nh(2, 0, -1), nh(0, 0, -1)].join(",");
// 6. bands across the stack in reading order; n / p from the current band, else from the cursor, wrapping
const bandsOf = (p, n) => (p === "a" ? [{ start: 1, end: 1, i: 0 }, { start: 3, end: 3, i: 2 }] : p === "c" ? [{ start: 2, end: 2, i: 1 }] : []);
const bands = S.stackBands(st, bandsOf);
out.bands = bands.map((b) => b.k + ":" + b.start + ":" + b.i).join(",");
const step = (cur, k, line, d) => { const r = S.stepStackBand(bands, cur, { k, line }, d); return r.i + (r.wrapped ? "w" : ""); };
out.steps = [step(0, 0, 1, 1), step(-1, 0, 2, 1), step(-1, 1, 0, 1), step(2, 2, 2, 1), step(-1, 0, 0, -1), step(-1, 2, 0, -1), step(0, 0, 1, -1)].join(",");
out.noBands = JSON.stringify(S.stepStackBand([], -1, { k: 0, line: 0 }, 1));
// 7. restack keeps loaded slots by path, drops gone ones, adds new idle ones
const re = S.restack(st, [{ path: "c" }, { path: "new" }, { path: "a" }]);
out.restack = re.map((s) => s.path + ":" + s.load).join(",") + "|" + (re[0] === st[2]);
// 8. placeholder rows, the title
out.ph = [S.placeholderRows({ n: 0 }), S.placeholderRows({ n: 40 }), S.placeholderRows({ n: 9000 })].join(",");
out.title = S.stackTitle("PR #7 review", 1, 3);
out.titleEmpty = S.stackTitle("Notes only", 0, 0);
console.log(JSON.stringify(out));
`

func TestSplitStackJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "splitstack.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "splitstack.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(splitStackHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
		t.Fatalf("not the harness JSON: %v\n%s", err, out)
	}
	want := map[string]any{
		"linked":      "a.txt,b.txt,c.txt",
		"review":      "z.txt:M:false,gone.txt:D:true,a.txt:A:false",
		"deletedSlot": "none|(deleted at the reviewed commit)",
		"smallOpen":   true,
		"manyFolded":  "100:false",
		"manyAuto":    "100:false:false",
		"slotOf":      "2,-1",
		"moves":       "0:1,1:0,2:0,1:0,0:3,0:0,2:2,2:2",
		"end":         `{"k":2,"line":2}`,
		"shows":       "true,false,false,false",
		"headers":     `{"k":1,"line":0},null,{"k":2,"line":0},{"k":1,"line":0},null`,
		"bands":       "0:1:0,0:3:2,2:2:1",
		"steps":       "1,1,2,0w,2w,1,2w",
		"noBands":     `{"i":-1,"wrapped":false}`,
		"restack":     "c:ok,new:idle,a:ok|true",
		"ph":          "3,40,2000",
		"title":       "Files: PR #7 review · file 2/3",
		"titleEmpty":  "Files: Notes only · no files",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}
