package web

import (
	"strings"
	"testing"
)

// allFilesShown: the mode is on AND the screen is a commit's files (not a
// stash, not a review on screen, not a compare/status screen the flag
// outlived, not the list a closed view left behind). A review left in state
// after another commit opened over it is NOT on screen (reviewActiveIn).
func TestAllFilesShownJS(t *testing.T) {
	t.Parallel()
	src := jsFunc(t, "reviews.js", "reviewActiveIn") + jsFunc(t, "files.js", "allFilesShownIn") + `
const files = [{ path: "a" }];
const base = { filesAll: true, filesMode: "commit", fileStash: false, review: null, layout: "files", files };
console.log(JSON.stringify([
  allFilesShownIn(base),
  allFilesShownIn({ ...base, layout: "diff" }),
  allFilesShownIn({ ...base, filesAll: false }),
  allFilesShownIn({ ...base, filesMode: "compare" }),
  allFilesShownIn({ ...base, filesMode: "status" }),
  allFilesShownIn({ ...base, fileStash: true }),
  allFilesShownIn({ ...base, review: { id: "r", files } }),
  allFilesShownIn({ ...base, review: { id: "r", files: [] } }),
  allFilesShownIn({ ...base, layout: "list" }),
]));
`
	if got := nodeRun(t, src); got != `[true,true,false,false,false,false,false,true,false]` {
		t.Errorf("allFilesShownIn = %s", got)
	}
}

// A tree row's diff is the commit against the working tree: its context
// names both sides (links address the working file on the right, the
// commit's own text on the left; go to line opens the working file), and it
// carries no notes.
func TestAllFilesDiffCtxJS(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("c", 40)
	src := jsFunc(t, "files.js", "allFilesDiffCtx") + `
console.log(JSON.stringify(allFilesDiffCtx({ path: "src/a.txt" }, "` + sha + `")));
`
	want := `{"path":"src/a.txt","rev":"` + sha + `","state":"commit","notes":false,"compare":true,"cmpSides":{"left":"commit:` + sha + `","right":"worktree"}}`
	if got := nodeRun(t, src); got != want {
		t.Errorf("allFilesDiffCtx = %s\nwant %s", got, want)
	}
}

// keepAllFiles: an open of another commit keeps the mode only while the
// screen it replaces is a commit's files view in it — the TUI's mode sticks
// while the files view is open and goes with it.
func TestKeepAllFilesJS(t *testing.T) {
	t.Parallel()
	src := jsFunc(t, "commits.js", "keepAllFiles") + `
const on = { filesAll: true, layout: "files", filesMode: "commit", fileStash: false };
console.log(JSON.stringify([
  keepAllFiles(on),
  keepAllFiles({ ...on, layout: "diff" }),
  keepAllFiles({ ...on, layout: "list" }),
  keepAllFiles({ ...on, filesMode: "compare" }),
  keepAllFiles({ ...on, fileStash: true }),
  keepAllFiles({ ...on, filesAll: false }),
]));
`
	if got := nodeRun(t, src); got != `[true,true,false,false,false,false]` {
		t.Errorf("keepAllFiles = %s", got)
	}
}

func TestAllFilesClientWiring(t *testing.T) {
	t.Parallel()
	commits := readStaticSrc(t, "commits.js")
	files := readStaticSrc(t, "files.js")
	keys := readStaticSrc(t, "keys.js")
	html := readStaticSrc(t, "index.html")
	for _, c := range []struct{ src, want, why string }{
		{commits, `getJSON(commitFilesURL(row.hash, all))`, "openCommit asks for the tree in all-files mode"},
		{commits, `getJSON(commitFilesURL(hash, all))`, "openCommitByHash too"},
		{commits, `const all = !(opts && opts.thenFile) && keepAllFiles(state);`, "a jump-in that opens a file lands on the commit's CHANGE (the TUI's beginFilesView)"},
		{files, `if (allFilesShown()) return allFilesDiffCtx(f, state.fileSha);`, "a tree row's diff context names both sides"},
		{files, `syncAllFilesChip();`, "the foot chip follows the layout"},
		{files, `the list stops at `, "a capped tree says so"},
		{commits, `return "/api/commit/" + hash + (all ? "?all=1" : "");`, "?all=1 selects the tree"},
		{commits, `state.fileStash = true;`, "a stash detail is marked, so a refuses it"},
		{files, `left: "commit:" + state.fileSha, right: "worktree"`, "a tree row diffs the commit against the working tree"},
		{files, `if (stackOn() && !allFilesShown()) return openStack(i);`, "a tree file always opens alone"},
		{files, `(all files)`, "the title says all files"},
		{files, `" ↔ working tree"`, "the diff title names the working tree side"},
		{keys, `e.key === "a" && !e.ctrlKey && !e.metaKey && !e.altKey && allFilesOK()`, "a toggles in a commit's files view"},
		{keys, `case "allfiles": toggleAllFiles(); break;`, "the foot chip toggles"},
		{html, `<button data-act="allfiles">a all files</button>`, "the foot advertises a"},
		{html, `<span class="hkey">a · all files</span>`, "help names a"},
	} {
		if !strings.Contains(c.src, c.want) {
			t.Errorf("%s: missing %q", c.why, c.want)
		}
	}
	// a must route before the notes agent's a (which needs a diff).
	if i, j := strings.Index(keys, `allFilesOK()`), strings.Index(keys, `noteKey(e, "a")`); i < 0 || j < 0 || i > j {
		t.Error("the all-files a is not routed before noteKey(e, \"a\")")
	}
}
