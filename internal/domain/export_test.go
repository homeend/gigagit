package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/shelf"
)

// gitRun runs a git subcommand in dir, failing the test on error. Mirrors the
// inline `run` closure in newRealRepo (compare_test.go) for use outside it.
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

// writeCommit writes relPath=content in dir and commits it with msg.
func writeCommit(t *testing.T, dir, relPath, content, msg string) {
	t.Helper()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", relPath)
	gitRun(t, dir, "commit", "-m", msg)
}

func TestShelfAddCommitAndExportRoundTrip(t *testing.T) {
	repoDir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()

	initial := headHash(t, repoDir)

	writeCommit(t, repoDir, "src/a.txt", "alpha\n", "add a")
	sha := headHash(t, repoDir)
	writeCommit(t, repoDir, "src/a.txt", "alpha2\n", "edit a") // move HEAD past sha

	e, err := svc.ShelfAddCommit(ctx, sha, "")
	if err != nil {
		t.Fatalf("ShelfAddCommit: %v", err)
	}
	if !e.IsCommit() {
		t.Fatal("entry must be IsCommit")
	}

	files, name, err := svc.ExportShelfEntry(ctx, e)
	if err != nil {
		t.Fatalf("ExportShelfEntry: %v", err)
	}
	if !strings.HasPrefix(name, "commit-") {
		t.Fatalf("name = %q, want commit-<sha> prefix", name)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.RelPath] = string(f.Data)
	}
	if got["src/a.txt"] != "alpha\n" {
		t.Fatalf("exported src/a.txt = %q, want alpha\\n (content AT the commit)", got["src/a.txt"])
	}

	// Durability: the export reads the stored tar, so gc'ing the commit must not
	// break it. First make sha genuinely unreachable: rewind main below it so
	// neither "add a" nor "edit a" remains reachable from any ref, then expire
	// the reflog and gc-prune. Without this, sha stays an ancestor of main's
	// tip and git would never actually collect it, making the "durability"
	// re-export below pass trivially even if ExportShelfEntry secretly read
	// from git instead of the stored shelf tar.
	gitRun(t, repoDir, "update-ref", "refs/heads/main", initial)
	gitRun(t, repoDir, "checkout", "-f", "main")
	gitRun(t, repoDir, "reflog", "expire", "--expire=all", "--all")
	gitRun(t, repoDir, "gc", "--prune=now")

	// Prove the commit object is really gone before trusting the re-export.
	catFile := exec.Command("git", "cat-file", "-e", sha)
	catFile.Dir = repoDir
	if out, err := catFile.CombinedOutput(); err == nil {
		t.Fatalf("commit %s was not pruned; git cat-file -e succeeded (out=%s) — durability scenario not exercised", sha, out)
	}

	files2, _, err := svc.ExportShelfEntry(ctx, e)
	if err != nil {
		t.Fatalf("ExportShelfEntry after gc: %v", err)
	}
	if len(files2) != len(files) {
		t.Fatalf("after gc got %d files, want %d (durable)", len(files2), len(files))
	}
}

func TestTempExportBaseIsSiblingDotTmp(t *testing.T) {
	repoDir, svc := newRealRepo(t)
	ctx := context.Background()

	base, err := svc.TempExportBase(ctx)
	if err != nil {
		t.Fatalf("TempExportBase: %v", err)
	}
	want := filepath.Clean(repoDir) + ".tmp"
	if filepath.Clean(base) != want {
		// git may report the worktree path with symlinks resolved (e.g. /tmp on
		// macOS); fall back to comparing against the Worktrees()-reported path,
		// but still require the ".tmp" sibling shape.
		wts, werr := svc.Worktrees(ctx)
		if werr != nil || len(wts) == 0 {
			t.Fatalf("base = %q, want %q", base, want)
		}
		wantAlt := filepath.Clean(wts[0].Path) + ".tmp"
		if filepath.Clean(base) != wantAlt {
			t.Fatalf("base = %q, want %q (or %q)", base, want, wantAlt)
		}
	}
	if !strings.HasSuffix(base, ".tmp") {
		t.Fatalf("base = %q, want a .tmp suffix", base)
	}
	if filepath.Base(filepath.Dir(base)) != filepath.Base(filepath.Dir(repoDir)) {
		t.Fatalf("base = %q, want a sibling of repo dir %q (no extra path segment)", base, repoDir)
	}
}
