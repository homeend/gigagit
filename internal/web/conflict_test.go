package web

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// conflictStatusResp decodes the parts of /api/status these tests assert on.
type conflictStatusResp struct {
	Counts   map[string]int `json:"counts"`
	Conflict *struct {
		Op         string `json:"op"`
		Source     string `json:"source"`
		Target     string `json:"target"`
		Desc       string `json:"desc"`
		Conflicted int    `json:"conflicted"`
	} `json:"conflict"`
}

// conflictedMergeState runs `git merge feature`, expecting the conflict to
// leave a paused merge in the tree. gitRun can't be used — it fails the test
// on any non-zero exit, and a conflicted merge exits 1 by design.
func conflictedMergeState(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "-c", "commit.gpgsign=false", "merge", "feature")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("merge feature unexpectedly succeeded:\n%s", out)
	}
	if gitRun(t, dir, "ls-files", "-u") == "" {
		t.Fatal("no unmerged entries after conflicted merge")
	}
}

func TestStatusConflictObject(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	var st conflictStatusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	c := st.Conflict
	if c == nil {
		t.Fatal("no conflict object on a paused merge")
	}
	if c.Op != "merge" || c.Source != "feature" || c.Target != "main" {
		t.Errorf("conflict = %+v, want merge feature→main", c)
	}
	if c.Desc != "merging feature into main" {
		t.Errorf("desc = %q", c.Desc)
	}
	if c.Conflicted != 1 {
		t.Errorf("conflicted = %d, want 1", c.Conflicted)
	}
}

func TestStatusConflictAbsentWhenClean(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	var st conflictStatusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if st.Conflict != nil {
		t.Errorf("conflict = %+v, want absent", st.Conflict)
	}
}

// A paused op whose conflicts were all resolved by hand (file written + staged
// outside gg, merge never continued) must STILL report — that is the
// resume-paused-op parity the banner's Continue depends on.
func TestStatusConflictPausedZeroConflicts(t *testing.T) {
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("resolved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "f.txt")
	ts := serve(t, New(domain.Open(dir)))

	var st conflictStatusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if st.Conflict == nil || st.Conflict.Op != "merge" {
		t.Fatalf("conflict = %+v, want paused merge reported", st.Conflict)
	}
	if st.Conflict.Conflicted != 0 {
		t.Errorf("conflicted = %d, want 0", st.Conflict.Conflicted)
	}
}
