package tui

import (
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// ---- tagsAtCommit ----

func TestTagsAtCommitReturnsMatchingTags(t *testing.T) {
	tags := []model.Tag{
		{Name: "v1.0.0", Target: "abc1234"},
		{Name: "v2.0.0", Target: "def5678"},
	}
	got := tagsAtCommit(tags, "abc1234")
	if len(got) != 1 || got[0].Name != "v1.0.0" {
		t.Fatalf("tagsAtCommit = %v, want [v1.0.0]", got)
	}
}

func TestTagsAtCommitEmptyHashReturnsNil(t *testing.T) {
	tags := []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	if got := tagsAtCommit(tags, ""); got != nil {
		t.Fatalf("tagsAtCommit with empty hash = %v, want nil", got)
	}
}

func TestTagsAtCommitNoMatchReturnsNil(t *testing.T) {
	tags := []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	if got := tagsAtCommit(tags, "zzz9999"); got != nil {
		t.Fatalf("tagsAtCommit with no match = %v, want nil", got)
	}
}

// ---- currentBranchTipHash ----

func TestCurrentBranchTipHashFound(t *testing.T) {
	m := footerModel()
	m.branches = []model.Branch{
		{Name: "main", IsHead: true, Hash: "abc1234"},
		{Name: "feat/x", Hash: "def5678"},
	}
	m.status = model.WorkingTreeStatus{Branch: "main"}
	if got := m.currentBranchTipHash(); got != "abc1234" {
		t.Fatalf("currentBranchTipHash = %q, want abc1234", got)
	}
}

func TestCurrentBranchTipHashNotFound(t *testing.T) {
	m := Model{}
	if got := m.currentBranchTipHash(); got != "" {
		t.Fatalf("currentBranchTipHash on empty model = %q, want empty", got)
	}
}

// ---- pushTagCheckMsg: no tip tags → no remote check, straight push (startPush fast path) ----

func TestStartPushNoTipTagsSkipsCheck(t *testing.T) {
	m := footerModel()
	m.branches = []model.Branch{{Name: "main", IsHead: true, Hash: "abc1234"}}
	m.status = model.WorkingTreeStatus{Branch: "main"}
	m.tags = nil // no tags

	// startPush must go straight to startOp (running=true), no pushTagCheckCmd
	mm, cmd := m.startPush()
	if !mm.(Model).running {
		t.Fatal("startPush with no tip tags must start the push op directly (running=true)")
	}
	if cmd == nil {
		t.Fatal("startPush returned nil cmd")
	}
}

// ---- pushTagCheckMsg: all tags already on remote → straight push ----

func TestPushTagCheckMsgAllAlreadyPushed(t *testing.T) {
	m := footerModel()
	m.branches = []model.Branch{{Name: "main", IsHead: true, Hash: "abc1234"}}
	m.status = model.WorkingTreeStatus{Branch: "main"}
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	m.pushCheckGen = 1
	tipTags := tagsAtCommit(m.tags, "abc1234")

	msg := pushTagCheckMsg{
		gen:       1,
		tipTags:   tipTags,
		remoteSet: map[string]bool{"v1.0.0": true}, // already there
	}
	u, _ := m.Update(msg)
	got := u.(Model)
	if got.modal != nil {
		t.Fatal("all tags on remote: must not open a modal")
	}
	if !got.running {
		t.Fatal("all tags on remote: must start the push op directly")
	}
}

// ---- pushTagCheckMsg: unpushed tag → modal opens ----

func TestPushTagCheckMsgUnpushedTagOpensModal(t *testing.T) {
	m := footerModel()
	m.branches = []model.Branch{{Name: "main", IsHead: true, Hash: "abc1234"}}
	m.status = model.WorkingTreeStatus{Branch: "main"}
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	m.pushCheckGen = 1
	tipTags := tagsAtCommit(m.tags, "abc1234")

	msg := pushTagCheckMsg{
		gen:       1,
		tipTags:   tipTags,
		remoteSet: map[string]bool{}, // v1.0.0 not on remote
	}
	u, _ := m.Update(msg)
	got := u.(Model)
	if got.modal == nil {
		t.Fatal("unpushed tip tag: modal must open")
	}
	if got.modal.req.ID != "push-with-tags" {
		t.Fatalf("modal ID = %q, want push-with-tags", got.modal.req.ID)
	}
	opts := got.modal.req.Options
	if len(opts) != 3 || opts[0] != "Push branch + tags" || opts[1] != "Push branch only" || opts[2] != "Cancel" {
		t.Fatalf("modal options = %v, want [Push branch + tags, Push branch only, Cancel]", opts)
	}
}

// ---- pushTagCheckMsg: error / nil remoteSet → straight push ----

func TestPushTagCheckMsgErrorSkipsTagCheck(t *testing.T) {
	m := footerModel()
	m.branches = []model.Branch{{Name: "main", IsHead: true, Hash: "abc1234"}}
	m.status = model.WorkingTreeStatus{Branch: "main"}
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	m.pushCheckGen = 1
	tipTags := tagsAtCommit(m.tags, "abc1234")

	msg := pushTagCheckMsg{
		gen:       1,
		tipTags:   tipTags,
		remoteSet: nil, // nil = timeout / error
		err:       errors.New("context deadline exceeded"),
	}
	u, _ := m.Update(msg)
	got := u.(Model)
	if got.modal != nil {
		t.Fatal("timeout/error: must not open a modal")
	}
	if !got.running {
		t.Fatal("timeout/error: must start the push op directly (no hang)")
	}
}

func TestPushTagCheckMsgNilRemoteSetNoError(t *testing.T) {
	m := footerModel()
	m.branches = []model.Branch{{Name: "main", IsHead: true, Hash: "abc1234"}}
	m.status = model.WorkingTreeStatus{Branch: "main"}
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	m.pushCheckGen = 1
	tipTags := tagsAtCommit(m.tags, "abc1234")

	msg := pushTagCheckMsg{
		gen:       1,
		tipTags:   tipTags,
		remoteSet: nil, // nil without error → also skip
	}
	u, _ := m.Update(msg)
	got := u.(Model)
	if got.modal != nil {
		t.Fatal("nil remoteSet: must not open a modal")
	}
	if !got.running {
		t.Fatal("nil remoteSet: must start the push op directly")
	}
}

// ---- pushTagCheckMsg: stale gen → ignored ----

func TestPushTagCheckMsgStaleGenIgnored(t *testing.T) {
	m := footerModel()
	m.branches = []model.Branch{{Name: "main", IsHead: true, Hash: "abc1234"}}
	m.status = model.WorkingTreeStatus{Branch: "main"}
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	m.pushCheckGen = 3 // current gen
	tipTags := tagsAtCommit(m.tags, "abc1234")

	msg := pushTagCheckMsg{
		gen:       1, // stale
		tipTags:   tipTags,
		remoteSet: map[string]bool{},
	}
	u, _ := m.Update(msg)
	got := u.(Model)
	if got.modal != nil {
		t.Fatal("stale gen: must not open a modal")
	}
	if got.running {
		t.Fatal("stale gen: must not start any op")
	}
}

// ---- remoteTagNames refreshed from a successful check ----

func TestPushTagCheckMsgRefreshesRemoteTagNames(t *testing.T) {
	m := footerModel()
	m.branches = []model.Branch{{Name: "main", IsHead: true, Hash: "abc1234"}}
	m.status = model.WorkingTreeStatus{Branch: "main"}
	m.tags = []model.Tag{
		{Name: "v1.0.0", Target: "abc1234"},
		{Name: "v2.0.0", Target: "abc1234"},
	}
	m.pushCheckGen = 1
	m.remoteTagNames = map[string]bool{"old": true}
	tipTags := tagsAtCommit(m.tags, "abc1234")

	// Both v1.0.0 and v2.0.0 are already on the remote → straight push, but
	// the handler must also update m.remoteTagNames to the fresh set.
	freshSet := map[string]bool{"v1.0.0": true, "v2.0.0": true}
	msg := pushTagCheckMsg{gen: 1, tipTags: tipTags, remoteSet: freshSet}
	u, _ := m.Update(msg)
	got := u.(Model)
	if !got.remoteTagNames["v1.0.0"] || !got.remoteTagNames["v2.0.0"] {
		t.Fatal("pushTagCheckMsg must refresh remoteTagNames with the fresh set")
	}
	if got.remoteTagNames["old"] {
		t.Fatal("pushTagCheckMsg must replace remoteTagNames (old key should be gone)")
	}
}

// ---- modal "Push branch + tags" sets pendingPushTags ----

func TestModalPushBranchAndTagsSetsPending(t *testing.T) {
	m := footerModel()
	m.branches = []model.Branch{{Name: "main", IsHead: true, Hash: "abc1234"}}
	m.status = model.WorkingTreeStatus{Branch: "main"}
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	m.pushCheckGen = 1
	tipTags := tagsAtCommit(m.tags, "abc1234")

	// Open the modal via the check message
	msg := pushTagCheckMsg{gen: 1, tipTags: tipTags, remoteSet: map[string]bool{}}
	u, _ := m.Update(msg)
	m = u.(Model)
	if m.modal == nil {
		t.Fatal("expected modal to be open")
	}

	// Resolve with "Push branch + tags"
	rm, _ := m.resolveModal("Push branch + tags")
	got := rm.(Model)
	if len(got.pendingPushTags) != 1 || got.pendingPushTags[0] != "v1.0.0" {
		t.Fatalf("pendingPushTags = %v after resolving Push branch + tags, want [v1.0.0]", got.pendingPushTags)
	}
}

// ---- modal "Push branch only" does not set pendingPushTags ----

func TestModalPushBranchOnlyNoPending(t *testing.T) {
	m := footerModel()
	m.branches = []model.Branch{{Name: "main", IsHead: true, Hash: "abc1234"}}
	m.status = model.WorkingTreeStatus{Branch: "main"}
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	m.pushCheckGen = 1
	tipTags := tagsAtCommit(m.tags, "abc1234")

	msg := pushTagCheckMsg{gen: 1, tipTags: tipTags, remoteSet: map[string]bool{}}
	u, _ := m.Update(msg)
	m = u.(Model)

	rm, _ := m.resolveModal("Push branch only")
	got := rm.(Model)
	if len(got.pendingPushTags) != 0 {
		t.Fatalf("pendingPushTags = %v after Push branch only, want nil/empty", got.pendingPushTags)
	}
}

// ---- opFinishedMsg chain: branch push success with pendingPushTags ----

func TestOpFinishedChainsPushTags(t *testing.T) {
	m := footerModel()
	m.running = true
	m.pendingPushTags = []string{"v1.0.0"}

	u, _ := m.Update(opFinishedMsg{res: engine.Result{Summary: "pushed", Changed: true}})
	got := u.(Model)

	// pendingPushTags must be cleared
	if len(got.pendingPushTags) != 0 {
		t.Fatalf("pendingPushTags = %v after success, want nil", got.pendingPushTags)
	}
	// pendingRemoteTagAdds must be set to the captured tags
	if len(got.pendingRemoteTagAdds) != 1 || got.pendingRemoteTagAdds[0] != "v1.0.0" {
		t.Fatalf("pendingRemoteTagAdds = %v after branch push success, want [v1.0.0]", got.pendingRemoteTagAdds)
	}
	// running must be true (PushTags op was chained)
	if !got.running {
		t.Fatal("PushTags must have been chained (running=true)")
	}
}

// ---- opFinishedMsg error: clears pendingPushTags and pendingRemoteTagAdds ----

func TestOpFinishedErrorClearsPending(t *testing.T) {
	m := footerModel()
	m.running = true
	m.pendingPushTags = []string{"v1.0.0"}
	m.pendingRemoteTagAdds = []string{"v1.0.0"}

	u, _ := m.Update(opFinishedMsg{err: errors.New("rejected")})
	got := u.(Model)
	if len(got.pendingPushTags) != 0 {
		t.Fatalf("pendingPushTags = %v after error, want nil", got.pendingPushTags)
	}
	if len(got.pendingRemoteTagAdds) != 0 {
		t.Fatalf("pendingRemoteTagAdds = %v after error, want nil", got.pendingRemoteTagAdds)
	}
}

// ---- No re-chain: PushTags success with pendingPushTags=nil does not re-chain ----

func TestOpFinishedPushTagsSuccessNoReChain(t *testing.T) {
	m := footerModel()
	m.running = true
	m.pendingPushTags = nil // already cleared (this is the PushTags op completing)
	m.pendingRemoteTagAdds = []string{"v1.0.0"}

	u, _ := m.Update(opFinishedMsg{res: engine.Result{Summary: "pushed tags"}})
	got := u.(Model)

	// pendingRemoteTagAdds must be cleared (applied)
	if len(got.pendingRemoteTagAdds) != 0 {
		t.Fatalf("pendingRemoteTagAdds = %v after PushTags success, want nil", got.pendingRemoteTagAdds)
	}
	// running must be false (no chaining)
	if got.running {
		t.Fatal("PushTags success must not re-chain (running=false)")
	}
}

// ---- Optimistic: applyPendingRemoteTag drains pendingRemoteTagAdds ----

func TestApplyPendingRemoteTagAdds(t *testing.T) {
	m := Model{pendingRemoteTagAdds: []string{"v1.0.0", "v2.0.0"}}
	m = m.applyPendingRemoteTag()
	if !m.remoteTagNames["v1.0.0"] || !m.remoteTagNames["v2.0.0"] {
		t.Fatal("applyPendingRemoteTag must add all tags in pendingRemoteTagAdds to remoteTagNames")
	}
	if len(m.pendingRemoteTagAdds) != 0 {
		t.Fatal("applyPendingRemoteTag must clear pendingRemoteTagAdds")
	}
}

func TestApplyPendingRemoteTagAddsLazyInit(t *testing.T) {
	m := Model{remoteTagNames: nil, pendingRemoteTagAdds: []string{"v3.0.0"}}
	m = m.applyPendingRemoteTag()
	if m.remoteTagNames == nil || !m.remoteTagNames["v3.0.0"] {
		t.Fatal("applyPendingRemoteTag must lazy-init remoteTagNames for pendingRemoteTagAdds")
	}
}

// ---- Bug-1 regression: aborted/cancelled push must NOT chain tag push ----

// A push that the user aborts returns Result{Changed:false}, nil (err==nil but
// nothing actually happened). pendingPushTags must be cleared WITHOUT chaining
// engine.PushTags — otherwise we'd upload tags the user explicitly skipped.
func TestAbortedPushDoesNotChainTags(t *testing.T) {
	m := footerModel()
	m.running = true
	m.pendingPushTags = []string{"v1.0.0"}

	// Changed:false, err:nil — simulates an aborted/cancelled push.
	u, _ := m.Update(opFinishedMsg{res: engine.Result{Summary: "push cancelled", Changed: false}})
	got := u.(Model)

	// pendingPushTags must be cleared
	if len(got.pendingPushTags) != 0 {
		t.Fatalf("pendingPushTags = %v after abort, want nil", got.pendingPushTags)
	}
	// PushTags must NOT have been chained: running must be false
	if got.running {
		t.Fatal("aborted push must NOT chain PushTags (running=true means it did)")
	}
	// pendingRemoteTagAdds must remain empty
	if len(got.pendingRemoteTagAdds) != 0 {
		t.Fatalf("pendingRemoteTagAdds = %v after abort, want nil", got.pendingRemoteTagAdds)
	}
}

// ---- Bug-2 regression: reRoot bumps pushCheckGen and clears push-pending state ----

func TestReRootBumpsCheckGen(t *testing.T) {
	m := footerModel()
	m.pushCheckGen = 2
	m.pendingPushTags = []string{"v1.0.0"}
	m.pendingRemoteTagAdds = []string{"v1.0.0"}

	updated, _ := m.reRoot(t.TempDir())
	got := updated.(Model)

	if got.pushCheckGen <= 2 {
		t.Fatalf("pushCheckGen = %d after reRoot, want > 2 (bumped)", got.pushCheckGen)
	}
	if len(got.pendingPushTags) != 0 {
		t.Fatalf("pendingPushTags = %v after reRoot, want nil", got.pendingPushTags)
	}
	if len(got.pendingRemoteTagAdds) != 0 {
		t.Fatalf("pendingRemoteTagAdds = %v after reRoot, want nil", got.pendingRemoteTagAdds)
	}
}

// ---- Optimistic ordering: branch push sets pendingRemoteTagAdds, PushTags success adds them ----

func TestOptimisticOrderingBranchThenTags(t *testing.T) {
	// Phase 1: branch Push succeeds with pendingPushTags set
	m := footerModel()
	m.running = true
	m.pendingPushTags = []string{"v1.0.0"}
	m.remoteTagNames = map[string]bool{}

	u, _ := m.Update(opFinishedMsg{res: engine.Result{Summary: "pushed", Changed: true}})
	m = u.(Model)

	// After branch push success, remoteTagNames must NOT yet contain "v1.0.0"
	// (applyPendingRemoteTag ran but pendingRemoteTagAdds was set AFTER it)
	if m.remoteTagNames["v1.0.0"] {
		t.Fatal("v1.0.0 must not be in remoteTagNames until PushTags succeeds")
	}
	// pendingRemoteTagAdds must be set
	if len(m.pendingRemoteTagAdds) != 1 || m.pendingRemoteTagAdds[0] != "v1.0.0" {
		t.Fatalf("pendingRemoteTagAdds = %v after branch push, want [v1.0.0]", m.pendingRemoteTagAdds)
	}

	// Phase 2: PushTags succeeds
	u, _ = m.Update(opFinishedMsg{res: engine.Result{Summary: "pushed tags"}})
	m = u.(Model)

	// Now v1.0.0 must be in remoteTagNames (applyPendingRemoteTag ran after PushTags success)
	if !m.remoteTagNames["v1.0.0"] {
		t.Fatal("v1.0.0 must be in remoteTagNames after PushTags success")
	}
	if len(m.pendingRemoteTagAdds) != 0 {
		t.Fatal("pendingRemoteTagAdds must be empty after PushTags success")
	}
}
