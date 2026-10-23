package workspace

import (
	"errors"
	"path/filepath"
	"testing"
)

// An entry-level edit of a workspace another gg deleted meanwhile does not
// bring it back: Edit reports it gone and leaves the file as it is.
func TestEditOfAGoneWorkspaceDoesNotCreateIt(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "workspaces.toml")
	if err := Save(path, []Entry{{Name: "keep", Repos: []string{"/r/keep"}}}); err != nil {
		t.Fatal(err)
	}
	ws, err := Edit(path, "shop", func(e *Entry) { _ = e.Add("/r/shop") })
	if !errors.Is(err, ErrNoSuchWorkspace) {
		t.Fatalf("err = %v, want ErrNoSuchWorkspace", err)
	}
	if len(ws) != 1 || len(Load(path)) != 1 {
		t.Fatalf("the gone workspace was created: %+v / %+v", ws, Load(path))
	}
}

// Rename of a workspace that is gone creates nothing.
func TestRenameOfAGoneWorkspaceDoesNotCreateIt(t *testing.T) {
	t.Parallel()
	ws := Rename([]Entry{{Name: "keep"}}, "shop", "mall")
	if len(ws) != 1 {
		t.Fatalf("Rename created an empty workspace: %+v", ws)
	}
}
