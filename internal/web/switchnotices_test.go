package web

import (
	"strings"
	"testing"
)

// After an in-place switch: the notices are read once the new status has
// landed (beside the scan they caught its index.lock); the console follows
// the panes when a bar appears above them; the big-repository message is
// cut with … and whole in its tooltip.
func TestSwitchNoticesAndBars(t *testing.T) {
	t.Parallel()
	ops := readStaticSrc(t, "ops.js")
	if strings.Contains(ops, "    fetchNotifications(),\n") {
		t.Error("ops.js: softSwitch must not read the notices beside the status scan")
	}
	if !strings.Contains(ops, "fetchNotifications(); // after the status scan") {
		t.Error("ops.js: softSwitch reads the notices after the status lands")
	}
	if !strings.Contains(readStaticSrc(t, "console.js"), `ro.observe($("panes")); // a bar appearing above the panes moves them`) {
		t.Error("console.js: the console follows the panes")
	}
	if !strings.Contains(readStaticSrc(t, "bigrepo.js"), `$("bigrepo-msg").title = msg;`) {
		t.Error("bigrepo.js: the whole message is the tooltip")
	}
	if !strings.Contains(readStaticSrc(t, "style.css"), "#bigrepo-msg { flex: 0 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }") {
		t.Error("style.css: the message is cut with …")
	}
}

// Review fixes: a lock an op left behind (a cancelled git killed after its
// grace) is looked at when the op ends — the notice now leaves out a young
// lock, so the bar's second look is what shows it; the bar's buttons may
// wrap their words again (no-wrap pushed the last one off a narrow window).
func TestSwitchNoticesReviewFixes(t *testing.T) {
	t.Parallel()
	if !strings.Contains(readStaticSrc(t, "ops.js"), "fetchLocks(); // an op can leave a lock behind") {
		t.Error("ops.js: the lock bar looks when an op ends")
	}
	if strings.Contains(readStaticSrc(t, "style.css"), "#bigrepo-bar button { flex: none; white-space: nowrap; }") {
		t.Error("style.css: the big-repo buttons may wrap")
	}
}
