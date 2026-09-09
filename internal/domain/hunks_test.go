package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
)

func TestParseDiffHunksCountForms(t *testing.T) {
	t.Parallel()
	patch := "diff --git a/src/search.ts b/src/search.ts\n" +
		"index 1111111..2222222 100644\n" +
		"--- a/src/search.ts\n" +
		"+++ b/src/search.ts\n" +
		"@@ -15,7 +15,9 @@ export function score\n" +
		" ctx\n" +
		"-old\n" +
		"+new\n" +
		"@@ -40 +42 @@\n" +
		"-a\n" +
		"+b\n" +
		"@@ -80,0 +90,3 @@ tail\n" +
		"+x\n" +
		"+y\n" +
		"+z\n"
	got := ParseDiffHunks(patch)
	if len(got) != 1 || got[0].Path != "src/search.ts" || got[0].OldPath != "" {
		t.Fatalf("files = %+v, want one src/search.ts", got)
	}
	want := []model.Hunk{
		{N: 1, Old: [2]int{15, 21}, New: [2]int{15, 23}, Header: "export function score"},
		{N: 2, Old: [2]int{40, 40}, New: [2]int{42, 42}, Header: ""},
		{N: 3, Old: [2]int{0, 0}, New: [2]int{90, 92}, Header: "tail"},
	}
	if len(got[0].Hunks) != len(want) {
		t.Fatalf("hunks = %+v, want %+v", got[0].Hunks, want)
	}
	for i, w := range want {
		if got[0].Hunks[i] != w {
			t.Errorf("hunk %d = %+v, want %+v (omitted count = 1; a zero count is [0,0])", i+1, got[0].Hunks[i], w)
		}
	}
}

func TestParseDiffHunksRenameBinaryAndDeletion(t *testing.T) {
	t.Parallel()
	patch := "diff --git a/old/name.go b/new/name.go\n" +
		"similarity index 90%\n" +
		"rename from old/name.go\n" +
		"rename to new/name.go\n" +
		"--- a/old/name.go\n" +
		"+++ b/new/name.go\n" +
		"@@ -1,2 +1,2 @@\n" +
		"-a\n" +
		"+b\n" +
		" c\n" +
		"diff --git a/img.png b/img.png\n" +
		"index 3333333..4444444 100644\n" +
		"Binary files a/img.png and b/img.png differ\n" +
		"diff --git a/gone.txt b/gone.txt\n" +
		"deleted file mode 100644\n" +
		"--- a/gone.txt\n" +
		"+++ /dev/null\n" +
		"@@ -1,3 +0,0 @@\n" +
		"-one\n" +
		"-two\n" +
		"-three\n" +
		"\\ No newline at end of file\n"
	got := ParseDiffHunks(patch)
	if len(got) != 3 {
		t.Fatalf("files = %+v, want 3", got)
	}
	if got[0].Path != "new/name.go" || got[0].OldPath != "old/name.go" {
		t.Errorf("rename = %q/%q, want new/name.go from old/name.go", got[0].Path, got[0].OldPath)
	}
	if got[1].Path != "img.png" || len(got[1].Hunks) != 0 {
		t.Errorf("binary = %+v, want a file row with zero hunks", got[1])
	}
	if got[2].Path != "gone.txt" || len(got[2].Hunks) != 1 {
		t.Fatalf("deletion = %+v, want gone.txt with one hunk", got[2])
	}
	if got[2].Hunks[0].New != [2]int{0, 0} || got[2].Hunks[0].Old != [2]int{1, 3} {
		t.Errorf("deletion hunk = %+v, want old 1-3 and new [0,0]", got[2].Hunks[0])
	}
}

func TestParseDiffHunksIgnoresBodyLinesThatLookLikeHeaders(t *testing.T) {
	t.Parallel()
	patch := "diff --git a/p.diff b/p.diff\n" +
		"--- a/p.diff\n" +
		"+++ b/p.diff\n" +
		"@@ -1,2 +1,2 @@\n" +
		"-@@ -9,9 +9,9 @@ not a header\n" +
		"+@@ -8,8 +8,8 @@ also not\n"
	got := ParseDiffHunks(patch)
	if len(got) != 1 || len(got[0].Hunks) != 1 {
		t.Fatalf("body lines starting with -/+ must never be read as headers: %+v", got)
	}
}

func TestHunkDiffSpecTargets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		cached bool
		rev    string
		want   model.DiffSpec
	}{
		{"worktree", false, "", model.DiffSpec{Paths: []string{"a.go"}}},
		{"cached", true, "", model.DiffSpec{Cached: true, Paths: []string{"a.go"}}},
		{"single commit is its OWN change", false, "abc123", model.DiffSpec{Rev: "abc123^..abc123", Paths: []string{"a.go"}}},
		{"explicit range passes through", false, "main..HEAD", model.DiffSpec{Rev: "main..HEAD", Paths: []string{"a.go"}}},
	}
	for _, c := range cases {
		got := HunkDiffSpec(c.cached, c.rev, []string{"a.go"})
		if got.Cached != c.want.Cached || got.Rev != c.want.Rev || len(got.Paths) != 1 || got.Paths[0] != "a.go" {
			t.Errorf("%s: HunkDiffSpec = %+v, want %+v", c.name, got, c.want)
		}
	}
}

// hunkRepo is a real repo whose working tree adds lines in two separate places,
// so the patch has two @@ hunks in a known order.
func hunkRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gittest.Run(t, dir, "init", "-b", "main")
	body := ""
	for i := 1; i <= 40; i++ {
		body += "line\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "add", "a.txt")
	gittest.Run(t, dir, "commit", "-m", "seed")
	lines := []byte(body)
	edited := string(lines[:5*len("line\n")]) + "TOP\n" +
		string(lines[5*len("line\n"):35*len("line\n")]) + "BOTTOM\n" +
		string(lines[35*len("line\n"):])
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDiffHunksAndHunkRangeOnRealRepo(t *testing.T) {
	t.Parallel()
	dir := hunkRepo(t)
	svc := svcIn(t, dir)
	ctx := context.Background()
	files, err := svc.DiffHunks(ctx, HunkDiffSpec(false, "", nil))
	if err != nil {
		t.Fatalf("DiffHunks: %v", err)
	}
	if len(files) != 1 || files[0].Path != "a.txt" || len(files[0].Hunks) != 2 {
		t.Fatalf("files = %+v, want a.txt with 2 hunks", files)
	}
	if files[0].Hunks[0].N != 1 || files[0].Hunks[1].N != 2 {
		t.Fatalf("hunks must be numbered 1..N in @@ order: %+v", files[0].Hunks)
	}
	side, rng, err := svc.HunkRange(ctx, HunkDiffSpec(false, "", []string{"a.txt"}), "a.txt", 2)
	if err != nil {
		t.Fatalf("HunkRange: %v", err)
	}
	if side != model.NoteSideNew || rng != files[0].Hunks[1].New {
		t.Fatalf("HunkRange(2) = %s %v, want new %v", side, rng, files[0].Hunks[1].New)
	}
	if _, _, err := svc.HunkRange(ctx, HunkDiffSpec(false, "", []string{"a.txt"}), "a.txt", 9); err == nil {
		t.Fatal("an out-of-range hunk number must error")
	} else if got := err.Error(); got != "a.txt has 2 hunks" {
		t.Fatalf("error = %q, want %q", got, "a.txt has 2 hunks")
	}
}
