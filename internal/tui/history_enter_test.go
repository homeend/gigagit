package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/domain"
)

// enter in the history view promotes the selected commit's diff onto the layer
// stack above the history view. The diff is the top layer and owns key routing;
// esc pops the diff and returns to the history view beneath.
func TestHistoryEnterOpensFullScreenDiff(t *testing.T) {
	m := Model{width: 100, height: 30}
	h := histFixture()
	m = m.pushLayer(h)

	m, cmd := h.update(m, keyMsg("enter"))

	if m.diffLayer() == nil {
		t.Fatal("enter should open the full-screen diff on the layer stack")
	}
	if m.topLayer() == nil {
		t.Fatal("the diff must be on the stack (topLayer) to own key routing")
	}
	if m.diffTag == "" {
		t.Fatal("diffTag must be set so the diffMsg is not dropped")
	}
	if cmd == nil {
		t.Fatal("enter should dispatch the diff load")
	}
}

// The full-screen loader must yield a diffMsg (lands in m.diffLayer()), not the
// right-pane historyDiffMsg, and carry the matching tag so the handler keeps it.
func TestHistoryEnterDiffLoadYieldsDiffMsg(t *testing.T) {
	_, repo := newRepoDir(t)
	m := Model{width: 100, height: 30, svc: domain.New(repo)}
	h := histFixture() // fake hashes → the load errors, but still returns a diffMsg
	m = m.pushLayer(h)

	m, cmd := h.update(m, keyMsg("enter"))

	msg, ok := cmd().(diffMsg)
	if !ok {
		t.Fatalf("full-screen history load must yield a diffMsg, got %T", cmd())
	}
	if msg.tag != m.diffTag {
		t.Errorf("diffMsg tag %q must match m.diffTag %q (else the handler drops it)", msg.tag, m.diffTag)
	}
}
