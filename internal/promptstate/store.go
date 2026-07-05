// Package promptstate is gigagit's machine-local memory of dismissed UX
// prompts: which related-option follow-up prompts the user never wants to see
// again (global — a prompt you never want is never wanted in any repo),
// which health notices they dismissed per repo (consumed by the notification
// center), and which external-tool commands they approved per repo. It is
// pure UX state with no git semantics: the TUI owns it directly (like the
// operation log), it is NOT config (no .gg.toml / settingDocs plumbing), and
// it lives in one TOML file under the gg state dir.
package promptstate

// Store persists prompt suppressions and notice dismissals. Safe for
// sequential use by one process; writes read-merge then atomically rewrite,
// so the common interleaved case does not lose a sibling's records.
type Store interface {
	// SuppressedPrompts returns the globally suppressed prompt ids.
	SuppressedPrompts() map[string]bool
	// SuppressPrompt records id as never-ask-again (idempotent) and persists.
	SuppressPrompt(id string) error
	// DismissedNotices returns the notice ids dismissed for repoKey.
	DismissedNotices(repoKey string) map[string]bool
	// DismissNotice records a per-repo notice dismissal (idempotent) and persists.
	DismissNotice(repoKey, id string) error
	// ApprovedToolCommands returns the external-tool command hashes approved
	// for repoKey (first-run approval memory; hash = truncated sha256 of the
	// command template text, so any edit re-prompts).
	ApprovedToolCommands(repoKey string) map[string]bool
	// ApproveToolCommand records a per-repo command approval (idempotent) and persists.
	ApproveToolCommand(repoKey, hash string) error
}
