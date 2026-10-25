package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The file-history overlay hands a row to a standalone diff of that file at
// that commit (the TUI's history → enter → a diff layer of its own, esc
// back): the page's diff keys, no commit header, no file list; esc brings
// the overlay back on the same row and the page as it was. Unused keys must not
// leak to the browser (/ started Firefox's quick find over the overlay).
func TestHistoryRowOpensInTheDiffView(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"filehist.js", `async function openHistoryInDiff(`, "the one hand-off from a history row to the diff view"},
		{"filehist.js", `if (e.key === "Enter") {`, "enter in the overlay opens the selected row's diff"},
		{"filehist.js", `openHistoryInDiff(hist.sel)`, "enter opens the SELECTED row"},
		{"filehist.js", `if (li) openHistoryInDiff(Number(li.dataset.i));`, "a click on a row opens it in the diff view"},
		{"filehist.js", `setDiffBack(() => returnFromHistoryDiff(snap))`, "esc in that diff comes back to the overlay, the page as it was"},
		{"filehist.js", `openFileChangeDiff({`, "the row opens ONE file's change (the TUI's standalone diff layer), not the commit"},
		{"files.js", `async function openFileChangeDiff(`, "the standalone file diff: one file at one commit, no file list"},
		{"files.js", `if (state.soloDiff) return [];`, "no file stepping in a standalone diff (the TUI's diff layer has none)"},
		{"files.js", `setDiffTitle(path, "", "  @ " + short + " " + subject)`, "the TUI's title: the path, then @ <short> <subject>"},
		{"style.css", `#panes.detail.solodiff.solodiff`, "the standalone diff takes the whole width: no commit header, no file list"},
		{"filehist.js", `function reshowHistory(`, "the overlay is hidden, never destroyed, and comes back as it was"},
		{"filehist.js", `if (finderOn()) closeFinder();`, "F steps aside so the diff stage shows and owns the keys"},
		{"filehist.js", `if (e.key.length === 1) e.preventDefault();`, "unused printable keys must not reach the browser's quick find"},
		{"index.html", `enter or a click opens this file's change full screen`, "the overlay's hint says so"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	// The overlay must be HIDDEN for the trip, not closed: closeHistory drops
	// hist, and the way back would have nothing to show.
	fh := read("filehist.js")
	i := strings.Index(fh, "async function openHistoryInDiff(")
	if i < 0 {
		t.Fatal("openHistoryInDiff is gone")
	}
	body := fh[i:]
	if j := strings.Index(body, "\n}\n"); j > 0 {
		body = body[:j]
	}
	if strings.Contains(body, "openCommitByHash(") {
		t.Error("openHistoryInDiff opens the whole commit; the TUI opens that file's change alone")
	}
	if strings.Contains(body, "closeHistory()") {
		t.Error("openHistoryInDiff closes the overlay; it must hide it (hideHistory) so esc can bring it back")
	}
}
