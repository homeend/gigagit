package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// gitRun runs git in dir with a pinned identity and fails the test on error.
// Returns trimmed stdout+stderr so callers can capture rev-parse output.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepoDir builds a repo with n linear commits c1..cn, each rewriting f.txt
// to "content <i>\n".
func newRepoDir(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	for i := 1; i <= n; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(fmt.Sprintf("content %d\n", i)), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", "-A")
		gitRun(t, dir, "commit", "-m", fmt.Sprintf("c%d", i))
	}
	return dir
}

func serve(t *testing.T, srv *Server) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// getJSON GETs path and decodes the body into out (when out != nil and the
// status is 200). Returns the status code.
func getJSON(t *testing.T, ts *httptest.Server, path string, out any) int {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp.StatusCode
}

func TestRepoEndpoint(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	var got struct {
		Name     string `json:"name"`
		Worktree string `json:"worktree"`
		Branch   string `json:"branch"`
	}
	if code := getJSON(t, ts, "/api/repo", &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if got.Branch != "main" {
		t.Errorf("branch = %q, want main", got.Branch)
	}
	want, _ := filepath.EvalSymlinks(dir)
	gotWT, _ := filepath.EvalSymlinks(got.Worktree)
	if gotWT != want {
		t.Errorf("worktree = %q, want %q", got.Worktree, want)
	}
	if got.Name != filepath.Base(got.Worktree) {
		t.Errorf("name = %q, want base of worktree %q", got.Name, got.Worktree)
	}
}
