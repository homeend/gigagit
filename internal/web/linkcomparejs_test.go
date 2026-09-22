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
		{"linkcompare.js", `getJSON("/api/linkhist")`, "the history comes from the server's ring, never browser storage"},
		{"style.css", "#linkcmp-hist-left.hidden", "the left history list's own hide rule"},
		{"style.css", "#linkcmp-hist-right.hidden", "the right history list's own hide rule"},
		{"index.html", `id="linkcmp-swap"`, "the swap control"},
		{"files.js", `id="link-save-chip"`, "an open link comparison offers save comparison…"},
		{"linkcompare.js", `postJSON("/api/saved-compares"`, "the chip saves through the write-guarded route"},
		{"linkcompare.js", "allowEmpty: true", "an empty label must reach the store, which owns the default"},
		{"layers.js", "promptAllowEmpty", "openPrompt must be able to submit an empty answer"},
		{"previews.js", `getJSON("/api/saved-compares")`, "the Previews tab loads pairs and comparisons"},
		{"previews.js", `runLinkCompare("id="`, "a saved row opens through the server's door, by id"},
		{"previews.js", "li.dataset.kind", "a row's KIND picks its list and its menu — the three kinds share one store"},
		{"previews.js", `"/api/saved-compares/rename"`, "rename goes through the kind-routing route"},
		{"previews.js", `"copy gg link — left"`, "a comparison offers each of its two links"},
		{"previews.js", `"copy gg link — right"`, "…by name"},
		{"live.js", "runLinkCompare(", "a @a..b navigate lands through the door"},
		{"core.js", "savedCompares:", "the state slot"},
		{"linkcompare.js", "/api/link-base", "the server classifies and rewrites; the page has no link parser"},
		{"style.css", "#linkcmp-base-left.hidden", "the left base row's own hide rule"},
		{"style.css", "#linkcmp-base-right.hidden", "the right base row's own hide rule"},
		{"style.css", "#linkcmp-baseerr-left.hidden", "the left base error's own hide rule"},
		{"style.css", "#linkcmp-baseerr-right.hidden", "the right base error's own hide rule"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	// The arm must sit BEFORE the hash lane: that lane has no hashes to read
	// in a link comparison. Both the single-file open and the shared URL
	// builder (fileDiffURL, which the stacked view also fetches through)
	// have one.
	files := read("files.js")
	if arm, hash := strings.Index(files, "state.compare.links) {"), strings.Index(files, "getJSON(fileDiffURL(f))"); arm < 0 || hash < 0 || arm > hash {
		t.Errorf("files.js: openFile's link-comparison arm (%d) must come before the hash lane (%d)", arm, hash)
	}
	if arm, hash := strings.Index(files, "(c.links || c.frozen)) {"), strings.Index(files, `q.set("left", c.aHash)`); arm < 0 || hash < 0 || arm > hash {
		t.Errorf("files.js: fileDiffURL's spec arm (%d) must come before the hash lane (%d)", arm, hash)
	}
	// A pick FILLS its field; it never compares.
	if i := strings.Index(read("linkcompare.js"), "function pickHist("); i < 0 {
		t.Error("linkcompare.js: no pickHist")
	} else if body := read("linkcompare.js")[i:]; strings.Contains(body[:strings.Index(body, "\n}\n")], "submit(") {
		t.Error("linkcompare.js: pickHist submits — a pick only fills the field")
	}
	// Nothing is rewritten until the user acts on the base row: the LOOKUP must
	// never assign a link field.
	if i := strings.Index(read("linkcompare.js"), "async function lookupBase("); i < 0 {
		t.Error("linkcompare.js: no lookupBase")
	} else if body := read("linkcompare.js")[i:]; strings.Contains(body[:strings.Index(body, "\n}\n")], "field(side).value =") {
		t.Error("linkcompare.js: lookupBase assigns the link field — only applyBase may rewrite it")
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
