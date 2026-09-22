package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// stack.js is the pure half of the stacked diff view: which rows a stack
// holds, what loads next, how a working-tree refresh keeps the reader's
// sections. Import-free, so it runs under node against real shapes.
const stackHarness = `
import * as S from "./stack.mjs";
const out = {};
const rows = (n) => Array.from({ length: n }, (_, i) => ({ f: { path: "p" + i, status: "M" }, idx: i }));

// 1. the auto-collapse threshold is "more than 100"
out.hundredOpen = S.buildSlots(rows(100)).every((s) => !s.collapsed);
out.hundredOneShut = S.buildSlots(rows(101)).every((s) => s.collapsed);

// 2. a working-tree stack holds its group only; a conflict is never fetched
const list = [
  { path: "a", section: "changes", unstaged: "M" },
  { path: "b", section: "untracked" },
  { path: "c", section: "conflicts" },
  { path: "a", section: "staged", staged: "A" },
];
const wt = S.stackRows(list, "worktree");
out.wtIdx = wt.map((r) => r.idx);
out.stagedIdx = S.stackRows(list, "staged").map((r) => r.idx);
out.allIdx = S.stackRows(list, "").map((r) => r.idx);
const ws = S.buildSlots(wt);
out.letters = ws.map((s) => s.status).join("");
out.conflictLoad = ws[2].load;
out.keysDiffer = S.slotKey(list[0]) !== S.slotKey(list[3]);

// 3. pick order: nearest the anchor, ties go below, skip collapsed/loaded/far
const sl = S.buildSlots(rows(6));
sl[3].load = "ok";
sl[4].collapsed = true;
out.pick1 = S.nextToLoad(sl, new Set([1, 2, 3, 4, 5]), 3); // 2 and 5 → dist 1 vs 2 → 2
out.pickTie = S.nextToLoad(S.buildSlots(rows(5)), new Set([1, 3]), 2); // tie → 3 (below)
out.pickNone = S.nextToLoad(sl, new Set([3, 4]), 3);

// 4. counts from a diff's aligned rows; binary; none
out.counts = S.countsFromDiff({ rows: [{ kind: "add" }, { kind: "del" }, { kind: "change" }, { kind: "same" }] });
out.bin = S.countsFromDiff({ binary: true });
out.none = S.countsFromDiff(null);

// 5. reconcile keeps objects, collapse and folds; changed status drops the diff
const old = S.buildSlots(rows(3));
old[0].collapsed = true; old[0].folds.add(7);
old[1].diff = { rows: [] }; old[1].load = "ok";
old[2].load = "loading";
const next = S.reconcileSlots(old, [
  { f: { path: "p1", status: "M" }, idx: 0 },
  { f: { path: "new", status: "A" }, idx: 1 },
  { f: { path: "p0", status: "D" }, idx: 2 },
  { f: { path: "p2", status: "M" }, idx: 3 },
]);
out.order = next.map((s) => s.path).join(",");
out.sameObject = next[0] === old[1];
out.keptDiffIdle = next[0].diff !== null && next[0].load === "idle";
out.statusChangeDropsDiff = next[2].diff === null && next[2].collapsed === true && next[2].folds.has(7);
out.midLoadAgain = next[3].again === true && next[3].load === "loading";
out.idxUpdated = next[3].idx === 3;

// 6. a symmetric row's pair, in the arrow's direction; a row's own spec wins
const c = { aSpec: "A", bSpec: "B", flipped: false };
const row = { path: "x", left: "present", right: "deleted", status: "D", kind: "ne", right_spec: "Rx" };
out.pair = S.symPair(row, c);
out.pairFlip = S.symPair({ ...row, status: "A" }, { ...c, flipped: true });
out.pairEq = S.symPair({ path: "y", left: "present", right: "present", status: "=", kind: "eq" }, c).status;
out.pairNone = S.symPair({ path: "z", left: "deleted", right: "absent", status: "D", kind: "or" }, c);
out.why = S.noContentWhy({ left: "deleted", right: "absent" });
// 7. sym slots: glyph data rides along; a no-content row is never fetched
const ss = S.buildSlots([
  { f: row, idx: 0 },
  { f: { path: "z", left: "deleted", right: "absent", status: "D", kind: "or" }, idx: 1 },
]);
out.symKind = ss[0].kind + ss[0].left + ss[0].right + ss[0].load + ss[0].none;
out.symNone = ss[1].load + ":" + ss[1].none;
out.plainNone = S.buildSlots(rows(1))[0].none + "|" + S.buildSlots(rows(1))[0].kind;
out.conflictWhy = ws[2].none;
out.glyphs = Object.values(S.GLYPH).join("");
// a working-tree entry has a kind of its own: it is NOT a symmetric row
const wtRow = S.buildSlots([{ f: { path: "a", section: "changes", unstaged: "M", kind: "tracked" }, idx: 0 }])[0];
out.wtKind = wtRow.load + "|" + wtRow.kind + "|" + wtRow.none;

console.log(JSON.stringify(out));
`

func TestStackJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "stack.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stack.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(stackHarness), 0o644); err != nil {
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
		"hundredOpen": true, "hundredOneShut": true,
		"letters": "M?U", "conflictLoad": "none", "keysDiffer": true,
		"pick1": float64(2), "pickTie": float64(3), "pickNone": float64(-1),
		"order":      "p1,new,p0,p2",
		"sameObject": true, "keptDiffIdle": true, "statusChangeDropsDiff": true,
		"midLoadAgain": true, "idxUpdated": true,
		"pairEq": "M", "pairNone": nil,
		"why":     "the left set deletes it, the right set does not touch it",
		"symKind": "nepresentdeletedidle", "symNone": "none:empty",
		"plainNone": "|", "conflictWhy": "conflict", "glyphs": "≠=◁▷", "wtKind": "idle||",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	idx := func(k string) string { b, _ := json.Marshal(got[k]); return string(b) }
	if idx("pair") != `{"left":"A","right":"Rx","status":"D"}` || idx("pairFlip") != `{"left":"Rx","right":"A","status":"A"}` {
		t.Errorf("symPair: %s / flipped %s", idx("pair"), idx("pairFlip"))
	}
	if idx("wtIdx") != "[0,1,2]" || idx("stagedIdx") != "[3]" || idx("allIdx") != "[0,1,2,3]" {
		t.Errorf("stackRows: wt=%s staged=%s all=%s", idx("wtIdx"), idx("stagedIdx"), idx("allIdx"))
	}
	if idx("counts") != `{"add":2,"del":2}` || idx("bin") != `{"binary":true}` || idx("none") != "null" {
		t.Errorf("countsFromDiff: %s %s %s", idx("counts"), idx("bin"), idx("none"))
	}
}
