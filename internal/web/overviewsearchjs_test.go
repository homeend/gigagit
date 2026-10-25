package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The TUI searches every overview surface with / @ ] [ (previewSearchKey):
// an overview opened on its own (file_viewer.go), the split's left column
// (split_overview.go) and the split's stacked file column
// (split_stack_pane.go). The page had none of the three — / in the stack even
// said "this acts on a single file". The overview is painted markdown, so its
// search is the help overlay's kind (rows = the document's text blocks, hits
// laid over their text nodes); the stack is one stream of every header and
// shown line across its files, painted near the screen (paintwin.js).
func TestOverviewSurfacesSearch(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	fnBody := func(src, sig string) string {
		i := strings.Index(src, sig)
		if i < 0 {
			t.Fatalf("%s is gone", sig)
		}
		body := src[i:]
		if j := strings.Index(body, "\n}\n"); j > 0 {
			body = body[:j]
		}
		return body
	}
	checks := []struct{ file, want, why string }{
		{"docsearch.js", `function docSearchHost(`, "one search host over a painted markdown document, shared by both overview surfaces"},
		{"docsearch.js", `export function paintRow(`, "the hit painter over a row's text nodes lives with the document host"},
		{"helpsearch.js", `import { paintRow } from "./docsearch.js";`, "the help overlay paints with the same painter"},
		{"viewer.js", `<div id="viewer-ov-search"`, "the left column has its own bar (the TUI's is in the left title row)"},
		{"viewer.js", `bindSearchBar("viewer-ov-search"`, "the left column's bar is wired to its own Search"},
		{"viewer.js", `e.target === $("viewer-ov-search-input")`, "a query typed on the left must not steer the overview's links"},
		{"style.css", `#viewer-ov-body .hit`, "hits on the left column are tinted (hit rules are selector-scoped)"},
		{"style.css", `#viewer-ov-search.hidden`, "the left bar hides (no global .hidden rule)"},
		{"viewer.js", `windowPainter({`, "the stack paints hits only near the screen: it holds whole files"},
		{"viewer.js", `stackStream(`, "the stack searches one stream across its files, the TUI's fileStream"},
		{"searchbar.js", `in an open file or an overview`, "the ? help names the overview surfaces"},
		{"viewer.js", `if (viewerSearch.active()) pumpStack(st);`, "a search reads the files not read yet (the TUI's stack reads them all)"},
		{"viewer.js", `if (k < 0 && viewerSearch.active()) k = nextUnreadSlot(st);`, "…beyond the files near the screen"},
		{"viewer.js", `refindStack(true); // the stream changed`, "a file read, folded or unfolded re-finds (the TUI's rebuild)"},
		{"viewer.js", `(split ? ovDocSearch : viewerDoc).leaveHit();`, "a link step makes the link the reader's place again"},
		{"searchbar.js", `host.cursorLed && host.cursorLed() ? host.here() : s.pos(host.here())`, "] / [ step from the cursor once it left the hit (the TUI's searchPos)"},
		{"viewer.js", `if (h && h.row === i) return hitPos(h);`, "…the stack's cursor on the current hit steps off it"},
		{"viewer.js", `stackPaint.markPainted(i);`, "a section a rebuild kept still carries its paint: the painter must clean it later"},
		{"viewer.js", `unpaintStack();`, "a cleared search leaves no tint in the stack"},
		{"viewer.js", `if (view.ov) viewerSearchBar.reset();`, "a standalone overview's query does not follow into a split"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	v := read("viewer.js")
	// The search key goes FIRST: each handler takes esc straight to
	// closeViewer, and a kept query's esc must clear it instead.
	for _, h := range []struct{ sig, call string }{
		{"function overviewKey(e) {", "if (viewerSearchKey(e)) return true;"},
		{"function splitOverviewKey(e) {", "if (ovSearchKey(e)) return true;"},
		{"function stackKey(e) {", "if (viewerSearchKey(e)) {"},
	} {
		body := fnBody(v, h.sig)
		sw := strings.Index(body, "switch (e.key)")
		at := strings.Index(body, h.call)
		if at < 0 || (sw >= 0 && at > sw) {
			t.Errorf("%s: %q must run before its switch", h.sig, h.call)
		}
	}
	if strings.Contains(fnBody(v, "function stackKeyMove(st, e) {"), `case "/": case "@":`) {
		t.Error(`stackKeyMove still answers / and @ with "this acts on a single file"`)
	}
	// Each footer that the TUI gives [/] find names it too.
	foot := fnBody(v, "function viewerFoot() {")
	if n := strings.Count(foot, "/ find"); n < 5 {
		t.Errorf("viewerFoot names / find %d times; want every variant (split left, stack, split file, overview, stored overview, file)", n)
	}
}
