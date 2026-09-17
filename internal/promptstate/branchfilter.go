package promptstate

import "fmt"

// The two lists a branch filter applies to. Wire names shared with the web
// client and the TUI panels; anything else is refused at the store.
const (
	BranchFilterListBranches = "branches"
	BranchFilterListRemotes  = "remotes"
)

// branchFilterRecord is one repo's active slots: 0 = none. Per repo (the
// git common dir), per list — which branches you want out of the way is a
// property of the repository, unlike the sidebar layout in WebUI.
type branchFilterRecord struct {
	Branches int `toml:"branches"`
	Remotes  int `toml:"remotes"`
}

// BranchFilterSlot returns the active slot for repoKey's list (0 = none).
func (fs *FileStore) BranchFilterSlot(repoKey, list string) int {
	rec, ok := fs.read().BranchFilter[repoKey]
	if !ok {
		return 0
	}
	switch list {
	case BranchFilterListBranches:
		return rec.Branches
	case BranchFilterListRemotes:
		return rec.Remotes
	}
	return 0
}

// SetBranchFilterSlot persists slot (0..5) for repoKey's list.
func (fs *FileStore) SetBranchFilterSlot(repoKey, list string, slot int) error {
	if slot < 0 || slot > 5 {
		return fmt.Errorf("promptstate: branch filter slot %d out of range 0..5", slot)
	}
	r := fs.read()
	rec := r.BranchFilter[repoKey]
	switch list {
	case BranchFilterListBranches:
		rec.Branches = slot
	case BranchFilterListRemotes:
		rec.Remotes = slot
	default:
		return fmt.Errorf("promptstate: unknown branch filter list %q", list)
	}
	r.BranchFilter[repoKey] = rec
	return fs.write(r)
}
