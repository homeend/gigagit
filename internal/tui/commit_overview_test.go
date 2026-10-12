package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
)

// commitOverviewModel: stackRepoModel plus commit c2 changing only a.go, and
// a permanent overview on c2 linking a.go:3 (remark "the func") and b.go:3.
func commitOverviewModel(t *testing.T) (Model, string) {
	t.Helper()
	m := stackRepoModel(t)
	m.svc.UseNotesDir(t.TempDir())
	dir := m.currentWorktree
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() { _ = 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInDir(t, dir, "commit", "-qam", "c2")
	ctx := context.Background()
	tg, err := m.svc.HeadOverviewTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := m.svc.SaveOverview(ctx, domain.SaveOverview{Target: tg, Agent: "Claude Code", Text: []byte(
		`{"version":1,"title":"Tour of a","overview":"Start at [the func](a.go:3), then [b](b.go:3).","files":[{"path":"a.go","annotations":[{"newRange":[3,3],"summary":"the func"}]}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	return m, got.ID
}

func openCommitOverviewT(t *testing.T, m Model, id string) Model {
	t.Helper()
	m, cmd := m.openCommitOverview(id)
	return drainCmds(t, m, cmd)
}

func TestOpenCommitOverviewShowsTheSplit(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m = openCommitOverviewT(t, m, id)
	sv := m.splitLayer()
	if sv == nil || m.topLayer() != sv {
		t.Fatalf("top = %T, want the split", m.topLayer())
	}
	ov := sv.left.doc()
	if ov == nil || ov.src.kind != srcCommitOverview || ov.src.rev != id || ov.ov.tip == "" {
		t.Fatalf("left = %+v", ov)
	}
	screen := ansi.Strip(m.View())
	for _, want := range []string{"Overview: Tour of a", "View a.go @", "func A()", "the func"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
}

func TestCommitOverviewNarrowOpensFullScreen(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m.width = 50
	m = openCommitOverviewT(t, m, id)
	if m.splitLayer() != nil || layerOf[*fileViewer](m) == nil {
		t.Fatalf("below 60 columns the overview opens full-screen; top = %T", m.topLayer())
	}
}

func TestCommitOverviewDiffUntouchedFile(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m = openCommitOverviewT(t, m, id)
	m = pressDrain(t, m, "down") // step to the b.go anchor
	m = pressDrain(t, m, "D")
	if screen := ansi.Strip(m.View()); !strings.Contains(screen, "no change to this file in the overview's commit") {
		t.Fatalf("D on a file the commit does not change:\n%s", screen)
	}
}

func TestCommitOverviewMissingCommitOpens(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	dir := m.currentWorktree
	gitInDir(t, dir, "commit", "--amend", "-qm", "rewritten")
	gitInDir(t, dir, "reflog", "expire", "--expire=now", "--all")
	gitInDir(t, dir, "gc", "-q", "--prune=now")
	m = openCommitOverviewT(t, m, id)
	if m.splitLayer() == nil && layerOf[*fileViewer](m) == nil {
		t.Fatalf("a gone commit still opens its overview (anchors plain); status %q", m.statusMsg)
	}
}

func TestStoredOverviewKinds(t *testing.T) {
	t.Parallel()
	if !srcReviewOverview.storedOverview() || !srcCommitOverview.storedOverview() || srcOverview.storedOverview() {
		t.Fatal("storedOverview")
	}
}

// The agent re-saves the overview (same commit + title replaces) and links it
// again: the open document shows the new text, not the one read first.
func TestReSavedOverviewReopensFresh(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m = openCommitOverviewT(t, m, id)
	ctx := context.Background()
	tg, err := m.svc.HeadOverviewTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := m.svc.SaveOverview(ctx, domain.SaveOverview{Target: tg, Agent: "Claude Code", Text: []byte(
		`{"version":1,"title":"Tour of a","overview":"Revised: [the func](a.go:3).","files":[{"path":"a.go","annotations":[{"newRange":[3,3],"summary":"revised remark"}]}]}`)})
	if err != nil || got.ID != id {
		t.Fatalf("re-save = %+v %v", got, err)
	}
	m = pressDrain(t, m, "esc") // backgrounded, then opened again (. menu / a link)
	m = openCommitOverviewT(t, m, id)
	if ov := m.splitLayer().left.doc(); ov == nil || !strings.Contains(ov.ov.text, "Revised") {
		t.Fatalf("the open overview kept the old text: %+v", ov.ov)
	}
	// The file beside it (the same a.go doc, already loaded) shows the new remark.
	rd := m.splitLayer().right.doc()
	if ns := rd.shownNotes(); len(ns) != 1 || ns[0].Summary != "revised remark" {
		t.Fatalf("the file beside it shows %+v", ns)
	}
}

// R9: the file an anchor opened is the shared doc for that file at that
// commit; opened again another way (the review view's enter, the Files view
// preview), it shows none of the overview's remarks.
func TestOverviewRemarksStayInTheOverview(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m = openCommitOverviewT(t, m, id)
	rd := m.splitLayer().right.doc()
	if rd == nil || len(rd.shownNotes()) != 1 {
		t.Fatalf("setup: the anchored file shows the remark: %+v", rd)
	}
	commit, path := rd.src.rev, rd.path
	m = pressDrain(t, m, "esc") // the overview goes to the background, still open
	m, cmd := m.openFileAtCommit(commit, path)
	m = drainCmds(t, m, cmd)
	if d := topDoc(m); d == nil || d != rd || len(d.shownNotes()) != 0 {
		t.Fatalf("opened from elsewhere, the file shows the overview's remarks: %+v", d)
	}
	m, cmd = m.openPreviewSrc(rd.src, path, func(context.Context) ([]byte, error) { return []byte("x\n"), nil })
	m = drainCmds(t, m, cmd)
	if len(rd.shownNotes()) != 0 {
		t.Fatal("the Files view preview shows the overview's remarks")
	}
}

// esc backgrounds the overview; opening it again (the same save) shows the
// file beside it with its remark again.
func TestOverviewRoundTripKeepsRemarks(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m = openCommitOverviewT(t, m, id)
	m = pressDrain(t, m, "esc")
	m = openCommitOverviewT(t, m, id)
	rd := m.splitLayer().right.doc()
	if ns := rd.shownNotes(); len(ns) != 1 || ns[0].Summary != "the func" {
		t.Fatalf("after the round trip the file shows %+v", ns)
	}
}
