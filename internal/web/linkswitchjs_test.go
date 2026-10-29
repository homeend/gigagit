package web

import (
	"strings"
	"testing"
)

// # with a link into another checkout asks to switch there, the TUI's
// confirm (spec 2026-10-29-web-link-switch): enter switches (the server arms
// the landing, the page's own switch lane goes there), esc stays.
func TestGotoLinkAsksToSwitchJS(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, want, why string }{
		{"live.js", `showLocalConfirm("that link names another checkout: " + body.checkout, ["switch there", "stay"]`, "the TUI's words; switch first (enter)"},
		{"live.js", `postJSON("/api/link-switch", { link })`, "the server re-resolves and arms the landing"},
		{"live.js", `doReroot(ans.checkout)`, "the page's own switch lane"},
		{"live.js", `if (opBusy()) return opLine("another operation is running — switch when it ends", true);`, "a busy page says so, before anything is armed"},
		{"ops.js", `const ESCAPE_WORDS = /^(abort|cancel|no|stop|keep|skip|stay)$/i;`, "esc answers stay"},
	} {
		if !strings.Contains(staticSrc(t, c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	if strings.Contains(staticSrc(t, "live.js"), "open gg web there") {
		t.Error("live.js still sends the user to open gg web there")
	}
}
