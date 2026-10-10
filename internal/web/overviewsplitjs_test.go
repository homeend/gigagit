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
		`function fetchContent(src, rev, path, review = "")`,
		`"&review=" + encodeURIComponent(review)`,
		`async function openViewer({ src = "worktree", rev = "", path = "", line = 0, id = "", from = null, steer = false })`,
		`fetchContent(reg.file.source, reg.file.rev || "", reg.file.path, remarksReview(from))`,
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
