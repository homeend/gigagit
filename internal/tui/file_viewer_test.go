package tui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		Target: &steer.Target{State: "unstaged"}, HintKind: model.ContentHintKind, HintID: model.ContentHintID, Wait: true}
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

// viewerAt lands a content link to a fresh file name/content at line (0 = no
// line) — the navigate `gg open gg://…/<name>:<line>?view=content` sends.
func viewerAt(t *testing.T, name, content string, line int) (Model, *fileViewer) {
	t.Helper()
	m := loadedNavModel(t)
	if err := os.WriteFile(filepath.Join(m.currentWorktree, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	c := steer.Command{ID: "fvl", Cmd: "navigate", File: name,
		Target: &steer.Target{State: "unstaged"}, HintKind: model.ContentHintKind, HintID: model.ContentHintID, Wait: true}
	if line > 0 {
		c.Line = &steer.Line{Side: "new", No: line}
	}
	nm, cmd := m.applySteer(c)
	nm = pumpAll(t, nm, cmd)
	fv := layerOf[*fileViewer](nm)
	if fv == nil {
		t.Fatal("no file viewer after a content-link navigate")
	}
	return nm, fv
}

func numberedLines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

func TestContentLinkLineLandsTheCursor(t *testing.T) {
	t.Parallel()
	m, fv := viewerAt(t, "long.txt", numberedLines(60), 40)
	rows, _ := fv.geom(m)
	if fv.p.cur != 39 {
		t.Fatalf("cursor = %d, want 39 (line 40)", fv.p.cur)
	}
	if fv.p.cur < fv.p.sel || fv.p.cur >= fv.p.sel+rows {
		t.Errorf("cursor %d outside the window [%d,%d)", fv.p.cur, fv.p.sel, fv.p.sel+rows)
	}
	if !strings.Contains(m.View(), "line 40") {
		t.Error("the screen does not show line 40")
	}
}

// The file may have shrunk since the link was copied: land on its last line
// and say so.
func TestContentLinkLinePastEOFClamps(t *testing.T) {
	t.Parallel()
	m, fv := viewerAt(t, "short.txt", numberedLines(5), 99)
	if fv.p.cur != 4 {
		t.Fatalf("cursor = %d, want 4 (the last line)", fv.p.cur)
	}
	if !strings.Contains(m.statusMsg, "line 99 is past the end of short.txt (5 lines)") {
		t.Errorf("statusMsg = %q, want the past-the-end notice", m.statusMsg)
	}
}

// A placeholder ("(empty file)") is not a line of the file.
func TestContentLinkLineOnEmptyFileIgnored(t *testing.T) {
	t.Parallel()
	m, fv := viewerAt(t, "empty.txt", "", 3)
	if fv.p.cur != 0 || strings.Contains(m.statusMsg, "past the end") {
		t.Fatalf("cursor=%d status=%q, want 0 and no notice", fv.p.cur, m.statusMsg)
	}
}

// A CRLF file has no phantom blank last line: the viewer's count is the one
// `gg link --content` checks a line against.
func TestFileViewerCRLFHasNoPhantomLine(t *testing.T) {
	t.Parallel()
	_, fv := viewerAt(t, "dos.txt", "a\r\nb\r\n", 0)
	if len(fv.p.lines) != 2 {
		t.Fatalf("lines = %d (%+v), want 2", len(fv.p.lines), fv.p.lines)
	}
}

// The viewer's own Copy file link points at the line under its cursor, so
// the link an agent is handed opens where the user was looking.
func TestFileViewerCopyFileLinkCarriesTheCursorLine(t *testing.T) {
	t.Parallel()
	m, _ := viewerModel(t)
	m = fvKeys(t, m, altDown(), altDown())
	m, copied := runFileLinkRow(t, m, true)
	if !strings.HasSuffix(copied, "/main.go:3?view=content") {
		t.Fatalf("copied %q, want …/main.go:3?view=content", copied)
	}
	if l, err := model.ParseLink(copied); err != nil || l.Line != 3 || !l.IsContent() {
		t.Fatalf("ParseLink(%q) = %+v, %v", copied, l, err)
	}
	_ = m
}

// The wheel pages the viewer like ↑/↓: the window moves, the cursor stays.
func TestFileViewerWheelScrolls(t *testing.T) {
	t.Parallel()
	m, fv := viewerAt(t, "long.txt", numberedLines(200), 0)
	tm, _ := m.Update(mouseMsg(10, 5, tea.MouseButtonWheelDown))
	m = tm.(Model)
	if want := m.wheelStep(); fv.p.sel != want || fv.p.cur != 0 {
		t.Fatalf("after one wheel-down sel=%d cur=%d, want sel=%d cur=0", fv.p.sel, fv.p.cur, want)
	}
}

// The agent that sent the link hears where the cursor landed — including
// when the file had fewer lines than the link named.
func TestContentLinkReplyReportsTheLandedLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		lines, line int
		want        string
	}{
		{60, 40, "opened long.txt at line 40"},
		{5, 99, "opened long.txt at line 5 (line 99 is past the end, 5 lines)"},
		{5, 0, "opened long.txt"},
	} {
		m, _ := viewerAt(t, "long.txt", numberedLines(tc.lines), tc.line)
		r, ok := steer.AwaitReply(m.steerDir, "fvl", time.Second)
		if !ok || !r.OK || r.Detail != tc.want {
			t.Errorf("line %d of %d: reply = %+v ok=%v, want detail %q", tc.line, tc.lines, r, ok, tc.want)
		}
	}
}

// A load that fails is a navigate that failed: the agent is told why.
func TestContentLinkReplyFailsWhenTheLoadFails(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	dir := filepath.Join(m.currentWorktree, "adir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := steer.Command{ID: "fvf", Cmd: "navigate", File: "adir",
		Target: &steer.Target{State: "unstaged"}, HintKind: model.ContentHintKind, HintID: model.ContentHintID, Wait: true}
	nm, cmd := m.applySteer(c)
	nm = pumpAll(t, nm, cmd)
	r, ok := steer.AwaitReply(nm.steerDir, "fvf", time.Second)
	if !ok || r.OK {
		t.Fatalf("reply = %+v ok=%v, want a failure", r, ok)
	}
}

// Two files opened in a row: the first viewer is covered by the second when
// its load arrives, and must still be filled (not dropped as stale).
func TestCoveredViewerStillFills(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m, _ = m.openFileViewer("a.txt", 0)
	a := layerOf[*fileViewer](m)
	m, _ = m.openFileViewer("b.txt", 0)
	b := layerOf[*fileViewer](m)
	if a == b {
		t.Fatal("the second open did not push a second viewer")
	}
	tm, _ := m.Update(fileContentMsg{tag: a.tag, lines: fileContentLinesTok([]byte("AAA\n"), nil)})
	m = tm.(Model)
	if len(a.p.lines) != 1 || a.p.lines[0].raw != "AAA" {
		t.Errorf("covered viewer shows %+v, want its loaded line", a.p.lines)
	}
	if b.p.lines[0].src {
		t.Errorf("the top viewer was filled with another file's lines: %+v", b.p.lines)
	}
}
