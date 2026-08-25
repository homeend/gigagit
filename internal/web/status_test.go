package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// statusResp mirrors the /api/status JSON contract (spec §B).
type statusResp struct {
	Files []struct {
		Path     string `json:"path"`
		OrigPath string `json:"orig_path"`
		Staged   string `json:"staged"`
		Unstaged string `json:"unstaged"`
		Kind     string `json:"kind"`
	} `json:"files"`
	Counts map[string]int `json:"counts"`
}

// gitTry runs git in dir ignoring a non-zero exit (e.g. a merge that
// deliberately conflicts — gitRun would t.Fatal on it).
func gitTry(dir string, args ...string) {
	cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	_ = cmd.Run()
}

// postJSON POSTs body to path with the given content type and optional
// Origin header, decoding a 200 response into out. Returns the status code.
func postJSON(t *testing.T, ts *httptest.Server, path, body, contentType, origin string, out any) int {
	t.Helper()
	req, err := http.NewRequest("POST", ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp.StatusCode
}

func findFile(t *testing.T, st statusResp, path string) (int, bool) {
	t.Helper()
	for i, f := range st.Files {
		if f.Path == path {
			return i, true
		}
	}
	return 0, false
}

func TestStatusClassification(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1) // f.txt committed as "content 1\n"
	// staged: new file added to the index
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "staged.txt")
	// unstaged: modify the tracked file
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// untracked
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("u\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ts := serve(t, New(domain.Open(dir)))
	var st statusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status code = %d", code)
	}
	if len(st.Files) != 3 {
		t.Fatalf("files = %d, want 3 (%+v)", len(st.Files), st.Files)
	}
	if i, ok := findFile(t, st, "staged.txt"); !ok || st.Files[i].Kind != "tracked" || st.Files[i].Staged != "A" {
		t.Errorf("staged.txt: %+v", st.Files)
	}
	if i, ok := findFile(t, st, "f.txt"); !ok || st.Files[i].Kind != "tracked" || st.Files[i].Unstaged != "M" {
		t.Errorf("f.txt: %+v", st.Files)
	}
	if i, ok := findFile(t, st, "untracked.txt"); !ok || st.Files[i].Kind != "untracked" {
		t.Errorf("untracked.txt: %+v", st.Files)
	}
	want := map[string]int{"staged": 1, "unstaged": 1, "untracked": 1, "conflicted": 0}
	for k, v := range want {
		if st.Counts[k] != v {
			t.Errorf("counts[%s] = %d, want %d", k, st.Counts[k], v)
		}
	}
}

func TestStatusConflicted(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "side")
	gitRun(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "main2")
	gitTry(dir, "merge", "side") // conflicts on f.txt by construction

	ts := serve(t, New(domain.Open(dir)))
	var st statusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status code = %d", code)
	}
	if i, ok := findFile(t, st, "f.txt"); !ok || st.Files[i].Kind != "conflicted" {
		t.Fatalf("f.txt not conflicted: %+v", st.Files)
	}
	if st.Counts["conflicted"] != 1 {
		t.Errorf("counts[conflicted] = %d, want 1", st.Counts["conflicted"])
	}
}

func TestStatusCleanRepo(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	var st statusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status code = %d", code)
	}
	if len(st.Files) != 0 {
		t.Errorf("files = %+v, want empty", st.Files)
	}
	for k, v := range st.Counts {
		if v != 0 {
			t.Errorf("counts[%s] = %d, want 0", k, v)
		}
	}
}
