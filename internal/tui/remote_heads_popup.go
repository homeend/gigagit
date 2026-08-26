package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// remoteHeadsPopup is the "Browse remote branches" picker: remote branches
// that exist on the remote but have no local remote-tracking ref (the
// branches a narrowed/single-branch monorepo fetch refspec hides). It loads
// in two async phases — remote names first (a local read; >1 remote shows a
// chooser), then `git ls-remote --heads` for the chosen remote (a NETWORK
// read) — and enter on a branch opens a checkout / checkout-and-switch menu.
// Navigation-first like the file finder: plain keys navigate, `/` filters.
type remoteHeadsPopup struct {
	popupMax
	remotes   []string           // chooser rows; non-nil only while remote is unpicked
	remote    string             // picked remote ("" while choosing)
	heads     []model.RemoteHead // unfetched branches on remote (filled async)
	loading   bool               // true until the current phase's load returns
	query     string             // current filter query
	filtering bool               // true while `/` filter sub-mode captures runes
	visible   []int              // indexes into heads (or remotes) matching query
	sel       int                // cursor index into visible
	mode      dispMode           // text display mode; z cycles
	hscroll   int                // modeScroll horizontal offset
}

// remoteHeadNamesMsg is the async result of the RemoteNames phase. gen is the
// loadGen value captured at launch; stale results (repo switched while the
// read was in flight) are dropped.
type remoteHeadNamesMsg struct {
	names []string
	err   error
	gen   int
}

// remoteHeadsMsg is the async result of the UnfetchedRemoteHeads phase.
type remoteHeadsMsg struct {
	remote string
	heads  []model.RemoteHead
	err    error
	gen    int
}

// openRemoteHeadsBrowser pushes a loading remoteHeadsPopup and starts the
// remote-names phase. No-op when an op is in flight or one is already open.
func (m Model) openRemoteHeadsBrowser() (Model, tea.Cmd) {
	if !m.opsIdle() || layerOf[*remoteHeadsPopup](m) != nil {
		return m, nil
	}
	m = m.pushLayer(&remoteHeadsPopup{loading: true})
	return m, m.loadRemoteHeadNamesCmd()
}

// loadRemoteHeadNamesCmd lists remote names off the UI thread.
func (m Model) loadRemoteHeadNamesCmd() tea.Cmd {
	svc := m.svc
	gen := m.loadGen
	return func() tea.Msg {
		names, err := svc.RemoteNames(context.Background())
		return remoteHeadNamesMsg{names: names, err: err, gen: gen}
	}
}

// loadRemoteHeadsCmd runs the ls-remote phase for remote off the UI thread.
func (m Model) loadRemoteHeadsCmd(remote string) tea.Cmd {
	svc := m.svc
	gen := m.loadGen
	return func() tea.Msg {
		heads, err := svc.UnfetchedRemoteHeads(context.Background(), remote)
		return remoteHeadsMsg{remote: remote, heads: heads, err: err, gen: gen}
	}
}

// rows returns the current phase's row labels: remote names while choosing,
// branch names after.
func (p *remoteHeadsPopup) rows() []string {
	if p.remote == "" {
		return p.remotes
	}
	names := make([]string, len(p.heads))
	for i, h := range p.heads {
		names[i] = h.Name
	}
	return names
}

// refilter rebuilds p.visible from the current rows and query (case-insensitive
// substring, the gitConfigPopup precedent) and clamps p.sel.
func (p *remoteHeadsPopup) refilter() {
	rows := p.rows()
	q := strings.ToLower(p.query)
	p.visible = p.visible[:0]
	for i, r := range rows {
		if q == "" || strings.Contains(strings.ToLower(r), q) {
			p.visible = append(p.visible, i)
		}
	}
	if p.sel >= len(p.visible) {
		p.sel = max(0, len(p.visible)-1)
	}
}

// setQuery is the single chokepoint for every query mutation.
func (p *remoteHeadsPopup) setQuery(q string) {
	p.query = q
	p.sel = 0
	p.refilter()
}

// moveSel moves the cursor by d, clamped to the visible list.
func (p *remoteHeadsPopup) moveSel(d int) {
	n := p.sel + d
	if n > len(p.visible)-1 {
		n = len(p.visible) - 1
	}
	if n < 0 {
		n = 0
	}
	p.sel = n
}

// update handles one key while the browser is open.
func (p *remoteHeadsPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if p.filtering {
		if filterMotion(msg, p.moveSel, popupFilterPage) {
			return m, nil
		}
		switch msg.Type {
		case tea.KeyEsc:
			p.filtering = false
			p.setQuery("")
		case tea.KeyEnter:
			p.filtering = false // keep the filter, leave input mode
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(p.query); len(r) > 0 {
				p.setQuery(string(r[:len(r)-1]))
			}
		case tea.KeySpace:
			p.setQuery(p.query + " ")
		case tea.KeyRunes:
			p.setQuery(p.query + string(msg.Runes))
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
		return m.popLayer(), nil
	case tea.KeyUp:
		p.moveSel(-1)
		return m, nil
	case tea.KeyDown:
		p.moveSel(1)
		return m, nil
	case tea.KeyPgUp:
		p.moveSel(-popupFilterPage)
		return m, nil
	case tea.KeyPgDown:
		p.moveSel(popupFilterPage)
		return m, nil
	case tea.KeyEnter:
		if p.loading || p.sel < 0 || p.sel >= len(p.visible) {
			return m, nil
		}
		idx := p.visible[p.sel]
		if p.remote == "" {
			// Remote chooser phase → start the ls-remote phase for the pick.
			p.remote = p.remotes[idx]
			p.remotes = nil
			p.loading = true
			p.setQuery("")
			return m, m.loadRemoteHeadsCmd(p.remote)
		}
		m.actionMenu = &actionMenu{rows: m.remoteHeadActionRows(p.remote, p.heads[idx].Name)}
		return m, nil
	case tea.KeyRunes:
		switch msg.String() {
		case "/":
			p.filtering = true
		case "j":
			p.moveSel(1)
		case "k":
			p.moveSel(-1)
		}
		return m, nil
	}
	return m, nil
}

// render composites the picker over the layer beneath.
func (p *remoteHeadsPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// box draws the picker box.
func (p *remoteHeadsPopup) box(m Model) string {
	w, termH := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
	textW := popupTextWidth(inner)

	var header string
	switch {
	case p.loading:
		header = i18n.T("Browse remote branches  (loading…)")
	case p.remote == "":
		header = i18n.T("Browse remote branches — pick a remote")
	default:
		header = i18n.T("Remote branches on %s not fetched locally  %d/%d", p.remote, len(p.visible), len(p.rows()))
	}
	switch {
	case p.filtering:
		header += "  /" + p.query + "█"
	case p.query != "":
		header += "  /" + p.query
	case !p.loading:
		header += i18n.T("   (press / to filter)")
	}

	rows := p.rows()
	var bodyLines []string
	switch {
	case p.loading:
		bodyLines = []string{padRight(i18n.T("  (loading…)"), textW)}
	case len(rows) == 0 && p.remote != "" && p.query == "":
		bodyLines = []string{padRight(i18n.T("  (every remote branch is already tracked)"), textW)}
	case len(p.visible) == 0:
		bodyLines = []string{padRight(i18n.T("  (no match)"), textW)}
	default:
		visH := len(p.visible)
		capRows := popupResolveRowCap(p.maximized, termH, 16)
		if visH > capRows {
			visH = capRows
		}
		start, end := windowBounds(len(p.visible), p.sel, visH)
		wr := make([]winRow, end-start)
		for i, vi := range rangeSlice(start, end) {
			name := rows[p.visible[vi]]
			if vi == p.sel {
				wr[i] = winRow{text: padRight("> "+name, textW), style: selectedRow}
			} else {
				wr[i] = winRow{text: padRight("  "+name, textW)}
			}
		}
		o := winOpts{w: textW, mode: p.mode, anchor: p.sel - start, hscroll: p.hscroll}
		o.h = wrapContentLines(wr, o, capRows)
		bodyLines = renderWindow(wr, o)
	}

	enterHint := i18n.T("[enter] checkout")
	if p.remote == "" {
		enterHint = i18n.T("[enter] pick remote")
	}
	hint := []string{enterHint, i18n.T("[↑↓ pgup/pgdn] nav"), i18n.T("[/] filter"), i18n.T("[z] mode"), i18n.T("[ctrl+t] full"), i18n.T("[esc] close")}
	parts := []string{header, ""}
	parts = append(parts, bodyLines...)
	parts = append(parts, "")
	parts = append(parts, wrapParts(hint, textW, "  ")...)
	return popupBox(inner, strings.Join(parts, "\n"))
}

// remoteHeadActionRows returns the checkout menu for one unfetched remote
// branch. Checkout rows pop the browser first so the op's progress renders
// over the panels; Cancel just closes the menu (the browser stays).
func (m Model) remoteHeadActionRows(remote, branch string) []actionRow {
	return []actionRow{
		{
			id:    "rh-checkout",
			label: i18n.T("Checkout %s (stay on current branch)", branch),
			run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.popLayer()
				return m.startOp(engine.CheckoutRemoteBranch{Remote: remote, Branch: branch, Intent: engine.CheckoutStay})
			},
		},
		{
			id:    "rh-checkout-switch",
			label: i18n.T("Checkout %s and switch to it", branch),
			run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.popLayer()
				return m.startOp(engine.CheckoutRemoteBranch{Remote: remote, Branch: branch, Intent: engine.CheckoutSwitch})
			},
		},
		{
			id:    "rh-cancel",
			label: i18n.T("Cancel"),
			run: func(m Model) (tea.Model, tea.Cmd) {
				return m, nil // menu is already closed; keep browsing
			},
		},
	}
}
