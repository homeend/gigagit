package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/model"
)

// graphWinModel builds a Model with a synthetically wide graph: `lanes` lanes,
// node on lane `nodeLane`, one commit selected, focus on the Commits panel. The
// commit subject is distinct so we can assert it survives windowing. Maps are
// initialized as New() does so Update can write to them; zero sortMode is
// sortDefault and no filter is set, so graphActive() is true.
func graphWinModel(lanes, nodeLane, cols, scroll int) Model {
	m := Model{
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		dispModes: map[panel]dispMode{},
		hscroll:   map[panel]int{},
		focus:     panelCommits,
		width:     120,
		height:    40,
	}
	m.commits = []model.Commit{{Hash: "h0", Subject: "WINDOWED_SUBJECT"}}
	m.commitGraphRows = []string{cellsWithNode(lanes, nodeLane)}
	m.commitGraphLanes = []int{nodeLane}
	m.commitGraphCols = cols
	m.commitGraphScroll = scroll
	return m
}

// feedKey routes one key through Update and returns the resulting Model.
func feedKey(m Model, key string) Model {
	var msg tea.KeyMsg
	switch key {
	case "shift+left":
		msg = tea.KeyMsg{Type: tea.KeyShiftLeft}
	case "shift+right":
		msg = tea.KeyMsg{Type: tea.KeyShiftRight}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	mm, _ := m.Update(msg)
	return mm.(Model)
}

// cellsWithNode builds a graph cell string `lanes` lanes wide, every lane a
// pass-through '│', with the node '●' on lane `nodeLane`.
func cellsWithNode(lanes, nodeLane int) string {
	r := make([]rune, lanes*2)
	for i := range r {
		r[i] = ' '
	}
	for l := 0; l < lanes; l++ {
		r[l*2] = '│'
	}
	r[nodeLane*2] = '●'
	return string(r)
}

func TestGraphWindowSlicesAndMarks(t *testing.T) {
	m := graphWinModel(50, 40, 8, 0) // window at lane 0, node off-screen right
	win, leftMore, rightMore := m.graphWindow(m.commitGraphRows[0])
	if lipgloss.Width(win) != 8*2 {
		t.Fatalf("window width = %d, want %d", lipgloss.Width(win), 16)
	}
	if leftMore {
		t.Error("leftMore should be false at scroll 0")
	}
	if !rightMore {
		t.Error("rightMore should be true (content beyond the window)")
	}
	if !strings.HasSuffix(win, "⋯") {
		t.Errorf("right edge should be the ⋯ marker, got %q", win)
	}
}

func TestGraphWindowLeftMarkerWhenScrolled(t *testing.T) {
	m := graphWinModel(50, 40, 8, 10) // scrolled right
	win, leftMore, _ := m.graphWindow(m.commitGraphRows[0])
	if !leftMore {
		t.Fatal("leftMore should be true when scrolled")
	}
	if !strings.HasPrefix(win, "⋯") {
		t.Errorf("left edge should be the ⋯ marker, got %q", win)
	}
}

func TestCommitRowWidthBoundedByWindow(t *testing.T) {
	// The core regression: with a 200-lane graph and an 8-lane window, the
	// rendered Commits line must still contain the subject and not be 400+ cols.
	m := graphWinModel(200, 150, 8, 0)
	rows := m.commitIdentRows(false)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if !strings.Contains(rows[0], "WINDOWED_SUBJECT") {
		t.Errorf("subject pushed off-screen by the graph: %q", rows[0])
	}
	if w := lipgloss.Width(rows[0]); w > 8*2+1+80 {
		t.Errorf("row width %d not bounded by the window", w)
	}
}
