package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// The TUI's `a` in a commit's files view: every file of the commit's tree
// (TreeFiles), not only the ones it changed. Rows carry no status.
func TestCommitAllFilesListsWholeTree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main", ".")
	gitRun(t, dir, "config", "user.email", "t@example.com")
	gitRun(t, dir, "config", "user.name", "t")
	for _, f := range []string{"a.txt", "src/b.txt", "src/c.txt"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), []byte(f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "src/b.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-am", "touch b")
	sha := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	ts := serve(t, New(domain.Open(dir)))

	type answer struct {
		All     bool   `json:"all"`
		Total   int    `json:"total"`
		Message string `json:"message"`
		Files   []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"files"`
	}
	var plain, all answer
	if code := getJSON(t, ts, "/api/commit/"+sha, &plain); code != http.StatusOK {
		t.Fatalf("plain code = %d", code)
	}
	if len(plain.Files) != 1 || plain.Files[0].Path != "src/b.txt" || plain.All {
		t.Fatalf("plain answer = %+v, want only src/b.txt", plain)
	}
	if code := getJSON(t, ts, "/api/commit/"+sha+"?all=1", &all); code != http.StatusOK {
		t.Fatalf("all code = %d", code)
	}
	if !all.All {
		t.Error("all answer does not say all")
	}
	if all.Total != 3 {
		t.Errorf("all answer total = %d, want 3", all.Total)
	}
	var paths []string
	for _, f := range all.Files {
		paths = append(paths, f.Path)
		if f.Status != "" {
			t.Errorf("%s status = %q, want none (a tree is not a change set)", f.Path, f.Status)
		}
	}
	if strings.Join(paths, ",") != "a.txt,src/b.txt,src/c.txt" {
		t.Errorf("all files = %v", paths)
	}
	if !strings.Contains(all.Message, "touch b") {
		t.Errorf("all answer lost the message: %q", all.Message)
	}
}

// A monorepo tree is hundreds of thousands of paths: the answer stops at a
// cap and says how many there were, so the page never paints them all.
func TestCapTreeFiles(t *testing.T) {
	t.Parallel()
	files := make([]model.CommitFile, 5)
	for i := range files {
		files[i] = model.CommitFile{Path: string(rune('a' + i))}
	}
	got, total := capTreeFiles(files, 3)
	if len(got) != 3 || total != 5 || got[2].Path != "c" {
		t.Errorf("capTreeFiles(5, 3) = %d files, total %d", len(got), total)
	}
	got, total = capTreeFiles(files, 10)
	if len(got) != 5 || total != 5 {
		t.Errorf("capTreeFiles(5, 10) = %d files, total %d", len(got), total)
	}
}
