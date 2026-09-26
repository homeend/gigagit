// Package promptstate is gigagit's machine-local memory of dismissed UX
// prompts: which related-option follow-up prompts the user never wants to see
// again (global — a prompt you never want is never wanted in any repo),
// which health notices they dismissed per repo (consumed by the notification
// center), which external-tool commands they approved per repo, and the
// active branch-filter slot per repo and list. It is pure UX state with no
// git semantics: the TUI owns it directly (like the operation log), it is
// NOT config (no .gg.toml / settingDocs plumbing), and it lives in one TOML
// file under the gg state dir.
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
	// BranchFilterSlot returns repoKey's active branch-filter slot for list
	// ("branches" | "remotes"); 0 = none.
	BranchFilterSlot(repoKey, list string) int
	// SetBranchFilterSlot persists it (0 clears).
	SetBranchFilterSlot(repoKey, list string, slot int) error
	// StackedDiff reports whether the TUI diff view opens stacked (the S key).
	// Machine-global and independent of the web's own pref.
	StackedDiff() bool
	// SetStackedDiff persists it.
	SetStackedDiff(on bool) error
	// TaskLaunchChoice returns the AI-task launch dialog's last choice for kind.
	TaskLaunchChoice(kind string) (TaskLaunch, bool)
	// SetTaskLaunchChoice persists it.
	SetTaskLaunchChoice(kind string, c TaskLaunch) error
}
