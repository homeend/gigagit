package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The hint reveal (Task 6) spans live.js and sidebar.js with top-level
// imports on both sides, so there is no pure slice to run under node — the
// same reason steerreflinkjs_test.go and steerpreviewjs_test.go pin their
// features by source assertion instead of execution. Every string below
// exists ONLY after this feature, so none of them can pass on the old file.
func TestSteerHintJSIsWired(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	live := read("live.js")
	sidebar := read("sidebar.js")

	// steerNavigate must be a thin wrapper that calls the renamed landing
	// function and THEN reveals the hint — unconditionally, never from
	// inside the landing function, which has several early `return`s (the
	// same S2 trap named one file over in domain's finishLink).
	liveChecks := []struct{ want, why string }{
		{"async function steerNavigate(s) {\n  await steerNavigateLand(s);\n  if (s.hint_kind) revealHintEntry(s.hint_kind, s.hint_id);\n}", "steerNavigate must land THEN reveal, unconditionally"},
		{"async function steerNavigateLand(s) {", "the old steerNavigate body must be renamed, not duplicated"},
		{"revealHintEntry } from \"./sidebar.js\"", "live.js must import revealHintEntry from sidebar.js"},
	}
	for _, c := range liveChecks {
		if !strings.Contains(live, c.want) {
			t.Errorf("live.js: missing %q — %s", c.want, c.why)
		}
	}
	// The reveal call must not also appear inside the (renamed) landing
	// function's own several branches — it belongs to the wrapper alone.
	if n := strings.Count(live, "revealHintEntry("); n != 1 {
		t.Errorf("live.js calls revealHintEntry %d times, want exactly 1 (the wrapper, never inside the landing)", n)
	}

	sidebarChecks := []struct{ want, why string }{
		{"function revealHintEntry(kind, id) {", "sidebar.js must define the reveal"},
		{`kind === "bookmark" ? "bookmarks-list" : kind === "shelf" ? "shelf-list" : null`, "the closed set is bookmark/shelf; anything else (stash) has no list to search"},
		{`querySelector('li[data-id="' + CSS.escape(id) + '"]')`, "the row lookup must escape the id (parseLinkHint permits a doublequote)"},
		{"revealHintEntry", "revealHintEntry must be exported"},
	}
	for _, c := range sidebarChecks {
		if !strings.Contains(sidebar, c.want) {
			t.Errorf("sidebar.js: missing %q — %s", c.want, c.why)
		}
	}
	if !strings.Contains(sidebar, "export {") || !strings.Contains(sidebar, "revealHintEntry,") && !strings.Contains(sidebar, ", revealHintEntry") {
		t.Error("sidebar.js: revealHintEntry must appear on the export line")
	}

	// steer.go's flattened wire must carry hint_kind/hint_id so live.js's
	// s.hint_kind/s.hint_id have something to read (toSteerWire itself is
	// exercised directly by steer_test.go; this only pins the JSON keys the
	// JS depends on).
	steerSrc, err := os.ReadFile("steer.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`json:"hint_kind,omitempty"`, `json:"hint_id,omitempty"`} {
		if !strings.Contains(string(steerSrc), want) {
			t.Errorf("steer.go: missing steerWire field tag %q", want)
		}
	}
}
