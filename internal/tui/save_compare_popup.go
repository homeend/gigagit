package tui

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// saveComparePopup asks for the label a link comparison is saved under. The
// field starts EMPTY and empty is an answer: the store names the entry itself
// (its own default label), a rule this package cannot see and must not copy.
type saveComparePopup struct {
	popupMax
	left, right string // the comparison's two link TEXTS, as the view holds them
	label       textfield
}

// compareSavedMsg reports a save: the stored (or already-stored) entry.
type compareSavedMsg struct {
	c   domain.SavedCompare
	err error
}

// saveComparisonRow is the link compare view's "Save comparison…". Only a
// link comparison has two link texts to store; every other compare is two
// endpoints, which no saved entry can hold.
func (m Model) saveComparisonRow() (actionRow, bool) {
	if m.filesSets == nil {
		return actionRow{}, false
	}
	left, right := m.filesSets.LeftText, m.filesSets.RightText
	return actionRow{
		id:    "save-comparison",
		label: i18n.T("Save comparison…"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.pushLayer(&saveComparePopup{left: left, right: right, label: newTextField("")}), nil
		},
	}, true
}

func (m Model) saveCompareCmd(left, right, label string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		c, err := svc.SavedCompareAdd(context.Background(), left, right, label)
		return compareSavedMsg{c: c, err: err}
	}
}

// savedCompare lands a save. Saving what is already saved is not a failure:
// the store hands back the existing entry, and saying its name is the answer.
func (m Model) savedCompare(msg compareSavedMsg) (Model, tea.Cmd) {
	switch {
	case errors.Is(msg.err, domain.ErrSavedCompareExists):
		m.statusMsg = i18n.T("already saved as %s", msg.c.Label)
		return m, nil
	case msg.err != nil:
		m.statusMsg = i18n.T("save comparison: %s", msg.err.Error())
		return m, nil
	}
	m.statusMsg = i18n.T("saved comparison: %s", msg.c.Label)
	return m.chainPreviewsRead()
}

func (p *saveComparePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		m = m.popLayer()
		return m, m.saveCompareCmd(p.left, p.right, strings.TrimSpace(p.label.Value()))
	default:
		p.label.HandleEditKey(msg)
	}
	return m, nil
}

func (p *saveComparePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *saveComparePopup) box(m Model) string {
	w, _ := m.overlayDims()
	var b strings.Builder
	b.WriteString(i18n.T("Save comparison") + "\n\n")
	b.WriteString(viewField(i18n.T("label: "), p.label, true, popupContentWidth(w)) + "\n")
	b.WriteString("\n" + i18n.T("[enter] save (empty = a default label)  [esc] cancel"))
	return st().modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}

// comparisonLinkRows are a saved comparison's two halves, offered by name. A
// comparison has no single "Copy link" — it IS two links — so the Previews
// row offers each; both go through the one clipboard writer, so both record.
func (m Model) comparisonLinkRows() []actionRow {
	if m.inContentWindow() || m.focus != panelPreviews {
		return nil
	}
	r, ok := m.selectedPreview()
	if !ok {
		return nil
	}
	c, ok := r.compare()
	if !ok {
		return nil
	}
	return []actionRow{
		m.copyRow("copy-left-link", i18n.T("Copy left link"), i18n.T("Copied link: %s", c.Left), c.Left),
		m.copyRow("copy-right-link", i18n.T("Copy right link"), i18n.T("Copied link: %s", c.Right), c.Right),
	}
}
