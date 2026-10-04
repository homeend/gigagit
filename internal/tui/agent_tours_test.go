package tui

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
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
	if domain.TourMaxTitle != agentdocs.MaxOverviewTitle {
		t.Fatalf("domain.TourMaxTitle %d != agentdocs.MaxOverviewTitle %d", domain.TourMaxTitle, agentdocs.MaxOverviewTitle)
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
	m, _ = m.fileReportTours()
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
	m, _ = m.fileReportTours()
	domain.AgentReportVerb(full, "all done", true)
	m, _ = m.fileReportTours()
	ovs := m.docs.Overviews(domain.CheckoutKey(wt))
	if len(ovs) != 1 || ovs[0].Text != "all done" || !strings.HasPrefix(ovs[0].Title, "Final report — ") {
		t.Fatalf("report tours = %+v", ovs)
	}
}

func TestClosedReportTourComesBackOnlyWithANewReport(t *testing.T) {
	m, id, wt := tourFixture(t)
	full := domain.FullSessionID(id)
	domain.AgentReportVerb(full, "one", false)
	m, _ = m.fileReportTours()
	root := domain.CheckoutKey(wt)
	m.docs.RemoveOverview(m.docs.Overviews(root)[0].ID) // the user closed it
	m, _ = m.fileReportTours()
	if n := len(m.docs.Overviews(root)); n != 0 {
		t.Fatalf("a closed tour came back without a new report (%d)", n)
	}
	domain.AgentReportVerb(full, "two", false)
	m, _ = m.fileReportTours()
	if ovs := m.docs.Overviews(root); len(ovs) != 1 || ovs[0].Text != "two" {
		t.Fatalf("a new report files it again: %+v", ovs)
	}
}

func topTour(m Model) string {
	if fv := layerOf[*fileViewer](m); fv != nil && fv.openFile != nil {
		return fv.id()
	}
	return ""
}

func TestOpenTourOnTheCurrentWorktree(t *testing.T) {
	m, id, wt := tourFixture(t)
	m.currentWorktree = wt
	m, _ = m.openTour(id, "brief")
	ovID, ok := m.docs.TourID("brief:" + domain.FullSessionID(id))
	if !ok || topTour(m) != ovID {
		t.Fatalf("the brief must show at once: top %q, tour %q %v", topTour(m), ovID, ok)
	}
	if m.consoleSwitch.armed || m.consoleSwitch.tour != "" {
		t.Fatalf("no switch on the current worktree: %+v", m.consoleSwitch)
	}
}

func TestOpenTourSwitchesWorktree(t *testing.T) {
	m, id, wt := tourFixture(t)
	m, _ = m.openTour(id, "brief")
	ovID, _ := m.docs.TourID("brief:" + domain.FullSessionID(id))
	if !m.consoleSwitch.armed || m.consoleSwitch.tour != ovID || topTour(m) != "" {
		t.Fatalf("a tour in another worktree waits for the switch: %+v, top %q", m.consoleSwitch, topTour(m))
	}
	// The switch lands (model.go: the snapshot syncs the docs, then settles).
	m.currentWorktree = wt
	m = m.syncOverviews()
	m, _ = m.settleConsoleAfterSwitch()
	if topTour(m) != ovID || m.consoleSwitch.tour != "" {
		t.Fatalf("after the switch: top %q want %q, %+v", topTour(m), ovID, m.consoleSwitch)
	}
}

func TestOpenTourRefilesAClosedTour(t *testing.T) {
	m, id, wt := tourFixture(t)
	m.currentWorktree = wt
	m, _ = m.onAgentSpawned(spawned(id, wt))
	first, _ := m.docs.TourID("brief:" + domain.FullSessionID(id))
	m.docs.RemoveOverview(first)
	m, _ = m.openTour(id, "brief")
	again, ok := m.docs.TourID("brief:" + domain.FullSessionID(id))
	if !ok || again == first || topTour(m) != again {
		t.Fatalf("closed → re-filed and shown: first %q again %q top %q", first, again, topTour(m))
	}
}

func TestOpenTourWithoutOneSaysSo(t *testing.T) {
	m, id, _ := tourFixture(t)
	m, _ = m.openTour(id, "report")
	if !strings.Contains(m.statusMsg, "no tour to open:") || !strings.Contains(m.statusMsg, "has not reported") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestTourMenuRows(t *testing.T) {
	m, id, _ := tourFixture(t)
	ids := func() string {
		var out []string
		for _, r := range m.tourMenuRows(id) {
			out = append(out, r.id)
		}
		return strings.Join(out, ",")
	}
	if got := ids(); got != "session-brief" {
		t.Fatalf("before a report: %q", got)
	}
	domain.AgentReportVerb(domain.FullSessionID(id), "r", false)
	if got := ids(); got != "session-brief,session-report" {
		t.Fatalf("after a report: %q", got)
	}
}

// A report refused at the cap is filed once there is room — without waiting
// for a new report — and its refusal is said once, not on every wake.
func TestReportRefusedAtTheCapIsFiledOnceThereIsRoom(t *testing.T) {
	m, id, wt := tourFixture(t)
	root := domain.CheckoutKey(wt)
	var first agentdocs.Overview
	for i := 0; i < agentdocs.MaxOverviewsPerRoot; i++ {
		o, err := m.docs.AddOverview(root, wt, "o", "x")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = o
		}
	}
	domain.AgentReportVerb(domain.FullSessionID(id), "done", false)
	m, _ = m.fileReportTours()
	if !strings.Contains(m.statusMsg, "report not filed") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	m.statusMsg = ""
	m, _ = m.fileReportTours()
	if m.statusMsg != "" {
		t.Fatalf("the refusal was said again: %q", m.statusMsg)
	}
	m.docs.RemoveOverview(first.ID) // the user closed one: room for the report
	m, _ = m.fileReportTours()
	if _, ok := m.docs.TourID("report:" + domain.FullSessionID(id)); !ok {
		t.Fatal("the refused report never came back")
	}
}

// runTourCmds runs a command and every command a batch holds, dropping their
// messages (a check's answer is the store's own signal).
func runTourCmds(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if b, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range b {
			runTourCmds(c)
		}
	}
}

func anchorMissing(t *testing.T, m Model, key string) bool {
	t.Helper()
	id, ok := m.docs.TourID(key)
	if !ok {
		t.Fatalf("no tour %s", key)
	}
	o, _ := m.docs.Overview(id)
	return len(o.Anchors) > 0 && o.Anchors[0].Missing
}

// A tour the TUI files has its anchors checked (spec §3.4): one whose file
// is not in the worktree is struck through — on spawn, on a report wake and
// when the user opens it.
func TestTUIFiledToursCheckTheirAnchors(t *testing.T) {
	m, id, wt := tourFixture(t) // the brief points at a.go, which wt lacks
	full := domain.FullSessionID(id)
	m, cmd := m.onAgentSpawned(spawned(id, wt))
	runTourCmds(cmd)
	if !anchorMissing(t, m, "brief:"+full) {
		t.Fatal("spawn: a missing anchor of the brief is not struck through")
	}
	domain.AgentReportVerb(full, "see [b](b.go:2)", false)
	m.actWatch = nil // no subscription: its wait would block runTourCmds
	m, cmd = m.onSessionActivity()
	runTourCmds(cmd)
	if !anchorMissing(t, m, "report:"+full) {
		t.Fatal("wake: a missing anchor of the report is not struck through")
	}
	m.docs.RemoveOverview(func() string { i, _ := m.docs.TourID("brief:" + full); return i }())
	m, cmd = m.openTour(id, "brief")
	runTourCmds(cmd)
	if !anchorMissing(t, m, "brief:"+full) {
		t.Fatal("open: a refiled brief's missing anchor is not struck through")
	}
}
