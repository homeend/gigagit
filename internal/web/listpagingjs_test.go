package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pagingPureStart = "// --- list paging (pure; guarded against Go) ---"
const pagingPureEnd = "// --- end list paging ---"

// pageTarget is where PgUp/PgDn/Home/End put a list cursor (the TUI's
// pgup/pgdn/home/end on its panels): a page at a time, clamped to the list,
// and an empty list stays put.
func TestPageTargetJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "keys.js", pagingPureStart, pagingPureEnd, `
const r = [];
r.push(pageTarget("PageDown", 0, 100, 20), pageTarget("PageDown", 90, 100, 20), pageTarget("PageDown", 99, 100, 20));
r.push(pageTarget("PageUp", 50, 100, 20), pageTarget("PageUp", 5, 100, 20), pageTarget("PageUp", 0, 100, 20));
r.push(pageTarget("Home", 42, 100, 20), pageTarget("End", 42, 100, 20));
r.push(pageTarget("PageDown", 0, 0, 20), pageTarget("End", 0, 0, 20), pageTarget("ArrowDown", 3, 100, 20));
console.log(r.join("|"));
`)
	want := "20|99|99|30|0|0|0|99|0|0|-1"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// PgUp/PgDn/Home/End move the FOCUSED list's cursor (commit list or file
// list), never the browser's scroll of whichever frame the mouse last
// touched; the file-history overlay pages its commits as the TUI's does
// (pgdn on the last row loads the older ones, as ↓ does).
func TestListPagingKeys(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"keys.js", `if (pageKey(e)) return;`, "the main page routes the paging keys to the focused list"},
		{"keys.js", `function revealFileCursor(`, "a page jump in the file list keeps the cursor on screen (below the sticky header)"},
		{"filehist.js", `if (e.key === "PageDown" || e.key === "PageUp") {`, "the history overlay pages its commits"},
		{"filehist.js", `pageTarget(e.key, hist.sel, hist.rows.length, historyPageRows())`, "a page is what the list shows"},
		{"index.html", `pgup/pgdn page`, "the history hint names the keys"},
		{"index.html", `pgup / pgdn · home / end`, "the ? help names the main lists' paging keys"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	// The diff layout keeps its own page keys (they scroll the diff): the
	// list paging must come AFTER diffScrollKey.
	k := read("keys.js")
	d, p := strings.Index(k, "if (diffScrollKey(e)) return;"), strings.Index(k, "if (pageKey(e)) return;")
	if d < 0 || p < 0 || p < d {
		t.Error("keys.js: pageKey must run after diffScrollKey — in the diff layout the page keys scroll the diff")
	}
}
