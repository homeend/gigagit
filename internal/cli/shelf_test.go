package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shelfRepo is a real git temp repo with the shelf rooted in its own temp dir
// (hermetic: never touches the user's real state dir).
func shelfRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	return newRepoDir(t)
}

func TestShelfAddListRestoreRoundTrip(t *testing.T) {
	dir := shelfRepo(t)
	// README.md is committed as "hi\n" by newRepoDir; make an unstaged edit.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errb := runCLI(t, dir, "shelf", "add", "README.md")
	if code != 0 {
		t.Fatalf("add exit %d: %s", code, errb)
	}
	id := strings.TrimSpace(out)
	if id == "" {
		t.Fatalf("add printed no entry id")
	}

	code, out, _ = runCLI(t, dir, "shelf", "list")
	if code != 0 || !strings.Contains(out, "README.md") {
		t.Fatalf("list exit %d: %s", code, out)
	}

	// Delete the file entirely, then restore from the shelf to a new path.
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	code, _, errb = runCLI(t, dir, "shelf", "restore", id, "restored.txt")
	if code != 0 {
		t.Fatalf("restore exit %d: %s", code, errb)
	}
	got, err := os.ReadFile(filepath.Join(dir, "restored.txt"))
	if err != nil || string(got) != "v2\n" {
		t.Fatalf("restored = %q err %v, want v2", got, err)
	}
}

func TestShelfRestoreRequiresDest(t *testing.T) {
	dir := shelfRepo(t)
	code, _, _ := runCLI(t, dir, "shelf", "restore", "some-id")
	if code != 2 {
		t.Fatalf("missing dest should exit 2, got %d", code)
	}
}

func TestShelfRestoreRefusesExistingWithoutForce(t *testing.T) {
	dir := shelfRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out, _ := runCLI(t, dir, "shelf", "add", "README.md")
	id := strings.TrimSpace(out)

	// README.md still exists and differs from the shelved bytes? It IS the
	// shelved bytes here, so restoring onto it is a no-op (identical). Use a
	// different existing file to force the differ branch.
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("different\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, _ := runCLI(t, dir, "shelf", "restore", id, "other.txt")
	if code != 2 {
		t.Fatalf("existing differing dest w/o --force should exit 2, got %d", code)
	}
	code, _, errb := runCLI(t, dir, "shelf", "restore", "--force", id, "other.txt")
	if code != 0 {
		t.Fatalf("--force should succeed, got %d: %s", code, errb)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "other.txt"))
	if string(got) != "v2\n" {
		t.Fatalf("after --force other.txt = %q, want v2", got)
	}
}

func TestShelfCommitThenExport(t *testing.T) {
	dir := shelfRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "a.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "add a")
	sha := runGit(t, dir, "rev-parse", "HEAD")

	code, out, errb := runCLI(t, dir, "shelf", "commit", sha)
	if code != 0 {
		t.Fatalf("shelf commit exit=%d stderr=%s", code, errb)
	}
	// stdout: "shelved commit as <id>\n"
	fields := strings.Fields(out)
	if len(fields) == 0 {
		t.Fatalf("shelf commit printed no output")
	}
	id := fields[len(fields)-1]
	if id == "" {
		t.Fatalf("shelf commit produced no entry id, stdout=%q", out)
	}

	outDir := filepath.Join(t.TempDir(), "out")
	code, _, errb = runCLI(t, dir, "shelf", "export", "--dir", outDir, id)
	if code != 0 {
		t.Fatalf("shelf export exit=%d stderr=%s", code, errb)
	}
	got, err := os.ReadFile(filepath.Join(outDir, "src", "a.txt"))
	if err != nil || string(got) != "alpha\n" {
		t.Fatalf("exported a.txt = %q err=%v, want alpha\\n", got, err)
	}
}

func TestShelfExportRefusesExistingWithoutForce(t *testing.T) {
	dir := shelfRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "a.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "add a")
	sha := runGit(t, dir, "rev-parse", "HEAD")

	_, out, errb := runCLI(t, dir, "shelf", "commit", sha)
	fields := strings.Fields(out)
	if len(fields) == 0 {
		t.Fatalf("shelf commit printed no output, stderr=%s", errb)
	}
	id := fields[len(fields)-1]

	outDir := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	} // pre-exists -> triggers the overwrite/cancel decision

	code, _, _ := runCLI(t, dir, "shelf", "export", "--dir", outDir, id)
	if code != 2 {
		t.Fatalf("existing target dir w/o --force should exit 2, got %d", code)
	}
	code, _, errb = runCLI(t, dir, "shelf", "export", "--dir", outDir, "--force", id)
	if code != 0 {
		t.Fatalf("--force should succeed, got %d: %s", code, errb)
	}
	got, err := os.ReadFile(filepath.Join(outDir, "src", "a.txt"))
	if err != nil || string(got) != "alpha\n" {
		t.Fatalf("after --force a.txt = %q err=%v, want alpha\\n", got, err)
	}
}

func TestShelfExportUnknownEntry(t *testing.T) {
	dir := shelfRepo(t)
	code, _, errb := runCLI(t, dir, "shelf", "export", "no-such-id")
	if code != 1 {
		t.Fatalf("unknown entry export should exit 1, got %d: %s", code, errb)
	}
}

func TestShelfCommitUsageErrors(t *testing.T) {
	dir := shelfRepo(t)
	if code, _, _ := runCLI(t, dir, "shelf", "commit"); code != 2 {
		t.Fatalf("shelf commit without sha should exit 2, got %d", code)
	}
	if code, _, _ := runCLI(t, dir, "shelf", "export"); code != 2 {
		t.Fatalf("shelf export without id should exit 2, got %d", code)
	}
}

func TestShelfUsageErrors(t *testing.T) {
	dir := shelfRepo(t)
	if code, _, _ := runCLI(t, dir, "shelf"); code != 2 {
		t.Fatalf("bare shelf should exit 2, got %d", code)
	}
	if code, _, _ := runCLI(t, dir, "shelf", "bogus"); code != 2 {
		t.Fatalf("unknown subcommand should exit 2, got %d", code)
	}
	if code, _, _ := runCLI(t, dir, "shelf", "add"); code != 2 {
		t.Fatalf("add without paths should exit 2, got %d", code)
	}
}
