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

// Review notes in a stack (plan 4a): every file carries its OWN note address,
// so a note written inside a stack is filed exactly where the same note
// written one file at a time would be. The single-file openers and the stack
// must therefore build that context with the SAME function — a second builder
// would drift silently, and the drift is a note filed against the wrong file.
func TestStackNoteContextIsShared(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	view := readStatic(t, "stackview.js")

	for _, fn := range []string{"function commitDiffCtx(f)", "function statusDiffCtx(f)", "function rowNoteCtx(f)"} {
		if !strings.Contains(files, fn) {
			t.Fatalf("files.js: %s is gone — the stack builds its slots' contexts with it", fn)
		}
	}
	if !strings.Contains(files, "state.diffCtx = commitDiffCtx(f);") || !strings.Contains(files, "state.diffCtx = statusDiffCtx(f);") {
		t.Fatal("files.js: the single-file openers must use the shared context builders")
	}
	if !strings.Contains(view, "s.ctx = rowNoteCtx(s.f)") {
		t.Fatal("stackview.js: a slot's note context must come from rowNoteCtx — the same door openFile uses")
	}

	// The renderer takes the context EXPLICITLY. A module-level "current slot"
	// would race: a stack paints one slot while another's notes are in flight.
	if !strings.Contains(files, "function diffHTML(d, paneWidth, notesOn = false, open = state.diffFolds, nctx = null, hctx = null, kctx = null)") {
		t.Fatal("files.js: diffHTML must take the note, search AND hunk contexts as parameters")
	}
	if !strings.Contains(view, "diffHTML(s.diff, $(\"diff-pane\").clientWidth, notesArmed(nc.ctx), s.folds, nc, hctx, kctx)") {
		t.Fatal("stackview.js: a slot must paint with its own note context")
	}

	// One address per file means there is no single re-read: a write, a sweep
	// or a live notes event fans out over the loaded slots.
	if !strings.Contains(files, "if (state.stack) return refreshStackNotes();") {
		t.Fatal("files.js: fetchNotes must fan out over a stack's slots")
	}
	if !strings.Contains(view, "async function refreshStackNotes()") || !strings.Contains(view, "function activeDiff()") {
		t.Fatal("stackview.js: refreshStackNotes and activeDiff are the stack's note accessors")
	}

	// The marked row says which FILE the reader is in.
	if !strings.Contains(files, "const sec = tr.closest(\".stk-file\");") {
		t.Fatal("files.js: markDiffRow must record the row on its own slot")
	}
	// …and the note prompt reads that slot, not the globals.
	add := jsFunc(t, "files.js", "addNotePrompt")
	if !strings.Contains(add, "const ad = activeDiff();") || !strings.Contains(add, "noteQuery(ad.ctx)") {
		t.Fatal("files.js: addNotePrompt must act on the active slot's address")
	}
}

// Both note gates must read the ACTIVE slot's context, not the single-file
// view's global one: state.diffCtx is null while a stack is up, so a global
// gate leaves every note key and every row click dead inside a stack — the
// exact defect the browser probe caught twice.
func TestStackNoteGatesReadTheActiveSlot(t *testing.T) {
	t.Parallel()
	keys := readStatic(t, "keys.js")
	if !strings.Contains(keys, "notesArmed(activeDiff().ctx)") {
		t.Fatal("keys.js: noteKey must gate on the active slot's context")
	}
	files := readStatic(t, "files.js")
	if !strings.Contains(files, "if (!notesArmed(rowSlotCtx(handle || e.target.closest(\"tr\")) || state.diffCtx)) return;") {
		t.Fatal("files.js: the diff-body click must gate on the CLICKED row's own file")
	}
}

// A gg:// link or a steering navigate with a LINE must land inside the named
// file's own section: line numbers repeat across a stack, so the pane-wide
// row lookup would mark whichever file carries that number first.
func TestStackLineLandingIsPerFile(t *testing.T) {
	t.Parallel()
	view := readStatic(t, "stackview.js")
	live := readStatic(t, "live.js")
	if !strings.Contains(view, "async function landStackLine(path, side, line)") {
		t.Fatal("stackview.js: landStackLine is the stack's line landing")
	}
	if !strings.Contains(view, "const sec = sectionEl(k);") || !strings.Contains(view, "sec.querySelector(`tr[data-side=") {
		t.Fatal("stackview.js: the row must be looked for inside the target file's OWN section")
	}
	if !strings.Contains(live, "await landStackLine(s.file, side, s.line);") {
		t.Fatal("live.js: a landing with a line must go through the stack's own lander")
	}
}

// Plan 4b: the in-view search spans a whole stack. The browser is the only
// place the wiring can be seen working, so these guards pin the shape the
// probe proved — one search over one document, keyed per slot, re-found at
// every site that changes which rows exist.
func TestStackSearchSpansTheWholeStack(t *testing.T) {
	t.Parallel()
	view := readStatic(t, "stackview.js")
	files := readStatic(t, "files.js")
	keys := readStatic(t, "keys.js")

	// The refusal is gone — and with it the toast that advertised it.
	if strings.Contains(keys, "search works in the single-file view") {
		t.Fatal("keys.js: a stack searches now; the refusal toast must be gone")
	}
	// …and the real gate was never the toast: diffSearchKey bails on
	// state.lastDiff, which is null while a stack is up.
	if !strings.Contains(files, "(!state.lastDiff && !state.stack)") {
		t.Fatal("files.js: diffSearchKey's gate must admit a stack — state.lastDiff is null there")
	}

	// Line numbers repeat across a stack, and the engine orders hits by a
	// NUMERIC row: each slot's rows are keyed into one document space.
	if !strings.Contains(view, "const STACK_ROW_SPAN") || !strings.Contains(view, "base: slotBase(s)") {
		t.Fatal("stackview.js: a slot must key its rows into the stack-wide space, or file B's row 12 collides with file A's")
	}
	// ONE re-find over every slot: a per-slot re-find leaves the hit list
	// holding only the last slot's hits.
	if !strings.Contains(view, "function refindStack()") {
		t.Fatal("stackview.js: the stack must re-find once over every loaded slot")
	}
	if !strings.Contains(files, "const hs = hctx ? (hctx.search.query ? hctx.search : null)") {
		t.Fatal("files.js: a slot's render must PAINT the stack's search, never re-find its own")
	}
	// Every site that changes which rows exist must re-find and repaint the
	// bar, or the count goes stale (the web-inview-search probe's own lesson).
	for _, site := range []string{"async function load(", "function rerenderStack(", "function reconcileStack("} {
		i := strings.Index(view, site)
		if i < 0 {
			t.Fatalf("stackview.js: %s vanished — re-point this guard", site)
		}
		end := i + 2400 // widened when 4c's per-slot staging joined load()
		if end > len(view) {
			end = len(view)
		}
		if !strings.Contains(view[i:end], "refindStack()") || !strings.Contains(view[i:end], "diffSearchBar.paint()") {
			t.Fatalf("stackview.js: %s changes which rows exist — it must re-find and repaint the count", site)
		}
	}
}

// ] and [ reach a hit in a file the stack has never read, one file per step.
func TestStackHitStepOpensUnreadFiles(t *testing.T) {
	t.Parallel()
	view := readStatic(t, "stackview.js")
	files := readStatic(t, "files.js")
	bar := readStatic(t, "searchbar.js")

	if !strings.Contains(view, "stepHitStrict") {
		t.Fatal("stackview.js: ] must know when the loaded slots have run out, not silently wrap")
	}
	i := strings.Index(view, "async function stackHitStep(")
	if i < 0 {
		t.Fatal("stackview.js: stackHitStep is the stack's ]/[")
	}
	body := view[i : i+2600]
	for _, want := range []string{"s.collapsed = false", "awaitSlot(st, s)", "refindStack()", "stepHit("} {
		if !strings.Contains(body, want) {
			t.Fatalf("stackview.js: stackHitStep must %q — unfold, fetch, re-find, and wrap only at the end", want)
		}
	}
	// The bar cannot do this itself: its own step is synchronous.
	if !strings.Contains(bar, "if (host.step && host.step(delta)) return;") {
		t.Fatal("searchbar.js: a host that must FETCH to reach a hit steps itself")
	}
	if !strings.Contains(bar, "host.count ? host.count() : s.count()") {
		t.Fatal("searchbar.js: a stacked host's count spans files it has not searched, and says so")
	}
	if !strings.Contains(files, `return unsearchedSlots() > 0 ? c + "+" : c;`) {
		t.Fatal("files.js: the stacked count must mark that files remain unsearched")
	}
}
