package tui

import (
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/workspace"
)

// A workspace fetches every member, not only the one on screen, so a
// sleeping member's ahead/behind counts follow its remote (spec §6: the
// background lane runs the sleeping members' repository sources). The
// fetches ride the one background lane: one git fetch at a time.

// fetchOnly turns every live background item off and gives each sleeping
// member a 60 s fetch, so a tick can only launch member fetches.
func fetchOnly(m Model) Model {
	m.cfg.Refresh = config.RefreshConfig{Enabled: true}
	m.loading = false
	for _, r := range m.ws.members {
		if r != m.liveRepo() {
			r.cfg.Refresh = config.RefreshConfig{Enabled: true, Fetch: 60}
		}
	}
	return m
}

// openThree is openTwo with a third member.
func openThree(t *testing.T, m Model) (Model, *repoView, *repoView) {
	t.Helper()
	home := m.homeRepo()
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "shop", Repos: []string{home.mainPath, secondRepo(t), secondRepo(t)}})
	m = settleWorkspace(t, nm, cmd)
	b, c := m.ws.members[1], m.ws.members[2]
	if !b.loaded || !c.loaded {
		t.Fatal("a member did not load")
	}
	return m, b, c
}

// A sleeping member's first fetch is one interval after it joins the
// lane (no burst on open), then it fetches on its own cadence.
func TestSleepingMemberFetchedOnItsCadence(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	m = fetchOnly(m)
	t0 := time.Unix(3_000_000, 0)
	m, _ = m.refreshTick(t0)
	if m.bgBusy {
		t.Fatalf("a member fetch fired on the first tick (burst on open): %+v", m.bgActiveItem)
	}
	m, cmd := m.refreshTick(t0.Add(61 * time.Second))
	if !m.bgBusy || m.bgActiveItem != memberFetchItem(b.key) || cmd == nil {
		t.Fatalf("B was not fetched after its interval: busy=%v item=%+v cmd=%v", m.bgBusy, m.bgActiveItem, cmd != nil)
	}
}

// Two sleeping members due at once are fetched one after the other: the
// second waits in the queue until the first one's fetch is done.
func TestMemberFetchesAreSequential(t *testing.T) {
	m := loadedModel(t)
	m, b, c := openThree(t, m)
	m = fetchOnly(m)
	t0 := time.Unix(3_000_000, 0)
	m, _ = m.refreshTick(t0)
	m, _ = m.refreshTick(t0.Add(61 * time.Second))
	if !m.bgBusy || m.bgActiveItem != memberFetchItem(b.key) {
		t.Fatalf("first fetch: busy=%v item=%+v", m.bgBusy, m.bgActiveItem)
	}
	if len(m.bgQueue) != 1 || m.bgQueue[0] != memberFetchItem(c.key) {
		t.Fatalf("C must wait in the queue, queue=%+v", m.bgQueue)
	}
	m, cmd := m.refreshTick(t0.Add(62 * time.Second))
	if cmd != nil || m.bgActiveItem != memberFetchItem(b.key) {
		t.Fatalf("a second fetch started while B's runs: item=%+v", m.bgActiveItem)
	}
	nm, _ := m.Update(bgFetchDoneMsg{gen: m.bgFetchGen, svc: b.svc, dur: time.Second})
	m = nm.(Model)
	if m.bgBusy {
		t.Fatal("B's finished fetch did not free the lane")
	}
	m, cmd = m.refreshTick(t0.Add(63 * time.Second))
	if !m.bgBusy || m.bgActiveItem != memberFetchItem(c.key) || cmd == nil {
		t.Fatalf("C was not fetched after B: busy=%v item=%+v", m.bgBusy, m.bgActiveItem)
	}
}

// The live member is fetched by the ordinary fetch item; a member item
// that comes due for it (it went live while queued) does not commit the
// lane.
func TestLiveMemberItemDropped(t *testing.T) {
	m := loadedModel(t)
	m, _ = openTwo(t, m)
	m = fetchOnly(m)
	m.bgQueue = []refreshItem{memberFetchItem(m.liveRepo().key)}
	m, cmd := m.refreshTick(time.Unix(3_000_000, 0))
	if m.bgBusy || cmd != nil {
		t.Fatalf("a member fetch ran for the live member: item=%+v", m.bgActiveItem)
	}
}

// A sleeping member without a watcher (polling) is re-read after its
// fetch, so its counts move at once; a fetch for a member that is gone
// frees the lane and reads nothing.
func TestMemberFetchDone(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	b.closeWatch()
	b.polling = true
	m.bgBusy, m.bgActiveItem, m.bgFetchGen = true, memberFetchItem(b.key), 5
	nm, cmd := m.Update(bgFetchDoneMsg{gen: 5, svc: b.svc, dur: time.Second})
	m = nm.(Model)
	if m.bgBusy {
		t.Fatal("the lane stayed busy")
	}
	var loaded bool
	for _, msg := range drainTimed(cmd) {
		if ml, ok := msg.(memberLoadedMsg); ok && ml.svc == b.svc {
			loaded = true
		}
	}
	if !loaded {
		t.Fatal("a polling member was not re-read after its fetch")
	}

	m.bgBusy, m.bgActiveItem, m.bgFetchGen = true, memberFetchItem("gone"), 6
	gone := domain.OpenTUIMemberRooted(b.mainPath, m.homeSvc())
	nm, cmd = m.Update(bgFetchDoneMsg{gen: 6, svc: gone, dur: time.Second})
	m = nm.(Model)
	if m.bgBusy || cmd != nil {
		t.Fatalf("a fetch for a gone member: busy=%v cmd=%v", m.bgBusy, cmd != nil)
	}
}

// The lane's hint names the repository being fetched.
func TestMemberFetchHintNamesRepo(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	m.bgBusy, m.bgActiveItem = true, memberFetchItem(b.key)
	if got, want := m.bgRefreshHint(), "⟳ fetch "+b.name+"…"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
}
