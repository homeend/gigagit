package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// agentOverviewModel is a worktree with a.txt (40 lines) and b.txt (5
// lines, a note t<n> on line 3), and an agent's overview over both shown at
// width columns: "the loop" = a.txt:2, "my note" = the note.
func agentOverviewModel(t *testing.T, width int) Model {
	t.Helper()
	m := tempPromptStore(t, loadedNavModel(t)) // S writes its choice: never the machine's store
	if err := os.WriteFile(filepath.Join(m.currentWorktree, "b.txt"), []byte("b1\nb2\nb3\nb4\nb5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.width = width
	m.filterTyping = true // the note's add never moves the screen
	nm, cmd := m.applySteer(noteAddCmd("n-1", "b.txt", 3, 3, "the note"))
	nm = pumpAll(t, nm, cmd)
	r := awaitNote(t, nm, "n-1")
	if !r.OK || len(r.Notes) != 1 {
		t.Fatalf("note add: %+v", r)
	}
	nm.filterTyping = false
	nm, _ = applyOverview(t, nm, overviewAddCmd("o-1", "Tour", "Look at [the loop](a.txt:2) and [my note](note:"+r.Notes[0].ID+")."))
	return nm
}

// findOverviewDoc is the agent overview among the open files.
func findOverviewDoc(t *testing.T, m Model) *openFile {
	t.Helper()
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if d.src.kind == srcOverview {
			return d
		}
	}
	t.Fatal("no agent overview open")
	return nil
}

// countSplits is how many splits the layer stack holds.
func countSplits(m Model) int {
	n := 0
	if m.layers != nil {
		for _, l := range m.layers.entries {
			if _, ok := l.(*splitView); ok {
				n++
			}
		}
	}
	return n
}

func TestAgentOverviewAddOpensTheSplit(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120)
	sv := m.splitLayer()
	if sv == nil || m.topLayer() != sv {
		t.Fatalf("top %T, want the split", m.topLayer())
	}
	r := sv.right.doc()
	if r == nil || r.path != "a.txt" || r.src.kind != srcWorktree || r.p.cur != 1 {
		t.Fatalf("right %+v", r)
	}
}

func TestAgentOverviewNarrowOpensTheViewer(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 50)
	if _, ok := m.topLayer().(*fileViewer); !ok {
		t.Fatalf("top %T, want the full-screen viewer below 60 columns", m.topLayer())
	}
}

func TestSplitNoteAnchorSteersToTheNoteLine(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120)
	sv := m.splitLayer()
	pressDrain(t, m, "down") // the note anchor
	r := sv.right.doc()
	if r == nil || r.path != "b.txt" || r.p.cur != 2 || r.anchorCur != "" {
		t.Fatalf("right %+v: a note anchor lands on the note's line, no band current", r)
	}
}

func TestSplitGoneNoteAnchorIsMarked(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120)
	sv := m.splitLayer()
	ov := sv.left.doc()
	b := m.openFiles.find(m.currentWorktree, docKey(fileSource{kind: srcWorktree}, "b.txt"))
	b.removeNote(b.notes[0].ID)
	m = pressDrain(t, m, "down")
	if !ov.ov.anchors[1].missing || m.statusMsg == "" {
		t.Fatalf("missing %v status %q", ov.ov.anchors[1].missing, m.statusMsg)
	}
}

func TestBringToFrontOnAnOverviewOpensTheSplit(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120)
	m = pressDrain(t, m, "esc") // the split steps aside, both documents in the background
	ov := findOverviewDoc(t, m)
	m, _ = m.bringToFront(ov) // the switcher's enter and file_focus both come here
	if sv := m.splitLayer(); sv == nil || m.topLayer() != sv || sv.left.doc() != ov {
		t.Fatalf("top %T: bringToFront on an overview opens the split", m.topLayer())
	}
}

func TestBringToFrontRaisesTheSplitAlreadyShowingIt(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120)
	sv := m.splitLayer()
	m = m.pushLayer(newContentPopup("x", nil)) // something over it
	m, _ = m.bringToFront(sv.left.doc())
	if m.topLayer() != sv || countSplits(m) != 1 {
		t.Fatalf("top %T splits %d: raise, never a second split", m.topLayer(), countSplits(m))
	}
}

// backspace in a file an overview opened (the narrow viewer) comes back to
// the overview — as the split once the window is wide again.
func TestAnchorBackOpensTheSplitWhenWide(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 50)
	m = pressDrain(t, m, "tab")   // select the first anchor
	m = pressDrain(t, m, "enter") // open it in the viewer
	if d := topDoc(m); d == nil || d.path != "a.txt" {
		t.Fatalf("top doc %+v", topDoc(m))
	}
	m.width = 120
	m = pressDrain(t, m, "backspace")
	if sv := m.splitLayer(); sv == nil || m.topLayer() != sv {
		t.Fatalf("top %T", m.topLayer())
	}
}

// S on an agent overview stacks the files its anchors name, first-anchored
// order (spec 2026-10-10-overview-split-all R4).
func TestStackOnAnAgentOverviewListsTheAnchoredFiles(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120) // anchors: a.txt:2, the note on b.txt
	m = pressDrain(t, m, "S")
	sp, ok := m.splitLayer().right.(*stackPane)
	if !ok {
		t.Fatalf("right %T, want the stack", m.splitLayer().right)
	}
	var got []string
	for _, f := range sp.stream.files {
		got = append(got, f.path)
	}
	if strings.Join(got, ",") != "a.txt,b.txt" {
		t.Fatalf("files %v, want the anchored files in first-anchored order", got)
	}
	if !strings.Contains(sp.d.title, "Tour") {
		t.Fatalf("title %q names the overview", sp.d.title)
	}
}

// A review's overview reopened after its review view closed stacks its
// anchored files too: the review tree is gone.
func TestStackOnAReviewOverviewAfterTheReviewClosed(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	m = pressDrain(t, m, "esc")
	m.filesReview = nil // the review view is gone
	m, _ = m.bringToFront(ov)
	m = pressDrain(t, m, "S")
	sp, ok := m.splitLayer().right.(*stackPane)
	if !ok {
		t.Fatalf("right %T, want the stack", m.splitLayer().right)
	}
	if len(sp.stream.files) != 2 || sp.stream.files[0].path != "a.go" || sp.stream.files[1].path != "b.go" {
		t.Fatalf("files %+v: a.go, b.go (zzz.go and the note are plain at the commit)", sp.stream.files)
	}
}

// Final review I2: a resize below the split's width turns it into the
// overview's full-screen viewer — the narrow door every way in uses.
func TestNarrowResizeTurnsTheSplitIntoTheViewer(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120)
	ov := findOverviewDoc(t, m)
	u, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 30})
	m = u.(Model)
	fv, ok := m.topLayer().(*fileViewer)
	if m.splitLayer() != nil || !ok || fv.openFile != ov {
		t.Fatalf("top %T split %v: the overview, full-screen", m.topLayer(), m.splitLayer() != nil)
	}
}

// Final review I2: a closing files view takes only the split that steers
// through it (a review's); an agent overview's split stays.
func TestFilesViewClosingKeepsAnAgentSplit(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120)
	m = m.closeFilesView()
	if m.splitLayer() == nil {
		t.Fatal("an agent overview's split does not depend on the files view")
	}
}

// Final review I3: the right pane's file taken out to the full-screen viewer
// (ctrl+\ on it), backspace brings the split back WITH a file on the right.
func TestSplitComesBackWithItsFileAfterTheFileWasTakenOut(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120)
	a := m.splitLayer().right.doc()
	m, cmd := m.bringToFront(a) // the switcher's enter on a.txt
	m = drainCmds(t, m, cmd)
	m = pressDrain(t, m, "backspace")
	sv := m.splitLayer()
	if sv == nil || m.topLayer() != sv || sv.right.doc() == nil {
		t.Fatalf("top %T right %v: the split comes back with its file", m.topLayer(), sv != nil && sv.right.doc() != nil)
	}
}

// Final review I4: r copies the selected anchor's reference on an agent
// overview in the split, by key and by the . menu (a stored review's
// overview has none, A9).
func TestSplitAgentOverviewRCopiesTheAnchorReference(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120)
	var got string
	m = captureClip(m, &got)
	m = pressDrain(t, m, "r")
	if !strings.Contains(got, "a.txt:2") {
		t.Fatalf("copied %q, want the anchor's reference", got)
	}
	got = ""
	runMenuRow(t, m, "overview-ref")
	if !strings.Contains(got, "a.txt:2") {
		t.Fatalf("menu copied %q", got)
	}
}

// Found in the fix pass: the . menu over an agent overview's split (no files
// view under it) is the split's, never the hidden Commits panel's actions.
func TestSplitMenuWithoutAFilesViewIsTheSplits(t *testing.T) {
	t.Parallel()
	m := agentOverviewModel(t, 120)
	for _, l := range menuLabels(m) {
		if l == "Show as list" || l == "Show as graph" {
			t.Fatalf("the Commits panel's rows leak into the split's menu: %q", menuLabels(m))
		}
	}
}
