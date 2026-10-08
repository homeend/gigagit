package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
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
	if _, err := resolveLinkArg(t.Context(), svc, link, linkShapes{Pair: true, Ref: true}, "review save"); err == nil || !strings.Contains(err.Error(), "gg pr fetch 7") {
		t.Fatalf("review save: err = %v, want the refusal", err)
	}
	var out, errb strings.Builder
	Run(dir, []string{"session", "navigate", link}, strings.NewReader(""), &out, &errb, "")
	if strings.Contains(errb.String(), "holds both") || strings.Contains(errb.String(), "not fetched here") {
		t.Fatalf("gg session navigate refused at resolve: %q", errb.String())
	}
}

// gg open hands an unfetched PR's link to the launcher (the TUI's landing
// fetches it) instead of refusing it at resolve. Serial: LaunchTUI is global.
func TestOpenLaunchesAnUnfetchedPRLink(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	var got model.Link
	LaunchTUI = func(_ string, at model.Link) int {
		got = at
		return 0
	}
	t.Cleanup(func() { LaunchTUI = nil })
	var out, errb strings.Builder
	link := "gg://" + filepath.ToSlash(dir) + "/a.txt@main...refs/gg/pr/7:1"
	if code := cmdOpen(svc, []string{link}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	if got.Target.Preview == nil || got.Target.Preview.Source != "refs/gg/pr/7" || got.Line != 1 {
		t.Fatalf("launched link = %+v line %d", got.Target, got.Line)
	}
}
