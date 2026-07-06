package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/clipboard"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/promptstate"
)

// noticeTestModel: loaded model + temp prompt store (never the developer's
// real prompts.toml — same rule as promptTestModel in related_prompts_test.go).
func noticeTestModel(t *testing.T) (Model, promptstate.Store) {
	t.Helper()
	m, _ := settingsModel(t)
	st := promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	m.promptStore = st
	return m, st
}

// bigRepoHealth fabricates a health snapshot that should trigger the
// commit-graph notice: big pack, no graph, config unset.
func bigRepoHealth() model.RepoHealth {
	return model.RepoHealth{
		GitCommonDir: "/fake/common/dir",
		PackBytes:    bigRepoPackBytes + 1,
	}
}

func TestRepoHealthMsgBuildsCommitGraphNotice(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	m = nm.(Model)
	if len(m.notices) != 1 || m.notices[0].id != noticeCommitGraph {
		t.Fatalf("notices = %+v, want the commit-graph notice", m.notices)
	}
	if !m.noticesUnread {
		t.Fatal("a fresh notice must start unread (blinking)")
	}
	if seg := m.noticeSegment(); !strings.Contains(seg, "1 notice") {
		t.Fatalf("status segment = %q, want '! 1 notice …'", seg)
	}
}

// x11NoToolAvail is the reported machine: an X11 desktop with no clipboard tool.
func x11NoToolAvail() clipboard.Availability {
	return clipboard.Availability{Session: "x11", Install: "xclip"}
}

func TestClipboardNoticeBuilder(t *testing.T) {
	// Fires: a local display is present but no native tool is installed.
	if n := clipboardNotice(x11NoToolAvail(), "/k"); n == nil || n.id != noticeClipboard {
		t.Fatalf("want the clipboard notice, got %+v", n)
	}
	// nil: a native tool is available.
	if n := clipboardNotice(clipboard.Availability{Available: true, Tool: "xclip"}, "/k"); n != nil {
		t.Errorf("no notice when a tool is available, got %+v", n)
	}
	// nil: headless/SSH (nothing to install) — OSC 52 is expected there, so a
	// "missing tool" nag would be a false positive.
	if n := clipboardNotice(clipboard.Availability{}, "/k"); n != nil {
		t.Errorf("no notice when there is nothing to install, got %+v", n)
	}
}

func TestClipboardNoticeInstallHintMatchesSession(t *testing.T) {
	x := clipboardNotice(clipboard.Availability{Session: "x11", Install: "xclip"}, "/k")
	if !strings.Contains(strings.Join(x.detail, "\n"), "apt install xclip") {
		t.Errorf("x11 notice must suggest xclip, detail=%v", x.detail)
	}
	w := clipboardNotice(clipboard.Availability{Session: "wayland", Install: "wl-clipboard"}, "/k")
	if !strings.Contains(strings.Join(w.detail, "\n"), "wl-clipboard") {
		t.Errorf("wayland notice must suggest wl-clipboard, detail=%v", w.detail)
	}
}

func TestRepoHealthMsgBuildsClipboardNotice(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: model.RepoHealth{GitCommonDir: "/k"}, clipAvail: x11NoToolAvail()})
	m = nm.(Model)
	if len(m.notices) != 1 || m.notices[0].id != noticeClipboard {
		t.Fatalf("notices = %+v, want the clipboard notice", m.notices)
	}
	if !m.noticesUnread {
		t.Fatal("a fresh clipboard notice must start unread (blinking)")
	}
}

func TestClipboardNoticePersistedDismissalFilters(t *testing.T) {
	m, st := noticeTestModel(t)
	if err := st.DismissNotice("/k", noticeClipboard); err != nil {
		t.Fatal(err)
	}
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: model.RepoHealth{GitCommonDir: "/k"}, clipAvail: x11NoToolAvail()})
	if got := nm.(Model).notices; len(got) != 0 {
		t.Fatalf("persisted dismissal must filter the clipboard notice, got %+v", got)
	}
}

func TestNoClipboardNoticeWhenToolAvailable(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: model.RepoHealth{GitCommonDir: "/k"}, clipAvail: clipboard.Availability{Available: true, Tool: "xclip"}})
	if got := nm.(Model).notices; len(got) != 0 {
		t.Fatalf("no clipboard notice when a tool is available, got %+v", got)
	}
}

func TestNoNoticeWhenRepoSmallOrGraphPresentOrConfigSet(t *testing.T) {
	m, _ := noticeTestModel(t)
	small := bigRepoHealth()
	small.PackBytes = bigRepoPackBytes - 1
	graphed := bigRepoHealth()
	graphed.HasCommitGraph = true
	configured := bigRepoHealth()
	configured.WriteCommitGraphSet = true
	for name, h := range map[string]model.RepoHealth{"small": small, "graphed": graphed, "configured": configured} {
		nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: h})
		if got := nm.(Model).notices; len(got) != 0 {
			t.Fatalf("%s: notices = %+v, want none", name, got)
		}
	}
}

func TestStaleHealthResultDropped(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen - 1, health: bigRepoHealth()})
	if got := nm.(Model).notices; len(got) != 0 {
		t.Fatalf("stale gen must be dropped, got %+v", got)
	}
}

func TestPersistedDismissalFiltersNotice(t *testing.T) {
	m, st := noticeTestModel(t)
	h := bigRepoHealth()
	if err := st.DismissNotice(h.GitCommonDir, noticeCommitGraph); err != nil {
		t.Fatal(err)
	}
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: h})
	if got := nm.(Model).notices; len(got) != 0 {
		t.Fatalf("persisted dismissal must filter the notice, got %+v", got)
	}
}

func TestSessionDismissalSurvivesHealthRereadWithinSession(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	m = nm.(Model)
	m = m.removeNotice(noticeCommitGraph)
	m.noticeSessionDismissed[noticeCommitGraph] = true // what "Not now" records
	nm, _ = m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	if got := nm.(Model).notices; len(got) != 0 {
		t.Fatalf("a session-dismissed notice must not resurrect on a mid-session re-read, got %+v", got)
	}
}

func TestBlinkTickStopsWhenRead(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, cmd := m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("a fresh unread notice must arm the blink tick")
	}
	before := m.blinkOn
	nm, cmd = m.Update(noticeBlinkMsg{gen: m.blinkGen})
	m = nm.(Model)
	if m.blinkOn == before {
		t.Fatal("blink tick must flip the phase while unread")
	}
	if cmd == nil {
		t.Fatal("blink must re-arm while unread")
	}
	m.noticesUnread = false // what opening the dialog does
	_, cmd = m.Update(noticeBlinkMsg{gen: m.blinkGen})
	if cmd != nil {
		t.Fatal("blink must stop re-arming once read")
	}
}

func TestUnreadOnlyOnNewNoticeIds(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	m = nm.(Model)
	m.noticesUnread = false // user opened the dialog (read)
	nm, _ = m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	if nm.(Model).noticesUnread {
		t.Fatal("a re-read carrying the SAME notice ids must not re-blink")
	}
}

// ---- Bug regression: a stale blink-tick lane (superseded arm-gen) must not
// re-arm even while noticesUnread is still true — otherwise a reRoot (or an
// unread→read→new-notice flip within one 800ms window) leaves two ticking
// lanes running forever. ----

func TestStaleBlinkGenDoesNotReArm(t *testing.T) {
	m, _ := noticeTestModel(t)
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	m = nm.(Model)
	if !m.noticesUnread {
		t.Fatal("setup: expected unread after a fresh notice")
	}
	staleGen := m.blinkGen - 1 // the arm-gen from a superseded lane

	before := m.blinkOn
	nm, cmd := m.Update(noticeBlinkMsg{gen: staleGen})
	got := nm.(Model)
	if got.blinkOn != before {
		t.Fatal("a stale-gen blink tick must not flip the phase")
	}
	if cmd != nil {
		t.Fatal("a stale-gen blink tick must not re-arm")
	}
}

// ---- pendingNoticeConfig chain: mirrors push_tip_tags_test.go's
// pendingPushTags chain tests (TestOpFinishedChainsPushTags /
// TestOpFinishedErrorClearsPending / TestAbortedPushDoesNotChainTags). ----

func TestOpFinishedChainsNoticeConfig(t *testing.T) {
	m, _ := noticeTestModel(t)
	m.running = true
	m.pendingNoticeConfig = &engine.SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"}

	u, cmd := m.Update(opFinishedMsg{res: engine.Result{Changed: true}})
	got := u.(Model)

	if got.pendingNoticeConfig != nil {
		t.Fatalf("pendingNoticeConfig = %v after success, want nil", got.pendingNoticeConfig)
	}
	if !got.running {
		t.Fatal("the chained SetGitConfig op must have been started (running=true)")
	}
	driveOp(t, got, cmd) // drain so the goroutine doesn't leak
}

func TestOpFinishedErrorClearsNoticeConfig(t *testing.T) {
	m, _ := noticeTestModel(t)
	m.running = true
	m.pendingNoticeConfig = &engine.SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"}

	u, _ := m.Update(opFinishedMsg{err: errors.New("boom")})
	got := u.(Model)

	if got.running {
		t.Fatal("an errored op must not chain (running must be false)")
	}
	if got.pendingNoticeConfig != nil {
		t.Fatalf("pendingNoticeConfig = %v after error, want nil", got.pendingNoticeConfig)
	}
}

func TestAbortedOpDoesNotChainNoticeConfig(t *testing.T) {
	m, _ := noticeTestModel(t)
	m.running = true
	m.pendingNoticeConfig = &engine.SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"}

	// Changed:false, err:nil — simulates an aborted/cancelled op.
	u, _ := m.Update(opFinishedMsg{res: engine.Result{Changed: false}})
	got := u.(Model)

	if got.running {
		t.Fatal("aborted op must NOT chain SetGitConfig (running=true means it did)")
	}
	if got.pendingNoticeConfig != nil {
		t.Fatalf("pendingNoticeConfig = %v after abort, want nil", got.pendingNoticeConfig)
	}
}

// ---- refreshHealthAfterOp: a health re-read after the commit-graph/config
// fix op (incl. its chain) finishes, so notices and the Settings
// Commit-graph label reflect the new state instead of inviting a second
// heavy write. ----

func TestOpFinishedAfterChainRefreshesHealth(t *testing.T) {
	m, _ := noticeTestModel(t)
	m.running = true
	m.refreshHealthAfterOp = true
	// No pendingNoticeConfig: this is the FINAL op of the chain finishing.
	nm, cmd := m.Update(opFinishedMsg{res: engine.Result{Changed: true}})
	m = nm.(Model)
	if m.refreshHealthAfterOp {
		t.Fatal("flag must clear once consumed")
	}
	if cmd == nil {
		t.Fatal("a health re-read must be dispatched after the fix op completes")
	}
}

func TestChainHopKeepsHealthRefreshArmed(t *testing.T) {
	m, _ := noticeTestModel(t)
	m.running = true
	m.refreshHealthAfterOp = true
	m.pendingNoticeConfig = &engine.SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"}
	nm, cmd := m.Update(opFinishedMsg{res: engine.Result{Changed: true}})
	m = nm.(Model)
	if !m.refreshHealthAfterOp {
		t.Fatal("the first hop must keep the flag armed for the chained op's finish")
	}
	if !m.running {
		t.Fatal("the chained op must have started")
	}
	m = driveOp(t, m, cmd)
	if m.refreshHealthAfterOp {
		t.Fatal("flag must clear after the chained op finishes")
	}
}

// ---- reRoot resets notice state and drops stale health: mirrors
// push_tip_tags_test.go's TestReRootBumpsCheckGen. ----

func TestReRootResetsNoticeStateAndDropsStaleHealth(t *testing.T) {
	m := footerModel()
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: bigRepoHealth()})
	m = nm.(Model)
	if len(m.notices) != 1 {
		t.Fatalf("setup: notices = %+v, want 1", m.notices)
	}
	oldGen := m.noticeGen
	m.pendingNoticeConfig = &engine.SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"}

	updated, _ := m.reRoot(t.TempDir())
	got := updated.(Model)

	if got.notices != nil {
		t.Fatalf("notices = %+v after reRoot, want nil", got.notices)
	}
	if got.noticesUnread {
		t.Fatal("noticesUnread must be false after reRoot")
	}
	if got.pendingNoticeConfig != nil {
		t.Fatal("pendingNoticeConfig must be cleared after reRoot")
	}
	if got.noticeGen <= oldGen {
		t.Fatalf("noticeGen = %d after reRoot, want > %d (bumped)", got.noticeGen, oldGen)
	}

	// A stale health result (carrying the OLD gen) arriving after reRoot must
	// be dropped, not resurrect a notice for the new repo.
	nm2, _ := got.Update(repoHealthMsg{gen: oldGen, health: bigRepoHealth()})
	if stale := nm2.(Model).notices; stale != nil {
		t.Fatalf("stale-gen health after reRoot must be dropped, got %+v", stale)
	}
}
