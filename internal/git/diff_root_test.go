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
