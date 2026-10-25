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
