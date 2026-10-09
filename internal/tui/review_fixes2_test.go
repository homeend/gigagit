package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// --- review item 1: a console shown into a worktree with parked windows ---

// Home parked a commit diff (alt+w to B); in B the user opens a console of
// a session in home: the console shows over home, and home's parked diff
// must NOT come up live over it (a focused console takes every key — the
// user would type into an agent under a screen they cannot see). The diff
// is displaced like a captured one and comes back when the console closes.
func TestConsoleShownIntoAWorktreeWithParkedWindowsDisplacesThem(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, home, "Shell")
	m = m.pushLayer(&diffView{title: "f", rev: "abc1"})
	m, _ = m.userSwitchView(other)
	m, _ = m.showConsoleBy(id, true, true) // alt+a into home
	if m.viewed != model.KeyOf(home) || m.console == nil {
		t.Fatalf("viewed=%q console=%v", m.viewed, m.console != nil)
	}
	if m.topLayer() != nil {
		t.Fatalf("home's parked diff is live over the console: %T", m.topLayer())
	}
	if m.consoleParked == nil || len(m.consoleParked.layers) != 1 {
		t.Fatalf("the diff was not displaced under the console: %+v", m.consoleParked)
	}
	m = m.closeConsole() // the console was shown from B: the panels return there
	if m.viewed != model.KeyOf(other) || m.topLayer() != nil {
		t.Fatalf("after the close: viewed=%q top=%T", m.viewed, m.topLayer())
	}
	m, _ = m.switchView(home)
	if d, _ := m.topLayer().(*diffView); d == nil || d.title != "f" {
		t.Fatalf("the diff did not come back with home: %T", m.topLayer())
	}
}

// --- review item 2: every window-addressed constructor stamps its slot ---

// A message type that embeds slotStamp must be stamped wherever it is
// built (a zero stamp passes the gate and lands on whatever worktree is
// on screen). Grep over the non-test sources: every literal of such a
// type carries slotStamp.
func TestEveryStampedMessageConstructorCarriesItsSlot(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			b, err := os.ReadFile(e.Name())
			if err != nil {
				t.Fatal(err)
			}
			sources = append(sources, string(b))
		}
	}
	all := strings.Join(sources, "\n")
	typeRe := regexp.MustCompile(`(?m)^type (\w+) struct \{\n\tslotStamp`)
	var stamped []string
	for _, mm := range typeRe.FindAllStringSubmatch(all, -1) {
		stamped = append(stamped, mm[1])
	}
	if len(stamped) < 19 {
		t.Fatalf("found %d stamped types, expected at least 19", len(stamped))
	}
	for _, name := range stamped {
		lit := regexp.MustCompile(`\b` + name + `\{[^}\n]*(}[^\n]*)?`)
		for _, l := range lit.FindAllString(all, -1) {
			if strings.HasPrefix(l, name+"{}") {
				continue // the empty literal (a zero value in a comparison or a field)
			}
			if strings.Contains(l, "slotStamp:") || strings.Contains(l, "// stamped by ") {
				continue // stamped here, or by the caller that wraps the result (named in the note)
			}
			t.Errorf("%s is built without its slot stamp: %q", name, strings.TrimSpace(l))
		}
	}
}

// --- review item 3: a popup with work in flight is not parkable ---

func TestPopupsWithWorkInFlightAreNotParkable(t *testing.T) {
	t.Parallel()
	for name, l := range map[string]layer{
		"commit generating":   &commitPopup{generating: true},
		"goto resolving":      &gotoCommitPopup{resolving: true},
		"repo path resolving": &repoPathPopup{resolving: true},
	} {
		if parkableLayer(l) {
			t.Errorf("%s: parkable, its result would land nowhere", name)
		}
	}
	if !parkableLayer(&commitPopup{}) || !parkableLayer(&gotoCommitPopup{}) || !parkableLayer(&repoPathPopup{}) {
		t.Error("the idle popups must stay parkable")
	}
}

// --- review item 4: a parked navigate expires in its sleeping worktree ---

func TestParkedSteerNavigateExpiresInItsSleepingWorktree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.viewed
	m, other := addWorktree(t, m, "wt2")
	if err := os.WriteFile(filepath.Join(other, "only-there.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = m.steerNavigateStatusFile(steer.Command{Cmd: "navigate", File: "only-there.txt"}, false)
	if m.pendingSteer == nil {
		t.Fatal("precondition: parked")
	}
	m, _ = m.switchView(other)
	m.views[home].windows.pendingSteer.at = time.Now().Add(-time.Minute)
	m, _ = m.expireParkedSteer(time.Now())
	if m.views[home].windows.pendingSteer != nil {
		t.Fatal("the parked navigate outlived its TTL in the sleeping worktree")
	}
}

// --- review item 5: a parked history's pane gets its diff ---

func TestParkedHistoryPaneReceivesItsDiff(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	h := &historyView{commits: []model.FileCommit{{Commit: model.Commit{Hash: "0123456789abcdef0123456789abcdef01234567"}, Path: "a.go"}}}
	m = m.pushLayer(h)
	cmd := h.selectCmd(m)
	m, _ = m.switchView(other)
	nm, _ := m.Update(cmd())
	m = nm.(Model)
	if h.diff == nil || h.diff.loading {
		t.Fatal("the parked history's pane is still loading: its diff was dropped or misrouted")
	}
}

// --- review item 6: a list read in flight at sleep does not defeat the re-read ---

func TestFWindowListReadInFlightAtSleepDoesNotDefeatTheReRead(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.openWorktreeFiles() // its read is in flight
	m, _ = m.switchView(other)
	old := lsFilesMsg{paths: []string{"stale.go"}}
	old.slot = model.KeyOf(home)
	nm, _ := m.Update(old) // the in-flight read lands while home sleeps: queued
	m = nm.(Model)
	m, _ = m.switchView(home)
	m, _ = m.replayQueued()
	for _, msg := range drainBatch(m.viewKickCmd()) {
		if ls, ok := msg.(lsFilesMsg); ok {
			nm, _ := m.Update(ls)
			m = nm.(Model)
		}
	}
	if m.wtFiles == nil || m.wtFiles.loading || len(m.wtFiles.all) == 0 || m.wtFiles.all[0] == "stale.go" {
		t.Fatalf("the stale pre-sleep list won over the fresh read: %+v", m.wtFiles)
	}
}

// --- review item 7: a queued shelf list does not overwrite a newer one on replay ---

func TestQueuedShelfListDoesNotOverwriteANewerOne(t *testing.T) {
	m := loadedModel(t)
	home := m.viewed
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.switchView(other)
	one := shelfLoadedMsg{entries: []model.ShelfEntry{{ID: "e1"}}}
	one.slot = home
	nm, _ := m.Update(one) // home's reload lands while it sleeps
	m = nm.(Model)
	if len(m.shelfEntries) != 1 {
		t.Fatalf("the shared shelf list was not updated at once: %d entries", len(m.shelfEntries))
	}
	nm, _ = m.Update(shelfLoadedMsg{entries: []model.ShelfEntry{{ID: "e1"}, {ID: "e2"}}}) // B's own newer reload
	m = nm.(Model)
	m, _ = m.switchView(m.viewPath(home))
	m, _ = m.replayQueued()
	if len(m.shelfEntries) != 2 {
		t.Fatalf("the replayed older list overwrote the newer one: %d entries", len(m.shelfEntries))
	}
}

// --- review item 9: the leftover put back on the leaving slot stays capped ---

func TestReplayLeftoverStaysCapped(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.viewed
	m, other := addWorktree(t, m, "wt2")
	m = m.pushLayer(&repoPathPopup{input: newTextField(other)})
	m.replay = []tea.Msg{repoResolvedMsg{path: other, top: other}}
	for i := 0; i < queueCap+10; i++ {
		msg := versionsLoadedMsg{gen: i}
		msg.slot = home
		m.replay = append(m.replay, msg)
	}
	m, _ = m.replayQueued()
	if n := len(m.views[home].queued); n != queueCap {
		t.Fatalf("leftover queue = %d, want the cap %d", n, queueCap)
	}
}

// --- review item 2, behaviour: a pair open asked from A never opens over B ---

func TestPairOpenResultForASleepingSlotStaysOffTheScreen(t *testing.T) {
	m := loadedModel(t)
	home := m.viewed
	m, other := addWorktree(t, m, "wt2")
	m = forceSwitch(t, m, other) // B: previewGen 0, as a fresh A's
	msg := pairOpenMsg{gen: m.previewGen}
	msg.slot = home
	nm, _ := m.Update(msg)
	m = nm.(Model)
	if m.filesView != nil {
		t.Fatal("A's pair opened its compare over B")
	}
	if len(m.views[home].queued) != 1 {
		t.Fatalf("A's queue = %d, want the pair open waiting there", len(m.views[home].queued))
	}
}
