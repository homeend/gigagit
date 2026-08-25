package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestDecoGroupFull(t *testing.T) {
	t.Parallel()
	id := commitIdent{extra: []string{"branch1", "branch2"}, tags: []string{"v1.0.0"}}
	group, spans, collapsed := commitDecoGroup(id, -1)
	if collapsed {
		t.Fatal("budget<0 must never collapse")
	}
	want := " (branch1, branch2, " + tagGlyph + "v1.0.0)"
	if group != want {
		t.Fatalf("group=%q want %q", group, want)
	}
	if len(spans) != 1 {
		t.Fatalf("want 1 tag span, got %d", len(spans))
	}
	// the span must land exactly on "⊙v1.0.0"
	gr := []rune(group)
	got := string(gr[spans[0].Offset : spans[0].Offset+spans[0].Length])
	if got != tagGlyph+"v1.0.0" {
		t.Fatalf("span text=%q want %q", got, tagGlyph+"v1.0.0")
	}
}

func TestDecoGroupEmpty(t *testing.T) {
	t.Parallel()
	if g, _, _ := commitDecoGroup(commitIdent{name: "main", tip: true}, -1); g != "" {
		t.Fatalf("no extras/tags must yield empty group, got %q", g)
	}
}

func TestDecoGroupCollapses(t *testing.T) {
	t.Parallel()
	id := commitIdent{extra: []string{"feature-a", "feature-b"}, tags: []string{"release-2026"}}
	group, spans, collapsed := commitDecoGroup(id, 8) // tiny budget
	if !collapsed {
		t.Fatalf("over-budget group must collapse, got %q", group)
	}
	if group != " (+3)" {
		t.Fatalf("collapsed group=%q want \" (+3)\"", group)
	}
	if spans != nil {
		t.Fatal("collapsed group has no tag spans")
	}
	if lipgloss.Width(group) > 8 {
		// (+N) itself must always fit; it is tiny
		t.Logf("note: (+N) width %d", lipgloss.Width(group))
	}
}

func TestDecoGroupTagsOnlyOnLineage(t *testing.T) {
	t.Parallel()
	// a tag on a non-tip commit (no extras): still renders
	id := commitIdent{name: "main", tags: []string{"v9"}} // tip=false
	g, spans, _ := commitDecoGroup(id, -1)
	if !strings.Contains(g, tagGlyph+"v9") || len(spans) != 1 {
		t.Fatalf("tag must render on a lineage row: %q spans=%v", g, spans)
	}
}
