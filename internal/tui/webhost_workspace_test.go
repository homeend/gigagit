package tui

import (
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gitwatch"
	"github.com/homeend/gigagit/internal/model"
)

// servedWorkspace is a loaded two-member workspace with a fake host serving.
func servedWorkspace(t *testing.T, blocked bool) (Model, *repoView, *fakeWebHost) {
	t.Helper()
	f := &fakeWebHost{}
	m := loadedModel(t)
	m.web = newWebHostState()
	m.web.host = f
	m, b := openTwo(t, m)
	m.webActive = blocked
	return m, b, f
}

func TestPushWorkspaceOnOpenLeaveEditAdoptLoad(t *testing.T) {
	m, b, f := servedWorkspace(t, false)
	m.pushWebWorkspace()
	if f.lastWS() == nil || f.lastWS().Name != "shop" || len(f.lastWS().Members) != 2 {
		t.Fatalf("open: %+v", f.lastWS())
	}
	n := len(f.workspaces)
	m.pushWebWorkspace()
	if len(f.workspaces) != n {
		t.Fatal("nothing moved: no second push")
	}
	m.ws.name = "renamed"
	m.pushWebWorkspace()
	if f.lastWS().Name != "renamed" {
		t.Fatalf("edit: %+v", f.lastWS())
	}
	m.ws.live = 1
	m.pushWebWorkspace()
	if f.lastWS().Live != 1 {
		t.Fatalf("adopt: %+v", f.lastWS())
	}
	b.missing = true
	m.pushWebWorkspace()
	if !f.lastWS().Members[1].Missing {
		t.Fatalf("member load: %+v", f.lastWS())
	}
	m.ws = nil
	m.pushWebWorkspace()
	if f.lastWS() != nil {
		t.Fatalf("leave: %+v", f.lastWS())
	}
}

func TestBlockedMemberWatchForwardsNotLoads(t *testing.T) {
	m, b, f := servedWorkspace(t, true)
	w := &gitwatch.Watcher{}
	b.watcher = w
	seq := b.loadSeq
	_, cmd := m.onMemberWatchEvent(memberWatchEventMsg{svc: b.svc, gen: b.watchGen})
	if len(f.changed) != 1 || f.changed[0] != b.mainPath {
		t.Fatalf("changed = %v", f.changed)
	}
	if b.loadSeq != seq {
		t.Fatal("a blocked terminal must not read the member itself")
	}
	if cmd == nil {
		t.Fatal("the listener must be re-armed")
	}
	b.watcher = nil
}

func TestBlockedPollForwards(t *testing.T) {
	m, b, f := servedWorkspace(t, true)
	b.polling = true
	seq := b.loadSeq
	_, cmd := m.onMemberPollTick(memberPollTickMsg{svc: b.svc, gen: b.watchGen})
	if len(f.changed) != 1 || f.changed[0] != b.mainPath || b.loadSeq != seq || cmd == nil {
		t.Fatalf("changed = %v, seq %d→%d, cmd %v", f.changed, seq, b.loadSeq, cmd != nil)
	}
}

func TestBlockedFetchDoneForwards(t *testing.T) {
	m, b, f := servedWorkspace(t, true)
	b.watcher = nil
	seq := b.loadSeq
	_, _ = m.memberFetchDone(bgFetchDoneMsg{svc: b.svc})
	if len(f.changed) != 1 || f.changed[0] != b.mainPath || b.loadSeq != seq {
		t.Fatalf("changed = %v, seq %d→%d", f.changed, seq, b.loadSeq)
	}
}

func TestBlockedRefreshTickRunsOnlyMemberFetches(t *testing.T) {
	m, b, _ := servedWorkspace(t, true)
	b.cfg.Refresh.Enabled = true
	b.cfg.Refresh.Fetch = 60
	m.cfg.Refresh.Enabled = true
	m.cfg.Refresh.Status = 1
	m.bgQueue = []refreshItem{{source: srcStatus}} // queued before the flip
	t0 := time.Unix(3_000_000, 0)
	m, _ = m.refreshTick(t0) // the member is stamped; nothing runs
	if m.bgBusy {
		t.Fatalf("ran %+v", m.bgActiveItem)
	}
	m, cmd := m.refreshTick(t0.Add(61 * time.Second))
	if cmd == nil || !m.bgBusy || m.bgActiveItem != memberFetchItem(b.key) {
		t.Fatalf("active %+v, busy %v, cmd %v", m.bgActiveItem, m.bgBusy, cmd != nil)
	}
}

func TestBlockedHealthStillEstablishesIdentity(t *testing.T) {
	m := loadedModel(t)
	m.webActive = true
	home := m.homeRepo()
	m.gitCommonDir, home.mainKnown = "", false
	nm, _ := m.applyRepoHealth(repoHealthMsg{gen: m.noticeGen, health: model.RepoHealth{GitCommonDir: home.mainPath + "/.git"}})
	if nm.gitCommonDir == "" {
		t.Fatal("identity was not established")
	}
	if nm.repoHealthKnown {
		t.Fatal("the health result itself stays unapplied")
	}
}

func TestBlockedKeepsFirstVisitFlags(t *testing.T) {
	m, _, _ := servedWorkspace(t, true)
	m.viewKick, m.memberFirstLive = true, true
	nm, _ := m.Update(struct{}{})
	got := nm.(Model)
	if !got.viewKick || !got.memberFirstLive {
		t.Fatalf("viewKick %v, memberFirstLive %v: a blocked terminal must not run them", got.viewKick, got.memberFirstLive)
	}
	got.webActive = false
	nm, _ = got.Update(struct{}{})
	got = nm.(Model)
	if got.viewKick || got.memberFirstLive {
		t.Fatal("activation must run them")
	}
}

func (f *fakeWebHost) lastWS() *domain.WorkspaceInfo {
	if len(f.workspaces) == 0 {
		return nil
	}
	return f.workspaces[len(f.workspaces)-1]
}
