package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The switch-repo table's ctrl+g grouping: the server hands each row its
// project, and the palette only reorders and names — it must keep reading
// the project from BOTH lanes (the list, then the details that add the
// common dirs), keep the key and the clickable hint wired, and remember the
// choice in the server-side UI state.
func TestRepoPaletteGroupingWired(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	pal := read("palette.js")
	for _, want := range []string{
		"project: r.project || null",    // /api/repos lane
		"r.project = d.project || null", // /api/repos/details lane
		`e.key.toLowerCase() === "g"`,   // ctrl+g
		"saveUI({ repo_grouped: pal.grouped })",
		"state.ui.repo_grouped",       // seeded on open
		"groupRepoRows(pal.filtered)", // filter first, then group
		`button[data-act="group"]`,    // the hint's click target
		`esc(r.nameText ?? r.label)`,  // a member's blank name must not fall back to its own
	} {
		if !strings.Contains(pal, want) {
			t.Errorf("palette.js no longer contains %q", want)
		}
	}
	if !strings.Contains(read("index.html"), `id="palette-hint"`) {
		t.Error("index.html lost the repo table's hint line")
	}
	if !strings.Contains(read("style.css"), "#palette-hint.hidden") {
		t.Error("style.css must hide #palette-hint by id (no global .hidden rule)")
	}
	if !strings.Contains(read("uistate.js"), "repo_grouped: false") {
		t.Error("uistate.js's base record must carry repo_grouped (the endpoint replaces the record)")
	}
}
