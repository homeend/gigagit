package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/steer"
)

// diskDoc is an n-line working-tree file that exists on disk AND is open
// (loaded, in the background) with the same content.
func diskDoc(t *testing.T, m Model, name string, n int) *openFile {
	t.Helper()
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(m.currentWorktree, name), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	d := newOpenFile(fileSource{kind: srcWorktree}, name)
	d.fill(fileContentMsg{lines: docLines(n)}, 10, 80)
	m.adoptDoc(d)
	m.openFiles.touch(m.currentWorktree, d, m.docShown)
	return d
}

func numbered(prefix string, n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%s%d\n", prefix, i)
	}
	return b.String()
}

// A note taller than the window is cut to it — the line it hangs under and
// the box's bottom rule stay reachable — and enter opens it in full.
func TestTallFileNoteIsCutToTheWindowAndOpensInFull(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	if _, err := d.addNote(2, 2, "tall one", numbered("r", 80), ""); err != nil {
		t.Fatal(err)
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "more lines") || strings.Contains(out, "r80") {
		t.Fatalf("an 80-line note is not cut to the window:\n%s", out)
	}
	if !strings.Contains(out, "line 3") {
		t.Fatalf("the tall box pushed the next line out of a window it should share:\n%s", out)
	}
	m = fvKeys(t, m, key("}"), keyType(tea.KeyEnter))
	cp, ok := m.topLayer().(*contentPopup)
	if !ok {
		t.Fatalf("enter on a noted line opened %T, want the full note", m.topLayer())
	}
	var all strings.Builder
	for _, l := range cp.lines {
		all.WriteString(l.text + "\n")
	}
	if !strings.Contains(all.String(), "tall one") || !strings.Contains(all.String(), "r80") {
		t.Fatalf("the full-note window lacks the text:\n%s", all.String())
	}
}

// } walks every annotated file, in a stable order, whatever the MRU order
// of the open-files list becomes on the way.
func TestBraceKeyReachesEveryAnnotatedFile(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	a, b, c := diskDoc(t, m, "x1.txt", 10), diskDoc(t, m, "x2.txt", 10), diskDoc(t, m, "x3.txt", 10)
	for _, d := range []*openFile{a, b, c} {
		if _, err := d.addNote(3, 3, "in "+d.path, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	m.openFiles.touch(m.currentWorktree, a, m.docShown)
	m = m.pushLayer(&fileViewer{a})
	m = fvKeys(t, m, key("}")) // a's own note
	var seen []string
	for i := 0; i < 3; i++ {
		m = fvKeys(t, m, key("}"))
		fv, ok := m.topLayer().(*fileViewer)
		if !ok {
			t.Fatalf("step %d: top is %T", i, m.topLayer())
		}
		if fv.p.cur != 2 {
			t.Fatalf("step %d: %s cursor on line %d, want 3 (its note)", i, fv.path, fv.p.cur+1)
		}
		seen = append(seen, fv.path)
	}
	if got := strings.Join(seen, " "); got != "x2.txt x3.txt x1.txt" {
		t.Fatalf("} visited %q, want x2.txt x3.txt x1.txt", got)
	}
}

// A note is placed on the file as it is on disk NOW, not on the copy gg
// read before the agent's last edit.
func TestNoteAddReadsTheDiskFirstWhenItChanged(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	nm, cmd := m.applySteer(bgNav("s-0", "a.txt", 0))
	nm = pumpAll(t, nm, cmd)
	path := filepath.Join(nm.currentWorktree, "a.txt")
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append([]byte("NEW TOP\n"), old...), 0o644); err != nil {
		t.Fatal(err)
	}
	nm, cmd = nm.applySteer(noteAddCmd("s-1", "a.txt", 1, 1, "on the new first line"))
	nm = pumpAll(t, nm, cmd)
	r, ok := steer.AwaitReply(nm.steerDir, "s-1", time.Second)
	d := nm.openFiles.find(nm.currentWorktree, docKey(fileSource{kind: srcWorktree}, "a.txt"))
	if !ok || !r.OK || d == nil || len(d.notes) != 1 {
		t.Fatalf("reply=%+v ok=%v doc=%v", r, ok, d)
	}
	if n := d.notes[0]; n.Start != 1 || len(d.docs.NoteText(n.ID)) != 1 || d.docs.NoteText(n.ID)[0] != "NEW TOP" {
		t.Fatalf("note = line %d text %q, want line 1 on the disk's NEW TOP", n.Start, d.docs.NoteText(n.ID))
	}
}

// Every preview scroll site clamps through clampTop (which counts note
// rows); previewClamp is its plain-arithmetic core and nothing else's.
func TestPreviewScrollSitesUseClampTop(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "file_preview.go" || f == "preview_select.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), "previewClamp(") {
			t.Errorf("%s calls previewClamp directly — a note under the last line cannot be scrolled into view there; use p.clampTop / p.scrollBy", f)
		}
	}
}

// A page down never skips lines the window had no room for.
func TestPageDownStopsAtTheFirstLineNotYetShown(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	d := newOpenFile(fileSource{kind: srcWorktree}, "long.txt") // far longer than the window
	d.fill(fileContentMsg{lines: docLines(200)}, 10, 80)
	m.adoptDoc(d)
	m.openFiles.touch(m.currentWorktree, d, m.docShown)
	m = m.pushLayer(&fileViewer{d})
	if _, err := d.addNote(2, 2, "tall", numbered("r", 80), ""); err != nil {
		t.Fatal(err)
	}
	shown := 0 // the last file line the first window shows
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if i := strings.Index(l, "line "); i >= 0 {
			fmt.Sscanf(l[i:], "line %d", &shown)
		}
	}
	if shown < 3 || shown > 100 {
		t.Fatalf("the first window shows up to line %d — the fixture is off", shown)
	}
	m = fvKeys(t, m, keyType(tea.KeyPgDown))
	if d.p.sel != shown {
		t.Fatalf("page down put line %d on top, want %d — the first line not yet shown", d.p.sel+1, shown+1)
	}
	m = fvKeys(t, m, keyType(tea.KeyPgUp))
	if d.p.sel != 0 {
		t.Fatalf("page up returned to line %d, want 1", d.p.sel+1)
	}
}

// The note keys never push the exits off the hint line.
func TestNoteHintKeepsTheExitsOnAnEightyColumnTerminal(t *testing.T) {
	t.Parallel()
	m, d := notedViewer(t)
	if _, err := d.addNote(1, 1, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 80, 24
	out := ansi.Strip(m.View())
	for _, want := range []string{"[}/{] notes", "[esc] background", "[X] close"} {
		if !strings.Contains(out, want) {
			t.Errorf("an 80-column viewer's hint lacks %q:\n%s", want, out)
		}
	}
}
