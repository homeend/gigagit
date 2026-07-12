package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// Manual remoteTagsMsg stores the set.
func TestRemoteTagsMsgStoresSet(t *testing.T) {
	m := Model{}
	u, _ := m.Update(remoteTagsMsg{names: map[string]bool{"v1": true}, manual: true})
	m = u.(Model)
	if !m.remoteTagNames["v1"] {
		t.Fatal("manual remoteTagsMsg should store the name set")
	}
}

// Manual error surfaces on the status line and leaves the set unchanged.
func TestRemoteTagsMsgManualErrorStatus(t *testing.T) {
	m := Model{remoteTagNames: map[string]bool{"old": true}}
	u, _ := m.Update(remoteTagsMsg{err: errTestRemote, manual: true})
	m = u.(Model)
	if m.statusMsg == "" {
		t.Fatal("manual error should set a status message")
	}
	if !m.remoteTagNames["old"] {
		t.Fatal("error must not clear the existing set")
	}
}

// Optimistic add on PushTag success; remove on DeleteRemoteTag success.
func TestOptimisticRemoteTagAdd(t *testing.T) {
	m := Model{pendingRemoteTagSet: "v9"}
	m = m.applyPendingRemoteTag() // success-path helper
	if !m.remoteTagNames["v9"] {
		t.Fatal("push success should add the tag to the remote set")
	}
}
func TestOptimisticRemoteTagRemove(t *testing.T) {
	m := Model{remoteTagNames: map[string]bool{"v9": true}, pendingRemoteTagUnset: "v9"}
	m = m.applyPendingRemoteTag()
	if m.remoteTagNames["v9"] {
		t.Fatal("delete-remote success should drop the tag from the remote set")
	}
}

// Background remoteTagsMsg must free the single lane.
func TestRemoteTagsBackgroundFreesLane(t *testing.T) {
	m := Model{bgBusy: true, bgActiveItem: remoteTagsItem}
	u, _ := m.Update(remoteTagsMsg{names: map[string]bool{"v1": true}, manual: false})
	if u.(Model).bgBusy {
		t.Fatal("background remoteTagsMsg must free bgBusy")
	}
}

// Background error must also free the lane.
func TestRemoteTagsBackgroundErrorFreesLane(t *testing.T) {
	m := Model{bgBusy: true, bgActiveItem: remoteTagsItem}
	u, _ := m.Update(remoteTagsMsg{err: errTestRemote, manual: false})
	if u.(Model).bgBusy {
		t.Fatal("background error remoteTagsMsg must also free bgBusy")
	}
}

// reRoot must clear remoteTagNames so tag names from the old repo don't
// bleed into the new one and produce false ▲ markers.
func TestReRootClearsRemoteTagNames(t *testing.T) {
	m := Model{remoteTagNames: map[string]bool{"v1.0.0": true, "latest": true}}
	updated, _ := m.reRoot(t.TempDir())
	if got := updated.(Model).remoteTagNames; got != nil {
		t.Fatalf("remoteTagNames = %v after reRoot, want nil", got)
	}
}

// Stale remoteTagsMsg (gen != loadGen) must not overwrite remoteTagNames and
// must still free the background lane if it was occupied by remoteTagsItem.
func TestRemoteTagsMsgStaleGenDropped(t *testing.T) {
	oldNames := map[string]bool{"old": true}
	m := Model{
		remoteTagNames: oldNames,
		bgBusy:         true,
		bgActiveItem:   remoteTagsItem,
		loadGen:        2, // current gen after a repo switch
	}
	// Send a stale background message with gen=1 (captured before the switch).
	u, _ := m.Update(remoteTagsMsg{names: map[string]bool{"new": true}, manual: false, gen: 1})
	got := u.(Model)
	// Lane must be freed even though the message is stale.
	if got.bgBusy {
		t.Fatal("stale remoteTagsMsg must still free bgBusy")
	}
	// Names must NOT be overwritten.
	if got.remoteTagNames["new"] {
		t.Fatal("stale remoteTagsMsg must not overwrite remoteTagNames with old-repo names")
	}
	if !got.remoteTagNames["old"] {
		t.Fatal("stale remoteTagsMsg must leave existing remoteTagNames unchanged")
	}
}

// Current-gen remoteTagsMsg (gen == loadGen) must still apply names normally.
func TestRemoteTagsMsgCurrentGenApplied(t *testing.T) {
	m := Model{loadGen: 2}
	u, _ := m.Update(remoteTagsMsg{names: map[string]bool{"v2": true}, manual: false, gen: 2})
	got := u.(Model)
	if !got.remoteTagNames["v2"] {
		t.Fatal("current-gen remoteTagsMsg must apply names")
	}
}

var errTestRemote = errString("boom")

type errString string

func (e errString) Error() string { return string(e) }

func TestPushTagCheckDroppedWhenModalOpen(t *testing.T) {
	m := footerModel()
	m.pushCheckGen = 3
	m.modal = &decisionState{req: engine.DecisionRequest{
		ID: "other", Prompt: "unrelated?", Options: []string{"x", "y"}}}
	// One unpushed tip tag: without the guard this opens the push-with-tags
	// modal (clobbering "other") — or worse, starts a push under the dialog.
	msg := pushTagCheckMsg{gen: 3, tipTags: []model.Tag{{Name: "v9"}},
		remoteSet: map[string]bool{}}
	mm, cmd := m.Update(msg)
	m = mm.(Model)
	if m.modal == nil || m.modal.req.ID != "other" {
		t.Fatal("an open modal must not be clobbered by a returning push-tag check")
	}
	if m.running || cmd != nil {
		t.Fatalf("no push may start under an open dialog (running=%v cmd=%v)", m.running, cmd)
	}
	if !strings.Contains(m.statusMsg, "press P again") {
		t.Fatalf("the drop must be visible; statusMsg = %q", m.statusMsg)
	}
}

func TestPushTagCheckDroppedWhenOpRunning(t *testing.T) {
	m := footerModel()
	m.pushCheckGen = 3
	m.running = true
	// Zero unpushed tags: without the guard this is the dangerous path that
	// calls startOp directly under the already-running op.
	msg := pushTagCheckMsg{gen: 3, tipTags: []model.Tag{{Name: "v9"}},
		remoteSet: map[string]bool{"v9": true}}
	mm, cmd := m.Update(msg)
	m = mm.(Model)
	if cmd != nil {
		t.Fatal("no push may start under a running op")
	}
	if !strings.Contains(m.statusMsg, "press P again") {
		t.Fatalf("the drop must be visible; statusMsg = %q", m.statusMsg)
	}
}
