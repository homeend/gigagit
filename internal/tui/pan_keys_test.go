package tui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// longLine is far wider than any test terminal: what a reader pans along.
var longLine = "START " + strings.Repeat("abcdefghij", 40) + " END"

var (
	shiftLeft  = tea.KeyMsg{Type: tea.KeyShiftLeft}
	shiftRight = tea.KeyMsg{Type: tea.KeyShiftRight}
)

// pannedPreview is F's window with a long-line file previewed, the keys on
// the preview and its long lines in scroll mode, after one frame (the frame
// records how wide the text is, which bounds the pan).
func pannedPreview(t *testing.T) Model {
	t.Helper()
	m := settle(t, wtDiskWindow(t, map[string]string{"a.txt": longLine + "\nshort\n"}))
	m = fvKeys(t, m, keyMsg("right"))
	if m.filesTreeFocused || m.filesPreview == nil {
		t.Fatal("→ did not hand the keys to the preview")
	}
	m.filesPreview.p.mode = modeScroll
	_ = m.View()
	return m
}

// In F's preview → pans the long lines and ← pans them back — ← only ever
// pans (it used to hand the keys back to the list at once, with no way to
// pan at all); tab and esc go back.
func TestWorktreePreviewArrowsPanBothWays(t *testing.T) {
	t.Parallel()
	m := pannedPreview(t)
	step := m.hscrollStep()
	m = fvKeys(t, m, keyMsg("right"), keyMsg("right"), keyMsg("right"))
	if p := m.filesPreview.p; p.hscroll != 3*step || m.filesTreeFocused {
		t.Fatalf("→×3: hscroll %d (want %d), tree focused %v", p.hscroll, 3*step, m.filesTreeFocused)
	}
	m = fvKeys(t, m, keyMsg("left"))
	if p := m.filesPreview.p; p.hscroll != 2*step || m.filesTreeFocused {
		t.Fatalf("←: hscroll %d (want %d), tree focused %v — ← left the preview while panned", p.hscroll, 2*step, m.filesTreeFocused)
	}
	m = fvKeys(t, m, shiftLeft, keyMsg("left"))
	if p := m.filesPreview.p; p.hscroll != 0 || m.filesTreeFocused {
		t.Fatalf("back at column 0: hscroll %d, tree focused %v", p.hscroll, m.filesTreeFocused)
	}
	m = fvKeys(t, m, keyMsg("left"))
	if m.filesPreview.p.hscroll != 0 || m.filesTreeFocused {
		t.Fatalf("← at the first column: hscroll %d, tree focused %v — ← must only pan", m.filesPreview.p.hscroll, m.filesTreeFocused)
	}
	m = fvKeys(t, m, keyMsg("tab"))
	if !m.filesTreeFocused {
		t.Fatal("tab did not hand the keys back to the list")
	}
}

// → stops where the widest line's end reaches the right edge: panning on
// into blank columns would only cost as many ← presses to come back.
func TestWorktreePreviewPanStopsAtTheWidestLine(t *testing.T) {
	t.Parallel()
	m := pannedPreview(t)
	for i := 0; i < 200; i++ {
		m = fvKeys(t, m, keyMsg("right"))
	}
	p := m.filesPreview.p
	if p.hscroll <= 0 || p.hscroll >= len(longLine) {
		t.Fatalf("→×200: hscroll %d, want a stop inside the %d-column line", p.hscroll, len(longLine))
	}
	if !strings.Contains(m.View(), "END") {
		t.Fatal("at the stop the line's end is not on screen")
	}
}

// Outside scroll mode there is nothing to pan, and ← still does nothing
// else: esc hands the keys back.
func TestWorktreePreviewLeftOutsideScrollModeStays(t *testing.T) {
	t.Parallel()
	m := pannedPreview(t)
	m.filesPreview.p.mode = modeCutoff
	m = fvKeys(t, m, keyMsg("right"))
	if m.filesPreview.p.hscroll != 0 || m.filesTreeFocused {
		t.Fatalf("→ in cutoff mode: hscroll %d, tree focused %v", m.filesPreview.p.hscroll, m.filesTreeFocused)
	}
	m = fvKeys(t, m, keyMsg("left"))
	if m.filesTreeFocused {
		t.Fatal("← in cutoff mode left the preview")
	}
	m = fvKeys(t, m, keyMsg("esc"))
	if !m.filesTreeFocused {
		t.Fatal("esc did not hand the keys back to the list")
	}
}

// A commit's files view, preview focused: the arrows (and shift+arrows) pan
// the PREVIEW — shift+→ used to pan the file tree behind it.
func TestFilesViewPreviewArrowsPanThePreview(t *testing.T) {
	t.Parallel()
	m := pannedPreview(t)
	m.wtFiles = nil // the same preview, as a commit's files view shows it
	if m.inWorktreeFiles() {
		t.Skip("cannot leave F's window this way")
	}
	step := m.hscrollStep()
	m = fvKeys(t, m, keyMsg("right"), shiftRight)
	if p := m.filesPreview.p; p.hscroll != 2*step || m.filesView.hscroll != 0 || m.filesTreeFocused {
		t.Fatalf("→ shift+→: preview %d (want %d), tree %d, tree focused %v", p.hscroll, 2*step, m.filesView.hscroll, m.filesTreeFocused)
	}
	m = fvKeys(t, m, keyMsg("left"), keyMsg("left"))
	if m.filesPreview.p.hscroll != 0 || m.filesTreeFocused {
		t.Fatalf("←×2: preview %d, tree focused %v", m.filesPreview.p.hscroll, m.filesTreeFocused)
	}
	m = fvKeys(t, m, keyMsg("left"))
	if m.filesTreeFocused {
		t.Fatal("← at the first column left the preview — ← must only pan")
	}
	m = fvKeys(t, m, keyMsg("tab"))
	if !m.filesTreeFocused {
		t.Fatal("tab did not focus the tree")
	}
}

// The full-screen file viewer: plain ←/→ pan in scroll mode, as shift does.
func TestFileViewerArrowsPan(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	if err := os.WriteFile(filepath.Join(m.currentWorktree, "long.txt"), []byte(longLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.clipWrite = func(_ io.Writer, s string) (string, error) { return "fake", nil }
	c := steer.Command{ID: "fv", Cmd: "navigate", File: "long.txt",
		Target: &steer.Target{State: "unstaged"}, HintKind: model.ContentHintKind, HintID: model.ContentHintID, Wait: true}
	nm, cmd := m.applySteer(c)
	m = pumpAll(t, nm, cmd)
	fv, ok := m.topLayer().(*fileViewer)
	if !ok {
		t.Fatalf("top layer %T, want the file viewer", m.topLayer())
	}
	fv.p.mode = modeScroll
	_ = m.View()
	step := m.hscrollStep()
	m = fvKeys(t, m, keyMsg("right"), keyMsg("right"))
	if fv.p.hscroll != 2*step {
		t.Fatalf("→×2: hscroll %d, want %d", fv.p.hscroll, 2*step)
	}
	m = fvKeys(t, m, keyMsg("left"), keyMsg("left"), keyMsg("left"))
	if fv.p.hscroll != 0 || m.topLayer() != fv {
		t.Fatalf("←×3: hscroll %d, viewer still up %v", fv.p.hscroll, m.topLayer() == fv)
	}
}

// Blame: plain ←/→ pan in scroll mode, as shift does.
func TestBlameArrowsPan(t *testing.T) {
	t.Parallel()
	m, b := charBlameModel(longLine, "short")
	b.mode = modeScroll
	_ = b.render(m, "")
	step := m.hscrollStep()
	m = typeBlame(m, b, "right", "right")
	if b.hscroll != 2*step {
		t.Fatalf("→×2: hscroll %d, want %d", b.hscroll, 2*step)
	}
	m = typeBlame(m, b, "left")
	if b.hscroll != step {
		t.Fatalf("←: hscroll %d, want %d", b.hscroll, step)
	}
	_ = typeBlame(m, b, "left", "left")
	if b.hscroll != 0 {
		t.Fatalf("←×2 more: hscroll %d, want 0", b.hscroll)
	}
}

// The → stop is the widest line: measured once per document, not per press
// — a 20,000-line file re-measured on every → cost a frame each time.
func TestPanStopIsMeasuredOncePerDocument(t *testing.T) {
	t.Parallel()
	p := &contentPopup{mode: modeScroll, panW: 80}
	b := &blameView{mode: modeScroll}
	for i := 0; i < 20000; i++ {
		p.lines = append(p.lines, contentLine{text: longLine})
		b.lines = append(b.lines, model.BlameLine{Content: longLine})
	}
	m := Model{width: 120, height: 40}
	t0 := time.Now()
	for i := 0; i < 100; i++ {
		m.panPreview(p, 1)
		m.panPreview(p, -1)
		b.pan(m, 1)
		b.pan(m, -1)
	}
	if d := time.Since(t0); d > time.Second {
		t.Fatalf("200 → presses over 20,000 lines took %v", d)
	}
	// a new document (a reload) is measured again
	p.lines = []contentLine{{text: "short"}}
	p.hscroll = 0
	if m.panPreview(p, 1) {
		t.Fatal("a short reloaded file still panned to the old widest line")
	}
}
