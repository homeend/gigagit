package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/forge/forgetest"
	"github.com/homeend/gigagit/internal/model"
)

// savePRReviewTUI stores a review document on PR #7's scope (what /gg-review
// does on a pull request link) and returns its id.
func savePRReviewTUI(t *testing.T, m Model, doc string) string {
	t.Helper()
	ctx := context.Background()
	set, err := m.svc.PreviewNotes(ctx, "refs/gg/pr/7", "main")
	if err != nil || !set.OK() {
		t.Fatalf("PR note set: %+v err %v", set, err)
	}
	id, _, err := m.svc.SaveReview(ctx, domain.SaveReview{Target: domain.ScopeReviewTarget(set), Agent: "claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// openPR7 opens PR #7's file list (its comments answered by the fake gh).
func openPR7(t *testing.T, m Model) Model {
	t.Helper()
	open := m.openPRPreviewCmd(model.PullRequest{Number: 7, State: "open", Target: "main"})().(previewOpenMsg)
	u, cmd := m.Update(open)
	m = drainCmds(t, u.(Model), cmd)
	if m.previewOpen == nil || m.previewOpen.prNumber != 7 {
		t.Fatalf("previewOpen = %+v", m.previewOpen)
	}
	return m
}

func reviewRow(t *testing.T, m Model, id string) contentLine {
	t.Helper()
	for _, l := range m.filesView.visible() {
		if l.noteID == id {
			return l
		}
	}
	t.Fatalf("no row for review %s in:\n%s", id, m.View())
	return contentLine{}
}

// R4: a review stored on the PR is a row under Reviews in the PR's file
// list; enter opens the review view titled with the PR; esc returns to the
// PR's file list (still the PR: its number, its title), the cursor on the row.
// Serial: env (prSendModel).
func TestPRReviewsRowOpensAndReturns(t *testing.T) {
	m, _, _ := prSendModel(t)
	id := savePRReviewTUI(t, m, `{"version":1,"summary":"## Summary\nfine","files":[{"path":"big.go","annotations":[{"newRange":[5,5],"summary":"why?"}]}]}`)
	m = openPR7(t, m)
	l := reviewRow(t, m, id)
	if !strings.Contains(l.text, "claude") {
		t.Fatalf("row %q", l.text)
	}
	u, cmd := m.openDiffForFileLine(l)
	m = drainCmds(t, u.(Model), cmd)
	if m.filesReview == nil || m.filesReview.id != id || !strings.Contains(m.filesTitle, "#7") {
		t.Fatalf("review view: %+v title %q", m.filesReview, m.filesTitle)
	}
	m, cmd = m.leaveReviewView()
	m = drainCmds(t, m, cmd)
	if m.filesReview != nil || m.previewOpen == nil || m.previewOpen.prNumber != 7 || !strings.Contains(m.filesTitle, "#7") {
		t.Fatalf("after esc: review %v previewOpen %+v title %q", m.filesReview != nil, m.previewOpen, m.filesTitle)
	}
	vis := m.filesView.visible()
	if m.filesView.sel < 0 || m.filesView.sel >= len(vis) || vis[m.filesView.sel].noteID != id {
		t.Fatalf("cursor not on the review's row: sel %d", m.filesView.sel)
	}
}

// Review Focus 3: a review of an older head still lists, says (older), opens.
// Serial: env (prSendModel).
func TestPRReviewsRowOlder(t *testing.T) {
	m, dir, _ := prSendModel(t)
	id := savePRReviewTUI(t, m, `{"version":1,"summary":"old","files":[{"path":"big.go","annotations":[{"newRange":[5,5],"summary":"why?"}]}]}`)
	// The PR head moves on (a new commit on feat; refs/gg/pr/7 and the fake
	// gh's head follow).
	runGit(t, dir, "checkout", "-q", "feat")
	if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte(strings.Repeat("x\n", 31)), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "big.go")
	runGit(t, dir, "commit", "-q", "-m", "more")
	head := gitOut(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "update-ref", "refs/gg/pr/7", head)
	runGit(t, dir, "checkout", "-q", "main")
	forgetest.Seed(t, filepath.Join(dir, ".git", "fakegh"), prSendFixtures(head))
	m = openPR7(t, m)
	l := reviewRow(t, m, id)
	if !strings.Contains(l.text, "older tip") {
		t.Fatalf("row %q lacks the older-tip mark", l.text)
	}
	u, cmd := m.openDiffForFileLine(l)
	m = drainCmds(t, u.(Model), cmd)
	if m.filesReview == nil || m.filesReview.id != id || !m.filesReview.older {
		t.Fatalf("review view: %+v", m.filesReview)
	}
}
