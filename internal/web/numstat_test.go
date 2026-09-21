package web

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type numstatWire struct {
	Files []struct {
		Path    string `json:"path"`
		OldPath string `json:"old_path"`
		Add     int    `json:"add"`
		Del     int    `json:"del"`
		Binary  bool   `json:"binary"`
	} `json:"files"`
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

// The stacked diff asks for a change set's per-file counts in ONE request;
// each of the three sources the web can name by hash answers here.
func TestNumstatSources(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3) // f.txt rewritten by each of 3 commits
	ts := serve(t, New(domain.Open(dir)))
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	first := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD~2"))

	var got numstatWire
	if code := getJSON(t, ts, "/api/numstat?sha="+head, &got); code != http.StatusOK {
		t.Fatalf("sha form: code %d", code)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "f.txt" || got.Files[0].Add != 1 || got.Files[0].Del != 1 {
		t.Fatalf("sha form = %+v, want f.txt +1 -1", got.Files)
	}

	got = numstatWire{}
	if code := getJSON(t, ts, "/api/numstat?left="+first+"&right="+head, &got); code != http.StatusOK {
		t.Fatalf("left/right form: code %d", code)
	}
	if len(got.Files) != 1 || got.Files[0].Add != 1 || got.Files[0].Del != 1 {
		t.Fatalf("left/right form = %+v", got.Files)
	}

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = numstatWire{}
	if code := getJSON(t, ts, "/api/numstat?wt=unstaged", &got); code != http.StatusOK {
		t.Fatalf("wt=unstaged: code %d", code)
	}
	if len(got.Files) != 1 || got.Files[0].Add != 2 || got.Files[0].Del != 1 {
		t.Fatalf("wt=unstaged = %+v, want f.txt +2 -1", got.Files)
	}
	got = numstatWire{}
	if code := getJSON(t, ts, "/api/numstat?wt=staged", &got); code != http.StatusOK || len(got.Files) != 0 {
		t.Fatalf("wt=staged: code %d files %+v, want 200 and none", code, got.Files)
	}
}

func TestNumstatRejectsBadInput(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	for _, q := range []string{"", "sha=-x", "sha=main", "left=abc1234", "left=abc1234&right=--output", "wt=both"} {
		if code := getJSON(t, ts, "/api/numstat?"+q, nil); code != http.StatusBadRequest {
			t.Errorf("?%s: code %d, want 400", q, code)
		}
	}
}
