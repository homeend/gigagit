# ctrl+\ "Overviews & notes" tree — plan 2 of 2: gg web

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task, inline (this repo forbids implementer subagents; one read-only review subagent at the end). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** gg web's `ctrl+\` switcher tab **Open files** becomes the **Overviews & notes** tree the TUI got in plan 1: overviews on top (collapsed) with the files their anchors use underneath, then **Notes** (noted files no overview names), then **Opened** (the rest); `enter` opens the row's own thing (a child: the overview with that file's first anchor selected, beside the file), `→`/`←` expand and collapse, `x`/`D` unchanged, `/` filters.

**Architecture:** The server adds each overview row's child paths to the page's list (a web-only row type embedding `steer.OpenFile`, so `gg session files`, `reply.schema.json` and the gg-notes plugin are untouched). A pure builder in `openfiles.js` (`docTree`) turns the list, the tab's expand state and the filter into a flat node list; the switcher renders nodes and dispatches its keys on the node kind through one `currentNode()`. `openViewer` takes an `anchor` hint: an overview opens with that anchor selected and — in the split — the right column steered to it, re-steering a split already showing that overview.

**Tech Stack:** Go 1.26 (`internal/web`), vanilla ES modules (`internal/web/static/openfiles.js`, `viewer.js`), node-run pure-JS tests (`runPureJS`).

**Spec:** `docs/superpowers/specs/2026-10-19-documents-tree-design.md` §6 (mock `2026-10-19-documents-tree-mock.txt`); plan 1 `docs/superpowers/plans/2026-10-19-documents-tree-1-tui.md` (the TUI behaviour this mirrors).

## Rulings taken while planning

- **Only session overviews are in the web list.** `followDocs` / `agenttour_http.go` are the only feeders of `source: "overview"` rows; a stored review overview and a permanent overview are page-local on the web (`openStoredOverview`, "never registered with the open-files list"). So the web tree has no `@ <sha7>` child and the wire carries no `rev` — spec §7 forbids changing what is in the list. Cost if wrong: one field and one row form later.
- **Wire:** `type ofRow struct { steer.OpenFile; Children []ofChild \`json:"children,omitempty"\` }`, `type ofChild struct { Path string; Anchor int }` (json `path`, `anchor`). A new `s.pageList(wt) []ofRow` feeds `GET /api/open-files`, `ofAnswer.Files` and `liveMsg.Files`; `steerFiles` keeps `ofList` (flat, §7). Field promotion keeps existing tests' `m.Files[0].ID` compiling.
- **Child identity:** `{path, anchor}`. The page re-finds the anchor in the overview it fetches (`anchorForPath`: first anchor whose resolved path is the child's, else the hint index when in range, else none) — the overview may have changed between the list and the open.
- **Expand state:** a module-level `Map` in `openfiles.js`, for the page's lifetime (survives re-renders and live list changes, not a reload; not shared between tabs). Not keyed by worktree: a hosted repo switch carries the two group keys over (harmless; overview ids are unique). The TUI's is per worktree per gg run.
- **Filter:** the web Files tab gains `/` (the TUI hint has it); same semantics as the TUI: matches shown expanded for the filter only, nothing saved under a filter, `←` only moves to the parent.
- **The `ctrl+\ … open files` chips** (index.html footer, viewer/wtfinder foots, guarded by `TestViewerFootChipKeepsTheBackslash`) stay: they name what the viewer's `ctrl+]` background list is, and the TUI's footers were not renamed either.
- **x / D on a child that is not in the list** (a plain path anchor never opened) say so (`"<path> is not open"` / the existing `"nothing to delete on this row"`), never silent — the TUI deferred minor does not repeat here.

## Global Constraints

- What is in the open-files list, when documents join or leave it, close (`x`) and delete (`D`) semantics, `gg session files` and the `●`/`○` marks do not change (spec §7). `steer.OpenFile`, `reply.schema.json` and `steerFiles` are untouched.
- Overviews keep the list's order; an overview's children are in the order its anchors first name them (path anchor: its path, missing or not; note anchor: its note's path; a gone note or an empty path adds nothing); a path appears under every overview naming it; a document shown as an overview's child is not repeated under Notes or Opened; an empty group is not drawn.
- Page strings are English (no i18n gate on the web). Paths are cut in the middle (`elidePath`), never at the end.
- `./test.sh race` must print `all green` before merge; `gg add <files>` + `git commit -F <msgfile>`; never `git add -A`. Worktree `/work/gigagit/.claude/worktrees/documents-tree-web`, branch `feat/documents-tree-web`.

## Review Focus

1. A live `agentdocs`/`open_files` fan-out while the tree is expanded (a child's note deleted, an overview removed, a file closed in another tab): the cursor stays on the same node, else the sibling now at its index, else its parent — never jumps to row 0 — Task 2 (`treeIndex`) + Task 3 (`switcherOpenFiles`).
2. `x` / `D` on a child whose document is not in the list, and on a group row: a message, never a request with an undefined id — Task 3.
3. An overview with zero children (all anchors gone notes): still an overview row, `→` does nothing harmful, `enter` opens it — Task 2.
4. Filter then clear: the expand state from before the filter is what remains — Task 2.
5. `enter` on a child of the overview the split already shows re-steers the right column to that file (the `same` branch of `openSplit` today keeps the old selection and does not steer) — Task 4.

---

### Task 1: the server lists each overview's child paths

**Files:** Modify `internal/web/agentdocs_follow.go` (`ofRow`, `ofChild`, `overviewChildren`, `pageList`), `internal/web/live.go` (`liveMsg.Files []ofRow`), `internal/web/openfiles_http.go` (`ofAnswer.Files []ofRow`, GET, the open_files fan-out), the agentdocs fan-out in `followDocs`. Test: `internal/web/documents_tree_web_test.go` (new).

**Interfaces — Produces:**

```go
type ofChild struct {
	Path   string `json:"path"`
	Anchor int    `json:"anchor"` // the first anchor naming Path
}
type ofRow struct {
	steer.OpenFile
	Children []ofChild `json:"children,omitempty"` // an overview row's files
}
func overviewChildren(o agentdocs.Overview, notePath func(id string) (string, bool)) []ofChild
func (s *Server) pageList(wt string) []ofRow
```

- [ ] **Step 1: failing test** — `documents_tree_web_test.go`:

```go
// An overview row carries the files its anchors use, in first-named order,
// each with the first anchor naming it: a note anchor by its note's file,
// a gone note and a repeated path adding nothing (spec §6).
func TestPageListGivesAnOverviewRowItsChildPaths(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	wt := s.service().Root()
	for _, p := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(wt, p), []byte("x\ny\nz\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.docs.AddNote(root, "c.txt", []string{"x", "y", "z"}, 1, 1, "s", "", "")
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.docs.AddOverview(root, wt, "Tour",
		"[a](a.txt:1) [b](b.txt:2) [c](note:"+n.ID+") [a again](a.txt:3) [gone](note:t999)")
	if err != nil {
		t.Fatal(err)
	}
	s.followDocs()
	var ov *ofRow
	for _, r := range s.pageList(wt) {
		if r.ID == o.ID {
			ov = &r
		} else if len(r.Children) != 0 {
			t.Fatalf("a %s row has children %+v", r.Source, r.Children)
		}
	}
	if ov == nil {
		t.Fatal("the overview is not listed")
	}
	want := []ofChild{{"a.txt", 0}, {"b.txt", 1}, {"c.txt", 2}}
	if !reflect.DeepEqual(ov.Children, want) {
		t.Fatalf("children = %+v, want %+v", ov.Children, want)
	}
	if got := s.ofList(wt); len(got) == 0 { // gg session files stays flat: the steer rows carry no children field
		t.Fatal("ofList is empty")
	}
	b, _ := json.Marshal(s.pageList(wt))
	if !strings.Contains(string(b), `"children":[{"path":"a.txt","anchor":0}`) || !strings.Contains(string(b), `"id":"`+o.ID+`"`) {
		t.Fatalf("wire = %s", b)
	}
}
```

  (Imports: `encoding/json`, `os`, `path/filepath`, `reflect`, `strings`, `testing`. If `AddNote`'s refusal rules want the lines to match the file, keep the file content and the note's `lines` equal as above.)

  Also extend `TestFollowPassTellsTheTabsWhichOverviewClosed`'s add event check: the overview text there is `"x"` (no anchors) — assert `m.Files[0].Children == nil` stays true (no field change needed; the type change must compile).

- [ ] **Step 2: run** `go test ./internal/web -run 'TestPageListGivesAnOverviewRowItsChildPaths|TestFollowPass' -count=1` — FAIL (undefined: `ofRow`, `pageList`).
- [ ] **Step 3: implement** in `agentdocs_follow.go`:

```go
// ofRow is a row of the PAGE's list: the steer row plus, on an overview, the
// files its anchors use (spec 2026-10-19-documents-tree §6) — the page builds
// its tree from them. gg session files keeps the flat steer rows (§7).
type ofRow struct {
	steer.OpenFile
	Children []ofChild `json:"children,omitempty"`
}

// ofChild is one file an overview uses and the first anchor naming it.
type ofChild struct {
	Path   string `json:"path"`
	Anchor int    `json:"anchor"`
}

// overviewChildren is one child per distinct path o's anchors name, in the
// order they first name it: a path anchor's path (missing or not), a note
// anchor's note's path (notePath; a gone note names nothing).
func overviewChildren(o agentdocs.Overview, notePath func(id string) (string, bool)) []ofChild {
	seen := map[string]bool{}
	var out []ofChild
	for i, a := range o.Anchors {
		p := a.Path
		if a.Note != "" {
			p = ""
			if np, ok := notePath(a.Note); ok {
				p = np
			}
		}
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, ofChild{Path: p, Anchor: i})
	}
	return out
}

// pageList is ofList for the page: each overview row with its children.
func (s *Server) pageList(wt string) []ofRow {
	l := s.ofList(wt)
	root := s.docsRoot(context.Background())
	notePath := func(id string) (string, bool) {
		n, ok := s.docs.FindNoteIn(root, id)
		return n.Path, ok
	}
	out := make([]ofRow, len(l))
	for i, f := range l {
		out[i].OpenFile = f
		if f.Source == "overview" {
			if o, ok := s.docs.OverviewIn(root, f.ID); ok {
				out[i].Children = overviewChildren(o, notePath)
			}
		}
	}
	return out
}
```

  Then switch the page's three feeders to it: `liveMsg.Files []ofRow` (live.go:66), `ofAnswer.Files []ofRow` (openfiles_http.go:43), `handleOpenFilesGet` → `s.pageList(...)`, `ans.Files = s.pageList(wt)` (openfiles_http.go:152), both fan-outs (`openfiles_http.go:172`, `agentdocs_follow.go:129`) → `s.pageList(wt)`. Any other compile error from the type change: convert at the call site with `pageList`, never touch `steerFiles`.
- [ ] **Step 4: run** `go test ./internal/web -count=1` — PASS (whole package: the type change touches the live and open-files tests).
- [ ] **Step 5: commit** `web: an overview row lists the files its anchors use`.

### Task 2: the pure tree builder in openfiles.js

**Files:** Modify `internal/web/static/openfiles.js` (inside the pure block `// --- switcher model (pure; guarded against Go) ---` … `// --- end switcher model ---`). Test: `internal/web/openfilesjs_test.go`.

**Interfaces — Consumes:** a list row `f` = `{id, path, source, title, rev, line, state, notes, author, created, anchors, children?: [{path, anchor}]}`.

**Produces** (all pure, in the block):

```js
// node: {kind: "overview"|"child"|"group"|"groupfile", id, parent, key, doc, ov, path, anchor, group, count, expanded}
//   id      stable identity: "ov:<id>", "child:<ovid>:<path>", "group:notes|opened", "file:<id>"
//   parent  the parent node's id ("" at the top)
//   key     the expand key of an overview / group ("ov:<id>" / "group:<g>"), else ""
//   doc     the list row the node acts on (overview, group file, an open child) or null
//   ov      a child's overview row
function docTree(files, expanded /* Set of keys */, query /* "" = none */, now)
function treeIndex(nodes, id, parent, old) // the cursor after a rebuild
function treeKeyAction(node, key, filtered) // "expand"|"collapse"|"parent"|"parent-collapse"|"toggle"|"open"|"open-at"|"none"
function treeRow(node, mine, now) // {indent, arrow, mark, name, meta}
function groupCount(n) // "1 file" / "N files"
```

- [ ] **Step 1: failing tests** — add to `openfilesjs_test.go`:

```go
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
```

  (`treeRow`'s meta for a non-overview doc is the flat row's: `line + "  " + versionLabel + notes`. `versionLabel` lives in viewer.js and the pure block cannot import — add a pure `verLabel(src, rev)` in the block with viewer.js's `versionLabel` wording (read `viewer.js:47` and copy its cases: `worktree` → `working tree`, `commit` → `@ <rev7>`, `shelf` → `shelf`, …) and have `renderSwitcher` keep calling viewer's `versionLabel` only where it already does, or switch it to `verLabel` — one wording either way. Adjust the expected strings above to the exact wording `versionLabel` gives if it differs; the TUI's are `working tree` and `@ <sha7>`.)

- [ ] **Step 2: run** `go test ./internal/web -run 'TestDocTree|TestTreeIndex|TestTreeKeyAction|TestTreeRow' -count=1` — FAIL (`docTree is not defined`).
- [ ] **Step 3: implement** in the pure block:

```js
// --- the Overviews & notes tree (spec 2026-10-19-documents-tree §6) ---
// docTree is the tab's rows: every overview (the list's order) with — when
// expanded — one child per file its anchors use (the server's children),
// then the Notes group (noted working-tree files no overview names) and
// the Opened group (the rest); a group with nothing in it is not drawn. A
// child's document is the list's working-tree row of its path (or null);
// a document shown as a child is not repeated in a group. A filter shows
// what matches expanded for the filter only (expanded is never written).
function docTree(files, expanded, query, now) {
  const q = (query || "").toLowerCase();
  const hit = (s) => !q || String(s).toLowerCase().includes(q);
  const wtDoc = new Map();
  for (const f of files) if (f.source === "worktree") wtDoc.set(f.path, f);
  const claimed = new Set();
  const out = [];
  for (const ov of files) {
    if (ov.source !== "overview") continue;
    const key = "ov:" + ov.id;
    const kids = (ov.children || []).map((c) => {
      const doc = wtDoc.get(c.path) || null;
      if (doc) claimed.add(doc.id);
      return { kind: "child", id: "child:" + ov.id + ":" + c.path, parent: key, key: "", doc, ov, path: c.path, anchor: c.anchor, group: "", count: 0, expanded: false };
    });
    const self = hit((ov.title || ov.path) + " " + overviewRowMeta(ov, now));
    const shown = q ? (self ? kids : kids.filter((k) => hit(k.path))) : kids;
    if (q && !self && !shown.length) continue;
    const open = q ? shown.length > 0 : expanded.has(key);
    out.push({ kind: "overview", id: key, parent: "", key, doc: ov, ov: null, path: "", anchor: -1, group: "", count: 0, expanded: open });
    if (open) out.push(...shown);
  }
  const groups = { notes: [], opened: [] };
  for (const f of files) {
    if (f.source === "overview" || claimed.has(f.id)) continue;
    groups[f.source === "worktree" && f.notes > 0 ? "notes" : "opened"].push(f);
  }
  for (const g of ["notes", "opened"]) {
    const docs = groups[g];
    if (!docs.length) continue;
    const key = "group:" + g;
    const label = g === "notes" ? "Notes" : "Opened";
    const shown = q ? (hit(label) ? docs : docs.filter((f) => hit(f.path + " " + verLabel(f.source, f.rev)))) : docs;
    if (q && !shown.length) continue;
    const open = q ? true : expanded.has(key);
    out.push({ kind: "group", id: key, parent: "", key, doc: null, ov: null, path: "", anchor: -1, group: g, count: docs.length, expanded: open });
    if (open) for (const f of shown) out.push({ kind: "groupfile", id: "file:" + f.id, parent: key, key: "", doc: f, ov: null, path: f.path, anchor: -1, group: g, count: 0, expanded: false });
  }
  return out;
}

// treeIndex is the cursor after a rebuild: node id if still there, else the
// row now at its old index under the same parent (the next sibling after a
// close), else the parent's last child... else the parent, else a clamp.
function treeIndex(nodes, id, parent, old) {
  const i = nodes.findIndex((n) => n.id === id);
  if (i >= 0) return i;
  if (parent) {
    const sibs = [];
    nodes.forEach((n, j) => { if (n.parent === parent) sibs.push(j); });
    const p = nodes.findIndex((n) => n.id === parent);
    if (sibs.length) {
      const at = sibs.find((j) => j >= old);
      return at !== undefined ? at : sibs[sibs.length - 1];
    }
    if (p >= 0) return p;
  }
  return clampSel(old, nodes.length);
}

// treeKeyAction is what a key does on node (the TUI's treeKey). Under a
// filter nothing is saved: → and enter on a group do nothing, ← only moves
// to the parent.
function treeKeyAction(node, key, filtered) {
  const top = node.kind === "overview" || node.kind === "group";
  if (key === "Enter") {
    if (node.kind === "group") return filtered ? "none" : "toggle";
    return node.kind === "child" ? "open-at" : "open";
  }
  if (key === "ArrowRight") return top && !node.expanded && !filtered ? "expand" : "none";
  if (key === "ArrowLeft") {
    if (top) return node.expanded && !filtered ? "collapse" : "none";
    return filtered ? "parent" : "parent-collapse";
  }
  return "none";
}

function groupCount(n) {
  return n === 1 ? "1 file" : n + " files";
}

// treeRow is a node's row: indent (0 top, 1 under a parent), arrow (▸ / ▾
// on an overview and a group), the ●/○ mark (mine = the file this tab
// shows), name and meta (the flat list's).
function treeRow(n, mine, now) {
  const arrow = n.kind === "overview" || n.kind === "group" ? (n.expanded ? "▾" : "▸") : "";
  if (n.kind === "group") return { indent: 0, arrow, mark: "", name: n.group === "notes" ? "Notes" : "Opened", meta: groupCount(n.count) };
  const indent = n.kind === "overview" ? 0 : 1;
  if (!n.doc) return { indent, arrow, mark: "○", name: n.path, meta: verLabel("worktree", "") };
  const r = switcherRows([n.doc], mine, now)[0];
  const meta = r.source === "overview" ? r.meta : (r.line ? r.line + "  " : "") + verLabel(r.source, r.rev) + (r.notes ? "  · " + r.notes + (r.notes === 1 ? " note" : " notes") : "");
  return { indent, arrow, mark: r.mark, name: r.path, meta };
}
```

  plus `verLabel(src, rev)` copied from `viewer.js`'s `versionLabel` wording. Fix the expected strings in the tests only if `versionLabel`'s wording differs from the TUI's (`working tree`, `@ <sha7>`) — the test asserts the page's own wording.
- [ ] **Step 4: run** the same command — PASS; then `go test ./internal/web -run 'JS$' -count=1` (every pure guard) — PASS.
- [ ] **Step 5: commit** `web: the Overviews & notes tree builder (pure, node-tested)`.

### Task 3: the switcher renders the tree and its keys

**Files:** Modify `internal/web/static/openfiles.js` (outside the pure block), `internal/web/static/style.css` only if the indent/arrow need a rule (`.ofrow .tw` arrow cell, `.ofrow.kid` indent). Test: a source guard in `openfilesjs_test.go` + the browser check.

Every place that reads `sw.files[sw.sel]` today goes through `currentNode()` on the files tab — list to change, none missed: the row `click` handler, the `contextmenu` handler, `showTab`'s `first`, `switcherOpenFiles`, `currentCount`, `activate`, `bringBack`, `deleteSelected`, `closeSelected`, `startFilter` (agents only today), the typing branch's clamp, `counts()` (N = `sw.files.length`, the documents in the list — unchanged).

- [ ] **Step 1: failing guard test** (the wiring is DOM; the guard pins that no handler reads the flat list by the cursor any more and that the label changed):

```go
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
```

- [ ] **Step 2: run** `go test ./internal/web -run TestSwitcherFilesTabIsTheTreeJS -count=1` — FAIL.
- [ ] **Step 3: implement:**
  - State: `const expandedNodes = new Set();` (module level; ruling above). `sw.nodes = []` rebuilt by `fileNodes()` = `docTree(sw.files, expandedNodes, sw.tab === "files" ? sw.query : "", Date.now())`; `currentNode()` = `sw.tab === "files" ? sw.nodes[sw.sel] : null`.
  - Tab span: `<span data-tab="files">Overviews &amp; notes <i></i></span>`.
  - `renderSwitcher` files branch: `sw.nodes = fileNodes()`; for each node `treeRow(n, viewerFileId(), now)` → `<div class="ofrow${sel}${r.indent ? " kid" : ""}" data-i="${i}" title="${esc(r.name)}"><span class="tw" data-tw="${i}">${r.arrow || " "}</span> ${r.mark ? r.mark + " " : ""}${esc(elided name)}<span class="ofmeta">${esc((r.indent ? "" : "  ") + r.meta)}</span></div>`; the indent is 4 characters (`&nbsp;` × 4 or the `.kid` padding — one CSS rule `#openfiles-list .ofrow.kid { padding-left: 4ch; }`), the name elided in the middle to `cols - 2 - meta.length - indent*4`. Empty: `no overviews, notes or open files — view file on a file row opens one` (a filter: `nothing matches`).
  - `showTab("files")`: `sw.nodes = fileNodes()`; `first` = index of the node whose `doc.id === viewerFileId()`, else 0.
  - `switcherOpenFiles(files)`: on the files tab remember `cur = currentNode()` (id, parent) and the old `sel` before replacing `sw.files`, rebuild `sw.nodes`, `sw.sel = treeIndex(sw.nodes, cur.id, cur.parent, oldSel)`.
  - `currentCount()`: files tab → `sw.nodes.length`.
  - Keys (files tab, not typing), before the shared switch: `ArrowRight`/`ArrowLeft`/`Enter` → `treeAct(e.key)`; `treeAct(key)` takes `treeKeyAction(n, key, !!sw.query)`: `expand` / `collapse` / `toggle` → add/delete `n.key` in `expandedNodes`, rebuild, keep the cursor on `n.id`; `parent` → cursor to `n.parent`; `parent-collapse` → delete the parent's key, cursor to the parent; `open` → `bringBack(n.doc)`; `open-at` → `openAt(n)` (Task 4 wires the anchor; in this task: `closeSwitcher(); openViewer({ id: n.ov.id })`).
  - `/` on the files tab: `startFilter` accepts `files` too; typing rebuilds the tree each key and clamps to `sw.nodes.length`; `Escape` while typing clears the query (the tree's own expand state shows again).
  - `bringBack(f)` / `closeSelected()` / `deleteSelected()` take the node's `doc`: `const n = currentNode(); const f = n && n.doc;` — a group row: `x` / `D` say `"a group has nothing to close"` / `"nothing to delete on this row"`; a child with no doc: `x` says `n.path + " is not open"`, `D` says `"nothing to delete on this row"`. `closeSelected` after the answer: `const cur = currentNode(), old = sw.sel; sw.files = ans.files || []; sw.nodes = fileNodes(); sw.sel = treeIndex(sw.nodes, cur.id, cur.parent, old)` (the next sibling, plan 1's ruling).
  - Click: a click on `.tw` of an overview/group row toggles it (not under a filter); elsewhere on a row: select + `activate()` (enter's action through `treeAct("Enter")`).
  - Row menu (`contextmenu`, files tab): select the row; overview: `open`, `Expand`/`Collapse` (by `n.expanded`; none under a filter), sep, `Delete overview`; group: `Expand`/`Collapse` only; child: `open beside the overview` (→ `openAt`), and when it has a doc `close in every tab` + `Delete all notes on this file` (deleteAction); group file: today's items.
  - `FOOT_FILES`: `<span>↑↓ j k move</span><button data-oact="enter">enter open</button><span>→ ← expand/collapse</span><button data-oact="x">x close</button><button data-oact="D">D delete</button><button data-oact="filter">/ filter</button><button data-oact="tab">tab next tab</button><button data-oact="esc">esc close</button>`; the foot's `filter` handler works on the files tab too.
  - `registerHelp` text: `ctrl+\` opens the switcher on its **Overviews & notes** tab, a tree: every overview (collapsed) with the files its links use underneath, then **Notes** (noted files no overview names) and **Opened** (the rest) — `→`/`←` expand and collapse, `enter` opens the row (a file under an overview opens the overview beside it at that file's first link; a group toggles), `x` closes, `D` deletes, `/` filters; right-click a row for the same. Keep the existing sentences about `●`, background documents and the author/age.
  - The header comment at the top of the file: "Open files" → "Overviews & notes" with the tree in one sentence.
- [ ] **Step 4: run** `go test ./internal/web -run 'JS$' -count=1` — PASS; `node --check internal/web/static/openfiles.js` — no output.
- [ ] **Step 5: commit** `web: ctrl+\ shows the Overviews & notes tree`.

### Task 4: enter on an overview's file opens the overview at that file

**Files:** Modify `internal/web/static/viewer.js` (pure overview block: `anchorForPath`; `openViewer`'s `anchor` option; `showOverview`; `openSplit`'s `pick`), `internal/web/static/openfiles.js` (`openAt`). Test: `internal/web/overviewsplitjs_test.go` (or the file whose markers are `// --- overview model (pure; guarded against Go) ---` / `// --- end overview model ---`), plus a source guard.

**Interfaces — Produces:**

```js
function anchorForPath(anchors, path, hint) // index of the first anchor whose path is path, else hint when in range, else -1
// openViewer({ id, anchor: {path, index} }) — an overview opened with that anchor selected (and steered in the split)
```

- [ ] **Step 1: failing tests:**

```go
// enter on an overview's file opens the overview at the first anchor that
// names the file in the overview as fetched (it may have changed since the
// list), else the list's index when still in range, else none.
func TestAnchorForPathJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", voPureStart, voPureEnd, `
const as = [{dest: "a.txt:1", path: "a.txt"}, {dest: "note:n1", note: "n1", path: "c.txt"}, {dest: "a.txt:3", path: "a.txt"}, {dest: "b.txt", path: "b.txt"}];
console.log([anchorForPath(as, "a.txt", 0), anchorForPath(as, "c.txt", 2), anchorForPath(as, "b.txt", 0),
  anchorForPath(as, "z.txt", 2), anchorForPath(as, "z.txt", 9), anchorForPath([], "a.txt", 0)].join(","));
`)
	if out != "0,1,3,2,-1,-1" {
		t.Fatalf("got %s", out)
	}
}

// The switcher's child opens through openViewer's anchor option, and a
// split already showing that overview re-steers to the picked anchor
// (Review Focus 5) — the wiring is DOM; this pins it.
func TestOpenAtAnchorIsWiredJS(t *testing.T) {
	t.Parallel()
	vb, _ := os.ReadFile(filepath.Join("static", "viewer.js"))
	ob, _ := os.ReadFile(filepath.Join("static", "openfiles.js"))
	for _, want := range []string{"anchor = null", "anchorForPath(", "pick"} {
		if !strings.Contains(string(vb), want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	if !strings.Contains(string(ob), "anchor: { path: n.path, index: n.anchor }") {
		t.Error("openfiles.js: a child does not open its overview at its anchor")
	}
}
```

  (`voPureStart` / `voPureEnd` are declared in `viewernotesjs_test.go`.)
- [ ] **Step 2: run** `go test ./internal/web -run 'TestAnchorForPathJS|TestOpenAtAnchorIsWiredJS' -count=1` — FAIL.
- [ ] **Step 3: implement:**
  - Pure block (`viewer.js` overview model):

```js
// anchorForPath is the anchor a file under an overview opens at (the
// switcher's tree): the first anchor whose path — a note anchor's is its
// note's, filled by the server — is path; else hint (the list's index,
// read before this fetch) when in range; else -1 (none selected).
function anchorForPath(anchors, path, hint) {
  const i = anchors.findIndex((a) => a.path === path);
  if (i >= 0) return i;
  return hint >= 0 && hint < anchors.length ? hint : -1;
}
```

  - `openViewer({ …, anchor = null })`: pass `anchor` to `showOverview(f, ov, ovMine, anchor)`.
  - `showOverview(f, ov, mine, anchor = null)`: `const pick = anchor ? anchorForPath(ov.anchors || [], anchor.path, anchor.index) : -1;` split: `return openSplit(f.id, f.path, { ...ov, sel: pick, want: pick >= 0 ? ov.anchors[pick].dest : "" }, { pick: pick >= 0 })`; document mode: `view.ov = { ...ov, sel: pick, want: pick >= 0 ? ov.anchors[pick].dest : "" }` and after `renderViewer()` `if (pick >= 0) selectAnchor(pick)` (it scrolls the anchor into view; the place's scrollTop is not restored then).
  - `openSplit(id, path, ov, { steer = true, pick = false } = {})`: in the `same` branch, when `pick`: `split.ov.sel = ov.sel; split.ov.want = ov.want;` and after the paint `steerSplit()` (the `same` branch steers only when picked); the new-split branch already takes `ov.sel >= 0` and steers.
  - `openfiles.js`: `function openAt(n) { closeSwitcher(); openViewer({ id: n.ov.id, anchor: { path: n.path, index: n.anchor } }); }` — used by `treeAct`'s `open-at` and the child's row menu.
- [ ] **Step 4: run** `go test ./internal/web -count=1` — PASS; `node --check` both files.
- [ ] **Step 5: commit** `web: enter on an overview's file opens the overview beside it`.

### Task 5: docs, binaries, browser check, race gate

**Files:** `CHANGELOG.md` (the 2026-10-19 "Overviews & notes" entry: replace "gg web keeps its flat Open files tab for now (plan 2)" with the web paragraph), `README.md` (~1661 tree paragraph: "gg web's switcher has the same tree"; ~1748 "its Open files tab lists each one" → "its Overviews & notes tab"), `docs/CLAUDE-details.md` (§ "ctrl+\ Overviews & notes tree": a gg web paragraph — the wire `ofRow.Children`, `pageList` vs `ofList`, the pure `docTree`/`treeIndex`/`treeKeyAction`, `anchorForPath`, the rulings above), the memory note `documents-tree-feature.md` (plan 2 status).

- [ ] **Step 1:** edit the docs.
- [ ] **Step 2: browser check** — try Claude-in-Chrome (`tabs_context_mcp`); with it: `./build.sh all` in the worktree, run `./gg web` on a scratch repo with a session overview naming two files (one noted) + one opened commit file, open the page, `ctrl+\`, see the tab "Overviews & notes", `→` shows the children, `enter` on a child opens the split with that file on the right and the anchor highlighted on the left; `x` on a group file keeps the cursor in the group. Without it: an HTTP probe (python `urllib`) of `GET /api/open-files` showing `children` on the overview row, and say plainly the page was not looked at.
- [ ] **Step 3: run** `./test.sh race > <workspace>/race.log 2>&1; tail -5 <workspace>/race.log` — Expected: `all green`.
- [ ] **Step 4: commit** `docs: the web's Overviews & notes tree`.
