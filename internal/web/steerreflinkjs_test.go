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
		{"await fetchBranches()", "resolveRefTip must re-fetch at apply time, not read a stale cache"},
		{"state.branches || []", "resolveRefTip must reuse the branch list"},
		{"state.tags || []", "resolveRefTip must reuse the tag list"},
		// Review round 1: openCompareForPair must send a NAME half straight
		// through the plain /api/compare?a=&b= lane, never resolve it
		// client-side off the sidebar's own abbreviated rows — the server
		// (compare.go's compareNamedEndpoint) resolves branches, tags AND
		// remote-tracking branches to a FULL sha itself now. Only a pair of
		// BARE SHAS (the one shape the plain lane cannot express — it
		// resolves names, not raw hex ids) goes through revs=1.
		{"function isHexLike(", "openCompareForPair must detect a bare-sha half itself"},
		{"isHexLike(a) && isHexLike(b)", "only a pair of bare shas may take the hex lane"},
		{"await openCompare(a, b, { revs: 1,", "a bare-sha pair must go through the hex lane"},
		{"await openCompare(a, b);\n}", "an ordinary name pair must go straight through the plain lane, unresolved"},
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

// TestResolveRefTipCoversRemotesAndSaysSoOnAMiss pins two things the Task 4
// review found missing, both in the same three lines.
//
// `@ref:<name>` is NOT restricted to a local branch or a tag: model.LinkRefOK
// forbids only the grammar's separators and whitespace, and
// domain.finishLink's ref arm resolves the name with a bare ResolveRev — so
// `@ref:origin/main` is a legal, resolvable link everywhere EXCEPT here, where
// resolveRefTip looked in state.branches and state.tags alone and returned "".
//
// And a "" used to `return` in silence, leaving a page that looked as though
// nothing had been asked of it. A swallowed failure that renders a plausible
// screen is this feature family's signature bug: two of Task 4's own defects
// were 4xx responses the page dropped on the floor (ruling S7).
func TestResolveRefTipCoversRemotesAndSaysSoOnAMiss(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "live.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, c := range []struct{ want, why string }{
		{"state.remotes || []", "resolveRefTip must look in state.remotes: @ref:origin/main is a legal link"},
		{`opLine("gg link: cannot place "`, "a ref this page cannot place must SAY so, never return in silence"},
		{"loadRepo, opLine", "opLine must be imported from ops.js for that message"},
	} {
		if !strings.Contains(src, c.want) {
			t.Errorf("live.js is missing %q — %s", c.want, c.why)
		}
	}
	// The miss must report BEFORE it returns: a bare `if (!sha) return;` is
	// the exact shape this test exists to forbid.
	i := strings.Index(src, "const sha = await resolveRefTip(s.ref);")
	if i < 0 {
		t.Fatal("the ref arm no longer calls resolveRefTip")
	}
	rest := src[i:]
	if j := strings.Index(rest, "opLine("); j < 0 || j > strings.Index(rest, "openCommitByHash") {
		t.Error("the ref arm must report an unplaceable ref before it opens anything")
	}
}
