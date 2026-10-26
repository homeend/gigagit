package web

import (
	"strings"
	"testing"
)

// A <table> cannot be size-contained, so content-visibility on a diff's
// chunk table did nothing: every chunk of every file stayed laid out, and a
// full-file stack of 20 files (240k nodes) cost 600–900 ms per ←/→. A diff's
// later chunks go in a <div class="dchunk"> wrapper instead; a line host's
// chunk is already a <div> and keeps its own style.
func TestDiffChunksAreWrappedInDivsJS(t *testing.T) {
	t.Parallel()
	var got []string
	pureJS(t, "diffpaint.js", "// --- chunk ranges (pure; guarded against Go) ---", "// --- end chunk ranges ---",
		`[...chunkParts('<table class="diff split">', "</table>", 200, 20, true), ...chunkParts('<div class="rchunk">', "</div>", 200, 18.5, true), ...chunkParts('<div class="rchunk">', "</div>", 200, 18.5, false)]`, &got)
	want := []string{
		`<div class="dchunk"><table class="diff split">`, `</table></div>`,
		`<div style="content-visibility:auto;contain-intrinsic-size:auto 3700px" class="rchunk">`, `</div>`,
		`<div class="rchunk">`, `</div>`,
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// The walk from row to row crosses a wrapper into the next chunk's table.
func TestRowWalkCrossesChunkWrappersJS(t *testing.T) {
	t.Parallel()
	var got []string
	pureJS(t, "files.js", "// --- next row (pure; guarded against Go) ---", "// --- end next row ---", `(() => {
  const mk = (id) => ({ id, nextElementSibling: null });
  const a = mk("a"), b = mk("b"), c = mk("c");
  const t2 = { matches: (s) => s === "table.diff", querySelector: () => c };
  const w2 = { matches: (s) => s === ".dchunk", querySelector: (s) => (s === "table.diff" ? t2 : null), nextElementSibling: null };
  const w1 = { matches: (s) => s === ".dchunk", nextElementSibling: w2 };
  const t1 = { matches: (s) => s === "table.diff", parentElement: w1 };
  t2.parentElement = w2;
  a.closest = () => t1;
  c.closest = () => t2;
  const out = [];
  for (let r = a; r; r = nextRow(r)) out.push(r.id);
  return out;
})()`, &got)
	if strings.Join(got, "") != "ac" {
		t.Fatalf("walk = %v; want a c across the wrappers", got)
	}
}

// Once a diff is painted its wrappers get content-visibility with their
// measured height (real heights: a stack keeps the reader's place by them),
// and a pan writes only the chunks near the screen; the others catch up as
// they scroll in. Outside scroll mode a pan's leftover transform is void.
func TestDiffPanTouchesOnlyNearChunks(t *testing.T) {
	t.Parallel()
	paint := readStatic(t, "diffpaint.js")
	files := readStatic(t, "files.js")
	css := readStatic(t, "style.css")
	for _, c := range []struct{ src, want, why string }{
		{paint, "function containChunks(host)", "the wrappers are contained once painted"},
		{paint, "if (job.wrapped) containChunks(host);", "done() contains a diff's wrappers"},
		{files, "function syncPanNear(host)", "a pan writes the chunks near the screen"},
		{files, `document.addEventListener("scroll", panOnScroll, { capture: true, passive: true });`, "chunks scrolled in catch up"},
		{css, "body:not(.lm-scroll) table.diff td.side > .pan { transform: none !important; }", "a leftover pan is void outside scroll mode"},
	} {
		if !strings.Contains(c.src, c.want) {
			t.Errorf("missing %q: %s", c.want, c.why)
		}
	}
	if strings.Contains(files, "panRules(") || strings.Contains(files, "pan-rules") {
		t.Error("files.js: the stylesheet-rule pan must be gone (a rule change walks the whole document)")
	}
}
