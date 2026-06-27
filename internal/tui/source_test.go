package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
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
	msg := m.readSourceCmd(context.Background(), srcBranches, true, false)()
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
	msg := m.readSourceCmd(context.Background(), srcStatus, false, false)().(dataAvailableMsg)
	if msg.source != srcStatus {
		t.Fatalf("envelope source = %v, want srcStatus", msg.source)
	}
	if msg.gen != m.srcGen[srcStatus] {
		t.Fatalf("envelope gen = %d, want %d", msg.gen, m.srcGen[srcStatus])
	}
	if msg.manual {
		t.Fatal("envelope manual must be false (passed false)")
	}
	if _, ok := msg.value.(statusPayload); !ok {
		t.Fatalf("status value type = %T, want statusPayload", msg.value)
	}
}

func TestReadSourceWorktreesCarriesPayload(t *testing.T) {
	m := newTestModel(t)
	msg := m.readSourceCmd(context.Background(), srcWorktrees, true, false)().(dataAvailableMsg)
	if msg.source != srcWorktrees {
		t.Fatalf("envelope source = %v, want srcWorktrees", msg.source)
	}
	if msg.gen != m.srcGen[srcWorktrees] {
		t.Fatalf("envelope gen = %d, want %d", msg.gen, m.srcGen[srcWorktrees])
	}
	if !msg.manual {
		t.Fatal("envelope manual must be true (passed true)")
	}
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
	// Baseline: srcLoading for this source must not be pre-set.
	// (m.loading starts true from New() — that is the startup hard-load flag,
	// not a per-source loading indicator; the auto-read arrival clears it via
	// anySourceLoading() recompute once all manual sources drain.)
	if m.srcLoading[srcTags] {
		t.Fatal("test precondition: srcLoading[srcTags] must start false")
	}
	nm, _ := m.Update(dataAvailableMsg{source: srcTags, gen: m.srcGen[srcTags],
		value: []model.Tag{{Name: "v1"}}, manual: false})
	mm := nm.(Model)
	if mm.srcLoading[srcTags] {
		t.Fatal("auto read must never set srcLoading")
	}
	// m.loading is derived as anySourceLoading(); an auto read never sets
	// srcLoading for any source, so after the update it must be false.
	if mm.loading {
		t.Fatal("auto read must not drive m.loading via srcLoading (action-blocking flag)")
	}
}

func TestReloadAllBumpsEveryGenAndBatches(t *testing.T) {
	m := newTestModel(t)
	before := map[sourceKey]int{}
	for s := sourceKey(0); s < srcCount; s++ {
		before[s] = m.srcGen[s]
	}
	m, cmd := m.reloadAllCmd(true, false)
	if cmd == nil {
		t.Fatal("reloadAllCmd must return a batch command")
	}
	for s := sourceKey(0); s < srcCount; s++ {
		if m.srcGen[s] != before[s]+1 {
			t.Errorf("source %d gen not bumped", s)
		}
		if !m.srcInflight[s] {
			t.Errorf("source %d not marked in-flight", s)
		}
	}
}

func TestReloadSourcesManualSetsConsumerSpinners(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.reloadSourcesCmd([]sourceKey{srcStatus}, true, false)
	if !m.srcLoading[srcStatus] {
		t.Fatal("manual reload must set srcLoading for the source")
	}
	m2 := newTestModel(t)
	m2, _ = m2.reloadSourcesCmd([]sourceKey{srcStatus}, false, false)
	if m2.srcLoading[srcStatus] {
		t.Fatal("auto reload must not set srcLoading")
	}
}

// Blocker 1 regression: m.loading is the legacy action-blocking gate; a manual
// reload must set it (so pull/push/etc. guards block during refresh) and the
// arrival of the last source must clear it.
func TestManualReloadDrivesLegacyLoadingFlag(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.reloadSourcesCmd([]sourceKey{srcTags}, true, false)
	if !m.loading {
		t.Fatal("manual reload must set m.loading (action guards depend on it)")
	}
	nm, _ := m.Update(dataAvailableMsg{source: srcTags, gen: m.srcGen[srcTags],
		value: []model.Tag{{Name: "v1"}}, manual: true})
	if nm.(Model).loading {
		t.Fatal("m.loading must clear when the last manual source lands")
	}
}

func TestOpFinishedRefreshesOnlyPendingSources(t *testing.T) {
	m := newTestModel(t)
	m.pendingSources = []sourceKey{srcBranches, srcWorktrees}
	genB, genS := m.srcGen[srcBranches], m.srcGen[srcStatus]
	nm, _ := m.Update(opFinishedMsg{res: engine.Result{Summary: "done"}})
	mm := nm.(Model)
	if mm.srcGen[srcBranches] != genB+1 {
		t.Error("branches should refresh")
	}
	if mm.srcGen[srcStatus] != genS {
		t.Error("status should NOT refresh for a refs-only op")
	}
	if mm.pendingSources != nil {
		t.Error("pendingSources must reset after consumption")
	}
}

func TestOpFinishedDefaultRefreshesAll(t *testing.T) {
	m := newTestModel(t)
	m.pendingSources = nil // unmapped op
	genS := m.srcGen[srcStatus]
	nm, _ := m.Update(opFinishedMsg{res: engine.Result{Summary: "done"}})
	if nm.(Model).srcGen[srcStatus] != genS+1 {
		t.Error("unmapped op must refresh all sources (incl. status)")
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

// TestManualRefreshShowsConsumerSpinner verifies that the Files panel title
// carries the loading glyph when srcStatus is mid manual-refresh.
// (Step 1 — TDD RED before panelLoading is wired into panelLabel.)
func TestManualRefreshShowsConsumerSpinner(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	m, _ = m.reloadSourcesCmd([]sourceKey{srcStatus}, true, false) // Files + Staged consume status
	// The Files panel title should carry the loading glyph while status is loading.
	got := m.panelLabel(panelFiles, "Files")
	if !strings.Contains(got, commitsLoadingGlyph) {
		t.Fatalf("Files title should show the loading glyph during a manual status refresh: %q", got)
	}
}

// TestReloadAfterFirstDataKeepsPanelsVisible verifies that pressing r AFTER
// first data has arrived keeps panels visible (no blank loading screen).
// (Step 5 — TDD RED before the ready flag is implemented.)
func TestReloadAfterFirstDataKeepsPanelsVisible(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	// Simulate first data having arrived (panels populated, ready set).
	m.ready = true
	m.branches = []model.Branch{{Name: "main"}}
	// A manual reload-all (r) marks sources loading but must NOT blank the screen.
	m, _ = m.reloadAllCmd(true, false)
	out := m.View()
	if strings.Contains(out, "(loading…)") {
		t.Fatal("r after first data must keep panels visible, not blank the screen")
	}
}

// TestInitialLoadBlanksUntilReady verifies that before any data has arrived
// (ready=false) the loading screen is shown.
func TestInitialLoadBlanksUntilReady(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	m.ready = false
	m, _ = m.reloadAllCmd(true, false) // startup fan-out, no data yet
	if !strings.Contains(m.View(), "(loading…)") {
		t.Fatal("initial load (no data yet) should show the loading screen")
	}
}

// TestOpAffectedSources verifies the op→source mapping for the hottest ops.
// Each checked op must map to exactly the documented sources; unmapped ops
// must return nil (meaning: refresh all, the safe default).
func TestOpAffectedSources(t *testing.T) {
	cases := []struct {
		op   engine.Operation
		want []sourceKey // nil = expect nil (all)
	}{
		{engine.Commit{}, []sourceKey{srcStatus, srcFeed, srcBranches}},
		{engine.Push{}, []sourceKey{srcBranches, srcRemotes}},
		{engine.Fetch{}, []sourceKey{srcRemotes}},
		{engine.CreateWorktree{}, []sourceKey{srcBranches, srcWorktrees}},
		{engine.RemoveWorktree{}, []sourceKey{srcBranches, srcWorktrees}},
		{engine.SetIdentity{}, []sourceKey{srcIdentity}},
		{engine.SmartMerge{}, []sourceKey{srcStatus, srcFeed, srcBranches}},
		{engine.SmartRebase{}, []sourceKey{srcStatus, srcFeed, srcBranches}},
		{engine.Stash{}, nil}, // unmapped → all (safe default)
	}
	for _, tc := range cases {
		got := opAffectedSources(tc.op)
		if len(got) != len(tc.want) {
			t.Errorf("opAffectedSources(%T) = %v, want %v", tc.op, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("opAffectedSources(%T)[%d] = %v, want %v", tc.op, i, got[i], tc.want[i])
			}
		}
	}
}

// TestSrcStatusRefreshRebuildsCommitGraph guards Fix #1: a srcStatus-only
// refresh (e.g. after a stash pop) must call rebuildCommitGraph() so the
// Commits panel's WIP pseudo-rows stay in sync with the new status.
func TestSrcStatusRefreshRebuildsCommitGraph(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	m.ready = true
	// Populate commits so commitGraphOn is true and the graph has something
	// to rebuild. A single commit with no parents produces one graph row.
	m.commits = []model.Commit{{Hash: "abc1234", Subject: "initial"}}
	m = m.rebuildCommitGraph()
	before := len(m.commitGraphRows)

	// Deliver a srcStatus update that adds a dirty file (introduces a WIP row).
	// FileStatus.Unstaged != '.' makes it show in the Files panel (inFilesPanel).
	dirty := model.WorkingTreeStatus{
		Files: []model.FileStatus{{Path: "foo.go", Unstaged: 'M'}},
	}
	nm, _ := m.Update(dataAvailableMsg{
		source: srcStatus,
		gen:    m.srcGen[srcStatus],
		value:  statusPayload{status: dirty},
		manual: true,
	})
	mm := nm.(Model)

	// commitsTotal() includes WIP pseudo-rows when the tree is dirty;
	// rebuildCommitGraph must have run so commitGraphRows has the same length.
	after := len(mm.commitGraphRows)
	total := mm.commitsTotal()
	if after == before {
		t.Fatalf("srcStatus refresh did not rebuild commit graph: rows stayed at %d (commitsTotal=%d)", after, total)
	}
	if after != total {
		t.Fatalf("commitGraphRows len %d != commitsTotal %d after srcStatus refresh", after, total)
	}
}

// TestSrcStatusRestoresFilesPanelSelection guards the panic-prone
// membership-split path: when files change between staged and unstaged, the
// selection restore must find the key in the right panel (no out-of-bounds).
func TestSrcStatusRestoresFilesPanelSelection(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	m.ready = true

	// Initial state: one file is modified (unstaged) and one is staged.
	// Unstaged != '.' → Files panel; Staged != '.' → Staged panel.
	m.status = model.WorkingTreeStatus{
		Files: []model.FileStatus{
			{Path: "a.go", Unstaged: 'M'},
			{Path: "b.go", Staged: 'A'},
		},
	}
	// Put cursor on the first file in the Files panel.
	m.sel[panelFiles] = 0
	m.sel[panelStaged] = 0

	// New status: swap — a.go is now staged, b.go is now modified.
	newStatus := model.WorkingTreeStatus{
		Files: []model.FileStatus{
			{Path: "b.go", Unstaged: 'M'},
			{Path: "a.go", Staged: 'M'},
		},
	}
	nm, _ := m.Update(dataAvailableMsg{
		source: srcStatus,
		gen:    m.srcGen[srcStatus],
		value:  statusPayload{status: newStatus},
		manual: true,
	})
	mm := nm.(Model)
	// No panic = selection restore handled the membership-split correctly.
	// Also assert selections are within bounds.
	if n := mm.panelLen(panelFiles); mm.sel[panelFiles] >= n && n > 0 {
		t.Fatalf("panelFiles sel %d out of bounds (len %d)", mm.sel[panelFiles], n)
	}
	if n := mm.panelLen(panelStaged); mm.sel[panelStaged] >= n && n > 0 {
		t.Fatalf("panelStaged sel %d out of bounds (len %d)", mm.sel[panelStaged], n)
	}
}
