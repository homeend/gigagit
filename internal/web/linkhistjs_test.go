package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// copyGGLinkRow matches a context-menu row whose LABEL offers to copy a gg://
// link — "copy gg link", "copy gg link to this line", "copy gg link to this
// note". Whatever the label's tail, the row's action must record.
var copyGGLinkRow = regexp.MustCompile(`label:\s*"copy gg link[^"]*"`)

// copyLinkCall matches the recording helper (or the row factory that wraps
// it), which is what makes a copy land in the ring.
var copyLinkCall = regexp.MustCompile(`\bcopyLink\(|\bcopyLinkRow\(`)

// TestEveryCopyGGLinkRowRecords pins the invariant Task 9 rests on: EVERY
// "copy gg link" action reports the link to /api/linkhist.
//
// This is not hypothetical tidiness. There were five such rows and only three
// went through copyLinkRow — files.js's "to this line" and "to this note"
// rows called copyText directly. Adding the POST to copyLinkRow alone would
// have left two shipped copy actions silently unrecorded, and nothing would
// have failed: the feature would simply have had holes, which is this
// feature family's signature defect (a rule applied to one arm and not its
// twin).
//
// A source assertion is the right shape. The rows live in three modules and
// fire from a DOM click handler, so a runtime test would need the whole page;
// reading the labels catches a SIXTH row added later that forgets to record,
// which is the case that actually matters.
func TestEveryCopyGGLinkRowRecords(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir("static")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		src, err := os.ReadFile(filepath.Join("static", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(src), "\n")
		for i, line := range lines {
			if !copyGGLinkRow.MatchString(line) {
				continue
			}
			found++
			// A row is an object literal, and prettier wraps it across lines
			// whenever it grows — so the `act` that must call copyLink can sit
			// several lines below the `label`. Read the whole statement: the
			// label's line plus the following lines up to the one that closes
			// the row. (Checking only the label's own line is what this gate
			// got wrong first, and it reported both correct rows as broken.)
			end := i + 1
			for end < len(lines) && end <= i+6 {
				if strings.Contains(lines[end-1], "})") || strings.Contains(lines[end-1], "});") {
					break
				}
				end++
			}
			stmt := strings.Join(lines[i:end], "\n")
			if !copyLinkCall.MatchString(stmt) {
				t.Errorf("%s:%d: a %q row does not go through copyLink/copyLinkRow — "+
					"it will copy without recording to /api/linkhist:\n%s",
					e.Name(), i+1, "copy gg link", stmt)
			}
		}
	}
	// If the labels are ever reworded, this gate would quietly guard nothing.
	if found < 3 {
		t.Errorf("found only %d \"copy gg link\" rows — the label changed and this gate has stopped seeing them", found)
	}
}

// TestLinkHistJSIsWired pins the client half of Task 9: the POST, its
// swallowed rejection, the shared Desc vocabulary, and the fact that no
// module keeps the ring in browser storage.
func TestLinkHistJSIsWired(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"links.js", `"/api/linkhist"`, "the copy must report the link it just wrote to the clipboard"},
		{"links.js", `method: "POST"`, "reporting a copy is a POST"},
		{"links.js", `"Content-Type": "application/json"`, "the server's writeGuard requires the JSON content type"},
		{"links.js", ".catch(() => {})", "a history that cannot be recorded must never make the copy look failed"},
		{"links.js", "function linkDesc(", "the Desc vocabulary is shared with internal/cli.linkDesc"},
		// Membership, not the whole export line — see the note in
		// linksjs_test.go: pinning the full spelling only pins the next
		// refactor shut.
		{"links.js", "copyLink,", "copyLink must be exported for files.js"},
		{"links.js", "linkDesc,", "linkDesc must be exported for files.js"},
		{"files.js", "copyLink(", "files.js's two copy rows must record like the rest"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}

	// The ring is SERVED, never stored in the browser: gg web binds a random
	// port every run and browser storage is per-origin, so a client-side ring
	// would vanish on the next start. That is the whole reason /api/linkhist
	// exists rather than a localStorage key.
	for _, f := range []string{"links.js", "files.js"} {
		src := read(f)
		for _, bad := range []string{"gg.linkhist", "linkhist\")", "linkHist\")"} {
			if strings.Contains(src, "localStorage") && strings.Contains(src, bad) {
				t.Errorf("%s: the link ring must not live in browser storage (%q)", f, bad)
			}
		}
	}
}
