package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
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

// Follow-ups 5, item 4: navigating to an unfetched PR waits for the TUI's
// fetch (steerPRFetchWaitForTest) instead of the usual 2s, and when even that
// runs out it says the fetch is the reason. Serial: it writes both waits.
func TestNavigateToAnUnfetchedPRWaitsForTheFetch(t *testing.T) {
	oldReply, oldPR := steerReplyWaitForTest, steerPRFetchWaitForTest
	steerReplyWaitForTest, steerPRFetchWaitForTest = 100*time.Millisecond, 600*time.Millisecond
	defer func() { steerReplyWaitForTest, steerPRFetchWaitForTest = oldReply, oldPR }()
	repo := newCLIRepo(t)
	link := "gg://" + filepath.ToSlash(repo) + "/README.md@main...refs/gg/pr/7:1"

	// The TUI answers once its fetch is done — after the usual wait.
	dir := t.TempDir()
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply {
		time.Sleep(250 * time.Millisecond)
		return steer.Reply{ID: c.ID, OK: true, Detail: "opened pull request #7"}
	})
	var out, errb bytes.Buffer
	if code := runSession(dir, domain.Open(repo), []string{"navigate", link}, &out, &errb); code != 0 || !strings.Contains(out.String(), "opened pull request #7") {
		t.Fatalf("slow answer: exit %d stdout %q stderr %q", code, out.String(), errb.String())
	}

	// It never answers: the queued line names the fetch.
	dir = t.TempDir()
	livePresence(t, dir)
	out.Reset()
	errb.Reset()
	if code := runSession(dir, domain.Open(repo), []string{"navigate", link}, &out, &errb); code != 0 || !strings.Contains(out.String(), "the TUI is fetching pull request #7") {
		t.Fatalf("no answer: exit %d stdout %q stderr %q", code, out.String(), errb.String())
	}
}

// navigateWait is shared by gg session navigate and gg open: only a link to
// a PR this machine has not fetched waits for the fetch.
func TestNavigateWaitIsLongOnlyForAnUnfetchedPR(t *testing.T) {
	t.Parallel()
	unfetched := domain.Resolved{Preview: &domain.PreviewNoteSet{Source: "refs/gg/pr/7", Target: "main"}}
	if w := navigateWait(unfetched); !strings.Contains(w.queued, "pull request #7") {
		t.Fatalf("unfetched PR: %+v", w)
	}
	for _, res := range []domain.Resolved{
		{},
		{Preview: &domain.PreviewNoteSet{Source: "refs/gg/pr/7", Target: "main", Tip: "abc"}}, // fetched
		{Preview: &domain.PreviewNoteSet{Source: "feat/x", Target: "main"}},
	} {
		if w := navigateWait(res); strings.Contains(w.queued, "fetching") {
			t.Errorf("%+v: %+v", res.Preview, w)
		}
	}
}
