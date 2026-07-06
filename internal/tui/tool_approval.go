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
// confToolApprove render, and the commit-popup generate lane's approveBox
// in commit_generate.go). The alternative — folding a generic header into
// this function so it matched the (command, width) signature literally —
// was rejected because it would have dropped the "(Name)" parenthetical and
// changed the conflict lane's rendered box; keeping the header at each call
// site keeps that box byte-identical to before the extraction. Consequence:
// width is unused here today (popupBox's own truncation already makes any
// internal wrapping redundant) — kept for signature symmetry and any future
// header-aware variant a caller might need; if one ever does, add it as a
// second small function rather than changing this one.
func approvalBoxView(command string, width int) string {
	_ = width
	return command + "\n\n" +
		"Approval is remembered for this repo until the command text changes.\n" +
		"[enter] run  [esc] cancel"
}
