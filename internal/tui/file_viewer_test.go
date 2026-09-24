package tui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

const viewerGo = "package main\n\nfunc main() {\n\tprintln(\"DISK\")\n}\n"

// viewerModel is loadedNavModel with an untracked main.go on disk, landed
// through a content link: the state every fileViewer test starts from.
func viewerModel(t *testing.T) (Model, *string) {
	t.Helper()
	m := loadedNavModel(t)
	if err := os.WriteFile(filepath.Join(m.currentWorktree, "main.go"), []byte(viewerGo), 0o644); err != nil {
		t.Fatal(err)
	}
	copied := new(string)
	m.clipWrite = func(_ io.Writer, s string) (string, error) { *copied = s; return "fake", nil }
	c := steer.Command{ID: "fv", Cmd: "navigate", File: "main.go",
		Target: &steer.Target{State: "unstaged"}, HintKind: model.ContentHintKind, HintID: model.ContentHintID}
	nm, cmd := m.applySteer(c)
	return pumpAll(t, nm, cmd), copied
}

func fvKeys(t *testing.T, m Model, msgs ...tea.KeyMsg) Model {
	t.Helper()
	for _, k := range msgs {
		tm, cmd := m.Update(k)
		m = pumpAll(t, tm.(Model), cmd)
	}
	return m
}

func altDown() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyDown, Alt: true} }

func TestContentLinkLandsInTheFileViewer(t *testing.T) {
	t.Parallel()
	m, _ := viewerModel(t)
	fv, ok := m.topLayer().(*fileViewer)
	if !ok {
		t.Fatalf("top layer = %T, want *fileViewer", m.topLayer())
	}
	var text []string
	coloured := false
	for _, l := range fv.p.lines {
		text = append(text, l.raw)
		if len(l.cls) > 0 {
			coloured = true
		}
	}
	if strings.Join(text, "\n") != strings.TrimRight(viewerGo, "\n") {
		t.Errorf("viewer lines = %q, want the disk text", text)
	}
	if !coloured {
		t.Error("no line carries a syntax class mask: the viewer is not colouring")
	}
	v := m.View()
	if !strings.Contains(v, "View main.go (working tree)") || !strings.Contains(v, `println("DISK")`) {
		t.Errorf("the screen does not show the viewer:\n%s", v)
	}
}

func TestFileViewerSelectsAndCopiesLines(t *testing.T) {
	t.Parallel()
	m, copied := viewerModel(t)
	// cursor on line 1 → mark, down twice, mark end, enter copies lines 1..3
	m = fvKeys(t, m, keyType(tea.KeySpace), altDown(), altDown(), keyType(tea.KeySpace), keyType(tea.KeyEnter))
	if want := "package main\n\nfunc main() {"; *copied != want {
		t.Fatalf("copied %q, want %q", *copied, want)
	}
}

func TestFileViewerSearchLandsTheCursor(t *testing.T) {
	t.Parallel()
	m, _ := viewerModel(t)
	m = fvKeys(t, m, key("/"), key("DISK"), keyType(tea.KeyEnter))
	fv := m.topLayer().(*fileViewer)
	if fv.p.cur != 3 {
		t.Fatalf("cursor on line %d, want 4 (the hit)", fv.p.cur+1)
	}
}

func TestFileViewerActionMenuIsTheViewersOwn(t *testing.T) {
	t.Parallel()
	m, _ := viewerModel(t)
	rows := availableActions(m)
	ids := map[string]bool{}
	for _, r := range rows {
		ids[r.id] = true
		// The viewer's menu is about the file and its lines: copy rows and the
		// file's bookmark/shelf rows. A panel's rows (the Commits panel's
		// "Show as list", a Files row's stage) must not leak in from beneath.
		if !strings.HasPrefix(r.id, "copy-") && !strings.HasPrefix(r.id, "bookmark-") && !strings.HasPrefix(r.id, "shelf-") {
			t.Errorf("row %q (%s) leaked into the viewer's . menu", r.id, r.label)
		}
	}
	for _, want := range []string{"copy-line", "copy-file-path", "copy-link", "copy-file-link"} {
		if !ids[want] {
			t.Errorf("viewer . menu lacks %q (have %v)", want, ids)
		}
	}
	link, _ := rowByID(rows, "copy-link")
	if !strings.HasSuffix(link.copyText, "/main.go") {
		t.Errorf("Copy link = %q, want the working-tree main.go", link.copyText)
	}
}

func TestFileViewerEscReturnsAndSwallowsKeys(t *testing.T) {
	t.Parallel()
	m, _ := viewerModel(t)
	before := m.focus
	m = fvKeys(t, m, key("p"), key("s")) // global keys: must do nothing here
	if _, ok := m.topLayer().(*fileViewer); !ok || m.running {
		t.Fatalf("a global key escaped the viewer (top=%T running=%v)", m.topLayer(), m.running)
	}
	m = fvKeys(t, m, keyType(tea.KeyEsc))
	if m.topLayer() != nil || m.focus != before {
		t.Fatalf("esc left top=%T focus=%v, want the panels back", m.topLayer(), m.focus)
	}
}

func TestFileViewerFitsAndBacksAPopup(t *testing.T) {
	t.Parallel()
	m, _ := viewerModel(t)
	m.width, m.height = 80, 24
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("line %d is %d wide, want ≤ %d", i, w, m.width)
		}
	}
	if n := len(strings.Split(m.View(), "\n")); n > m.height {
		t.Errorf("%d lines, want ≤ %d", n, m.height)
	}
	m = m.pushLayer(newContentPopup("probe", []contentLine{{text: "over the viewer"}}))
	v := m.View()
	if !strings.Contains(v, "over the viewer") || !strings.Contains(v, "View main.go") {
		t.Error("a popup over the viewer must composite onto it (isFullScreenLayer)")
	}
}
