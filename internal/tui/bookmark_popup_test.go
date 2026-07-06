package tui

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
)

func TestBookmarkPopupMaximizeWidensAndLiftsRowCap(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &bookmarkPopup{}
	for i := 0; i < 30; i++ { // more than the fixed cap of 12
		p.items = append(p.items, model.Bookmark{ID: fmt.Sprintf("b%d", i), Path: fmt.Sprintf("path/to/file%d", i)})
		p.rows = append(p.rows, fmt.Sprintf("path/to/file%d", i))
	}

	normal := m.renderBookmarkPopupBox(p)
	p.maximized = true
	maxed := m.renderBookmarkPopupBox(p)

	if lipgloss.Width(maxed) <= lipgloss.Width(normal) {
		t.Fatalf("maximized width %d must exceed normal %d", lipgloss.Width(maxed), lipgloss.Width(normal))
	}
	if lipgloss.Height(maxed) <= lipgloss.Height(normal) {
		t.Fatalf("maximized must show more rows: height %d vs %d", lipgloss.Height(maxed), lipgloss.Height(normal))
	}
}

func TestBookmarkPopupTKeyDoesNotMaximizeWhileFiltering(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &bookmarkPopup{filtering: true}
	p.update(m, runeKey("T"))
	if p.maximized {
		t.Fatal(`"T" while filtering must not maximize`)
	}
	if p.filter != "T" {
		t.Fatalf(`"T" while filtering must be a literal char; filter=%q`, p.filter)
	}
}
