package tui

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// symmetricBasePopup asks for the BASE of a symmetric merge preview of (a, b):
// what a and b would each bring into it, compared. The field starts EMPTY and
// is never filled in for the user — the base is not guessed. Completion is
// the preview form's (branchSuggestions); tab or enter accept the top
// suggestion when the typed text is not itself a branch. It sits ON the pair
// menu (pairOp.stacked), so esc returns there.
type symmetricBasePopup struct {
	popupMax
	a, b string
	base textfield
}

// symmetricSavedMsg reports the save: the stored entry, or the one that was
// already there (existed), or why nothing was stored.
type symmetricSavedMsg struct {
	c       domain.SavedCompare
	existed bool
	err     error
}

func (m Model) openSymmetricBase(a, b string) (Model, tea.Cmd) {
	return m.pushLayer(&symmetricBasePopup{a: a, b: b, base: newTextField("")}), nil
}

// accept replaces the field with the top suggestion unless it already names a
// branch; text that matches nothing is left for the save to refuse by name.
func (p *symmetricBasePopup) accept(m Model) {
	if m.namesABranch(p.base.Value()) {
		return
	}
	if s := m.branchSuggestions(p.base.Value()); len(s) > 0 {
		p.base = newTextField(s[0])
	}
}

func (p *symmetricBasePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyTab:
		p.accept(m)
	case tea.KeyEnter:
		p.accept(m)
		base := strings.TrimSpace(p.base.Value())
		if base == "" {
			return m, nil
		}
		if base == p.a || base == p.b {
			m.statusMsg = i18n.T("the base must differ from %s and %s", p.a, p.b)
			return m, nil
		}
		// Handing off to a save that ends in the comparison view: drop this
		// popup, the pair menu under it and the mark they came from.
		m = m.clearLayers()
		m.mark = nil
		m.statusMsg = i18n.T("saving symmetric merge preview…")
		return m, m.symmetricAddCmd(p.a, p.b, base)
	case tea.KeySpace:
		// branch names cannot contain spaces — drop it
	default:
		p.base.HandleEditKey(msg)
	}
	return m, nil
}

func (m Model) symmetricAddCmd(a, b, base string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		c, err := svc.SymmetricPreviewAdd(context.Background(), a, b, base, "")
		existed := errors.Is(err, domain.ErrSavedCompareExists)
		if existed {
			err = nil
		}
		return symmetricSavedMsg{c: c, existed: existed, err: err}
	}
}

// symmetricSaved lands the save. Saving what is already saved is not a
// failure: the existing entry opens exactly as a new one would. The Previews
// tab re-reads so the row is there when the comparison is closed.
func (m Model) symmetricSaved(msg symmetricSavedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("symmetric merge preview: %s", msg.err.Error())
		return m, nil
	}
	m.statusMsg = i18n.T("saved to previews: %s", msg.c.Label)
	if msg.existed {
		m.statusMsg = i18n.T("already saved as %s", msg.c.Label)
	}
	m, open := m.openCompareWithLoading(msg.c.Left, msg.c.Right, msg.c.Label)
	m, reload := m.chainPreviewsRead()
	return m, tea.Batch(open, reload)
}

func (p *symmetricBasePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *symmetricBasePopup) box(m Model) string {
	w, _ := m.overlayDims()
	cw := popupContentWidth(w)
	var b strings.Builder
	b.WriteString(i18n.T("Symmetric merge preview") + "\n\n")
	b.WriteString(i18n.T("compare what %s and %s would each bring into a base", p.a, p.b) + "\n\n")
	b.WriteString(viewField("> "+i18n.T("base: "), p.base, true, cw) + "\n")
	if s := m.branchSuggestions(p.base.Value()); len(s) > 0 {
		b.WriteString("\n" + i18n.T("matches: ") + strings.Join(s, "  ") + "\n")
	}
	b.WriteString("\n" + i18n.T("[enter] save & open  [tab] complete  [esc] back"))
	return st().modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}
