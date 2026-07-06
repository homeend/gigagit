package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// shelfPopup is the centered shelf quick-switcher (G): a type-to-filter list of
// the repo's shelved files. Mirrors bookmarkPopup.
type shelfPopup struct {
	popupMax
	items     []model.ShelfEntry
	rows      []string // e.Origin.Display(), parallel to items
	sel       int
	filter    string
	filtering bool
	markID    string   // first mark for a two-entry compare ("" = none)
	mode      dispMode // text display mode; z cycles (cutoff default)
	hscroll   int      // modeScroll horizontal offset

	compareRef   *model.FileRef // compare mode: enter diffs compareRef (left) vs the picked entry (right)
	compareLabel string
}

// shelfEntryDisplay is the switcher row text: the address, plus " — <label>"
// when the entry carries a human name (mirrors bookmarkDisplay).
func shelfEntryDisplay(e model.ShelfEntry) string {
	s := e.Origin.Display()
	if e.Label != "" {
		s += " — " + e.Label
	}
	return s
}

// openShelfSwitcher opens the global shelf quick-switcher (G). Wired into every
// navigable surface like g; render+routing hoisted above content surfaces.
func (m Model) openShelfSwitcher() (Model, tea.Cmd) {
	if m.opsIdle() && m.shelfSwitcher() == nil {
		return m, m.loadShelfCmd(true)
	}
	return m, nil
}

func newShelfPopup(items []model.ShelfEntry) *shelfPopup {
	p := &shelfPopup{items: items}
	for _, e := range items {
		p.rows = append(p.rows, shelfEntryDisplay(e))
	}
	return p
}

// visibleIdx returns item indices matching the filter (case-insensitive).
func (p *shelfPopup) visibleIdx() []int {
	var idx []int
	q := strings.ToLower(p.filter)
	for i, row := range p.rows {
		if q == "" || strings.Contains(strings.ToLower(row), q) {
			idx = append(idx, i)
		}
	}
	return idx
}

func (p *shelfPopup) selected() (model.ShelfEntry, bool) {
	vis := p.visibleIdx()
	if p.sel < 0 || p.sel >= len(vis) {
		return model.ShelfEntry{}, false
	}
	return p.items[vis[p.sel]], true
}

// render composites the switcher box over `below` (the overlay-stack contract).
func (p *shelfPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), m.renderShelfPopupBox(p), w, h)
}

func (m Model) renderShelfPopupBox(p *shelfPopup) string {
	w, termH := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
	textW := popupTextWidth(inner)

	header := "Shelf"
	if p.compareRef != nil {
		header = "Compare " + p.compareRef.Path + " against:"
	}
	if p.filtering {
		header += "  /" + p.filter + "█"
	} else if p.filter != "" {
		header += "  /" + p.filter
	}

	vis := p.visibleIdx()
	var bodyLines []string
	if len(vis) == 0 {
		bodyLines = []string{padRight("  (none)", textW)}
	} else {
		wr := make([]winRow, len(vis))
		for n, i := range vis {
			prefix := "  "
			var st lipgloss.Style
			if n == p.sel {
				prefix, st = "> ", selectedRow
			}
			mark := " "
			if p.items[i].ID == p.markID {
				mark = "•"
			}
			wr[n] = winRow{text: prefix + mark + " " + p.rows[i], style: st}
		}
		capRows := 12
		if p.maximized {
			capRows = popupMaxRowCap(termH)
		}
		h := len(vis)
		if h > capRows {
			h = capRows
		}
		bodyLines = renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
	}

	parts := []string{header, ""}
	parts = append(parts, bodyLines...)
	// Wrap the hint to the text width so [z] mode / [esc] close stay visible even
	// on a narrow terminal, where a single-line footer would truncate them off
	// (the reason z went undiscovered).
	hint := []string{"[?] keys", "[enter] diff", "[e] editor", "[p] restore", "[t] temp dir", "[m] mark/compare", "[x] remove", "[c] vs bookmark", "[/] filter", "[z] mode", "[T] full", "[esc] close"}
	parts = append(parts, "")
	parts = append(parts, wrapParts(hint, textW, "  ")...)
	return popupBox(inner, strings.Join(parts, "\n"))
}

func (p *shelfPopup) moveSel(d int) {
	n := p.sel + d
	if hi := len(p.visibleIdx()) - 1; n > hi {
		n = hi
	}
	if n < 0 {
		n = 0
	}
	p.sel = n
}

// update handles one key while the shelf switcher is open (the overlay contract).
// Navigation-first; `/` enters a filter sub-mode (mirrors bookmarkPopup).
func (p *shelfPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if m.running {
		// B1: the switcher stays visible during a restore WriteFile op but must be
		// inert — a keypress here must not launch a second op.
		return m, nil
	}
	if p.filtering {
		if nm, nq, handled, commit := m.recallUpdate(scopeShelf, msg, p.filter); handled {
			m = nm
			p.filter, p.sel = nq, 0
			if commit {
				p.filtering = false
				return m.recordSearch(scopeShelf, p.filter)
			}
			return m, nil
		} else {
			m = nm
		}
		// Arrows/pages move the selection live while typing (no cursor reset),
		// like the commit filter; j/k stay query text.
		if filterMotion(msg, p.moveSel, popupFilterPage) {
			return m, nil
		}
		switch msg.Type {
		case tea.KeyEsc:
			p.filtering, p.filter, p.sel = false, "", 0
		case tea.KeyEnter:
			p.filtering = false
			return m.recordSearch(scopeShelf, p.filter)
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(p.filter); len(r) > 0 {
				p.filter = string(r[:len(r)-1])
				p.sel = 0
			}
		case tea.KeyRunes:
			p.filter += string(msg.Runes)
			p.sel = 0
		}
		return m, nil
	}
	if p.handleMaxKey(msg) { // "T" toggles fullscreen (nav mode only)
		return m, nil
	}
	switch msg.String() {
	case "z":
		p.mode = p.mode.next()
		p.hscroll = 0
		return m, nil
	case "shift+left":
		if p.mode == modeScroll && p.hscroll > 0 {
			if p.hscroll -= m.hscrollStep(); p.hscroll < 0 {
				p.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
		}
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
	case tea.KeyEnter:
		e, ok := p.selected()
		if !ok {
			return m, nil
		}
		if p.compareRef != nil {
			return m.openCompareFocusedVsShelf(*p.compareRef, p.compareLabel, e)
		}
		return m.openShelfCompareEntry(e)
	case tea.KeyUp:
		p.moveSel(-1)
	case tea.KeyDown:
		p.moveSel(1)
	case tea.KeyPgUp:
		p.moveSel(-popupFilterPage)
	case tea.KeyPgDown:
		p.moveSel(popupFilterPage)
	case tea.KeyRunes:
		switch msg.String() {
		case "?":
			// Open the compact cheat sheet over the still-open switcher; esc
			// closes it and returns here (contentPopup's esc just nils itself).
			m = m.pushLayer(newContentPopup(shelfSwitcherHelpTitle, shelfSwitcherHelp(p.compareRef != nil)))
			return m, nil
		case "/":
			p.filtering = true
			m = m.recallReset()
		case "k":
			p.moveSel(-1)
		case "j":
			p.moveSel(1)
		case "x":
			if p.compareRef != nil {
				return m, nil
			}
			return m.shelfPopupRemovePrompt()
		case "p":
			if p.compareRef != nil {
				return m, nil
			}
			e, ok := p.selected()
			if !ok {
				return m, nil
			}
			return m.openShelfRestore(e)
		case "m":
			if p.compareRef != nil {
				return m, nil
			}
			return m.shelfPopupMark()
		case "c":
			if p.compareRef != nil {
				return m, nil
			}
			return m.shelfCompareAgainstBookmark()
		case "e":
			if p.compareRef != nil {
				return m, nil
			}
			e, ok := p.selected()
			if !ok {
				return m, nil
			}
			svc, id, name := m.svc, e.ID, e.Origin.Path
			return m, m.openInEditorCmd(name, func(ctx context.Context) ([]byte, error) {
				return svc.ShelfBlob(ctx, id)
			})
		case "t":
			if p.compareRef != nil {
				return m, nil
			}
			e, ok := p.selected()
			if !ok {
				return m, nil
			}
			return m.startTempExportShelf(e)
		}
	}
	return m, nil
}

func (m Model) shelfPopupRemovePrompt() (Model, tea.Cmd) {
	p := m.shelfSwitcher()
	if p == nil {
		return m, nil
	}
	e, ok := p.selected()
	if !ok {
		return m, nil
	}
	// Leave the switcher on the stack: the modal renders above it; Cancel reveals
	// it, Remove refreshes it in place via the shelf-loaded reload.
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "shelf-remove",
			Prompt:  "Remove " + e.Origin.Path + " from the shelf? (the frozen copy is destroyed)",
			Options: []string{"Remove", "Cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt == "Remove" {
				return m, m.shelfRemoveCmd(e.ID)
			}
			return m, nil
		},
	}
	return m, nil
}

// shelfPopupMark records the first mark, or diffs the two entries on the second
// press on a different entry (mirrors bookmarkMark).
func (m Model) shelfPopupMark() (Model, tea.Cmd) {
	p := m.shelfSwitcher()
	if p == nil {
		return m, nil
	}
	e, ok := p.selected()
	if !ok {
		return m, nil
	}
	if p.markID == "" || p.markID == e.ID {
		if p.markID == e.ID {
			p.markID = ""
		} else {
			p.markID = e.ID
		}
		return m, nil
	}
	a, okA := m.shelfEntryByID(p.markID)
	b, okB := m.shelfEntryByID(e.ID)
	if !okA || !okB {
		return m, nil
	}
	return m.openShelfCompareTwoEntries(a, b)
}

// shelfCompareAgainstBookmark: the highlighted entry becomes the left side, then
// the bookmark popup opens in compare mode to pick the right side.
func (m Model) shelfCompareAgainstBookmark() (Model, tea.Cmd) {
	p := m.shelfSwitcher()
	if p == nil {
		return m, nil
	}
	e, ok := p.selected()
	if !ok {
		return m, nil
	}
	ref := model.FileRef{Source: model.SourceShelf, Locator: e.ID, Path: e.Origin.Path}
	// Keep this switcher on the stack: the bookmark picker is pushed on top so esc
	// in it returns here (the diff on a pick clears both via openPickerDiff).
	m.pendingCompare = &pendingCompare{ref: ref, label: "shelf #" + shortShelf(e), target: compareBookmark}
	return m, m.loadBookmarksCmd()
}
