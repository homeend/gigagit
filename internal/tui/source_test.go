package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// newTestModel returns a freshly constructed Model via New(), the canonical
// constructor. There is no pre-existing helper with this name in the suite;
// this is the thin wrapper the registry tests need to prove New() initializes
// the per-source state maps.
func newTestModel(t *testing.T) Model {
	t.Helper()
	return New(domain.New(newRepo(t)))
}

func TestSrcConsumersCoverAllSources(t *testing.T) {
	for s := sourceKey(0); s < srcCount; s++ {
		if s == srcIdentity {
			continue // identity feeds the Settings popup, not a left panel
		}
		if len(srcConsumers[s]) == 0 {
			t.Errorf("source %d has no consumer panels", s)
		}
	}
}

func TestRegistryMapsInitialized(t *testing.T) {
	m := newTestModel(t) // existing helper used across *_test.go
	if m.srcGen == nil || m.srcInflight == nil || m.srcLoading == nil {
		t.Fatal("registry maps must be initialized by the constructor")
	}
}

func TestReadSourceBranchesProducesMsg(t *testing.T) {
	m := newTestModel(t) // wired to a real temp repo (newRepo(t))
	msg := m.readSourceCmd(srcBranches, true)()
	dm, ok := msg.(dataAvailableMsg)
	if !ok {
		t.Fatalf("want dataAvailableMsg, got %T", msg)
	}
	if dm.source != srcBranches || dm.gen != m.srcGen[srcBranches] || !dm.manual {
		t.Fatalf("bad envelope: %+v", dm)
	}
	if _, ok := dm.value.([]model.Branch); !ok {
		t.Fatalf("branches value type = %T, want []model.Branch", dm.value)
	}
}

func TestReadSourceStatusCarriesConflict(t *testing.T) {
	m := newTestModel(t)
	msg := m.readSourceCmd(srcStatus, false)().(dataAvailableMsg)
	if _, ok := msg.value.(statusPayload); !ok {
		t.Fatalf("status value type = %T, want statusPayload", msg.value)
	}
}

func TestReadSourceWorktreesCarriesPayload(t *testing.T) {
	m := newTestModel(t)
	msg := m.readSourceCmd(srcWorktrees, true)().(dataAvailableMsg)
	if msg.err != nil {
		t.Fatalf("unexpected error: %v", msg.err)
	}
	p, ok := msg.value.(worktreesPayload)
	if !ok {
		t.Fatalf("worktrees value type = %T, want worktreesPayload", msg.value)
	}
	if len(p.worktrees) == 0 {
		t.Fatal("expected at least one worktree")
	}
	// headTimes is best-effort; a fresh repo may have no commits, so we only
	// assert the map is non-nil (the Snapshot arm does the same).
	if p.headTimes == nil {
		t.Fatal("headTimes must be non-nil (may be empty)")
	}
}

func TestDataAvailableStaleGenDropped(t *testing.T) {
	m := newTestModel(t)
	m.srcGen[srcBranches] = 5
	old := []model.Branch{{Name: "old"}}
	m.branches = old
	nm, _ := m.Update(dataAvailableMsg{source: srcBranches, gen: 4,
		value: []model.Branch{{Name: "new"}}, manual: true})
	if got := nm.(Model).branches; len(got) != 1 || got[0].Name != "old" {
		t.Fatalf("stale gen must be ignored, branches=%+v", got)
	}
}

func TestDataAvailableManualClearsSpinner(t *testing.T) {
	m := newTestModel(t)
	m.srcLoading[srcBranches] = true
	m.srcInflight[srcBranches] = true
	nm, _ := m.Update(dataAvailableMsg{source: srcBranches, gen: m.srcGen[srcBranches],
		value: []model.Branch{{Name: "x"}}, manual: true})
	mm := nm.(Model)
	if mm.srcLoading[srcBranches] || mm.srcInflight[srcBranches] {
		t.Fatal("manual read must clear srcLoading and srcInflight on arrival")
	}
	if len(mm.branches) != 1 || mm.branches[0].Name != "x" {
		t.Fatalf("value not stored: %+v", mm.branches)
	}
	// m.loading is the legacy action-blocking flag, derived as anySourceLoading().
	// With the last (and only) manual source cleared, it must be false.
	if mm.loading {
		t.Fatal("m.loading must clear when the last manual source lands")
	}
}

func TestDataAvailableAutoLeavesNoSpinner(t *testing.T) {
	m := newTestModel(t)
	nm, _ := m.Update(dataAvailableMsg{source: srcTags, gen: m.srcGen[srcTags],
		value: []model.Tag{{Name: "v1"}}, manual: false})
	if nm.(Model).srcLoading[srcTags] {
		t.Fatal("auto read must never set a spinner")
	}
}

// TestBranchesDefersFeedRewalkWhileFeedInflight is a regression test for
// Blocker 2: the initial LoadInitial (srcFeed) and the upstream-scoped
// re-walk (triggered by srcBranches arriving) must NOT run concurrently.
// maybeFeedUpstreamRewalk returns false while srcFeed is in-flight, deferring
// the re-walk to the srcFeed arrival handler instead.
func TestBranchesDefersFeedRewalkWhileFeedInflight(t *testing.T) {
	m := newTestModel(t)
	// Wire a tracked upstream so feedUpstreams() returns non-empty.
	m.branches = []model.Branch{{Name: "main", Upstream: "origin/main"}}
	m.remoteBranches = []model.RemoteBranch{{Name: "origin/main"}}
	// Mark the scope as stale so maybeFeedUpstreamRewalk would otherwise fire.
	m.feedScopeApplied = "stale"

	m.srcInflight[srcFeed] = true // a srcFeed read is outstanding
	if m.maybeFeedUpstreamRewalk() {
		t.Fatal("re-walk must be deferred while srcFeed is in flight")
	}
	m.srcInflight[srcFeed] = false // feed read has now landed
	if !m.maybeFeedUpstreamRewalk() {
		t.Fatal("re-walk must fire once the feed read has landed")
	}
}
