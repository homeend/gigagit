package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

var enterKey = tea.KeyMsg{Type: tea.KeyEnter}

// typeJump presses : then each digit of n, without enter.
func typeJump(t *testing.T, m Model, digits string) Model {
	t.Helper()
	m = fvKeys(t, m, runeKey(":"))
	for _, r := range digits {
		m = fvKeys(t, m, runeKey(string(r)))
	}
	return m
}

func TestJumpViewerLands(t *testing.T) {
	t.Parallel()
	m, fv := viewerAt(t, "long.txt", numberedLines(50), 0)
	m = fvKeys(t, typeJump(t, m, "30"), enterKey)
	rows, _ := fv.geom(m)
	if fv.p.cur != 29 {
		t.Fatalf("cursor = %d, want 29 (line 30)", fv.p.cur)
	}
	if fv.p.cur < fv.p.sel || fv.p.cur >= fv.p.sel+rows {
		t.Errorf("cursor %d outside the window [%d,%d)", fv.p.cur, fv.p.sel, fv.p.sel+rows)
	}
	if fv.p.jump.typing {
		t.Error("enter must close the prompt")
	}
}

func TestJumpViewerPastEnd(t *testing.T) {
	t.Parallel()
	m, fv := viewerAt(t, "long.txt", numberedLines(50), 0)
	m = fvKeys(t, typeJump(t, m, "99"), enterKey)
	if fv.p.cur != 49 {
		t.Fatalf("cursor = %d, want 49 (the last line)", fv.p.cur)
	}
	if !strings.Contains(m.statusMsg, "line 99 is past the end of long.txt (50 lines)") {
		t.Errorf("statusMsg = %q, want the past-the-end notice", m.statusMsg)
	}
}

func TestJumpViewerEscKeepsPlace(t *testing.T) {
	t.Parallel()
	m, fv := viewerAt(t, "long.txt", numberedLines(50), 11)
	cur, sel := fv.p.cur, fv.p.sel
	m = fvKeys(t, typeJump(t, m, "4"), tea.KeyMsg{Type: tea.KeyEsc})
	if fv.p.cur != cur || fv.p.sel != sel {
		t.Fatalf("esc moved the view: cur %d→%d sel %d→%d", cur, fv.p.cur, sel, fv.p.sel)
	}
	if fv.p.jump.typing {
		t.Fatal("esc must close the prompt")
	}
	if m.topLayer() != fv {
		t.Fatal("esc on the prompt must not close the viewer")
	}
}

// While a search is typed, : is query text.
func TestJumpColonIsSearchText(t *testing.T) {
	t.Parallel()
	m, fv := viewerAt(t, "long.txt", numberedLines(50), 0)
	m = fvKeys(t, m, runeKey("/"), runeKey(":"))
	if fv.p.search.query != ":" || fv.p.jump.typing {
		t.Fatalf("query %q jump typing %v, want \":\" and no jump", fv.p.search.query, fv.p.jump.typing)
	}
	_ = m
}

// A placeholder ("(empty file)") is not a file's line: : opens nothing.
func TestJumpPlaceholderNoop(t *testing.T) {
	t.Parallel()
	m, fv := viewerAt(t, "empty.txt", "", 0)
	m = fvKeys(t, m, runeKey(":"))
	if fv.p.jump.typing {
		t.Fatal(": opened a prompt over a placeholder")
	}
	m = fvKeys(t, m, runeKey("3"), enterKey)
	if fv.p.cur != 0 {
		t.Fatalf("cursor moved to %d", fv.p.cur)
	}
}

func TestJumpBadge(t *testing.T) {
	t.Parallel()
	m, _ := viewerAt(t, "long.txt", numberedLines(50), 0)
	m = typeJump(t, m, "7")
	if !strings.Contains(ansi.Strip(m.View()), ":7█") {
		t.Fatalf("the title does not show the prompt:\n%s", ansi.Strip(m.View()))
	}
}

// In the split's diff a line the diff shows is landed in place; one it folds
// away brings the file back (D) at that line.
func TestJumpSplitDiffShownAndHidden(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	d := sv.right.doc()
	m = pressDrain(t, m, "D")
	m = pressDrain(t, m, "f") // only the changed lines: a.txt's line 12 is folded
	if d.diff == nil || !d.diff.partial || rowOfNew(d.diff.rows, 12) >= 0 {
		t.Fatalf("fixture: want a partial diff hiding line 12, got %+v", d.diff)
	}
	m = fvKeys(t, typeJump(t, m, "3"), enterKey)
	if d.diff == nil || rowNewLine(d.diff.rows, d.diff.p.cur) != 3 || d.diff.rows[d.diff.p.cur].kind == diffDel {
		t.Fatalf("shown line 3: diff %v cursor row %+v", d.diff != nil, d.diff.rows[d.diff.p.cur])
	}
	m = fvKeys(t, typeJump(t, m, "12"), enterKey)
	if d.diff != nil {
		t.Fatal("a folded line brings the file back")
	}
	if d.p.cur != 11 {
		t.Fatalf("file cursor on line %d, want 12", d.p.cur+1)
	}
	_ = m
}

func TestJumpSplitPaneCaptures(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	m = fvKeys(t, m, runeKey(":"))
	p, ok := sv.right.(*docPane)
	if !ok {
		t.Fatalf("right pane is %T", sv.right)
	}
	if !p.captures(runeKey("4")) || !p.captures(runeKey("\t")) {
		t.Fatal("the pane must capture every key while the jump is typed")
	}
	_ = m
}

// A diff showing only a notice ("binary file — D shows the file") shows no
// line at all: the jump brings the file back (D) at that line.
func TestJumpNoticeDiffBringsTheFile(t *testing.T) {
	t.Parallel()
	m, sv := diffSplitModel(t)
	d := sv.right.doc()
	m = pressDrain(t, m, "D")
	if d.diff == nil {
		t.Fatal("fixture: D shows the diff")
	}
	d.diff.notice = "binary file — D shows the file"
	m = fvKeys(t, typeJump(t, m, "12"), enterKey)
	if d.diff != nil {
		t.Fatal("the jump must bring the file back over a notice")
	}
	if d.p.cur != 11 {
		t.Fatalf("file cursor on line %d, want 12", d.p.cur+1)
	}
	_ = m
}

// The viewer's exits survive a 100-column terminal, whatever leads the hint —
// a file opened from an overview carries [bksp] back, the anchors and D — and
// no two keys share one word: [:] is the line jump, alt+↑↓ the cursor.
func TestViewerHintKeepsExitsAt100(t *testing.T) {
	t.Parallel()
	hintOf := func(m Model) string {
		for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
			if strings.Contains(l, "[alt+↑↓]") {
				return l
			}
		}
		t.Fatalf("no hint line:\n%s", ansi.Strip(m.View()))
		return ""
	}
	m, _ := viewerAt(t, "long.txt", numberedLines(500), 0)
	m.width = 100
	if h := hintOf(m); !strings.Contains(h, "[esc] close") || !strings.Contains(h, "[alt+↑↓] cursor") {
		t.Errorf("plain viewer hint %q", h)
	}
	m = loadedNavModel(t)
	d := storeOverview(t, m, "Tour", "# Tour\n\n- [line twelve](a.txt:12)\n")
	m, cmd := inViewer(m, d)
	m = pumpAll(t, m, cmd)
	m.width = 100
	m = openNth(t, m, d, 0)
	if h := hintOf(m); !strings.Contains(h, "[bksp] back") || !strings.Contains(h, "[esc] background") || !strings.Contains(h, "[X] close") {
		t.Errorf("overview-opened viewer hint %q", h)
	}
}
