package web

import (
	"os"
	"path/filepath"
	"regexp"
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
		{"async function steerNavigate(s) {\n  await steerNavigateLand(s);\n  if (s.hint_kind) await revealHintEntry(s.hint_kind, s.hint_id);\n}", "steerNavigate must land THEN reveal, unconditionally"},
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
		{"async function revealHintEntry(kind, id) {", "sidebar.js must define the reveal, and it must be async (fix F1's cross-bucket fallback awaits a fetch)"},
		{`kind === "bookmark" ? "bookmarks-list" : kind === "shelf" ? "shelf-list" : null`, "the closed set is bookmark/shelf; anything else (stash) has no list to search"},
		{`querySelector('li[data-id="' + CSS.escape(id) + '"]')`, "the row lookup must escape the id (parseLinkHint permits a doublequote)"},
		{"revealHintEntry", "revealHintEntry must be exported"},
		// Fix F1: GET /api/shelf lists the default bucket only, but domain's
		// own presence check (ShelfFind) scans every bucket — the same
		// disagreement the TUI had (loadShelfForHintCmd). The fallback tries
		// every OTHER known bucket before reporting an entry gone.
		{"async function findShelfEntryInOtherBuckets(id) {", "sidebar.js must define the F1 cross-bucket fallback"},
		{"li = await findShelfEntryInOtherBuckets(id);", "revealHintEntry must call the fallback on a shelf miss"},
		{"state.shelfBuckets = sh.buckets || [];", "fetchBranches must keep the bucket name list the fallback iterates"},
		{`getJSON("/api/shelf?bucket=" + encodeURIComponent(name))`, "the fallback must reuse the EXISTING /api/shelf?bucket= endpoint, not invent a new one"},
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

// TestRevealHintEntryTargetsHaveAFlashRule is fix F7's GO GATE (a comment
// alone does not stop the next reveal on a sixth list from repeating this):
// every list id revealHintEntry (sidebar.js) can pass to $(listName) must
// have a matching `li.flash` rule in style.css, or the reveal unfolds and
// scrolls correctly and then paints nothing — exactly what the controller's
// browser pass caught (a `#bookmarks-list`/`#shelf-list` row painted
// rgba(0,0,0,0) beside a `#branches-list` row painting rgb(42,53,80) on the
// SAME page). This reads BOTH files and fails on either: a targetable list
// with no rule, or (defensively) a rule for a list revealHintEntry can no
// longer reach.
func TestRevealHintEntryTargetsHaveAFlashRule(t *testing.T) {
	t.Parallel()
	sidebar, err := os.ReadFile(filepath.Join("static", "sidebar.js"))
	if err != nil {
		t.Fatal(err)
	}
	css, err := os.ReadFile(filepath.Join("static", "style.css"))
	if err != nil {
		t.Fatal(err)
	}

	i := strings.Index(string(sidebar), "function revealHintEntry(kind, id) {")
	if i < 0 {
		t.Fatal("sidebar.js: revealHintEntry is gone")
	}
	j := strings.Index(string(sidebar)[i:], "\n}\n")
	if j < 0 {
		t.Fatal("sidebar.js: could not find the end of revealHintEntry")
	}
	body := string(sidebar)[i : i+j]

	listIDRe := regexp.MustCompile(`"([a-z]+-list)"`)
	matches := listIDRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		t.Fatal("sidebar.js: revealHintEntry names no *-list id — this test's extraction regex is stale")
	}
	seen := map[string]bool{}
	var ids []string
	for _, m := range matches {
		id := m[1]
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	// The set this task shipped, pinned so a change to either side is
	// caught: growing revealHintEntry's targets without touching this list
	// (or vice versa) means the extraction or the fixture drifted.
	wantIDs := []string{"bookmarks-list", "shelf-list"}
	if len(ids) != len(wantIDs) {
		t.Fatalf("revealHintEntry targets %v, want exactly %v", ids, wantIDs)
	}
	for _, id := range wantIDs {
		if !seen[id] {
			t.Fatalf("revealHintEntry targets %v, want %v among them", ids, wantIDs)
		}
	}

	flashRuleRe := regexp.MustCompile(`#([a-z]+-list)\s+li\.flash\b`)
	haveFlash := map[string]bool{}
	for _, m := range flashRuleRe.FindAllStringSubmatch(string(css), -1) {
		haveFlash[m[1]] = true
	}
	for _, id := range ids {
		if !haveFlash[id] {
			t.Errorf("style.css has no `#%s li.flash` rule — revealHintEntry can target this list and flash it invisibly", id)
		}
	}
}
