package tui

import (
	"os"
	"path/filepath"
	"time"

	"github.com/homeend/gigagit/internal/steer"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

const (
	cmpShaA = "e296f86a1b2c3d4e5f60718293a4b5c6d7e8f901"
	cmpShaB = "228b22aa1b2c3d4e5f60718293a4b5c6d7e8f902"
)

// compareLinkModel opens a diff the compare loader stamped with its two
// endpoints: 40 context rows with one deletion at row 20 (old line 21).
func compareLinkModel(t *testing.T, left, right model.Endpoint) Model {
	t.Helper()
	rows := sameRowsTUI(40)
	rows[20] = textdiff.Row{Kind: textdiff.Del, Left: "gone", LeftNo: 21}
	m := openedDiffModel(12, rows, []int{20})
	m.linkRepoName = "gigagit"
	m.currentWorktree = "/repo"
	v := m.diffLayer()
	v.title, v.compare = "a.txt", true
	v.cmp = &compareStamp{left: left, right: right, path: "a.txt"}
	return m
}

func wantLink(t *testing.T, m Model, want string) {
	t.Helper()
	got, ok := m.contextLinkText()
	if !ok {
		t.Fatalf("contextLinkText refused, want %q", want)
	}
	if got != want {
		t.Fatalf("contextLinkText = %q, want %q", got, want)
	}
	if _, err := model.ParseLink(got); err != nil {
		t.Fatalf("ParseLink(%q) = %v", got, err)
	}
}

// An ad-hoc compare of two commits carries no note scope, yet its lines are
// addressable: the pair form, on the side the cursor is on.
func TestCompareDiffLinkIsThePairWithTheCursorLine(t *testing.T) {
	t.Parallel()
	m := compareLinkModel(t, mustCommitEndpoint(cmpShaA), mustCommitEndpoint(cmpShaB))
	v := m.diffLayer()

	v.curLine = 5
	wantLink(t, m, "gg://gigagit/a.txt@"+cmpShaA+".."+cmpShaB+":6")

	v.curLine = 20 // the deletion: it exists on the old side only
	wantLink(t, m, "gg://gigagit/a.txt@"+cmpShaA+".."+cmpShaB+":old:21")

	v.curLine, v.onOld = 5, true
	wantLink(t, m, "gg://gigagit/a.txt@"+cmpShaA+".."+cmpShaB+":old:6")

	if r, ok := m.contextLinkRow(); !ok || r.id != "copy-link" {
		t.Fatalf("contextLinkRow = %+v ok=%v, want the copy-link row", r, ok)
	}
}

// A compare against the working tree or the index has no pair form: each side
// is addressed as the version it shows.
func TestCompareDiffLinkAgainstTheWorkingTree(t *testing.T) {
	t.Parallel()
	m := compareLinkModel(t, mustCommitEndpoint(cmpShaA), model.WorkTreeEndpoint())
	v := m.diffLayer()
	v.cmp.oldPath = "old.txt" // a rename: the old side has its own path

	v.curLine = 5
	wantLink(t, m, "gg://gigagit/a.txt:6")
	v.curLine = 20
	wantLink(t, m, "gg://gigagit/old.txt@"+cmpShaA+":21")

	m = compareLinkModel(t, mustCommitEndpoint(cmpShaA), model.IndexEndpoint())
	m.diffLayer().curLine = 5
	wantLink(t, m, "gg://gigagit/a.txt@staged:6")
}

// An endpoint no link names (a shelf entry) refuses, as does a short sha.
func TestCompareDiffLinkRefusesWhatHasNoAddress(t *testing.T) {
	t.Parallel()
	shelf, err := model.ShelfEndpoint("s1")
	if err != nil {
		t.Fatal(err)
	}
	m := compareLinkModel(t, shelf, mustCommitEndpoint(cmpShaB))
	m.diffLayer().curLine = 20 // the old side is the shelf entry
	if got, ok := m.contextLinkText(); ok {
		t.Fatalf("contextLinkText = %q, want a refusal for a shelf side", got)
	}
	m = compareLinkModel(t, mustCommitEndpoint("e296f86"), mustCommitEndpoint(cmpShaB))
	m.diffLayer().curLine = 5
	if got, ok := m.contextLinkText(); ok {
		t.Fatalf("contextLinkText = %q, want a refusal for an abbreviated sha", got)
	}
}

// In a stack the link names the file the CURSOR is in, from that file's own
// loader stamp.
func TestStackedCompareDiffLinkNamesTheCursorFile(t *testing.T) {
	t.Parallel()
	r := sameRowsTUI(6)
	v := stackViewOf(t, r, r)
	for i := range v.stk.files {
		v.stk.files[i].d.cmp = &compareStamp{left: mustCommitEndpoint(cmpShaA), right: mustCommitEndpoint(cmpShaB), path: v.stk.files[i].path}
	}
	v.curLine = -1
	for i, l := range v.lines {
		if l.file == 1 && l.isBody() && l.Row.RightNo == 3 {
			v.curLine = i
		}
	}
	if v.curLine < 0 {
		t.Fatal("no body row for line 3 of the second file")
	}
	m := diffModel()
	m.linkRepoName = "gigagit"
	m.currentWorktree = "/repo"
	m = m.pushLayer(v)
	wantLink(t, m, "gg://gigagit/f1.go@"+cmpShaA+".."+cmpShaB+":3")
}

// A SAVED pair's old side is commit a, exactly like an ad-hoc compare's: the
// link carries old:<line> rather than degrading to the file form.
func TestSavedPairLinkCarriesTheOldSide(t *testing.T) {
	t.Parallel()
	m := compareLinkModel(t, mustCommitEndpoint(cmpShaA), mustCommitEndpoint(cmpShaB))
	v := m.diffLayer()
	v.cmp = nil
	v.previewSet = &domain.PreviewNoteSet{Base: cmpShaA, Tip: cmpShaB}
	v.noteAddr = model.FileAddress{State: model.StateCommitted, Commit: cmpShaB, Path: "a.txt"}
	if !v.previewSet.IsPair() {
		t.Fatal("a nameless set must be a pair")
	}
	v.curLine = 20
	wantLink(t, m, "gg://gigagit/a.txt@"+cmpShaA+".."+cmpShaB+":old:21")
}

// The full-screen diff draws no status bar: a copy made there confirms in the
// diff's own notice box, and a long link keeps both ends (the line is last).
func TestCopyInTheDiffViewConfirmsInItsNoticeBox(t *testing.T) {
	t.Parallel()
	m := compareLinkModel(t, mustCommitEndpoint(cmpShaA), mustCommitEndpoint(cmpShaB))
	link := "gg://gigagit/a.txt@" + cmpShaA + ".." + cmpShaB + ":old:21"
	nm, _ := m.Update(clipboardCopiedMsg{ok: "Copied link: " + link})
	got := nm.(Model).diffNotice
	if !strings.HasPrefix(got, "▸ Copied link: gg://") || !strings.HasSuffix(got, ":old:21") {
		t.Fatalf("diffNotice = %q, want the confirmation with the link's tail", got)
	}
	w, _ := m.overlayDims()
	if lipgloss.Width(got) > w-6 {
		t.Fatalf("diffNotice is %d columns wide, the box holds %d", lipgloss.Width(got), w-6)
	}

	plain := diffModel() // no diff on top: the status bar alone says it
	pm, _ := plain.Update(clipboardCopiedMsg{ok: "Copied link: x"})
	if n := pm.(Model).diffNotice; n != "" {
		t.Fatalf("diffNotice = %q with no diff view open, want none", n)
	}
}

// The round trip: an old-side pair link pasted back lands on that line's row,
// and L on its old pane copies the very same link.
func TestOldSidePairLinkLandsWhereItWasCopied(t *testing.T) {
	t.Parallel()
	dir, c1, _, _ := refPairRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("A2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-am", "c4")
	c4 := gitOut(t, dir, "rev-parse", "HEAD")
	m := refPairModel(t, dir)
	want := "gg://gigagit/a.txt@" + c1 + ".." + c4 + ":old:1"
	l, err := model.ParseLink(want)
	if err != nil {
		t.Fatal(err)
	}
	if l.Side != model.NoteSideOld || l.Line != 1 || l.Target.Pair == nil {
		t.Fatalf("ParseLink(%q) = %+v, want the pair's old line 1", want, l)
	}
	c := steer.Command{ID: "cl-1", Cmd: "navigate", File: l.Path,
		Target: &steer.Target{State: "pair", A: l.Target.Pair.A, B: l.Target.Pair.B},
		Line:   &steer.Line{Side: "old", No: l.Line}, Wait: true}
	m, cmd := m.applySteer(c)
	m = pumpAll(t, m, cmd)
	v := m.diffLayer()
	if v == nil {
		r, ok := steer.AwaitReply(m.steerDir, "cl-1", 3*time.Second)
		t.Fatalf("the file's diff must be open; reply = %+v ok=%v pending=%+v", r, ok, m.pendingSteer)
	}
	if row, ok := v.cursorRow(); !ok || row.LeftNo != 1 {
		t.Fatalf("cursor row = %+v ok=%v, want the row of OLD line 1", row, ok)
	}
	// A landing places the ROW; the pane is the reader's (alt+left).
	v.onOld = true
	m.linkRepoName = "gigagit" // only the copy reads it: the landing resolves a real repo
	wantLink(t, m, want)
}
