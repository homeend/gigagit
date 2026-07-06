package tui

// tool_approval.go holds the first-run approval primitives shared by every
// external-tool lane: the conflict process (Stage 1) and the commit-message
// lane (Stage 2). Each lane keeps its own state machine — the conflict
// process preempts the layer stack while a window is open, so a pushed
// approval layer would be unreachable — but the predicate, the remember, and
// the box render are identical, so they live here once instead of forking.

// toolCommandApproved reports whether command (a resolved-command template's
// text, hashed by toolCommandHash) has already been approved for the current
// repo. A nil promptStore (no state dir) fails closed: never approved.
func (m Model) toolCommandApproved(command string) bool {
	if m.promptStore == nil {
		return false
	}
	return m.promptStore.ApprovedToolCommands(m.toolRepoKey())[toolCommandHash(command)]
}

// rememberToolApproval persists that command is approved for the current
// repo, so future runs of the same template text skip the approval gate. A
// nil promptStore is a no-op — there is nowhere to persist to.
func (m Model) rememberToolApproval(command string) {
	if m.promptStore == nil {
		return
	}
	_ = m.promptStore.ApproveToolCommand(m.toolRepoKey(), toolCommandHash(command))
}

// approvalBoxView renders the shared body of the first-run approval preview:
// the fully resolved command, the per-repo remember note, and the Run/Cancel
// hint. It does NOT include a lane-specific header line or the popupBox
// border/width wrap — the conflict lane's header names the chosen tool
// ("Run this command?  (Name)"), a piece of state this function has no way
// to receive under the (command, width) signature, so the header and the
// single popupBox wrap stay owned by each call site (conflict_process.go's
// confToolApprove render); this keeps the conflict lane's rendered box
// byte-identical to before the extraction. width is accepted for signature
// symmetry with the brief and future callers that may want to wrap width-
// aware content here; popupBox's own truncation already makes it unused
// today — see task-5-report.md for the extraction-residue note.
func approvalBoxView(command string, width int) string {
	_ = width
	return command + "\n\n" +
		"Approval is remembered for this repo until the command text changes.\n" +
		"[enter] run  [esc] cancel"
}
