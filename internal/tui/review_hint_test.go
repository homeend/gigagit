package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// A navigate carrying ?review= opens the review view — the user's gg open /
// start-at, an agent's navigate: one steer path — back to its commit.
func TestReviewLinkOpensTheReview(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, reviewViewDoc)
	sha := m.commits[0].Hash
	c := steer.Command{Cmd: "navigate", Commit: sha, HintKind: model.ReviewHintKind, HintID: id}
	m, cmd := m.steerNavigate(c)
	m = drainCmds(t, m, cmd)
	if m.filesReview == nil || m.filesReview.id != id {
		t.Fatalf("the review did not open: %+v", m.filesReview)
	}
	if m.filesReview.back.Hash != sha {
		t.Fatalf("back = %q, want the review's commit %q", m.filesReview.back.Hash, sha)
	}
}

// A deleted review: a notice, then the commit opens as a plain link would.
func TestDeletedReviewLinkLandsTheCommit(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, reviewViewDoc)
	sha := m.commits[0].Hash
	if err := m.svc.NoteRemove(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	c := steer.Command{Cmd: "navigate", Commit: sha, HintKind: model.ReviewHintKind, HintID: id}
	m, cmd := m.steerNavigate(c)
	m = drainCmds(t, m, cmd)
	if m.filesReview != nil || !strings.Contains(m.statusMsg, "no longer exists") {
		t.Fatalf("review=%v status=%q", m.filesReview, m.statusMsg)
	}
}

// The user's own paste into # lands the same way.
func TestPastedReviewLinkOpensTheReview(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, reviewViewDoc)
	m.statePath = filepath.Join(t.TempDir(), "repos.toml")
	m.ready, m.loading = true, false // as loadedNavModel: # waits for a loaded session
	if m.filesView != nil {
		m = m.closeFilesView() // # is a base-layout key
	}
	link, err := m.svc.ReviewLink(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := pasteLink(t, m, link)
	m = drainCmds(t, m, cmd)
	if m.filesReview == nil || m.filesReview.id != id {
		perr := ""
		if p := layerOf[*gotoCommitPopup](m); p != nil {
			perr = "prompt still open: " + p.err
		}
		t.Fatalf("the pasted review link did not open the review: %+v (status %q) %s", m.filesReview, m.statusMsg, perr)
	}
}
