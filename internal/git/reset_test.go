package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestResetArgv(t *testing.T) {
	for _, mode := range []string{"soft", "mixed", "hard"} {
		f := gitexec.NewFakeRunner()
		f.SetResponse("git reset --"+mode, gitexec.Result{})
		r := &Repo{Runner: f}
		if err := r.Reset(context.Background(), mode, "abc123"); err != nil {
			t.Fatalf("reset --%s: %v", mode, err)
		}
		want := []string{"reset", "--" + mode, "abc123"}
		if !reflect.DeepEqual(f.Calls[0].Argv, want) {
			t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
		}
	}
}

// resetRepo builds main with two commits (base, then add b.txt) and HEAD on the
// second; returns dir, repo, and the base SHA.
func resetRepo(t *testing.T) (string, *Repo, string) {
	t.Helper()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "base")
	base := gitOutIn(t, dir, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "add b.txt")
	return dir, repo, base
}

func TestResetSoftStagesDiff(t *testing.T) {
	dir, repo, base := resetRepo(t)
	if err := repo.Reset(context.Background(), "soft", base); err != nil {
		t.Fatalf("reset soft: %v", err)
	}
	if revParse(t, dir, "HEAD") != base {
		t.Fatal("HEAD should be at base after soft reset")
	}
	if st, _ := repo.Status(context.Background()); st.Counts().Staged == 0 {
		t.Fatalf("soft reset should leave the diff staged, got %+v", st.Counts())
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal("b.txt should survive a soft reset")
	}
}

func TestResetMixedUnstagesDiff(t *testing.T) {
	dir, repo, base := resetRepo(t)
	if err := repo.Reset(context.Background(), "mixed", base); err != nil {
		t.Fatalf("reset mixed: %v", err)
	}
	if revParse(t, dir, "HEAD") != base {
		t.Fatal("HEAD should be at base after mixed reset")
	}
	st, _ := repo.Status(context.Background())
	if st.Counts().Staged != 0 {
		t.Fatalf("mixed reset should leave nothing staged, got %+v", st.Counts())
	}
	// b.txt is now an untracked file (the add-commit was undone, tree kept).
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal("b.txt should survive a mixed reset")
	}
}

func TestResetHardDiscardsTrackedChanges(t *testing.T) {
	dir, repo, base := resetRepo(t)
	// dirty a tracked file too
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("dirty\n"), 0o644)

	if err := repo.Reset(context.Background(), "hard", base); err != nil {
		t.Fatalf("reset hard: %v", err)
	}
	if revParse(t, dir, "HEAD") != base {
		t.Fatal("HEAD should be at base after hard reset")
	}
	// the committed b.txt is gone (it was added after base, tree reset)
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("hard reset should remove b.txt, stat err = %v", err)
	}
	// the dirty tracked edit is discarded
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "base\n" {
		t.Fatalf("a.txt = %q after hard reset, want base", got)
	}
	if st, _ := repo.Status(context.Background()); st.Counts().Staged+st.Counts().Unstaged != 0 {
		t.Fatalf("hard reset should leave a clean tree, got %+v", st.Counts())
	}
}
