package cli

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// The three-dot form needs no saved record, and the resolved DiffSpec is the
// PREVIEW's patch, built by the one constructor (set.DiffSpec()) — so hunk
// numbers under --preview match `gg diff --preview --hunks` and are never the
// tip commit's own parent→tip numbering.
func TestResolvePreviewTargetThreeDotForm(t *testing.T) {
	svc, dir := newCLIPreviewRepo(t) // helper below
	tgt, err := resolvePreviewTarget(context.Background(), svc, "main...feat")
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Source != "feat" || tgt.Target != "main" {
		t.Fatalf("got %s/%s", tgt.Source, tgt.Target)
	}
	if !tgt.Set.OK() || tgt.Set.Tip != revParseCLI(t, dir, "feat") {
		t.Fatalf("the set's tip must be feat's tip, got %+v", tgt.Set)
	}
	if tgt.Spec.Rev != tgt.Set.DiffSpec().Rev {
		t.Fatalf("the CLI must not build its own range: %q vs %q", tgt.Spec.Rev, tgt.Set.DiffSpec().Rev)
	}
	// The preview's patch runs merge-base → tip, NOT parent(tip) → tip.
	if !strings.HasPrefix(tgt.Spec.Rev, revParseCLI(t, dir, "main")) {
		t.Fatalf("the range must start at the merge base, got %q", tgt.Spec.Rev)
	}
}

func TestDiffPreviewRefusesASecondTarget(t *testing.T) {
	svc, _ := newCLIPreviewRepo(t)
	var out, errb strings.Builder
	code := cmdDiff(svc, []string{"--preview", "main...feat", "--cached"}, &out, &errb)
	if code != 2 {
		t.Fatalf("want exit 2, got %d", code)
	}
	if !strings.Contains(errb.String(), "one target only") {
		t.Fatalf("want the one-target message, got %q", errb.String())
	}
}

func TestDiffPreviewHunksNumberThePreviewDiff(t *testing.T) {
	svc, _ := newCLIPreviewRepo(t)
	var out, errb strings.Builder
	if code := cmdDiff(svc, []string{"--preview", "main...feat", "--hunks"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "a.txt") || !strings.Contains(out.String(), "1 @@ -") {
		t.Fatalf("want numbered hunks over the preview diff, got %q", out.String())
	}
}

// newCLIPreviewRepo builds a real repo: main with one commit, then feat with
// three commits, the second of which rewrites a line the first added — the
// same shape internal/domain's newPreviewRepo uses.
func newCLIPreviewRepo(t *testing.T) (*domain.Service, string) {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	writeFile(t, dir, "a.txt", "alpha\nbravo\ncharlie\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "seed")
	gitRun(t, dir, "checkout", "-b", "feat")
	writeFile(t, dir, "a.txt", "alpha\nbravo\ncharlie\nDELTA\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "c1 adds DELTA")
	writeFile(t, dir, "a.txt", "alpha\nbravo\ncharlie\nECHO\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "c2 rewrites DELTA")
	writeFile(t, dir, "b.txt", "bee\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "c3 adds b.txt")
	gitRun(t, dir, "checkout", "main")
	return domain.Open(dir), dir
}

// revParseCLI is the test's own rev resolver: full sha for a rev in dir.
func revParseCLI(t *testing.T, dir, rev string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", rev)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", rev, err)
	}
	return strings.TrimSpace(string(out))
}
