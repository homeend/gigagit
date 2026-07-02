package tui

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

// logLine builds one git-log feed line in the \x1f-separated logFormat.
func logLine(hash, parents, subject string) string {
	return hash + "\x1f" + parents + "\x1f" + "Ada" + "\x1f" + "0" + "\x1f" + subject + "\x1f" + "\x1f" + "\n"
}

// forkFeed is an all-branches walk: tip aaa plus a side branch bbb→ccc that
// forks around it and rejoins at ddd. Lays with a second lane.
func forkFeed() string {
	return logLine("aaa", "ddd", "tip") +
		logLine("bbb", "ccc", "side-2") +
		logLine("ccc", "ddd", "side-1") +
		logLine("ddd", "eee", "base") +
		logLine("eee", "", "root")
}

// linearFeed is the solo walk of the checked-out branch: the SAME newest
// commit (aaa) and the SAME row count, but a purely linear ancestry.
func linearFeed() string {
	return logLine("aaa", "ddd", "tip") +
		logLine("ddd", "eee", "base") +
		logLine("eee", "fff", "older-1") +
		logLine("fff", "ggg", "older-2") +
		logLine("ggg", "", "root")
}

func requireSingleLane(t *testing.T, m Model) {
	t.Helper()
	if len(m.commits) != 5 || m.commits[1].Hash != "ddd" {
		t.Fatalf("replaced rows not applied: %+v", m.commits)
	}
	for i, lane := range m.commitGraphLanes {
		if lane != 0 {
			t.Fatalf("a linear feed must re-lay to a single lane; row %d is on lane %d (lanes %v)",
				i, lane, m.commitGraphLanes)
		}
	}
}

// TestSoloReloadRelaysGraphWithSameTip reproduces the stale solo graph: solo
// the checked-out branch whose own tip is already the newest commit in the
// all-branches feed. The scope reload REPLACES the row set with a same-tip,
// same-length list, which the incremental append fast path cannot tell from a
// no-op — it kept painting the previous walk's fork lanes beside the new
// linear rows.
func TestSoloReloadRelaysGraphWithSameTip(t *testing.T) {
	f := gitexec.NewFakeRunner()
	svc := domain.New(&git.Repo{Runner: f})
	m := branchesPanelModel("main", "feat")
	m.svc = svc
	m.feed = svc.CommitFeed()
	m.sel = map[panel]int{}

	f.SetResponse("git log", gitexec.Result{Stdout: forkFeed()})
	m.feed.LoadInitial(context.Background())
	m.commits = m.feed.Snapshot().Commits
	m = m.rebuildCommitGraph()
	if m.commitGraphLanes[1] == 0 {
		t.Fatalf("precondition: the all-branches graph should fork onto a second lane, lanes = %v",
			m.commitGraphLanes)
	}

	f.SetResponse("git log", gitexec.Result{Stdout: linearFeed()})
	m.commitScopeBranches = []string{"main"}
	mm, _ := m.Update(m.reloadFeedCmd()())
	requireSingleLane(t, mm.(Model))
}

// TestFeedSourceArrivalRelaysGraphWithSameTip covers the same replacement
// hazard on the per-source refresh path: a srcFeed re-read (r / background
// refresh) replaces m.commits wholesale; a same-tip, same-length result (e.g.
// upstream refs moved mid-list after a fetch) must not inherit the previous
// walk's lanes.
func TestFeedSourceArrivalRelaysGraphWithSameTip(t *testing.T) {
	m := footerModel()
	m.srcInflight = map[sourceKey]bool{}
	m.srcLoading = map[sourceKey]bool{}

	fork, err := git.ParseLog([]byte(forkFeed()))
	if err != nil {
		t.Fatal(err)
	}
	m.commits = fork
	m = m.rebuildCommitGraph()
	if m.commitGraphLanes[1] == 0 {
		t.Fatalf("precondition: the all-branches graph should fork onto a second lane, lanes = %v",
			m.commitGraphLanes)
	}

	linear, err := git.ParseLog([]byte(linearFeed()))
	if err != nil {
		t.Fatal(err)
	}
	mm, _ := m.Update(dataAvailableMsg{
		source:  srcFeed,
		gen:     m.srcGen[srcFeed],
		value:   feedPayload{commits: linear, exhausted: true},
		startup: true, // skip duration recording (no refreshDur ring in this test)
	})
	requireSingleLane(t, mm.(Model))
}
