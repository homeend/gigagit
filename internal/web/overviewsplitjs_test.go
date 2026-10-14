package web

import (
	"strings"
	"testing"
)

// Spec 2026-10-11 §6 / W5 / W6: a file an overview opened carries its from
// before its first render, its stored review's remarks ride the content
// read, every note reader sees the remarks, and n/p never move the
// overview's selection.
func TestViewerRemarksAndMarkOnlyBandsAreWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`function fetchContent(src, rev, path, remarks = "", init = undefined)`,
		`(remarks ? "&" + remarks : "")`,
		`async function openViewer({ src = "worktree", rev = "", path = "", line = 0, id = "", from = null, steer = false })`,
		`fetchContent(reg.file.source, reg.file.rev || "", reg.file.path, remarksQuery(from))`,
		`view.remarks = body.remarks || [];`,
		`function shownNotes()`,
		`boxesAfter(shownNotes(), i)`,
		`noteAtLine(shownNotes(), i + 1)`,
		`nextNoteLine(shownNotes(), view.cur, dir)`,
		`"review remarks are read-only"`,
		`f.cur = a.dest; // W5: the current band only`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	for _, gone := range []string{`f.cur = f.dest = a.dest;`, "  f.sel = b.i;\n"} {
		if strings.Contains(v, gone) {
			t.Errorf("viewer.js still has %q (W5: n/p move no selection)", gone)
		}
	}
}

// Spec 2026-10-11 §3–§5: the box has a left column, the two overview doors
// open the split when the box is wide enough, and the split steers the right
// column through openViewer.
func TestOverviewSplitIsWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`<div id="viewer-ov" class="hidden"><div id="viewer-ov-title"></div><div id="viewer-ov-body" tabindex="-1"></div></div>`,
		`<div id="viewer-file">`,
		`let split = null;`,
		`function openSplit(id, path, ov, { steer = true } = {})`,
		`if (splitFits(boxWidth())) return openSplit(f.id, f.path, `,
		`if (splitFits(boxWidth())) return openSplit("", "", `,
		`await openViewer({ ...target, from, steer: true })`,
		`if (split) return splitKey(e);`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	css := readStatic(t, "style.css")
	for _, want := range []string{`#viewer-ov.hidden { display: none; }`, `#viewer-file {`, `.md a.md-anchor.ahere`} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css lacks %q", want)
		}
	}
}

// Spec 2026-10-11 §7 / §8: the right column's own keys, and the mark follows
// every cursor move.
func TestOverviewSplitRightColumnIsWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{`function syncHere()`, `hereOf(bands, cur, view.cur, selB)`, `case "Backspace": e.preventDefault(); focusSplit(true); return true;`,
		`case "N": case "P":`, `$("viewer-ov-body").addEventListener("click"`} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
}

// Spec 2026-10-11 §5 / §10: the split follows the store, collapses when the
// box narrows, and backspace in a file comes back INTO the split.
func TestOverviewSplitLiveIsWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{`function refreshSplitOverview()`, `function splitOverviewGone(`, `function narrowSplit()`,
		`function backIntoSplit(f)`, `if (f && splitFits(boxWidth())) return backIntoSplit(f);`,
		`window.addEventListener("resize"`, `if (split) return; // the split adds no history entry`} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
}

// The split is advertised: the viewer's help entry and the ≡ Overview row's.
func TestOverviewSplitIsAdvertised(t *testing.T) {
	t.Parallel()
	if !strings.Contains(readStatic(t, "viewer.js"), "opens beside the file its selected link points to") {
		t.Error("viewer.js help lacks the split")
	}
	if !strings.Contains(readStatic(t, "reviews.js"), "beside the file") {
		t.Error("reviews.js ≡ Overview help lacks the split")
	}
}

// Final review: the split's right title is re-cut to its half on every open
// (backIntoSplit keeps a file whose title was cut for the whole box — the
// end, the file NAME, would be clipped); a fresh split never shows the last
// file's lines; reusing a split re-reports its file as the one shown.
func TestOverviewSplitFinalFixesAreWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		"  paintSplitFocus();\n  paintTitle(); // the right column's title is cut to its half",
		"if (view.ov || !view.id || steer) Object.assign(view, {",
		`if (view.id) ofQueue({ op: "focus", id: view.id }); // the tab shows the file, not the overview`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
}

// Spec 2026-10-11 §11 (plan 2): S stacks the files in the right column and
// back; the stack is its own state (split.stack) with the single file
// stepped aside; loads are guarded by the stack they were started for; the
// pref is remembered; D and the single-file keys say so in the stack.
func TestOverviewSplitStackIsWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`from "./splitstack.js"`,
		`const SINGLE_ONLY = "this acts on a single file — S opens it";`,
		`const STACK_READONLY = "notes are read-only in the stack — S opens the file";`,
		`function toggleSplitStack()`,
		`function enterStack(at)`,
		`async function leaveStack(open = false)`,
		`if (split && split.stack) return renderStack();`,
		`if (!split || split.stack !== st) return; // the stack left the screen while this loaded`,
		`saveUI({ stacked_review_files: true });`,
		`saveUI({ stacked_review_files: false });`,
		`state.ui && state.ui.stacked_review_files`,
		`case "S": e.preventDefault(); toggleSplitStack(); return true;`,
		`{ root: $("viewer-body"), rootMargin: "100% 0px" }`,
		`st.inFlight < SPLIT_STACK_MAX_IN_FLIGHT`,
		`await openViewer({ ...target, from, steer: true });`,
		`function stackKey(e)`,
		`if (split.stack) return stackKey(e);`,
		`case "N": case "P": { const h = nextHeader(st.slots, st.cur, e.key === "N" ? 1 : -1); if (h) { st.cur = h; paintStackCursor("start"); } break; }`,
		`case "/": case "@": case "L": case ".": case "}": case "{": case "f": opLine(SINGLE_ONLY, false); break;`,
		`opLine(STACK_READONLY, false)`,
		`function stepStackAnchor(dir)`,
		`function foldStackFile()`,
		`function foldAllStack()`,
		`st.slots = restack(st.slots, stackFiles(stackReviewFiles(sp), sp.ov.anchors));`,
		"<button data-vact=\"stack\">S ",
		`<b>S</b> stacks`,
		`k = st.slots.indexOf(s); // a refresh of the links may have moved (or dropped) the file meanwhile`,
		`if (STACK_MOVES.has(e.key)) st.want = null;`,
		`if (head || row) st.want = null;`,
		`if (split && split.stack) leaveStack(); // another overview takes the split: its stack goes first`,
		`st.abort.abort(); // its reads are cancelled, never left to finish for nothing`,
		`{ signal: st.abort.signal }`,
		`" — the stack stays"`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	// S back must keep the overview's D choice: entering the stack never clears it.
	if i := strings.Index(v, "function enterStack(at)"); i < 0 {
		t.Error("viewer.js lacks enterStack")
	} else if j := strings.Index(v[i:], "\n}\n"); j < 0 || strings.Contains(v[i:i+j], "diffPref = null") {
		t.Error("enterStack must keep diffPref (S back returns to the held D choice)")
	}
	css := readStatic(t, "style.css")
	for _, want := range []string{`.vstk-head {`, `.vstk-head.vcur`, `.vstk-ph {`} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css lacks %q", want)
		}
	}
}
