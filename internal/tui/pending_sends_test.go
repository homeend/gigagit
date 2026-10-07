package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

// feedOnce runs cmd and feeds its message(s) to Update once — never the
// commands Update returns (a new notice arms the self-re-arming blink tick).
func feedOnce(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range msgsOf(cmd) { // nested batches flattened
		if msg == nil {
			continue
		}
		nm, _ := m.Update(msg)
		m = nm.(Model)
	}
	return m
}

func waitingSend(id string, req domain.PRSendRequest) domain.PendingSend {
	return domain.PendingSend{ID: id, Requester: "claude", Request: req, Created: time.Now().Add(-2 * time.Minute), State: domain.PendingWaiting}
}

func TestPendingSendsBecomeNotices(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	list := []domain.PendingSend{
		waitingSend("p1", domain.PRSendRequest{PR: 7, Notes: []string{"a", "b", "c", "d"}}),
		waitingSend("p2", domain.PRSendRequest{PR: 7, Review: "r1", Event: "approve"}),
	}
	nm, cmd := m.Update(pendingSendsMsg{gen: m.noticeGen, list: list})
	m = nm.(Model)
	n := noticeByID(m, pendingSendNoticeID("p1"))
	if n == nil || n.title != "claude wants to send 4 notes to #7" {
		t.Fatalf("notice = %+v", n)
	}
	n2 := noticeByID(m, pendingSendNoticeID("p2"))
	if n2 == nil || !strings.Contains(strings.Join(n2.detail, "\n"), "approve") {
		t.Fatalf("the asked verdict is shown: %+v", n2)
	}
	if !m.noticesUnread || cmd == nil {
		t.Fatal("a new queued send blinks the notice segment")
	}
	// The entry left the queue: the notice goes.
	nm, _ = m.Update(pendingSendsMsg{gen: m.noticeGen, list: list[1:]})
	if noticeByID(nm.(Model), pendingSendNoticeID("p1")) != nil {
		t.Fatal("a finished entry keeps its notice")
	}
}

func TestPendingSendsFromAnOldRepoAreDropped(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	nm, _ := m.Update(pendingSendsMsg{gen: m.noticeGen - 1, list: []domain.PendingSend{waitingSend("p1", domain.PRSendRequest{PR: 7, Mine: true})}})
	if len(nm.(Model).pendingSends) != 0 {
		t.Fatal("a read from before a repo switch must be dropped")
	}
}

// T6: no action dismisses the notice — the queue does.
func TestPendingNoticeActionsLeaveTheNoticeToTheQueue(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	nm, _ := m.Update(pendingSendsMsg{gen: m.noticeGen, list: []domain.PendingSend{waitingSend("p1", domain.PRSendRequest{PR: 7, Mine: true})}})
	m = nm.(Model)
	n := *noticeByID(m, pendingSendNoticeID("p1"))
	later := n.actions[len(n.actions)-1]
	if !later.sourced || later.run != nil {
		t.Fatalf("Later = %+v", later)
	}
	m, _ = m.applyNoticeAction(n, later)
	if noticeByID(m, pendingSendNoticeID("p1")) == nil || m.noticeSessionDismissed[n.id] {
		t.Fatal("Later must not dismiss a queued send")
	}
}

// Review Focus 2: approving while another op runs says so and sends nothing.
func TestApproveWhileAnOpRunsKeepsTheEntryWaiting(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	nm, _ := m.Update(pendingSendsMsg{gen: m.noticeGen, list: []domain.PendingSend{waitingSend("p1", domain.PRSendRequest{PR: 7, Mine: true})}})
	m = nm.(Model)
	m.running = true
	n := *noticeByID(m, pendingSendNoticeID("p1"))
	m, cmd := m.applyNoticeAction(n, n.actions[0])
	if cmd != nil || !strings.Contains(m.statusMsg, "another operation") || noticeByID(m, n.id) == nil {
		t.Fatalf("status %q cmd %v", m.statusMsg, cmd != nil)
	}
}

// Reject writes the outcome; the re-read drops the notice.
func TestRejectAnswersTheAgent(t *testing.T) {
	t.Parallel()
	dir, _ := newRepoDir(t)
	m := New(domain.OpenTUI(dir))
	ctx := context.Background()
	e, err := m.svc.PendingSendAdd(ctx, domain.PRSendRequest{PR: 7, Mine: true}, "claude")
	if err != nil {
		t.Fatal(err)
	}
	m = feedOnce(t, m, m.pendingSendsReadCmd(m.noticeGen))
	n := noticeByID(m, pendingSendNoticeID(e.ID))
	if n == nil {
		t.Fatal("no notice for the queued send")
	}
	reject := n.actions[1]
	m, cmd := m.applyNoticeAction(*n, reject)
	m = feedOnce(t, m, cmd)
	got, _ := m.svc.PendingSendGet(ctx, e.ID)
	if got.State != domain.PendingRejected {
		t.Fatalf("state %q", got.State)
	}
	if noticeByID(m, n.id) != nil {
		t.Fatal("the rejected entry's notice must go")
	}
}
