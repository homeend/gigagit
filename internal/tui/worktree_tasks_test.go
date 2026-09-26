package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// withOneWorktree lists the test repo as the only worktree, focused.
func withOneWorktree(m Model) Model {
	m.worktrees = []model.Worktree{{Path: m.currentWorktree, Branch: "main"}}
	m.focus = panelWorktrees
	return m
}

// selectWorktreeTask puts the Worktrees cursor on task id's ◆ row.
func selectWorktreeTask(t *testing.T, m Model, id domain.TaskID) Model {
	t.Helper()
	ents := m.worktreeEntries()
	for di, u := range m.displayIndices(panelWorktrees) {
		if u < len(ents) && ents[u].task == id {
			m.sel[panelWorktrees] = di
			return m
		}
	}
	t.Fatalf("no ◆ row for %s", id)
	return m
}

func TestWorktreesShowLiveHeadlessTasks(t *testing.T) {
	m := withOneWorktree(launchTestModel(t))
	id := submitReview(t, m, "sleep 5")
	ents := m.worktreeEntries()
	rows := m.worktreeRows(ents)
	found := false
	for i, e := range ents {
		if e.task == id {
			found = true
			if !strings.Contains(rows[i], "◆") || !strings.Contains(rows[i], "review") {
				t.Fatalf("row %q", rows[i])
			}
		}
	}
	if !found {
		t.Fatal("no ◆ row for the live headless task")
	}
	_ = domain.Tasks().Cancel(id)
	waitTaskState(t, id, taskEndedFn)
	for _, e := range m.worktreeEntries() {
		if e.task == id {
			t.Fatal("an ended task leaves Worktrees")
		}
	}
}

func TestTaskRowIsNotAWorktree(t *testing.T) {
	m := withOneWorktree(launchTestModel(t))
	id := submitReview(t, m, "sleep 5")
	m = selectWorktreeTask(t, m, id)
	if _, ok := m.selectedWorktree(); ok {
		t.Fatal("a ◆ row must not act as its worktree")
	}
}

func TestTaskRowMenuCancels(t *testing.T) {
	m := withOneWorktree(launchTestModel(t))
	id := submitReview(t, m, "sleep 5")
	m = selectWorktreeTask(t, m, id)
	rows := m.sessionMenuRows()
	if len(rows) == 0 || rows[0].id != "task-cancel" {
		t.Fatalf("menu %+v", rows)
	}
	_, _ = rows[0].run(m)
	info := waitTaskState(t, id, taskEndedFn)
	if info.State != domain.TaskCancelled {
		t.Fatalf("state %s", info.State)
	}
}

func TestTaskRowEnterOpensTasksTab(t *testing.T) {
	m := withOneWorktree(launchTestModel(t))
	m.loading = false
	id := submitReview(t, m, "sleep 5")
	m = selectWorktreeTask(t, m, id)
	m, _ = updateKey(m, "enter")
	p := layerOf[*sessionsPopup](m)
	if p == nil || p.tab != tabTasks {
		t.Fatalf("enter on ◆ opens the tasks tab: %+v", p)
	}
	if r, ok := p.currentTask(); !ok || r.id() != id {
		t.Fatal("cursor on the task")
	}
}
