package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// typeText pastes text into the focused field (one multi-rune key, the shape
// a terminal paste arrives in) and pumps whatever that set off.
func typeText(t *testing.T, m Model, text string) Model {
	t.Helper()
	m, cmd := pressKeys(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	return pumpAll(t, m, cmd)
}

// wipeField backspaces the focused field empty, pumping as it goes (each key
// is a link-text change the dialog reacts to).
func wipeField(t *testing.T, m Model) Model {
	t.Helper()
	p := linkDialog(t, m)
	f := &p.cur().input
	if p.focus.isBase() {
		f = &p.cur().base
	}
	for n := len([]rune(f.Value())); n > 0; n-- {
		var cmd tea.Cmd
		m, cmd = pressKeys(t, m, typeKey(tea.KeyBackspace))
		m = pumpAll(t, m, cmd)
	}
	return m
}

// baseDialog: refPairRepo has main (c1..c3) and feat/x at c3; feat/y is one
// commit past it, so bounding feat/y by main is a real, non-empty preview.
func baseDialog(t *testing.T) (m Model, dir, tip, parent string) {
	t.Helper()
	m, dir, _, _ = linkDialogModel(t)
	parent = gitOut(t, dir, "rev-parse", "HEAD")
	tip = advanceBranch(t, dir, "feat/x", "y.txt", "y\n", "feat work")
	gitRun(t, dir, "checkout", "-")
	return m, dir, tip, parent
}

func TestABaseRowShowsOnlyForAnUnboundedPoint(t *testing.T) {
	t.Parallel()
	m, dir, tip, parent := baseDialog(t)
	if n := len(linkDialog(t, m).rows()); n != 2 {
		t.Fatalf("an empty dialog has %d rows, want 2", n)
	}
	m = typeText(t, m, localLink(dir, "@ref:feat/x"))
	if n := len(linkDialog(t, m).rows()); n != 3 {
		t.Fatalf("a ref link gives %d rows, want its base row", n)
	}
	m = wipeField(t, m)
	if n := len(linkDialog(t, m).rows()); n != 2 {
		t.Fatalf("clearing the field leaves %d rows", n)
	}
	m = typeText(t, m, localLink(dir, "@"+parent+".."+tip))
	if n := len(linkDialog(t, m).rows()); n != 2 {
		t.Fatalf("a change-set is already bounded, yet there are %d rows", n)
	}
}

// The LOCAL-form row: the pure kind calls this a ref (the file is still
// inside the repo half); domain, which locates it, says it is a file.
func TestALocalFormFileLinkGetsNoBaseRow(t *testing.T) {
	t.Parallel()
	m, dir, _, _ := baseDialog(t)
	text := localLink(dir, "/a.txt@ref:feat/x")
	if l, _ := model.ParseLink(text); l.BoundKind() != model.LinkBoundRef {
		t.Fatal("fixture: the pure kind already sees the file — the two answers must differ")
	}
	m = typeText(t, m, text)
	if n := len(linkDialog(t, m).rows()); n != 2 {
		t.Fatalf("a file link got a base row (%d rows)", n)
	}
}

func TestTabbingThroughABaseRowRewritesNothing(t *testing.T) {
	t.Parallel()
	m, dir, _, _ := baseDialog(t)
	text := localLink(dir, "@ref:feat/x")
	m = typeText(t, m, text)
	for i := 0; i < 4; i++ {
		var cmd tea.Cmd
		m, cmd = pressKeys(t, m, typeKey(tea.KeyTab))
		m = pumpAll(t, m, cmd)
	}
	if got := linkDialog(t, m).side[0].input.Value(); got != text {
		t.Errorf("field = %q, want %q byte-identical — only enter on the base row rewrites", got, text)
	}
}

// TARGET FIRST, asserted on the field TEXT. The two sides are different KINDS
// (a ref, a commit), so a rewrite applied to the wrong side cannot pass.
func TestEnterOnTheBaseRowRewritesTargetFirst(t *testing.T) {
	t.Parallel()
	m, dir, tip, _ := baseDialog(t)
	tipParent := gitOut(t, dir, "rev-parse", tip+"^")
	m = typeText(t, m, localLink(dir, "@ref:feat/x"))
	m, _ = pressKeys(t, m, typeKey(tea.KeyTab), typeKey(tea.KeyTab)) // base 1 → link 2
	m = typeText(t, m, localLink(dir, "@"+tip[:7]))
	p := linkDialog(t, m)
	if n := len(p.rows()); n != 4 {
		t.Fatalf("rows = %d, want both base rows", n)
	}
	if p.side[0].sug.Why != "trunk" || p.side[1].sug.Why != "parent" {
		t.Fatalf("suggestions = %+v | %+v", p.side[0].sug, p.side[1].sug)
	}
	trunk := p.side[0].sug.Base

	p.focus = lcBase1
	m, cmd := pressKeys(t, m, typeKey(tea.KeyEnter))
	m = pumpAll(t, m, cmd)
	p = linkDialog(t, m)
	if got := p.side[0].input.Value(); !strings.HasSuffix(got, "@"+trunk+"...feat/x") {
		t.Errorf("left = %q, want @%s...feat/x (target first)", got, trunk)
	}
	if p.focus != lcLink1 {
		t.Errorf("focus = %v, want it back on the link the row belonged to", p.focus)
	}

	p.focus = lcBase2
	m, cmd = pressKeys(t, m, typeKey(tea.KeyEnter))
	m = pumpAll(t, m, cmd)
	p = linkDialog(t, m)
	if got := p.side[1].input.Value(); !strings.HasSuffix(got, "@"+tipParent+".."+tip) {
		t.Errorf("right = %q, want @%s..%s — the FULL sha, though %s was typed", got, tipParent, tip, tip[:7])
	}
	if n := len(p.rows()); n != 2 {
		t.Errorf("rows = %d, want both base rows gone: the fields are bounded now", n)
	}
	// And what the dialog now holds is a comparison the door accepts.
	p.focus = lcLink2
	m, cmd = pressKeys(t, m, typeKey(tea.KeyEnter))
	m = pumpAll(t, m, cmd)
	if m.filesSets == nil {
		t.Fatalf("the rewritten links did not compare: %q | %q | %q", p.side[0].err, p.side[1].err, p.err)
	}
}

func TestEditingBackToAPointBringsTheRowBack(t *testing.T) {
	t.Parallel()
	m, dir, _, _ := baseDialog(t)
	m = typeText(t, m, localLink(dir, "@ref:feat/x"))
	linkDialog(t, m).focus = lcBase1
	m, cmd := pressKeys(t, m, typeKey(tea.KeyEnter))
	m = pumpAll(t, m, cmd)
	if n := len(linkDialog(t, m).rows()); n != 2 {
		t.Fatalf("rows after the rewrite = %d", n)
	}
	m = wipeField(t, m)
	m = typeText(t, m, localLink(dir, "@ref:feat/x"))
	if n := len(linkDialog(t, m).rows()); n != 3 {
		t.Errorf("rows = %d, want the base row back", n)
	}
}

func TestAStaleSuggestionIsDropped(t *testing.T) {
	t.Parallel()
	m, dir, _, _ := baseDialog(t)
	old := localLink(dir, "@ref:feat/x")
	m, _ = pressKeys(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(old)}) // asked, not pumped
	m = wipeField(t, m)
	nm, _ := m.Update(baseSuggestedMsg{text: old, sug: domain.BaseSuggestion{Kind: model.LinkBoundRef, Base: "main", Why: "trunk"}})
	m = nm.(Model)
	if p := linkDialog(t, m); len(p.rows()) != 2 || p.side[0].base.Value() != "" {
		t.Errorf("an answer for text the field no longer holds was applied: rows=%d base=%q", len(p.rows()), p.side[0].base.Value())
	}
}

func TestSwapCarriesTheBaseRows(t *testing.T) {
	t.Parallel()
	m, dir, _, _ := baseDialog(t)
	m = typeText(t, m, localLink(dir, "@ref:feat/x"))
	// Swap FROM the base row: it is the row that stops existing on this side,
	// so it is the only place a stranded focus can be seen.
	linkDialog(t, m).focus = lcBase1
	m, _ = pressKeys(t, m, typeKey(tea.KeyCtrlS))
	p := linkDialog(t, m)
	if p.side[0].hasBase() || !p.side[1].hasBase() {
		t.Errorf("after the swap the base row must belong to the RIGHT side")
	}
	if p.focus != lcLink1 {
		t.Errorf("focus = %v, want it moved off the vanished row onto its side's link", p.focus)
	}
}

func TestTabCompletesATypedBase(t *testing.T) {
	t.Parallel()
	m, dir, _, _ := baseDialog(t)
	m.branches = []model.Branch{{Name: "main"}, {Name: "feat/x"}, {Name: "release/2.0"}}
	m = typeText(t, m, localLink(dir, "@ref:feat/x"))
	linkDialog(t, m).focus = lcBase1
	m = wipeField(t, m)
	m = typeText(t, m, "rele")
	m, _ = pressKeys(t, m, typeKey(tea.KeyTab))
	p := linkDialog(t, m)
	if p.side[0].base.Value() != "release/2.0" || p.focus != lcBase1 {
		t.Errorf("base = %q focus = %v, want the completion, still on the row", p.side[0].base.Value(), p.focus)
	}
	// An UNTOUCHED suggestion is never "completed" into something else.
	m2, dir2, _, _ := baseDialog(t)
	m2.branches = []model.Branch{{Name: "maintenance"}}
	m2 = typeText(t, m2, localLink(dir2, "@ref:feat/x"))
	before := linkDialog(t, m2).side[0].base.Value()
	linkDialog(t, m2).focus = lcBase1
	m2, _ = pressKeys(t, m2, typeKey(tea.KeyTab))
	if got := linkDialog(t, m2).side[0].base.Value(); got != before {
		t.Errorf("tab rewrote an untouched suggestion %q → %q", before, got)
	}
}

func TestABaseTheGrammarCannotHoldIsRefusedInline(t *testing.T) {
	t.Parallel()
	m, dir, _, _ := baseDialog(t)
	text := localLink(dir, "@ref:feat/x")
	m = typeText(t, m, text)
	p := linkDialog(t, m)
	p.focus, p.side[0].base = lcBase1, newTextField("feat/x") // bounded by itself
	m, _ = pressKeys(t, m, typeKey(tea.KeyEnter))
	p = linkDialog(t, m)
	if p.side[0].err == "" || p.side[0].input.Value() != text {
		t.Errorf("err=%q field=%q, want a refusal and the link untouched", p.side[0].err, p.side[0].input.Value())
	}
}

// Spec §4: no merge base is the door's error, shown under the side it names.
func TestNoMergeBaseRendersUnderItsSide(t *testing.T) {
	t.Parallel()
	m, dir, _, _ := baseDialog(t)
	gitRun(t, dir, "checkout", "--orphan", "island")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "island")
	gitRun(t, dir, "checkout", "main")
	p := linkDialog(t, m)
	setSides(p, localLink(dir, "@main...island"), localLink(dir, "@ref:main"))
	p.focus = lcLink2
	m, cmd := pressKeys(t, m, typeKey(tea.KeyEnter))
	m = pumpAll(t, m, cmd)
	p = linkDialog(t, m)
	if !strings.Contains(p.side[0].err, domain.ErrNoMergeBase.Error()) || p.side[1].err != "" {
		t.Errorf("errs = %q | %q, want the LEFT side to say %q", p.side[0].err, p.side[1].err, domain.ErrNoMergeBase)
	}
}
