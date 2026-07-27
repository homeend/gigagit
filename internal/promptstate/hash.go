package promptstate

import (
	"crypto/sha256"
	"encoding/hex"
)

// CommandHash is the key format for first-run external-tool approvals: a
// truncated sha256 of the command TEMPLATE text (not the per-run resolved
// values), so approving once covers every run until the config text changes.
//
// It lives here, beside the store that keys on it, because more than one
// frontend records approvals — the TUI's conflict/commit-message/review lanes
// and the web's review lane — and they cannot import each other. Both call
// this, so an approval granted in either is honoured by the other; a private
// copy in each would silently split the two the moment one drifted.
func CommandHash(command string) string {
	sum := sha256.Sum256([]byte(command))
	return hex.EncodeToString(sum[:])[:16]
}
