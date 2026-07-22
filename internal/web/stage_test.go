package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestStageRoundTrip(t *testing.T) {
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	// stage the untracked file
	var st statusResp
	code := postJSON(t, ts, "/api/stage", `{"paths":["new.txt"]}`, "application/json", "", &st)
	if code != http.StatusOK {
		t.Fatalf("stage code = %d", code)
	}
	if st.Counts["staged"] != 1 || st.Counts["untracked"] != 0 {
		t.Errorf("counts after stage = %v", st.Counts)
	}
	if out := gitRun(t, dir, "status", "--porcelain"); !strings.Contains(out, "A  new.txt") {
		t.Errorf("git status after stage:\n%s", out)
	}

	// unstage it back
	code = postJSON(t, ts, "/api/stage", `{"paths":["new.txt"],"unstage":true}`, "application/json", "", &st)
	if code != http.StatusOK {
		t.Fatalf("unstage code = %d", code)
	}
	if st.Counts["staged"] != 0 || st.Counts["untracked"] != 1 {
		t.Errorf("counts after unstage = %v", st.Counts)
	}
	if out := gitRun(t, dir, "status", "--porcelain"); !strings.Contains(out, "?? new.txt") {
		t.Errorf("git status after unstage:\n%s", out)
	}
}

func TestStageAll(t *testing.T) {
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "u.txt"), []byte("u\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	var st statusResp
	if code := postJSON(t, ts, "/api/stage", `{"all":true}`, "application/json", "", &st); code != http.StatusOK {
		t.Fatalf("stage all code = %d", code)
	}
	if st.Counts["staged"] != 2 || st.Counts["unstaged"] != 0 || st.Counts["untracked"] != 0 {
		t.Errorf("counts after stage all = %v", st.Counts)
	}
}

func TestStageValidation(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	cases := []struct {
		name, body string
	}{
		{"empty paths", `{}`},
		{"all plus paths", `{"all":true,"paths":["x"]}`},
		{"all plus unstage", `{"all":true,"unstage":true}`},
		{"argv-unsafe path", `{"paths":["--evil"]}`},
		{"bad json", `{`},
	}
	for _, c := range cases {
		if code := postJSON(t, ts, "/api/stage", c.body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("%s: code = %d, want 400", c.name, code)
		}
	}
}

func TestWriteGuard(t *testing.T) {
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "g.txt"), []byte("g\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	// wrong content type → 415
	if code := postJSON(t, ts, "/api/stage", `{"paths":["g.txt"]}`, "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain code = %d, want 415", code)
	}
	// missing content type → 415
	if code := postJSON(t, ts, "/api/stage", `{"paths":["g.txt"]}`, "", "", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("no content-type code = %d, want 415", code)
	}
	// cross-site origin → 403
	if code := postJSON(t, ts, "/api/stage", `{"paths":["g.txt"]}`, "application/json", "http://evil.example.com", nil); code != http.StatusForbidden {
		t.Errorf("evil origin code = %d, want 403", code)
	}
	// opaque "null" origin (sandboxed iframe) → 403
	if code := postJSON(t, ts, "/api/stage", `{"paths":["g.txt"]}`, "application/json", "null", nil); code != http.StatusForbidden {
		t.Errorf("null origin code = %d, want 403", code)
	}
	// loopback origin passes the guard and the request succeeds
	if code := postJSON(t, ts, "/api/stage", `{"paths":["g.txt"]}`, "application/json", "http://127.0.0.1:9999", nil); code != http.StatusOK {
		t.Errorf("loopback origin code = %d, want 200", code)
	}
}
