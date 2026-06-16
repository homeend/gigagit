package tui

import "testing"

// A stale transient status message (e.g. an error from a failed op) must clear
// on the next key interaction when idle — otherwise it lingers across
// navigation and reloads forever.
func TestStatusMsgClearsOnNextKeyWhenIdle(t *testing.T) {
	m := markModel()
	m.statusMsg = "error: 'feat/x' is already checked out at ..."
	updated, _ := m.Update(keyMsg("tab")) // benign navigation, sets no message
	m = updated.(Model)
	if m.statusMsg != "" {
		t.Fatalf("stale statusMsg should clear on the next key, got %q", m.statusMsg)
	}
}

// While an operation is running, the "working…" notice must survive a stray
// keypress (the clear is gated on idle).
func TestStatusMsgSurvivesKeyWhileRunning(t *testing.T) {
	m := markModel()
	m.running = true
	m.statusMsg = "working…"
	updated, _ := m.Update(keyMsg("tab"))
	m = updated.(Model)
	if m.statusMsg != "working…" {
		t.Fatalf("running status should survive a keypress, got %q", m.statusMsg)
	}
}
