package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/gittest"
)

// addWorktree adds a second worktree on a new branch, reloads the Worktrees
// list so the model knows it, and returns its path.
func addWorktree(t *testing.T, m Model, name string) (Model, string) {
	t.Helper()
	other := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("git", "-C", m.currentWorktree, "worktree", "add", "-b", name, other).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	for _, s := range []sourceKey{srcWorktrees, srcBranches} { // the new branch too: the head marks key on it
		nm, _ := m.Update(m.readSourceCmd(context.Background(), s, reloadOpts{manual: true})())
		m = nm.(Model)
	}
	return m, other
}

// The first load seeds home: one slot, viewed = home = the worktree gg runs in.
func TestFirstLoadSeedsTheHomeSlot(t *testing.T) {
	m := loadedModel(t)
	if m.home == "" || m.viewed != m.home || m.home != model.KeyOf(m.currentWorktree) {
		t.Fatalf("home=%q viewed=%q current=%q", m.home, m.viewed, m.currentWorktree)
	}
	if v, ok := m.views[m.home]; !ok || v.svc != m.svc {
		t.Fatalf("views[home] = %+v, want the live service", v)
	}
}

// saveView then loadView is a round trip of the Status panel, its cursor and
// its marks; the live service is the slot's.
func TestSaveAndLoadViewRoundTrip(t *testing.T) {
	m := loadedModel(t)
	m.sel[panelFiles] = 3
	m.fileMarks = map[string]bool{"a.txt": true}
	m = m.saveView()
	home := m.views[m.home]
	m, otherPath := addWorktree(t, m, "wt2")
	other := m.ensureView(otherPath)
	m = m.loadView(other)
	if m.viewed != other.key || m.svc != other.svc || m.sel[panelFiles] != 0 || len(m.fileMarks) != 0 || m.currentWorktree != other.path {
		t.Fatalf("after load: viewed=%q sel=%d marks=%v current=%q", m.viewed, m.sel[panelFiles], m.fileMarks, m.currentWorktree)
	}
	m = m.loadView(home)
	if m.sel[panelFiles] != 3 || !m.fileMarks["a.txt"] || m.svc != home.svc {
		t.Fatalf("home not restored: sel=%d marks=%v", m.sel[panelFiles], m.fileMarks)
	}
}

// ensureView is keyed by the cleaned path and reuses the slot.
func TestEnsureViewIsKeyedByCleanPath(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	a := m.ensureView(other + string(filepath.Separator))
	b := m.ensureView(other)
	if a != b || a.path != filepath.Clean(other) {
		t.Fatalf("slots differ: %p %p path=%q", a, b, a.path)
	}
}

// landView runs the swap's refresh kick (what the Update tail launches) and
// applies its status read, as the runtime would.
func landView(t *testing.T, m Model) Model {
	t.Helper()
	cmd := m.viewKickCmd()
	m.viewKick = false
	if cmd == nil {
		return m
	}
	for _, msg := range drainBatch(cmd) {
		if _, ok := msg.(dataAvailableMsg); !ok {
			continue // the watcher/docs results need a live loop; the status read is what we assert
		}
		nm, _ := m.Update(msg)
		m = nm.(Model)
	}
	return m
}

// drainBatch runs a cmd (a tea.Batch or a single cmd) and collects the
// messages its leaves return.
func drainBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, drainBatch(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// Switching to another worktree shows ITS files; switching back restores
// home's cursor and marks without a reload.
func TestSwitchViewShowsTheOtherWorktreesStatusAndRestoresHome(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	if err := os.WriteFile(filepath.Join(other, "only-there.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.sel[panelFiles] = 1
	m.fileMarks = map[string]bool{"home.txt": true}
	m, ok := m.switchView(other)
	if !ok || m.viewed != model.KeyOf(other) || !m.viewKick {
		t.Fatalf("switch: ok=%v viewed=%q kick=%v msg=%q", ok, m.viewed, m.viewKick, m.statusMsg)
	}
	if m.home == m.viewed {
		t.Fatal("home must stay the worktree gg runs in")
	}
	m = landView(t, m)
	var seen bool
	for _, f := range m.status.Files {
		seen = seen || f.Path == "only-there.txt"
	}
	if !seen {
		t.Fatalf("status after switch = %+v, want wt2's untracked file", m.status.Files)
	}
	m, _ = m.switchView(m.homeWorktree())
	if m.sel[panelFiles] != 1 || !m.fileMarks["home.txt"] || m.viewed != m.home {
		t.Fatalf("home not restored: sel=%d marks=%v viewed=%q", m.sel[panelFiles], m.fileMarks, m.viewed)
	}
	for _, f := range m.status.Files {
		if f.Path == "only-there.txt" {
			t.Fatal("home shows wt2's file")
		}
	}
}

// A status read launched for the old slot lands after the swap: dropped.
func TestSwitchViewDropsAStaleStatusRead(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	stale := m.readSourceCmd(context.Background(), srcStatus, reloadOpts{manual: true})
	m, _ = m.switchView(other)
	m = landView(t, m)
	before := m.status
	nm, _ := m.Update(stale())
	m = nm.(Model)
	if len(m.status.Files) != len(before.Files) || m.viewed != model.KeyOf(other) {
		t.Fatalf("a stale read landed: %+v", m.status.Files)
	}
}

// The swap is a no-op for the viewed path, refused for a path that is not a
// worktree of this repository, and refused while an operation runs.
func TestSwitchViewRefusals(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	if nm, ok := m.switchView(m.viewPath(m.viewed)); !ok || nm.viewKick {
		t.Fatalf("same path: ok=%v kick=%v", ok, nm.viewKick)
	}
	if _, ok := m.switchView(t.TempDir()); ok {
		t.Fatal("a foreign path must be refused")
	}
	m.running = true
	if nm, ok := m.switchView(other); ok || nm.statusMsg == "" {
		t.Fatalf("running op: ok=%v msg=%q", ok, nm.statusMsg)
	}
}

// Only the live slot is watched: the slot left behind has no watcher.
func TestSwitchViewPutsTheLeavingSlotToSleep(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m.watchSupported = true
	m.srcInflight[srcStatus], m.srcLoading[srcStatus] = true, true // a status read of the leaving slot in flight
	wg, sg, fg := m.watchGen, m.srcGen[srcStatus], m.srcGen[srcFeed]
	m, _ = m.switchView(other)
	if m.watcher != nil || m.watchSupported {
		t.Fatal("the arriving slot starts with no watcher until its kick lands")
	}
	if m.watchGen == wg || m.srcGen[srcStatus] == sg || m.srcGen[srcFeed] == fg {
		t.Fatalf("a result for the leaving slot could still land: watch %d→%d status %d→%d feed %d→%d", wg, m.watchGen, sg, m.srcGen[srcStatus], fg, m.srcGen[srcFeed])
	}
	if m.srcInflight[srcStatus] || m.srcLoading[srcStatus] {
		t.Fatal("the leaving slot's read is still marked in flight / loading on the arriving one")
	}
}

// The Update tail launches the kick once and clears the flag.
func TestUpdateTailLaunchesTheViewKick(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.switchView(other)
	nm, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if nm.(Model).viewKick || cmd == nil {
		t.Fatalf("kick=%v cmd=%v", nm.(Model).viewKick, cmd)
	}
}

// A worktree removed while viewed: the view falls back to home and the
// slot is gone; a removed hidden slot is dropped silently.
func TestWorktreesReloadDropsAGoneSlot(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.switchView(other)
	m = landView(t, m)
	if out, err := exec.Command("git", "-C", m.homeWorktree(), "worktree", "remove", "--force", other).CombinedOutput(); err != nil {
		t.Fatalf("worktree remove: %v\n%s", err, out)
	}
	read := m.readSourceCmd(context.Background(), srcWorktrees, reloadOpts{manual: true})
	nm, _ := m.Update(read())
	m = nm.(Model)
	if m.viewed != m.home || m.views[model.KeyOf(other)] != nil || m.svc != m.views[m.home].svc {
		t.Fatalf("viewed=%q slots=%v", m.viewed, m.views)
	}
	if !strings.Contains(m.statusMsg, "wt2") {
		t.Fatalf("status = %q, want it to name the removed worktree", m.statusMsg)
	}
}

// enter on another worktree's row is instant: the view is its, gg's exit
// dir and home follow, the Commits cursor and an open diff survive, the
// screen never blanks.
func TestInRepoSwitchAdoptsWithoutAReload(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	m.sel[panelCommits] = 0
	m = m.pushLayer(&diffView{rev: "abc1"}) // a commit's diff survives (a working-tree one closes: it would show the old tree's file)
	nm, _ := m.guardedReRoot(other, true)
	m = nm.(Model)
	if m.viewed != model.KeyOf(other) || m.home != m.viewed || m.switchTarget != filepath.Clean(other) || publishedWorktree() != filepath.Clean(other) {
		t.Fatalf("viewed=%q home=%q target=%q published=%q", m.viewed, m.home, m.switchTarget, publishedWorktree())
	}
	if !m.ready || m.loading || m.diffLayer() == nil {
		t.Fatalf("the in-repo switch reloaded: ready=%v loading=%v diff=%v", m.ready, m.loading, m.diffLayer())
	}
	if m.snapshotPath != "" {
		t.Fatal("the old snapshot target must be disabled until the new one resolves")
	}
}

// The snapshot target re-resolves for the adopted worktree.
func TestAdoptViewReResolvesTheSnapshotTarget(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	nm, cmd := m.guardedReRoot(other, true)
	m = nm.(Model)
	for _, msg := range drainBatch(cmd) {
		if st, ok := msg.(snapshotTargetMsg); ok {
			nm, _ := m.Update(st)
			m = nm.(Model)
		}
	}
	if filepath.Clean(m.snapshotWorktree) != filepath.Clean(other) {
		t.Fatalf("snapshotWorktree = %q, want %q", m.snapshotWorktree, other)
	}
}

// A target in another repository keeps the full reload.
func TestOtherRepoSwitchStillReRoots(t *testing.T) {
	m := loadedModel(t)
	nm, _ := m.guardedReRoot(gittest.BasicRepo(t, "b\n"), false)
	m = nm.(Model)
	if m.ready || len(m.views) != 0 {
		t.Fatalf("ready=%v views=%v: another repo must reRoot", m.ready, m.views)
	}
}

// A switch asked while a console views B adopts the target and the console's
// return now points there.
func TestSwitchWhileAConsoleIsShownAdoptsAndRetargetsTheReturn(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.showConsole(id, false)
	nm, _ := m.guardedReRoot(other, true)
	m = nm.(Model)
	if m.home != model.KeyOf(other) || m.console == nil || m.console.ret.view != m.home {
		t.Fatalf("home=%q console=%+v", m.home, m.console)
	}
	m = m.closeConsole()
	if m.viewed != model.KeyOf(other) {
		t.Fatalf("close must stay in the adopted worktree, viewed=%q", m.viewed)
	}
}

// reRoot to another repository drops the slots and the console's return
// view, so a later close cannot swap to a path of the old repo.
func TestRepoSwitchDropsTheSlots(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.showConsole(id, false)
	nm, _ := m.reRoot(gittest.BasicRepo(t, "b\n"))
	m = nm.(Model)
	if len(m.views) != 0 || m.viewed != "" || (m.console != nil && m.console.ret.view != "") {
		t.Fatalf("views=%v viewed=%q console=%+v", m.views, m.viewed, m.console)
	}
}

// A console looking at B does not move the hosted web page; adopting does.
func TestAdoptViewRerootsTheWebPageLookDoesNot(t *testing.T) {
	f := installFakeHost(t)
	m := servingModel(t, f)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.showConsole(id, false)
	if hasWebReroot(drainBatch(m.viewKickCmd())) {
		t.Fatal("a look must not reroot the page")
	}
	m = m.closeConsole()
	nm, cmd := m.guardedReRoot(other, true)
	_ = nm
	if !hasWebReroot(drainBatch(cmd)) {
		t.Fatal("adopt must reroot the page")
	}
}

func hasWebReroot(msgs []tea.Msg) bool {
	for _, msg := range msgs {
		if _, ok := msg.(webRerootMsg); ok {
			return true
		}
	}
	return false
}

// The startup path (configReadyMsg → the per-source fan-out, never the
// legacy dataLoadedMsg) seeds home too: without it every in-repo switch
// would still reload and a console would have no home to return to.
func TestConfigReadySeedsTheHomeSlot(t *testing.T) {
	t.Parallel()
	m := New(nil)
	const root = "/mnt/t/others/gigagit"
	got, _ := m.Update(configReadyMsg{top: root})
	mm := got.(Model)
	if mm.home != root || mm.viewed != root || mm.views[root] == nil {
		t.Fatalf("home=%q viewed=%q views=%v", mm.home, mm.viewed, mm.views)
	}
}

// A slot made for another worktree runs the same policies as the live
// service: the config's branch-version policy decides whether an op run
// THERE writes version refs, and the diff colouring, EOL and notes
// policies follow the same config. A bare service would silently use the
// defaults (versions on, 90 days).
func TestASlotServiceCarriesTheConfigPolicies(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m.cfg.Versions.Disabled = true
	m.cfg.Versions.MaxAgeDays = 7
	m.cfg.UI.DiffSyntax = "off"
	v := m.ensureView(other)
	if p := v.svc.VersionsPolicy(); p.Enabled || p.MaxAgeDays != 7 {
		t.Fatalf("slot versions policy = %+v, want disabled, 7 days", p)
	}
	if v.svc.SyntaxHighlighting() {
		t.Fatal("slot service highlights syntax although the config turned it off")
	}
}

// A versions setting changed while several slots exist reaches every
// slot, not only the one on screen.
func TestVersionsSettingReachesEverySlot(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	v := m.ensureView(other)
	m = m.toggleVersionsRecording()
	if p := v.svc.VersionsPolicy(); p.Enabled != !m.cfg.Versions.Disabled {
		t.Fatalf("slot versions policy = %+v after the toggle, cfg disabled=%v", p, m.cfg.Versions.Disabled)
	}
	m = m.saveVersionsRetention(3)
	if p := v.svc.VersionsPolicy(); p.MaxAgeDays != 3 {
		t.Fatalf("slot retention = %d after the save, want 3", p.MaxAgeDays)
	}
}
