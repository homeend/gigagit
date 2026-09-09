package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestRenderStat(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	renderStat(&buf, []model.DiffStat{
		{Path: "main.go", Added: 3, Deleted: 1},
		{Path: "img.png", Binary: true},
		{Path: "new.go", OldPath: "old.go", Added: 1},
	})
	want := "main.go +3 -1\nimg.png bin\nold.go => new.go +1 -0\n3 files +4 -1\n"
	if buf.String() != want {
		t.Fatalf("got:\n%q\nwant:\n%q", buf.String(), want)
	}
}

func TestRenderStatEmpty(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	renderStat(&buf, nil)
	if buf.String() != "" {
		t.Fatalf("empty diff must print nothing, got %q", buf.String())
	}
}

func TestDiffStatWorkingTree(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\nmore\nlines\n"), 0o644)
	code, out, errb := runCLI(t, dir, "diff", "--stat")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	want := "README.md +2 -0\n1 files +2 -0\n"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestDiffPatchDefault(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	code, out, _ := runCLI(t, dir, "diff")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out, "-hi") || !strings.Contains(out, "+changed") {
		t.Fatalf("patch missing hunks:\n%s", out)
	}
}

func TestDiffNameOnly(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	code, out, _ := runCLI(t, dir, "diff", "--name-only")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if out != "README.md\n" {
		t.Fatalf("got %q", out)
	}
}

func TestDiffEmptyPrintsNothing(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	for _, mode := range [][]string{{"diff"}, {"diff", "--stat"}, {"diff", "--name-only"}} {
		code, out, _ := runCLI(t, dir, mode...)
		if code != 0 || out != "" {
			t.Fatalf("%v: exit=%d out=%q (want 0, empty)", mode, code, out)
		}
	}
}

func TestDiffPathsRequireDashDash(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "diff", "main", "README.md")
	if code != 2 {
		t.Fatalf("exit=%d, want 2 (two positionals without --)", code)
	}
}

func TestDiffStatBothFlagsRejected(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "diff", "--stat", "--name-only")
	if code != 2 {
		t.Fatalf("exit=%d, want 2", code)
	}
}

func TestDiffPathsOnlyNoRev(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\nmore\n"), 0o644)
	code, out, errb := runCLI(t, dir, "diff", "--stat", "--", "README.md")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	want := "README.md +1 -0\n1 files +1 -0\n"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestDiffTwoPathsNoRev(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\nmore\n"), 0o644)
	code, _, errb := runCLI(t, dir, "diff", "--", "README.md", "CHANGELOG.md")
	if code != 0 {
		t.Fatalf("two paths after -- must be accepted, exit=%d stderr=%s", code, errb)
	}
}

func TestDiffHunksListsNumberedHunks(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	body := ""
	for i := 0; i < 40; i++ {
		body += "line\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	edited := strings.Replace(body, "line\n", "TOP\n", 1)
	edited = edited[:len(edited)-len("line\n")] + "BOTTOM\n"
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errb := runCLI(t, dir, "diff", "--hunks")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "a.txt\n") {
		t.Fatalf("stdout must name the file:\n%s", out)
	}
	if !strings.Contains(out, "  1 @@ -") || !strings.Contains(out, "  2 @@ -") {
		t.Fatalf("stdout must list two numbered hunks:\n%s", out)
	}
}

func TestDiffHunksJSONShape(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runCLI(t, dir, "diff", "--hunks", "--json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	var got []struct {
		Path  string `json:"path"`
		Hunks []struct {
			N      int    `json:"n"`
			Old    [2]int `json:"old"`
			New    [2]int `json:"new"`
			Header string `json:"header"`
		} `json:"hunks"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not the documented JSON array: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0].Path != "a.txt" || len(got[0].Hunks) != 1 || got[0].Hunks[0].N != 1 {
		t.Fatalf("json = %+v, want a.txt with hunk n=1", got)
	}
}

// A single commit positional means THAT COMMIT'S OWN change (<c>^..<c>) — the
// same patch a `gg note add --rev <c>` note anchors to — so the number an agent
// reads is the number it can pass back.
func TestDiffHunksSingleCommitIsItsOwnChange(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "grow")
	sha := runGit(t, dir, "rev-parse", "HEAD")

	code, out, errb := runCLI(t, dir, "diff", "--hunks", sha)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "a.txt") || !strings.Contains(out, "  1 @@") {
		t.Fatalf("a clean checkout must still show the commit's own hunk:\n%s", out)
	}
}

func TestDiffHunksRejectsStatCombination(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir, "diff", "--hunks", "--stat")
	if code != 2 {
		t.Fatalf("exit=%d stderr=%s, want 2 (usage error)", code, errb)
	}
	if !strings.Contains(errb, "mutually exclusive") {
		t.Fatalf("stderr must name the conflict, got %q", errb)
	}
}

// --cached with a single commit under --hunks is rejected: HunkDiffSpec's
// single-commit branch always builds <c>^..<c> (that commit's own change)
// regardless of --cached, so combining the two would silently drop --cached
// rather than doing what it looks like it asks for.
func TestDiffHunksCachedWithCommitRejected(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	sha := runGit(t, dir, "rev-parse", "HEAD")

	code, _, errb := runCLI(t, dir, "diff", "--hunks", "--cached", sha)
	if code != 2 {
		t.Fatalf("exit=%d stderr=%s, want 2 (usage error)", code, errb)
	}
	if !strings.Contains(errb, "--cached") {
		t.Fatalf("stderr must name the conflict, got %q", errb)
	}
}
