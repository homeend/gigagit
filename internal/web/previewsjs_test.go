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
