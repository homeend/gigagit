package tui

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// lcRow is one focusable row of the compare dialog, in tab order. The base
// rows exist only while their side is an unbounded point (see rows).
type lcRow int

const (
	lcLink1 lcRow = iota
	lcBase1
	lcLink2
	lcBase2
)

// side is the link field (0 = left, 1 = right) a row belongs to.
func (r lcRow) side() int {
	if r >= lcLink2 {
		return 1
	}
	return 0
}

func (r lcRow) isBase() bool { return r == lcBase1 || r == lcBase2 }

// linkCompareSide is one half of the dialog: a link field, the copied-link
// history under it, and the error that belongs to this side alone.
type linkCompareSide struct {
	input textfield
	hist  linkHistPicker
	err   string

	// The base row (link_compare_base.go). sug answers for sugFor and no other
	// text; asked is the text a suggestion is in flight for.
	base      textfield
	baseDirty bool // the user typed into the base field
	sug       domain.BaseSuggestion
	sugFor    string
	asked     string

	// The description row (link_compare_desc.go): desc answers for descFor and
	// no other text; descAsked is the text a description is in flight for.
	desc, descFor, descAsked string
}

// linkComparePopup is the palette's "Compare with link…": two gg:// links,
// typed, pasted or picked from the copied-link history, compared through
// domain.CompareLinks. There is no state here a `gg compare <left> <right>`
// invocation could not express.
type linkComparePopup struct {
	popupMax
	side  [2]linkCompareSide
	focus lcRow
	busy  bool   // a comparison is in flight
	err   string // a failure that belongs to neither side
}

func (p *linkComparePopup) histPickers() []*linkHistPicker {
	return []*linkHistPicker{&p.side[0].hist, &p.side[1].hist}
}

func (m Model) openLinkComparePopup() (Model, tea.Cmd) {
	p := &linkComparePopup{}
	p.side[0].input, p.side[1].input = newTextField(""), newTextField("")
	m.linkHistGen++
	return m.pushLayer(p), m.linkHistCmd(m.linkHistGen)
}

// rows is the tab order: the link fields, each followed by its base row when
// it has one.
func (p *linkComparePopup) rows() []lcRow {
	rows := []lcRow{lcLink1}
	if p.side[0].hasBase() {
		rows = append(rows, lcBase1)
	}
	rows = append(rows, lcLink2)
	if p.side[1].hasBase() {
		rows = append(rows, lcBase2)
	}
	return rows
}

// refresh re-derives both base rows; every path that can change a link field
// ends here.
func (p *linkComparePopup) refresh(m Model) tea.Cmd {
	cmd := tea.Batch(p.side[0].refresh(m), p.side[1].refresh(m))
	p.normalizeFocus()
	return cmd
}

// step moves the focus by d rows, wrapping, over the rows that exist NOW.
func (p *linkComparePopup) step(d int) {
	rows := p.rows()
	at := 0
	for i, r := range rows {
		if r == p.focus {
			at = i
		}
	}
	p.focus = rows[(at+d+len(rows))%len(rows)]
}

func (p *linkComparePopup) cur() *linkCompareSide { return &p.side[p.focus.side()] }

// submit compares the two fields. Both must hold something; what they hold is
// the door's to judge, and its verdict comes back under the right field.
func (p *linkComparePopup) submit(m Model) (Model, tea.Cmd) {
	left, right := strings.TrimSpace(p.side[0].input.Value()), strings.TrimSpace(p.side[1].input.Value())
	if left == "" || right == "" || p.busy {
		return m, nil
	}
	p.side[0].err, p.side[1].err, p.err = "", "", ""
	m, cmd := m.startLinkCompare(left, right)
	if cmd == nil {
		// Exactly this comparison is already on screen beneath the dialog:
		// step aside for it, parked like any other hand-off.
		return m.handOffToFilesView(func(m Model) (Model, tea.Cmd) { return m, nil })
	}
	p.busy = true
	return m, cmd
}

func (p *linkComparePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// LOCKED while a comparison loads: it was built from the fields as they
	// stood, so an edit, a swap or a focus move now would leave the form
	// disagreeing with the view about to open. Only esc (withdraw) gets through.
	if p.busy && msg.Type != tea.KeyEsc {
		return m, nil
	}
	if !p.focus.isBase() {
		if picked, handled := p.cur().hist.key(msg); handled {
			if picked != "" {
				// A pick FILLS its field and stays: unlike the # prompt this is
				// a form, and the other side may still be empty.
				p.cur().input = newTextField(picked)
				p.cur().err, p.err = "", ""
				return m, p.refresh(m)
			}
			return m, nil
		}
	}
	switch msg.Type {
	case tea.KeyEsc:
		if p.busy {
			// Cancel the comparison, keep the form: the load still lands, and
			// an empty want is what makes loadedLinkCompare drop it.
			p.busy = false
			m.linkCompareWant = ""
			return m, nil
		}
		return m.popLayer(), nil
	case tea.KeyTab:
		if p.focus.isBase() && p.cur().completeBase(m) {
			return m, nil
		}
		p.step(1)
	case tea.KeyShiftTab, tea.KeyUp:
		p.step(-1)
	case tea.KeyDown:
		if p.focus.isBase() || !p.cur().hist.enter() {
			p.step(1)
		}
	case tea.KeyCtrlS:
		p.side[0], p.side[1] = p.side[1], p.side[0]
		p.normalizeFocus()
	case tea.KeyEnter:
		if p.focus.isBase() {
			if p.cur().applyBase() {
				p.err = ""
				p.normalizeFocus() // the row is gone: back onto its link field
				return m, p.refresh(m)
			}
			return m, nil
		}
		if p.focus != lcLink2 {
			p.step(1)
			return m, nil
		}
		return p.submit(m)
	case tea.KeySpace:
		// a link holds no space — drop it
	default:
		if p.busy {
			return m, nil
		}
		if p.focus.isBase() {
			if p.cur().base.HandleEditKey(msg) {
				p.cur().baseDirty = true
				p.cur().err, p.err = "", ""
			}
			return m, nil
		}
		if p.cur().input.HandleEditKey(msg) {
			p.cur().err, p.err = "", ""
			return m, p.refresh(m)
		}
	}
	return m, nil
}

// loaded takes the comparison this dialog asked for. A failure goes under the
// field it belongs to; success hands off to the view with the dialog parked,
// so closing the view comes back here with both fields intact.
func (p *linkComparePopup) loaded(m Model, msg linkCompareLoadedMsg) (Model, tea.Cmd) {
	// Reset BEFORE the hand-off: the dialog a closed view returns to must be
	// able to submit again.
	p.busy = false
	if msg.err != nil {
		var se *domain.LinkSideError
		if errors.As(msg.err, &se) {
			p.side[se.Side].err = se.Err.Error()
		} else {
			p.err = msg.err.Error()
		}
		return m, nil
	}
	return m.handOffToFilesView(func(m Model) (Model, tea.Cmd) { return m.openLinkCompare(msg) })
}

func (p *linkComparePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *linkComparePopup) box(m Model) string {
	w, _ := m.overlayDims()
	// The WIDE popup width: a gg:// link is a path with a sha on the end, and
	// the standard 56-column prose box wraps even a short one over three lines.
	inner := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
	cw := inner - st().modalStyle.GetHorizontalPadding()
	if cw < 1 {
		cw = 1
	}
	mark := func(r lcRow) string {
		if p.focus == r {
			return "> "
		}
		return "  "
	}
	var b strings.Builder
	b.WriteString(i18n.T("Compare two gg:// links") + "\n\n")
	for i, r := range []lcRow{lcLink1, lcLink2} {
		s := &p.side[i]
		label := i18n.T("left:  ")
		if i == 1 {
			label = i18n.T("right: ")
		}
		focused := p.focus == r
		b.WriteString(viewField(mark(r)+label, s.input, focused && !s.hist.active, cw) + "\n")
		// The history opens under ITS field, and only once ↓ asks for it: this
		// is a form, and a 20-row list between a link and its base row (or two
		// of them) would bury it. The footer says the list is there.
		if focused && s.hist.active {
			b.WriteString(s.hist.view(cw) + "\n")
		}
		if d := s.descLine(); d != "" {
			b.WriteString("    " + st().dim.Render(elideMiddle(d, cw-4)) + "\n")
		}
		if s.hasBase() {
			br := lcBase1
			if i == 1 {
				br = lcBase2
			}
			b.WriteString(viewField(mark(br)+"  "+s.baseLabel(), s.base, p.focus == br, cw) + "\n")
			if p.focus == br && s.sug.Kind == model.LinkBoundRef && s.baseDirty {
				if ms := m.branchSuggestions(s.base.Value()); len(ms) > 0 {
					b.WriteString("    " + elideMiddle(i18n.T("matches: ")+strings.Join(ms, "  "), cw-4) + "\n")
				}
			}
		}
		if s.err != "" {
			b.WriteString("    " + st().errorText.Render(elideMiddle(s.err, cw-4)) + "\n")
		}
	}
	if p.err != "" {
		b.WriteString("\n" + st().errorText.Render(elideMiddle(p.err, cw)) + "\n")
	}
	switch {
	case p.busy:
		b.WriteString("\n" + i18n.T("comparing…") + "\n")
		b.WriteString("\n" + i18n.T("[esc] cancel"))
	case p.focus.isBase():
		b.WriteString("\n" + i18n.T("[enter] bound the link with this base  [tab] complete / next  [esc] close"))
	case !p.focus.isBase() && p.cur().hist.active:
		b.WriteString("\n" + i18n.T("[enter] use this link  [↑] back to the field  [esc] back"))
	case len(p.cur().hist.rows) == 0:
		b.WriteString("\n" + i18n.T("[tab] next  [ctrl+s] swap  [enter] compare  [esc] close"))
	default:
		b.WriteString("\n" + i18n.T("[tab] next  [↓] copied links  [ctrl+s] swap  [enter] compare  [esc] close"))
	}
	return st().modalStyle.Width(inner).Render(b.String()) + "\n"
}
