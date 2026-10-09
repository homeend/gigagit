package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/repos"
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

// A review link into ANOTHER checkout reads the review there: the check and
// the read must use the same repository's store.
//
// SERIAL — no t.Parallel(): withState writes the package's RepoStatePath.
func TestReviewShowFollowsTheLinksCheckout(t *testing.T) {
	dirA, id := reviewedRepo(t)
	dirB, _ := reviewedRepo(t)
	state := withState(t)
	if err := repos.Touch(state, dirA, "", time.Unix(9000, 0)); err != nil { // A was opened in gg once
		t.Fatal(err)
	}
	_, link, _ := runCLI(t, dirA, "link", "--review", id)
	code, out, errb := runCLI(t, dirB, "review", "show", strings.TrimSpace(link))
	if code != 0 || !strings.Contains(out, "review "+id) {
		t.Fatalf("from another checkout = %d\n%s\n%s", code, out, errb)
	}
}

// A working review compared HEAD with the working tree: the header says so.
func TestPrintReviewShowNamesWorkingChanges(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	printReviewShow(&b, domain.ReviewShow{ID: "rv1", Agent: "Echo", Working: true})
	if first := strings.SplitN(b.String(), "\n", 2)[0]; !strings.HasSuffix(first, " · working changes") {
		t.Fatalf("header = %q", first)
	}
}

// A review link with a path shows that file's remarks; with a line, the
// remark there. Every remark prints its review link and id.
func TestReviewShowNarrowsToAFileOrRemarkLink(t *testing.T) {
	t.Parallel()
	dir, id := reviewedRepo(t)
	svc := openCLIService(t, dir)
	ctx := context.Background()
	rl0, err := svc.ReviewRemarkLink(ctx, "review:"+id+":0")
	if err != nil {
		t.Fatal(err)
	}
	code, out, errb := runCLI(t, dir, "review", "show", id)
	if code != 0 || !strings.Contains(out, "[0] a.txt:1-2") || !strings.Contains(out, "[1] a.txt:-1") || !strings.Contains(out, rl0) {
		t.Fatalf("whole review = %d %q %q", code, out, errb)
	}
	code, out, errb = runCLI(t, dir, "review", "show", rl0)
	if code != 0 || !strings.Contains(out, "[0] a.txt:1-2") || strings.Contains(out, "[1]") || !strings.Contains(out, "id review:"+id+":0") {
		t.Fatalf("remark link = %d %q %q", code, out, errb)
	}
	code, out, errb = runCLI(t, dir, "review", "show", "--json", rl0)
	var rs struct {
		Remarks []struct {
			N          int
			ReviewLink string `json:"review_link"`
		}
	}
	if code != 0 || json.Unmarshal([]byte(out), &rs) != nil || len(rs.Remarks) != 1 || rs.Remarks[0].ReviewLink != rl0 {
		t.Fatalf("remark link --json = %d %q %q", code, out, errb)
	}
	fl, err := svc.ReviewFileLink(ctx, id, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	code, out, errb = runCLI(t, dir, "review", "show", fl)
	if code != 0 || !strings.Contains(out, "[0]") || !strings.Contains(out, "[1]") {
		t.Fatalf("file link = %d %q %q", code, out, errb)
	}
}

// Narrowed to a file or remark, the outdated threads stay out: a reply never
// recorded its remark's file, so none can be shown as belonging there. The
// view says how many it left out and where they are.
func TestReviewShowNarrowedHidesOutdatedThreads(t *testing.T) {
	t.Parallel()
	rs := domain.ReviewShow{ID: "rv1", Remarks: []domain.ReviewShowRemark{{N: 0, Path: "a.txt", Side: "new", Start: 1, End: 1}},
		Outdated: []domain.ReviewShowOutdated{{Summary: "gone one"}, {Summary: "gone two"}}}
	at := domain.Resolved{}
	at.Addr.Path = "a.txt"
	got := narrowReviewShow(rs, at)
	if len(got.Outdated) != 0 || got.OutdatedHidden != 2 {
		t.Fatalf("narrowed: outdated %v, hidden %d", got.Outdated, got.OutdatedHidden)
	}
	var b strings.Builder
	printReviewShow(&b, got)
	if strings.Contains(b.String(), "gone one") || !strings.Contains(b.String(), "2 outdated threads not shown — gg review show rv1 lists them") {
		t.Fatalf("printed:\n%s", b.String())
	}
	if whole := narrowReviewShow(rs, domain.Resolved{}); len(whole.Outdated) != 2 || whole.OutdatedHidden != 0 {
		t.Fatalf("a link with no path narrows nothing: %+v", whole)
	}
}

// The review's text is its SUMMARY on the wire (spec §0): "overview" is
// the stored walk, added later.
func TestReviewShowJSONSaysSummary(t *testing.T) {
	t.Parallel()
	dir, _ := reviewedRepo(t) // review_show_test.go's fixture: one stored review on HEAD
	code, out, errs := runCLI(t, dir, "review", "show", "--json", "latest")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["summary"]; !ok {
		t.Fatalf("no \"summary\" key: %s", out)
	}
	if _, ok := got["overview"]; ok {
		t.Fatalf("\"overview\" must not carry the summary any more: %s", out)
	}
}

func TestReviewShowPrintsTheOverview(t *testing.T) {
	t.Parallel()
	dir, link := reviewSaveRepo(t)
	doc := `{"version":1,"summary":"the summary","overview":"Walk: [here](f.txt:1)","files":[]}`
	if code, _, errs := runCLIStdin(t, dir, doc, "review", "save", link, "--agent", "c", "--stdin"); code != 0 {
		t.Fatalf("save: %s", errs)
	}
	code, out, _ := runCLI(t, dir, "review", "show", "latest")
	if code != 0 || !strings.Contains(out, "the summary\n") || !strings.Contains(out, "\nOverview\nWalk: [here](f.txt:1)") {
		t.Fatalf("show =\n%s", out)
	}
	_, outJ, _ := runCLI(t, dir, "review", "show", "--json", "latest")
	var got struct {
		Summary  string `json:"summary"`
		Overview string `json:"overview"`
	}
	if err := json.Unmarshal([]byte(outJ), &got); err != nil || got.Summary != "the summary" || got.Overview != "Walk: [here](f.txt:1)" {
		t.Fatalf("json %q: %v", outJ, err)
	}
}

// Review Focus 1: a prose review has no document — its text is the summary,
// there is no overview, nothing panics. The CLI refuses to save prose, so
// the review is stored through the service the CLI itself opens.
func TestReviewShowProseReview(t *testing.T) {
	t.Parallel()
	dir, _ := reviewedRepo(t) // review_show_test.go: one document review on HEAD
	svc := openCLIService(t, dir)
	head := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	tg := domain.ReviewTarget{Kind: domain.ReviewRange, Range: head + "^.." + head, Commit: head}
	if _, _, err := svc.SaveReview(context.Background(), domain.SaveReview{Target: tg, Agent: "c", Text: "just prose, not a document"}); err != nil {
		t.Fatal(err)
	}
	code, out, errs := runCLI(t, dir, "review", "show", "latest")
	if code != 0 || !strings.Contains(out, "just prose, not a document") || strings.Contains(out, "\nOverview\n") {
		t.Fatalf("exit %d out=%q err=%q", code, out, errs)
	}
}
