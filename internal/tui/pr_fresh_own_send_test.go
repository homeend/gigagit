package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

func revalidated(m Model, n, seq int, changed bool) Model {
	nm, _ := m.Update(prRevalidatedMsg{n: n, gen: m.forgeGen, seq: seq, pr: model.PullRequest{Number: n}, commentsChanged: changed})
	return nm.(Model)
}

// Item 10: the change my own send made is not "updated"; a read already in
// flight when the send ended neither shows nor absorbs it.
func TestMyOwnSendIsNotUpdated(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m = revalidated(m, 7, 1, false) // the open's first read
	m.prReadSeq = 2                 // read 2 is in flight
	m.prRevalidateInflight, m.prCommentsInflight = true, true
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7}, engine.Result{Changed: true}, nil)
	m = revalidated(m, 7, 2, false)
	m = revalidated(m, 7, 3, true) // the post-send read: my own comment
	if m.prUpdated == 7 {
		t.Fatal(`my own send marked the PR "updated"`)
	}
	m = revalidated(m, 7, 4, true)
	if m.prUpdated != 7 {
		t.Fatal("a genuine change after it was swallowed")
	}
}

// F1: an aborted send (nothing changed) arms nothing.
func TestAnAbortedSendArmsNothing(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m = revalidated(m, 7, 1, false)
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7}, engine.Result{}, nil)
	if m.prOwnSend != 0 {
		t.Fatal("an abort armed the own-send absorb")
	}
}

// A post-send read dropped because one was running is asked again when that
// one lands.
func TestADroppedPostSendReadIsReasked(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m = revalidated(m, 7, 1, false)
	m.prRevalidateInflight, m.prCommentsInflight = true, true
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7}, engine.Result{Changed: true}, nil)
	if m.prRefreshAgain != 7 {
		t.Fatal("the dropped read was not queued")
	}
	nm, cmd := m.Update(prRevalidatedMsg{n: 7, gen: m.forgeGen, seq: m.prReadSeq, pr: model.PullRequest{Number: 7}})
	if mm := nm.(Model); mm.prRefreshAgain != 0 || cmd == nil || !mm.prRevalidateInflight {
		t.Fatalf("again=%d cmd=%v inflight=%v", mm.prRefreshAgain, cmd != nil, mm.prRevalidateInflight)
	}
}

// A repo switch forgets the old repo's PR freshness.
func TestReRootForgetsPRFreshness(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.prSeen, m.prUpdated, m.prOwnSend, m.prRefreshAgain = 7, 7, 7, 7
	nm, _ := m.reRoot(t.TempDir())
	mm := nm.(Model)
	if mm.prSeen != 0 || mm.prUpdated != 0 || mm.prOwnSend != 0 || mm.prRefreshAgain != 0 || !mm.prOfflineSince.IsZero() {
		t.Fatalf("seen %d updated %d own %d again %d", mm.prSeen, mm.prUpdated, mm.prOwnSend, mm.prRefreshAgain)
	}
}

// Final review I1: a send's mark set while its PR was not on screen dies with
// that PR's next first read — it must not swallow a later genuine update.
func TestAnOwnSendMarkDoesNotOutliveTheView(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m = revalidated(m, 8, 1, false) // PR 8 was the open one
	m.prOwnSend, m.prOwnSendSeq = 7, 1
	m = revalidated(m, 7, 2, false) // PR 7 opens: its first read
	m = revalidated(m, 7, 3, true)  // a colleague's comment
	if m.prUpdated != 7 {
		t.Fatal("a genuine update was swallowed by a stale own-send mark")
	}
}
