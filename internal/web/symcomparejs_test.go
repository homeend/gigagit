package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The symmetric comparison view touches markup, CSS, four modules, the wire
// and the stored layout. A missed edit half-works silently: a chip that paints
// nothing, a preference that forgets itself, a live refresh that hands the
// painter rows with no per-side state.
func TestSymmetricCompareIsWiredEverywhere(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, c := range []struct{ file, want, why string }{
		{"index.html", `id="symleft-pane"`, "the left set's list"},
		{"index.html", `id="symleft-spacer"`, "what keeps row N at the same y on both sides"},
		{"index.html", `id="sym-dir" class="hidden"`, "the direction bar starts hidden"},
		{"style.css", `#sym-dir.hidden { display: none; }`, "gg web hides BY ID: a bare .hidden class shows"},
		{"style.css", `#symleft-pane { display: none; }`, "the left pane exists only inside the symmetric grid"},
		{"style.css", `#panes.detail.sym {`, "the three-column grid"},
		{"files.js", `function compareRows(c)`, "the ONE writer of a comparison's state.files"},
		{"files.js", `state.files = compareRows(c);`, "applyCompareFilter AND updateLinkCompareFiles go through it"},
		{"files.js", `if (symActive()) return renderSymLists();`, "renderFiles defers to the two aligned lists"},
		{"files.js", `symActive()) return openSymRow(f);`, "a row opens in the arrow's direction"},
		{"files.js", `sym: body.sym || null,`, "no sym on the wire → the view is not offered"},
		{"files.js", `symLayoutChanged();`, "leaving the diff stage drops the grid"},
		{"linkcompare.js", `updateLinkCompareFiles(files, body.sym);`, "a live refresh must carry the aligned rows too"},
		{"keys.js", `if (symKey(e)) return;`, "v / x / 1–4"},
		{"symcompare.js", `saveUI({ sym_compare:`, "the choice persists server-side (random port: localStorage is useless)"},
		{"symcompare.js", `data-sf=`, "the chips must NOT use data-f: the compare bar's own handler owns that"},
		{"uistate.js", `sym_compare: false`, "saveUI's base must carry the field — the endpoint REPLACES the record"},
	} {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	if n := strings.Count(read("files.js"), "state.files = compareRows(c);"); n != 2 {
		t.Errorf("files.js: compareRows must feed exactly the two writers, found %d", n)
	}
}

// The module must be SERVED: a new static file that 404s leaves every import
// of it — and so the whole page — dead.
func TestSymmetricCompareModuleIsServed(t *testing.T) {
	ts := linkServe(t, newRepoDir(t, 1))
	if code := getAny(t, ts, "/static/symcompare.js", nil); code != 200 {
		t.Fatalf("/static/symcompare.js = %d", code)
	}
}
