package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/repogate"
)

// An op refused because a headless AI task holds the repo (repogate.BusyError)
// opens a dismiss-only notice naming the task instead of leaving only a
// status-line error — the user's "b did nothing" report.
func TestBusyErrorOpensNoticeModal(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.running = true
	nm, _ := m.Update(opFinishedMsg{err: &repogate.BusyError{Holder: "op ConflictAgent", Mode: repogate.Read}})
	rm := nm.(Model)
	if rm.modal == nil || rm.modal.req.ID != "repo-busy" {
		t.Fatalf("expected the repo-busy notice, got %+v", rm.modal)
	}
	if got := rm.modal.req.Options; len(got) != 1 || got[0] != "ok" {
		t.Fatalf("a notice has one dismiss option, got %v", got)
	}
	if rm.running {
		t.Fatal("the refused op is over; running must clear")
	}
	nm2, cmd := rm.resolveModal("ok")
	if cmd != nil || nm2.(Model).modal != nil {
		t.Fatal("dismissing the notice starts nothing and closes it")
	}
}

func TestBusyHolderLabelKnowsTheCaptureOps(t *testing.T) {
	t.Parallel()
	for holder, want := range map[string]string{
		"op ConflictAgent":    "an AI agent resolving conflicts",
		"op CompleteConflict": "an AI agent resolving and completing conflicts",
		"op ReviewChanges":    "an AI review",
		"op GenerateMessage":  "an AI commit-message generation",
		"op Something":        "a long-running AI task",
	} {
		if got := busyHolderLabel(holder); got != want {
			t.Errorf("%s: got %q, want %q", holder, got, want)
		}
	}
}
