package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The file-history overlay hands a row to the page's diff view (the TUI's
// history → enter → diff, esc back): every diff key works there, and esc in
// that diff brings the overlay back on the same row. Unused keys must not
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
		{"filehist.js", `setDiffBack(reshowHistory)`, "esc in that diff comes back to the overlay"},
		{"filehist.js", `function reshowHistory(`, "the overlay is hidden, never destroyed, and comes back as it was"},
		{"filehist.js", `if (finderOn()) closeFinder();`, "F steps aside so the diff stage shows and owns the keys"},
		{"filehist.js", `if (e.key.length === 1) e.preventDefault();`, "unused printable keys must not reach the browser's quick find"},
		{"index.html", `enter or a click opens the change in the diff view`, "the overlay's hint says so"},
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
	if strings.Contains(body, "closeHistory()") {
		t.Error("openHistoryInDiff closes the overlay; it must hide it (hideHistory) so esc can bring it back")
	}
}
