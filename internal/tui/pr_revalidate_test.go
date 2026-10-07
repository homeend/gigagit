package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A PR diff opens from what is local; the forge is asked afterwards. A head
// that moved re-runs the open chain — once — while the user is still there.
func TestPRRevalidatedMovedReopensWhileStillShowing(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	pr := model.PullRequest{Number: 7, Title: "x", State: model.PRStateOpen}
	nm, cmd := m.Update(prRevalidatedMsg{n: 7, pr: pr, moved: true})
	mm := nm.(Model)
	if cmd == nil || mm.prRevalidateSkip != 7 {
		t.Fatalf("a moved head must re-run the open (cmd=%v skip=%d)", cmd != nil, mm.prRevalidateSkip)
	}
	if !strings.Contains(mm.statusMsg, "#7") {
		t.Errorf("the update must be announced: status=%q", mm.statusMsg)
	}
}

func TestPRRevalidatedIgnoredWhenNotMovedOrGone(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	pr := model.PullRequest{Number: 7, State: model.PRStateOpen}
	// A good refresh still asks whether a send was interrupted (plan 3), so
	// "no reopen" is the reopen's own marker, not "no command at all".
	reopened := func(tm tea.Model) bool { return tm.(Model).prRevalidateSkip != 0 }
	if nm, _ := m.Update(prRevalidatedMsg{n: 7, pr: pr, moved: false}); reopened(nm) {
		t.Error("an unchanged head needs nothing")
	}
	if nm, _ := m.Update(prRevalidatedMsg{n: 9, pr: pr, moved: true}); reopened(nm) {
		t.Error("another PR's verdict must not reopen this view")
	}
	closed := m.closePreviewView()
	if nm, _ := closed.Update(prRevalidatedMsg{n: 7, pr: pr, moved: true}); reopened(nm) {
		t.Error("the user left: the next open fetches, nothing reopens behind their back")
	}
}

// The reopen a revalidation caused must not revalidate again (a fetch that
// cannot reach the forge's head would otherwise loop).
func TestPRReopenAfterRevalidateDoesNotRevalidateAgain(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	if !m.prRevalidateInflight {
		t.Fatal("opening a PR diff must start one revalidation")
	}
	m2 := prDiffModel(t)
	m2.prRevalidateInflight = false
	m2.prRevalidateSkip = 7
	msg := m2.reopenPreviewCmd("", "feat/x", "main", "", "")().(previewOpenMsg)
	m2 = m2.closePreviewView()
	msg.gen = m2.previewGen
	nm, _ := m2.Update(msg)
	if mm := nm.(Model); mm.prRevalidateInflight || mm.prRevalidateSkip != 0 {
		t.Errorf("inflight=%v skip=%d, want the skip consumed and no new revalidation", mm.prRevalidateInflight, mm.prRevalidateSkip)
	}
}
