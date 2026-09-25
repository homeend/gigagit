package tui

import (
	"encoding/json"
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

// bgDoc registers a loaded background document in m's current worktree.
func bgDoc(m Model, src fileSource, path string, n int) *openFile {
	d := newOpenFile(src, path)
	lines := make([]contentLine, n)
	for i := range lines {
		lines[i] = contentLine{text: "x", src: true}
	}
	d.p.lines = lines
	m.openFiles.touch(m.currentWorktree, d, m.docShown)
	return d
}

func TestOpenFilesProtoListsIDsLinesAndState(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	a := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 10)
	a.p.cur = 4
	c := bgDoc(m, fileSource{kind: srcCommit, rev: "0123456789abcdef0123456789abcdef01234567"}, "b.txt", 3)
	m = m.pushLayer(&fileViewer{c})
	got := m.openFilesProto()
	if len(got) != 2 {
		t.Fatalf("got %+v, want 2 files", got)
	}
	if got[0].Path != "b.txt" || got[0].Source != "commit" || got[0].Rev == "" || got[0].State != "shown" || got[0].ID != c.id() {
		t.Errorf("first = %+v, want the shown commit version of b.txt", got[0])
	}
	if got[1].Path != "a.txt" || got[1].Source != "worktree" || got[1].Line != 5 || got[1].State != "background" {
		t.Errorf("second = %+v, want a.txt in the background at line 5", got[1])
	}
}

func TestFindOpenFileByIDThenPathMostRecentFirst(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	old := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 2)
	recent := bgDoc(m, fileSource{kind: srcCommit, rev: "abc"}, "a.txt", 2)
	if d := m.findOpenFile(old.id(), ""); d != old {
		t.Error("id lookup missed")
	}
	if d := m.findOpenFile("", "a.txt"); d != recent {
		t.Error("a path must pick the most recently shown version")
	}
	if d := m.findOpenFile("a.txt", ""); d != recent {
		t.Error("an id that matches no id must be tried as a path")
	}
	if d := m.findOpenFile("f999999", ""); d != nil {
		t.Error("an unknown id found a document")
	}
}

func TestSnapshotPublishesOpenFiles(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 3)
	data, err := json.Marshal(buildSessionSnapshot(m))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Version   int              `json:"version"`
		OpenFiles []steer.OpenFile `json:"open_files"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.Version != 1 || len(s.OpenFiles) != 1 || s.OpenFiles[0].Path != "a.txt" || s.OpenFiles[0].State != "background" {
		t.Fatalf("snapshot = %s", data)
	}
}
