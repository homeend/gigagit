package domain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/repogate"
)

// countedOvRepo is permOvRepo with a countingRunner under the Service.
func countedOvRepo(t *testing.T) (string, *Service, *countingRunner) {
	t.Helper()
	dir, _ := permOvRepo(t)
	cr := newCountingRunner(gitexec.NewExecRunner("git", dir, observ.NewRing(50)))
	svc := New(&git.Repo{Runner: cr})
	svc.UseNotesDir(t.TempDir())
	return dir, svc, cr
}

func TestSaveOverviewCountsItsGitCalls(t *testing.T) {
	t.Parallel()
	_, svc, cr := countedOvRepo(t)
	tg, err := svc.HeadOverviewTarget(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cr.reset()
	doc := ovDoc("T", "[1](a.go:1) [2](a.go:2-3) [3](a.go:4) [y](b.go:2)",
		`[{"path":"a.go","annotations":[{"newRange":[5,5],"summary":"r"}]}]`)
	if _, _, err := svc.SaveOverview(context.Background(), SaveOverview{Target: tg, Text: doc, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if n := cr.count("git ls-tree (tree paths)"); n != 1 {
		t.Errorf("ls-tree calls = %d, want 1", n)
	}
	if n := cr.count("git show"); n != 2 {
		t.Errorf("file reads = %d, want 2 (one per file)", n)
	}
}

func TestSaveOverviewBatchesItsPaths(t *testing.T) {
	t.Parallel()
	_, svc, cr := countedOvRepo(t)
	tg, _ := svc.HeadOverviewTarget(context.Background())
	var files []string
	for i := range pathProbeBatch + 1 {
		files = append(files, fmt.Sprintf(`{"path":"gone%d.go","annotations":[{"newRange":[1,1],"summary":"r"}]}`, i))
	}
	cr.reset()
	_, _, err := svc.SaveOverview(context.Background(), SaveOverview{Target: tg,
		Text: ovDoc("T", "[a](a.go:1)", "["+strings.Join(files, ",")+"]"), DryRun: true})
	if got := problems(t, err); len(got) != pathProbeBatch+1 {
		t.Fatalf("problems = %d, want %d", len(got), pathProbeBatch+1)
	}
	if n := cr.count("git ls-tree (tree paths)"); n != 2 {
		t.Errorf("ls-tree calls = %d, want 2 batches", n)
	}
}

// The check's git reads take the per-repo reservation: none runs while a
// writer holds the repository.
func TestSaveOverviewCheckWaitsForTheGate(t *testing.T) {
	t.Parallel()
	_, svc, cr := countedOvRepo(t)
	ctx := context.Background()
	tg, _ := svc.HeadOverviewTarget(ctx)
	res, err := svc.gateFor(ctx).Acquire(ctx, repogate.TreeWrite, "test holds the repo")
	if err != nil {
		t.Fatal(err)
	}
	cr.reset()
	short, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	_, _, _ = svc.SaveOverview(short, SaveOverview{Target: tg, Text: ovDoc("T", "[file](a.go)", ""), DryRun: true})
	res.Release()
	for _, name := range []string{"git ls-tree (tree paths)", "git status (paths)", "git diff --name-only (paths)"} {
		if n := cr.count(name); n != 0 {
			t.Errorf("%s ran %d times while a writer held the repo", name, n)
		}
	}
}

func TestSaveOverviewNamesADirectory(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	commitFile(t, dir, "pkg/x.go", "1\n2\n", "pkg")
	_, err := saveOv(t, svc, ovDoc("T", "[d](pkg) [l](pkg:1)", ""))
	if got := problems(t, err); !slices.Equal(got, []string{"pkg: a directory — link a file"}) {
		t.Fatalf("problems = %v", got)
	}
}

func TestSaveOverviewNamesALargeTextFile(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	big := strings.Repeat("a line of plain text\n", MaxDiffBytes/20+10)
	commitFile(t, dir, "big.txt", big, "big")
	_, err := saveOv(t, svc, ovDoc("T", "[whole](big.txt) [l](big.txt:2)", ""))
	if got := problems(t, err); !slices.Equal(got, []string{"big.txt: too large to show its lines — link the file"}) {
		t.Fatalf("problems = %v", got)
	}
}

func TestSaveOverviewReportsAProblemOnce(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	sha7 := revParse(t, dir, "HEAD")[:7]
	_, err := saveOv(t, svc, ovDoc("T", "[a](a.go:9) [b](a.go:9)",
		`[{"path":"a.go","annotations":[{"newRange":[9,9],"summary":"r"}]}]`))
	if got := problems(t, err); !slices.Equal(got, []string{"a.go:9: past the end (5 lines at " + sha7 + ")"}) {
		t.Fatalf("problems = %v", got)
	}
}

// A NUL in a remark path is refused on its own; the other paths still check.
func TestSaveOverviewNULPathKeepsTheCheck(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	sha7 := revParse(t, dir, "HEAD")[:7]
	_, err := saveOv(t, svc, ovDoc("T", "[a](a.go:9) [b](b.go:1)",
		`[{"path":"x\u0000y.go","annotations":[{"newRange":[1,1],"summary":"r"}]}]`))
	want := []string{"a.go:9: past the end (5 lines at " + sha7 + ")", `"x\x00y.go": not in ` + sha7}
	if got := problems(t, err); !slices.Equal(got, want) {
		t.Fatalf("problems = %q, want %q", got, want)
	}
}

func TestNoteLinkTextOfAnOverviewIsItsOverviewLink(t *testing.T) {
	t.Parallel()
	_, svc := permOvRepo(t)
	got, err := saveOv(t, svc, ovDoc("T", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	text, err := svc.NoteLinkText(context.Background(), got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(text, "?overview="+got.ID) || strings.Contains(text, "?note=") {
		t.Fatalf("link = %q, want the overview's link", text)
	}
}

func TestCleanRepoPathKeepsLegalNames(t *testing.T) {
	t.Parallel()
	for p, want := range map[string]bool{"a\x00b.go": false, "dir/a\tb.go": true, "a\nb.go": true, "./a.go": false, "a/../b": false} {
		if got := cleanRepoPath(p); got != want {
			t.Errorf("cleanRepoPath(%q) = %v, want %v", p, got, want)
		}
	}
}
