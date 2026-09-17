package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The ref/pair landing spans live.js and files.js with top-level imports on
// both sides, so there is no pure slice to run under node (see
// linksjs_test.go's convention; steerpreviewjs_test.go's source-assertion
// twin for the preview arm). Pin the wiring by source assertion instead —
// every string below exists ONLY after this feature, so none of them can
// pass on the old file.
func TestSteerRefPairJSIsWired(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	live := read("live.js")
	checks := []struct{ want, why string }{
		{`s.state === "ref"`, "the navigate handler must branch on the ref target"},
		{`s.state === "pair"`, "the navigate handler must branch on the pair target"},
		{"function resolveRefTip(", "the ref arm must resolve the NAME to a tip itself"},
		{"function openCompareForPair(", "the pair arm must open the two-dot compare itself"},
		{"await resolveRefTip(s.ref)", "the ref arm must call resolveRefTip with the wire's ref"},
		{"await openCompareForPair(s.a, s.b)", "the pair arm must call openCompareForPair with the wire's a/b"},
		{"await openCommitByHash(sha,", "the ref arm must open the RESOLVED sha, not the name"},
		// resolveRefTip must reuse the sidebar's own lists (fetchBranches loads
		// both branches and tags) rather than adding a new endpoint — but it
		// must call fetchBranches ITSELF at apply time (ruling R2), never read
		// whatever the sidebar last happened to cache, or a tip that moved a
		// moment ago would resolve to its OLD hash.
		{"await fetchBranches()", "resolveRefTip/openCompareForPair must re-fetch at apply time, not read a stale cache"},
		{"state.branches || []", "resolveRefTip/resolveCompareSide must reuse the branch list"},
		{"state.tags || []", "resolveRefTip/resolveCompareSide must reuse the tag list"},
		// openCompareForPair must reuse files.js's existing compare renderer
		// (the same one the branch-pair "compare" menu row and the preview
		// arm both call), never a second endpoint. An ORDINARY branch pair
		// must take the plain lane FIRST (the server resolves each name to a
		// FULL sha itself, branchTipEndpoint) — only a half that is NOT a
		// local branch (a tag or a sha, which /api/compare's plain lane
		// refuses by design: compare_test.go's TestCompareRejects pins "a tag
		// or a raw sha is not a local branch") falls to client-side
		// resolution and the revs:1 hex lane.
		{"function isLocalBranch(", "openCompareForPair must gate on BOTH halves being local branches first"},
		{"isLocalBranch(a) && isLocalBranch(b)", "an ordinary branch pair must take the plain lane, not the client-resolved hex one"},
		{"function resolveCompareSide(", "the pair arm must resolve a tag/sha half to a hash itself"},
		{"await openCompare(ah, bh, { revs: 1,", "a resolved non-branch pair must go through the hex lane"},
		{"await openCompare(a, b)", "the branch-pair and unresolved-pair paths both fall back to the plain name lane"},
		{`from "./files.js"`, "files.js must be imported in live.js"},
	}
	for _, c := range checks {
		if !strings.Contains(live, c.want) {
			t.Errorf("live.js: missing %q — %s", c.want, c.why)
		}
	}
	// The import must be on the EXISTING files.js import line — a second
	// import statement for the same module is a lint smell and easy to
	// strand (the same rule steerpreviewjs_test.go pins for previews.js).
	if n := strings.Count(live, `from "./files.js"`); n != 1 {
		t.Errorf("live.js imports ./files.js %d times, want exactly 1", n)
	}
	if !strings.Contains(live, "openCompare,") && !strings.Contains(live, ", openCompare") {
		t.Error("live.js: openCompare must be imported from files.js on the existing import line")
	}

	// steer.go's flattened wire must carry ref/a/b so live.js's s.ref/s.a/s.b
	// have something to read (toSteerWire is exercised directly by
	// steer_test.go; this only pins the JSON keys the JS depends on).
	steerSrc, err := os.ReadFile("steer.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`json:"ref,omitempty"`, `json:"a,omitempty"`, `json:"b,omitempty"`} {
		if !strings.Contains(string(steerSrc), want) {
			t.Errorf("steer.go: missing steerWire field tag %q", want)
		}
	}
}

// The line-less rule (S1: a navigate whose file is set and whose line is
// nil moves no cursor) must keep covering the two new branches — asserted
// rather than assumed, per the task brief.
func TestSteerNavigateLineGuardCoversRefAndPair(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "live.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	i := strings.Index(src, "async function steerNavigate(s) {")
	if i < 0 {
		t.Fatal("live.js: steerNavigate is gone")
	}
	j := strings.Index(src[i:], "\n}\n")
	if j < 0 {
		t.Fatal("live.js: could not find the end of steerNavigate")
	}
	body := src[i : i+j]
	if !strings.Contains(body, `if (!s.line) return;`) {
		t.Fatal(`live.js: steerNavigate lost its "if (!s.line) return;" guard`)
	}
	// Both new branches must fall through to that ONE guard rather than
	// returning early themselves right after opening the file — an early
	// return would make the guard unreachable for ref/pair and the line
	// landing below it dead code for them. Checking that the branch merely
	// PRECEDES the guard is not enough (an early `return;` right after
	// `openFile(i)` would still pass that check), so this also asserts each
	// branch's OWN body ends at `await openFile(i);` with nothing after it.
	for _, st := range []string{"ref", "pair"} {
		marker := `s.state === "` + st + `"`
		bi := strings.Index(body, marker)
		if bi < 0 {
			t.Fatalf("live.js: steerNavigate has no %s branch", marker)
		}
		if bi >= strings.Index(body, `if (!s.line) return;`) {
			t.Fatalf("live.js: the %s branch must precede the shared line guard, not bypass it", marker)
		}
		open := strings.Index(body[bi:], "{")
		if open < 0 {
			t.Fatalf("%s: no opening brace found", marker)
		}
		start := bi + open + 1
		end := strings.Index(body[start:], "} else")
		if end < 0 {
			t.Fatalf("%s: could not find the branch's end", marker)
		}
		branchBody := body[start : start+end]
		lastCall := strings.LastIndex(branchBody, "await openFile(i);")
		if lastCall < 0 {
			t.Fatalf("%s: branch never calls openFile", marker)
		}
		if trailing := strings.TrimSpace(branchBody[lastCall+len("await openFile(i);"):]); trailing != "" {
			t.Errorf("%s: trailing code %q after openFile(i) would bypass the shared line guard", marker, trailing)
		}
	}
}
