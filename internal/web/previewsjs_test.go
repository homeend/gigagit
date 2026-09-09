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
		{"sidebar.js", `"previews"]`, "COLLAPSED_DEFAULT must fold previews on a first run"},
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
