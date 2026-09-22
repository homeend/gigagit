package web

import (
	"strings"
	"testing"
)

// The stacked view fetches each file through the SAME URL builder the
// single-file view uses, so the two can never disagree on what a row's diff
// is. A regression here is a stack that quietly shows a different diff.
func TestFileDiffURLIsShared(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	if !strings.Contains(files, "function fileDiffURL(f)") {
		t.Fatal("files.js: fileDiffURL is gone")
	}
	if n := strings.Count(files, "getJSON(fileDiffURL("); n < 2 {
		t.Errorf("single-file opens use fileDiffURL %d times, want >= 2 (commit/compare and working tree)", n)
	}
	if strings.Contains(files, `getJSON("/api/diff?" + q)`) {
		t.Error("files.js builds a /api/diff URL inline again — route it through fileDiffURL")
	}
}

// The stack is only reachable if the doors route into it and every exit
// tears it down; each of these lines is one of those doors or exits.
func TestStackViewWired(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	for _, want := range []string{
		"if (stackOn()) return openStack(i);",      // openFile routes into the stack
		`if (mode !== "diff") teardownStack();`,    // leaving the diff stage drops it
		"if (state.stack) return rerenderStack();", // f / w / resize re-render the stack
		"reconcileStack();",                        // a status re-read keeps its sections
	} {
		if !strings.Contains(files, want) {
			t.Errorf("files.js is missing %q", want)
		}
	}
	view := readStatic(t, "stackview.js")
	for _, want := range []string{
		"new IntersectionObserver(",
		"STACK_MAX_IN_FLIGHT",
		`getJSON("/api/numstat?"`,
		"getJSON(fileDiffURL(",
		"state.ui && state.ui.stacked_diff",
		"saveUI({ stacked_diff:",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("stackview.js is missing %q", want)
		}
	}
	css := readStatic(t, "style.css")
	for _, want := range []string{".stk-head", "position: sticky", "var(--diff-head-h", "#stack-fold-all.hidden"} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css is missing %q", want)
		}
	}
	if !strings.Contains(readStatic(t, "app.js"), `from "./stackview.js";`) {
		t.Error("app.js does not load stackview.js")
	}
	// A single-file diff still loading when S builds a stack must not paint
	// over it (found by the working-tree browser probe: click a file, press S
	// before its diff lands).
	if !strings.Contains(files, "// A single diff never paints over a stack") {
		t.Error("renderDiff lost its stack guard")
	}
	if n := strings.Count(files, "if (gen !== state.detailGen) return;"); n < 3 {
		t.Errorf("only %d opens drop a superseded answer, want the commit, working-tree and entry opens", n)
	}
}

func TestStackKeysWired(t *testing.T) {
	t.Parallel()
	keys := readStatic(t, "keys.js")
	for _, want := range []string{
		`e.key === "S"`, "toggleStacked()",
		`e.key === "-"`, "collapseCurrent()",
		`e.key === "_"`, "toggleAllCollapsed()",
		`case "stacked": toggleStacked(); break;`,
		"if (state.stack && state.layout === \"diff\") return openFile(state.fileCursor);",
	} {
		if !strings.Contains(keys, want) {
			t.Errorf("keys.js is missing %q", want)
		}
	}
	if !strings.Contains(readStatic(t, "index.html"), `data-act="stacked"`) {
		t.Error("the footer has no stacked chip (advertise in help AND footer)")
	}
	if !strings.Contains(readStatic(t, "palette.js"), "toggleStacked()") {
		t.Error("the ☰ menu has no stacked-diff row")
	}
}

// The symmetric view's single-file open and its stack read ONE pair builder:
// a flip must never leave the stack diffing the old direction.
func TestSymPairShared(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	sym := readStatic(t, "symcompare.js")
	if !strings.Contains(files, "const p = symPair(f, c);") {
		t.Error("fileDiffURL does not build a symmetric row's URL through symPair")
	}
	if !strings.Contains(sym, "const p = symPair(f, c);") {
		t.Error("openSymRow does not read its pair from symPair")
	}
	if strings.Contains(sym, "function noContentWhy(") || strings.Contains(sym, "const GLYPH =") {
		t.Error("symcompare.js keeps its own noContentWhy / GLYPH — stack.js owns them")
	}
}

// Plan 2: the symmetric view stacks. Each line is a door into it or a path
// that must keep the reader's file across a rebuild.
func TestSymStackWired(t *testing.T) {
	t.Parallel()
	view := readStatic(t, "stackview.js")
	sym := readStatic(t, "symcompare.js")
	files := readStatic(t, "files.js")
	if strings.Contains(view, "symActive()") {
		t.Error("stackview.js still refuses the symmetric view")
	}
	for _, want := range []string{
		"neither set has content for this file", // the no-content body
		`class="stk-sides"`,                     // each side's state in the header
		"if (!st.painted) {",                    // an open before the first paint moves the anchor
		`scrollIntoView({ block: "nearest" })`,  // the list keeps the reader's row in view
	} {
		if !strings.Contains(view, want) {
			t.Errorf("stackview.js is missing %q", want)
		}
	}
	if !strings.Contains(sym, "// the stack keeps the file being read when the filter still shows it") {
		t.Error("setFilter no longer keeps the reader's file under a stack")
	}
	for _, want := range []string{
		"if (state.stack && state.layout === \"diff\") return openFile(state.fileCursor);", // live refresh rebuilds
		"teardownStack(); return symEmpty(); }",                                            // an empty view drops the stack
	} {
		if !strings.Contains(files, want) {
			t.Errorf("files.js is missing %q", want)
		}
	}
}

// A file near the end of a stack must be reachable: without a one-pane tail
// the pane runs out of scroll, the header at the top is a file ABOVE the one
// clicked, and the list highlight follows that file (probe-found, link sets).
func TestStackTailLetsAnyHeaderReachTheTop(t *testing.T) {
	t.Parallel()
	if !strings.Contains(readStatic(t, "stackview.js"), `pane.style.setProperty("--stk-tail",`) {
		t.Error("stackview.js no longer measures the stack's tail")
	}
	if !strings.Contains(readStatic(t, "style.css"), "height: var(--stk-tail, 0px)") {
		t.Error("style.css no longer gives the stack its tail")
	}
}

// A stack mixes one-column tables (a pure add or delete) with two-column ones.
// The pan bars must be sized over EVERY table, each line against its own cell:
// sized from the first table only, a one-column file on top gave one bar
// measured against a full-width cell — no scrollbar at all while the
// two-column files below had lines cut off (user-found, symmetric view).
func TestPanBarsMeasureEveryTable(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	for _, want := range []string{
		`const twoCol = !!host.querySelector("table.diff colgroup col:nth-child(4)");`,
		"p.offsetWidth - (p.parentElement.clientWidth - 12)",
	} {
		if !strings.Contains(files, want) {
			t.Errorf("mountPanBars is missing %q", want)
		}
	}
	if strings.Contains(files, `table.querySelectorAll("colgroup col").length === 4`) {
		t.Error("mountPanBars decides the bars from the first table again")
	}
}

// Scroll mode's bars paint their own thumb: Firefox (and macOS) draw OVERLAY
// scrollbars that only show on hover, so a native 14px bar read as "no
// scrollbar at all" (user-found in Firefox). The native one stays as the
// scroller, hidden.
func TestPanBarsPaintTheirThumb(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	css := readStatic(t, "style.css")
	for _, want := range []string{`<div class="hthumb"></div>`, "const paintThumb = mountThumb(bar);", "paintThumb();"} {
		if !strings.Contains(files, want) {
			t.Errorf("files.js is missing %q", want)
		}
	}
	for _, want := range []string{".hbars .hbar { overflow-x: auto; overflow-y: hidden; height: 14px; scrollbar-width: none; }", ".hbars .hbar::-webkit-scrollbar { display: none; }", ".hbars .hthumb {"} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css is missing %q", want)
		}
	}
}

// In a stack each file pans on its own: the section's body is the pan host
// and the section carries its own bars. One pane-wide pair panned every
// file's side at once (user ruling: bars under each file).
func TestStackFilesPanOnTheirOwn(t *testing.T) {
	t.Parallel()
	view := readStatic(t, "stackview.js")
	for _, want := range []string{
		`<div class="hbars stk-hbars hidden"></div></section>`,
		`mountPanBars(el.querySelector(".stk-body"), el.querySelector(".stk-hbars"));`,
		`$("diff-hbars").classList.add("hidden");`,
	} {
		if !strings.Contains(view, want) {
			t.Errorf("stackview.js is missing %q", want)
		}
	}
	if strings.Contains(view, `mountPanBars($("diff-body"), $("diff-hbars"))`) || strings.Contains(view, `mountPanBars(body, $("diff-hbars"))`) {
		t.Error("stackview.js mounts the pane-wide bars over the whole stack again")
	}
}
