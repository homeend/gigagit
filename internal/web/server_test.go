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

func TestCommitsEndpoint(t *testing.T) {
	dir := newRepoDir(t, 3)
	ts := serve(t, New(domain.Open(dir)))
	var got struct {
		Rows []struct {
			Hash    string `json:"hash"`
			Short   string `json:"short"`
			Subject string `json:"subject"`
			Author  string `json:"author"`
			Time    int64  `json:"time"`
			Cells   string `json:"cells"`
			Refs    []struct {
				Name string `json:"name"`
				Kind string `json:"kind"`
			} `json:"refs"`
		} `json:"rows"`
		CanLoadMore bool `json:"can_load_more"`
	}
	if code := getJSON(t, ts, "/api/commits", &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(got.Rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(got.Rows))
	}
	tip := got.Rows[0]
	if tip.Subject != "c3" {
		t.Errorf("first subject = %q, want c3 (newest first)", tip.Subject)
	}
	if len(tip.Short) != 8 || !strings.HasPrefix(tip.Hash, tip.Short) {
		t.Errorf("short = %q not an 8-char prefix of %q", tip.Short, tip.Hash)
	}
	if tip.Time == 0 || tip.Author == "" {
		t.Errorf("missing time/author: %+v", tip)
	}
	if !strings.Contains(tip.Cells, "●") {
		t.Errorf("cells = %q, want node glyph ●", tip.Cells)
	}
	var hasMain bool
	for _, ref := range tip.Refs {
		if ref.Name == "main" && ref.Kind == "local" {
			hasMain = true
		}
	}
	if !hasMain {
		t.Errorf("tip refs = %+v, want local main", tip.Refs)
	}
}

func TestCommitsPaging(t *testing.T) {
	dir := newRepoDir(t, 30)
	srv := New(domain.Open(dir))
	srv.pageInitial, srv.pageBatch = 10, 10
	ts := serve(t, srv)
	var first struct {
		Rows        []json.RawMessage `json:"rows"`
		CanLoadMore bool              `json:"can_load_more"`
	}
	getJSON(t, ts, "/api/commits", &first)
	if len(first.Rows) < 10 || len(first.Rows) >= 30 {
		t.Fatalf("initial rows = %d, want a partial page (>=10, <30)", len(first.Rows))
	}
	if !first.CanLoadMore {
		t.Fatal("can_load_more = false after a partial first page")
	}
	var second struct {
		Rows []json.RawMessage `json:"rows"`
	}
	getJSON(t, ts, "/api/commits?more=1", &second)
	if len(second.Rows) <= len(first.Rows) {
		t.Fatalf("more=1 did not grow the feed: %d -> %d", len(first.Rows), len(second.Rows))
	}
}
