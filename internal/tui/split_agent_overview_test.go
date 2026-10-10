package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// agentOverviewModel is a worktree with a.txt (40 lines) and b.txt (5
// lines, a note t<n> on line 3), and an agent's overview over both shown at
// width columns: "the loop" = a.txt:2, "my note" = the note.
func agentOverviewModel(t *testing.T, width int) Model {
	t.Helper()
	m := loadedNavModel(t)
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
