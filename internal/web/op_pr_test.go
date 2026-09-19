package web

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

// prFixture is a work repo whose origin is a bare repository holding
// refs/pull/7/head (one commit ahead of main, adding pr7.txt). It returns the
// work dir, the bare path and the PR head's sha.
func prFixture(t *testing.T) (dir, bare, head string) {
	t.Helper()
	isolateState(t)
	dir = newRepoDir(t, 1)
	bare = filepath.Join(t.TempDir(), "base.git")
	gitRun(t, dir, "clone", "-q", "--bare", dir, bare)
	gitRun(t, dir, "remote", "add", "origin", bare)
	gitRun(t, dir, "checkout", "-q", "-b", "pr7")
	if err := os.WriteFile(filepath.Join(dir, "pr7.txt"), []byte("from the pull request\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "pr 7")
	head = strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))
	gitRun(t, dir, "push", "-q", "origin", "HEAD:refs/pull/7/head")
	gitRun(t, dir, "checkout", "-q", "main")
	gitRun(t, dir, "branch", "-q", "-D", "pr7")
	return dir, bare, head
}

func startPROp(t *testing.T, ts *httptest.Server, op string, n int) (int, string) {
	t.Helper()
	var out struct {
		OpID string `json:"op_id"`
	}
	code := postJSON(t, ts, "/api/op", fmt.Sprintf(`{"op":%q,"number":%d}`, op, n), "application/json", "", &out)
	return code, out.OpID
}

func runPROp(t *testing.T, ts *httptest.Server, op string, n int) wireEvent {
	t.Helper()
	code, id := startPROp(t, ts, op, n)
	if code != 202 {
		t.Fatalf("%s #%d start = %d", op, n, code)
	}
	done, _ := findEvent(readSSE(t, ts, id, 30*time.Second), "done")
	return done
}

// waitPRRow polls the list until want accepts it (the post-op re-list is a
// background kick).
func waitPRRow(t *testing.T, ts *httptest.Server, want func(prListResp) bool) prListResp {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var out prListResp
		getJSON(t, ts, "/api/pr", &out)
		if out.Loaded && want(out) {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("the list never settled: %+v", out)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type prOpenResp struct {
	State      string `json:"state"`
	Label      string `json:"label"`
	Source     string `json:"source"`
	Target     string `json:"target"`
	Left       string `json:"left"`
	Right      string `json:"right"`
	SourceHash string `json:"source_hash"`
	PR         int    `json:"pr"`
}

var hex40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

func TestPRFetchOpBringsTheHead(t *testing.T) {
	dir, bare, head := prFixture(t)
	pr := openPR(7, "Add a thing")
	pr.HeadSHA = head
	ts, srv := prServe(t, dir, &fakeForge{open: []model.PullRequest{pr}, baseURL: bare})
	waitPRsLoaded(t, ts)
	ch, cancel := srv.liveHubRef().subscribe()
	defer cancel()

	if done := runPROp(t, ts, "pr-fetch", 7); done["ok"] != true {
		t.Fatalf("pr-fetch did not succeed: %v", done)
	}
	if got := strings.TrimSpace(gitRun(t, dir, "rev-parse", "refs/gg/pr/7")); got != head {
		t.Fatalf("refs/gg/pr/7 = %s, want %s", got, head)
	}
	if m, ok := recvLive(t, ch, 10*time.Second); !ok || !slices.Equal(m.Changed, []string{"prs"}) {
		t.Fatalf("a finished pr-fetch must re-list and emit prs, got %+v ok=%v", m, ok)
	}
	out := waitPRRow(t, ts, func(l prListResp) bool { return len(l.PRs) == 1 && l.PRs[0].Fetched })
	if out.PRs[0].Number != 7 {
		t.Errorf("row = %+v", out.PRs[0])
	}
}

func TestPRFetchRejectsBadNumbers(t *testing.T) {
	dir, bare, _ := prFixture(t)
	ts, _ := prServe(t, dir, &fakeForge{baseURL: bare})
	waitPRsLoaded(t, ts)
	for _, n := range []int{0, -1} {
		if code, _ := startPROp(t, ts, "pr-fetch", n); code != 400 {
			t.Errorf("pr-fetch #%d = %d, want 400", n, code)
		}
		if code, _ := startPROp(t, ts, "pr-forget", n); code != 400 {
			t.Errorf("pr-forget #%d = %d, want 400", n, code)
		}
	}
	if code, _ := startPROp(t, ts, "pr-fetch", 99); code != 422 {
		t.Errorf("pr-fetch of a PR the forge does not have = %d, want 422", code)
	}
}

func TestPROpenAnswersThePreviewShape(t *testing.T) {
	dir, bare, head := prFixture(t)
	pr := openPR(7, "Add a thing")
	pr.HeadSHA = head
	ts, _ := prServe(t, dir, &fakeForge{open: []model.PullRequest{pr}, baseURL: bare})
	waitPRsLoaded(t, ts)

	var before prOpenResp
	if code := getJSON(t, ts, "/api/pr/open?n=7", &before); code != 200 || before.State != "unfetched" || before.PR != 7 {
		t.Fatalf("before the fetch: code=%d %+v, want state unfetched", code, before)
	}
	if done := runPROp(t, ts, "pr-fetch", 7); done["ok"] != true {
		t.Fatalf("pr-fetch: %v", done)
	}
	// Straight after done — the background re-list may not have landed, and
	// the verdict must not depend on it.
	var out prOpenResp
	if code := getJSON(t, ts, "/api/pr/open?n=7", &out); code != 200 {
		t.Fatalf("open = %d", code)
	}
	if out.State != "ok" || out.PR != 7 || !strings.HasPrefix(out.Label, "PR #7") {
		t.Fatalf("got %+v", out)
	}
	if !hex40.MatchString(out.Left) || out.Right != head || out.SourceHash != head {
		t.Errorf("left=%q right=%q source_hash=%q head=%q", out.Left, out.Right, out.SourceHash, head)
	}
	if out.Source != "feat/x" || out.Target != "main" {
		t.Errorf("display names = %q → %q", out.Source, out.Target)
	}
}

func TestPROpenUnknownNumber(t *testing.T) {
	dir, bare, _ := prFixture(t)
	ts, _ := prServe(t, dir, &fakeForge{baseURL: bare})
	waitPRsLoaded(t, ts)
	if code := getJSON(t, ts, "/api/pr/open?n=99", nil); code != 404 {
		t.Errorf("a PR the page was never shown = %d, want 404", code)
	}
	for _, n := range []string{"abc", "0", "-3", "", "refs/gg/pr/7"} {
		if code := getJSON(t, ts, "/api/pr/open?n="+n, nil); code != 400 {
			t.Errorf("n=%q = %d, want 400", n, code)
		}
	}
}

// A merged PR's target already contains its head, so target...head is empty:
// the pair must fall back to the base sha the forge recorded.
func TestPROpenMergedUsesBaseSHA(t *testing.T) {
	dir, bare, head := prFixture(t)
	base := strings.TrimSpace(gitRun(t, dir, "rev-parse", "main"))
	gitRun(t, dir, "fetch", "-q", "origin", "refs/pull/7/head:refs/gg/pr/7")
	gitRun(t, dir, "merge", "-q", "--ff-only", head)
	merged := openPR(7, "Add a thing")
	merged.State, merged.HeadSHA, merged.BaseSHA = model.PRStateMerged, head, base
	ts, _ := prServe(t, dir, &fakeForge{byN: map[int]model.PullRequest{7: merged}, baseURL: bare})
	waitPRRow(t, ts, func(l prListResp) bool { return len(l.PRs) == 1 && l.PRs[0].State == "merged" })

	var out prOpenResp
	getJSON(t, ts, "/api/pr/open?n=7", &out)
	if out.State != "ok" || out.Left != base || out.Right != head {
		t.Fatalf("got %+v, want ok %s → %s", out, base, head)
	}
}

func TestPRForgetDropsRefAndRow(t *testing.T) {
	dir, bare, head := prFixture(t)
	gitRun(t, dir, "update-ref", "refs/gg/pr/5", head)
	closed := openPR(5, "An old one")
	closed.State = model.PRStateClosed
	ts, srv := prServe(t, dir, &fakeForge{byN: map[int]model.PullRequest{5: closed}, baseURL: bare})
	waitPRRow(t, ts, func(l prListResp) bool { return len(l.PRs) == 1 && l.PRs[0].State == "closed" && l.PRs[0].Fetched })
	ch, cancel := srv.liveHubRef().subscribe()
	defer cancel()

	if done := runPROp(t, ts, "pr-forget", 5); done["ok"] != true {
		t.Fatalf("pr-forget: %v", done)
	}
	if m, ok := recvLive(t, ch, 10*time.Second); !ok || !slices.Equal(m.Changed, []string{"prs"}) {
		t.Fatalf("want a prs emit, got %+v ok=%v", m, ok)
	}
	waitPRRow(t, ts, func(l prListResp) bool { return len(l.PRs) == 0 })
	if out := gitRun(t, dir, "for-each-ref", "refs/gg/pr/5"); out != "" {
		t.Errorf("refs/gg/pr/5 survived the forget: %s", out)
	}
}
