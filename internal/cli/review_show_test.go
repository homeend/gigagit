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

const showReviewDoc = `{"version":1,"summary":"looks fine","files":[{"path":"a.txt","annotations":[
 {"newRange":[1,2],"summary":"two lines","rationale":"why"},
 {"oldRange":[1,1],"summary":"removed line"}]}]}`

// reviewedRepo is a two-commit repo whose HEAD carries one stored review
// document; it returns the dir and the review's id.
func reviewedRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.name", "t")
	gitRun(t, dir, "config", "user.email", "t@t")
	for _, body := range []string{"a\nb\nc\n", "A\nB\nc\n"} {
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", ".")
		gitRun(t, dir, "commit", "-q", "-m", "c")
	}
	head := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	svc := openCLIService(t, dir)
	tg := domain.ReviewTarget{Kind: domain.ReviewRange, Range: head + "^.." + head, Commit: head}
	id, _, err := svc.SaveReview(context.Background(), domain.SaveReview{Target: tg, Agent: "Claude", Text: showReviewDoc})
	if err != nil {
		t.Fatal(err)
	}
	return dir, id
}

func TestLinkReviewPrintsTheReviewLink(t *testing.T) {
	t.Parallel()
	dir, id := reviewedRepo(t)
	code, out, errb := runCLI(t, dir, "link", "--review", id)
	if code != 0 || !strings.Contains(out, "?review="+id) {
		t.Fatalf("link --review = %d %q %q", code, out, errb)
	}
	if code, out, _ = runCLI(t, dir, "link", "--review", "latest"); code != 0 || !strings.Contains(out, "?review="+id) {
		t.Fatalf("latest = %d %q", code, out)
	}
	if code, _, _ = runCLI(t, dir, "link", "--review", "deadbeef"); code != 1 {
		t.Fatalf("unknown id = %d, want 1", code)
	}
	if code, _, _ = runCLI(t, dir, "link", "--review", id, "--rev", "HEAD"); code != 2 {
		t.Fatalf("--review with another flag = %d, want 2", code)
	}
}

func TestReviewShowTextAndJSON(t *testing.T) {
	t.Parallel()
	dir, id := reviewedRepo(t)
	_, link, _ := runCLI(t, dir, "link", "--review", id)
	link = strings.TrimSpace(link)
	for _, arg := range []string{id, link, "latest"} {
		code, out, errb := runCLI(t, dir, "review", "show", arg)
		if code != 0 || !strings.Contains(out, "review "+id) || !strings.Contains(out, "looks fine") ||
			!strings.Contains(out, "[0] a.txt:1-2 — two lines") || !strings.Contains(out, "\n    why\n") ||
			!strings.Contains(out, "[1] a.txt:-1 — removed line") || !strings.Contains(out, "/a.txt@") {
			t.Fatalf("review show %s = %d\n%s\n%s", arg, code, out, errb)
		}
	}
	code, out, _ := runCLI(t, dir, "review", "show", "--json", id)
	var w domain.ReviewShow
	if code != 0 || json.Unmarshal([]byte(out), &w) != nil || w.ID != id || len(w.Remarks) != 2 ||
		w.Remarks[1].Side != "old" || w.Remarks[0].Link == "" || w.Link != link {
		t.Fatalf("json = %d %s", code, out)
	}
	if code, _, _ = runCLI(t, dir, "review", "show", "deadbeef"); code != 1 {
		t.Fatalf("unknown = %d, want 1", code)
	}
	if code, _, _ = runCLI(t, dir, "review", "show", "gg://x@@"); code != 2 {
		t.Fatalf("malformed = %d, want 2", code)
	}
}

// `show` is the subcommand even when a branch is named show; the branch is
// reviewed by its full ref name.
func TestReviewShowIsASubcommand(t *testing.T) {
	t.Parallel()
	dir, _ := reviewedRepo(t)
	gitRun(t, dir, "branch", "show")
	if code, _, errb := runCLI(t, dir, "review", "show"); code != 2 || !strings.Contains(errb, "usage: gg review show") {
		t.Fatalf("bare show = %d %q (want the show usage)", code, errb)
	}
}

func TestLinkResolvePrintsTheReview(t *testing.T) {
	t.Parallel()
	dir, id := reviewedRepo(t)
	_, link, _ := runCLI(t, dir, "link", "--review", id)
	code, out, errb := runCLI(t, dir, "link", "resolve", strings.TrimSpace(link))
	if code != 0 || !strings.Contains(out, "review "+id) {
		t.Fatalf("resolve = %d %q %q", code, out, errb)
	}
}
