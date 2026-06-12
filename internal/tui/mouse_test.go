package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/model"
)

// mouseModel is markModel sized 80x24: leftW=26, three left boxes of height 7
// at y=1/8/15, Commits 26..79 full body height.
func mouseModel() Model {
	m := markModel()
	m.width, m.height = 80, 24
	return m
}

func TestPanelAt(t *testing.T) {
	m := mouseModel()
	cases := []struct {
		x, y int
		want panel
		ok   bool
	}{
		{0, 1, panelBranches, true},  // top-left border cell
		{5, 4, panelBranches, true},  // data area
		{0, 8, panelWorktrees, true}, // second box top
		{0, 15, panelStatus, true},   // third box top
		{25, 21, panelStatus, true},  // bottom-right of the left column
		{26, 1, panelCommits, true},  // commits left edge
		{79, 21, panelCommits, true}, // commits bottom-right
		{5, 0, 0, false},             // header row
		{5, 22, 0, false},            // footer row
		{5, 23, 0, false},            // status row
	}
	for _, c := range cases {
		p, ok := m.panelAt(c.x, c.y)
		if ok != c.ok || (ok && p != c.want) {
			t.Errorf("panelAt(%d,%d) = %v,%v want %v,%v", c.x, c.y, p, ok, c.want, c.ok)
		}
	}
}

func TestPanelAtNarrowTerminal(t *testing.T) {
	m := mouseModel()
	m.width = 30 // single commits column
	if p, ok := m.panelAt(5, 5); !ok || p != panelCommits {
		t.Fatalf("panelAt = %v,%v, want commits on a narrow terminal", p, ok)
	}
}

func TestPanelRowAt(t *testing.T) {
	m := mouseModel() // branches box: border y=1, label y=2, data y=3..6
	if idx, ok := m.panelRowAt(panelBranches, 3); !ok || idx != 0 {
		t.Fatalf("row at y=3 = %d,%v, want 0,true", idx, ok)
	}
	if idx, ok := m.panelRowAt(panelBranches, 4); !ok || idx != 1 {
		t.Fatalf("row at y=4 = %d,%v, want 1,true", idx, ok)
	}
	if _, ok := m.panelRowAt(panelBranches, 2); ok {
		t.Fatal("the label line must not map to a row")
	}
	if _, ok := m.panelRowAt(panelBranches, 6); ok {
		t.Fatal("padding below the last row (3 branches) must not map") // rows y=3,4,5 hold the 3 branches
	}
}

func TestPanelRowAtScrolledPanel(t *testing.T) {
	m := mouseModel()
	m.branches = nil
	for i := 0; i < 30; i++ {
		m.branches = append(m.branches, model.Branch{Name: string(rune('a'+i%26)) + "-br"})
	}
	m.sel[panelBranches] = 20 // branches rowsCap = 7-3 = 4; windowStart(30,4,20)=18
	if idx, ok := m.panelRowAt(panelBranches, 3); !ok || idx != 18 {
		t.Fatalf("scrolled row at y=3 = %d,%v, want 18,true (windowStart consistency)", idx, ok)
	}
}

func TestWindowStartMatchesWindowRows(t *testing.T) {
	for _, c := range []struct{ total, n, sel int }{
		{2, 5, 0}, {10, 4, 0}, {10, 4, 9}, {30, 4, 20}, {30, 4, 2},
	} {
		rows := make([]string, c.total)
		_, _, start := windowRows(rows, c.n, c.sel)
		if got := windowStart(c.total, c.n, c.sel); got != start {
			t.Errorf("windowStart(%d,%d,%d) = %d, windowRows start = %d", c.total, c.n, c.sel, got, start)
		}
	}
}

func TestWheelStepHelper(t *testing.T) {
	m := mouseModel()
	if m.wheelStep() != 3 {
		t.Fatalf("wheelStep = %d before any config load, want 3", m.wheelStep())
	}
	m.cfg = config.Config{UI: config.UIConfig{WheelStep: 5}}
	if m.wheelStep() != 5 {
		t.Fatalf("wheelStep = %d, want configured 5", m.wheelStep())
	}
}
