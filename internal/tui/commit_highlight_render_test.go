package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// TestHighlightDecoratorDimsOnlyNonMatches checks at the decorator level (the
// reliable seam — full-screen 240/RGB matching is fragile) that with @-highlight
// active a NON-matching row gets a whole-row dim decorator while a MATCHING row
// gets none. Commits have no refs/source, so there is no lineage-ident dim and
// no graph lane color to confound the result: the only decoration is the
// highlight dim.
func TestHighlightDecoratorDimsOnlyNonMatches(t *testing.T) {
	forceColor(t)
	m := footerModel()
	m.focus = panelCommits
	m.commits = []model.Commit{
		{Hash: "aaaaaaa", Subject: "alpha"},
		{Hash: "bbbbbbb", Subject: "bravo"},
	}
	m.highlightQuery = "alpha" // matches row 0 only
	m.sel[panelCommits] = 1    // selected row is excluded from decoration; keep it off row 0

	rows, idx := m.panelView(panelCommits)
	decos := m.commitDecorators(rows, idx, -1)

	// Matching row 0: nothing to decorate (no lineage, no lane color) → nil.
	if decos[0] != nil {
		if got := decos[0]("  alpha", 0, 0); strings.Contains(got, "\x1b[") {
			t.Fatalf("matching row must not be dimmed, got styled %q", got)
		}
	}
	// Non-matching row 1: whole-row dim decorator present and it styles the row.
	if decos[1] == nil {
		t.Fatal("non-matching row must get a dim decorator")
	}
	if got := decos[1]("  bravo", 0, 0); !strings.Contains(got, "\x1b[") {
		t.Fatalf("non-matching row must be dimmed, got %q", got)
	}
}

// TestHighlightInertWithoutQuery proves an empty query (e.g. @ just opened)
// dims nothing — every row decorator is nil for a clean feed.
func TestHighlightInertWithoutQuery(t *testing.T) {
	forceColor(t)
	m := footerModel()
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "aaaaaaa", Subject: "alpha"}, {Hash: "bbbbbbb", Subject: "bravo"}}
	m.highlightTyping = true // active mode, but no query yet
	m.highlightQuery = ""
	m.sel[panelCommits] = 0

	rows, idx := m.panelView(panelCommits)
	decos := m.commitDecorators(rows, idx, -1)
	for i, d := range decos {
		if d != nil {
			t.Fatalf("empty query must dim nothing, row %d decorated", i)
		}
	}
}

// TestHighlightKeepsAllRowsVisible proves @-highlight never filters: every commit
// still renders even though only one matches.
func TestHighlightKeepsAllRowsVisible(t *testing.T) {
	m := relabel(loadedModelLinearCommits(t, 6)) // commits[5].Subject == "uniform"
	m.focus = panelCommits
	m.highlightQuery = "uniform"
	rows, _ := m.panelView(panelCommits)
	if len(rows) != len(m.commits) {
		t.Fatalf("highlight must keep every row: %d rows for %d commits", len(rows), len(m.commits))
	}
}

func TestHighlightLabelShowsQuery(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.highlightQuery = "fix"
	label := m.panelLabel(panelCommits, "Commits")
	if !strings.Contains(label, "@fix") {
		t.Fatalf("label = %q, want it to contain @fix", label)
	}
	// while typing, the label shows the cursor block
	m.highlightQuery = "wip"
	m.highlightTyping = true
	if label := m.panelLabel(panelCommits, "Commits"); !strings.Contains(label, "@wip") {
		t.Fatalf("typing label = %q, want @wip", label)
	}
}
