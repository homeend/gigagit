package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

// showSvc builds a Service whose `git show` returns content, for the read-only
// open-in-editor surfaces that resolve a file's bytes via ShowFile.
func showSvc(content string) *domain.Service {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git show", gitexec.Result{Stdout: content})
	return domain.New(&git.Repo{Runner: f})
}

// e in the history view opens the selected commit's version of the file.
func TestHistoryOpenExternalResolvesContent(t *testing.T) {
	m := Model{width: 100, height: 30, svc: showSvc("historic content\n")}
	h := histFixture() // path a.go
	m = m.pushLayer(h)
	_, cmd := h.update(m, keyMsg("e"))
	if cmd == nil {
		t.Fatal("e should dispatch an open command")
	}
	msg, ok := cmd().(editorViewMsg)
	if !ok {
		t.Fatalf("want editorViewMsg, got %T", cmd())
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	defer removeTempFile(msg.path)
	if filepath.Base(msg.name) != "a.go" {
		t.Errorf("name = %q, want the file path", msg.name)
	}
	data, _ := os.ReadFile(msg.path)
	if string(data) != "historic content\n" {
		t.Fatalf("temp content = %q", data)
	}
}

// e in the blame view opens the blamed file at its rev.
func TestBlameOpenExternalResolvesContent(t *testing.T) {
	m := Model{width: 100, height: 30, svc: showSvc("blamed content\n")}
	b := &blameView{ctx: navContext{path: "pkg/x.go", rev: "abc123"}}
	m = m.pushLayer(b)
	_, cmd := b.update(m, keyMsg("e"))
	if cmd == nil {
		t.Fatal("e should dispatch an open command")
	}
	msg, ok := cmd().(editorViewMsg)
	if !ok {
		t.Fatalf("want editorViewMsg, got %T", cmd())
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	defer removeTempFile(msg.path)
	data, _ := os.ReadFile(msg.path)
	if string(data) != "blamed content\n" {
		t.Fatalf("temp content = %q", data)
	}
}

// Blame opened on a working-tree file (rev=="") must open the ON-DISK working
// file — not ShowFile("",path), which is `git show :path` (the index blob) and
// would swap content (or error for an unstaged file).
func TestBlameOpenExternalWorkingTreeUsesOnDiskFile(t *testing.T) {
	dir, repo := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "w.go"), []byte("working edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := Model{width: 100, height: 30, svc: domain.New(repo)}
	b := &blameView{ctx: navContext{path: "w.go", rev: ""}} // working-tree blame
	m = m.pushLayer(b)
	_, cmd := b.update(m, keyMsg("e"))
	if cmd == nil {
		t.Fatal("e should dispatch an open command")
	}
	msg, ok := cmd().(editorViewMsg)
	if !ok {
		t.Fatalf("want editorViewMsg, got %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("rev=\"\" blame should resolve the working file, got err: %v", msg.err)
	}
	defer removeTempFile(msg.path)
	data, _ := os.ReadFile(msg.path)
	if string(data) != "working edit\n" {
		t.Fatalf("rev=\"\" blame must open the on-disk working file, got %q", data)
	}
}

// The Staged panel offers "Open staged version in external editor" — the index
// blob, not the working file. Off on the Files panel, and off for a staged
// deletion (no index blob).
func TestStagedOpenExternalRow(t *testing.T) {
	base := Model{
		width: 100, height: 40, focus: panelStaged, svc: showSvc("index blob\n"),
		sel:       map[panel]int{panelStaged: 0},
		sortModes: map[panel]sortMode{},
		status:    model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "s.go", Staged: 'M', Unstaged: '.'}}},
	}

	row, ok := base.stagedOpenExternalRow()
	if !ok {
		t.Fatal("expected a staged open-external row")
	}
	_, cmd := row.run(base)
	msg, ok := cmd().(editorViewMsg)
	if !ok {
		t.Fatalf("want editorViewMsg, got %T", cmd())
	}
	defer removeTempFile(msg.path)
	if filepath.Base(msg.name) != "s.go" {
		t.Errorf("name = %q, want the staged path", msg.name)
	}
	data, _ := os.ReadFile(msg.path)
	if string(data) != "index blob\n" {
		t.Fatalf("temp content = %q (want the index blob)", data)
	}

	pf := base
	pf.focus = panelFiles
	if _, ok := pf.stagedOpenExternalRow(); ok {
		t.Error("the staged row must not be offered on the Files panel (it keeps live Edit)")
	}

	del := base
	del.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "g.go", Staged: 'D', Unstaged: '.'}}}
	if _, ok := del.stagedOpenExternalRow(); ok {
		t.Error("a staged deletion has no index blob; the row must be skipped")
	}
}
