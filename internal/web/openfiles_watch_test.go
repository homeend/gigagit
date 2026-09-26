package web

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestPollOpenFilesReportsEditsAndDeletes(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	wt := srv.service().Root()
	f, _ := srv.ofs.open(wt, ofKey{Src: "worktree", Path: "f.txt"}, "t1", 0)
	srv.baseline(wt, f.ID, ofKey{Src: "worktree", Path: "f.txt"})
	now := time.Unix(5000, 0)
	if got := srv.pollOpenFiles(now, false, 5*time.Second); len(got) != 0 {
		t.Fatalf("unchanged: %v", got)
	}
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("edited, longer\n"), 0o644)
	if got := srv.pollOpenFiles(now.Add(time.Second), false, 5*time.Second); len(got) != 1 || got[0] != f.ID {
		t.Fatalf("edit: %v", got)
	}
	os.Remove(filepath.Join(dir, "f.txt"))
	if got := srv.pollOpenFiles(now.Add(2*time.Second), false, 5*time.Second); len(got) != 1 {
		t.Fatalf("delete: %v", got)
	}
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("back\n"), 0o644)
	if got := srv.pollOpenFiles(now.Add(3*time.Second), false, 5*time.Second); len(got) != 1 {
		t.Fatalf("back: %v", got)
	}
}

func TestPollOpenFilesSkipsWhileAnOpRuns(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	wt := srv.service().Root()
	f, _ := srv.ofs.open(wt, ofKey{Src: "worktree", Path: "f.txt"}, "t1", 0)
	srv.baseline(wt, f.ID, ofKey{Src: "worktree", Path: "f.txt"})
	srv.opMu.Lock()
	srv.cur = &opRun{} // a live op (not done)
	srv.opMu.Unlock()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("the op's edit\n"), 0o644)
	if got := srv.pollOpenFiles(time.Unix(1, 0), true, 5*time.Second); got != nil {
		t.Fatalf("mid-op round must be skipped: %v", got)
	}
	srv.opMu.Lock()
	srv.cur.done = true
	srv.opMu.Unlock()
	if got := srv.pollOpenFiles(time.Unix(2, 0), true, 5*time.Second); len(got) != 1 {
		t.Fatalf("the first round after the op sees the edit (baseline not advanced): %v", got)
	}
}

func TestPollOpenFilesBackgroundCadence(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	wt := srv.service().Root()
	f, _ := srv.ofs.open(wt, ofKey{Src: "worktree", Path: "f.txt"}, "", 0) // background
	srv.baseline(wt, f.ID, ofKey{Src: "worktree", Path: "f.txt"})
	now := time.Unix(9000, 0)
	srv.pollOpenFiles(now, false, 5*time.Second) // first check
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("edited, longer\n"), 0o644)
	if got := srv.pollOpenFiles(now.Add(time.Second), false, 5*time.Second); len(got) != 0 {
		t.Fatalf("a background file waits 5 s: %v", got)
	}
	if got := srv.pollOpenFiles(now.Add(6*time.Second), false, 5*time.Second); len(got) != 1 {
		t.Fatalf("then it is looked at: %v", got)
	}
}
