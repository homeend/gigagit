package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The pre-push tag check is a network round trip (ls-remote, bounded at 5s
// server-side). Against a slow remote the page showed NOTHING for that whole
// window — no status line, an enabled button, and a second click fanning a
// second ls-remote — which read as "push does nothing" (user report: pushed,
// created an annotated tag, hit push, nothing; reloaded, hit push, got the
// tag prompt). The TUI says "checking remote tags…" for the same window.
// doPush must: say so, hold the button, and drop a repeat start onto the
// running check via the shared runOnce gate.
func TestPushTagCheckClientShowsItsWait(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("static", "ops.js"))
	if err != nil {
		t.Fatal(err)
	}
	body := funcBody(t, string(src), "async function doPush()")
	for _, want := range []string{
		`runOnce("push-check"`,              // one check in flight, a repeat press is dropped onto it
		`opLine("⟳ checking remote tags…")`, // the TUI's status line, word for word
		`$("push-btn").disabled = true`,     // the button is held for the check
		`$("push-btn").disabled = false`,    // and released when it settles
	} {
		if !strings.Contains(body, want) {
			t.Errorf("doPush lacks %s:\n%s", want, body)
		}
	}
	// The check must be started BEFORE the status line goes up: a dropped
	// repeat start must not re-announce the wait as if it were its own.
	if i, j := strings.Index(body, `runOnce("push-check"`), strings.Index(body, `disabled = true`); i > j {
		t.Errorf("doPush disables the button before the gate decides whether this press starts a check")
	}
}

// funcBody returns the text of the top-level function whose header starts
// with head, up to the closing brace at column 0.
func funcBody(t *testing.T, src, head string) string {
	t.Helper()
	i := strings.Index(src, head)
	if i < 0 {
		t.Fatalf("ops.js: %q not found", head)
	}
	j := strings.Index(src[i:], "\n}\n")
	if j < 0 {
		t.Fatalf("ops.js: %q has no closing brace at column 0", head)
	}
	return src[i : i+j]
}
