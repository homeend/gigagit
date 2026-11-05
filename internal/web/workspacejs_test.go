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

func TestRowActionPlan(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
console.log(JSON.stringify([
  rowActionPlan("/a", "/a", "/a-wt", "/a"),
  rowActionPlan("/b", "/a", "/b-wt", "/b"),
  rowActionPlan("/b", "/a", "", "/b-last"),
  rowActionPlan("", "/a", "/x", ""),
]));
`)
	if want := `[{},{"switchTo":"/b-wt","member":"/b"},{"switchTo":"/b-last","member":"/b"},{}]`; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// A click on a branch checked out in another worktree of the repository on
// screen switches there, as a click on another member's branch does; the
// right-click (rowActionPlan) stays put and offers "go to its worktree".
func TestBranchClickPlan(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
console.log(JSON.stringify([
  branchClickPlan("/a", "/a", "/a-wt", "/a", "/a"),
  branchClickPlan("/a", "/a", "/a/", "/a", "/a"),
  branchClickPlan("/a", "/a", "", "/a", "/a"),
  branchClickPlan("/b", "/a", "/b-wt", "/b", "/a"),
  branchClickPlan("", "", "/x-wt", "", "/x"),
  branchClickPlan("", "", "/x", "", "/x"),
]));
`)
	if want := `[{"switchTo":"/a-wt","member":"/a"},{},{},{"switchTo":"/b-wt","member":"/b"},{"switchTo":"/x-wt","member":""},{}]`; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestRowActionPlanRefusedSwitchStops(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
const log = [];
const run = () => log.push("ran");
const b = {switchTo: "/b", member: "/b"};
await runRowPlan(b, async () => { log.push("switch"); return false; }, async () => log.push("landed"), () => "/a", run);
log.push("|");
await runRowPlan(b, async () => { log.push("switch"); return true; }, async () => log.push("landed"), () => "/b", run);
log.push("|");
await runRowPlan({}, async () => log.push("switch"), async () => log.push("landed"), () => "/a", run);
log.push("|");
// A repair question, or a later switch, answered true without landing there.
await runRowPlan(b, async () => { log.push("switch"); return true; }, async () => log.push("landed"), () => "/a", run);
console.log(log.join(" "));
`)
	if want := "switch | switch landed ran | ran | switch landed"; out != want {
		t.Fatalf("got %s, want %s", out, want)
	}
}

func TestCompareAllowed(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
console.log([compareAllowed("/a", "/a"), compareAllowed("/a", "/b"), compareAllowed("", ""), compareAllowed(undefined, "")].join("|"));
`)
	if want := "true|false|true|true"; out != want {
		t.Fatalf("got %s, want %s", out, want)
	}
}

func TestDefaultWorktreeOf(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
const wts = [{path: "/r.git", bare: true}, {path: "/r/a"}, {path: "/r/b"}];
console.log([
  defaultWorktreeOf({path: "/r", last_viewed: "/r/b"}, wts),
  defaultWorktreeOf({path: "/r", last_viewed: "/gone"}, wts),
  defaultWorktreeOf({path: "/r"}, []),
].join("|"));
`)
	if want := "/r/b|/r/a|/r"; out != want {
		t.Fatalf("got %s, want %s", out, want)
	}
}

func TestAltSAllMembersAltCapitalSLiveOnly(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
const order = ["/a", "/a-wt", "/b", "/c"];
console.log([
  walkOrder(order, false, ["/a", "/a-wt"]).join(","),
  walkOrder(order, true, ["/a", "/a-wt"]).join(","),
  walkOrder(order, true, null).join(","),
].join("|"));
`)
	if want := "/a,/a-wt,/b,/c|/a,/a-wt|/a,/a-wt,/b,/c"; out != want {
		t.Fatalf("got %s, want %s", out, want)
	}
}

// The rings key on full paths: two members' worktrees with the same directory
// name stay two stops.
func TestRingAcrossMembersSameDirName(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "walk.js", "function pathKey", "export {", `
const wtBase = (p) => p.split("/").pop();
const order = ["/shop/api/main", "/billing/api/main"];
const a = wtCycle(order, order[0], false, false, []);
const b = wtCycle(order, order[1], false, false, []);
console.log([a.path, b.path].join("|"));
`)
	if want := "/billing/api/main|/shop/api/main"; out != want {
		t.Fatalf("got %s, want %s", out, want)
	}
}

func TestRingSkipsUnreachableAndMissing(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "walk.js", "function pathKey", "export {", `
const wtBase = (p) => p.split("/").pop();
const order = ["/a", "/b-gone", "/c"];
console.log(wtCycle(order, "/a", false, false, ["/b-gone"]).path);
`)
	if want := "/c"; out != want {
		t.Fatalf("got %s, want %s", out, want)
	}
}

func TestSwitchTargetAcrossMembersInPlace(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "walk.js", "function pathKey", "export {", `
const wtBase = (p) => p.split("/").pop();
const listed = ["/a", "/a-wt", "/b"];
console.log([switchTarget("/b", "/a", listed), switchTarget("/x", "/a", listed) === "" || !switchTarget("/x", "/a", listed)].join("|"));
`)
	if want := "/b|true"; out != want {
		t.Fatalf("got %s, want %s", out, want)
	}
}

func TestRepoListWorkspacesFirstWithDots(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
const defined = [{name: "shop", repos: [{path: "/api", label: "api"}, {path: "/web", label: "web"}]}, {name: "core", repos: [{path: "/api", label: "api"}]}];
const open = {name: "shop", members: [{path: "/api", label: "api"}, {path: "/web", label: "web"}]};
const repos = [{label: "web", path: "/web"}, {label: "other", path: "/other"}];
console.log(JSON.stringify(repoListRows(defined, open, repos, "/api").map((r) => [r.label, r.ws || "", r.path || ""])));
`)
	if want := `[["● shop","shop",""],["core","core",""],["● web","","/web"],["other","","/other"]]`; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestRepoListNoWorkspacesUnchanged(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
const repos = [{label: "web", path: "/web"}];
console.log(repoListRows([], null, repos, "/api") === repos);
`)
	if out != "true" {
		t.Fatalf("got %s", out)
	}
}

func TestRepoEnterPlan(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
const open = {name: "shop", members: [{path: "/api"}, {path: "/web"}]};
console.log(JSON.stringify([
  repoEnterPlan({ws: "core"}, open),
  repoEnterPlan({ws: "shop"}, open),
  repoEnterPlan({path: "/web"}, open),
  repoEnterPlan({path: "/other"}, open),
  repoEnterPlan({path: "/other"}, null),
]));
`)
	if want := `[{"open":"core"},{},{"member":"/web"},{"leave":"/other"},{"reroot":"/other"}]`; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestDropQuestionSafeFirst(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
console.log(JSON.stringify([DROP_OPTIONS, dropAnswer(0), dropAnswer(1), dropAnswer(2), dropAnswer(-1)]));
`)
	if want := `[["Leave them running (hidden)","Stop them","Cancel"],"keep","stop",null,null]`; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestLaunchEscOpensAlone(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
const q = {repo: "shop-api", rows: [{name: "shop", label: "Open workspace shop — a, b"}]};
console.log(JSON.stringify([launchOptions(q), launchAnswer(q, 0), launchAnswer(q, 1), launchAnswer(q, -1)]));
`)
	if want := `[["Open workspace shop — a, b","Open shop-api alone"],"shop","",""]`; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestPruneKeepsOnlyMembers(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
const m = new Map([["/a", 1], ["/gone", 2], ["/b/", 3]]);
pruneToMembers(m, [{path: "/a"}, {path: "/b"}]);
console.log([...m.keys()].join(","));
`)
	if want := "/a,/b/"; out != want {
		t.Fatalf("got %s, want %s", out, want)
	}
}

// git prints a member's real path; the workspace may spell it through a symlink.
func TestRowsOwnerFallsBackToLive(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "workspace.js", wsPureStart, wsPureEnd, `
const ws = {live: 1, members: [{path: "/a"}, {path: "/link/b"}]};
console.log([
  rowsOwner(ws, [{path: "/real/b"}]).path,
  rowsOwner(ws, [{path: "/a"}]).path,
  rowsOwner(ws, []),
].join("|"));
`)
	if want := "/link/b|/a|"; out != want {
		t.Fatalf("got %s, want %s", out, want)
	}
}
