// Package pusherr classifies git push-rejection stderr. It is pure (no git/TUI
// deps) so both the engine's recovery trigger and the TUI's status-bar message
// match against one source of truth and cannot drift.
package pusherr

import "strings"

// IsNonFastForward reports whether errText is a push rejected because the remote
// branch has commits the local tip lacks — the recoverable case.
func IsNonFastForward(errText string) bool {
	low := strings.ToLower(errText)
	return strings.Contains(low, "non-fast-forward") ||
		strings.Contains(low, "fetch first") ||
		strings.Contains(low, "tip of your current branch is behind")
}

// IsStaleInfo reports whether errText is a --force-with-lease rejection because
// the remote moved since the last fetch.
func IsStaleInfo(errText string) bool {
	return strings.Contains(strings.ToLower(errText), "stale info")
}

// IsHookRejection reports whether errText is a server-side rejection (protected
// branch or pre-receive hook), which pull/rebase cannot fix.
func IsHookRejection(errText string) bool {
	low := strings.ToLower(errText)
	return strings.Contains(low, "pre-receive hook declined") ||
		strings.Contains(low, "protected branch") ||
		strings.Contains(low, "[remote rejected]")
}
