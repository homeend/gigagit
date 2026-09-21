package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// linkDialogModel is refPairRepo behind a Model with the compare dialog open
// and its history load pumped. recorded links land in the copied-link history
// first (newest last → newest first in the picker).
func linkDialogModel(t *testing.T, recorded ...string) (m Model, dir, c1, c3 string) {
	t.Helper()
	dir, c1, _, c3 = refPairRepo(t)
	m = refPairModel(t, dir)
	m.svc.UseLinkHistDir(t.TempDir())
	for _, l := range recorded {
		m.svc.RecordCopiedLink(context.Background(), l)
	}
	m, cmd := m.openLinkComparePopup()
	for _, msg := range runCmds(cmd) {
		nm, _ := m.Update(msg)
		m = nm.(Model)
	}
	return m, dir, c1, c3
}

func linkDialog(t *testing.T, m Model) *linkComparePopup {
	t.Helper()
	p, ok := m.topLayer().(*linkComparePopup)
	if !ok {
		t.Fatalf("top layer = %T, want the compare dialog", m.topLayer())
	}
	return p
}

func pressKeys(t *testing.T, m Model, msgs ...tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range msgs {
		nm, c := m.Update(k)
		m, cmd = nm.(Model), c
	}
	return m, cmd
}

func setSides(p *linkComparePopup, left, right string) {
	p.side[0].input, p.side[1].input = newTextField(left), newTextField(right)
}

func TestTheHistoryLoadFillsEveryPickerOfItsHost(t *testing.T) {
	t.Parallel()
	m, _, _, _ := linkDialogModel(t, "gg://r@ref:main", "gg://r@ref:feat/x")
	p := linkDialog(t, m)
	for i := range p.side {
		if n := len(p.side[i].hist.rows); n != 2 || !p.side[i].hist.loaded {
			t.Errorf("side %d: %d rows loaded=%v, want both fields to hold the 2-row history", i, n, p.side[i].hist.loaded)
		}
	}
}

// A pick fills the field it sits under and nothing else happens: this is a
// form with another side still to fill, not the # prompt (where a pick goes).
func TestAPickFillsItsOwnFieldAndDoesNotSubmit(t *testing.T) {
	t.Parallel()
	m, dir, c1, _ := linkDialogModel(t, "gg://r@ref:main")
	// The LEFT side is already filled: with it empty a submit is a no-op, and
	// this test could not tell a pick that submits from one that does not.
	left := localLink(dir, "@"+c1)
	linkDialog(t, m).side[0].input = newTextField(left)
	m, _ = pressKeys(t, m, typeKey(tea.KeyTab)) // → the right link
	m, cmd := pressKeys(t, m, typeKey(tea.KeyDown), typeKey(tea.KeyEnter))
	p := linkDialog(t, m)
	if got := p.side[1].input.Value(); got != "gg://r@ref:main" {
		t.Errorf("right field = %q, want the picked link", got)
	}
	if got := p.side[0].input.Value(); got != left {
		t.Errorf("left field = %q, want it untouched", got)
	}
	// Checked BEFORE anything is pumped: a submit sets busy and the want
	// synchronously, and a comparison that then fails would reset both. The
	// cmd itself may be non-nil — the pick asks what would bound the link.
	if p.busy || m.linkCompareWant != "" {
		t.Error("a pick must not submit")
	}
	if m = pumpAll(t, m, cmd); m.filesView != nil {
		t.Error("a pick opened a view")
	}
}

func TestCtrlSSwapsTheTwoSides(t *testing.T) {
	t.Parallel()
	m, _, c1, _ := linkDialogModel(t)
	p := linkDialog(t, m)
	ref, commit := "gg://r@ref:feat/x", "gg://r@"+c1 // different KINDS: a no-op swap cannot pass
	setSides(p, ref, commit)
	p.side[0].err = "left's own error"
	m, _ = pressKeys(t, m, typeKey(tea.KeyCtrlS))
	p = linkDialog(t, m)
	if p.side[0].input.Value() != commit || p.side[1].input.Value() != ref {
		t.Errorf("after swap: %q | %q", p.side[0].input.Value(), p.side[1].input.Value())
	}
	if p.side[1].err != "left's own error" {
		t.Error("a side's error travels with it")
	}
}

func TestSubmitOpensTheSetView(t *testing.T) {
	t.Parallel()
	m, dir, c1, c3 := linkDialogModel(t)
	setSides(linkDialog(t, m), localLink(dir, "/b.txt@"+c1), localLink(dir, "/b.txt@"+c3))
	m, cmd := pressKeys(t, m, typeKey(tea.KeyTab), typeKey(tea.KeyEnter))
	if !linkDialog(t, m).busy {
		t.Fatal("enter on the right link must submit")
	}
	m = pumpAll(t, m, cmd)
	if got := strings.Join(rowTexts(m), "|"); got != "A b.txt" || m.filesSets == nil {
		t.Fatalf("rows = %q sets=%v, want the set view over exactly b.txt", got, m.filesSets != nil)
	}
	if m.topLayer() != nil {
		t.Errorf("top layer = %T, want the dialog parked behind the view", m.topLayer())
	}
}

func TestEscOnTheViewReturnsToTheDialogWithItsFields(t *testing.T) {
	t.Parallel()
	m, dir, c1, c3 := linkDialogModel(t)
	left, right := localLink(dir, "@"+c1), localLink(dir, "@"+c3)
	setSides(linkDialog(t, m), left, right)
	m, cmd := pressKeys(t, m, typeKey(tea.KeyTab), typeKey(tea.KeyEnter))
	m = pumpAll(t, m, cmd)
	m, _ = pressKeys(t, m, typeKey(tea.KeyEsc))
	p := linkDialog(t, m)
	if p.side[0].input.Value() != left || p.side[1].input.Value() != right {
		t.Errorf("fields = %q | %q, want them intact", p.side[0].input.Value(), p.side[1].input.Value())
	}
}

// The dialog the view returns to must work: busy is reset before the hand-off.
func TestTheDialogCanSubmitAgainAfterComingBack(t *testing.T) {
	t.Parallel()
	m, dir, c1, c3 := linkDialogModel(t)
	setSides(linkDialog(t, m), localLink(dir, "@"+c1), localLink(dir, "@"+c3))
	m, cmd := pressKeys(t, m, typeKey(tea.KeyTab), typeKey(tea.KeyEnter))
	m = pumpAll(t, m, cmd)
	m, _ = pressKeys(t, m, typeKey(tea.KeyEsc))
	linkDialog(t, m).side[1].input = newTextField(localLink(dir, "/c.txt@"+c3))
	m, cmd = pressKeys(t, m, typeKey(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("the second submit did nothing — the dialog came back still busy")
	}
	m = pumpAll(t, m, cmd)
	if got := strings.Join(rowTexts(m), "|"); got != "A c.txt" {
		t.Fatalf("rows = %q, want the second comparison", got)
	}
}

func TestEscWhileBusyCancelsTheLoad(t *testing.T) {
	t.Parallel()
	m, dir, c1, c3 := linkDialogModel(t)
	setSides(linkDialog(t, m), localLink(dir, "@"+c1), localLink(dir, "@"+c3))
	m, cmd := pressKeys(t, m, typeKey(tea.KeyTab), typeKey(tea.KeyEnter))
	m, _ = pressKeys(t, m, typeKey(tea.KeyEsc))
	if m.linkCompareWant != "" {
		t.Error("esc on a busy dialog must withdraw the comparison")
	}
	m = pumpAll(t, m, cmd) // the load still lands
	if m.filesView != nil {
		t.Error("a cancelled comparison opened a view")
	}
	if p := linkDialog(t, m); p.busy {
		t.Error("the dialog must be usable again")
	}
}

func TestASideErrorRendersUnderItsOwnField(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		bad  int
	}{{"right", 1}, {"left", 0}} {
		m, dir, c1, _ := linkDialogModel(t)
		sides := [2]string{localLink(dir, "@"+c1), localLink(dir, "@"+c1)}
		sides[c.bad] = "gg://r@not-a-target"
		setSides(linkDialog(t, m), sides[0], sides[1])
		m, cmd := pressKeys(t, m, typeKey(tea.KeyTab), typeKey(tea.KeyEnter))
		m = pumpAll(t, m, cmd)
		p := linkDialog(t, m)
		if p.busy {
			t.Fatalf("%s: still busy after a failure", c.name)
		}
		if p.side[c.bad].err == "" || p.side[1-c.bad].err != "" {
			t.Fatalf("%s: errs = %q | %q, want only side %d to carry one", c.name, p.side[0].err, p.side[1].err, c.bad)
		}
		if strings.HasPrefix(p.side[c.bad].err, "left: ") || strings.HasPrefix(p.side[c.bad].err, "right: ") {
			t.Errorf("%s: %q repeats the side its position already says", c.name, p.side[c.bad].err)
		}
		// Position on screen: the message sits below ITS field and, for the
		// left side, above the right field.
		lines := strings.Split(p.box(m), "\n")
		at := func(sub string) int {
			for i, l := range lines {
				if strings.Contains(l, sub) {
					return i
				}
			}
			return -1
		}
		le, ri, er := at("left:"), at("right:"), at("bad gg link")
		if er < 0 {
			t.Fatalf("%s: the error is not rendered:\n%s", c.name, p.box(m))
		}
		if c.bad == 0 && !(le < er && er < ri) {
			t.Errorf("left error at line %d, want between the fields (%d, %d)", er, le, ri)
		}
		if c.bad == 1 && !(ri < er) {
			t.Errorf("right error at line %d, want below the right field (%d)", er, ri)
		}
	}
}

func TestEditingClearsTheError(t *testing.T) {
	t.Parallel()
	m, _, _, _ := linkDialogModel(t)
	p := linkDialog(t, m)
	p.side[0].err, p.err = "stale", "stale too"
	m, _ = pressKeys(t, m, keyMsg("g"))
	p = linkDialog(t, m)
	if p.side[0].err != "" || p.err != "" {
		t.Errorf("errs = %q | %q, want an edit to clear them", p.side[0].err, p.err)
	}
}

func TestEnterOnLinkOneMovesOnAndNeverSubmits(t *testing.T) {
	t.Parallel()
	m, dir, c1, c3 := linkDialogModel(t)
	setSides(linkDialog(t, m), localLink(dir, "@"+c1), localLink(dir, "@"+c3))
	m, cmd := pressKeys(t, m, typeKey(tea.KeyEnter))
	p := linkDialog(t, m)
	if p.focus != lcLink2 || p.busy || cmd != nil {
		t.Errorf("focus=%v busy=%v cmd=%v, want enter on the left link to move to the right one", p.focus, p.busy, cmd != nil)
	}
}

func TestAnEmptySideDoesNotSubmit(t *testing.T) {
	t.Parallel()
	m, dir, c1, _ := linkDialogModel(t)
	setSides(linkDialog(t, m), localLink(dir, "@"+c1), "  ")
	m, cmd := pressKeys(t, m, typeKey(tea.KeyTab), typeKey(tea.KeyEnter))
	if cmd != nil || linkDialog(t, m).busy {
		t.Error("an empty side must not submit")
	}
}

func TestTheDialogNeverExceedsItsWidth(t *testing.T) {
	t.Parallel()
	m, _, _, _ := linkDialogModel(t, "gg://r/"+strings.Repeat("deep/", 60)+"f.go@ref:main")
	m.width, m.height = 80, 30
	p := linkDialog(t, m)
	setSides(p, "gg://r/"+strings.Repeat("x", 300), "gg://r@ref:main")
	p.side[0].err = strings.Repeat("e", 300)
	p.err = strings.Repeat("z", 300)
	w, _ := m.overlayDims()
	for _, line := range strings.Split(p.box(m), "\n") {
		if lipgloss.Width(line) > w {
			t.Fatalf("line is %d wide, the overlay is %d:\n%s", lipgloss.Width(line), w, line)
		}
	}
}

// A busy dialog is LOCKED: the comparison in flight was built from the fields
// as they stood, so typing, swapping, moving or re-submitting must do nothing
// until it lands or esc withdraws it.
func TestABusyDialogIsLocked(t *testing.T) {
	t.Parallel()
	m, dir, c1, c3 := linkDialogModel(t)
	left, right := localLink(dir, "@"+c1), localLink(dir, "@"+c3)
	setSides(linkDialog(t, m), left, right)
	m, _ = pressKeys(t, m, typeKey(tea.KeyTab), typeKey(tea.KeyEnter))
	p := linkDialog(t, m)
	focus, want := p.focus, m.linkCompareWant
	m, cmd := pressKeys(t, m, keyMsg("x"), typeKey(tea.KeyBackspace), typeKey(tea.KeyCtrlS),
		typeKey(tea.KeyTab), typeKey(tea.KeyDown), typeKey(tea.KeyUp), typeKey(tea.KeyEnter))
	p = linkDialog(t, m)
	if cmd != nil || !p.busy || p.focus != focus || m.linkCompareWant != want ||
		p.side[0].input.Value() != left || p.side[1].input.Value() != right {
		t.Fatalf("a busy dialog changed: cmd=%v busy=%v focus=%v left=%q right=%q",
			cmd != nil, p.busy, p.focus, p.side[0].input.Value(), p.side[1].input.Value())
	}
}

// A link field is forty hex digits; the row under it says what it IS — the
// same words the copied-link history shows — and where a hinted link came from.
func TestTheDialogDescribesItsLinks(t *testing.T) {
	t.Parallel()
	m, dir, c1, c3 := linkDialogModel(t)
	p := linkDialog(t, m)
	pair := localLink(dir, "@"+c1+".."+c3)
	setSides(p, pair+"?preview=abc123", "not a link")
	m = pumpAll(t, m, p.refresh(m))
	p = linkDialog(t, m)
	want := describeLinkText(context.Background(), m.svc, pair+"?preview=abc123") + " · copied from a saved preview"
	if got := p.side[0].descLine(); got != want {
		t.Fatalf("left description = %q, want %q", got, want)
	}
	if got := p.side[1].descLine(); got != "" {
		t.Fatalf("text that is no link has no description, got %q", got)
	}
	if out := m.View(); !strings.Contains(out, "copied from a saved preview") {
		t.Fatalf("the description must be on screen:\n%s", out)
	}
	// Editing the field drops the words at once: they described other text.
	m, _ = pressKeys(t, m, keyMsg("x"))
	if got := linkDialog(t, m).side[0].descLine(); got != "" {
		t.Fatalf("a stale description survived an edit: %q", got)
	}
}

// ctrl+s swaps the fields; each description follows ITS link.
func TestDescriptionsFollowASwap(t *testing.T) {
	t.Parallel()
	m, dir, c1, c3 := linkDialogModel(t)
	p := linkDialog(t, m)
	setSides(p, localLink(dir, "@"+c1), localLink(dir, "@"+c3))
	m = pumpAll(t, m, p.refresh(m))
	l0, r0 := linkDialog(t, m).side[0].descLine(), linkDialog(t, m).side[1].descLine()
	if l0 == "" || r0 == "" || l0 == r0 {
		t.Fatalf("fixture: descriptions %q / %q", l0, r0)
	}
	m, cmd := pressKeys(t, m, typeKey(tea.KeyCtrlS))
	m = pumpAll(t, m, cmd)
	p = linkDialog(t, m)
	if p.side[0].descLine() != r0 || p.side[1].descLine() != l0 {
		t.Fatalf("after swap: %q / %q, want %q / %q", p.side[0].descLine(), p.side[1].descLine(), r0, l0)
	}
}
