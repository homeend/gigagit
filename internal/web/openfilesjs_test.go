package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ofPureStart = "// --- switcher model (pure; guarded against Go) ---"
const ofPureEnd = "// --- end switcher model ---"

func TestSwitcherModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const rows = switcherRows([
  {id: "f2", path: "a/b.go", line: 12, source: "worktree", state: "shown", notes: 2},
  {id: "f1", path: "c.txt", line: 0, source: "commit", rev: "0123456789", state: "shown"},
], "f2");
console.log(JSON.stringify(rows) + "|" + [clampSel(5, 2), clampSel(-1, 2), clampSel(3, 0)].join(","));
`)
	want := `[{"id":"f2","mark":"●","path":"a/b.go","line":":12","source":"worktree","rev":"","notes":2,"meta":""},{"id":"f1","mark":"○","path":"c.txt","line":"","source":"commit","rev":"0123456789","notes":0,"meta":""}]|1,0,0`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// The viewer's footer is built in a template literal, where "\ " is an escape
// for a plain space: the chip must write "\\" or the page shows "ctrl+ open
// files" (found by the 5c browser check).
// An overview's row names it by its title (its path is the TUI's display
// name, overview-<n>.md).
func TestSwitcherOverviewRowJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const r = switcherRows([{id: "f4", path: "overview-4.md", source: "overview", title: "The tour", line: 0}], "")[0];
console.log(r.path + "|" + r.source);
`)
	if out != "The tour|overview" {
		t.Fatalf("got %s", out)
	}
}

// An overview row's meta is the TUI's: "overview · N anchors · author ·
// age"; D deletes the overview, or a noted file's notes, or nothing, and
// the confirm's prompt follows (the TUI's words).
func TestSwitcherOverviewRowMetaAndDeleteActionJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const now = Date.parse("2026-01-02T00:00:00Z");
const ov = {id: "20260101T220000-ab12", path: "overview-20260101T220000-ab12.md", source: "overview", title: "Tour of f", author: "agent", created: "2026-01-01T22:00:00Z", anchors: 1};
const noted = {id: "f2", path: "a/b.go", source: "worktree", notes: 2};
const plain = {id: "f3", path: "c.txt", source: "worktree"};
const rows = switcherRows([ov, noted, plain], "", now);
console.log([rows[0].meta, rows[1].meta, rowAge("", now), rowAge("2025-01-01T00:00:00Z", now),
  deleteAction(ov), deleteAction(noted), deleteAction(plain),
  deletePrompt(ov), deletePrompt(noted), deletePrompt({id: "f4", path: "d.go", source: "worktree", notes: 1}), deletePrompt(plain)].join("|"));
`)
	want := "overview · 1 anchors · agent · 2h||" + "|1y|overview|notes||Delete this overview?\nTour of f|Delete the 2 notes on a/b.go?|Delete the note on d.go?|"
	if out != want {
		t.Fatalf("got  %q\nwant %q", out, want)
	}
}

func TestViewerFootChipKeepsTheBackslash(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "viewer.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if strings.Contains(src, `>ctrl+\ open files<`) || !strings.Contains(src, `>ctrl+\\ open files<`) {
		t.Error(`viewer.js: the "ctrl+\ open files" chip loses its backslash in the template literal`)
	}
}

// The tree (spec 2026-10-19-documents-tree §6, the TUI's plan 1): every
// overview collapsed in the list's order, then Notes (noted files no
// overview names) and Opened (the rest); an empty group is not drawn.
func TestDocTreeCollapsedAndExpandedJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const files = [
  {id: "o1", path: "overview-o1.md", source: "overview", title: "Tour", anchors: 4, children: [{path: "a.txt", anchor: 0}, {path: "c.txt", anchor: 2}]},
  {id: "o2", path: "overview-o2.md", source: "overview", title: "Other", anchors: 1, children: [{path: "a.txt", anchor: 0}]},
  {id: "f1", path: "a.txt", source: "worktree", notes: 1},
  {id: "f2", path: "d.txt", source: "worktree", notes: 2},
  {id: "f3", path: "e.txt", source: "commit", rev: "1a2b3c4d5e"},
];
const show = (ns) => ns.map((n) => n.kind + ":" + (n.kind === "group" ? n.group + "/" + n.count : n.kind === "child" ? n.path + "@" + n.anchor + (n.doc ? "=" + n.doc.id : "") : n.doc.id) + (n.expanded ? "+" : "")).join(" ");
console.log(show(docTree(files, new Set(), "", 0)));
console.log(show(docTree(files, new Set(["ov:o1", "ov:o2", "group:notes", "group:opened"]), "", 0)));
console.log(docTree([files[2]], new Set(), "", 0).map((n) => n.kind + ":" + n.group).join(" "));
const t = docTree(files, new Set(["ov:o1", "group:notes"]), "", 0);
console.log(t.map((n) => n.id + "<" + n.parent).join(" "));
`)
	want := "overview:o1 overview:o2 group:notes/1 group:opened/1\n" +
		"overview:o1+ child:a.txt@0=f1 child:c.txt@2 overview:o2+ child:a.txt@0=f1 group:notes/1+ groupfile:f2 group:opened/1+ groupfile:f3\n" +
		"group:notes\n" +
		"ov:o1< child:o1:a.txt<ov:o1 child:o1:c.txt<ov:o1 ov:o2< group:notes< file:f2<group:notes group:opened<"
	if out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
}

// A filter shows what matches expanded for the filter only — an overview
// with all its children, a child under its overview, a file under its
// group — and never writes the expand set (Review Focus 4).
func TestDocTreeFilterJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const files = [
  {id: "o1", path: "overview-o1.md", source: "overview", title: "Tour", children: [{path: "a.txt", anchor: 0}, {path: "b.txt", anchor: 1}]},
  {id: "f2", path: "d.txt", source: "worktree", notes: 2},
  {id: "f3", path: "e.txt", source: "commit", rev: "1a2b3c4d5e"},
];
const ex = new Set();
const ids = (q) => docTree(files, ex, q, 0).map((n) => n.id + (n.expanded ? "+" : "")).join(" ");
console.log([ids("b.tx"), ids("tour"), ids("e.tx"), ids("zzz") || "-", ex.size].join("|"));
`)
	want := "ov:o1+ child:o1:b.txt|ov:o1+ child:o1:a.txt child:o1:b.txt|group:opened+ file:f3|-|0"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// The cursor keeps its node across a rebuild, else the row now at its old
// index under the same parent, else the parent, else a clamp (Review Focus 1).
func TestTreeIndexJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const n = (id, parent) => ({id, parent});
const before = [n("ov:o1", ""), n("group:notes", ""), n("file:f1", "group:notes"), n("file:f2", "group:notes"), n("file:f3", "group:notes")];
const after = [n("ov:o1", ""), n("group:notes", ""), n("file:f1", "group:notes"), n("file:f3", "group:notes")];
console.log([
  treeIndex(after, "file:f3", "group:notes", 4),  // still there
  treeIndex(after, "file:f2", "group:notes", 3),  // gone: the sibling now at its index
  treeIndex(after, "file:f9", "group:notes", 9),  // gone past the end: the last sibling
  treeIndex([n("ov:o1", ""), n("group:notes", "")], "file:f1", "group:notes", 2), // no sibling left: the parent
  treeIndex([n("ov:o1", "")], "group:x", "", 5),  // nothing: clamp
  treeIndex([], "x", "", 3),
].join(","));
`)
	if out != "3,3,3,1,0,0" {
		t.Fatalf("got %s", out)
	}
}

// What a key does on a node (the TUI's treeKey); under a filter nothing is
// saved: → / enter-on-group do nothing, ← only moves to the parent.
func TestTreeKeyActionJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const ov = {kind: "overview", expanded: false}, ovx = {kind: "overview", expanded: true};
const ch = {kind: "child"}, gr = {kind: "group", expanded: false}, grx = {kind: "group", expanded: true}, gf = {kind: "groupfile"};
const a = (n, k, f) => treeKeyAction(n, k, !!f);
console.log([
  a(ov, "ArrowRight"), a(ovx, "ArrowRight"), a(ov, "ArrowLeft"), a(ovx, "ArrowLeft"), a(ov, "Enter"),
  a(ch, "ArrowRight"), a(ch, "ArrowLeft"), a(ch, "Enter"),
  a(gr, "ArrowRight"), a(grx, "ArrowLeft"), a(gr, "Enter"), a(grx, "Enter"),
  a(gf, "ArrowLeft"), a(gf, "Enter"),
  a(ovx, "ArrowRight", 1), a(ovx, "ArrowLeft", 1), a(ch, "ArrowLeft", 1), a(gr, "Enter", 1), a(ch, "Enter", 1),
].join(" "));
`)
	want := "expand none none collapse open none parent-collapse open-at expand collapse toggle toggle parent-collapse open " +
		"none none parent none open-at"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// A row's text: ▸/▾ on an overview and a group, children and group files
// indented; a child not in the list is its path on the working tree; an
// overview with no children is still an overview row (Review Focus 3).
func TestTreeRowJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const now = Date.parse("2026-01-02T00:00:00Z");
const files = [
  {id: "o1", path: "overview-o1.md", source: "overview", title: "Tour", author: "agent", created: "2026-01-01T22:00:00Z", anchors: 2, children: [{path: "a.txt", anchor: 0}, {path: "b.txt", anchor: 1}]},
  {id: "o2", path: "overview-o2.md", source: "overview", title: "Empty", anchors: 1},
  {id: "f1", path: "a.txt", source: "worktree", line: 3, notes: 1},
  {id: "f3", path: "e.txt", source: "commit", rev: "1a2b3c4d5e"},
];
const rows = docTree(files, new Set(["ov:o1", "group:opened"]), "", now).map((n) => treeRow(n, "f1", now));
console.log(rows.map((r) => r.indent + "|" + r.arrow + "|" + r.mark + "|" + r.name + "|" + r.meta).join("\n"));
console.log(groupCount(1) + "," + groupCount(3));
`)
	want := "0|▾|○|Tour|overview · 2 anchors · agent · 2h\n" +
		"1||●|a.txt|:3  working tree  · 1 note\n" +
		"1||○|b.txt|working tree\n" +
		"0|▸|○|Empty|overview · 1 anchors\n" +
		"0|▾||Opened|1 file\n" +
		"1||○|e.txt|@ 1a2b3c4"
	// groupCount on its own line
	if out != want+"\n1 file,3 files" {
		t.Fatalf("got\n%s\nwant\n%s\n1 file,3 files", out, want)
	}
}

// The files tab acts on the tree's node under the cursor, never on
// sw.files[sw.sel] (the flat list's index is not the tree's), and is named
// "Overviews & notes" (spec §6).
func TestSwitcherFilesTabIsTheTreeJS(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "openfiles.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, bad := range []string{"sw.files[sw.sel]", `data-tab="files">Open files`} {
		if strings.Contains(src, bad) {
			t.Errorf("openfiles.js still has %q", bad)
		}
	}
	for _, want := range []string{`data-tab="files">Overviews &amp; notes`, "currentNode()", `"ArrowRight"`, `"ArrowLeft"`, "Expand", "Collapse"} {
		if !strings.Contains(src, want) {
			t.Errorf("openfiles.js lacks %q", want)
		}
	}
}

// The tab opens on the file this tab shows; when it sits under a collapsed
// overview or group, on that row (final review: it fell to row 0).
func TestLandingIndexJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const files = [
  {id: "o1", path: "overview-o1.md", source: "overview", title: "Tour", children: [{path: "a.txt", anchor: 0}]},
  {id: "f1", path: "a.txt", source: "worktree", notes: 1},
  {id: "f2", path: "d.txt", source: "worktree", notes: 2},
  {id: "f3", path: "e.txt", source: "commit", rev: "1a2b3c4d5e"},
];
const at = (ex, mine) => landingIndex(files, docTree(files, new Set(ex), "", 0), mine, 0);
console.log([at([], "f1"), at(["ov:o1"], "f1"), at([], "f3"), at(["group:opened"], "f3"), at([], "o1"), at([], "zz"), at(["group:notes"], "f3")].join(","));
`)
	if out != "0,1,2,3,0,0,3" {
		t.Fatalf("got %s", out)
	}
}
