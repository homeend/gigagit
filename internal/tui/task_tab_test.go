package tui

import (
	"context"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
)

func TestTaskRowsOrderLiveThenHistory(t *testing.T) {
	t.Parallel()
	now := time.Now()
	live := []domain.TaskInfo{{ID: "b", Key: "review — a..b", State: domain.TaskRunning, Started: now}}
	hist := []domain.TaskRecord{{ID: "a", Key: "commit message — w @ 1234567", State: "done", Ended: now}, {ID: "b"}}
	rows := taskRows(live, hist, "", nil)
	if len(rows) != 2 || rows[0].live == nil || rows[1].record == nil || rows[1].record.ID != "a" {
		t.Fatalf("rows %+v", rows)
	}
	if got := taskRows(live, hist, "commit", nil); len(got) != 1 {
		t.Fatalf("filter: %+v", got)
	}
	if got := taskRows(live, hist, "", map[domain.TaskID]bool{"a": true}); len(got) != 1 {
		t.Fatalf("removed ids hide: %+v", got)
	}
}

func TestTaskRowText(t *testing.T) {
	t.Parallel()
	now := time.Now()
	r := taskRow{live: &domain.TaskInfo{Key: "review — a..b", Agent: "Claude Code", State: domain.TaskRunning, Started: now.Add(-90 * time.Second)}}
	got := taskRowText(r, now)
	for _, want := range []string{"review — a..b", "Claude Code", "running", "1m"} {
		if !strings.Contains(got, want) {
			t.Errorf("row %q lacks %q", got, want)
		}
	}
}

// submitReview submits a headless review task running line and returns its id.
func submitReview(t *testing.T, m Model, line string) domain.TaskID {
	t.Helper()
	spec, err := m.svc.ReviewTask(context.Background(), captureCmd(exttool.CatReview, line), domain.WorkingReviewTarget(), "")
	if err != nil {
		t.Fatal(err)
	}
	return domain.Tasks().Submit(spec)
}

func TestTaskTabEnterShowsResultAndCopy(t *testing.T) {
	m := launchTestModel(t)
	id := submitReview(t, m, "echo fine")
	waitTaskState(t, id, taskEndedFn)
	m, _ = m.openSessionsPopupOn(tabTasks, id)
	m, _ = updateKey(m, "enter")
	v := layerOf[*reviewView](m)
	if v == nil || v.copyText != "fine" || v.apply != nil {
		t.Fatalf("viewer %+v", v)
	}
}

func TestTaskTabApplyCommitMessage(t *testing.T) {
	m := launchTestModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatCommitMessage, `echo "feat: from tab"`)}
	spec, err := m.svc.CommitMessageTask(context.Background(), m.cfg.Tools.Command[0])
	if err != nil {
		t.Fatal(err)
	}
	id := domain.Tasks().Submit(spec)
	waitTaskState(t, id, taskEndedFn)
	m, _ = m.openSessionsPopupOn(tabTasks, id)
	m, _ = updateKey(m, "enter")
	v := layerOf[*reviewView](m)
	if v == nil || v.apply == nil {
		t.Fatalf("a commit message offers apply: %+v", v)
	}
	m, _ = updateKey(m, "a")
	if p := m.topCommitPopup(); p == nil || p.title.Value() != "feat: from tab" {
		t.Fatalf("a must open the commit box filled: %+v", p)
	}
}

func TestTaskTabKKCancelsQueued(t *testing.T) {
	m := launchTestModel(t)
	first := submitReview(t, m, "sleep 5")
	second := submitReview(t, m, "sleep 5") // same key: queued behind first
	m, _ = m.openSessionsPopupOn(tabTasks, second)
	m, _ = updateKey(m, "k")
	if info, _ := domain.Tasks().Get(second); info.State != domain.TaskQueued {
		t.Fatalf("one k only asks: %s", info.State)
	}
	m, _ = updateKey(m, "k")
	if info, _ := domain.Tasks().Get(second); info.State != domain.TaskCancelled {
		t.Fatalf("second = %s", info.State)
	}
	_ = domain.Tasks().Cancel(first)
	waitTaskState(t, first, taskEndedFn)
	if s, _ := domain.Tasks().Get(second); s.State != domain.TaskCancelled || !s.Started.IsZero() {
		t.Fatal("a cancelled queued task must never start")
	}
	_ = m
}

func TestTaskTabXRemovesEnded(t *testing.T) {
	m := launchTestModel(t)
	id := submitReview(t, m, "echo ok")
	waitTaskState(t, id, taskEndedFn)
	m, _ = m.openSessionsPopupOn(tabTasks, id)
	m, _ = updateKey(m, "x")
	for _, r := range domain.Tasks().History() {
		if r.ID == string(id) {
			t.Fatal("record still in history")
		}
	}
	p := layerOf[*sessionsPopup](m)
	if p == nil || len(p.taskRows) != 0 {
		t.Fatalf("removed task still listed: %+v", p)
	}
}

func TestTaskTabTabSwitches(t *testing.T) {
	m := launchTestModel(t)
	submitReview(t, m, "sleep 5")
	m, _ = m.openSessionsPopup(false)
	p := layerOf[*sessionsPopup](m)
	if p == nil {
		t.Fatalf("a live task alone opens the popup (status %q)", m.statusMsg)
	}
	if p.tab != tabTasks || !strings.Contains(p.render(m, ""), "review — ") {
		t.Fatalf("only tasks: the popup opens on the tasks tab, got %d", p.tab)
	}
	m, _ = updateKey(m, "tab")
	if p.tab != tabSessions {
		t.Fatal("tab → sessions")
	}
	m, _ = updateKey(m, "tab")
	if p.tab != tabTasks {
		t.Fatal("tab back → tasks")
	}
}

func TestAgentsPopupFixedHeightAndTabStrip(t *testing.T) {
	m := launchTestModel(t)
	for i := 0; i < 3; i++ {
		id := submitReview(t, m, "echo ok")
		waitTaskState(t, id, taskEndedFn)
	}
	m, _ = m.openSessionsPopup(false)
	p := layerOf[*sessionsPopup](m)
	if p == nil {
		t.Fatal("popup did not open")
	}
	lines := func() int { return strings.Count(ansi.Strip(p.render(m, "")), "\n") }
	tasksOut := ansi.Strip(p.render(m, ""))
	if !strings.Contains(tasksOut, "[AI tasks 3]") || !strings.Contains(tasksOut, "Agents 0") {
		t.Fatalf("tab strip missing:\n%s", tasksOut)
	}
	tasksH := lines()
	m, _ = updateKey(m, "tab")
	sessOut := ansi.Strip(p.render(m, ""))
	if !strings.Contains(sessOut, "[Agents 0]") {
		t.Fatalf("active tab not bracketed:\n%s", sessOut)
	}
	if got := lines(); got != tasksH {
		t.Fatalf("height changed with the tab: tasks %d, agents %d", tasksH, got)
	}
}
