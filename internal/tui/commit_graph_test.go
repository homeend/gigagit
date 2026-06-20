package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/model"
)

func graphModel() Model {
	m := footerModel()
	m.commits = []model.Commit{
		{Hash: "c2", Parents: []string{"c1"}, Subject: "second"},
		{Hash: "c1", Subject: "first"},
	}
	m = m.rebuildCommitGraph()
	m.focus = panelCommits
	return m
}

func TestCommitRowsHaveGraphInNaturalOrder(t *testing.T) {
	m := graphModel()
	rows := m.commitRows()
	if len(rows) != 2 || !strings.HasPrefix(rows[0], "●") {
		t.Fatalf("natural-order rows should start with the graph node: %q", rows)
	}
	if !strings.Contains(rows[0], "second") {
		t.Fatalf("row should still contain the subject: %q", rows[0])
	}
}

func TestCommitRowsDropGraphWhenFiltered(t *testing.T) {
	m := graphModel()
	m.filterPanel = panelCommits
	m.filterQuery = "second"
	rows := m.commitRows()
	if strings.HasPrefix(rows[0], "●") {
		t.Fatalf("graph must be suppressed when the Commits panel is filtered: %q", rows[0])
	}
}

func TestCommitRowsDropGraphWhenSorted(t *testing.T) {
	m := graphModel()
	m.sortModes[panelCommits] = sortDateDesc
	rows := m.commitRows()
	if strings.HasPrefix(rows[0], "●") {
		t.Fatalf("graph must be suppressed when the Commits panel is re-sorted: %q", rows[0])
	}
}

func TestRebuildCommitGraphAligns(t *testing.T) {
	m := graphModel()
	if len(m.commitGraphRows) != len(m.commits) {
		t.Fatalf("graph rows (%d) must align with commits (%d)", len(m.commitGraphRows), len(m.commits))
	}
}

func TestGraphRowFitsNarrowPanel(t *testing.T) {
	// A multi-lane (wide) graph prefix at a small panel size must still fit:
	// cutoff truncates the combined graph+subject, and box-drawing runes measure
	// as width 1 to lipgloss.
	m := footerModel()
	m.commits = []model.Commit{
		{Hash: "m", Parents: []string{"a", "b"}, Subject: strings.Repeat("x", 200)}, // merge → 2 lanes
		{Hash: "a", Parents: []string{"r"}, Subject: "main"},
		{Hash: "b", Parents: []string{"r"}, Subject: "feat"},
		{Hash: "r", Subject: "root"},
	}
	m = m.rebuildCommitGraph()
	m.focus = panelCommits
	m.width, m.height = 50, 20
	out := m.View()
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Fatalf("line wider than panel (%d > %d): %q", w, m.width, line)
		}
	}
}
