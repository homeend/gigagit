package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The link-comparison surface touches seven files; a missed one half-works
// silently. Membership, never whole lines: a gate pins the CONTRACT.
func TestLinkCompareJSIsWiredEverywhere(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"app.js", "./linkcompare.js", "the module must be imported at boot"},
		{"linkcompare.js", "/api/compare-links", "the dialog goes through the server's door"},
		{"linkcompare.js", "e.data.side", "a failure is painted under the field the server names"},
		{"core.js", "err.data = body", "getJSON must keep a refusal's body, or the side is lost"},
		{"files.js", "state.compare.links", "openFile needs the link-comparison arm"},
		{"files.js", "f.left_spec || state.compare.aSpec", "a row's own byte source wins over its side's"},
		{"files.js", "f.right_spec || state.compare.bSpec", "…on both sides"},
		{"files.js", `q.set("old_path"`, "a rename's left side is read at its old path"},
		{"palette.js", "compare with link…", "the command palette row"},
		{"linkcompare.js", `registerRows("menu"`, "the ☰ menu row"},
		// gg web hides by ID: a class="hidden" element with no #id.hidden rule
		// is always visible.
		{"style.css", "#linkcmp.hidden", "the overlay's own hide rule"},
		{"style.css", "#linkcmp-err.hidden", "the form error's own hide rule"},
		{"style.css", "#linkcmp-err-left.hidden", "the left error's own hide rule"},
		{"style.css", "#linkcmp-err-right.hidden", "the right error's own hide rule"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	// The arm must sit BEFORE the hash lane: that lane has no hashes to read
	// in a link comparison.
	files := read("files.js")
	if arm, hash := strings.Index(files, "state.compare.links) {"), strings.Index(files, `q.set("left", state.compare.aHash)`); arm < 0 || hash < 0 || arm > hash {
		t.Errorf("files.js: the link-comparison arm (%d) must come before the hash lane (%d)", arm, hash)
	}
	// gg web binds a random port each run, which empties browser storage.
	src := read("linkcompare.js")
	for _, banned := range []string{"localStorage", "sessionStorage", "indexedDB"} {
		if strings.Contains(src, banned) {
			t.Errorf("linkcompare.js uses %s — the dialog keeps nothing in the browser", banned)
		}
	}
	html := read("index.html")
	for _, id := range []string{"linkcmp", "linkcmp-left", "linkcmp-right", "linkcmp-err-left", "linkcmp-err-right"} {
		if n := strings.Count(html, `id="`+id+`"`); n != 1 {
			t.Errorf("index.html: %d elements with id %q, want exactly 1", n, id)
		}
	}
}
