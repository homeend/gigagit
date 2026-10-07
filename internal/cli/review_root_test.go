package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `gg review <rev>` of a repository's FIRST commit reviews what that commit
// adds — it has no parent, so <rev>^..<rev> does not exist — and the review
// is stored like any commit's.
func TestReviewOfTheRootCommit(t *testing.T) {
	skipOnWindows(t)
	isolateReviewEnv(t)
	dir := newRepoDir(t) // root commit: README.md
	if err := os.WriteFile(filepath.Join(dir, "later.txt"), []byte("later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "later.txt")
	runGit(t, dir, "commit", "-qm", "second")
	root := strings.TrimSpace(runGit(t, dir, "rev-list", "--max-parents=0", "HEAD"))
	seen := filepath.Join(t.TempDir(), "seen.diff")
	writeReviewTool(t, dir, "Echo", `cp "$GG_REVIEW_DIFF" "`+seen+`"; printf '{"version":1,"summary":"S","files":[]}' > "$GG_MESSAGE_FILE"`)
	for _, rev := range []string{root, "HEAD~1"} {
		code, out, errb := runCLI(t, dir, "review", "--tool", "Echo", "--no-save", "--json", rev)
		if code != 0 {
			t.Fatalf("%s: exit=%d out=%q stderr=%q", rev, code, out, errb)
		}
		diff, err := os.ReadFile(seen)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(diff), "+++ b/README.md") || strings.Contains(string(diff), "later.txt") {
			t.Fatalf("%s: the reviewed diff is not the root commit's own change:\n%s", rev, diff)
		}
	}
	// Stored, it reads back as that commit's review.
	if code, out, errb := runCLI(t, dir, "review", "--tool", "Echo", root); code != 0 {
		t.Fatalf("stored: exit=%d out=%q stderr=%q", code, out, errb)
	}
	code, out, errb := runCLI(t, dir, "review", "show", "--json", "latest")
	if code != 0 || !strings.Contains(out, `"tip":"`+root+`"`) {
		t.Fatalf("show: exit=%d out=%q stderr=%q", code, out, errb)
	}
}
