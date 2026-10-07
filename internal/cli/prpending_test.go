package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/forge/forgetest"
)

// Serial: swaps sessionGetenv and pendingWaitTimeout.
func inSession(t *testing.T) {
	t.Helper()
	old := sessionGetenv
	sessionGetenv = func(k string) string {
		if k == "GG_INBOX" {
			return t.TempDir()
		}
		return ""
	}
	oldWait := pendingWaitTimeout
	pendingWaitTimeout = 50 * time.Millisecond
	t.Cleanup(func() { sessionGetenv, pendingWaitTimeout = old, oldWait })
}

func TestAgentSendIsQueuedThenApproved(t *testing.T) {
	dir, head, fixtures := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	inSession(t)
	out, errs, code := runPR(t, dir, "send", "7", "--note", id, "--yes")
	if code != 3 || !strings.Contains(errs, "--yes is ignored inside a gg session") || !strings.Contains(out, "still pending: ") {
		t.Fatalf("exit %d out %q err %q", code, out, errs)
	}
	if ws := forgetest.Writes(t, fixtures); len(ws) != 0 {
		t.Fatalf("an agent's send posted: %v", ws)
	}
	pid := strings.Fields(strings.TrimPrefix(out[strings.Index(out, "still pending: "):], "still pending: "))[0]
	if _, errs, code := runPR(t, dir, "pending", "approve", pid, "--yes"); code != 1 || !strings.Contains(errs, "own terminal") {
		t.Fatalf("approve inside a session: exit %d %q", code, errs)
	}
	// The user, in a plain terminal:
	sessionGetenv = func(string) string { return "" }
	list, _, _ := runPR(t, dir, "pending")
	if !strings.Contains(list, pid) || !strings.Contains(list, "pending") || !strings.Contains(list, "#7") {
		t.Fatalf("list = %q", list)
	}
	if _, errs, code := runPR(t, dir, "pending", "approve", pid, "--yes"); code != 0 {
		t.Fatalf("approve: exit %d %s", code, errs)
	}
	if ws := forgetest.Writes(t, fixtures); len(ws) != 3 || ws[2].Op != "SubmitReview" {
		t.Fatalf("writes after approve = %v", ws)
	}
	out, _, code = runPR(t, dir, "pending", "wait", pid)
	if code != 0 || !strings.Contains(out, "sent 1 comments") {
		t.Fatalf("wait: exit %d %q", code, out)
	}
}

func TestAgentCancelsItsSend(t *testing.T) {
	dir, head, _ := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	inSession(t)
	out, _, _ := runPR(t, dir, "send", "7", "--note", id)
	pid := strings.Fields(strings.TrimPrefix(out[strings.Index(out, "still pending: "):], "still pending: "))[0]
	if _, errs, code := runPR(t, dir, "pending", "cancel", pid); code != 0 {
		t.Fatalf("cancel: %s", errs)
	}
	if _, errs, code := runPR(t, dir, "pending", "wait", pid); code != 1 || !strings.Contains(errs, "cancelled") {
		t.Fatalf("wait after cancel: exit %d %q", code, errs)
	}
}

func TestUserRejects(t *testing.T) {
	dir, head, fixtures := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	inSession(t)
	out, _, _ := runPR(t, dir, "send", "7", "--note", id)
	pid := strings.Fields(strings.TrimPrefix(out[strings.Index(out, "still pending: "):], "still pending: "))[0]
	sessionGetenv = func(string) string { return "" }
	if _, _, code := runPR(t, dir, "pending", "reject", pid); code != 0 {
		t.Fatal("reject failed")
	}
	if ws := forgetest.Writes(t, fixtures); len(ws) != 0 {
		t.Fatalf("a rejected send posted: %v", ws)
	}
}

// queuePID queues an agent's send and returns the pending entry's id.
func queuePID(t *testing.T, dir string, args ...string) string {
	t.Helper()
	inSession(t)
	out, errs, _ := runPR(t, dir, append([]string{"send", "7"}, args...)...)
	i := strings.Index(out, "still pending: ")
	if i < 0 {
		t.Fatalf("not queued: out %q err %q", out, errs)
	}
	sessionGetenv = func(string) string { return "" } // the user, in a plain terminal
	return strings.Fields(out[i+len("still pending: "):])[0]
}

func pendingState(t *testing.T, dir, pid string) string {
	t.Helper()
	list, _, _ := runPR(t, dir, "pending")
	for _, l := range strings.Split(list, "\n") {
		if strings.HasPrefix(l, pid) {
			return strings.Fields(l)[2]
		}
	}
	t.Fatalf("%s not listed: %q", pid, list)
	return ""
}

// editSnapshot rewrites the PR as GitHub now reports it.
func editSnapshot(t *testing.T, fixtures, old, new string) {
	t.Helper()
	p := filepath.Join(fixtures, "snapshot-7.json")
	b, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), old) {
		t.Fatalf("snapshot lacks %q: %v", old, err)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestApproveWithoutATerminalStaysPending(t *testing.T) {
	dir, head, fixtures := sendPRRepo(t)
	pid := queuePID(t, dir, "--note", addCLINote(t, dir, head, 5))
	_, errs, code := runPR(t, dir, "pending", "approve", pid) // no --yes, stdin is no terminal
	if code != 1 || !strings.Contains(errs, "needs a decision") || !strings.Contains(errs, "still pending") {
		t.Fatalf("exit %d err %q", code, errs)
	}
	if st := pendingState(t, dir, pid); st != "pending" {
		t.Fatalf("state = %s: the user's own confirm problem must not fail the agent's send", st)
	}
	if ws := forgetest.Writes(t, fixtures); len(ws) != 0 {
		t.Fatalf("writes = %v", ws)
	}
}

func TestApproveYesIntoAPendingReviewIsRefusedAndStaysPending(t *testing.T) {
	dir, head, fixtures := sendPRRepo(t)
	pid := queuePID(t, dir, "--note", addCLINote(t, dir, head, 5))
	editSnapshot(t, fixtures, `"viewerLatestReview":null`, `"viewerLatestReview":{"id":"PRR_mine","state":"PENDING"}`)
	_, errs, code := runPR(t, dir, "pending", "approve", pid, "--yes")
	if code != 1 || !strings.Contains(errs, "you have a review pending on GitHub") {
		t.Fatalf("exit %d err %q", code, errs)
	}
	if st := pendingState(t, dir, pid); st != "pending" {
		t.Fatalf("state = %s", st)
	}
	if ws := forgetest.Writes(t, fixtures); len(ws) != 0 {
		t.Fatalf("writes = %v", ws)
	}
}

func TestQueuedVerdictKeepsTheAgentsEvent(t *testing.T) {
	dir, _, fixtures := sendPRRepo(t)
	pid := queuePID(t, dir, "--verdict", "--event", "approve", "--body", "lgtm")
	if list, _, _ := runPR(t, dir, "pending"); !strings.Contains(list, "verdict (asks: approve)") {
		t.Fatalf("list = %q", list)
	}
	if _, errs, code := runPR(t, dir, "pending", "approve", pid, "--yes"); code != 0 {
		t.Fatalf("approve: exit %d %s", code, errs)
	}
	ws := forgetest.Writes(t, fixtures)
	if len(ws) == 0 || ws[len(ws)-1].Op != "SubmitReview" || ws[len(ws)-1].Vars["event"] != "APPROVE" {
		t.Fatalf("writes = %v", ws)
	}
}

func TestQueuedApproveOnYourOwnPRFails(t *testing.T) {
	dir, _, fixtures := sendPRRepo(t)
	pid := queuePID(t, dir, "--verdict", "--event", "approve")
	editSnapshot(t, fixtures, `"viewerDidAuthor":false`, `"viewerDidAuthor":true`)
	_, errs, code := runPR(t, dir, "pending", "approve", pid, "--yes")
	if code != 1 || !strings.Contains(errs, "your own pull request") {
		t.Fatalf("exit %d err %q", code, errs)
	}
	if st := pendingState(t, dir, pid); st != "failed" {
		t.Fatalf("state = %s: an impossible request fails", st)
	}
	if ws := forgetest.Writes(t, fixtures); len(ws) != 0 {
		t.Fatalf("writes = %v", ws)
	}
}
