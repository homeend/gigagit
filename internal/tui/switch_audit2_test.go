package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repos"
	"github.com/homeend/gigagit/internal/steer"
)

// --- 7: a slot whose status never landed says so ---

func TestAFreshSlotShowsLoadingUntilItsStatusLands(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	if m.viewLoading() {
		t.Fatal("home loaded: must not say loading")
	}
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.switchView(other)
	if !m.viewLoading() {
		t.Fatal("a slot without a status read must say loading")
	}
	if !strings.Contains(m.View(), i18n.T("⏳ loading…")) {
		t.Fatal("the status line must show the loading marker")
	}
	m = landView(t, m)
	if m.viewLoading() {
		t.Fatal("the status landed: the marker must go")
	}
	m, _ = m.switchView(m.home)
	m, _ = m.switchView(other)
	if m.viewLoading() {
		t.Fatal("a slot seen once keeps its remembered status: no marker")
	}
}

// --- 8: the kick reads notes too; a slot load re-derives the WIP rows ---

func TestViewKickReadsTheNoteBadges(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.switchView(other)
	seen := map[sourceKey]bool{}
	for _, msg := range drainBatch(m.viewKickCmd()) {
		if d, ok := msg.(dataAvailableMsg); ok {
			seen[d.source] = true
		}
	}
	if !seen[srcStatus] || !seen[srcNotes] {
		t.Fatalf("kick read %v, want status and notes", seen)
	}
}

// --- 9: the session snapshot says what gg IS and what it SHOWS ---

func TestSnapshotNamesTheViewedWorktreeAndHomesBranch(t *testing.T) {
	m := loadedModel(t)
	m.snapshotWorktree = m.home
	homeBranch := m.status.Branch
	m, other := viewedOther(t, m)
	s := buildSessionSnapshot(m)
	if s.Repo.Worktree != m.home || s.Repo.Viewed != filepath.Clean(other) {
		t.Fatalf("worktree=%q viewed=%q, want home %q and viewed %q", s.Repo.Worktree, s.Repo.Viewed, m.home, other)
	}
	if s.Repo.Branch != homeBranch {
		t.Fatalf("branch=%q is the viewed slot's; want home's %q", s.Repo.Branch, homeBranch)
	}
}

// --- 10: a steer switch ask and a gg:// checkout take the fast path ---

func TestAcceptedSteerAskSwitchesWithoutAReload(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	m.steerDir = filepath.Join(t.TempDir(), "steer")
	m.snapshotWorktree = m.home
	m, _ = m.applySteer(steer.Command{ID: "n1", Cmd: "navigate", File: "x.go", Worktree: other})
	n := noticeByID(m, "steer_switch")
	if n == nil {
		t.Fatal("no ask")
	}
	m, _ = m.applyNoticeAction(*n, noticeActionByLabel(t, n, i18n.T("Switch to %s and show", filepath.Base(other))))
	if m.viewed != filepath.Clean(other) || m.home != m.viewed || !m.ready || m.loading {
		t.Fatalf("viewed=%q home=%q ready=%v loading=%v: want the slot swap, no reload", m.viewed, m.home, m.ready, m.loading)
	}
	if m.startAtCmd == nil || !m.startAtPending {
		t.Fatal("the replay must be armed")
	}
}

func TestLinkCheckoutSwitchesWithoutAReload(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	p := &gotoCommitPopup{}
	m = m.pushLayer(p)
	m, _ = m.switchToLink(p, gotoLinkSwitch{checkout: other, bare: true})
	if m.viewed != filepath.Clean(other) || m.home != m.viewed || !m.ready || m.loading {
		t.Fatalf("viewed=%q home=%q ready=%v loading=%v: want the slot swap, no reload", m.viewed, m.home, m.ready, m.loading)
	}
}

// --- lower: the dump, the MRU, a move of the viewed tree, the seed guard ---

func TestStateDumpNamesHomeAndViewed(t *testing.T) {
	m := loadedModel(t)
	m, other := viewedOther(t, m)
	m.pendingReturnView = m.home
	var b strings.Builder
	m.writeTUIState(&b, time.Now())
	out := b.String()
	for _, want := range []string{"home: " + m.home, "viewed: " + filepath.Clean(other), "pending return: " + m.home} {
		if !strings.Contains(out, want) {
			t.Fatalf("dump lacks %q:\n%s", want, out)
		}
	}
}

func TestAdoptTouchesTheRepoMRU(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	nm, cmd := m.guardedReRoot(other, true)
	m = nm.(Model)
	for _, msg := range drainBatch(cmd) {
		nm, _ := m.Update(msg)
		m = nm.(Model)
	}
	found := false
	for _, e := range repos.Load(repos.DefaultStatePath()) {
		if filepath.Clean(e.Path) == filepath.Clean(other) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the adopted worktree is not in the MRU: %+v", repos.Load(repos.DefaultStatePath()))
	}
}

func TestMovingTheViewedWorktreeGoesHomeFirst(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := viewedOther(t, m)
	var wt model.Worktree
	for _, w := range m.worktrees {
		if filepath.Clean(w.Path) == filepath.Clean(other) {
			wt = w
		}
	}
	p := &moveWorktreePopup{wt: wt, field: newTextField(filepath.Join(filepath.Dir(other), "wt2-moved"))}
	m = m.pushLayer(p)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.viewed != m.home {
		t.Fatalf("viewed=%q while its directory is being renamed; want home", m.viewed)
	}
	if !m.running {
		t.Fatal("the move must still start")
	}
	if m.opCancel != nil {
		m.opCancel()
	}
}

func TestSwitchViewRefusesBeforeTheSeed(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m.home, m.viewed = "", ""
	if nm, ok := m.switchView(other); ok || nm.home != "" {
		t.Fatalf("switched before the seed: ok=%v home=%q", ok, nm.home)
	}
}

var _ = context.Background
