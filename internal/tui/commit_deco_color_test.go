package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/model"
)

// tagColorStyle is the expected yellow style for ⊙tag labels in the deco
// group. Must match tagDecoStyle in commit_ident.go.
var tagColorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))

// tagDecoModel builds a Model in list mode with a tag-bearing commit at index
// 0 and a plain lineage commit at index 1. The tag-bearing commit carries refs
// main(HEAD)+tag "v1" so commitDecoGroup returns " (⊙v1)". sel=1 keeps
// commit[0] unselected so renderPanel applies its decorator.
func tagDecoModel() Model {
	m := Model{
		sel:            map[panel]int{panelCommits: 1},
		sortModes:      map[panel]sortMode{},
		dispModes:      map[panel]dispMode{},
		hscroll:        map[panel]int{},
		focus:          panelCommits,
		commitListMode: true,
		width:          80,
		height:         20,
	}
	m.branches = []model.Branch{{Name: "main", IsHead: true}}
	m.commits = []model.Commit{
		{
			Hash:    "aaaa111122223333444455556666777788889999aaaa",
			Subject: "Hello",
			Source:  "main",
			Refs: []model.Ref{
				{Name: "main", Kind: model.RefLocal, Head: true},
				{Name: "v1", Kind: model.RefTag},
			},
		},
		{
			Hash:    "bbbb111122223333444455556666777788889999bbbb",
			Subject: "Lineage commit",
			Source:  "dev",
		},
	}
	return m
}

// TestCommitRowTagColoredEndToEnd asserts that ⊙v1 in a tag-bearing commit
// row is colored yellow by the decorator pipeline, end-to-end through
// commitBody + renderPanel (the production assembly chain). Goes through the
// real column math so any groupBase off-by-one will be caught. Also verifies
// the surrounding "(" and ")" are NOT inside the yellow span.
func TestCommitRowTagColoredEndToEnd(t *testing.T) {
	forceColor(t)
	m := tagDecoModel()
	rows, _, decos := m.commitBody(80, 20)
	out := m.renderPanel(panelCommits, "Commits", rows, decos, 80, 20)

	// Derive the escape from the style rather than hardcoding the SGR form
	// (256 vs TrueColor produce different sequences).
	probe := tagColorStyle.Render("⊙v1")
	esc := probe[:strings.Index(probe, "⊙")]
	if esc == "" {
		t.Fatal("tagColorStyle produced no escape prefix; forceColor may not have taken effect")
	}

	// ⊙v1 must be colored yellow.
	if !strings.Contains(out, esc+"⊙v1") {
		t.Fatalf("rendered panel does not contain yellow ⊙v1 (esc=%q)\npanel:\n%s", esc, out)
	}
	// The "(" immediately before the yellow span must NOT itself be yellow:
	// "(" followed by the color escape means ( is outside the span.
	if !strings.Contains(out, "("+esc+"⊙v1") {
		t.Fatalf("expected '(' immediately before yellow ⊙v1 (groupBase off-by-one?)\npanel:\n%s", out)
	}
	// Neither may the span extend to include "(": the first yellow char is ⊙.
	if strings.Contains(out, esc+"(⊙v1") {
		t.Fatalf("'(' must not be inside the yellow span\npanel:\n%s", out)
	}
	// The ")" after v1 must NOT be inside the yellow span (trailing boundary).
	if strings.Contains(out, esc+"⊙v1)") {
		t.Fatalf("')' must not be inside the yellow span (span over-extended?)\npanel:\n%s", out)
	}
}

// TestCommitRowTagColoredGraphMode repeats the end-to-end tag-color assertion
// in graph mode, where identStart gains graphCols()*2+1 instead of +2. This
// is where the column math most likely drifts.
func TestCommitRowTagColoredGraphMode(t *testing.T) {
	forceColor(t)
	m := tagDecoModel()
	m.commitListMode = false
	m = m.rebuildCommitGraph()

	rows, _, decos := m.commitBody(80, 20)
	out := m.renderPanel(panelCommits, "Commits", rows, decos, 80, 20)

	probe := tagColorStyle.Render("⊙v1")
	esc := probe[:strings.Index(probe, "⊙")]
	if esc == "" {
		t.Fatal("tagColorStyle produced no escape prefix in graph mode; forceColor may not have taken effect")
	}

	if !strings.Contains(out, esc+"⊙v1") {
		t.Fatalf("graph-mode panel does not contain yellow ⊙v1 (esc=%q)\npanel:\n%s", esc, out)
	}
	if !strings.Contains(out, "("+esc+"⊙v1") {
		t.Fatalf("graph mode: expected '(' immediately before yellow ⊙v1 (groupBase off-by-one?)\npanel:\n%s", out)
	}
	if strings.Contains(out, esc+"(⊙v1") {
		t.Fatalf("graph mode: '(' must not be inside the yellow span\npanel:\n%s", out)
	}
	// The ")" after v1 must NOT be inside the yellow span (trailing boundary).
	if strings.Contains(out, esc+"⊙v1)") {
		t.Fatalf("graph mode: ')' must not be inside the yellow span (span over-extended?)\npanel:\n%s", out)
	}
}

// TestCommitRowLineageStillDimmedNoWidthChange asserts that after Task 4 the
// lineage row's identity is still dimmed (decos[1] is non-nil) and that the
// decorator changes no visible width.
func TestCommitRowLineageStillDimmedNoWidthChange(t *testing.T) {
	forceColor(t)
	m := tagDecoModel()
	rows, _, decos := m.commitBody(80, 20)

	// commit[1] is the lineage row; it must still have a dim decorator.
	if len(decos) < 2 || decos[1] == nil {
		t.Fatal("lineage row (index 1) must have a non-nil dim decorator")
	}
	// Apply the decorator to the production-assembled line (selection prefix +
	// raw row), as renderWindow does.
	prefixed := "  " + rows[1]
	padded := padRight(prefixed, 76) // innerW = 80-4
	decorated := decos[1](padded, 0, 0)
	// Dimming must not add display cells.
	if lipgloss.Width(decorated) != lipgloss.Width(padded) {
		t.Errorf("dim decorator must not change visible width: before=%d after=%d",
			lipgloss.Width(padded), lipgloss.Width(decorated))
	}
	// The decorated string must contain the dim ANSI escape.
	if !strings.Contains(decorated, "\x1b[") {
		t.Error("lineage row must contain ANSI escapes (dimIdentStyle not applied)")
	}
}
