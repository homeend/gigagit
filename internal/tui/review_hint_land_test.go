package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// A review link with a file and line opens the REVIEW on that file — its
// review diff, remarks stamped — with the cursor on the line.
func TestReviewLinkWithALineOpensTheReviewThere(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, reviewViewDoc)
	sha := m.commits[0].Hash
	c := steer.Command{Cmd: "navigate", Commit: sha, File: "a.go", Line: &steer.Line{Side: "new", No: 3},
		HintKind: model.ReviewHintKind, HintID: id}
	m, cmd := m.steerNavigate(c)
	m = drainCmds(t, m, cmd)
	if m.filesReview == nil || m.filesReview.id != id {
		t.Fatalf("the review did not open: %+v", m.filesReview)
	}
	v := m.diffLayer()
	if v == nil || v.reviewID != id {
		t.Fatalf("no review diff on top (%T, status %q)", m.topLayer(), m.statusMsg)
	}
	if row, ok := v.cursorRow(); !ok || row.RightNo != 3 {
		t.Fatalf("cursor row %+v ok=%v, want new line 3", row, ok)
	}
	if m.pendingSteer != nil {
		t.Fatal("the landing must not stay parked")
	}
}

// A file the review does not hold: the review stays up, nothing parked.
func TestReviewLinkToAFileNotInTheReview(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, reviewViewDoc)
	sha := m.commits[0].Hash
	c := steer.Command{Cmd: "navigate", Commit: sha, File: "nope.go", HintKind: model.ReviewHintKind, HintID: id}
	m, cmd := m.steerNavigate(c)
	m = drainCmds(t, m, cmd)
	if m.filesReview == nil || m.filesReview.id != id || m.diffLayer() != nil {
		t.Fatalf("review=%+v diff=%v", m.filesReview, m.diffLayer() != nil)
	}
	if m.pendingSteer != nil {
		t.Fatal("a landing that cannot land must not stay parked")
	}
}

// A RANGE review (a compare) lands too, on an old-side line.
func TestRangeReviewLinkLandsOnAnOldSideLine(t *testing.T) {
	t.Parallel()
	m := stackRepoModel(t)
	m.svc.UseNotesDir(t.TempDir())
	tip, base := m.commits[0].Hash, m.commits[1].Hash
	doc := `{"version":1,"summary":"ok","files":[{"path":"a.go","annotations":[{"oldRange":[1,1],"summary":"old"}]}]}`
	tg := domain.ReviewTarget{Kind: domain.ReviewRange, Range: base + ".." + tip, Commit: tip}
	id, _, err := m.svc.SaveReview(context.Background(), domain.SaveReview{Target: tg, Agent: "Claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	c := steer.Command{Cmd: "navigate", Commit: tip, File: "a.go", Line: &steer.Line{Side: "old", No: 1},
		HintKind: model.ReviewHintKind, HintID: id}
	m, cmd := m.steerNavigate(c)
	m = drainCmds(t, m, cmd)
	v := m.diffLayer()
	if m.filesReview == nil || v == nil || v.reviewID != id {
		t.Fatalf("review=%+v diff=%v (status %q)", m.filesReview, v != nil, m.statusMsg)
	}
	if row, ok := v.cursorRow(); !ok || row.LeftNo != 1 {
		t.Fatalf("cursor row %+v ok=%v, want old line 1", row, ok)
	}
	if m.pendingSteer != nil {
		t.Fatal("the landing must not stay parked")
	}
}

// Cancelling the review's loading box (esc) answers the navigate: a landing
// that cannot happen never leaves its sender waiting.
func TestCancelledReviewLandingIsAnswered(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, reviewViewDoc)
	sha := m.commits[0].Hash
	c := steer.Command{Cmd: "navigate", Commit: sha, File: "a.go", Line: &steer.Line{Side: "new", No: 3},
		HintKind: model.ReviewHintKind, HintID: id}
	m, cmd := m.steerNavigate(c)
	u, next := m.Update(cmd()) // the review lookup lands: the loading box opens
	m = u.(Model)
	var view tea.Msg
	for _, msg := range flattenCmd(t, next) {
		if _, ok := msg.(reviewViewMsg); ok {
			view = msg
		}
	}
	if view == nil {
		t.Fatal("no review read was started")
	}
	u, _ = m.Update(keyMsg("esc")) // cancel the loading box
	m = u.(Model)
	u, next = m.Update(view)
	m = drainCmds(t, u.(Model), next)
	if !strings.Contains(m.statusMsg, "did not open") || m.pendingSteer != nil {
		t.Fatalf("status %q pending %v: the cancelled landing must be answered", m.statusMsg, m.pendingSteer != nil)
	}
}

// A working review lands through the compare stage.
func TestWorkingReviewLinkLandsOnTheFile(t *testing.T) {
	t.Parallel()
	m := stackRepoModel(t)
	m.svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	top, err := m.svc.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(top, "u.txt"), []byte("u1\nu2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := `{"version":1,"summary":"ok","files":[{"path":"u.txt","annotations":[{"newRange":[2,2],"summary":"on u"}]}]}`
	id, _, err := m.svc.SaveReview(ctx, domain.SaveReview{Target: domain.WorkingReviewTarget(), Agent: "Claude", Text: doc,
		Files: []model.NoteFile{{Path: "u.txt", Blob: blobOfForTest("u1\nu2\n")}}})
	if err != nil {
		t.Fatal(err)
	}
	c := steer.Command{Cmd: "navigate", File: "u.txt", Line: &steer.Line{Side: "new", No: 2},
		Target: &steer.Target{State: "unstaged"}, HintKind: model.ReviewHintKind, HintID: id}
	m, cmd := m.steerNavigate(c)
	m = drainCmds(t, m, cmd)
	v := m.diffLayer()
	if m.filesReview == nil || v == nil || v.reviewID != id {
		t.Fatalf("review=%+v diff=%v (status %q)", m.filesReview, v != nil, m.statusMsg)
	}
	if row, ok := v.cursorRow(); !ok || row.RightNo != 2 {
		t.Fatalf("cursor row %+v ok=%v, want new line 2", row, ok)
	}
	if m.pendingSteer != nil {
		t.Fatal("the landing must not stay parked")
	}
}
