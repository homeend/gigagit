package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/fuzzy"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// fileFinderLimit is the maximum number of fuzzy results shown; keeps rank
// O(n log limit) even over 100k-path repos.
const fileFinderLimit = 200

// fileFinderPopup is the global `F` fuzzy file-finder quick-switcher. It
// mirrors repoPopup in structure: a centered overlay, window-then-build
// render, z mode-cycle, hscroll. The list can be large (~100k entries), so
// matched rows are capped and the render uses window-then-build (only the
// visible slice is built into winRows each frame).
type fileFinderPopup struct {
	popupMax
	all       []string      // full path list from LsFiles (filled async)
	loading   bool          // true until LsFiles returns
	query     string        // current filter query
	filtering bool          // true while `/` filter sub-mode captures runes
	matches   []fuzzy.Match // ranked subset of all; updated by rerank()
	sel       int           // cursor index into matches
	mode      dispMode      // text display mode; z cycles
	hscroll   int           // modeScroll horizontal offset
}

// lsFilesMsg is the async result of the LsFiles domain call.
type lsFilesMsg struct {
	paths []string
	err   error
}

// openFileFinder pushes a loading fileFinderPopup and starts the async load.
// It is a no-op when an op is in flight or a finder is already open (double-F
// guard).
func (m Model) openFileFinder() (Model, tea.Cmd) {
	if !m.opsIdle() || layerOf[*fileFinderPopup](m) != nil {
		return m, nil
	}
	m = m.pushLayer(&fileFinderPopup{loading: true})
	return m, m.loadLsFilesCmd()
}

// loadLsFilesCmd returns a Cmd that calls LsFiles off-thread and delivers
// lsFilesMsg back to Update.
func (m Model) loadLsFilesCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		paths, err := svc.LsFiles(context.Background())
		return lsFilesMsg{paths: paths, err: err}
	}
}

// rerank rebuilds p.matches from p.all using the current p.query and clamps
// p.sel. Called from setQuery and from the lsFilesMsg handler.
func (p *fileFinderPopup) rerank() {
	p.matches = fuzzy.Rank(p.query, p.all, fileFinderLimit)
	if p.sel >= len(p.matches) {
		p.sel = max(0, len(p.matches)-1)
	}
}

// setQuery is the single chokepoint for every query mutation: set the query,
// reset the cursor, and rerank. Unlike bookmarkPopup (which recomputes its
// visible set live each render), the finder caches p.matches — so every edit,
// including recall and esc-clear, MUST funnel through here or the list shows
// stale rows.
func (p *fileFinderPopup) setQuery(q string) {
	p.query = q
	p.sel = 0
	p.rerank()
}

// moveSel moves the cursor by d, clamped to the current match list.
func (p *fileFinderPopup) moveSel(d int) {
	n := p.sel + d
	if n > len(p.matches)-1 {
		n = len(p.matches) - 1
	}
	if n < 0 {
		n = 0
	}
	p.sel = n
}

// update handles one key while the file finder is open. The finder is
// navigation-first (like every other gg list and the bookmark/shelf switchers):
// plain keys navigate, `/` enters a filter sub-mode where runes (including `z`
// and `/`) type a query until esc/enter.

func (p *fileFinderPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if p.filtering {
		if nm, nq, handled, commit := m.recallUpdate(scopeFiletree, msg, p.query); handled {
			m = nm
			p.setQuery(nq)
			if commit {
				p.filtering = false
				return m.recordSearch(scopeFiletree, p.query)
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
			p.filtering = false
			p.setQuery("") // clear the filter, back to the full first-N list
		case tea.KeyEnter:
			p.filtering = false // keep the filter, leave input mode
			return m.recordSearch(scopeFiletree, p.query)
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
	// Navigation mode. Display-mode + pan keys act here (they are query chars
	// while filtering).
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
		return m, nil
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
		if p.loading || p.sel < 0 || p.sel >= len(p.matches) {
			return m, nil
		}
		path := p.matches[p.sel].S
		m.actionMenu = &actionMenu{rows: m.fileFinderActionRows(path)}
		return m, nil
	case tea.KeyRunes:
		switch msg.String() {
		case "/":
			p.filtering = true
			m = m.recallReset()
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
func (p *fileFinderPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// box draws the picker box.
func (p *fileFinderPopup) box(m Model) string {
	w, termH := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
	textW := popupTextWidth(inner)

	// Header shows count / loading state + the filter query. While in the `/`
	// sub-mode a cursor block trails the query; otherwise a hint advertises `/`.
	var header string
	if p.loading {
		header = i18n.T("Find file  (loading…)")
	} else {
		header = i18n.T("Find file  %d/%d", len(p.matches), len(p.all))
	}
	switch {
	case p.filtering:
		header += "  /" + p.query + "█"
	case p.query != "":
		header += "  /" + p.query
	case !p.loading:
		header += i18n.T("   (press / to filter)")
	}

	var bodyLines []string
	if p.loading {
		bodyLines = []string{padRight(i18n.T("  (loading…)"), textW)}
	} else if len(p.matches) == 0 {
		bodyLines = []string{padRight(i18n.T("  (no match)"), textW)}
	} else {
		// Window-then-build: determine the visible slice first, then build
		// only those winRows. This keeps render O(window) even at 200 results.
		visH := len(p.matches)
		capRows := popupResolveRowCap(p.maximized, termH, 16)
		if visH > capRows {
			visH = capRows
		}
		// Compute window bounds (anchor = p.sel, height = visH).
		start, end := windowBounds(len(p.matches), p.sel, visH)
		wr := make([]winRow, end-start)
		for i, idx := range rangeSlice(start, end) {
			path := p.matches[idx].S
			if idx == p.sel {
				wr[i] = winRow{text: padRight("> "+path, textW), style: selectedRow}
			} else {
				wr[i] = winRow{text: padRight("  "+path, textW)}
			}
		}
		// Wrap mode: hang-indent continuations at the row text (intrinsic to wrap mode).
		// The height budget counts display lines of the pre-windowed slice, capped.
		o := winOpts{w: textW, mode: p.mode, anchor: p.sel - start, hscroll: p.hscroll}
		o.h = wrapContentLines(wr, o, capRows)
		bodyLines = renderWindow(wr, o)
	}

	// Wrap the hint so [/] filter / [esc] survive on a narrow terminal (mirrors
	// the bookmark/shelf switchers).
	hint := []string{i18n.T("[enter] open"), i18n.T("[↑↓ pgup/pgdn] nav"), i18n.T("[/] filter"), i18n.T("[z] mode"), i18n.T("[ctrl+t] full"), i18n.T("[esc] close")}
	parts := []string{header, ""}
	parts = append(parts, bodyLines...)
	parts = append(parts, "")
	parts = append(parts, wrapParts(hint, textW, "  ")...)
	return popupBox(inner, strings.Join(parts, "\n"))
}

// windowBounds returns [start, end) for a window of height h anchored on sel
// within a list of length n.
func windowBounds(n, sel, h int) (int, int) {
	if h >= n {
		return 0, n
	}
	start := sel - h/2
	if start < 0 {
		start = 0
	}
	end := start + h
	if end > n {
		end = n
		start = end - h
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

// rangeSlice returns indices [start, end).
func rangeSlice(start, end int) []int {
	out := make([]int, end-start)
	for i := range out {
		out[i] = start + i
	}
	return out
}

// fileFinderActionRows returns the per-file action menu rows for path. Each row
// pops the finder (m = m.popLayer()) before opening the target surface so the
// finder is gone and the chosen surface is what remains.
func (m Model) fileFinderActionRows(path string) []actionRow {
	svc := m.svc
	return []actionRow{
		{
			id:    "ff-view",
			label: i18n.T("View content"),
			run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.popLayer()
				cp := newContentPopup(i18n.T("View %s", path), []contentLine{{text: i18n.T("(loading…)")}})
				m = m.pushLayer(cp)
				return m, m.loadFileContentLayerCmd(path)
			},
		},
		{
			id:    "ff-diff",
			label: i18n.T("Diff (HEAD ↔ working tree)"),
			run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.popLayer()
				left := model.Endpoint{Kind: model.EndpointCommit, Hash: "HEAD"}
				right := model.Endpoint{Kind: model.EndpointWorkTree}
				v := &diffView{
					title:   path,
					context: i18n.T("HEAD ↔ working tree"),
					loading: true,
					partial: m.diffPartial,
					long:    m.diffLong,
				}
				m = m.pushLayer(v)
				m.diffNav = diffNavNone
				m.diffTag = "cmp:" + left.CacheTag() + ":" + right.CacheTag() + ":" + path
				return m, m.loadCompareDiffCmd(left, right, contentLine{path: path})
			},
		},
		{
			id:    "ff-history",
			label: i18n.T("History"),
			run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.popLayer()
				ctx := navContext{path: path}
				hv := newHistoryView(ctx)
				m = m.pushLayer(hv)
				return m, m.loadHistoryListCmd(ctx, hv.listTag)
			},
		},
		{
			id:    "ff-blame",
			label: i18n.T("Blame"),
			run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.popLayer()
				ctx := navContext{path: path}
				bv := newBlameView(ctx)
				m = m.pushLayer(bv)
				return m, m.loadBlameCmd(ctx, bv.tag)
			},
		},
		{
			id:    "ff-editor",
			label: i18n.T("Open in editor"),
			run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.popLayer()
				return m, m.openInEditorCmd(path, func(ctx context.Context) ([]byte, error) {
					return svc.ShowFile(ctx, "HEAD", path)
				})
			},
		},
		{
			id:    "ff-copy-path",
			label: i18n.T("Copy path"),
			run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.popLayer()
				return m, m.copyToClipboardCmd(i18n.T("Copied %s", path), path)
			},
		},
		{
			id:    "ff-copy-abspath",
			label: i18n.T("Copy absolute path"),
			run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.popLayer()
				abs := m.absFilePath("", path)
				return m, m.copyToClipboardCmd(i18n.T("Copied %s", abs), abs)
			},
		},
		{
			id:    "ff-commits-touching",
			label: i18n.T("Commits touching this"),
			run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.popLayer()
				m.commitFilter = commitFilterFields{Paths: []string{path}}
				m = m.focusCommitsPanel()
				return m.startFeedReload()
			},
		},
	}
}

// fileContentLayerMsg carries the async result of loadFileContentLayerCmd: the
// content lines (or error) for the contentPopup pushed onto the layer stack by
// the "View content" file-finder action. Tagged by path to gate stale loads.
type fileContentLayerMsg struct {
	path  string
	lines []contentLine
	err   error
}

// loadFileContentLayerCmd reads path at HEAD off the UI thread and delivers
// fileContentLayerMsg. Fills the contentPopup layer (title "View <path>"),
// NOT m.filesPreview (the files-view right column).
func (m Model) loadFileContentLayerCmd(path string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		data, err := svc.ShowFile(context.Background(), "HEAD", path)
		if err != nil {
			return fileContentLayerMsg{path: path, err: err}
		}
		if len(data) > domain.MaxDiffBytes {
			return fileContentLayerMsg{path: path, lines: []contentLine{{text: i18n.T("(file too large to preview)")}}}
		}
		return fileContentLayerMsg{path: path, lines: fileContentLines(data)}
	}
}
