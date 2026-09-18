package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The web's in-view text search (the TUI's / @ ] [ in the diff view and
// blame) is a client-side feature: the Go side can only pin the wiring the
// browser depends on — the hidden rules (gg web hides by ID; a bare
// class="hidden" is ALWAYS visible), the CSS scoping (a `.hit` rule scoped to
// table.diff alone paints nothing in blame, as the syntax classes once did),
// the module imports, and the key routing.
func TestInViewSearchStaticWiring(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"index.html", `id="diff-search"`, "the diff pane's search bar"},
		{"index.html", `id="diff-search-input"`, "the diff bar's query field"},
		{"index.html", `id="blame-search"`, "the blame overlay's search bar"},
		{"index.html", `id="blame-search-input"`, "the blame bar's query field"},
		{"index.html", `/ find · ] [ next / prev`, "the blame hint line advertises the keys"},
		{"style.css", `#diff-search.hidden { display: none; }`, "per-ID hidden rule — the bar would never hide"},
		{"style.css", `#blame-search.hidden { display: none; }`, "per-ID hidden rule — the bar would never hide"},
		{"style.css", `table.diff .hit, #blame-body .hit {`, "hit styling must be scoped to BOTH hosts, like the .tk-* classes"},
		{"style.css", `table.diff .hit.cur, #blame-body .hit.cur {`, "the current hit paints differently in both hosts"},
		{"style.css", `--hit-cur-bg:`, "the current-hit colour lives with the theme tokens (the TUI's search_current_bg)"},
		{"files.js", `from "./inviewsearch.js"`, "the diff host uses the shared engine"},
		{"filehist.js", `from "./inviewsearch.js"`, "the blame host uses the shared engine"},
		{"files.js", `function diffSearchKey(`, "the diff host's key hook"},
		{"filehist.js", `function blameSearchKey(`, "the blame host's key hook"},
		{"filehist.js", `e.target === $("blame-search-input")`, "keys typed into the blame bar must not reach w / d / D"},
		{"keys.js", `diffSearchKey(e)`, "the document handler routes diff-layout keys to the search first"},
		{"files.js", `hitsOn(`, "hits are painted from state inside the render, never added to the DOM afterwards"},
		{"files.js", `data-i="`, "rows carry their index so a hit can be found in the DOM"},
		{"inviewsearch.js", `function findHits(`, "the case-folded rune-walk matcher"},
		{"inviewsearch.js", `function stepHit(`, "] and [: strict, wrapping, document order"},
		{"searchbar.js", `key: "/ · @ · ] · [ · in-view search"`, "the ? help advertises the keys"},
		{"searchbar.js", `function bindSearchBar(`, "the one bar controller both hosts share"},
		{"files.js", `from "./searchbar.js"`, "the diff host mounts the shared bar"},
		{"filehist.js", `from "./searchbar.js"`, "the blame host mounts the shared bar"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	// The current hit must be an element a probe can assert VISIBLE: the
	// class pair the CSS keys on, emitted by the one cell renderer.
	if fj := read("files.js"); !strings.Contains(fj, `"hit cur"`) && !strings.Contains(fj, `" cur"`) {
		t.Error("files.js: renderCell never emits the current-hit class")
	}
	// The footer chip is the same `/` in both layouts and must say which.
	if fj := read("files.js"); !strings.Contains(fj, `"/ find"`) {
		t.Error("files.js: the footer's / chip is not relabelled for the diff layout")
	}
}

// Opening a diff or blame must hand the keyboard to the CONTENT: focus lands
// on the pane / the blame body (both focusable, ring-less), and the arrow and
// page keys scroll it whatever the mouse left the focus on.
func TestContentFocusStaticWiring(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"index.html", `id="diff-pane" class="pane" tabindex="-1"`, "the diff pane must be focusable"},
		{"index.html", `id="blame-body" tabindex="-1"`, "the blame body must be focusable"},
		{"style.css", `#diff-pane:focus, #blame-body:focus { outline: none; }`, "no focus ring on the content"},
		{"files.js", `function focusDiff(`, "the one put-focus-on-the-diff step"},
		{"files.js", `function scrollKey(`, "the shared arrow/page/home/end scroller"},
		{"files.js", `function diffScrollKey(`, "the diff layout's scroll-key hook"},
		{"files.js", `focus: focusDiff,`, "enter in the search bar returns focus to the diff"},
		{"keys.js", `if (diffScrollKey(e)) return;`, "the document handler routes diff-layout arrows to the scroller before the cursor move"},
		{"filehist.js", `$("blame-body").focus({ preventScroll: true });`, "blame focuses its body on open"},
		{"filehist.js", `scrollKey($("blame-body"), e)`, "the blame layer scrolls for the arrows and page keys"},
		{"searchbar.js", `if (host.focus) host.focus();`, "enter hands the keys to the content, not the body"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	// focusDiff must run at every diff OPEN (entry diff, commit file, status
	// file) — three call sites plus the definition and the bar hook.
	if n := strings.Count(read("files.js"), "    focusDiff();\n"); n != 3 {
		t.Errorf("files.js: focusDiff() called %d times, want 3 (entry diff, commit file, status file)", n)
	}
}
