package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/forge/forgetest"
	"github.com/homeend/gigagit/internal/model"
)

// prSendModel: main holds big.go (30 lines); PR #7 (refs/gg/pr/7 = feat)
// changes its line 5; the fake gh answers the PR and records every write.
// SERIAL (every test using it): it sets process env and lifts
// domain.ForgeDisabled.
func prSendModel(t *testing.T) (Model, string, string) {
	t.Helper()
	dir, _ := newRepoDir(t)
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	write := func(body, msg string) {
		if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "big.go")
		runGit(t, dir, "commit", "-q", "-m", msg)
	}
	write(strings.Join(lines, "\n")+"\n", "big")
	runGit(t, dir, "checkout", "-q", "-b", "feat")
	lines[4] = "line 5 changed"
	write(strings.Join(lines, "\n")+"\n", "change")
	head := gitOut(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "update-ref", "refs/gg/pr/7", head)
	runGit(t, dir, "checkout", "-q", "main")
	fixtures := filepath.Join(dir, ".git", "fakegh")
	t.Setenv(forgetest.EnvBin, forgetest.BuildFakeGH(t))
	t.Setenv(forgetest.EnvFixtures, fixtures)
	domain.ForgeDisabled = false
	t.Cleanup(func() { domain.ForgeDisabled = true })
	forgetest.Seed(t, fixtures, prSendFixtures(head))
	svc := domain.OpenTUI(dir)
	svc.UseNotesDir(t.TempDir()) // TestMain disables notes; this test needs them
	m := New(svc)
	// No bootstrap runs here (its batch holds the never-ending heartbeat):
	// a fresh model may still say "loading", which opsIdle reads, and every
	// send would be refused as "another operation is running".
	m.loading = false
	return m, dir, head
}

// prSendFixtures are the CLI's sendPRRepo answers (internal/cli/prsend_test.go):
// the PR snapshot before and after a submit, the view, an empty list, the repo.
func prSendFixtures(head string) map[string]string {
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
	return map[string]string{"snapshot-7.json": pr(""), "snapshot-7-sent.json": pr(sent), "pr-view-7.json": view,
		"pr-list.json": "[]", "repo-view.json": `{"nameWithOwner":"o/r","url":"https://github.com/o/r","sshUrl":"git@github.com:o/r.git"}`}
}

func addTUINote(t *testing.T, m Model, head string, line int, sum string) string {
	t.Helper()
	n, err := m.svc.NoteAdd(context.Background(), model.Note{Source: model.NoteSourceUser, Summary: sum,
		Preview: "main...refs/gg/pr/7", // written in PR #7's view
		Address: model.FileAddress{State: model.StateCommitted, Commit: head, Path: "big.go"},
		Side:    model.NoteSideNew, Range: [2]int{line, line}})
	if err != nil {
		t.Fatal(err)
	}
	return n.ID
}

// runToModal runs a forgeSendCmd chain until the op asks its question; it
// returns the model and the op's pending wait (the command that yields the
// op's next message). startOp returns a BATCH (the heartbeat beside the op
// wait), so each step's batch is flattened: the op's wait is the command
// whose message is an op message; the heartbeat's tick is dropped.
func runToModal(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	for i := 0; i < 50 && m.modal == nil; i++ {
		if cmd == nil {
			t.Fatalf("no modal (status %q)", m.statusMsg)
		}
		msg := cmd()
		if b, ok := msg.(tea.BatchMsg); ok {
			// Run the batch's commands one by one; keep the one the op answers.
			for _, c := range b {
				if c == nil {
					continue
				}
				if inner := c(); inner != nil {
					if _, tick := inner.(heartbeatMsg); !tick {
						nm, next := m.Update(inner)
						m, cmd = nm.(Model), next
					}
				}
			}
			continue
		}
		nm, next := m.Update(msg)
		m, cmd = nm.(Model), next
	}
	if m.modal == nil {
		t.Fatal("no modal")
	}
	return m, cmd
}

// Serial: env (prSendModel).
func TestSendANoteFromTheTUI(t *testing.T) {
	m, dir, head := prSendModel(t)
	id := addTUINote(t, m, head, 5, "look here")
	m, cmd := m.forgeSendCmd(domain.PRSendRequest{PR: 7, Notes: []string{id}})
	m, wait := runToModal(t, m, cmd)
	prompt := renderPrompt(m.modal.req)
	if !strings.HasPrefix(prompt, "Send to o/r #7:") || !strings.Contains(prompt, "+ big.go:5 look here") {
		t.Fatalf("confirm:\n%s", prompt)
	}
	if ws := forgetest.Writes(t, filepath.Join(dir, ".git", "fakegh")); len(ws) != 0 {
		t.Fatalf("nothing is written before the confirm: %v", ws)
	}
	nm, _ := m.resolveModal("send")
	m = driveOp(t, nm.(Model), wait)
	var ops []string
	for _, w := range forgetest.Writes(t, filepath.Join(dir, ".git", "fakegh")) {
		ops = append(ops, w.Op)
	}
	if strings.Join(ops, ",") != "StartReview,AddThread,SubmitReview" {
		t.Fatalf("writes = %v", ops)
	}
	notes, err := m.svc.PRNotes(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range notes["big.go"] {
		if r.Note.ID == id {
			t.Fatalf("the sent note is still local: %+v", r)
		}
	}
	if m.forgeSend != nil {
		t.Fatal("the send state is cleared once the op ends")
	}
}

// Serial: env. A plan error (here: nothing to send) never starts an op.
func TestSendPlanErrorIsSaidAndNothingRuns(t *testing.T) {
	m, _, _ := prSendModel(t)
	m, cmd := m.forgeSendCmd(domain.PRSendRequest{PR: 7, Notes: []string{"no-such-note"}})
	nm, _ := m.Update(cmd())
	mm := nm.(Model)
	if mm.running || mm.modal != nil || !strings.Contains(mm.statusMsg, "no-such-note") {
		t.Fatalf("running=%v modal=%v status=%q", mm.running, mm.modal != nil, mm.statusMsg)
	}
}

// Serial: env. "my draft review" with a verdict: one review, every local note,
// the event the user picked.
func TestSendMyDraftReviewWithAVerdict(t *testing.T) {
	m, dir, head := prSendModel(t)
	addTUINote(t, m, head, 5, "look here")
	m, _ = m.openSendReviewBody(7, domain.GroupMine)
	p := layerOf[*sendReviewPopup](m)
	p.body = newTextField("LGTM")
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m, wait := runToModal(t, m, cmd)
	if got := strings.Join(m.modal.req.Options, ","); got != "comment,approve,request-changes,abort" {
		t.Fatalf("options %s", got)
	}
	nm, _ := m.resolveModal("approve")
	m = driveOp(t, nm.(Model), wait)
	ws := forgetest.Writes(t, filepath.Join(dir, ".git", "fakegh"))
	last := ws[len(ws)-1]
	if last.Op != "SubmitReview" || last.Vars["event"] != "APPROVE" || !strings.Contains(last.Vars["body"].(string), "LGTM") {
		t.Fatalf("submit = %+v", last)
	}
}

// Serial: env. Reply & send: the draft is written, then sent as a reply.
func TestReplyAndSendFromThePRView(t *testing.T) {
	m, dir, _ := prSendModel(t)
	fixtures := filepath.Join(dir, ".git", "fakegh")
	b, _ := os.ReadFile(filepath.Join(fixtures, "snapshot-7-sent.json"))
	forgetest.Seed(t, fixtures, map[string]string{"snapshot-7.json": string(b)}) // a thread to answer
	ctx := context.Background()
	if _, err := m.svc.PRRevalidate(ctx, 7); err != nil {
		t.Fatal(err)
	}
	d, err := m.svc.NoteReply(ctx, "forge:PRRC_new1", model.Note{Source: model.NoteSourceUser, Summary: "on it"})
	if err != nil {
		t.Fatal(err)
	}
	nm, cmd := m.Update(noteMutatedMsg{sendPR: 7, sendID: d.ID})
	m, wait := runToModal(t, nm.(Model), cmd)
	if !strings.Contains(renderPrompt(m.modal.req), "reply: on it") {
		t.Fatalf("confirm:\n%s", renderPrompt(m.modal.req))
	}
	nm2, _ := m.resolveModal("send")
	driveOp(t, nm2.(Model), wait)
	ws := forgetest.Writes(t, fixtures)
	if len(ws) != 1 || ws[0].Op != "Reply" || ws[0].Vars["thread"] != "PRRT_new1" {
		t.Fatalf("writes %+v", ws)
	}
}

// Item 12 / Review Focus 4: the re-resolve a note edit triggers keeps the
// PR's group bars (it used to hand the view nil groups). Serial: prSendModel.
func TestAPRReResolveKeepsTheGroupBars(t *testing.T) {
	m, _, head := prSendModel(t)
	open := m.openPRPreviewCmd(model.PullRequest{Number: 7, State: "open", Target: "main"})().(previewOpenMsg)
	nm, _ := m.Update(open)
	m = nm.(Model)
	addTUINote(t, m, head, 5, "mine")
	msg := m.reopenPreviewCmd("", "refs/gg/pr/7", "main", "", "")().(previewOpenMsg)
	if g := msg.groups["big.go"]; len(g) == 0 || g[0] != domain.GroupMine {
		t.Fatalf("re-resolve groups = %v", msg.groups)
	}
	// F-i: what the HANDLER keeps — the view's bars, not just the message.
	nm, _ = m.Update(msg)
	if g := nm.(Model).filesPreviewGroups["big.go"]; len(g) == 0 || g[0] != domain.GroupMine {
		t.Fatalf("after the handler groups = %v", nm.(Model).filesPreviewGroups)
	}
}
