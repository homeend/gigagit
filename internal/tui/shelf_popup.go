package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

// shelfPopup is the centered shelf quick-switcher (G): a type-to-filter list of
// the repo's shelved files. Mirrors bookmarkPopup.
type shelfPopup struct {
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

// openShelfSwitcher opens the global shelf quick-switcher (G). Wired into every
// navigable surface like g; render+routing hoisted above content surfaces.
func (m Model) openShelfSwitcher() (Model, tea.Cmd) {
	if m.opsIdle() && m.shelfPopup == nil {
		return m, m.loadShelfCmd(true)
	}
	return m, nil
}

func newShelfPopup(items []model.ShelfEntry) *shelfPopup {
	p := &shelfPopup{items: items}
	for _, e := range items {
		p.rows = append(p.rows, e.Origin.Display())
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

func (m Model) popupSelectedShelfEntry() (model.ShelfEntry, bool) {
	p := m.shelfPopup
	vis := p.visibleIdx()
	if p.sel < 0 || p.sel >= len(vis) {
		return model.ShelfEntry{}, false
	}
	return p.items[vis[p.sel]], true
}

func (m Model) renderShelfPopup() string {
	p := m.shelfPopup
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
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
		h := len(vis)
		if h > 12 {
			h = 12
		}
		bodyLines = renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
	}

	parts := []string{header, ""}
	parts = append(parts, bodyLines...)
	parts = append(parts, "", "[enter] diff  [p] restore  [m] mark/compare  [x] remove  [c] vs bookmark  [/] filter  [z] mode  [esc] close")
	return popupBox(inner, strings.Join(parts, "\n"))
}

func (m Model) shelfPopupMoveSel(d int) {
	p := m.shelfPopup
	if n := p.sel + d; n >= 0 && n < len(p.visibleIdx()) {
		p.sel = n
	}
}

// updateShelfPopupKey handles one key while the shelf switcher is open.
// Navigation-first; `/` enters a filter sub-mode (mirrors bookmarkPopup).
func (m Model) updateShelfPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.shelfPopup
	if p.filtering {
		switch msg.Type {
		case tea.KeyEsc:
			p.filtering, p.filter, p.sel = false, "", 0
		case tea.KeyEnter:
			p.filtering = false
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
		m.shelfPopup = nil
	case tea.KeyEnter:
		e, ok := m.popupSelectedShelfEntry()
		if !ok {
			return m, nil
		}
		if p.compareRef != nil {
			return m.openCompareFocusedVsShelf(*p.compareRef, p.compareLabel, e)
		}
		return m.openShelfCompareEntry(e)
	case tea.KeyUp:
		m.shelfPopupMoveSel(-1)
	case tea.KeyDown:
		m.shelfPopupMoveSel(1)
	case tea.KeyRunes:
		switch msg.String() {
		case "/":
			p.filtering = true
		case "k":
			m.shelfPopupMoveSel(-1)
		case "j":
			m.shelfPopupMoveSel(1)
		case "x":
			if p.compareRef != nil {
				return m, nil
			}
			return m.shelfPopupRemovePrompt()
		case "p":
			if p.compareRef != nil {
				return m, nil
			}
			e, ok := m.popupSelectedShelfEntry()
			if !ok {
				return m, nil
			}
			m.shelfPopup = nil
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
		}
	}
	return m, nil
}

func (m Model) shelfPopupRemovePrompt() (tea.Model, tea.Cmd) {
	e, ok := m.popupSelectedShelfEntry()
	if !ok {
		return m, nil
	}
	m.shelfPopup = nil
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
func (m Model) shelfPopupMark() (tea.Model, tea.Cmd) {
	e, ok := m.popupSelectedShelfEntry()
	if !ok {
		return m, nil
	}
	p := m.shelfPopup
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

// --- Stage B stubs (filled in the compare-matrix task) -------------------

func (m Model) shelfCompareAgainstBookmark() (tea.Model, tea.Cmd) { return m, nil }

func (m Model) openCompareFocusedVsShelf(ref model.FileRef, label string, e model.ShelfEntry) (Model, tea.Cmd) {
	return m.openShelfCompareEntry(e) // placeholder; replaced in the compare-matrix task
}
