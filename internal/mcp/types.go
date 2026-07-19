package mcp

// RepoInfo identifies which repository answered — every tool reply carries it
// so an agent juggling projects can sanity-check the target.
type RepoInfo struct {
	CommonDir string `json:"common_dir"`
	Worktree  string `json:"worktree"`
}
