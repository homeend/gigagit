package web

import (
	"strings"
	"testing"
)

const wtfPureStart = "// --- finder model (pure; guarded against Go) ---"
const wtfPureEnd = "// --- end finder model ---"

func TestFinderModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "wtfinder.js", wtfPureStart, wtfPureEnd, `
const r = [];
r.push(wtTitle(true, "", 0, 0), wtTitle(false, "", 3, 10), wtTitle(false, "ab", 3, 10));
r.push(wtClamp(5, 0), wtClamp(-1, 3), wtClamp(9, 3), wtClamp(1, 3));
r.push(wtWantMore(190, 200, 200, 15), wtWantMore(100, 200, 200, 15), wtWantMore(199, 200, 0, 15), wtWantMore(0, 0, 200, 15));
r.push(wtPathCols(40, false), wtPathCols(40, true), wtPathCols(10, true), wtPathCols(0, false), wtPathCols(5, false), wtPathCols(3, false));
r.push(wtEmpty(""), wtEmpty("x"));
console.log(r.join("|"));
`)
	want := "Files (working tree)  (loading…)|Files (working tree)  10/10|Files (working tree)  3/10|" +
		"0|0|2|1|true|false|false|false|40|27|0|0|5|0|(no files)|(no match)"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestFinderJSIsWired(t *testing.T) {
	t.Parallel()
	for _, c := range finderWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
	for _, c := range finderGone {
		if strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s still has %q: %s", c.file, c.want, c.why)
		}
	}
}

// The chip names ctrl+\ — a bare "\ " in the source would be an escaped
// space and the chip would read "ctrl+ open files" (the 5c bug).
func TestFinderFootChipKeepsTheBackslash(t *testing.T) {
	t.Parallel()
	if !strings.Contains(readStatic(t, "wtfinder.js"), `ctrl+\\ open files`) {
		t.Fatal(`wtfinder.js: the foot chip must spell ctrl+\\ (escaped) — "\ " is an escaped space`)
	}
}
func TestFinderPreviewModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "wtfinder.js", wtfPureStart, wtfPureEnd, `
const r = [];
r.push(WT_SETTLE_MS);
r.push(wtPreviewFresh(3, 3, "a", "a"), wtPreviewFresh(2, 3, "a", "a"), wtPreviewFresh(3, 3, "a", "b"));
r.push(wtPlaceholder({missing: true}, 0), wtPlaceholder({too_large: true}, 0), wtPlaceholder({}, 0), wtPlaceholder({}, 4) === "");
console.log(r.join("|"));
`)
	want := "150|true|false|false|(file deleted on disk)|(file too large to preview)|(empty file)|true"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
// The preview follows the disk: a stamp that moved reloads it; an unknown
// stamp ("" — a failed stat, or none yet) never does.
func TestFinderStampModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "wtfinder.js", wtfPureStart, wtfPureEnd, `
const r = [];
r.push(WT_STAMP_MS);
r.push(wtStampChanged("1:2", "1:3"), wtStampChanged("1:2", "1:2"), wtStampChanged("1:2", "missing"), wtStampChanged("missing", "4:5"));
r.push(wtStampChanged("", "1:2"), wtStampChanged("1:2", ""), wtStampChanged(undefined, "1:2"));
console.log(r.join("|"));
`)
	if want := "1000|true|false|true|true|false|false|false"; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestFinderActionsJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "wtfinder.js", wtfPureStart, wtfPureEnd, `
console.log(wtActions(false).join(",") + "|" + wtActions(true).join(","));
`)
	if want := "view,diff,history,blame,copy|view,copy"; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// finderWiring grows task by task: each entry pins code that exists only once
// the step it names is done.
var finderWiring = []struct{ file, want, why string }{
	// Task 2: the list.
	{"app.js", "./wtfinder.js", "the module must be imported at boot"},
	{"wtfinder.js", `pushLayer("wtf"`, "F rides the layer stack (owns the keyboard, surfaces over it return to it)"},
	{"wtfinder.js", `classList.add("wtf")`, "F shows itself in the panes by a class on #panes"},
	{"wtfinder.js", "/api/worktree-files", "the list comes from the server"},
	{"wtfinder.js", "fresh", "F re-reads the list on open"},
	{"wtfinder.js", "elidePath(", "rows cut the path in the middle"},
	{"wtfinder.js", `pushFoot("wtf"`, "F's keys go in the bottom bar"},
	{"wtfinder.js", `closest("input,textarea")`, "typing F in a field stays a letter"},
	{"wtfinder.js", "registerHelp(", "the ? overlay lists F"},
	{"palette.js", `from "./wtfinder.js"`, "the palette row opens the new F"},
	{"index.html", `data-act="finder"`, "the main footer advertises F"},
	{"style.css", "#panes.wtf.wtf.wtf", "F's grid outranks every layout rule"},
	{"style.css", "#wtf.hidden", "the list hides by id (no global .hidden)"},
	{"style.css", "#wtf-preview.hidden", "the preview hides by id"},
	// Task 3: the preview.
	{"wtfinder.js", "src=worktree&path=", "the preview reads the file on disk"},
	{"wtfinder.js", "renderCell(", "lines paint through the shared cell renderer (syntax colour)"},
	{"wtfinder.js", "setTimeout(() => showPreview(", "the preview waits for the cursor to settle"},
	{"style.css", "#wtf-body .tk-kw", "the preview is syntax-coloured"},
	// Task 4: the actions.
	{"wtfinder.js", "showCtxMenu(", "enter / . / right-click open the actions menu"},
	{"wtfinder.js", `label: "view file"`, "view file opens the viewer over F"},
	{"wtfinder.js", "openFileHistory(f.path", "history opens over F"},
	{"wtfinder.js", "openFileBlame(f.path", "blame opens over F"},
	{"wtfinder.js", "openWorktreeFileDiff(", "diff shares the viewer's helper"},
	{"wtfinder.js", "copyPathRows(", "the copy rows are the file rows' own"},
	{"wtfinder.js", "copyFileLink(", "copy file link goes through the shared presence check"},
	{"wtfinder.js", `tab: ""`, "ctrl+] opens the row in the background (no tab shows it)"},
	{"wtfinder.js", "opened in the background", "ctrl+] says so in 5c's words"},
	{"wtfinder.js", `addEventListener("dblclick"`, "a double-click views the file"},
	{"wtfinder.js", `addEventListener("contextmenu"`, "right-click opens the actions"},
	{"files.js", "copyPathRows,", "the copy rows are shared, not copied"},
	{"viewer.js", "async function openWorktreeFileDiff(", "the diff lookup is shared by the viewer and F"},
	{"viewer.js", "diff (working tree changes)", "the row says what it opens"},
	{"live.js", "closeFinder();", "a steered navigate onto the panes closes F first"},
	// Browser check: hiding a pane's children resets its scroll — esc must put it back.
	{"wtfinder.js", "restorePanes(", "esc gives the panes back at the scroll they had"},
	// Final review: a viewer opened over F that hands off to the diff stage must not leave F covering it.
	{"viewer.js", "closeViewer(\"background\");\n  closeFinder();", "the viewer's diff rows step F aside"},
	// Minors: the preview follows the disk; a resize re-cuts the rows and the title.
	{"wtfinder.js", "/api/file-stamp?path=", "the preview re-stats its file (one stat, no read) while F is up"},
	{"wtfinder.js", "wtStampChanged(previewStamp,", "a moved stamp reloads the preview"},
	{"wtfinder.js", "clearInterval(stampTimer)", "closing F stops the re-stat"},
	{"wtfinder.js", "paintPreview(path, lines, wtPlaceholder(body, lines.length), keep)", "a reload keeps the preview's place"},
	{"wtfinder.js", "new ResizeObserver(", "a width change re-cuts F's rows and title"},
	{"wtfinder.js", "paintPTitle(previewPath)", "a resize re-cuts the preview title from the path"},
}

// finderGone pins what 5d removes.
var finderGone = []struct{ file, want, why string }{
	{"search.js", `mountOverlay("finder")`, "the overlay finder is replaced by F (the TUI deleted its popup)"},
	{"search.js", "/api/files", "the endpoint is gone"},
	{"search.js", `e.key === "F"`, "F belongs to wtfinder.js"},
	{"palette.js", "openFeedFilter, openFinder", "openFinder no longer comes from search.js"},
	{"viewer.js", "diff (HEAD ↔ working tree)", "no /api/diff lane opens HEAD ↔ working tree"},
}
