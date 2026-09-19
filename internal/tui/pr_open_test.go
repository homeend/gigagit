package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// Both PR ops touch only gg's private refs: they must refresh NOTHING from the
// source registry. nil would mean "every source" — and auto-fire the remote
// tags ls-remote probe.
func TestPROpsRefreshNoSources(t *testing.T) {
	t.Parallel()
	for _, op := range []engine.Operation{engine.FetchPRHead{}, engine.ForgetPR{}} {
		if got := opAffectedSources(op); got == nil || len(got) != 0 {
			t.Errorf("%T: sources = %v, want empty and non-nil", op, got)
		}
	}
}

func TestPRTitle(t *testing.T) {
	t.Parallel()
	got := prTitle(model.PullRequest{Number: 7, Title: "Add\tparser"})
	if got != "PR #7 · Add parser" {
		t.Fatalf("title = %q", got)
	}
}

// A PR diff rides the merge-preview surface but wears its own title — and
// keeps it when a refresh re-resolves the pair.
func TestPreviewOpenHonoursATitleOverride(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	eps, err := m.svc.PreviewOpen(context.Background(), "feat/x", "main")
	if err != nil {
		t.Fatal(err)
	}
	nm, _ := m.Update(previewOpenMsg{source: "feat/x", target: "main", gen: m.previewGen, eps: eps, title: "PR #7 · x"})
	m = nm.(Model)
	if m.filesView == nil || m.filesTitle != "PR #7 · x" || m.previewOpen == nil || m.previewOpen.title != "PR #7 · x" {
		t.Fatalf("filesTitle = %q, previewOpen = %+v", m.filesTitle, m.previewOpen)
	}
	// The one-off re-resolve a previews refresh issues carries the title along.
	msg := m.reopenPreviewCmd("", "feat/x", "main", "", "")().(previewOpenMsg)
	if msg.title != "PR #7 · x" {
		t.Fatalf("re-resolve lost the title: %q", msg.title)
	}
	// An ordinary preview of another pair does not inherit it.
	if other := m.reopenPreviewCmd("", "main", "feat/x", "", "")().(previewOpenMsg); other.title != "" {
		t.Fatalf("another pair inherited the title: %q", other.title)
	}
}

func TestEnterOnAPRStartsTheFetch(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	if !m.canOpenPR() {
		t.Fatal("a focused PR row with idle ops must be openable")
	}
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if cmd == nil || !strings.Contains(m.statusMsg, "#7") {
		t.Fatalf("enter must resolve the fetch off-thread (cmd=%v status=%q)", cmd != nil, m.statusMsg)
	}
}

func TestPRFetchReadyChainsTheOpen(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	pr := m.prs[0]
	// A resolve failure opens nothing and says why.
	nm, cmd := m.Update(prFetchReadyMsg{pr: pr, err: errors.New("no remote for o/r")})
	mm := nm.(Model)
	if cmd != nil || mm.pendingPROpen != nil || !strings.Contains(mm.statusMsg, "no remote") {
		t.Fatalf("failure: cmd=%v pending=%v status=%q", cmd != nil, mm.pendingPROpen, mm.statusMsg)
	}
	// Success arms the chain and runs the op.
	nm, cmd = m.Update(prFetchReadyMsg{pr: pr, op: engine.FetchPRHead{Remote: "origin", Refspec: "refs/pull/7/head", Number: 7}})
	mm = nm.(Model)
	if cmd == nil || mm.pendingPROpen == nil || mm.pendingPROpen.Number != 7 || !mm.running {
		t.Fatalf("success: cmd=%v pending=%+v running=%v", cmd != nil, mm.pendingPROpen, mm.running)
	}
	// The op finished: the chain fires once and the slot clears.
	nm, cmd = mm.Update(opFinishedMsg{res: engine.Result{Summary: "fetched"}})
	mm = nm.(Model)
	if cmd == nil || mm.pendingPROpen != nil {
		t.Fatalf("finish: cmd=%v pending=%+v", cmd != nil, mm.pendingPROpen)
	}
	// A failed op clears the slot too and opens nothing.
	m2 := prModel(t)
	m2.pendingPROpen = &pr
	m2.running = true
	nm, _ = m2.Update(opFinishedMsg{err: errors.New("fatal: couldn't find remote ref")})
	if got := nm.(Model); got.pendingPROpen != nil || got.filesView != nil {
		t.Fatalf("a failed fetch must clear the chain (pending=%+v)", got.pendingPROpen)
	}
}

// ahead == 0 reads as "merged" on the branch surface; for a PR the honest
// sentence is about the PR.
func TestPRPreviewStateNotice(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	eps := domain.PreviewEndpoints{}
	eps.Summary.State = domain.PreviewMerged
	nm, _ := m.Update(previewOpenMsg{source: "refs/gg/pr/7", target: "abc", gen: m.previewGen, eps: eps, title: "PR #7 · x"})
	if got := nm.(Model).statusMsg; !strings.Contains(got, "PR #7") || strings.Contains(got, "refs/gg/pr") {
		t.Fatalf("notice = %q", got)
	}
}
