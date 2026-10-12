package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

const exportReviewDoc = `{"version":1,"summary":"looks fine","overview":"Start at [line two](a.txt:2).",` +
	`"files":[{"path":"a.txt","annotations":[{"newRange":[1,2],"summary":"two lines"}]}]}`

// exportRepo is reviewedRepo with doc as the stored review.
func exportRepo(t *testing.T, doc string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.name", "t")
	gitRun(t, dir, "config", "user.email", "t@t")
	for _, body := range []string{"a\nb\nc\n", "A\nB\nc\n"} {
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", ".")
		gitRun(t, dir, "commit", "-q", "-m", "c")
	}
	head := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	svc := openCLIService(t, dir)
	tg := domain.ReviewTarget{Kind: domain.ReviewRange, Range: head + "^.." + head, Commit: head}
	id, _, err := svc.SaveReview(context.Background(), domain.SaveReview{Target: tg, Agent: "Claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	return dir, id
}

func TestReviewExportToStdoutAndFile(t *testing.T) {
	t.Parallel()
	dir, _ := exportRepo(t, exportReviewDoc)
	code, page, errb := runCLI(t, dir, "review", "export", "--out", "-", "latest")
	if code != 0 || !strings.Contains(page, `id="gg-data"`) || !strings.Contains(page, `"path":"a.txt"`) || strings.Contains(page, "two lines") {
		t.Fatalf("stdout export = %d %q", code, errb)
	}
	out := filepath.Join(t.TempDir(), "x.html")
	if code, _, errb := runCLI(t, dir, "review", "export", "--out", out, "latest"); code != 0 {
		t.Fatalf("file export = %d %q", code, errb)
	}
	if b, err := os.ReadFile(out); err != nil || len(b) == 0 {
		t.Fatalf("file: %v", err)
	}
	if err := os.WriteFile(out, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errb := runCLI(t, dir, "review", "export", "--out", out, "latest"); code != 2 || !strings.Contains(errb, "--force") {
		t.Fatalf("no --force = %d %q", code, errb)
	}
	if code, _, errb := runCLI(t, dir, "review", "export", "--force", "--out", out, "latest"); code != 0 {
		t.Fatalf("--force = %d %q", code, errb)
	}
	if b, _ := os.ReadFile(out); string(b) == "old" {
		t.Fatal("--force did not overwrite")
	}
}

func TestReviewExportWithoutOverview(t *testing.T) {
	t.Parallel()
	dir, id := reviewedRepo(t) // showReviewDoc has no overview
	if code, _, errb := runCLI(t, dir, "review", "export", "--out", "-", id); code != 1 || !strings.Contains(errb, "has no overview") {
		t.Fatalf("= %d %q", code, errb)
	}
	if code, _, errb := runCLI(t, dir, "review", "export"); code != 2 || !strings.Contains(errb, "usage") {
		t.Fatalf("no id = %d %q", code, errb)
	}
}
