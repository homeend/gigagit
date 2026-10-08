package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// Follow-ups 4: a navigate takes a link to a PR this repo has not fetched
// (the landing fetches it); a verb that needs the PR's commits still refuses.
func TestUnfetchedPRLinkResolvesOnlyForANavigate(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	link := "gg://" + filepath.ToSlash(dir) + "/README.md@main...refs/gg/pr/7:1"
	svc := domain.Open(dir)
	res, err := resolveLinkArg(t.Context(), svc, link, linkShapes{Ref: true, Pair: true, Content: true, UnfetchedPR: true}, "navigate")
	if err != nil || res.Preview == nil || res.Preview.Source != "refs/gg/pr/7" {
		t.Fatalf("navigate: res=%+v err=%v", res.Preview, err)
	}
	if _, err := resolveLinkArg(t.Context(), svc, link, linkShapes{Pair: true, Ref: true}, "review save"); err == nil || !strings.Contains(err.Error(), "holds both") {
		t.Fatalf("review save: err = %v, want the refusal", err)
	}
	var out, errb strings.Builder
	Run(dir, []string{"session", "navigate", link}, strings.NewReader(""), &out, &errb, "")
	if strings.Contains(errb.String(), "holds both") {
		t.Fatalf("gg session navigate refused at resolve: %q", errb.String())
	}
	b, err := os.ReadFile("open.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `UnfetchedPR: true}, "open")`) {
		t.Error("gg open does not take an unfetched PR link")
	}
}
