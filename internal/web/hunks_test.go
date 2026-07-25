package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func writeRepoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// hunkFixture: h.txt committed with 40 lines, then lines 5 and 35 edited —
// two well-separated change blocks.
func hunkFixture(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t, 1)
	lines := make([]string, 0, 40)
	for i := 1; i <= 40; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	writeRepoFile(t, dir, "h.txt", strings.Join(lines, "\n")+"\n")
	gitRun(t, dir, "add", "h.txt")
	gitRun(t, dir, "commit", "-m", "seed h")
	lines[4] = "line 5 EDITED"
	lines[34] = "line 35 EDITED"
	writeRepoFile(t, dir, "h.txt", strings.Join(lines, "\n")+"\n")
	return dir
}

type hunksResp struct {
	Count  int    `json:"count"`
	Hash   string `json:"hash"`
	Blocks []struct {
		Index int      `json:"index"`
		Del   []string `json:"del"`
		Add   []string `json:"add"`
	} `json:"blocks"`
}

func TestHunksList(t *testing.T) {
	dir := hunkFixture(t)
	ts := serve(t, New(domain.Open(dir)))

	var out hunksResp
	if code := getJSON(t, ts, "/api/hunks?path=h.txt", &out); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if out.Count != 2 || len(out.Blocks) != 2 || out.Hash == "" {
		t.Fatalf("resp = %+v", out)
	}
	if len(out.Blocks[0].Del) != 1 || out.Blocks[0].Del[0] != "line 5" || out.Blocks[0].Add[0] != "line 5 EDITED" {
		t.Errorf("block 0 = %+v", out.Blocks[0])
	}
	if out.Blocks[1].Del[0] != "line 35" || out.Blocks[1].Add[0] != "line 35 EDITED" {
		t.Errorf("block 1 = %+v", out.Blocks[1])
	}
}

func TestStageHunksFirstOnly(t *testing.T) {
	dir := hunkFixture(t)
	ts := serve(t, New(domain.Open(dir)))

	var out hunksResp
	getJSON(t, ts, "/api/hunks?path=h.txt", &out)
	body := fmt.Sprintf(`{"path":"h.txt","picks":[0],"hash":%q}`, out.Hash)
	if code := postJSON(t, ts, "/api/stage-hunks", body, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("stage code = %d", code)
	}
	cached := gitRun(t, dir, "diff", "--cached", "--", "h.txt")
	if !strings.Contains(cached, "line 5 EDITED") || strings.Contains(cached, "line 35 EDITED") {
		t.Fatalf("staged diff wrong:\n%s", cached)
	}
	work := gitRun(t, dir, "diff", "--", "h.txt")
	if !strings.Contains(work, "line 35 EDITED") {
		t.Fatalf("worktree-vs-index lost the unpicked hunk:\n%s", work)
	}
	// second round: the remaining block is now index 0 with a fresh hash
	var out2 hunksResp
	getJSON(t, ts, "/api/hunks?path=h.txt", &out2)
	if out2.Count != 1 {
		t.Fatalf("remaining count = %d", out2.Count)
	}
	body2 := fmt.Sprintf(`{"path":"h.txt","picks":[0],"hash":%q}`, out2.Hash)
	if code := postJSON(t, ts, "/api/stage-hunks", body2, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("second stage code = %d", code)
	}
	if rest := gitRun(t, dir, "diff", "--", "h.txt"); strings.TrimSpace(rest) != "" {
		t.Fatalf("worktree still differs from index:\n%s", rest)
	}
}

func TestStageHunksStaleHash(t *testing.T) {
	dir := hunkFixture(t)
	ts := serve(t, New(domain.Open(dir)))

	var out hunksResp
	getJSON(t, ts, "/api/hunks?path=h.txt", &out)
	writeRepoFile(t, dir, "h.txt", "totally different\n") // drift after the GET
	body := fmt.Sprintf(`{"path":"h.txt","picks":[0],"hash":%q}`, out.Hash)
	if code := postJSON(t, ts, "/api/stage-hunks", body, "application/json", "", nil); code != http.StatusConflict {
		t.Fatalf("stale hash code = %d, want 409", code)
	}
	if cached := gitRun(t, dir, "diff", "--cached", "--", "h.txt"); strings.TrimSpace(cached) != "" {
		t.Fatalf("index changed despite 409:\n%s", cached)
	}
}

func TestHunksGuards(t *testing.T) {
	dir := hunkFixture(t)
	ts := serve(t, New(domain.Open(dir)))

	// untracked: no index blob
	writeRepoFile(t, dir, "new.txt", "x\n")
	if code := getJSON(t, ts, "/api/hunks?path=new.txt", &struct{}{}); code != http.StatusUnprocessableEntity {
		t.Errorf("untracked = %d, want 422", code)
	}
	// binary
	writeRepoFile(t, dir, "bin.dat", "a\x00b")
	gitRun(t, dir, "add", "bin.dat")
	gitRun(t, dir, "commit", "-m", "bin")
	writeRepoFile(t, dir, "bin.dat", "c\x00d")
	if code := getJSON(t, ts, "/api/hunks?path=bin.dat", &struct{}{}); code != http.StatusUnprocessableEntity {
		t.Errorf("binary = %d, want 422", code)
	}
	// CRLF
	writeRepoFile(t, dir, "cr.txt", "a\r\nb\r\n")
	gitRun(t, dir, "add", "cr.txt")
	gitRun(t, dir, "commit", "-m", "cr")
	writeRepoFile(t, dir, "cr.txt", "a\r\nB\r\n")
	if code := getJSON(t, ts, "/api/hunks?path=cr.txt", &struct{}{}); code != http.StatusUnprocessableEntity {
		t.Errorf("crlf = %d, want 422", code)
	}
	// missing file
	if code := getJSON(t, ts, "/api/hunks?path=nope.txt", &struct{}{}); code != http.StatusNotFound {
		t.Errorf("missing = %d, want 404", code)
	}
	// bad picks
	var out hunksResp
	getJSON(t, ts, "/api/hunks?path=h.txt", &out)
	for body, want := range map[string]int{
		fmt.Sprintf(`{"path":"h.txt","picks":[9],"hash":%q}`, out.Hash): http.StatusBadRequest,
		fmt.Sprintf(`{"path":"h.txt","picks":[],"hash":%q}`, out.Hash):  http.StatusBadRequest,
	} {
		if code := postJSON(t, ts, "/api/stage-hunks", body, "application/json", "", nil); code != want {
			t.Errorf("body %s = %d, want %d", body, code, want)
		}
	}
}

func TestStageHunksWriteGuard(t *testing.T) {
	dir := hunkFixture(t)
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/stage-hunks", `{"path":"h.txt","picks":[0],"hash":"x"}`, "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("non-JSON = %d, want 415", code)
	}
	if code := postJSON(t, ts, "/api/stage-hunks", `{"path":"h.txt","picks":[0],"hash":"x"}`, "application/json", "http://evil.example", nil); code != http.StatusForbidden {
		t.Errorf("cross-origin = %d, want 403", code)
	}
}
