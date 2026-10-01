package repos

import (
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// The switcher's grouped view (ctrl+g in the TUI's R, the web palette's repo
// mode): which registry entries are checkouts of one project, and the order
// that puts them together. Shared so both frontends group identically.

// Project is an entry's project in the switcher's grouped view: Key tells
// projects apart, Label names one on its head row.
type Project struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Projects maps each entry's path to its project. Two entries are one project
// when they share a git common dir (a checkout and its linked worktrees) or a
// remote repository name (separate clones).
// It is computed over ALL entries, so a filter hiding the entry that bridges
// two others never splits their group. An entry with neither — an unprobed
// entry with no usable remote — is absent and never joins a group. The label
// is a member's remote name, else the main checkout's directory name. common
// maps an entry's path to its git common dir as the caller read it (this
// package knows nothing about git); a missing path means not known (yet).
func Projects(entries []Entry, common map[string]string) map[string]Project {
	parent := make([]int, len(entries))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	has := make([]bool, len(entries))
	firstBy := make(map[string]int) // "c:"+common dir / "r:"+remote → first entry
	join := func(i int, key string) {
		has[i] = true
		if j, ok := firstBy[key]; ok {
			if ri, rj := find(i), find(j); ri != rj {
				parent[max(ri, rj)] = min(ri, rj) // the MRU-earliest stays root
			}
			return
		}
		firstBy[key] = i
	}
	for i, e := range entries {
		if c := common[e.Path]; c != "" {
			if runtime.GOOS == "windows" {
				c = strings.ToLower(c) // drive-letter and path case vary
			}
			join(i, "c:"+c)
		}
		if e.Remote != "" && e.Remote != NoRemote {
			join(i, "r:"+e.Remote)
		}
	}
	label := make(map[int]string)
	for i, e := range entries { // a remote name wins, in MRU order
		if r := find(i); has[i] && label[r] == "" && e.Remote != "" && e.Remote != NoRemote {
			label[r] = e.Remote
		}
	}
	for i, e := range entries {
		if r := find(i); has[i] && label[r] == "" {
			label[r] = commonDirLabel(common[e.Path])
		}
	}
	out := make(map[string]Project, len(entries))
	for i, e := range entries {
		if has[i] {
			r := find(i)
			out[e.Path] = Project{Key: strconv.Itoa(r), Label: label[r]}
		}
	}
	return out
}

// commonDirLabel names a project after its git common dir: the main
// checkout's directory for <checkout>/.git, the repository's own name for a
// bare <name>.git or a submodule's .git/modules/<name>.
func commonDirLabel(dir string) string {
	if base := filepath.Base(dir); base != ".git" {
		return strings.TrimSuffix(base, ".git")
	}
	return filepath.Base(filepath.Dir(dir))
}

// Group reorders MRU-sorted entries so each project's checkouts sit
// together: the first entry of a project heads its group and the project's
// later entries move up under it, in their MRU order. Groups are therefore
// ordered by their most recently opened checkout. The input is not modified.
func Group(entries []Entry, proj map[string]Project) []Entry {
	out := make([]Entry, 0, len(entries))
	seen := make(map[string]bool)
	for i, e := range entries {
		pr, ok := proj[e.Path]
		if !ok {
			out = append(out, e)
			continue
		}
		if seen[pr.Key] {
			continue // already pulled up under its head
		}
		seen[pr.Key] = true
		out = append(out, e)
		for _, later := range entries[i+1:] {
			if lp, ok := proj[later.Path]; ok && lp.Key == pr.Key {
				out = append(out, later)
			}
		}
	}
	return out
}
