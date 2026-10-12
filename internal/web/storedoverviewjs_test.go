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
	for _, want := range []string{`data-ovdoc="1"`, `≡ Overview`, `d.overviewMd`, `openStoredOverview({`,
		`text: d.overviewText || ""`, // y copies the overview's markdown (§4.1)
		`<b>≡ Overview</b>`} {        // its help row
		if !strings.Contains(rv, want) {
			t.Errorf("reviews.js lacks %q", want)
		}
	}
	v := readStatic(t, "viewer.js")
	for _, want := range []string{`export function openStoredOverview(`, `function showStoredOverview(`, `view.ov.stored`, `storedAnchorOpen(`, `f.stored`, `if (view.ov && view.ov.stored) return Promise.resolve(true);`,
		// never registered: the open-files list never names it, so a change of
		// that list (the one its own anchor's open causes) must not close it
		`return isOpen() && !(view.ov && view.ov.stored) ? view.id : "";`,
		// an agent's docs changing must not "close" the way back (the stored id is never in the stamps)
		`if (f && !f.closed && !f.stored && !view.ov && !split) {`} {
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

// Spec 2026-10-12-permanent-overviews §10: a permanent overview opens in the
// viewer's stored mode, and every lane names its kind (review= / overview=).
func TestPermanentOverviewIsWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`export async function openPermanentOverview(id)`,
		`getJSON("/api/overview/" + encodeURIComponent(id))`,
		`kind: "overview"`,
		`function fetchContent(src, rev, path, remarks = "", init = undefined)`,
		`(remarks ? "&" + remarks : "")`,
		`remarksQuery(from)`,
		`export function closeGoneOverview(ids)`,
		`"the overview was deleted"`,
		`"/api/overview-export?" + q`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	for _, gone := range []string{`replace(/^review-overview:/, "")`, `function remarksReview(`} {
		if strings.Contains(v, gone) {
			t.Errorf("viewer.js still has %q (storedRef reads the kind)", gone)
		}
	}
	if sd := readStatic(t, "splitdiff.js"); strings.Contains(sd, `replace(/^review-overview:/, "")`) || !strings.Contains(sd, `storedQuery(from.stored)`) {
		t.Error("splitdiff.js does not ask storedQuery for the stored overview's lane")
	}
	if f := readStatic(t, "files.js"); !strings.Contains(f, `overviews: c.overviews || []`) || !strings.Contains(f, `closeGoneOverview(state.noteCounts.overview_ids)`) {
		t.Error("files.js does not keep the counts' overviews or close a gone one")
	}
}
