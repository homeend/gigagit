package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestPackHintsKeepsKeyPairsIntact(t *testing.T) {
	t.Parallel()
	pairs := []string{
		"[tab] switch field", "[enter] newline/next",
		"[ctrl+g] generate", "[ctrl+s] commit", "[esc] cancel",
	}
	// A narrow width forces wrapping; every pair must stay on one line and no
	// line may exceed the width — the bug was "[ctrl+g]" wrapping away from its
	// "generate" label, reading as a dangling key.
	out := packHints(pairs, 52)
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 52 {
			t.Fatalf("line exceeds width 52: %q (%d)", line, len(line))
		}
		for _, p := range pairs {
			key := p[:strings.IndexByte(p, ']')+1] // e.g. "[ctrl+g]"
			if strings.Contains(line, key) && !strings.Contains(line, p) {
				t.Fatalf("%q split from its label on line %q", key, line)
			}
		}
	}
	for _, p := range pairs { // nothing dropped
		if !strings.Contains(out, p) {
			t.Fatalf("missing pair %q in %q", p, out)
		}
	}
	if strings.Contains(packHints(pairs, 200), "\n") {
		t.Fatal("a wide width should keep all pairs on one line")
	}
}

// TestPackHintsCJKUsesDisplayWidthNotBytes covers a translated hint label
// (e.g. under a ja/ko/zh catalog): CJK glyphs are 3 bytes in UTF-8 but only 2
// display cells, so byte-length packing systematically undercounts how much
// fits on a line. pair1 is 22 bytes / 16 cells; pair2 is 7 bytes/cells. At
// width 27, byte math sees 22+2+7=31 > 27 (wraps pair2 to its own line) while
// cell math sees 16+2+7=25 <= 27 (both pairs fit on one line) — the two
// counting methods disagree on the split point, which is exactly the bug.
func TestPackHintsCJKUsesDisplayWidthNotBytes(t *testing.T) {
	t.Parallel()
	pair1 := "[a] 生成生成生成" // 4 ASCII + 6 CJK runes: 22 bytes, 16 display cells
	pair2 := "[b] set"    // 7 ASCII bytes/cells
	pairs := []string{pair1, pair2}
	const width = 27

	out := packHints(pairs, width)

	lines := strings.Split(out, "\n")
	if len(lines) != 1 {
		t.Fatalf("expected pairs to pack onto one line by display width, got %d lines: %q", len(lines), out)
	}
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("line exceeds display width %d: %q (width %d)", width, line, w)
		}
	}
	if !strings.Contains(out, pair1) || !strings.Contains(out, pair2) {
		t.Fatalf("missing a pair in %q", out)
	}
}

func TestCommitPopupMessageAssembly(t *testing.T) {
	t.Parallel()
	if got := (&commitPopup{title: newTextField("subj")}).message(); got != "subj" {
		t.Fatalf("title-only = %q", got)
	}
	if got := (&commitPopup{title: newTextField("subj"), desc: newTextField("body line")}).message(); got != "subj\n\nbody line" {
		t.Fatalf("title+body = %q", got)
	}
	if got := (&commitPopup{title: newTextField("  spaced  ")}).message(); got != "spaced" {
		t.Fatalf("title should be trimmed: %q", got)
	}
}

func TestSplitMessage(t *testing.T) {
	t.Parallel()
	ti, de := splitMessage("subject\n\nbody one\nbody two\n")
	if ti != "subject" || de != "body one\nbody two" {
		t.Fatalf("split = (%q, %q)", ti, de)
	}
	if ti, de := splitMessage("only subject"); ti != "only subject" || de != "" {
		t.Fatalf("single-line split = (%q, %q)", ti, de)
	}
}

func TestCommitPopupTypingAndFieldSwitch(t *testing.T) {
	t.Parallel()
	m := Model{sel: map[panel]int{}}
	m = m.pushLayer(&commitPopup{})
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
	m = tm.(Model)
	if layerOf[*commitPopup](m).title.Value() != "hi" {
		t.Fatalf("title = %q", layerOf[*commitPopup](m).title.Value())
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // title → description
	m = tm.(Model)
	if layerOf[*commitPopup](m).field != 1 {
		t.Fatal("enter in title must move to description")
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("body")})
	m = tm.(Model)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // newline in description
	m = tm.(Model)
	if layerOf[*commitPopup](m).desc.Value() != "body\n" {
		t.Fatalf("desc = %q, want \"body\\n\"", layerOf[*commitPopup](m).desc.Value())
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab}) // back to title
	m = tm.(Model)
	if layerOf[*commitPopup](m).field != 0 {
		t.Fatal("tab must switch field")
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(Model)
	if layerOf[*commitPopup](m) != nil {
		t.Fatal("esc must close the popup")
	}
}

func TestCommitPopupEmptyTitleRefused(t *testing.T) {
	t.Parallel()
	m := Model{sel: map[panel]int{}}
	m = m.pushLayer(&commitPopup{})
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = tm.(Model)
	if cmd != nil {
		t.Fatal("ctrl+s with an empty title must not start a commit")
	}
	if layerOf[*commitPopup](m) == nil {
		t.Fatal("empty-title submit must keep the popup open")
	}
}
