package tui

import (
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// Second post-merge hunt of the fast worktree switch (2026-10-10): the
// operation boundary, the keys as a state machine, the replay queue and
// the shared state.

// An async step between an op key and its startOp (the pre-push remote-tag
// check) is addressed to the worktree it was asked from: landing after a
// swap it must not push the worktree now on screen.
func TestPushTagCheckAfterASwapIsDropped(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	svcA := m.svc
	m.pushCheckGen++
	gen := m.pushCheckGen
	m, _ = viewedOther(t, m)
	mm, cmd := m.Update(pushTagCheckMsg{gen: gen, svc: svcA, remoteSet: map[string]bool{}})
	m = mm.(Model)
	if m.running || cmd != nil || m.modal != nil {
		t.Fatalf("a push check from the worktree that left the screen must not start a push here (running=%v cmd=%v modal=%v)", m.running, cmd, m.modal)
	}
	if m.statusMsg == "" {
		t.Fatal("the dropped push must be said")
	}
}

// Likewise the cherry-pick probe of the bookmark switcher (a parkable
// popup): its confirm must not open over, nor pick into, another worktree.
func TestPickProbeAfterASwapIsDropped(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	svcA := m.svc
	m.pickGen++
	gen := m.pickGen
	m, _ = viewedOther(t, m)
	mm, _ := m.Update(pickProbeMsg{gen: gen, svc: svcA, target: pickTarget{sha: "abc"},
		line: model.LogLine{Hash: "abc1234", Subject: "s"}, found: true})
	m = mm.(Model)
	if m.modal != nil {
		t.Fatal("a probe from the worktree that left the screen must not open its confirm here")
	}
}

// Every message whose handler reaches startOp (or a confirm that starts
// one) after an async step names the worktree it was asked from — the
// Service it was read through, or a slot stamp — and its handler drops a
// mismatch: between the key and the op the panels may have swapped, and
// the op would run in the worktree on screen THEN. The range loads had
// this from the first; the push check and the pick probe did not.
func TestAsyncOpStartersNameTheirWorktree(t *testing.T) {
	t.Parallel()
	for _, msg := range []any{
		pushTagCheckMsg{}, pickProbeMsg{},
		rebaseRangeLoadedMsg{}, squashRangeLoadedMsg{}, dropRangeLoadedMsg{}, irebaseLoadedMsg{},
		amendPrefillMsg{}, conflictFileLoadedMsg{}, stageHunksLoadedMsg{}, unstageHunksLoadedMsg{}, reviewTargetReadyMsg{},
	} {
		if _, ok := msg.(slotMsg); ok {
			continue
		}
		if f, ok := reflect.TypeOf(msg).FieldByName("svc"); !ok || f.Type != reflect.TypeOf((*domain.Service)(nil)) {
			t.Errorf("%T carries neither a slot stamp nor the *domain.Service it was asked through", msg)
		}
	}
}

// The editor's exit re-reads the status with no op of its own; its result
// must not clear the busy flag of an op started meanwhile (a pull pressed
// before the slow read landed): with it cleared the swap refusal lifted
// mid-op, and a second op key overwrote the first op's channel.
func TestEditorStatusReadKeepsARunningOpBusy(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.running, m.opName = true, "pull"
	mm, _ := m.Update(m.reloadStatusCmd("")())
	m = mm.(Model)
	if !m.running || m.opName != "pull" {
		t.Fatalf("the editor's status read cleared the running op (running=%v opName=%q)", m.running, m.opName)
	}
	if m.switchRefusalBy(true) == "" {
		t.Fatal("alt+w must stay refused while the op runs")
	}
}

// A staging round's own result does clear it: that round set it.
func TestStagingResultClearsBusy(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.running = true
	mm, _ := m.Update(statusRefreshedMsg{svc: m.svc, staging: true, status: m.status})
	if mm.(Model).running {
		t.Fatal("a staging round's result must clear the busy flag it set")
	}
}
