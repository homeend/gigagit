package tui

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

func TestMeanDuration(t *testing.T) {
	if got := meanDuration(nil); got != 0 {
		t.Fatalf("empty → 0, got %v", got)
	}
	got := meanDuration([]time.Duration{2 * time.Second, 4 * time.Second})
	if got != 3*time.Second {
		t.Fatalf("mean(2s,4s) = 3s, got %v", got)
	}
}

func TestScheduledInterval(t *testing.T) {
	st := refreshItem{source: srcStatus}
	// configured 0 → off.
	if secs, on := scheduledInterval(config.RefreshConfig{Enabled: true}, st); on || secs != 0 {
		t.Fatalf("interval 0 → off, got %d/%v", secs, on)
	}
	// configured below the floor → clamped to min (default 10).
	if secs, on := scheduledInterval(config.RefreshConfig{Enabled: true, Status: 3}, st); !on || secs != 10 {
		t.Fatalf("3 → floored to 10, got %d/%v", secs, on)
	}
	// configured at/above the floor → passthrough.
	if secs, on := scheduledInterval(config.RefreshConfig{Enabled: true, Status: 30}, st); !on || secs != 30 {
		t.Fatalf("30 → 30, got %d/%v", secs, on)
	}
	// custom min_seconds honored.
	if secs, _ := scheduledInterval(config.RefreshConfig{Enabled: true, Status: 3, MinSeconds: 20}, st); secs != 20 {
		t.Fatalf("min 20 → 20, got %d", secs)
	}
}

func TestRefreshTomlKeyAndSetField(t *testing.T) {
	// feed's display name is "commits" but its toml key is "feed".
	if k := refreshTomlKey(refreshItem{source: srcFeed}); k != "feed" {
		t.Fatalf("feed key = feed, got %q", k)
	}
	if k := refreshTomlKey(fetchItem); k != "fetch" {
		t.Fatalf("fetch key = fetch, got %q", k)
	}
	var c config.RefreshConfig
	setRefreshIntervalField(&c, refreshItem{source: srcRemotes}, 45)
	setRefreshIntervalField(&c, fetchItem, 90)
	if c.Remotes != 45 || c.Fetch != 90 {
		t.Fatalf("set fields: got remotes=%d fetch=%d", c.Remotes, c.Fetch)
	}
}

func TestRecordDurationCapsAtTen(t *testing.T) {
	m := Model{refreshDur: map[refreshItem][]time.Duration{}}
	it := refreshItem{source: srcStatus}
	for i := 1; i <= 13; i++ {
		m = m.recordDuration(it, time.Duration(i)*time.Second)
	}
	got := m.refreshDur[it]
	if len(got) != maxDurationSamples {
		t.Fatalf("ring should cap at %d, got %d", maxDurationSamples, len(got))
	}
	// Oldest dropped: the ring holds samples 4s..13s, mean = 8.5s.
	if mean := meanDuration(got); mean != 8500*time.Millisecond {
		t.Fatalf("want mean 8.5s of last 10, got %v", mean)
	}
}

func TestDataAvailableRecordsDuration(t *testing.T) {
	m := newTestModel(t)
	m.bgActiveItem = refreshItem{} // lane idle
	it := refreshItem{source: srcTags}
	gen := m.srcGen[srcTags]
	msg := dataAvailableMsg{source: srcTags, gen: gen, value: []model.Tag(nil), dur: 2 * time.Second}
	nm, _ := m.Update(msg)
	got := nm.(Model).refreshDur[it]
	if len(got) != 1 || got[0] != 2*time.Second {
		t.Fatalf("a source read should record dur, got %v", got)
	}
}

// A manual r read (startup=false) DOES feed the ring — its measured duration
// is shown in the Refresh rates editor stats alongside background reads.
func TestManualReadRecordsDuration(t *testing.T) {
	m := newTestModel(t)
	m.bgActiveItem = refreshItem{} // lane idle
	it := refreshItem{source: srcTags}
	gen := m.srcGen[srcTags]
	msg := dataAvailableMsg{source: srcTags, gen: gen, value: []model.Tag(nil), dur: 5 * time.Second, manual: true}
	nm, _ := m.Update(msg)
	if got := nm.(Model).refreshDur[it]; len(got) != 1 || got[0] != 5*time.Second {
		t.Fatalf("manual read should feed the duration ring, got %v", got)
	}
}

// The app-start fan-out (startup=true) must NOT feed the ring — its parallel,
// contended durations are unrepresentative.
func TestStartupReadDoesNotRecordDuration(t *testing.T) {
	m := newTestModel(t)
	m.bgActiveItem = refreshItem{} // lane idle
	it := refreshItem{source: srcTags}
	gen := m.srcGen[srcTags]
	msg := dataAvailableMsg{source: srcTags, gen: gen, value: []model.Tag(nil), dur: 5 * time.Second, manual: true, startup: true}
	nm, _ := m.Update(msg)
	if got := nm.(Model).refreshDur[it]; len(got) != 0 {
		t.Fatalf("startup read must not feed the duration ring, got %v", got)
	}
}

// A foreground fetch op records its duration into the fetch row (so the user
// sees how long fetch takes), without enabling the background fetch task.
func TestForegroundFetchRecordsDuration(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	m.opIsFetch = true
	m.opStart = time.Now().Add(-2 * time.Second)
	nm, _ := m.Update(opFinishedMsg{res: engine.Result{}, err: nil})
	mm := nm.(Model)
	got := mm.refreshDur[fetchItem]
	if len(got) != 1 || got[0] < 2*time.Second || got[0] > 5*time.Second {
		t.Fatalf("foreground fetch should record ~2s into the fetch row, got %v", got)
	}
	if mm.opIsFetch {
		t.Fatal("opIsFetch must be cleared after the op completes")
	}
}

// End-to-end: a real successful foreground fetch (the `f` key path) driven
// through startOp → the op message loop → opFinishedMsg must populate the fetch
// row. Uses a real local bare remote so the fetch actually succeeds.
func TestForegroundFetchRecordsThroughOpLoop(t *testing.T) {
	m := newTestModelWithRemote(t)
	model, c := m.startOp(engine.Fetch{})
	if !model.opIsFetch {
		t.Fatal("startOp(engine.Fetch{}) must set opIsFetch")
	}
	done := false
	for i := 0; i < 200 && c != nil; i++ {
		msg := c()
		var nm tea.Model
		nm, c = model.Update(msg)
		model = nm.(Model)
		if _, ok := msg.(opFinishedMsg); ok {
			done = true
			break
		}
	}
	if !done {
		t.Fatal("op loop never reached opFinishedMsg")
	}
	if got := model.refreshDur[fetchItem]; len(got) != 1 {
		t.Fatalf("a successful foreground fetch should record into the fetch row, got %v", got)
	}
}

func TestEnqueueDueDedup(t *testing.T) {
	a := refreshItem{source: srcStatus}
	b := refreshItem{source: srcBranches}

	// Empty queue, not busy → both appended in FIFO order.
	q := enqueueDue(nil, refreshItem{}, false, []refreshItem{a, b})
	if len(q) != 2 || q[0] != a || q[1] != b {
		t.Fatalf("want [a b], got %v", q)
	}
	// Re-enqueue same types → no duplicates.
	q = enqueueDue(q, refreshItem{}, false, []refreshItem{a, b})
	if len(q) != 2 {
		t.Fatalf("dedup failed, got %v", q)
	}
	// Active (busy) type is not re-enqueued even though absent from the queue.
	q2 := enqueueDue(nil, a, true, []refreshItem{a, b})
	if len(q2) != 1 || q2[0] != b {
		t.Fatalf("active type must be skipped, got %v", q2)
	}
}

func TestRefreshTickSingleLane(t *testing.T) {
	m := newTestModel(t)
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, Status: 1, Branches: 1}
	m.loading = false
	t0 := time.Unix(4_000_000, 0)
	// First tick: both due, but only ONE fires (lane single-file); the other queues.
	m2, cmd := m.refreshTick(t0)
	if cmd == nil || !m2.bgBusy {
		t.Fatal("first tick should fire one read and mark the lane busy")
	}
	if len(m2.bgQueue) != 1 {
		t.Fatalf("the second due item should be queued, got %v", m2.bgQueue)
	}
	// Second tick while busy: nothing new fires.
	_, cmd2 := m2.refreshTick(t0)
	if cmd2 != nil {
		t.Fatal("must not fire a second read while the lane is busy")
	}
}

func TestManualRFreesLane(t *testing.T) {
	// Reproduces the stranding bug: a bg read of branches is in flight; manual r
	// bumps the gen; the bg message arrives stale and MUST still free the lane.
	m := newTestModel(t)
	m.bgBusy = true
	m.bgActiveItem = refreshItem{source: srcBranches}
	staleGen := m.srcGen[srcBranches]
	m.srcGen[srcBranches] = staleGen + 1 // simulate manual r having superseded it
	msg := dataAvailableMsg{source: srcBranches, gen: staleGen, manual: false, value: []model.Branch(nil)}
	nm, _ := m.Update(msg)
	if nm.(Model).bgBusy {
		t.Fatal("a stale bg read message must still free the lane (stranding bug)")
	}
}

func TestStartOpClearsLaneAndQueue(t *testing.T) {
	m := newTestModel(t)
	ctx, cancel := context.WithCancel(context.Background())
	m.bgCtx, m.bgCancel = ctx, cancel
	m.bgBusy = true
	m.bgActiveItem = refreshItem{source: srcTags}
	m.bgQueue = []refreshItem{{source: srcStatus}}
	m2, _ := m.startOp(engine.Fetch{})
	if m2.bgBusy || len(m2.bgQueue) != 0 || m2.bgCancel != nil {
		t.Fatalf("startOp must clear the lane + queue, got busy=%v queue=%v", m2.bgBusy, m2.bgQueue)
	}
}

func TestSaveRefreshIntervalUpdatesAndReseeds(t *testing.T) {
	m := newTestModel(t)
	m.repoConfigPath = filepath.Join(t.TempDir(), ".gg.toml")
	m.refreshLastRun = map[refreshItem]time.Time{}
	it := refreshItem{source: srcBranches}
	m = m.saveRefreshInterval(it, 45)
	if m.cfg.Refresh.Branches != 45 {
		t.Fatalf("in-memory cfg not updated, got %d", m.cfg.Refresh.Branches)
	}
	if _, seeded := m.refreshLastRun[it]; !seeded {
		t.Fatal("lastRun must be reseeded so the edit doesn't burst")
	}
	cfg, err := config.Load("", m.repoConfigPath)
	if err != nil || cfg.Refresh.Branches != 45 {
		t.Fatalf("config file not written: err=%v branches=%d", err, cfg.Refresh.Branches)
	}
}

func TestRatesEditorEnterEditSave(t *testing.T) {
	m := newTestModel(t)
	m.repoConfigPath = filepath.Join(t.TempDir(), ".gg.toml")
	m, _ = m.openSettings()
	p := layerOf[*settingsPopup](m)
	p.ratesView = true
	p.ratesSel = 0 // status
	// enter → open edit field
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if !p.ratesEditing {
		t.Fatal("enter should open the edit field")
	}
	// type "25"
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	// enter → save
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})
	if p.ratesEditing {
		t.Fatal("enter should close the edit field")
	}
	if got := refreshIntervalFor(m.cfg.Refresh, scheduledItems[0]); got != 25 {
		t.Fatalf("status interval should be 25, got %d", got)
	}
}

func TestRatesEditorSpaceTogglesWatch(t *testing.T) {
	m := newTestModel(t)
	m.repoConfigPath = filepath.Join(t.TempDir(), ".gg.toml")
	m, _ = m.openSettings()
	p := layerOf[*settingsPopup](m)
	p.ratesView = true
	for i, it := range scheduledItems { // select the worktrees row (watch-eligible)
		if !it.isFetch && !it.isRemoteTags && it.source == srcWorktrees {
			p.ratesSel = i
		}
	}
	if m.cfg.Refresh.WorktreesWatch {
		t.Fatal("precondition: worktrees file-watch should start off")
	}
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeySpace})
	if !m.cfg.Refresh.WorktreesWatch {
		t.Fatal("space should toggle worktrees file-watch on")
	}
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeySpace})
	if m.cfg.Refresh.WorktreesWatch {
		t.Fatal("space again should toggle worktrees file-watch back off")
	}
}

func TestRatesEditorEscCancelsEdit(t *testing.T) {
	m := newTestModel(t)
	m, _ = m.openSettings()
	p := layerOf[*settingsPopup](m)
	p.ratesView = true
	p.ratesSel = 0

	// enter → open edit field
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !p.ratesEditing {
		t.Fatal("enter should open the edit field")
	}

	// esc → cancel edit (stay on rates screen, popup still open)
	m, _ = updateModel(m, tea.KeyMsg{Type: tea.KeyEsc})
	if p.ratesEditing {
		t.Fatal("esc should cancel the edit (ratesEditing must be false)")
	}
	if !p.ratesView {
		t.Fatal("esc while editing should stay on rates screen (ratesView must remain true)")
	}
	if layerOf[*settingsPopup](m) == nil {
		t.Fatal("esc while editing must not close the settings popup")
	}
}

// updateModel is a tiny helper to thread Update returns as Model.
func updateModel(m Model, msg tea.Msg) (Model, tea.Cmd) {
	nm, cmd := m.Update(msg)
	return nm.(Model), cmd
}

// TestBgFetchDoneIgnoresStaleGen verifies that a bgFetchDoneMsg from a
// superseded fetch cycle does not clear the live background lane.
func TestBgFetchDoneIgnoresStaleGen(t *testing.T) {
	m := newTestModel(t)
	m.bgBusy = true
	m.bgActiveItem = fetchItem
	m.bgFetchGen = 2

	// Stale completion (gen 1) must be ignored — lane stays busy.
	nm, _ := m.Update(bgFetchDoneMsg{gen: 1, dur: time.Second})
	if !nm.(Model).bgBusy {
		t.Fatal("stale bgFetchDoneMsg (gen 1 != bgFetchGen 2) must not clear the live lane")
	}

	// Current completion (gen 2) must free the lane.
	nm, _ = nm.(Model).Update(bgFetchDoneMsg{gen: 2, dur: time.Second})
	if nm.(Model).bgBusy {
		t.Fatal("current bgFetchDoneMsg (gen 2) must free the lane")
	}
}

func TestBgRefreshHint(t *testing.T) {
	m := newTestModel(t)
	if m.bgRefreshHint() != "" {
		t.Fatal("idle lane → no hint")
	}
	m.bgBusy = true
	m.bgActiveItem = refreshItem{source: srcBranches}
	if got := m.bgRefreshHint(); got != "⟳ branches…" {
		t.Fatalf("want '⟳ branches…', got %q", got)
	}
	m.bgActiveItem = fetchItem
	if got := m.bgRefreshHint(); got != "⟳ fetch…" {
		t.Fatalf("want '⟳ fetch…', got %q", got)
	}

	// A known-fast read (avg < 1s) is suppressed to avoid status-bar flicker.
	fast := refreshItem{source: srcTags}
	m.bgActiveItem = fast
	m.refreshDur[fast] = []time.Duration{200 * time.Millisecond, 300 * time.Millisecond}
	if got := m.bgRefreshHint(); got != "" {
		t.Fatalf("fast read (avg < 1s) should suppress the hint, got %q", got)
	}
	// A slow read (avg >= 1s) still shows the hint.
	slow := refreshItem{source: srcReflog}
	m.bgActiveItem = slow
	m.refreshDur[slow] = []time.Duration{2 * time.Second}
	if got := m.bgRefreshHint(); got != "⟳ reflog…" {
		t.Fatalf("slow read (avg >= 1s) should show the hint, got %q", got)
	}
}
