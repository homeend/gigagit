package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
)

// A root commit's own change is everything it adds: DiffSpec{Root} diffs it
// against the empty tree, in either object format. A bare `git diff <root>`
// would compare the WORKING TREE with it instead (here: b.txt, not a.txt).
func TestDiffOfARootCommitIsItsOwnChange(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"sha1", "sha256"} {
		dir := initRepo(t, format)
		git := func(args ...string) string {
			cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
			return strings.TrimSpace(string(out))
		}
		for _, f := range []string{"a.txt", "b.txt"} {
			if err := os.WriteFile(filepath.Join(dir, f), []byte(f+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			git("add", f)
			git("commit", "-qm", f)
		}
		root := git("rev-list", "--max-parents=0", "HEAD")
		r := &Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(10))}
		spec := model.DiffSpec{Rev: root, Root: true}
		patch, err := r.DiffPatch(context.Background(), spec)
		if err != nil || !strings.Contains(patch, "+++ b/a.txt") || strings.Contains(patch, "b.txt") {
			t.Fatalf("%s patch: %v\n%s", format, err, patch)
		}
		stat, err := r.DiffNumstat(context.Background(), spec)
		if err != nil || ParseNumstat(stat)[0].Path != "a.txt" || len(ParseNumstat(stat)) != 1 {
			t.Fatalf("%s numstat: %v %q", format, err, stat)
		}
	}
}

// The oldest commit of a SHALLOW clone shows no parent, but it has one —
// git just did not fetch it. Diffing it as a root would hand over the whole
// tree (gigabytes in a monorepo); it is refused, saying how to get history.
func TestDiffOfAShallowBoundaryIsRefused(t *testing.T) {
	t.Parallel()
	src := initRepo(t, "sha1")
	git := func(dir string, args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	for _, f := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(src, f), []byte(f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(src, "add", f)
		git(src, "commit", "-qm", f)
	}
	dst := filepath.Join(t.TempDir(), "shallow")
	git(src, "clone", "-q", "--depth", "1", "file://"+src, dst)
	r := &Repo{Runner: gitexec.NewExecRunner("git", dst, observ.NewRing(10))}
	_, err := r.DiffPatch(context.Background(), model.DiffSpec{Rev: "HEAD", Root: true})
	if err == nil || !strings.Contains(err.Error(), "shallow") || !strings.Contains(err.Error(), "git fetch --deepen") {
		t.Fatalf("a shallow boundary diffed as a root: %v", err)
	}
	if _, err := r.DiffNumstat(context.Background(), model.DiffSpec{Rev: "HEAD", Root: true}); err == nil {
		t.Fatal("numstat of a shallow boundary as a root was not refused")
	}
}
