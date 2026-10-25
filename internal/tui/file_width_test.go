package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/workspace"
)

func TestFileColumn(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ w, gut, fw, col, margin int }{
		{236, 0, 120, 120, 58}, // capped, centred
		{236, 2, 120, 122, 57}, // the gutter comes on top of the text
		{100, 0, 120, 100, 0},  // narrower than the cap: the whole width
		{236, 0, -1, 236, 0},   // unlimited
		{120, 0, 120, 120, 0},  // exactly the cap
	} {
		if col, margin := fileColumn(c.w, c.gut, c.fw); col != c.col || margin != c.margin {
			t.Errorf("fileColumn(%d, %d, %d) = %d, %d; want %d, %d", c.w, c.gut, c.fw, col, margin, c.col, c.margin)
		}
	}
}

// One file shown alone — the full-screen viewer, the files view's preview —
// lays its text in a centred [ui] file_width column; the frame keeps its
// size. A split pane, prose and -1 keep their own widths.
func TestPreviewCodeUsesFileWidth(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.width, m.height = 240, 30
	m.cfg.UI.FileWidth = 60
	p := &contentPopup{lines: []contentLine{{text: strings.Repeat("code_token(x) + ", 40)}}, mode: modeWrap}
	for _, viewer := range []bool{true, false} {
		box := m.renderPreviewBoxOpts(p, "x.go", 240, 30, previewBoxOpts{focused: true, viewer: viewer, single: true})
		lo, hi := textSpan(t, rowsOnly(box, viewer)) // the title and keys keep the frame
		if hi-lo > 60 || lo < 80 {
			t.Fatalf("viewer=%v: code spans %d..%d; want ≤60 wide and centred", viewer, lo, hi)
		}
		if w := lipgloss.Width(strings.Split(box, "\n")[0]); w != 240 {
			t.Fatalf("viewer=%v: the frame must keep its full width, got %d", viewer, w)
		}
	}
	// A split pane (no single) keeps the whole box.
	if lo, hi := textSpan(t, m.renderPreviewBoxOpts(p, "x.go", 240, 30, previewBoxOpts{focused: true})); hi-lo <= 60 {
		t.Fatalf("a split pane must keep its width, spans %d..%d", lo, hi)
	}
	// Unlimited: the whole frame.
	m.cfg.UI.FileWidth = -1
	if lo, hi := textSpan(t, m.renderPreviewBoxOpts(p, "x.go", 240, 30, previewBoxOpts{focused: true, viewer: true, single: true})); hi-lo <= 200 {
		t.Fatalf("-1 must use the whole frame, spans %d..%d", lo, hi)
	}
}

// rowsOnly is box without its title line (and, in the viewer, its key line):
// the text rows alone.
func rowsOnly(box string, viewer bool) string {
	ls := strings.Split(box, "\n")
	// ls[0] is the top border, ls[1] the title; the viewer's keys sit above the bottom border
	out := append([]string{ls[0]}, ls[2:]...)
	if viewer {
		out = append(out[:len(out)-2], out[len(out)-1])
	}
	return strings.Join(out, "\n")
}

// The keys' geometry is the drawn column: the cursor, wrap and go-to-line
// count the capped width, never the frame's.
func TestActivePreviewUsesFileWidth(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.width, m.height = 240, 30
	m.cfg.UI.FileWidth = 60
	fv := &fileViewer{openFile: &openFile{p: &contentPopup{lines: []contentLine{{text: "x"}}}}}
	m = m.pushLayer(fv)
	if _, _, innerW, ok := m.activePreview(); !ok || innerW != 60 {
		t.Fatalf("activePreview innerW = %d (%v), want 60", innerW, ok)
	}
}

// Blame: the gutter + │ + at most file_width columns of code, centred.
func TestBlameUsesFileWidth(t *testing.T) {
	t.Parallel()
	m := Model{width: 240, height: 20}
	m.cfg.UI.FileWidth = 60
	b := &blameView{ctx: navContext{path: "a.go"}, tag: "blame::a.go", mode: modeWrap}
	b.lines = []model.BlameLine{{Hash: "aaaaaaa", Author: "Ada", Time: 1, LineNo: 1, Content: strings.Repeat("word ", 60) + "END"}}
	b.blocks = groupBlame(b.lines)
	m = m.pushLayer(b)
	out := plain(b.render(m, ""))
	lines := strings.Split(out, "\n")
	lo, hi := 1<<30, -1
	for _, l := range lines[1 : len(lines)-1] {
		s := strings.TrimRight(l, " ")
		if s == "" {
			continue
		}
		lo = min(lo, len([]rune(s))-len([]rune(strings.TrimLeft(s, " "))))
		hi = max(hi, len([]rune(s)))
	}
	if hi-lo > blameGutterW+1+60 || lo < 70 {
		t.Fatalf("blame spans %d..%d; want ≤%d wide and centred:\n%s", lo, hi, blameGutterW+1+60, out)
	}
	if !strings.Contains(out, "END") {
		t.Fatalf("the long line must wrap inside the column:\n%s", out)
	}
	if got := b.codeWidth(m); got != 60 {
		t.Fatalf("codeWidth = %d, want 60 (the selection and search pan count the capped column)", got)
	}
}

// Settings → File width: enter opens the row's field, enter saves the
// GLOBAL [ui] file_width and the views take it at once; a width the views
// cannot use (below 20, 0) is refused and nothing is written.
func TestSettingsFileWidthEdits(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := New(domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()}))
	idx := -1
	for i, e := range settingsMenu {
		if e == settingsMenuFileWidth {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("File width is missing from the Settings menu")
	}
	if got := settingsMenuLabel(m, idx); !strings.Contains(got, "200 characters") {
		t.Fatalf("the default label = %q, want 200 characters", got)
	}
	m, _ = m.openSettings()
	p, ok := m.topLayer().(*settingsPopup)
	if !ok {
		t.Fatal("Settings did not open")
	}
	edit := func(m Model, typed string) Model {
		p.menuSel = idx
		m, _ = p.update(m, keyMsg("enter"))
		if !p.fileWidthEditing {
			t.Fatal("enter on File width must open its field")
		}
		for range 5 {
			m, _ = p.update(m, keyMsg("backspace"))
		}
		m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(typed)})
		m, _ = p.update(m, keyMsg("enter"))
		return m
	}
	m = edit(m, "120")
	cfg, err := config.Load(config.DefaultGlobalPath(), "")
	if err != nil || cfg.UI.FileWidthCols() != 120 {
		t.Fatalf("global file_width = %d (%v), want 120", cfg.UI.FileWidthCols(), err)
	}
	if m.fileWidth() != 120 || !strings.Contains(settingsMenuLabel(m, idx), "120 characters") {
		t.Fatalf("the views must take it at once: fileWidth %d, label %q", m.fileWidth(), settingsMenuLabel(m, idx))
	}
	m = edit(m, "7")
	if cfg, _ := config.Load(config.DefaultGlobalPath(), ""); cfg.UI.FileWidthCols() != 120 || m.fileWidth() != 120 {
		t.Fatalf("7 must be refused: global %d, views %d", cfg.UI.FileWidthCols(), m.fileWidth())
	}
	if !strings.Contains(m.statusMsg, "at least 20") {
		t.Fatalf("a refused width says why, got %q", m.statusMsg)
	}
	m = edit(m, "-1")
	if m.fileWidth() != -1 || !strings.Contains(settingsMenuLabel(m, idx), "unlimited") {
		t.Fatalf("-1: fileWidth %d, label %q", m.fileWidth(), settingsMenuLabel(m, idx))
	}
	// esc closes the field without writing.
	p.menuSel = idx
	m, _ = p.update(m, keyMsg("enter"))
	m, _ = p.update(m, keyMsg("esc"))
	if p.fileWidthEditing || m.topLayer() != p {
		t.Fatal("esc must close the field, not Settings")
	}
}

// In a workspace [ui] is process-wide: a saved width reaches every member
// and survives the next per-member reload (a branch-filter save, a tools
// write), which merges the workspace's [ui] back over the member.
func TestSaveFileWidthInWorkspace(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := loadedModel(t)
	home := m.homeRepo()
	other := secondRepo(t)
	e := workspace.Entry{Name: "shop", Repos: []string{home.mainPath, other}}
	nm, cmd := m.openWorkspace(e)
	m = settleWorkspace(t, nm, cmd)
	m = m.saveFileWidth(80)
	m = m.reloadOwnConfig()
	if m.fileWidth() != 80 {
		t.Fatalf("after a member reload the width = %d, want 80", m.fileWidth())
	}
	if b := m.ws.members[1]; b.cfg.UI.FileWidthCols() != 80 {
		t.Fatalf("the other member's width = %d, want 80", b.cfg.UI.FileWidthCols())
	}
}

// A column one cell narrower than its frame has no margin, yet every line
// still fills the frame.
func TestCentreBlockPadsWithoutMargin(t *testing.T) {
	t.Parallel()
	lines := []string{"abc", "de"}
	centreBlock(lines, 0, 4)
	for _, l := range lines {
		if lipgloss.Width(l) != 4 {
			t.Fatalf("lines = %q, want each 4 wide", lines)
		}
	}
}

// A narrow column never cuts the way out: the viewer's and blame's title and
// keys keep the frame's width.
func TestNarrowFileWidthKeepsTitleAndKeys(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.width, m.height = 240, 30
	m.cfg.UI.FileWidth = 20
	p := &contentPopup{lines: []contentLine{{text: strings.Repeat("x", 100)}}, mode: modeCutoff}
	box := plain(m.renderPreviewBoxOpts(p, "a/very/long/path/to/the/file.go", 240, 30, previewBoxOpts{focused: true, viewer: true, single: true}))
	if !strings.Contains(box, "a/very/long/path/to/the/file.go") || !strings.Contains(box, "[ctrl+w] view") {
		t.Fatalf("the title or the keys were cut to the 20-column text:\n%s", box)
	}
	bm := Model{width: 240, height: 20}
	bm.cfg.UI.FileWidth = 20
	b := &blameView{ctx: navContext{path: "a.go"}, tag: "blame::a.go"}
	b.lines = []model.BlameLine{{Hash: "aaaaaaa", Author: "Ada", Time: 1, LineNo: 1, Content: "x"}}
	b.blocks = groupBlame(b.lines)
	bm = bm.pushLayer(b)
	if out := plain(b.render(bm, "")); !strings.Contains(out, "[esc/b] back") {
		t.Fatalf("blame's keys lost the way out:\n%s", out)
	}
}
