package web

import (
	"strings"
	"testing"
)

// The review view lists ≡ Overview ABOVE ≡ Summary: the Summary sits on the
// files, as the stack reads it (Summary, then the first file).
func TestStoredOverviewRowLeadsTheSummary(t *testing.T) {
	t.Parallel()
	rv := readStatic(t, "reviews.js")
	i := strings.Index(rv, "function renderReviewFiles(")
	if i < 0 {
		t.Fatal("reviews.js lacks renderReviewFiles")
	}
	body := rv[i:]
	ov, sum, files := strings.Index(body, `data-ovdoc="1"`), strings.Index(body, `data-ov="1"`), strings.Index(body, `state.files.forEach(`)
	if ov < 0 || sum < 0 || files < 0 || ov > sum || sum > files {
		t.Fatalf("renderReviewFiles order: overview %d, summary %d, files %d", ov, sum, files)
	}
}

// §4.2 / R12: the review view's first row (when it has one) opens the stored overview in the
// viewer's stored mode — its own open-files entry (spec
// 2026-10-29-web-stored-overview-tree); its anchors open the file at the
// reviewed tip; Back re-shows the kept copy.
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
		// its entry id once registered: before the answer a list change never closes it
		`return view.ov && view.ov.stored ? view.ov.stored.fid || "" : view.id;`,
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
		`export async function openPermanentOverview(id, { onClose = null, doc = null } = {})`,
		`getJSON("/api/overview/" + encodeURIComponent(id))`,
		`kind: "overview"`,
		`function fetchContent(src, rev, path, remarks = "", init = undefined, deferTok = false)`,
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

// Spec 2026-10-29-web-stored-overview-tree: a stored overview registers its
// own open-files entry (the server reads it) and keeps the id on its doc;
// esc closes it — alone, in the split, from the backdrop and the foot's
// close — ctrl+] backgrounds it, each after the register answered; a split
// whose stored overview another tab closed says so.
func TestStoredOverviewRegisters(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		"function showStoredOverview(doc) {\n  registerStored(doc);",
		`if (doc.fid) return ofQueue({ op: "focus", id: doc.fid });`,
		`.then(() => ofPost({ op: "stored", kind: r.kind, id: r.id }))`,
		`.then((a) => { if (a && a.file) doc.fid = a.file.id; })`,
		`ofSync = ofSync.then(() => (doc.fid ? ofPost({ op, id: doc.fid }) : null)).catch(() => {});`,
		`else if (sp.ov.stored) storedQueue(sp.ov.stored, how === "close" ? "close" : "background");`,
		`if (view.ov && view.ov.stored) storedQueue(view.ov.stored, how === "close" ? "close" : "background");`,
		`case "Escape": closeViewer(split.ov.stored ? "close" : "background"); break;`,
		`closeViewer(escHow(!!view.ov && !view.ov.stored, view.notes.length));`,
		`splitOverviewGone(splitName(), split.id ? "was deleted" : "was closed in another tab");`,
		`if (!split.id) return docName({ ov: split.ov });`,
		"const id = viewerFileId();\n  if (id) ofPost({ op: \"shown\", id }).catch(() => {});",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	if strings.Contains(v, `return closeViewer("close"); // page-local: nothing to background`) {
		t.Error("ctrl+] on a stored overview must background it, not close it")
	}
	if strings.Count(v, `case "Escape": closeViewer(split.ov.stored ? "close" : "background"); break;`) != 2 {
		t.Error("both split key handlers must close a stored overview on esc")
	}
	if !strings.Contains(readStatic(t, "openfiles.js"), `if (f.id === viewerFileId() || f.id === viewerStoredId()) dropViewer();`) {
		t.Error("x on this tab's stored overview must drop its viewer first")
	}
}

// Spec 2026-10-29-web-stored-overview-tree: the tree draws a stored
// overview's files at its commit (an open one is the commit row), opens a
// stored row by reading its document again (at a child's anchor), closes a
// row whose document is gone, D has nothing to delete and y copies the
// stored overview's own link.
func TestStoredOverviewTreeRows(t *testing.T) {
	t.Parallel()
	of := readStatic(t, "openfiles.js")
	for _, want := range []string{
		`if (f.source === "commit") cmDoc.set(f.rev + "\0" + f.path, f);`,
		`const doc = (ov.stored && c.rev ? cmDoc.get(c.rev + "\0" + c.path) : wtDoc.get(c.path)) || null;`,
		`rev: c.rev || ""`,
		`meta: n.rev ? "@ " + n.rev.slice(0, 7) : verLabel("worktree", "")`,
		`if (f.stored) return ""; // a stored overview: nothing of an agent's to delete`,
		`stored: n.doc.stored ? { kind: n.doc.stored.kind, id: n.doc.stored.id } : null`,
	} {
		if !strings.Contains(of, want) {
			t.Errorf("openfiles.js lacks %q", want)
		}
	}
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`stored = (reg.files || []).find((x) => x.id === reg.file.id && x.stored) || null;`,
		`if (stored) return openStoredRow(stored, anchor, seq);`,
		`getJSON("/api/review/" + encodeURIComponent(st.id))`,
		`getJSON("/api/overview/" + encodeURIComponent(st.id))`,
		`ofQueue({ op: "close", id: row.id, everywhere: true });`,
		`st.kind === "review" ? "that review no longer exists" : "that overview no longer exists"`,
		`sel: anchor ? anchorForPath(doc.anchors, anchor.path, anchor.index) : -1`,
		`openSplit("", "", { title: doc.title, blocks: doc.blocks, anchors: doc.anchors, text: doc.text, sel, want: "", stored: doc }, { pick: sel >= 0 })`, "doc.sel = -1; // one open's pick",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
}
