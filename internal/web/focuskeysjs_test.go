package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// visibleStep / snapVisible walk a cursor over the rows a filter shows (the
// TUI's panel filter: the cursor never rests on a hidden row; typing snaps it
// to the nearest match at or after it, else the last one).
func TestVisibleStepJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "keys.js", pagingPureStart, pagingPureEnd, `
const v = [2, 5, 9];
const r = [];
r.push(visibleStep(v, 5, 1), visibleStep(v, 5, -1), visibleStep(v, 9, 1), visibleStep(v, 2, -1), visibleStep(v, 5, 10));
r.push(visibleStep([], 4, 1), visibleStep(v, 3, 1), visibleStep(v, 3, -1));
r.push(snapVisible(v, 5), snapVisible(v, 6), snapVisible(v, 10), snapVisible([], 7));
console.log(r.join("|"));
`)
	// a cursor off the visible rows first snaps (3 → 5), then moves
	want := "9|2|9|2|9|4|9|2|5|9|9|7"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// `/` searches the FOCUSED list: the commit list's filter, or the file
// list's own filter (the TUI's `/` filters the focused panel). `@` is the
// commit list's key only. The main lists page 25% of what they show (the
// TUI's pageStep).
func TestFocusLocalKeys(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"keys.js", `if (state.pane === "files") openFileFilter();`, "/ with the file list focused filters the file list"},
		{"keys.js", `} else if (e.key === "@" && state.layout !== "diff" && state.pane === "commits") {`, "@ is the commit list's key only"},
		{"keys.js", `/ 4)`, "a page is a quarter of the list (the TUI's pageStep)"},
		{"keys.js", `if (state.pane === "files" && closeFileFilter()) return;`, "esc on the file list drops its kept filter first"},
		{"files.js", `function openFileFilter(`, "the file list's filter bar"},
		{"files.js", `if (!fileShown(f)) return;`, "the renderers skip rows the filter hides"},
		{"reviews.js", `if (!fileShown(f)) return;`, "a review's file list filters too"},
		{"index.html", `id="flfilter"`, "the bar sits in the file pane's sticky header"},
		{"index.html", `id="flfilter-input"`, "…with its input"},
		{"style.css", `#flfilter.hidden`, "every hideable element has its own #id.hidden rule"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	// the footer chip follows the same routing as the key
	k := read("keys.js")
	if !strings.Contains(k, `case "filter": if (state.layout === "diff") diffSearchBar.open(false); else if (state.pane === "files") openFileFilter(); else openCommitFilter(); break;`) {
		t.Error("keys.js: the / filter footer chip must route by focus like the key")
	}
}
