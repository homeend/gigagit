package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// tourFixture: a fresh session manager, a Model on its own store whose
// current worktree is NOT the worker's, and a spawned worker (with a brief)
// in a directory of its own. Not parallel: the manager is process-global.
func tourFixture(t *testing.T) (Model, domain.SessionID, string) {
	t.Helper()
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { domain.Sessions().KillAll(context.Background()); restore() })
	t.Cleanup(domain.UseSessionStates(domain.NewStaticStates(nil)))
	repo, _ := newRepoDir(t)
	wt := t.TempDir()
	tc := config.ToolCommand{Category: "session", Name: "Sh", Mode: "session", Command: "sh -c 'sleep 600'"}
	s, _, err := domain.Open(repo).StartAgentSession(context.Background(), tc, wt, "", 80, 24, nil, "",
		domain.SpawnRecord{Parent: "x/ov", Brief: "# Task\nstart at [a](a.go:1)", Worktree: wt, Spawned: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	m := New(domain.Open(repo))
	m.docs = agentdocs.New()
	m.currentWorktree = repo
	return m, s.Info().ID, wt
}

func spawned(id domain.SessionID, wt string) agentSpawnedMsg {
	return agentSpawnedMsg{res: domain.AgentStartResult{ID: domain.FullSessionID(id), Worktree: wt, Tool: "Sh"},
		id: id, reply: make(chan agentSpawnReply, 1)}
}

func TestTourCapIsTheOverviewCap(t *testing.T) {
	if domain.TourMaxBytes != agentdocs.MaxOverviewBytes {
		t.Fatalf("domain.TourMaxBytes %d != agentdocs.MaxOverviewBytes %d", domain.TourMaxBytes, agentdocs.MaxOverviewBytes)
	}
}

func TestBriefTourFiledOnSpawn(t *testing.T) {
	m, id, wt := tourFixture(t)
	m, _ = m.onAgentSpawned(spawned(id, wt))
	ovs := m.docs.Overviews(domain.CheckoutKey(wt))
	if len(ovs) != 1 || !strings.HasPrefix(ovs[0].Title, "Brief — Sh · ") || !strings.Contains(ovs[0].Text, "a.go:1") {
		t.Fatalf("brief tours = %+v", ovs)
	}
	if len(m.docs.Overviews(domain.CheckoutKey(m.currentWorktree))) != 0 {
		t.Fatal("the brief belongs to the worker's worktree, not the current one")
	}
	if !strings.Contains(m.statusMsg, "started in") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestBriefNotFiledAtTheCap(t *testing.T) {
	m, id, wt := tourFixture(t)
	for i := 0; i < agentdocs.MaxOverviewsPerRoot; i++ {
		if _, err := m.docs.AddOverview(domain.CheckoutKey(wt), wt, "o", "x"); err != nil {
			t.Fatal(err)
		}
	}
	msg := spawned(id, wt)
	m, _ = m.onAgentSpawned(msg)
	if r := <-msg.reply; r.err != nil {
		t.Fatalf("the spawn answer must stay OK: %v", r.err)
	}
	if !strings.Contains(m.statusMsg, "brief not filed: 20 overviews are open") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestReportTourFiledUnderTheWorkersWorktree(t *testing.T) {
	m, id, wt := tourFixture(t)
	if _, err := domain.AgentReportVerb(domain.FullSessionID(id), "done: [a](a.go:1)", false); err != nil {
		t.Fatal(err)
	}
	m = m.fileReportTours()
	ovs := m.docs.Overviews(domain.CheckoutKey(wt))
	if len(ovs) != 1 || !strings.HasPrefix(ovs[0].Title, "Report — Sh · ") {
		t.Fatalf("report tours = %+v", ovs)
	}
	if len(m.docs.Overviews(domain.CheckoutKey(m.currentWorktree))) != 0 {
		t.Fatal("filed under the current worktree")
	}
}

func TestSecondReportReplacesTheTour(t *testing.T) {
	m, id, wt := tourFixture(t)
	full := domain.FullSessionID(id)
	domain.AgentReportVerb(full, "half way", false)
	m = m.fileReportTours()
	domain.AgentReportVerb(full, "all done", true)
	m = m.fileReportTours()
	ovs := m.docs.Overviews(domain.CheckoutKey(wt))
	if len(ovs) != 1 || ovs[0].Text != "all done" || !strings.HasPrefix(ovs[0].Title, "Final report — ") {
		t.Fatalf("report tours = %+v", ovs)
	}
}

func TestClosedReportTourComesBackOnlyWithANewReport(t *testing.T) {
	m, id, wt := tourFixture(t)
	full := domain.FullSessionID(id)
	domain.AgentReportVerb(full, "one", false)
	m = m.fileReportTours()
	root := domain.CheckoutKey(wt)
	m.docs.RemoveOverview(m.docs.Overviews(root)[0].ID) // the user closed it
	m = m.fileReportTours()
	if n := len(m.docs.Overviews(root)); n != 0 {
		t.Fatalf("a closed tour came back without a new report (%d)", n)
	}
	domain.AgentReportVerb(full, "two", false)
	m = m.fileReportTours()
	if ovs := m.docs.Overviews(root); len(ovs) != 1 || ovs[0].Text != "two" {
		t.Fatalf("a new report files it again: %+v", ovs)
	}
}
