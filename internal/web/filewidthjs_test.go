package web

import (
	"strings"
	"testing"
)

// [ui] file_width (spec 2026-10-25-web-file-width): the single-file views —
// the viewer showing one file, the F preview, blame — show their TEXT column at
// most N code characters wide; the gutter comes on top. The box narrows (never
// widens), so the w long-line modes need nothing new.
func TestFileWidthCapsSingleFileViews(t *testing.T) {
	t.Parallel()
	css := readStatic(t, "style.css")
	for _, c := range []struct{ src, want, why string }{
		{readStatic(t, "bigrepo.js"), `applyFileWidth(state.health ? state.health.file_width : 200);`, "every health read (boot, repo switch) sets the width; no health = 200"},
		{readStatic(t, "core.js"), `function applyFileWidth(n) {`, "the one setter"},
		{readStatic(t, "core.js"), `root.setProperty("--file-text", n < 0 ? "100000px" : `, "-1 = unlimited"},
		{css, `:root { --file-text: 100000px; --blame-gut: clamp(150px, 32vw, 300px); }`, "the variables"},
		{css, `#viewer-box.vhalf { width: min(max(50vw, min(94vw, 760px)), calc(var(--file-text) + 50px)); }`, "the viewer: half width, capped"},
		{css, `#viewer-box.vhalf.vdiff { width: min(max(50vw, min(94vw, 760px)), calc(var(--file-text) + 66.8px)); }`, "…a one-column diff has a sign column too"},
		{css, `#viewer-box.vhalf.vpic { width: max(50vw, min(94vw, 760px)); }`, "…an image is no text: the half width only"},
		{readStatic(t, "viewer.js"), `box.classList.toggle("vdiff", file && !!view.diff);`, "the box is sized from the view before paintTitle measures it, not by :has() after the rows"},
		{css, `#viewer-box.split #viewer-body { width: min(100%, calc(var(--file-text) + 50px)); align-self: center; }`, "the split's right column: the same text column, centred in its half (the scroller narrows: scroll mode stays inside)"},
		{css, `#viewer-box.split.vpic #viewer-body { width: 100%; }`, "…an image keeps the column"},
		{css, `:root { --diff-side: 100000px; }`, "a diff with no script (the export) stays unlimited"},
		{css, `table.diff[data-cols="4"] { width: min(100%, calc(2 * (var(--diff-no) + var(--diff-side)))); margin-inline: auto; }`, "a side-by-side diff: each side at most N, centred"},
		{css, `table.diff[data-cols="2"] { width: min(100%, calc(var(--diff-no) + var(--diff-side))); margin-inline: auto; }`, "…one side (added / deleted) too"},
		{css, `.hbars[data-cols="4"] { max-width: calc(2 * (var(--diff-no) + var(--diff-side))); margin-inline: auto; }`, "scroll mode's pan bars span the table"},
		{css, `#diff-body .stk-file { max-width: calc(2 * (var(--diff-no) + var(--diff-side)) + 2px); margin-inline: auto; }`, "a stacked file spans its table"},
		{css, `#diff-body .stk-file:has(table.diff[data-cols="2"]) { max-width: calc(var(--diff-no) + var(--diff-side) + 2px); }`, "…an added / deleted file its one side"},
		{readStatic(t, "viewer.js"), `const inner = b.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight);`, "a split stack's paths fit the centred column"},
		{readStatic(t, "core.js"), `root.setProperty("--diff-side", n < 0 ? "100000px" : `, "the diff side is measured in the table's own font"},
		{readStatic(t, "files.js"), `bars.dataset.cols = twoCol ? "4" : table.dataset.cols || "";`, "the bars know their table's layout"},
		{css, `#panes.wtf.wtf.wtf:not(.wtf-full) #diff-pane > #wtf-preview:not(.hidden):has(.vimg) { width: max(50%, min(100%, 760px)); }`, "the F preview's image: the half width only"},
		{css, `width: min(max(50%, min(100%, 760px)), calc(var(--file-text) + 50px));`, "the F preview: half the area, capped"},
		{css, `width: min(100%, calc(var(--file-text) + 50px)); margin: 0 auto; }`, "…m lifts the half, never the cap"},
		{readStatic(t, "core.js"), `if (!Number.isFinite(n)) n = 200;`, "a missing width is the default, never NaNpx"},
		{css, `width: min(94vw, calc(var(--blame-gut) + 50px + var(--file-text)));`, "blame: the gutter on top of the text"},
		{css, `.bgut { flex: none; width: var(--blame-gut);`, "blame's gutter no longer depends on the box it sizes"},
		{css, `body.lm-scroll .bno { position: sticky; left: var(--blame-gut);`, "…nor in scroll mode"},
	} {
		if !strings.Contains(c.src, c.want) {
			t.Errorf("%s: missing %q", c.why, c.want)
		}
	}
}
