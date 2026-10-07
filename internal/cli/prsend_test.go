package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/forge/forgetest"
)

// sendPRRepo: main holds big.go (30 lines); the PR #7 changes line 5;
// refs/gg/pr/7 is its head; the fake gh answers the PR, its (empty) threads
// and — once a review was submitted — the thread PRRT_new1 it created.
// Serial: env.
func sendPRRepo(t *testing.T) (dir, head, fixtures string) {
	t.Helper()
	dir = newRepoDir(t)
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	writeCommit(t, dir, "big.go", strings.Join(lines, "\n")+"\n", "big")
	runGit(t, dir, "checkout", "-q", "-b", "feat")
	lines[4] = "line 5 changed"
	writeCommit(t, dir, "big.go", strings.Join(lines, "\n")+"\n", "change")
	head = runGit(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "update-ref", "refs/gg/pr/7", head)
	runGit(t, dir, "checkout", "-q", "main")
	fixtures = filepath.Join(dir, ".git", "fakegh")
	t.Setenv(forgetest.EnvBin, forgetest.BuildFakeGH(t))
	t.Setenv(forgetest.EnvFixtures, fixtures)
	pr := func(threads string) string {
		return fmt.Sprintf(`{"data":{"repository":{"pullRequest":{"id":"PR_7","number":7,"title":"t","body":"","author":{"login":"ann"},
"state":"OPEN","isDraft":false,"reviewDecision":"","headRefName":"feat","isCrossRepository":false,"headRepositoryOwner":{"login":"ann"},
"headRepository":{"name":"r"},"baseRefName":"main","baseRefOid":"","headRefOid":"%s","url":"https://github.com/o/r/pull/7",
"createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-06T10:00:00Z","viewerDidAuthor":false,"viewerLatestReview":null,
"reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[%s]},"comments":{"pageInfo":{"hasNextPage":false},"nodes":[]},
"reviews":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}}}`, head, threads)
	}
	sent := `{"id":"PRRT_new1","path":"big.go","line":5,"startLine":null,"originalLine":5,"originalStartLine":null,"diffSide":"RIGHT",
"subjectType":"LINE","isResolved":false,"isOutdated":false,"comments":{"pageInfo":{"hasNextPage":false},"nodes":[{"id":"PRRC_new1",
"replyTo":null,"author":{"login":"me"},"body":"x","diffHunk":"","createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T10:00:00Z",
"pullRequestReview":{"id":"PRR_new"}}]}}`
	view := fmt.Sprintf(`{"number":7,"title":"t","author":{"login":"ann"},"state":"OPEN","isDraft":false,"headRefName":"feat",
"isCrossRepository":false,"baseRefName":"main","baseRefOid":"","headRefOid":"%s","url":"https://github.com/o/r/pull/7",
"createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-06T10:00:00Z","body":""}`, head)
	forgetest.Seed(t, fixtures, map[string]string{
		"snapshot-7.json": pr(""), "snapshot-7-sent.json": pr(sent), "pr-view-7.json": view,
		"pr-list.json": "[]", "repo-view.json": `{"nameWithOwner":"o/r","url":"https://github.com/o/r","sshUrl":"git@github.com:o/r.git"}`,
	})
	return dir, head, fixtures
}

func addCLINote(t *testing.T, dir, head string, line int) string {
	t.Helper()
	var out, errb strings.Builder
	code := Run(dir, []string{"note", "add", "--rev", head, "--file", "big.go", "--new-line", fmt.Sprint(line),
		"--summary", "look here", "--source", "user", "--json"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("note add: %s", errb.String())
	}
	var n struct{ ID string }
	if err := json.Unmarshal([]byte(out.String()), &n); err != nil || n.ID == "" {
		t.Fatalf("note add json %q: %v", out.String(), err)
	}
	return n.ID
}

func TestPRSendNoteWithYes(t *testing.T) {
	dir, head, fixtures := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	out, errs, code := runPR(t, dir, "send", "7", "--note", id, "--yes")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if !strings.Contains(out, "sent 1 comments to o/r #7") {
		t.Errorf("stdout = %q", out)
	}
	var ops []string
	for _, w := range forgetest.Writes(t, fixtures) {
		ops = append(ops, w.Op)
	}
	if strings.Join(ops, ",") != "StartReview,AddThread,SubmitReview" {
		t.Fatalf("writes = %v", ops)
	}
	notes, _, _ := runPR(t, dir, "notes", "7", "--json")
	if strings.Contains(notes, id) {
		t.Errorf("the sent note is still local:\n%s", notes)
	}
}

func TestPRSendWithoutYesInAPipeAsks(t *testing.T) {
	dir, head, fixtures := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	_, errs, code := runPR(t, dir, "send", "7", "--note", id)
	if code == 0 || !strings.Contains(errs, "forge.send") || !strings.Contains(errs, "--yes") {
		t.Fatalf("exit %d, stderr %q", code, errs)
	}
	if ws := forgetest.Writes(t, fixtures); len(ws) != 0 {
		t.Fatalf("nothing may be written before the confirm: %v", ws)
	}
}

func TestPRSendFailureKeepsTheNoteWithItsError(t *testing.T) {
	dir, head, fixtures := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	if err := os.WriteFile(filepath.Join(fixtures, "fail-SubmitReview"), []byte("HTTP 502: Bad Gateway"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errs, code := runPR(t, dir, "send", "7", "--note", id, "--yes")
	if code != 1 || !strings.Contains(errs, "HTTP 502") {
		t.Fatalf("exit %d, stderr %q", code, errs)
	}
	ops := forgetest.Writes(t, fixtures)
	if ops[len(ops)-1].Op != "DeleteReview" {
		t.Fatalf("the pending review must be deleted: %v", ops)
	}
	notes, _, _ := runPR(t, dir, "notes", "7", "--json")
	if !strings.Contains(notes, id) || !strings.Contains(notes, `"sync": "failed"`) {
		t.Fatalf("the note stays local, failed:\n%s", notes)
	}
}

func TestPRSendUsage(t *testing.T) {
	dir, _, _ := sendPRRepo(t)
	for _, args := range [][]string{
		{"send"}, {"send", "x"}, {"send", "7"}, {"send", "7", "--mine", "--review", "r1"},
		{"send", "7", "--event", "maybe", "--mine"}, {"send", "7", "--finish", "--discard"},
		{"send", "7", "--note", "n1", "--event", "approve"}, // a verdict needs --review, --mine or --verdict
	} {
		if _, _, code := runPR(t, dir, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}

func TestPRResolveIsImmediate(t *testing.T) {
	dir, _, fixtures := sendPRRepo(t)
	// Seed a thread to resolve.
	b, _ := os.ReadFile(filepath.Join(fixtures, "snapshot-7-sent.json"))
	forgetest.Seed(t, fixtures, map[string]string{"snapshot-7.json": string(b)})
	out, errs, code := runPR(t, dir, "resolve", "7", "PRRT_new1")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	ws := forgetest.Writes(t, fixtures)
	if len(ws) != 1 || ws[0].Op != "Resolve" || ws[0].Vars["thread"] != "PRRT_new1" || !strings.Contains(out, "sent 1 actions") {
		t.Fatalf("writes %v out %q", ws, out)
	}
}

func TestPRReplyDraftThenSend(t *testing.T) {
	dir, _, fixtures := sendPRRepo(t)
	b, _ := os.ReadFile(filepath.Join(fixtures, "snapshot-7-sent.json"))
	forgetest.Seed(t, fixtures, map[string]string{"snapshot-7.json": string(b)})
	out, errs, code := runPR(t, dir, "reply", "7", "PRRT_new1", "on it")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if ws := forgetest.Writes(t, fixtures); len(ws) != 0 {
		t.Fatalf("a draft reply writes nothing: %v", ws)
	}
	id := strings.Fields(out)[0] // "<id> draft reply to PRRT_new1"
	if _, errs, code := runPR(t, dir, "send", "7", "--note", id, "--yes"); code != 0 {
		t.Fatalf("send the draft: %s", errs)
	}
	ws := forgetest.Writes(t, fixtures)
	if len(ws) != 1 || ws[0].Op != "Reply" || ws[0].Vars["thread"] != "PRRT_new1" || ws[0].Vars["review"] != nil {
		t.Fatalf("writes = %+v", ws)
	}
}

func TestPRCommentsShowsThreadIDs(t *testing.T) {
	dir, _, fixtures := sendPRRepo(t)
	b, _ := os.ReadFile(filepath.Join(fixtures, "snapshot-7-sent.json"))
	forgetest.Seed(t, fixtures, map[string]string{"snapshot-7.json": string(b), "threads-7.json": string(b)})
	out, _, _ := runPR(t, dir, "comments", "7")
	if !strings.Contains(out, "[PRRT_new1] big.go:5 (new) me: x") {
		t.Fatalf("comments = %q", out)
	}
}

// writeCommit writes one file and commits it.
func writeCommit(t *testing.T, dir, name, body, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", name)
	runGit(t, dir, "commit", "-q", "-m", msg)
}

// With your own review pending on GitHub, --yes is always refused: the pipe's
// "rerun with --yes" hint would send the user in a circle.
func TestPRSendHintSkipsYesWhenAReviewIsPending(t *testing.T) {
	dir, head, fixtures := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	editSnapshot(t, fixtures, `"viewerLatestReview":null`, `"viewerLatestReview":{"id":"PRR_mine","state":"PENDING"}`)
	_, errs, code := runPR(t, dir, "send", "7", "--note", id)
	if code == 0 || strings.Contains(errs, "rerun with --yes") || !strings.Contains(errs, "review pending on GitHub") {
		t.Fatalf("exit %d, stderr %q", code, errs)
	}
}
