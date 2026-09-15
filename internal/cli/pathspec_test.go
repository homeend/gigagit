package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// subdirRepo is newRepoDir plus an untracked src/xxx.txt and a dirty
// root-level file, so a test can tell "staged the subdirectory" apart from
// "staged the whole repo".
func subdirRepo(t *testing.T) (root, sub string) {
	t.Helper()
	root = newRepoDir(t)
	sub = filepath.Join(root, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "xxx.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "root-dirty.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, sub
}

func staged(t *testing.T, root string) []string {
	t.Helper()
	c := exec.Command("git", "diff", "--cached", "--name-only")
	c.Dir = root
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git diff --cached: %v\n%s", err, out)
	}
	var paths []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			paths = append(paths, l)
		}
	}
	return paths
}

// A pathspec the user TYPES is relative to their cwd, exactly as it is for
// git: `gg add .` in src/ stages src/, never the whole repo.
func TestAddDotFromSubdirStagesOnlyThatSubdir(t *testing.T) {
	t.Parallel()
	root, sub := subdirRepo(t)
	if code, _, errb := runCLI(t, sub, "add", "."); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	got := staged(t, root)
	if len(got) != 1 || got[0] != "src/xxx.txt" {
		t.Fatalf("staged %v, want only src/xxx.txt (root-dirty.txt must stay unstaged)", got)
	}
}

func TestAddBareNameFromSubdirStagesThatFile(t *testing.T) {
	t.Parallel()
	root, sub := subdirRepo(t)
	if code, _, errb := runCLI(t, sub, "add", "xxx.txt"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if got := staged(t, root); len(got) != 1 || got[0] != "src/xxx.txt" {
		t.Fatalf("staged %v, want src/xxx.txt", got)
	}
}

// From the root, nothing changes.
func TestAddFromRootIsUnchanged(t *testing.T) {
	t.Parallel()
	root, _ := subdirRepo(t)
	if code, _, errb := runCLI(t, root, "add", "src/xxx.txt"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if got := staged(t, root); len(got) != 1 || got[0] != "src/xxx.txt" {
		t.Fatalf("staged %v, want src/xxx.txt", got)
	}
}

func TestRepoPathspecPassesThroughMagicAndEscapes(t *testing.T) {
	t.Parallel()
	root, sub := subdirRepo(t)
	for _, spec := range []string{":/src/xxx.txt", ":(exclude)x", filepath.Join(root, "..", "outside.txt")} {
		if got := repoPathspec(root, sub, spec); got != spec {
			t.Fatalf("repoPathspec(%q) = %q, want it passed through", spec, got)
		}
	}
}

// A pathspec naming a file that no longer exists (staging a deletion) still
// rebases: EvalSymlinks must not fail the whole translation.
func TestRepoPathspecHandlesMissingFile(t *testing.T) {
	t.Parallel()
	root, sub := subdirRepo(t)
	if got := repoPathspec(root, sub, "deleted.txt"); got != "src/deleted.txt" {
		t.Fatalf("repoPathspec(deleted.txt) = %q, want src/deleted.txt", got)
	}
}
