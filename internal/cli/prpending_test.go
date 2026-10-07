package cli

import (
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
