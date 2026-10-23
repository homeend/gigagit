package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
	"github.com/homeend/gigagit/internal/workspace"
)

// Probe round 2: a member's life while it sleeps — revive, a removed
// last-viewed worktree, cursor over a refreshed group, out-of-order reads,
// the poll fallback, a member gone missing, a parked window's answer.

// a member that was visited (everLive), went missing while asleep
// and came back: the revive must read it and, asleep again, watch it.
func TestRevivedVisitedMemberReadsAndWatches(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	a := m.homeRepo()
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = settleTail(t, nm)
	nm, ok = m.switchRepo(a, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = settleTail(t, nm)
	away := b.mainPath + "-away"
	if err := os.Rename(b.mainPath, away); err != nil {
		t.Fatal(err)
	}
	u, _ := m.Update(memberLoadCmd(b)()) // its watcher fired: the read says missing
	m = u.(Model)
	if !b.missing {
		t.Fatal("not marked missing")
	}
	if err := os.Rename(away, b.mainPath); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, b.mainPath, "branch", "while-away")
	nm, ok = m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = settleTail(t, nm)
	seen := false
	for _, br := range m.branches {
		if br.Name == "while-away" {
			seen = true
		}
	}
	if !seen {
		t.Errorf("revived member's live Branches never re-read: %d branches, loaded=%v", len(m.branches), b.loaded)
	}
	nm, ok = m.switchRepo(a, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = settleTail(t, nm)
	if !b.loaded || !(b.watcher != nil || b.polling || b.watchStarting) {
		t.Fatalf("revived member asleep unwatched: loaded=%v watcher=%v polling=%v starting=%v", b.loaded, b.watcher != nil, b.polling, b.watchStarting)
	}
}

// a sleeping member's last-viewed worktree removed from a shell:
// a switch into the member (R, heading click) lands on its main checkout.
func TestDefaultWorktreeSkipsRemovedLastViewed(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	a := m.homeRepo()
	linked := filepath.Join(t.TempDir(), "b-linked")
	gittest.Run(t, b.mainPath, "worktree", "add", "-b", "linked", linked)
	m = settleWorkspace(t, m, memberLoadCmd(b))
	nm, ok := m.switchRepo(b, linked, true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = settleTail(t, nm)
	nm, ok = m.switchRepo(a, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = settleTail(t, nm)
	gittest.Run(t, b.mainPath, "worktree", "remove", linked)
	m = settleWorkspace(t, m, memberLoadCmd(b)) // its watcher re-read the list: linked is gone
	if len(b.worktrees) != 1 {
		t.Fatalf("B list = %+v", b.worktrees)
	}
	nm, ok = m.switchRepo(b, "", true)
	if !ok {
		t.Fatalf("switch into B refused: %q (lastViewed=%q)", nm.statusMsg, b.lastViewed)
	}
	if model.KeyOf(nm.currentWorktree) != b.key {
		t.Fatalf("landed on %q, want B main %q", nm.currentWorktree, b.mainPath)
	}
}

// open another workspace while LOOKING at member B (home in A),
// the new workspace drops A: home must follow B, settings follow a member.
func TestOpenOtherWorkspaceDuringLookDropsHome(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = settleTail(t, nm)
	c := secondRepo(t)
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "two", Repos: []string{b.mainPath, c}})
	m = settleWorkspace(t, nm, cmd)
	if m.homeView() == nil || m.homeRepo() != b {
		t.Fatalf("home dangling: view=%v homeRepo==b %v", m.homeView() != nil, m.homeRepo() == b)
	}
	sr := m.settingsRepo()
	if !m.isMember(sr) {
		t.Fatalf("settings repo %s is not a member", sr.mainPath)
	}
	if m.repoConfigPath != memberConfigPath(sr.mainPath) {
		t.Fatalf("target %q", m.repoConfigPath)
	}
}

// leave / re-enter cycles: no member keeps a watcher after leave.
func TestLeaveReenterLeavesNoWatchers(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	home := m.homeRepo()
	other := secondRepo(t)
	e := workspace.Entry{Name: "w", Repos: []string{home.mainPath, other}}
	var all []*repoView
	for i := 0; i < 4; i++ {
		nm, cmd := m.openWorkspace(e)
		m = settleWorkspace(t, nm, cmd)
		m = settleTail(t, m)
		all = append(all, m.ws.members...)
		nm, cmd = m.leaveWorkspace()
		m = settleWorkspace(t, nm, cmd)
	}
	for _, r := range all {
		if r.watcher != nil || r.polling || r.watchStarting {
			t.Errorf("member %s still watched after leave (watcher=%v polling=%v starting=%v)", r.mainPath, r.watcher != nil, r.polling, r.watchStarting)
		}
	}
	if m.liveRepo() != home || model.KeyOf(m.currentWorktree) != home.key {
		t.Fatalf("not back on home")
	}
}

// a member whose FIRST read failed polls; once a read succeeds it
// must get its watcher (polling is the fallback for an unwatchable git dir,
// spec §6), not poll on the 30 s beat for the rest of the session.
func TestRetriedMemberUpgradesToWatcher(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	home := m.homeRepo()
	nm, _ := m.openWorkspace(workspace.Entry{Name: "w", Repos: []string{home.mainPath, secondRepo(t)}})
	m = nm
	closeWatchersOnExit(t, m)
	b := m.ws.members[1]
	u, _ := m.Update(memberLoadedMsg{svc: b.svc, gen: b.loadGen, err: os.ErrDeadlineExceeded})
	m = u.(Model)
	if !b.retrying {
		t.Fatal("precondition: the poll beat is not reading again")
	}
	// the poll beat's read succeeds
	m = settleWorkspace(t, m, memberLoadCmd(b))
	m = settleTail(t, m)
	if !b.loaded {
		t.Fatal("not loaded")
	}
	if b.watcher == nil && !b.watchStarting {
		t.Fatalf("B still on the poll fallback after a good read: polling=%v watcher=%v", b.polling, b.watcher != nil)
	}
}

// a sleeping member's list refresh (its watcher's read) shrinks the
// group the cursor is in: the cursor stays on its row (by identity, as a
// live refresh restores it), never slides into the next member's group.
func TestSleepingMemberRefreshKeepsCursorRow(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	home := m.homeRepo()
	bDir := secondRepo(t)
	gittest.Run(t, bDir, "branch", "b1")
	gittest.Run(t, bDir, "branch", "b2")
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "w", Repos: []string{bDir, home.mainPath}}) // B first, A (live) second
	m = settleWorkspace(t, nm, cmd)
	b := m.ws.members[0]
	if !b.loaded || m.liveRepo() != home {
		t.Fatal("setup")
	}
	m.activeLeftTab, m.focus = panelBranches, panelBranches
	row := -1
	for i := 0; i < m.panelLen(panelBranches); i++ {
		if m.rowKeyAt(panelBranches, i) == "main" { // B's LAST row (its group lists b1, b2, main)
			mem, _, _, _ := func() (int, bool, string, bool) { m.sel[panelBranches] = i; return m.cursorRowMember(panelBranches) }()
			if mem == 0 {
				row = i
			}
		}
	}
	if row < 0 {
		t.Fatal("B's main row not found")
	}
	m.sel[panelBranches] = row
	before := m.panelSelKey(panelBranches)
	gittest.Run(t, bDir, "branch", "-D", "b1")
	m = settleWorkspace(t, m, memberLoadCmd(b))
	m = settleTail(t, m)
	mem, _, _, _ := m.cursorRowMember(panelBranches)
	if after := m.panelSelKey(panelBranches); after != before || mem != 0 {
		t.Fatalf("cursor moved from B's %q to member %d's %q", before, mem, after)
	}
}

// two reads of one sleeping member in flight (two watcher events
// while a slow read runs) landing out of order: the older read must not
// overwrite the newer one's lists (no in-flight guard, same gen).
func TestOutOfOrderMemberReadsKeepNewest(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	older := memberLoadCmd(b)() // event 1's read, slow
	gittest.Run(t, b.mainPath, "branch", "newer")
	newer := memberLoadCmd(b)() // event 2's read, fast
	u, _ := m.Update(newer)
	m = u.(Model)
	u, _ = m.Update(older)
	m = u.(Model)
	for _, br := range b.branches {
		if br.Name == "newer" {
			return
		}
	}
	t.Fatalf("an older member read overwrote a newer one: %d branches, no %q", len(b.branches), "newer")
}

// Identity triple after look -> adopt -> Leave: everything names B.
func TestLookAdoptLeaveIdentity(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	if err := os.WriteFile(filepath.Join(b.mainPath, ".gg.toml"), []byte("[ui]\nwheel_step = 7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = settleTail(t, nm)
	tm, cmd := m.guardedReRoot(b.mainPath, false, true)
	m = settleWorkspace(t, tm.(Model), cmd)
	if m.home != b.key {
		t.Fatalf("adopt: home=%q", m.home)
	}
	nm, cmd = m.leaveWorkspace()
	m = settleWorkspace(t, nm, cmd)
	m = settleTail(t, m)
	if m.inWorkspace() || m.liveRepo() != b || m.homeRepo() != b {
		t.Fatal("not single B")
	}
	if m.repoConfigPath != memberConfigPath(b.mainPath) {
		t.Errorf("repoConfigPath=%q", m.repoConfigPath)
	}
	if m.cfg.UI.WheelStep != 7 {
		t.Errorf("cfg not B's own: wheel_step=%d", m.cfg.UI.WheelStep)
	}
	if model.KeyOf(filepath.Dir(m.gitCommonDir)) != b.key {
		t.Errorf("gitCommonDir=%q", m.gitCommonDir)
	}
	if model.KeyOf(publishedWorktree()) != b.key || model.KeyOf(m.currentWorktree) != b.key {
		t.Errorf("published=%q current=%q", publishedWorktree(), m.currentWorktree)
	}
	if m.snapshotCommonDir != "" && model.KeyOf(filepath.Dir(m.snapshotCommonDir)) != b.key {
		t.Errorf("snapshotCommonDir=%q", m.snapshotCommonDir)
	}
}

// a member missing at open whose path is back (remounted): R revives
// it (switchRepo's revive), a click on its heading refuses "(missing)".
func TestHeadingClickRevivesRemountedMember(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	home := m.homeRepo()
	gone := filepath.Join(t.TempDir(), "later")
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "w", Repos: []string{home.mainPath, gone}})
	m = settleWorkspace(t, nm, cmd)
	r := m.ws.members[1]
	if !r.missing {
		t.Fatal("not missing")
	}
	if err := os.Rename(gittest.BasicRepo(t, "hi\n"), gone); err != nil {
		t.Fatal(err)
	}
	m.activeLeftTab, m.focus = panelWorktrees, panelWorktrees
	row := -1
	for i := 0; i < m.panelLen(panelWorktrees); i++ {
		m.sel[panelWorktrees] = i
		if mem, heading, _, ok := m.cursorRowMember(panelWorktrees); ok && heading && mem == 1 {
			row = i
		}
	}
	if row < 0 {
		t.Fatal("no heading")
	}
	m, _ = m.clickHeading(panelWorktrees, row)
	if m.liveRepo() != r {
		t.Fatalf("heading click on a remounted member refused: %q", m.statusMsg)
	}
}

// a sleeping member that goes missing (unmounted) keeps its old rows
// under its "(missing)" heading and in the ring (missing-at-open shows none);
// alt+w stat-skips its stops and names them on every pass.
func TestMemberGoneMissingLeavesTheRing(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	home := m.homeRepo()
	bDir, cDir := secondRepo(t), secondRepo(t)
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "w", Repos: []string{home.mainPath, bDir, cDir}})
	m = settleWorkspace(t, nm, cmd)
	b := m.ws.members[1]
	if len(m.worktreeRing()) != 3 {
		t.Fatalf("ring = %+v", m.worktreeRing())
	}
	if err := os.Rename(bDir, bDir+"-away"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(bDir+"-away", bDir) })
	u, _ := m.Update(memberLoadCmd(b)())
	m = u.(Model)
	if !b.missing {
		t.Fatal("not missing")
	}
	for _, s := range m.worktreeRing() {
		if s.member == 1 {
			t.Fatalf("the missing member still has ring stops: %+v", m.worktreeRing())
		}
	}
}

// a window-addressed answer asked from member A's worktree lands
// while member B is on screen. routeSlotMsg looks the slot up in m.views
// (the LIVE member's map) only: A's sleeping slot reads as GONE — the
// answer is dropped instead of queued (spec §6: "results for a sleeping
// worktree keep queuing as today"), and its process-wide part
// (applyShared: a PR row) is written into B's lists.
func TestSleepingMemberSlotAnswerQueuesInItsSlot(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	a := m.homeRepo()
	m.prs = []model.PullRequest{{Number: 7, Title: "A seven"}}
	stamp := m.stamp() // A's PR view asks for PR #7
	aSlot := m.viewed
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = settleTail(t, nm)
	m.prs = []model.PullRequest{{Number: 7, Title: "B seven"}}
	u, _ := m.Update(prRevalidatedMsg{slotStamp: stamp, n: 7, gen: m.forgeGen, pr: model.PullRequest{Number: 7, Title: "A seven v2"}})
	m = u.(Model)
	if m.prs[0].Title != "B seven" {
		t.Errorf("A's PR #7 row was written into member B's PR list: %q", m.prs[0].Title)
	}
	v := a.views[aSlot]
	if v == nil {
		t.Fatal("A's slot gone")
	}
	if len(v.queued) == 0 {
		t.Errorf("the answer for A's sleeping slot was dropped, not queued (queued=%d)", len(v.queued))
	}
}

// switching into a member whose load has not landed yet (R /
// a heading click / a link right after the workspace opens) attaches the
// slot's session-docs directory under HOME's common dir (commonDirFor
// falls back to snapshotCommonDir) — the agent's `gg session note|
// overview` in B writes under B's own common dir, so its notes and
// overviews never show; nothing re-attaches once B's common dir is known.
func TestDocsDirOfAMemberSwitchedToBeforeItsLoad(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo()
	other := secondRepo(t)
	m.snapshotCommonDir = filepath.Join(home.mainPath, ".git") // what Run resolves at start
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "shop", Repos: []string{home.mainPath, other}})
	m = nm
	b := m.ws.members[1]
	if b.loaded {
		t.Fatal("fixture: B loaded synchronously")
	}
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	root := domain.CheckoutKey(b.mainPath)
	t.Cleanup(func() {
		m.docs.Detach(root)
		m.docs.Detach(domain.CheckoutKey(home.mainPath))
	})
	m = settleWorkspace(t, m, cmd) // B's load lands (live now)
	m = runTail(t, m)              // its first-visit reads and health probe
	out, err := exec.Command("git", "-C", other, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		t.Fatal(err)
	}
	want := steer.DocsDir(config.SessionSteerDir(strings.TrimSpace(string(out)), b.mainPath))
	if got := m.docs.Attached(root); got != want {
		t.Fatalf("B's main worktree docs dir = %q\nwant (where `gg session note` in B writes) %q", got, want)
	}
}
