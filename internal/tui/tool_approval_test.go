package tui

import "testing"

// TestToolApprovalRoundTrip exercises the shared predicate/remember pair used
// by both the conflict lane and the (upcoming) commit-message lane: a fresh
// command is unapproved, remembering it approves it, and editing the command
// text (a different hash) re-prompts.
func TestToolApprovalRoundTrip(t *testing.T) {
	t.Parallel()
	m, _ := promptTestModel(t) // temp promptstate store — never the real machine file
	const cmd = `claude -p "x"`
	if m.toolCommandApproved(cmd) {
		t.Fatal("fresh command must be unapproved")
	}
	m.rememberToolApproval(cmd)
	if !m.toolCommandApproved(cmd) {
		t.Fatal("remembered command must be approved")
	}
	if m.toolCommandApproved(cmd + " --edited") {
		t.Fatal("edited text must re-prompt")
	}
}

// TestToolCommandApprovedNilStore covers the nil-promptStore case (a
// no-state-dir install): the predicate must fail closed, never panic.
func TestToolCommandApprovedNilStore(t *testing.T) {
	t.Parallel()
	var m Model
	m.promptStore = nil
	if m.toolCommandApproved("anything") {
		t.Fatal("nil promptStore must report unapproved")
	}
	m.rememberToolApproval("anything") // must be a no-op, not a panic
}
