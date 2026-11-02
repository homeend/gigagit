package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// worktreeMarksModel: main + two linked worktrees; marks injected directly.
func worktreeMarksModel() Model {
	m := showInWorktreesModel()
	m.worktrees = []model.Worktree{
		{Path: "/repo", Branch: "main"},
		{Path: "/repo-wt/a", Branch: "a"},
		{Path: "/repo-wt/b", Branch: "b"},
	}
	m.currentWorktree = "/repo"
	m.worktreeMarks = map[string]domain.WorktreeMark{
		"/repo-wt/a": {Claim: &domain.ClaimInfo{Session: "p/s1", Agent: "claude", Since: time.Now(), Note: "https://x/1"}},
		"/repo-wt/b": {Reserved: true},
	}
	return m
}

func TestWorktreeRowShowsMarks(t *testing.T) {
	t.Parallel()
	m := worktreeMarksModel()
	joined := strings.Join(m.worktreeRows(m.worktreeEntries()), "\n")
	if !strings.Contains(joined, "⚑ claude") || !strings.Contains(joined, "⊘") {
		t.Fatalf("rows = %q", joined)
	}
}

func TestWorktreeClaimHintOnSelectedRow(t *testing.T) {
	t.Parallel()
	m := worktreeMarksModel()
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1 // the claimed worktree
	if h := m.worktreeClaimHint(); !strings.Contains(h, "https://x/1") || !strings.Contains(h, "claude") {
		t.Fatalf("hint = %q", h)
	}
	m.focus = panelBranches
	if h := m.worktreeClaimHint(); h != "" {
		t.Fatalf("hint off the Worktrees tab = %q", h)
	}
}

func TestWorktreeMarkMenuRows(t *testing.T) {
	t.Parallel()
	m := worktreeMarksModel()
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1
	ids := rowIDs(m.sessionMenuRows())
	if !slices.Contains(ids, "worktree-release-claim") || !slices.Contains(ids, "worktree-reserve") {
		t.Fatalf("ids = %v", ids)
	}
	m.sel[panelWorktrees] = 2 // the reserved one
	if ids := rowIDs(m.sessionMenuRows()); !slices.Contains(ids, "worktree-unreserve") || slices.Contains(ids, "worktree-release-claim") {
		t.Fatalf("ids = %v", ids)
	}
}

func TestReleaseClaimAsksFirst(t *testing.T) {
	t.Parallel()
	m := worktreeMarksModel()
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1
	var row actionRow
	for _, r := range m.sessionMenuRows() {
		if r.id == "worktree-release-claim" {
			row = r
		}
	}
	if row.run == nil {
		t.Fatal("no release row")
	}
	mm, _ := row.run(m)
	if got := mm.(Model); got.modal == nil || got.modal.req.ID != "worktree-release-claim" {
		t.Fatal("release must confirm first")
	}
}

func TestRecyclePickerMarksClaimedAndReserved(t *testing.T) {
	t.Parallel()
	m := worktreeMarksModel()
	m = m.openRecyclePicker("loose", "")
	var labels []string
	for _, r := range m.actionMenu.rows {
		labels = append(labels, r.label)
	}
	joined := strings.Join(labels, "\n")
	if !strings.Contains(joined, "(claimed by claude)") || !strings.Contains(joined, "(reserved)") {
		t.Fatalf("picker rows = %q", labels)
	}
}

// The TUI no longer confirms a recycle itself: the op asks recycle.blocked
// (engine + domain tests pin the question), so picking a worktree with a
// claim starts the op at once.
func TestRecycleIntoStartsTheOpWithoutItsOwnConfirm(t *testing.T) {
	t.Parallel()
	m := worktreeMarksModel()
	m.recycleBranch = "loose"
	nm, cmd := m.recycleInto("/repo-wt/a")
	got := nm.(Model)
	if got.modal != nil || cmd == nil || !got.running {
		t.Fatalf("modal=%v running=%v cmd=%v, want the op running and no TUI confirm", got.modal != nil, got.running, cmd != nil)
	}
}

// While an op holds the repo, the reserve/release rows (gated domain reads
// on the Update goroutine) are not offered — they would freeze the UI.
func TestWorktreeMarkRowsHiddenWhileAnOpRuns(t *testing.T) {
	t.Parallel()
	m := worktreeMarksModel()
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1
	m.running = true
	for _, id := range rowIDs(m.sessionMenuRows()) {
		if strings.HasPrefix(id, "worktree-") {
			t.Fatalf("row %q offered while an op runs", id)
		}
	}
}

func TestPausedMarkOnWorktreeAndBranchRows(t *testing.T) {
	t.Parallel()
	m := worktreeMarksModel()
	m.branches = []model.Branch{{Name: "main"}, {Name: "a"}, {Name: "b"}}
	m.worktrees[1].Paused, m.worktrees[1].Conflicted = "rebase", true
	m.worktrees[2].Paused = "merge"
	wt := strings.Join(m.worktreeRows(m.worktreeEntries()), "\n")
	br := strings.Join(m.branchRows(), "\n")
	for _, want := range []string{"✗ rebase conflict", "⑂ merge in progress"} {
		if !strings.Contains(wt, want) {
			t.Errorf("worktree rows lack %q:\n%s", want, wt)
		}
		if !strings.Contains(br, want) {
			t.Errorf("branch rows lack %q:\n%s", want, br)
		}
	}
	m.worktrees[1].Conflicted = false
	if got := pausedMark(m.worktrees[1]); got != "↻ rebase in progress" {
		t.Errorf("rebase without conflicts = %q", got)
	}
	if got := pausedMark(m.worktrees[0]); got != "" {
		t.Errorf("idle worktree = %q", got)
	}
}

func TestPausedWorktreeRaisesANoticeUntilItEnds(t *testing.T) {
	t.Parallel()
	m := worktreeMarksModel()
	m.noticeSessionDismissed = map[string]bool{}
	// The worktree on screen has its banner: no notice.
	m.worktrees[0].Paused = "merge"
	m, cmd := m.syncPausedNotices()
	if len(m.notices) != 0 || cmd != nil || m.noticesUnread {
		t.Fatalf("on-screen pause: notices %v", m.notices)
	}
	m.worktrees[1].Paused = "rebase"
	m, _ = m.syncPausedNotices()
	if len(m.notices) != 1 || !m.noticesUnread || !strings.Contains(m.notices[0].title, "rebase in progress") {
		t.Fatalf("notices = %+v unread %v", m.notices, m.noticesUnread)
	}
	// Dismissed, then the rebase stops on a conflict: a new notice, blinking again.
	m.noticeSessionDismissed[m.notices[0].id] = true
	m.noticesUnread = false
	m.worktrees[1].Conflicted = true
	m, _ = m.syncPausedNotices()
	if len(m.notices) != 1 || !m.noticesUnread || !strings.Contains(m.notices[0].title, "rebase conflict") {
		t.Fatalf("conflict notices = %+v unread %v", m.notices, m.noticesUnread)
	}
	m.worktrees[1].Paused, m.worktrees[1].Conflicted = "", false
	m, _ = m.syncPausedNotices()
	if len(m.notices) != 0 {
		t.Fatalf("op ended: notices %+v", m.notices)
	}
}
