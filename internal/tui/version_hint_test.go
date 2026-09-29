package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

func versionCmd(hintID string) steer.Command {
	return steer.Command{Cmd: "navigate", Target: &steer.Target{State: "pair", A: tvBase, B: tvOurs}, HintKind: "version", HintID: hintID}
}

// landedVersionModel: a Model whose compare view is already open on the
// pair (what steerNavigatePair leaves behind before navigateLanded runs).
func landedVersionModel(t *testing.T) Model {
	t.Helper()
	m := Model{width: 120, height: 40, linkRepoName: "gigagit"}
	base, _ := model.CommitEndpoint(tvBase)
	ours, _ := model.CommitEndpoint(tvOurs)
	m, _ = m.openCompareFiles(base, ours)
	if m.filesView == nil {
		t.Fatal("fixture: compare view did not open")
	}
	m.filesReturnLayers = nil
	return m
}

func rowsFixture() []model.BranchVersion {
	other := twoBranchVersion()
	other.Ref, other.Unix, other.Op = "refs/gg/versions/main/1753200000-merge", 1753200000, "merge"
	return []model.BranchVersion{other, twoBranchVersion()} // newest first; ours is row 1
}

// The arm parks a load, never a for-each-ref on the Update thread.
func TestVersionHintParksALoadUnderTheHintGeneration(t *testing.T) {
	t.Parallel()
	m := landedVersionModel(t)
	nm, cmd := m.navigateLanded(versionCmd("1753100000-rebase"), "opened")
	if nm.pendingHint == nil || nm.pendingHint.cmd.HintKind != "version" || nm.pendingHint.tag != nm.hintGen {
		t.Fatalf("pendingHint = %+v, want the version hint parked under hintGen %d", nm.pendingHint, nm.hintGen)
	}
	if cmd == nil {
		t.Fatal("the arm must return the load command")
	}
}

// A hit parks the versions popup UNDER the open compare, on the record's
// row: esc on the compare restores it, exactly "popup → enter" in reverse.
func TestVersionHintParksThePopupUnderTheCompareOnTheRow(t *testing.T) {
	t.Parallel()
	m := landedVersionModel(t)
	m, _ = m.navigateLanded(versionCmd("1753100000-rebase"), "opened")
	m, _ = m.versionHintLoaded(versionHintLoadedMsg{gen: m.hintGen, branch: "main", rows: rowsFixture(), idx: 1, found: true})
	if m.pendingHint != nil {
		t.Fatal("the pending hint must be consumed")
	}
	if len(m.filesReturnLayers) != 1 {
		t.Fatalf("filesReturnLayers = %d, want the popup parked", len(m.filesReturnLayers))
	}
	p, ok := m.filesReturnLayers[0].(*versionsPopup)
	if !ok || p.mode != versionsModeVersions || p.branch != "main" || p.sel != 1 || len(p.rows) != 2 {
		t.Fatalf("parked = %+v, want versions mode for main on row 1", p)
	}
	if layerOf[*versionsPopup](m) != nil {
		t.Fatal("the popup must be parked, not drawn over the compare")
	}
}

// The compare was closed before the lookup returned: the popup is pushed
// live instead of parked under nothing.
func TestVersionHintPushesThePopupLiveWhenTheCompareIsGone(t *testing.T) {
	t.Parallel()
	m := landedVersionModel(t)
	m, _ = m.navigateLanded(versionCmd("1753100000-rebase"), "opened")
	m = m.closeFilesView()
	m, _ = m.versionHintLoaded(versionHintLoadedMsg{gen: m.hintGen, branch: "main", rows: rowsFixture(), idx: 1, found: true})
	p := layerOf[*versionsPopup](m)
	if p == nil || p.sel != 1 {
		t.Fatalf("popup = %+v, want it pushed live on row 1", p)
	}
}

func TestVersionHintMissIsANoticeAndTouchesNothing(t *testing.T) {
	t.Parallel()
	m := landedVersionModel(t)
	m, _ = m.navigateLanded(versionCmd("1600000000-rebase"), "opened")
	m, _ = m.versionHintLoaded(versionHintLoadedMsg{gen: m.hintGen, found: false})
	if m.statusMsg != i18n.T("version %s is not recorded here; the link still landed", "1600000000-rebase") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	if len(m.filesReturnLayers) != 0 || layerOf[*versionsPopup](m) != nil {
		t.Fatal("a miss must not park or push anything")
	}
}

func TestVersionHintStaleGenerationIsDropped(t *testing.T) {
	t.Parallel()
	m := landedVersionModel(t)
	m, _ = m.navigateLanded(versionCmd("1753100000-rebase"), "opened")
	m, _ = m.versionHintLoaded(versionHintLoadedMsg{gen: m.hintGen - 1, branch: "main", rows: rowsFixture(), idx: 1, found: true})
	if m.pendingHint == nil || len(m.filesReturnLayers) != 0 {
		t.Fatal("a stale load must neither consume the pending hint nor park anything")
	}
}

var _ tea.Msg = versionHintLoadedMsg{}

// A lookup that outlives pendingHintTTL used to expire silently: the diff
// stayed open with no popup and no word. It is a notice now.
func TestVersionHintExpiryIsANotice(t *testing.T) {
	t.Parallel()
	m := landedVersionModel(t)
	m, _ = m.navigateLanded(versionCmd("1753100000-rebase"), "opened")
	at := m.pendingHint.at
	m, cmd := m.expirePendingHint(at.Add(pendingHintTTL))
	if cmd != nil {
		t.Fatal("a with-address version hint never answers the steer command twice")
	}
	if m.pendingHint != nil {
		t.Fatal("the expired pending must be cleared")
	}
	if want := i18n.T("version lookup timed out; the link still landed"); m.statusMsg != want {
		t.Fatalf("status = %q, want %q", m.statusMsg, want)
	}
	if len(m.filesReturnLayers) != 0 || layerOf[*versionsPopup](m) != nil {
		t.Fatal("an expiry must not park or push anything")
	}
}

// Under the TTL the reveal is still parked: no notice, pending kept.
func TestVersionHintExpiryWaitsOutTheTTL(t *testing.T) {
	t.Parallel()
	m := landedVersionModel(t)
	m, _ = m.navigateLanded(versionCmd("1753100000-rebase"), "opened")
	at := m.pendingHint.at
	m, _ = m.expirePendingHint(at.Add(pendingHintTTL - time.Millisecond))
	if m.pendingHint == nil {
		t.Fatal("the pending must survive until the TTL")
	}
	if m.statusMsg != "" {
		t.Fatalf("status = %q, want none before the TTL", m.statusMsg)
	}
}

// A FAILED lookup (a git error) is not a miss: it must not claim the
// version is unrecorded here.
func TestVersionHintLookupErrorIsItsOwnNotice(t *testing.T) {
	t.Parallel()
	m := landedVersionModel(t)
	m, _ = m.navigateLanded(versionCmd("1753100000-rebase"), "opened")
	m, _ = m.versionHintLoaded(versionHintLoadedMsg{gen: m.hintGen, err: errors.New("boom")})
	if want := i18n.T("could not look up version %s; the link still landed", "1753100000-rebase"); m.statusMsg != want {
		t.Fatalf("status = %q, want %q", m.statusMsg, want)
	}
	if strings.Contains(m.statusMsg, "not recorded") {
		t.Fatalf("status = %q must not read as a miss", m.statusMsg)
	}
	if m.pendingHint != nil || len(m.filesReturnLayers) != 0 || layerOf[*versionsPopup](m) != nil {
		t.Fatal("an error must clear the pending and park or push nothing")
	}
}

// The heartbeat expires the reveal even with steering OFF (steerDir ""):
// the # prompt's pasted link and the --at landing stage the same pending,
// and drainSteer — which used to own the expiry — returns early for them.
func TestVersionHintHeartbeatExpiresWithSteeringOff(t *testing.T) {
	t.Parallel()
	m := landedVersionModel(t)
	m, _ = m.navigateLanded(versionCmd("1753100000-rebase"), "opened")
	if m.steerActive() {
		t.Fatal("fixture: steering must be off for this test")
	}
	m.pendingHint.at = time.Now().Add(-pendingHintTTL - time.Second)
	tm, _ := m.Update(heartbeatMsg{})
	m = tm.(Model)
	if m.pendingHint != nil {
		t.Fatal("the heartbeat must expire the pending without a steer inbox")
	}
	if want := i18n.T("version lookup timed out; the link still landed"); m.statusMsg != want {
		t.Fatalf("status = %q, want %q", m.statusMsg, want)
	}
}
