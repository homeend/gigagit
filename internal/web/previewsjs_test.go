package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The previews section touches five lists; a missed one half-works silently.
func TestPreviewsJSIsWiredEverywhere(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"core.js", `"previews"`, "SECTIONS must include previews (header click wiring)"},
		{"sidebar.js", `COLLAPSED_DEFAULT = ["previews"`, "COLLAPSED_DEFAULT must fold previews on a first run"},
		{"previews.js", `getJSON("/api/preview")`, "previews.js owns its fetch"},
		{"live.js", `fetchPreviews()`, "an SSE sidebar refresh must reload previews"},
		{"ops.js", `fetchPreviews()`, "manual refresh must reload previews"},
		{"app.js", `fetchPreviews()`, "boot must load previews"},
		{"live.js", `want.has("previews")`, "a bare previews event (a rename) must re-fetch the list"},
		{"live.js", `reopenPreviewIfMoved()`, "a refresh must re-open a preview whose tips moved"},
		{"menus.js", `"preview"`, "MENUS must accept preview rows"},
		{"app.js", `./previews.js`, "the module must be imported"},
		{"index.html", `id="previews-list"`, "the section markup"},
		{"index.html", `id="previews-header"`, "the section header (fold + add control)"},
		{"previews.js", `revs: 1`, "open must use the hash form of openCompare"},
		{"previews.js", `"Content-Type": "application/json"`, "DELETE goes through writeGuard"},
		{"previews.js", `originsError`, "the per-side origin filter is meaningless over merge-base → tip"},
		{"sidebar.js", `__ggAddPreview`, "the previews header's + control starts the add flow"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
}

// The sidebar draws its sections in source order, and previews sits with the
// things you STEER with (branches/remotes/worktrees) rather than at the bottom
// past the reference lists — a merge preview is a working surface, and the
// user asked for it right after worktrees. Nothing reads SECTIONS' order, so
// only the markup can be asserted; SECTIONS is kept in step for readers.
func TestPreviewsSectionSitsAfterWorktrees(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	at := func(id string) int {
		i := strings.Index(html, `id="`+id+`"`)
		if i < 0 {
			t.Fatalf("index.html: no element with id %q", id)
		}
		return i
	}
	if !(at("worktrees-list") < at("previews-header") && at("previews-list") < at("tags-header")) {
		t.Errorf("index.html: the previews section must sit between the worktrees and tags sections")
	}
	// Exactly one of each: a stale copy left behind by the move would render
	// a second, permanently empty section.
	for _, id := range []string{"previews-header", "previews-list"} {
		if n := strings.Count(html, `id="`+id+`"`); n != 1 {
			t.Errorf("index.html: %d elements with id %q, want exactly 1", n, id)
		}
	}
	core, err := os.ReadFile(filepath.Join("static", "core.js"))
	if err != nil {
		t.Fatal(err)
	}
	// (The pull-requests section sits right after previews — prs_static_test.go.)
	if !strings.Contains(string(core), `"worktrees", "previews", "prs", "tags"`) {
		t.Errorf("core.js: SECTIONS must list previews between worktrees and tags, matching the markup")
	}
}

// Drag & drop compare: the highlight class is styled per list, and each
// listener is load-bearing (dragover's preventDefault IS the drop permission).
func TestPreviewsDragDropIsWired(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"previews.js", `draggable="true"`, "preview and pair rows start a drag"},
		{"previews.js", `data-kind="preview"`, "a merge row names its kind for the drop lookup"},
		{"previews.js", `addEventListener("dragstart"`, "the drag source"},
		{"previews.js", `addEventListener("dragover"`, "the drop permission and the highlight"},
		{"previews.js", `addEventListener("dragleave"`, "the highlight goes off"},
		{"previews.js", `addEventListener("dragend"`, "an abandoned drag clears its state"},
		{"previews.js", `addEventListener("drop"`, "the drop opens the pair menu"},
		{"previews.js", `"compare and save…"`, "the drop menu saves"},
		{"previews.js", `"compare with…"`, "the menu twin of the drag"},
		{"previews.js", `previewRowLink`, "one link builder, shared with copy gg link"},
		{"links.js", `previewRowLink`, "links.js exports the builder"},
		{"style.css", `#previews-list li.drop-target`, "the highlight is styled per list"},
		{"core.js", `dragPreview`, "the drag state lives in core state"},
		{"previews.js", `<b>Drag</b> a merge preview`, "the section's help says the gesture exists"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
}

// Review sub-rows sit under their preview / pair row and route to the review,
// never to a preview handler (Review Focus 1).
func TestPreviewsJSReviewSubRowsWired(t *testing.T) {
	t.Parallel()
	src := staticSrc(t, "previews.js")
	for _, want := range []string{
		`previewReviewText`,
		`class="brev" data-review=`,
		`li.dataset.review`,
		`openReview(li.dataset.review, { kind: "list" })`,
		`reviewMenu(`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("previews.js lacks %q", want)
		}
	}
	// The review rows carry no data-id: every preview handler's guard skips them.
	if strings.Contains(src, `class="brev" data-id=`) {
		t.Error("a review sub-row carries a data-id")
	}
}

func TestPreviewsJSReviewMenuRows(t *testing.T) {
	t.Parallel()
	src := staticSrc(t, "previews.js")
	for _, want := range []string{
		`label: "review (AI)…"`,
		`window.__ggStartReview("preview", "", "", e.id)`,
		`label: "show review"`,
		`e.state === "ok"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("previews.js lacks %q", want)
		}
	}
	rv := staticSrc(t, "review.js")
	for _, want := range []string{`async function startReview(target, branch, sha, preview)`, `"&preview="`, `preview, tool: tool.name`, `window.__ggStartReview = startReview`} {
		if !strings.Contains(rv, want) {
			t.Errorf("review.js lacks %q", want)
		}
	}
}

func TestPreviewReviewsBlockWired(t *testing.T) {
	t.Parallel()
	rv := staticSrc(t, "reviews.js")
	for _, want := range []string{
		`state.previewReviews`,
		`back.kind === "preview"`,
		`window.__ggOpenPreviewForPair`,
		`back.kind === "pair"`,
		`runLinkCompare("a=" + encodeURIComponent(back.a) + "&b=" + encodeURIComponent(back.b))`,
		`d.older ? " · older tip" : ""`,
	} {
		if !strings.Contains(rv, want) {
			t.Errorf("reviews.js lacks %q", want)
		}
	}
	pv := staticSrc(t, "previews.js")
	for _, want := range []string{`window.__ggOpenPreviewForPair = openPreviewForPair`, `state.previewReviews = d.reviews || []`} {
		if !strings.Contains(pv, want) {
			t.Errorf("previews.js lacks %q", want)
		}
	}
	fs := staticSrc(t, "files.js")
	for _, want := range []string{`state.previewReviews = d.reviews || []`, `previewBack(`} {
		if !strings.Contains(fs, want) {
			t.Errorf("files.js lacks %q", want)
		}
	}
	lv := staticSrc(t, "live.js")
	if !strings.Contains(lv, `await Promise.all([refreshNoteCounts(), fetchPreviews()])`) {
		t.Error("live.js: a review hint does not re-read the counts and previews first")
	}
}

// Review Focus 4: a scoped pair (one review's range) gets no block. A PR
// diff gets one since R4 (its stored reviews ride /api/pr/notes).
func TestReviewRowsHTMLSkipsScopedPair(t *testing.T) {
	t.Parallel()
	rv := staticSrc(t, "reviews.js")
	if !strings.Contains(rv, `function previewScopeReviews()`) || !strings.Contains(rv, `p.scope`) || strings.Contains(rv, `po.pr ? []`) {
		t.Error("reviews.js: previewScopeReviews must refuse a scoped pair and keep a PR's reviews")
	}
}

// Final-review fixes, each proven in a browser (the playwright probe); these
// pin the wiring so a refactor cannot quietly drop one.
func TestPreviewReviewsFinalReviewWiring(t *testing.T) {
	t.Parallel()
	css := staticSrc(t, "style.css")
	if !strings.Contains(css, "#previews-list li.brev {") && !strings.Contains(css, "#previews-list li.brev,") && !strings.Contains(css, ", #previews-list li.brev {") {
		t.Error("style.css: a preview's review sub-row is not styled as a sub-row")
	}
	rv := staticSrc(t, "reviews.js")
	if !strings.Contains(rv, "return previewScopeReviews()\n    .concat(commitReviewList())") {
		t.Error("reviews.js: the opened preview's review rows are not keyboard rows (headRowIds)")
	}
	if !strings.Contains(rv, "previewBack(state.reviewSel) || reviewBackFromCommit(state.reviewSel)") {
		t.Error("reviews.js: enter on a preview's review row does not return to the preview")
	}
	pv := staticSrc(t, "previews.js")
	if !strings.Contains(pv, "state.previewReviews = row.reviews || [];") {
		t.Error("previews.js: an open preview's Reviews block does not follow a refresh")
	}
}
