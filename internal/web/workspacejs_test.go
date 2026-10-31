package web

import "testing"

const wsPureStart = "// --- workspace model (pure; guarded against Go) ---"
const wsPureEnd = "// --- end workspace model ---"

func TestGroupedRowsWorkspaceOrderOwnSort(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
const members = [{path: "/a", label: "a"}, {path: "/b", label: "b"}];
const reads = {"/a": {worktrees: [{p: 2}, {p: 1}]}, "/b": {worktrees: [{p: 3}, {p: 4}]}};
const g = groupedRows(members, 1, "worktrees", (p) => reads[p], (r) => [...r].sort((x, y) => x.p - y.p));
console.log(JSON.stringify(g.map((x) => [x.label, x.live, x.rows.map((r) => r.p)])));
`)
	if want := `[["[a]",false,[1,2]],["[b]",true,[3,4]]]`; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestGroupedRowsMissingAndErrorHeadings(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
const members = [{path: "/a", label: "shop-docs", missing: true}, {path: "/b", label: "b"}, {path: "/c", label: "c"}, {path: "/d", label: "d", missing: true}];
const reads = {"/b": {error: "boom", worktrees: [{}]}, "/c": {missing: true, worktrees: [{}]}, "/d": {missing: false, worktrees: [{}]}};
const g = groupedRows(members, 0, "worktrees", (p) => reads[p], (r) => r);
console.log(JSON.stringify(g.map((x) => [x.label, x.error, x.rows.length])));
`)
	if want := `[["[shop-docs] (missing)","",0],["[b]","boom",0],["[c] (missing)","",0],["[d]","",1]]`; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestMembersToReread(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
const members = [{path: "/live", changes: 9}, {path: "/moved", changes: 2}, {path: "/same", changes: 1}, {path: "/never", changes: 0}];
const seen = new Map([["/moved", 1], ["/same", 1]]);
console.log(membersToReread(members, seen, "/live").join(","));
`)
	if want := "/moved,/never"; out != want {
		t.Fatalf("got %s, want %s", out, want)
	}
}

func TestMarkViewedOnlyOnScreenWorktree(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
console.log([headMark(true, true), headMark(true, false), headMark(false, true) === ""].join("|"));
`)
	if want := "✓:|✓|true"; out != want {
		t.Fatalf("got %s, want %s", out, want)
	}
}

func TestOwnerOfIsTheMainCheckoutsMember(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
const members = [{path: "/a"}, {path: "/b/"}];
console.log([ownerOf(members, [{path: "/b"}, {path: "/b-wt"}]).path, ownerOf(members, [{path: "/x"}]), ownerOf(members, [])].join("|"));
`)
	if want := "/b/||"; out != want {
		t.Fatalf("got %s, want %s", out, want)
	}
}
