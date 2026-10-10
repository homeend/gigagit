package web

import (
	"strings"
	"testing"
)

// §4.2 / R12: the review view's second row opens the stored overview in the
// viewer's stored mode — page-local, never registered as an open file; its
// anchors open the file at the reviewed tip; Back re-shows the kept copy.
func TestStoredOverviewIsWired(t *testing.T) {
	t.Parallel()
	rv := readStatic(t, "reviews.js")
	for _, want := range []string{`data-ovdoc="1"`, `≡ Overview`, `d.overviewMd`, `openStoredOverview({`} {
		if !strings.Contains(rv, want) {
			t.Errorf("reviews.js lacks %q", want)
		}
	}
	v := readStatic(t, "viewer.js")
	for _, want := range []string{`export function openStoredOverview(`, `function showStoredOverview(`, `view.ov.stored`, `storedAnchorOpen(`, `f.stored`, `if (view.ov && view.ov.stored) return Promise.resolve(true);`,
		// never registered: the open-files list never names it, so a change of
		// that list (the one its own anchor's open causes) must not close it
		`return isOpen() && !(view.ov && view.ov.stored) ? view.id : "";`} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	f := readStatic(t, "files.js")
	if !strings.Contains(f, `li.dataset.ovdoc`) {
		t.Error("files.js does not route the ≡ Overview row")
	}
	if !strings.Contains(readStatic(t, "style.css"), `li.rovd`) {
		t.Error("style.css lacks the overview row's rule")
	}
}
