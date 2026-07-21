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

func TestCommitFilesEndpoint(t *testing.T) {
	dir := newRepoDir(t, 2)
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	var got struct {
		Sha   string `json:"sha"`
		Files []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"files"`
	}
	if code := getJSON(t, ts, "/api/commit/"+sha, &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if got.Sha != sha {
		t.Errorf("sha = %q, want %q", got.Sha, sha)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "f.txt" || got.Files[0].Status != "M" {
		t.Errorf("files = %+v, want [{f.txt M}]", got.Files)
	}
}

func TestCommitFilesBadSha(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	if code := getJSON(t, ts, "/api/commit/zzzz", nil); code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", code)
	}
}

func TestCommitFilesRejectsFlag(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	// A flag-shaped sha must not reach the git argv (argument injection).
	if code := getJSON(t, ts, "/api/commit/--output=x", nil); code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a flag-shaped sha", code)
	}
}

func TestDiffEndpoint(t *testing.T) {
	dir := newRepoDir(t, 2) // c2 rewrote f.txt: "content 1" -> "content 2"
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	var got struct {
		Rows []struct {
			Kind       string   `json:"kind"`
			Left       string   `json:"left"`
			Right      string   `json:"right"`
			LeftNo     int      `json:"left_no"`
			RightNo    int      `json:"right_no"`
			LeftSpans  [][2]int `json:"left_spans"`
			RightSpans [][2]int `json:"right_spans"`
		} `json:"rows"`
		Binary   bool `json:"binary"`
		TooLarge bool `json:"too_large"`
	}
	code := getJSON(t, ts, "/api/diff?sha="+sha+"&path=f.txt&status=M", &got)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if got.Binary || got.TooLarge {
		t.Fatalf("binary/too_large set on a text diff: %+v", got)
	}
	var change bool
	for _, r := range got.Rows {
		if r.Kind == "change" && strings.Contains(r.Left, "content 1") && strings.Contains(r.Right, "content 2") {
			change = true
			if len(r.LeftSpans) == 0 || len(r.RightSpans) == 0 {
				t.Errorf("change row missing intraline spans: %+v", r)
			}
		}
	}
	if !change {
		t.Fatalf("no change row content 1 -> content 2 in %+v", got.Rows)
	}
}

func TestDiffMissingParams(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	if code := getJSON(t, ts, "/api/diff?path=f.txt", nil); code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 without sha", code)
	}
}

func TestDiffRejectsFlag(t *testing.T) {
	dir := newRepoDir(t, 1)
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	// A flag-shaped path must not reach the git argv (argument injection).
	if code := getJSON(t, ts, "/api/diff?sha="+sha+"&path=--output=x&status=M", nil); code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a flag-shaped path", code)
	}
}

func TestDiffAddedFile(t *testing.T) {
	dir := newRepoDir(t, 1) // root commit: f.txt is status A, sha^ does not exist
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	var got struct {
		Rows []struct {
			Kind string `json:"kind"`
		} `json:"rows"`
	}
	code := getJSON(t, ts, "/api/diff?sha="+sha+"&path=f.txt&status=A", &got)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(got.Rows) == 0 || got.Rows[0].Kind != "add" {
		t.Fatalf("rows = %+v, want add rows for a new file", got.Rows)
	}
}
