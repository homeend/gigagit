package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const viewerPureStart = "// --- viewer model (pure; guarded against Go) ---"
const viewerPureEnd = "// --- end viewer model ---"

// runPureJS runs driver under node with the guarded section [start, end) of
// static/<file> in scope, and returns its trimmed stdout. Skips without node.
func runPureJS(t *testing.T, file, start, end, driver string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", file))
	if err != nil {
		t.Fatal(err)
	}
	i, j := strings.Index(string(src), start), strings.Index(string(src), end)
	if i < 0 || j < i {
		t.Fatalf("%s: the guarded section markers are gone (%q / %q)", file, start, end)
	}
	script := filepath.Join(t.TempDir(), "run.mjs")
	if err := os.WriteFile(script, []byte(string(src)[i:j]+"\n"+driver), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, script).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestViewerModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", viewerPureStart, viewerPureEnd, `
const r = [];
r.push(clampLine(5, 0), clampLine(0, 3), clampLine(9, 3), clampLine(2, 3));
r.push(JSON.stringify(landLine(0, 3, "a.txt")), JSON.stringify(landLine(7, 3, "a.txt")), JSON.stringify(landLine(2, 3, "a.txt")));
r.push(sameLines([{text:"a"}],[{text:"a"}]), sameLines([{text:"a"}],[{text:"b"}]), sameLines([],[{text:""}]));
r.push(versionLabel("worktree",""), versionLabel("commit","0123456789"), versionLabel("shelf","x"));
console.log(r.join("|"));
`)
	want := `0|1|3|2|{"line":1,"notice":""}|{"line":3,"notice":"line 7 is past the end of a.txt (3 lines)"}|{"line":2,"notice":""}|true|false|false|working tree|@ 0123456|shelf`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestViewerPlaceJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", viewerPureStart, viewerPureEnd, `
const r = [];
r.push(pickLine(7, {cur: 3}, 5), pickLine(0, {cur: 3}, 5), pickLine(0, null, 5), pickLine(0, null, 0), pickLine(0, {cur: 0}, 4));
r.push(keepLine(9, 4, false), keepLine(9, 0, true), keepLine(0, 4, false), keepLine(2, 4, false));
console.log(r.join("|"));
`)
	if want := "7|3|5|0|4|4|9|1|2"; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestViewerJSIsWired(t *testing.T) {
	t.Parallel()
	read := func(n string) string {
		b, err := os.ReadFile(filepath.Join("static", n))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, c := range viewerWiring {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}

// viewerWiring grows task by task: each entry pins code that exists only once
// the step it names is done.
var viewerWiring = []struct{ file, want, why string }{
	{"app.js", "./viewer.js", "the module must be imported at boot"},
	{"viewer.js", `mountOverlay("viewer")`, "the overlay is a mounted layer"},
	{"viewer.js", `pushLayer("viewer"`, "it rides the layer stack"},
	{"viewer.js", "/api/file-content", "it reads through the content endpoint"},
	{"viewer.js", "bindSearchBar(", "it has the in-view search"},
	{"viewer.js", "renderCell(", "lines paint through the shared cell renderer"},
	{"viewer.js", `$("foot")`, "the keys go in the bottom bar"},
	{"viewer.js", "elidePath(", "the title cuts the path in the middle"},
	{"style.css", "#viewer", "the overlay is styled"},
	// Task 5: the . menu.
	{"viewer.js", "copy file link", "the menu copies the content link at the cursor line"},
	{"viewer.js", "copy line", "the menu copies the cursor line's text"},
	{"viewer.js", "sameLines(disk", "a commit/shelf version copies a content link only when the disk matches"},
	{"viewer.js", "openFileHistory(", "the menu opens the file's history"},
	{"viewer.js", "openFileBlame(", "the menu opens blame"},
	{"viewer.js", "openCommitByHash(", "a commit version's diff is that commit's change"},
	{"links.js", "copyFileLink,", "the working-tree presence check is shared, not copied"},
	// Task 6: entry points.
	{"viewer.js", `registerRows("fileview"`, "file rows offer view file"},
	{"files.js", `extraRows("fileview"`, "view file sits with history and blame, not after the discard rows"},
	{"viewer.js", `registerRows("shelf"`, "shelved files offer view file"},
	{"viewer.js", `label: "view file"`, "the row is named view file"},
	{"viewer.js", "registerHelp(", "the ? overlay lists the viewer"},
	{"files.js", "deleted: ", "a file with no bytes offers no view file row"},
	// Task 7: landing.
	{"live.js", `s.hint_kind === "view"`, "a content navigate lands in the viewer"},
	{"live.js", "openViewer(", "the steered landing opens the viewer"},
	{"live.js", "/api/link-command", "a pasted gg:// link resolves on the server"},
	{"commits.js", "gotoLink(", "# accepts a pasted gg:// link"},
	// The # keystroke itself must not land in the prompt it opens: the field
	// read "#gg://…" and a pasted link went to the rev resolver (a 404).
	{"keys.js", "} else if (e.key === \"#\") {\n    e.preventDefault();", "# must not type itself into the goto prompt"},
	// Plan 5b Task 4: the list.
	{"core.js", "tabId", "a page load names itself to the server"},
	{"live.js", `"/api/events?tab="`, "the event stream carries the tab id"},
	{"layers.js", "function pushFoot(", "the footer chips stack (viewer + switcher)"},
	{"viewer.js", `pushFoot("viewer"`, "the viewer's chips ride the footer stack"},
	{"viewer.js", `"/api/open-files"`, "the viewer registers what it shows"},
	{"viewer.js", `op: "background"`, "ctrl+] and the menu hand-offs background the file"},
	{"viewer.js", `op: "cursor"`, "the cursor line is reported"},
	{"viewer.js", "closed in another tab", "x in another tab closes this viewer"},
	{"viewer.js", "files open)", "an eviction names the file (the TUI's words)"},
	{"viewer.js", `opLine(name + " is in the background", false)`, "ctrl+] says where the file went (the key hint lives in #foot)"},
	// Plan 5b Task 5: events.
	{"live.js", `msg.reason === "open_files"`, "the list's broadcast reaches the page"},
	{"live.js", "viewerOpenFiles(msg.files || [])", "an empty list arrives as no files (omitempty)"},
	{"live.js", `msg.reason === "file_changed"`, "a disk change reaches the viewer"},
	{"live.js", "viewerFileChanged(msg.file_id)", "the viewer reloads the changed file"},
	{"live.js", "viewerHello()", "every hello re-reports what this tab shows"},
	// Plan 5b Task 6: the switcher.
	{"app.js", "./openfiles.js", "the switcher module is imported at boot"},
	{"openfiles.js", `mountOverlay("openfiles")`, "the switcher is a mounted layer"},
	{"openfiles.js", `pushFoot("openfiles"`, "its keys go in the bottom bar"},
	{"openfiles.js", "elidePath(", "rows cut the path in the middle"},
	{"openfiles.js", "everywhere: true", "x closes the file in every tab"},
	{"openfiles.js", "no open files", "an empty list says so"},
	{"keys.js", "isSwitcherKey(e)", "ctrl+\\ works from the main page"},
	{"keys.js", `case "openfiles":`, "the footer chip opens the switcher"},
	{"index.html", `data-act="openfiles"`, "the main footer advertises ctrl+\\"},
	{"viewer.js", "isSwitcherKey(e)", "ctrl+\\ works from the viewer"},
	{"live.js", "switcherOpenFiles(", "an open switcher follows the list"},
	{"style.css", "#openfiles.hidden", "the overlay hides by id (a global .hidden does not exist)"},
	// Open-files minors.
	{"viewer.js", "if (landed.line !== (f.line || 0)) reportCursor();", "a reopen by place reports the line it landed on"},
	{"ops.js", "function clearOpLine(", "a notice can be taken down when it no longer holds"},
	{"viewer.js", `clearOpLine(f.path + " opened in the background")`, "focusing a background-opened file drops its notice"},
	{"style.css", `#op-line.err #op-head::before { content: "Problem"; }`, "the error head's words are CSS, not text a notice carries"},
	// Agent docs plan 2: overviews.
	{"viewer.js", `"/api/overview?id="`, "an overview reads through its own endpoint"},
	{"viewer.js", "{ anchors: true }", "an overview paints its anchors as links"},
	{"viewer.js", `closest("a.md-anchor")`, "a single click opens an anchor"},
	{"viewer.js", `case "Backspace":`, "backspace goes back to the overview"},
	{"viewer.js", "the overview was closed", "back from a closed overview says so"},
	{"viewer.js", " vrange", "a range anchor highlights its lines"},
	{"viewer.js", `" was closed"`, "an overview closed elsewhere closes this viewer"},
	{"live.js", "viewerAgentDocs(msg.files || [], msg.closed || [], msg.stamps)", "the tabs hear which overview closed, and which moved"},
	// Agent docs minors.
	{"viewer.js", "closeViewer(escHow(!!view.ov, view.notes.length)); // the backdrop is esc", "the backdrop leaves a document as esc does"},
	{"viewer.js", "const name = docName(view);\n  closeViewer(\"background\")", "ctrl+] names an overview by id and title"},
	{"viewer.js", `opLine("back failed: "`, "a failed list fetch on backspace is an error, not a closed overview"},
	{"viewer.js", "overviewStale(stamps, view.id, view.ov.stamp)", "an overview re-fetches only when its stamp moved"},
	{"live.js", "if (msg.evicted) opLine(evictedText(msg.evicted, msg.cap), false)", "every tab names a file the follow pass pushed out"},
	{"viewer.js", "if (reg.evicted) opLine(evictedText(reg.evicted, reg.cap), false)", "an open names the file it pushed out, with the server's cap"},
	{"wtfinder.js", "evictedText(ans.evicted, ans.cap)", "a background open from the finder names the file it pushed out, with the server's cap"},
	{"viewer.js", "if (mine !== ovSeq) return false", "an overview refresh older than the last one started is dropped"},
	{"style.css", ".md-anchor.asel", "the selected anchor is styled"},
	{"style.css", ".md-anchor.agone", "a missing anchor is styled"},
	{"style.css", ".vline.vrange", "the range is tinted"},
	{"viewer.js", "selectAnchor(backAnchor(view.ov.anchors, f))", "back finds the anchor by its destination (a set may have moved it)"},
	{"viewer.js", "dest: a.dest }", "the way back carries the anchor's destination"},
}

// viewerGone pins what the open-files minors removed.
var viewerGone = []struct{ file, want, why string }{
	{"viewer.js", "lists open files", "key hints live in #foot, never in a notice"},
	{"index.html", "<span>Problem</span>", "the hidden head's words leaked into every notice's text"},
}

func TestViewerJSDroppedLeaks(t *testing.T) {
	t.Parallel()
	for _, c := range viewerGone {
		if strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s still has %q: %s", c.file, c.want, c.why)
		}
	}
}
