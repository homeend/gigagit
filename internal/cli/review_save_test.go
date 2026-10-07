package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

const reviewSaveDoc = `{"version":1,"summary":"## Summary\nfine","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"one"}]}]}`

// reviewSaveRepo: main + feat/x one commit ahead; returns dir and the preview link.
func reviewSaveRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.name", "t")
	gitRun(t, dir, "config", "user.email", "t@t")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("f.txt", "a\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	gitRun(t, dir, "checkout", "-q", "-b", "feat/x")
	write("f.txt", "a\nb\n")
	gitRun(t, dir, "commit", "-q", "-am", "feature")
	code, out, errb := runCLI(t, dir, "link", "--preview", "main...feat/x")
	if code != 0 {
		t.Fatalf("link --preview = %d %q", code, errb)
	}
	return dir, strings.TrimSpace(out)
}

func TestReviewSavePreviewLink(t *testing.T) {
	t.Parallel()
	dir, link := reviewSaveRepo(t)
	code, out, errb := runCLIStdin(t, dir, reviewSaveDoc, "review", "save", link, "--agent", "Claude Code", "--stdin", "--json")
	if code != 0 {
		t.Fatalf("save = %d %q", code, errb)
	}
	var got struct{ ID, Link, Warn string }
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.ID == "" || !strings.Contains(got.Link, "?review="+got.ID) {
		t.Fatalf("json = %q %v", out, err)
	}
	// It is the preview's review: `gg review show` reads it back with its remark.
	code, out, _ = runCLI(t, dir, "review", "show", got.Link)
	if code != 0 || !strings.Contains(out, "fine") || !strings.Contains(out, "one") {
		t.Fatalf("show = %d %q", code, out)
	}
}

func TestReviewSaveDryRun(t *testing.T) {
	t.Parallel()
	dir, link := reviewSaveRepo(t)
	code, out, errb := runCLI(t, dir, "review", "save", link, "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("dry-run = %d %q", code, errb)
	}
	var got struct{ Kind, Label, Range, Diff string }
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Kind != "preview" || got.Label != "main ... feat/x" ||
		got.Diff != got.Range || !strings.Contains(got.Range, "..") {
		t.Fatalf("dry-run json = %q %v", out, err)
	}
	// Nothing was stored.
	if code, _, _ = runCLI(t, dir, "review", "show", "latest"); code == 0 {
		t.Fatal("dry-run stored a review")
	}
}

func TestReviewSaveFileLinkReviewsWholeChange(t *testing.T) {
	t.Parallel()
	dir, _ := reviewSaveRepo(t)
	code, link, errb := runCLI(t, dir, "link", "f.txt:2", "--rev", "HEAD")
	if code != 0 {
		t.Fatalf("link = %d %q", code, errb)
	}
	code, out, errb := runCLI(t, dir, "review", "save", strings.TrimSpace(link), "--dry-run", "--json")
	var got struct{ Kind, Range string }
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil || got.Kind != "commit" || !strings.HasSuffix(got.Range, "^.."+strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))) {
		t.Fatalf("file link dry-run = %d %q %q", code, out, errb)
	}
}

func TestReviewSaveRefusals(t *testing.T) {
	t.Parallel()
	dir, link := reviewSaveRepo(t)
	if code, _, errb := runCLIStdin(t, dir, "just prose", "review", "save", link, "--agent", "a", "--stdin"); code != 1 || !strings.Contains(errb, "review document") {
		t.Fatalf("prose = %d %q, want 1 + a document error", code, errb)
	}
	if code, _, _ := runCLIStdin(t, dir, reviewSaveDoc, "review", "save", link, "--stdin"); code != 2 {
		t.Fatalf("no --agent = %d, want 2", code)
	}
	if code, _, _ := runCLI(t, dir, "review", "save", link, "--agent", "a"); code != 2 {
		t.Fatalf("no input = %d, want 2", code)
	}
	if code, _, _ := runCLI(t, dir, "review", "save", "gg://", "--dry-run"); code != 2 {
		t.Fatalf("malformed link = %d, want 2", code)
	}
	bare := link[:strings.Index(link, "@")] // gg://<repo> with no target
	if code, _, errb := runCLI(t, dir, "review", "save", bare, "--dry-run"); code != 1 || !strings.Contains(errb, "names no change") {
		t.Fatalf("bare repo link = %d %q, want 1", code, errb)
	}
}

func TestReviewSaveDryRunWorkingAndRoot(t *testing.T) {
	t.Parallel()
	dir, _ := reviewSaveRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	type target struct{ Kind, Range, Diff, Hunks, Checkout string }
	dry := func(link string) target {
		t.Helper()
		code, out, errb := runCLI(t, dir, "review", "save", strings.TrimSpace(link), "--dry-run", "--json")
		var got target
		if code != 0 || json.Unmarshal([]byte(out), &got) != nil {
			t.Fatalf("dry-run %s = %d %q %q", link, code, out, errb)
		}
		return got
	}
	_, link, _ := runCLI(t, dir, "link", "f.txt:3")
	got := dry(link)
	// Working changes: patches via `gg diff HEAD`; no hunk numbering exists for
	// HEAD → working tree, and the reads must run in the link's checkout.
	if got.Kind != "working" || got.Diff != "HEAD" || got.Hunks != "" || !domain.SamePath(got.Checkout, dir) {
		t.Fatalf("working dry-run = %+v (dir %s)", got, dir)
	}
	root := strings.TrimSpace(runGit(t, dir, "rev-list", "--max-parents=0", "HEAD"))
	_, link, _ = runCLI(t, dir, "link", "f.txt", "--rev", root)
	got = dry(link)
	// A root commit: `gg diff <sha>` would be the working tree vs the root.
	if got.Kind != "commit" || got.Diff != domain.EmptyTreeSHA1+".."+root || got.Hunks != root {
		t.Fatalf("root dry-run = %+v", got)
	}
}

func TestReviewSaveWorkingReviewIsCurrent(t *testing.T) {
	t.Parallel()
	dir, _ := reviewSaveRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, link, _ := runCLI(t, dir, "link", "f.txt:3")
	code, out, errb := runCLIStdin(t, dir, reviewSaveDoc, "review", "save", strings.TrimSpace(link), "--agent", "a", "--stdin", "--json")
	var saved struct{ ID string }
	if code != 0 || json.Unmarshal([]byte(out), &saved) != nil {
		t.Fatalf("save = %d %q %q", code, out, errb)
	}
	svc := openCLIService(t, dir)
	r, err := svc.Review(context.Background(), saved.ID)
	if err != nil || len(r.Files) == 0 || !domain.WorkingReviewState(r.Worktree, r.Files).Current {
		t.Fatalf("a working review stored by save must match the files it read: %+v %v", r.Files, err)
	}
}
