package observ

import (
	"encoding/json"
	"os"
	"time"
)

// RepoInfo captures non-secret repository identity for a debug dump.
type RepoInfo struct {
	WorktreePath string `json:"worktree_path"`
	GitDir       string `json:"git_dir,omitempty"`
	Branch       string `json:"branch"`
	Upstream     string `json:"upstream,omitempty"`
	Head         string `json:"head"`
	Sparse       bool   `json:"sparse"`
	PartialClone bool   `json:"partial_clone"`
}

// DumpCounts mirrors model.Counts without importing model (keeps observ a leaf).
type DumpCounts struct {
	Staged     int `json:"staged"`
	Unstaged   int `json:"unstaged"`
	Untracked  int `json:"untracked"`
	Conflicted int `json:"conflicted"`
}

// Dump is the serialisable diagnostic snapshot. It deliberately contains NO
// file contents or diffs — only counts, identity, and the recent-span buffer.
type Dump struct {
	GeneratedAt time.Time  `json:"generated_at"`
	GGVersion   string     `json:"gg_version"`
	GitVersion  string     `json:"git_version"`
	OS          string     `json:"os,omitempty"`
	Arch        string     `json:"arch,omitempty"`
	Repo        RepoInfo   `json:"repo"`
	WorkingTree DumpCounts `json:"working_tree"`
	Recent      []Span     `json:"recent"`
	Errors      []string   `json:"errors,omitempty"`
}

// WriteDump redacts span args and writes the dump as indented JSON to path.
func WriteDump(path string, d Dump) error {
	for i := range d.Recent {
		d.Recent[i].Args = Redact(d.Recent[i].Args)
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
