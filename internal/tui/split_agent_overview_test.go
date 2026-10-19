package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// agentOverviewModel is a worktree with a.txt (40 lines) and b.txt (5
// lines, a note t<n> on line 3), and an agent's overview over both shown at
// width columns: "the loop" = a.txt:2, "my note" = the note.
func agentOverviewModel(t *testing.T, width int) Model {
	t.Helper()
	return agentOverviewModelOf(t, width, "")
}

// agentOverviewModelOf is agentOverviewModel with tail after the overview's
// anchors and the extra files written (each "x1\nx2\n…") before it is added.
func agentOverviewModelOf(t *testing.T, width int, tail string, extra ...string) Model {
	t.Helper()
	m := tempPromptStore(t, loadedNavModel(t)) // S writes its choice: never the machine's store
	if err := os.WriteFile(filepath.Join(m.currentWorktree, "b.txt"), []byte("b1\nb2\nb3\nb4\nb5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range extra {
		if err := os.WriteFile(filepath.Join(m.currentWorktree, name), []byte("x1\nx2\nx3\nx4\nx5\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m.width = width
	nm, n := addFileNote(t, m, "b.txt", 3, 3, "the note")
	nm, _ = addOverviewDoc(t, nm, "Tour", "Look at [the loop](a.txt:2) and [my note](note:"+n.ID+")."+tail, false)
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

// docsMissing is the store's missing flag of agent overview ov's anchor i
// (what the page shows and a sync brings back).
func docsMissing(t *testing.T, m Model, ov *openFile, i int) bool {
	t.Helper()
	o, ok := m.docs.Overview(ov.id())
	if !ok || i >= len(o.Anchors) {
		t.Fatalf("overview %s has no anchor %d in the store", ov.id(), i)
	}
	return o.Anchors[i].Missing
}

// A file deleted after the agent added its overview: the steer's failed
// load marks the anchor missing in the store too, so a sync keeps the ✗.
func TestSplitSteerToADeletedFileMarksTheStore(t *testing.T) {
	t.Parallel()
	m := agentOverviewModelOf(t, 120, " See [c](c.txt:1).", "c.txt")
	sv := m.splitLayer()
	ov := sv.left.doc()
	if ov.ov.anchors[2].missing || docsMissing(t, m, ov, 2) {
		t.Fatal("c.txt was there at the add: its anchor starts found")
	}
	if err := os.Remove(filepath.Join(m.currentWorktree, "c.txt")); err != nil {
		t.Fatal(err)
	}
	m = pressDrain(t, m, "down")
	m = pressDrain(t, m, "down")
	if ov.ov.sel != 2 || !ov.ov.anchors[2].missing {
		t.Fatalf("sel %d missing %v: the failed load marks the anchor", ov.ov.sel, ov.ov.anchors[2].missing)
	}
	if !docsMissing(t, m, ov, 2) {
		t.Fatal("the store (the page) never heard the anchor is missing")
	}
	m = m.syncOverviews()
	if !ov.ov.anchors[2].missing {
		t.Fatal("a store sync cleared the ✗")
	}
}

// A file that was missing at the add and appears later: the steer's load
// clears the anchor's mark, here and in the store.
func TestSplitSteerToAFileThatAppearedClearsTheMark(t *testing.T) {
	t.Parallel()
	m := agentOverviewModelOf(t, 120, " See [c](c.txt:1).")
	sv := m.splitLayer()
	ov := sv.left.doc()
	if !ov.ov.anchors[2].missing || !docsMissing(t, m, ov, 2) {
		t.Fatal("c.txt was not there at the add: its anchor starts missing")
	}
	if err := os.WriteFile(filepath.Join(m.currentWorktree, "c.txt"), []byte("c1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = pressDrain(t, m, "down")
	m = pressDrain(t, m, "down")
	if ov.ov.sel != 2 || ov.ov.anchors[2].missing {
		t.Fatalf("sel %d missing %v: the load found the file", ov.ov.sel, ov.ov.anchors[2].missing)
	}
	if docsMissing(t, m, ov, 2) {
		t.Fatal("the store still says the anchor is missing")
	}
}

// Stacked, a file deleted after the add fails to load in the stream: its
// anchor is marked missing, here and in the store; a file that loads clears
// a mark an earlier check left.
func TestStackLoadOfADeletedFileMarksItsAnchor(t *testing.T) {
	t.Parallel()
	m := agentOverviewModelOf(t, 120, " See [c](c.txt:1).", "c.txt")
	sv := m.splitLayer()
	ov := sv.left.doc()
	if err := os.Remove(filepath.Join(m.currentWorktree, "c.txt")); err != nil {
		t.Fatal(err)
	}
	ov.ov.anchors[0].missing = true // an earlier check found a.txt gone; it is back
	m = pressDrain(t, m, "S")
	if _, ok := sv.right.(*stackPane); !ok {
		t.Fatalf("right %T, want the stack", sv.right)
	}
	if !ov.ov.anchors[2].missing || !docsMissing(t, m, ov, 2) {
		t.Fatalf("missing %v: the stream's failed load marks the anchor", ov.ov.anchors[2].missing)
	}
	if ov.ov.anchors[0].missing {
		t.Fatal("a.txt loaded: the stream's read clears its mark")
	}
}

// A deleted file shown in the split reloads on every store change (its
// notes re-align): the reload repeats nothing new, so it leaves the status
// line alone — "no file" is said when the mark changes.
func TestSplitReloadOfADeletedFileKeepsTheStatus(t *testing.T) {
	t.Parallel()
	m := agentOverviewModelOf(t, 120, " See [c](c.txt:1).", "c.txt")
	sv := m.splitLayer()
	if err := os.Remove(filepath.Join(m.currentWorktree, "c.txt")); err != nil {
		t.Fatal(err)
	}
	m = pressDrain(t, m, "down")
	m = pressDrain(t, m, "down")
	if want := i18n.T("no file %s", "c.txt"); m.statusMsg != want {
		t.Fatalf("status %q, want %q on the steer", m.statusMsg, want)
	}
	m.statusMsg = "something else"
	m = pumpAll(t, m, m.reloadDocCmd(sv.right.doc()))
	if m.statusMsg != "something else" {
		t.Fatalf("status %q: a reload that changed no mark rewrote the status", m.statusMsg)
	}
}

// A steer to an anchor already marked at the add still names the file: a
// first load says it whether or not the mark changes.
func TestSplitSteerToAnAlreadyMissingFileNamesIt(t *testing.T) {
	t.Parallel()
	m := agentOverviewModelOf(t, 120, " See [c](c.txt:1).")
	ov := m.splitLayer().left.doc()
	if !ov.ov.anchors[2].missing {
		t.Fatal("c.txt was not there at the add: its anchor starts missing")
	}
	m = pressDrain(t, m, "down")
	m = pressDrain(t, m, "down")
	if want := i18n.T("no file %s", "c.txt"); m.statusMsg != want {
		t.Fatalf("status %q, want %q", m.statusMsg, want)
	}
}

// The shown file deleted on disk: the watcher's reload flips the mark and
// says so once.
func TestSplitReloadThatFindsTheFileDeletedNamesIt(t *testing.T) {
	t.Parallel()
	m := agentOverviewModelOf(t, 120, " See [c](c.txt:1).", "c.txt")
	sv := m.splitLayer()
	m = pressDrain(t, m, "down")
	m = pressDrain(t, m, "down")
	if ov := sv.left.doc(); ov.ov.sel != 2 || ov.ov.anchors[2].missing {
		t.Fatalf("sel %d missing %v: c.txt loads found", ov.ov.sel, ov.ov.anchors[2].missing)
	}
	if err := os.Remove(filepath.Join(m.currentWorktree, "c.txt")); err != nil {
		t.Fatal(err)
	}
	m.statusMsg = ""
	m = pumpAll(t, m, m.reloadDocCmd(sv.right.doc()))
	if want := i18n.T("no file %s", "c.txt"); m.statusMsg != want || !sv.left.doc().ov.anchors[2].missing {
		t.Fatalf("status %q missing %v, want %q and the mark", m.statusMsg, sv.left.doc().ov.anchors[2].missing, want)
	}
}

// unreadable makes the worktree file name unreadable (permission denied)
// and restores it at the end. Windows (chmod 0 only sets read-only) and a
// root run read it anyway, so they skip.
func unreadable(t *testing.T, m Model, name string) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("the file stays readable: Windows or root")
	}
	p := filepath.Join(m.currentWorktree, name)
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
}

// A working file that is there but cannot be read is not a file that is
// gone: the split's failed read marks no ✗ (the store's check says present).
func TestSplitUnreadableFileIsNotMarkedMissing(t *testing.T) {
	t.Parallel()
	m := agentOverviewModelOf(t, 120, " See [c](c.txt:1).", "c.txt")
	ov := m.splitLayer().left.doc()
	unreadable(t, m, "c.txt")
	m = pressDrain(t, m, "down")
	m = pressDrain(t, m, "down")
	if ov.ov.sel != 2 || ov.ov.anchors[2].missing || docsMissing(t, m, ov, 2) {
		t.Fatalf("sel %d missing %v: an unreadable file is still there", ov.ov.sel, ov.ov.anchors[2].missing)
	}
}

// The same in the stack: a read failing for any reason but "not there"
// marks nothing.
func TestStackUnreadableFileIsNotMarkedMissing(t *testing.T) {
	t.Parallel()
	m := agentOverviewModelOf(t, 120, " See [c](c.txt:1).", "c.txt")
	sv := m.splitLayer()
	ov := sv.left.doc()
	unreadable(t, m, "c.txt")
	m = pressDrain(t, m, "S")
	sp, ok := sv.right.(*stackPane)
	if !ok {
		t.Fatalf("right %T, want the stack", sv.right)
	}
	if f := sp.stream.files[sp.fileIndex("c.txt")]; f.err == nil {
		t.Fatal("the read must fail: the test needs an unreadable file")
	}
	if ov.ov.anchors[2].missing || docsMissing(t, m, ov, 2) {
		t.Fatal("an unreadable file is still there: no ✗")
	}
}
