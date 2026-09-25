package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
)

// blameView is the single-file blame surface: the file's content with a
// per-line gutter naming the commit that last touched it. It reuses navContext
// (defined in history_view.go); rev "" blames HEAD's working content.
type blameView struct {
	ctx    navContext
	lines  []model.BlameLine
	tok    [][]syntax.Tok // syntax runs per line (index = line−1); nil = plain
	blocks []blameBlock   // grouped runs, recomputed after each load
	sel    int            // line cursor (index into lines)
	// lsel is the line selection over lines (space/space/enter, spec §4.7).
	// Cleared by blameMsg: a reload replaces the very lines it indexes.
	lsel    lineSel
	mode    dispMode // text display mode; z cycles (applies to the whole gutter│code row)
	hscroll int      // modeScroll horizontal offset
	loading bool
	err     error
	tag     string // gates stale loads
	// search is the in-view text search (spec §4.3); san caches every line's
	// display text (what the window paints) so a keystroke does not
	// re-sanitize a 40k-line file, and searchOrig is what esc restores.
	search     textSearch
	searchOrig blameOrigin
	san        []string
	// recent is the d-key age highlight. On the VIEW, so every open starts
	// off; the last dialog text lives on Model.blameRecentLast instead.
	recent blameRecent
}

// blameOrigin is the view state a live search restores when esc cancels it.
type blameOrigin struct{ sel, hscroll int }

// blameBlock is a maximal run of consecutive lines sharing a commit. hash ""
// means the run is uncommitted.
type blameBlock struct {
	start, end int
	hash       string
	author     string
	time       int64
}

func newBlameView(ctx navContext) *blameView {
	return &blameView{ctx: ctx, loading: true, tag: "blame:" + ctx.rev + ":" + ctx.path}
}

// groupBlame collapses maximal runs of lines sharing a Hash into blocks.
func groupBlame(lines []model.BlameLine) []blameBlock {
	var blocks []blameBlock
	for i, ln := range lines {
		if i == 0 || lines[i-1].Hash != ln.Hash {
			blocks = append(blocks, blameBlock{start: i, end: i, hash: ln.Hash, author: ln.Author, time: ln.Time})
		} else {
			blocks[len(blocks)-1].end = i
		}
	}
	return blocks
}

// blockAt returns the block containing line, if any.
func blockAt(blocks []blameBlock, line int) (blameBlock, bool) {
	for _, b := range blocks {
		if line >= b.start && line <= b.end {
			return b, true
		}
	}
	return blameBlock{}, false
}

// ensureSan builds the per-line display strings the search matches. They are
// exactly what the window shows: sanitizeCell's disp for a lexed file is the
// same expansion sanitizeLine performs, so one cache serves both paths.
func (b *blameView) ensureSan() {
	if len(b.san) == len(b.lines) {
		return
	}
	b.san = make([]string, len(b.lines))
	for i, ln := range b.lines {
		b.san[i] = sanitizeLine(ln.Content)
	}
}

func (b *blameView) searchText(i int) string {
	b.ensureSan()
	if i < 0 || i >= len(b.san) {
		return ""
	}
	return b.san[i]
}

// searchLines is the code, and only the code: the commit gutter lives in
// winRow.prefix, never in text, so "code lines only" (spec §4.3) is automatic.
func (b *blameView) searchLines() []searchLine {
	b.ensureSan()
	out := make([]searchLine, len(b.san))
	for i, t := range b.san {
		out[i] = searchLine{row: i, side: 0, text: t}
	}
	return out
}

// searchPos is where ] and [ measure from — the current hit while the cursor is
// still on its line, else the head of the cursor line.
func (b *blameView) searchPos() searchPos {
	if b.search.cur >= 0 && b.search.cur < len(b.search.hits) {
		if h := b.search.hits[b.search.cur]; h.row == b.sel {
			return searchPos{row: h.row, side: h.side, col: h.start}
		}
	}
	return searchPos{row: b.sel, side: 0, col: -1}
}

// goToHit selects hits[i]: the line cursor moves onto it (the render anchors
// its window on sel, so the scroll follows) and scroll mode pans to its column.
func (b *blameView) goToHit(m Model, i int) {
	if i < 0 || i >= len(b.search.hits) {
		return
	}
	h := b.search.hits[i]
	b.search.cur = i
	b.sel = h.row
	if b.mode != modeScroll {
		return
	}
	w, _ := m.overlayDims()
	gw := blameGutterW
	if gw > w-10 {
		gw = w - 10
	}
	if gw < 0 {
		gw = 0
	}
	cs, ce := hitCols(b.searchText(h.row), h)
	b.hscroll = panFor(b.hscroll, w-(gw+1), cs, ce) // prefixW = gw+1
}

// blameSearchKey gives the in-view search first refusal on a key (see
// diffSearchKey; the shape is deliberately identical across the four hosts).
func (m Model) blameSearchKey(b *blameView, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if b.search.typing {
		nm, cmd, ev := m.searchTypingKey(&b.search, msg)
		m = nm
		switch ev {
		case searchChanged, searchCommitted:
			b.search.refindFrom(b.searchLines(), b.search.origin)
			if b.search.cur >= 0 {
				b.goToHit(m, b.search.cur)
			} else {
				b.sel, b.hscroll = b.searchOrig.sel, b.searchOrig.hscroll
			}
		case searchCancelled:
			b.sel, b.hscroll = b.searchOrig.sel, b.searchOrig.hscroll
		}
		return m, cmd, true
	}
	switch searchCommandKey(&b.search, msg) {
	case searchOpenFwd, searchOpenBack:
		b.searchOrig = blameOrigin{sel: b.sel, hscroll: b.hscroll}
		b.search.open(msg.String() == "@", b.searchPos())
		return m.recallReset(), nil, true
	case searchNext:
		b.goToHit(m, stepHit(b.search.hits, b.searchPos(), 1))
		return m, nil, true
	case searchPrev:
		b.goToHit(m, stepHit(b.search.hits, b.searchPos(), -1))
		return m, nil, true
	case searchCleared:
		b.search.clear()
		return m, nil, true
	case searchIgnored:
		return m, nil, true
	}
	return m, nil, false
}

// blameAge is a compact relative age (now/5m/3h/2d/3mo/2y) for the gutter.
// ageString in repo_popup.go caps at days and is too wide for a fixed gutter.
func blameAge(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return i18n.T("now")
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 365*24*time.Hour:
		days := int(d.Hours() / 24)
		if days < 30 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dmo", days/30)
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/24/365))
	}
}

// blameMsg carries the async blame result, tag-gated like historyListMsg.
type blameMsg struct {
	tag   string
	lines []model.BlameLine
	tok   [][]syntax.Tok
	err   error
}

// loadBlameCmd fetches blame off the UI thread, lexing the blamed content in
// the same goroutine (chroma costs ~1 s at MaxSyntaxBytes — never on the UI
// thread). The syntax switch is read at DISPATCH time so a config reload
// mid-load cannot flip the answer under the closure.
func (m Model) loadBlameCmd(ctx navContext, tag string) tea.Cmd {
	svc, on := m.svc, m.cfg.UI.SyntaxOn()
	return func() tea.Msg {
		ls, err := svc.Blame(context.Background(), ctx.rev, ctx.path)
		if err != nil {
			return blameMsg{tag: tag, lines: ls, err: err}
		}
		return blameMsg{tag: tag, lines: ls, tok: lexBlame(ctx.path, ls, on)}
	}
}

// lexBlame is domain.LexBlameLines behind the [ui] diff_syntax switch: nil
// (plain rendering) when the switch is off, the path has no lexer, the file is
// past domain.MaxSyntaxBytes, or a line holds a BARE \r. That last case is not
// a desync here — blame lines are already split, and sanitizeCell maps an
// interior \r to one '·' exactly as the lexer counts it — but such content is
// pathological enough that every self-lexing surface refuses it alike (the
// file preview, which turns a lone \r into a line break, genuinely must). The
// web blame overlay shares the same domain helper.
func lexBlame(path string, lines []model.BlameLine, on bool) [][]syntax.Tok {
	if !on {
		return nil
	}
	return domain.LexBlameLines(path, lines)
}

// blameGutterW is the fixed gutter width: shortHash(7) + space + author(12) +
// space + age(≤6), padded.
const blameGutterW = 28

func (m Model) blameBodyRows() int {
	_, h := m.overlayDims()
	n := h - 2
	if n < 1 {
		n = 1
	}
	return n
}

func (b *blameView) render(m Model, _ string) string {
	w, scrH := m.overlayDims()
	body := m.blameBodyRows()

	title := i18n.T("blame: %s", b.ctx.path+revSuffix(b.ctx.rev))
	header := elidePath(title, w) // keep the file name
	// Right-aligned badges, like the diff's: the age-filter badge (-7d, +30d,
	// +1d -7d) and the search badge share the slot as "-7d · /q  1/3".
	bd := b.search.badge()
	if b.recent.on {
		rb := b.recent.f.String()
		if bd != "" {
			bd = rb + " · " + bd
		} else {
			bd = rb
		}
	}
	if bd != "" {
		avail := w - lipgloss.Width(bd) - 2
		if avail < 1 {
			avail = 1
		}
		header = truncate(padRight(elidePath(title, avail), avail)+"  "+bd, w)
	}
	hint := truncate(i18n.T("[↑↓] line  [pgup/pgdn] page  [spc] mark  [/] find  [enter] history  [e] editor  [esc/b] back  [d] age"), w)
	if b.lsel.on {
		// The selection variant replaces the lot, so the way out (esc) is
		// always on screen.
		hint = truncate(i18n.T("[space] mark end  [enter] copy  [esc] unmark  [↑↓] extend"), w)
	}

	gw := blameGutterW
	if gw > w-10 {
		gw = w - 10
	}
	if gw < 0 {
		gw = 0
	}

	now := time.Now()
	// Build a winRow only for the lines renderWindow can actually show — a
	// full-file blame is thousands of lines, and building+lex-mapping every
	// one of them on every frame (most of it never rendered) is wasted work.
	// windowRowBounds is the exact bound renderWindow itself would compute,
	// so the two can never disagree.
	lo, hi := windowRowBounds(len(b.lines), body, b.sel, b.mode)
	wr := make([]winRow, hi-lo)
	s := st()
	for i := lo; i < hi; i++ {
		ln := b.lines[i]
		gutter := padRight("", gw)
		// i is the FULL (unsliced) line index, so a visible row that starts a
		// block right at the window's top edge still looks one row back — at
		// b.lines[i-1], which may itself be off-window — to decide whether it
		// carries the gutter text.
		if i == 0 || b.lines[i-1].Hash != ln.Hash {
			gutter = padRight(truncate(blameGutterText(ln, now), gw), gw)
		}
		// The gutter is a frozen left column (prefix); only the code body
		// wraps/scrolls per b.mode, so a long line never bleeds across the author
		// column. Sanitize tabs/control runes (like the diff pane) so indentation
		// maps to display columns.
		var st lipgloss.Style
		if i == b.sel {
			st = s.selectedRow
		} else if b.recent.on && lineMatches(ln, now, b.recent.f) {
			// The age tint is the ROW style, so the gutter (winRow.prefix)
			// wears it too and a block's extent reads. The cursor row keeps its
			// reverse video untouched, and the selection stripe below composes
			// over the tint on the code half (most specific wins).
			st = s.blameRecentStyle(st)
		}
		// The stripe paints the CODE only: the commit gutter is winRow.prefix
		// and keeps the row style, so the eye reads the range's extent against
		// an unchanged gutter. On the cursor row the stripe sits over the
		// reverse-video base, which with selection_bg unset un-reverses it — the
		// "hole" the theme contract describes.
		var bodySt *lipgloss.Style
		if b.lsel.contains(i, b.sel) {
			sel := s.selectionStyle(st)
			bodySt = &sel
		}
		// Search hits ride on winRow.emph — a separate mask from cls, so an
		// unlexed file still paints them, and they survive the selected row's
		// reverse video (which is exactly where the current hit lands).
		var emph []emphLevel
		if b.search.active() {
			if hs := b.search.hitsOn(i, 0); len(hs) > 0 {
				emph = overlayHits(nil, 0, len([]rune(b.searchText(i))), hs)
			}
		}
		if b.tok == nil { // no lexer / colouring off: the plain (pre-syntax) path
			wr[i-lo] = winRow{prefix: gutter + "│", text: sanitizeLine(ln.Content), emph: emph, style: st, body: bodySt}
			continue
		}
		// sanitizeCell expands exactly like sanitizeLine but also returns the
		// per-display-rune class mask, so tabs and control glyphs keep the
		// colours aligned with the columns they land on.
		disp, _, cls := sanitizeCell(ln.Content, nil, tokAt(b.tok, i+1))
		wr[i-lo] = winRow{prefix: gutter + "│", text: string(disp), cls: cls, emph: emph, style: st, body: bodySt}
	}

	win := renderWindow(wr, winOpts{w: w, h: body, mode: b.mode, anchor: b.sel - lo, hscroll: b.hscroll, prefixW: gw + 1, charWrap: true})
	switch {
	case b.loading:
		win = padLines(i18n.T("  (loading…)"), w, body)
	case b.err != nil:
		win = padLines(i18n.T("  error: %s", b.err.Error()), w, body)
	case len(b.lines) == 0:
		win = padLines(i18n.T("  (empty)"), w, body)
	}

	out := header + "\n" + strings.Join(win, "\n") + "\n" + hint
	return clipToHeight(out, scrH)
}

// revSuffix annotates the header with the blamed revision, if any.
func revSuffix(rev string) string {
	if rev == "" {
		return ""
	}
	return " @ " + shortHash(rev)
}

// blameGutterText is the per-block remark: hash, author (≤12), compact age.
func blameGutterText(ln model.BlameLine, now time.Time) string {
	if ln.Hash == "" {
		return i18n.T("(uncommitted)")
	}
	author := padRight(truncate(ln.Author, 12), 12)
	return shortHash(ln.Hash) + " " + author + " " + blameAge(now, time.Unix(ln.Time, 0))
}

func (b *blameView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// Order (spec §4.7): a search being TYPED owns every key, then the line
	// selection, then a committed query, then the view's own esc. blameSearchKey
	// covers the first and third in one call, so the selection hook runs first
	// and declines while the search is typing. b is NOT two-stage at all: it
	// always leaves (never trap the user behind a search or a selection).
	if nm, cmd, handled := m.blameSelectKey(b, msg); handled {
		return nm, cmd
	}
	if nm, cmd, handled := m.blameSearchKey(b, msg); handled {
		return nm, cmd
	}
	switch msg.String() {
	case ".":
		return m.openActionMenu(), nil
	case "g": // global bookmark quick-switcher
		return m.openBookmarkSwitcher()
	case "G": // global shelf quick-switcher
		return m.openShelfSwitcher()
	case "F": // the working-tree files window (F)
		return m.openWorktreeFilesHere()
	case "ctrl+w":
		b.mode = b.mode.next()
		b.hscroll = 0
		return m, nil
	case "shift+left":
		if b.mode == modeScroll && b.hscroll > 0 {
			if b.hscroll -= m.hscrollStep(); b.hscroll < 0 {
				b.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if b.mode == modeScroll {
			b.hscroll += m.hscrollStep()
		}
		return m, nil
	// q is inert here: only the base layout quits on q. esc is the back key;
	// ctrl+c (handled above) remains the universal quit.
	case "esc", "b":
		return m.popLayer(), nil
	case "d": // highlight lines by commit age (also to change the filter)
		return m.openBlameRecentPopup(b)
	case "D": // highlight off; the last text is kept on the Model for the next d
		b.recent.on = false
		return m, nil
	case "down", "j":
		if b.sel < len(b.lines)-1 {
			b.sel++
		}
		return m, nil
	case "up", "k":
		if b.sel > 0 {
			b.sel--
		}
		return m, nil
	case "pgdown":
		b.sel += m.blameBodyRows()
		if b.sel > len(b.lines)-1 {
			b.sel = len(b.lines) - 1
		}
		if b.sel < 0 {
			b.sel = 0
		}
		return m, nil
	case "pgup":
		b.sel -= m.blameBodyRows()
		if b.sel < 0 {
			b.sel = 0
		}
		return m, nil
	case "e": // open the blamed file (at this rev) in $EDITOR (read-only)
		if r, ok := m.surfaceExternalRow(); ok {
			nm, cmd := r.run(m)
			return nm.(Model), cmd
		}
	case "enter":
		blk, ok := blockAt(b.blocks, b.sel)
		if !ok || blk.hash == "" {
			return m, nil
		}
		ctx := navContext{path: b.ctx.path, rev: blk.hash}
		hv := newHistoryView(ctx)
		m = m.pushLayer(hv)
		return m, m.loadHistoryListCmd(ctx, hv.listTag)
	}
	return m, nil
}
