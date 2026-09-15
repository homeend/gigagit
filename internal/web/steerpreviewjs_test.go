package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The preview landing spans two modules with top-level imports on both sides,
// so there is no pure slice to run under node (see linksjs_test.go's
// convention). Pin the wiring by source assertion instead — every string below
// exists ONLY after this feature, so none of them can pass on the old file.
func TestSteerPreviewJSIsWired(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"previews.js", "export async function openPreviewForPair", "the pair opener must be exported for live.js"},
		{"previews.js", "await openPreviewEntry(e, \"\")", "the saved row takes the record path"},
		{"previews.js", "await openOnce(source, target,", "an unsaved pair falls back to the transient open"},
		{"live.js", "openPreviewForPair", "the navigate handler must call it"},
		{"live.js", `from "./previews.js"`, "previews.js must be imported in live.js"},
		{"live.js", `s.state === "preview"`, "the navigate handler must branch on the preview target"},
		{"live.js", "s.source, s.target", "the pair rides the wire, not a sha"},
	}
	src := map[string]string{"previews.js": read("previews.js"), "live.js": read("live.js")}
	for _, c := range checks {
		if !strings.Contains(src[c.file], c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	// The import must be on the EXISTING previews.js import line (live.js
	// already imports fetchPreviews/reopenPreviewIfMoved from it) — a second
	// import statement for the same module is a lint smell and easy to strand.
	if n := strings.Count(src["live.js"], `from "./previews.js"`); n != 1 {
		t.Errorf("live.js imports ./previews.js %d times, want exactly 1", n)
	}
}
