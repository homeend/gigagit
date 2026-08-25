package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/model"
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
	t.Parallel()
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

// TestDiffTreeFilesNonASCIIPath is the compare-view twin of
// TestCommitFilesNonASCIIPathRoundTrip: DiffTreeFiles shares ParseNameStatus,
// so a non-ASCII added file must surface as a raw UTF-8 path (not git's quoted
// "timing \342\200\224 …" form), else the compare diff's ShowFile fails.
func TestDiffTreeFilesNonASCIIPath(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t) // one commit: README.md
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	a := revParse(t, dir, "HEAD")
	const name = "timing — kopia.log" // em-dash U+2014
	if err := os.WriteFile(filepath.Join(dir, name), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "B")
	b := revParse(t, dir, "HEAD")

	commit := func(h string) model.Endpoint { return model.Endpoint{Kind: model.EndpointCommit, Hash: h} }
	got, err := repo.DiffTreeFiles(ctx, commit(a), commit(b))
	if err != nil {
		t.Fatal(err)
	}
	if !setStr(got)["A "+name] {
		t.Fatalf("A→B = %v, want raw %q added", setStr(got), name)
	}
}

func TestUntrackedFiles(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	// A committed .gitignore so an ignored file is excluded (and .gitignore itself
	// is tracked, not reported).
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".gitignore")
	gitRun(t, dir, "commit", "-m", "ignore")

	// An untracked file with a space + em-dash (like the user's "timing — kopia.log").
	special := "timing — kopia.log"
	if err := os.WriteFile(filepath.Join(dir, special), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.log"), []byte("nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := repo.UntrackedFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != special {
		t.Fatalf("UntrackedFiles = %#v, want exactly [%q] (no ignored.log, exact path)", got, special)
	}
}

func TestDiffTreeFilesRejectsReversePair(t *testing.T) {
	t.Parallel()
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	_, err := repo.DiffTreeFiles(context.Background(),
		model.Endpoint{Kind: model.EndpointWorkTree},
		model.Endpoint{Kind: model.EndpointCommit, Hash: "HEAD"})
	if err == nil {
		t.Fatal("worktree→commit (reverse) must error")
	}
}
