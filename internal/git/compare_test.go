package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// setStr returns the set of "<status> <path>" strings for easy comparison.
func setStr(files []model.CommitFile) map[string]bool {
	m := map[string]bool{}
	for _, f := range files {
		m[f.Status+" "+f.Path] = true
	}
	return m
}

func TestDiffTreeFilesAllForwardForms(t *testing.T) {
	dir, runner := newTestRepo(t) // one commit: README.md
	repo := &Repo{Runner: runner}
	ctx := context.Background()
	git := func(args ...string) { gitRun(t, dir, args...) }

	a := revParse(t, dir, "HEAD")

	// second commit B: modify README, add b.txt
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-m", "B")
	b := revParse(t, dir, "HEAD")

	// stage a change to README, leave an unstaged change to b.txt
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "README.md")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("worktree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	commit := func(h string) model.Endpoint { return model.Endpoint{Kind: model.EndpointCommit, Hash: h} }
	index := model.Endpoint{Kind: model.EndpointIndex}
	work := model.Endpoint{Kind: model.EndpointWorkTree}

	// commit A → commit B: README modified, b.txt added
	got, err := repo.DiffTreeFiles(ctx, commit(a), commit(b))
	if err != nil {
		t.Fatal(err)
	}
	s := setStr(got)
	if !s["M README.md"] || !s["A b.txt"] {
		t.Errorf("A→B = %v", s)
	}

	// commit B → index: README staged-modified
	got, err = repo.DiffTreeFiles(ctx, commit(b), index)
	if err != nil {
		t.Fatal(err)
	}
	if !setStr(got)["M README.md"] {
		t.Errorf("B→index = %v", setStr(got))
	}

	// commit B → worktree: README + b.txt both differ from B
	got, err = repo.DiffTreeFiles(ctx, commit(b), work)
	if err != nil {
		t.Fatal(err)
	}
	s = setStr(got)
	if !s["M README.md"] || !s["M b.txt"] {
		t.Errorf("B→worktree = %v", s)
	}

	// index → worktree: only b.txt is unstaged
	got, err = repo.DiffTreeFiles(ctx, index, work)
	if err != nil {
		t.Fatal(err)
	}
	s = setStr(got)
	if !s["M b.txt"] || s["M README.md"] {
		t.Errorf("index→worktree = %v (README should be absent — it's staged)", s)
	}
}

func TestDiffTreeFilesRejectsReversePair(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	_, err := repo.DiffTreeFiles(context.Background(),
		model.Endpoint{Kind: model.EndpointWorkTree},
		model.Endpoint{Kind: model.EndpointCommit, Hash: "HEAD"})
	if err == nil {
		t.Fatal("worktree→commit (reverse) must error")
	}
}
