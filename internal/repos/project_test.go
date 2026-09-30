package repos

import (
	"strings"
	"testing"
)

// A checkout groups with its worktree through the common dir, a remote name
// bridges clones (pulling in a remote-less worktree of one of them), and an
// entry with neither stays alone. Labels: the remote, else the checkout dir.
func TestProjectsAndGroup(t *testing.T) {
	t.Parallel()
	entries := []Entry{
		{Path: "/r/t1", Remote: NoRemote},
		{Path: "/r/lone", Remote: ""},
		{Path: "/r/t1.worktrees/a"},
		{Path: "/r/clone", Remote: "proj"},
		{Path: "/r/clone-wt"},
		{Path: "/r/clone2", Remote: "proj"},
	}
	common := map[string]string{
		"/r/t1": "/r/t1/.git", "/r/t1.worktrees/a": "/r/t1/.git",
		"/r/clone": "/r/clone/.git", "/r/clone-wt": "/r/clone/.git", "/r/clone2": "/r/clone2/.git",
	}
	proj := Projects(entries, common)
	if _, ok := proj["/r/lone"]; ok {
		t.Error("an entry with no common dir and no remote must have no project")
	}
	if a, b := proj["/r/t1"], proj["/r/t1.worktrees/a"]; a.Key != b.Key || a.Label != "t1" {
		t.Errorf("checkout + worktree = %+v / %+v, want one project labelled t1", a, b)
	}
	if a, b, c := proj["/r/clone"], proj["/r/clone-wt"], proj["/r/clone2"]; a.Key != b.Key || b.Key != c.Key || a.Label != "proj" {
		t.Errorf("clones = %+v %+v %+v, want one project labelled proj", a, b, c)
	}
	var got []string
	for _, e := range Group(entries, proj) {
		got = append(got, e.Path)
	}
	want := "/r/t1 /r/t1.worktrees/a /r/lone /r/clone /r/clone-wt /r/clone2"
	if strings.Join(got, " ") != want {
		t.Errorf("Group = %v, want %s", got, want)
	}
	if commonDirLabel("/srv/proj.git") != "proj" || commonDirLabel("/r/m/.git/modules/sub") != "sub" {
		t.Error("a bare repo / submodule is named after its own directory")
	}
}
