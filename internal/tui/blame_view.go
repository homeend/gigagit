package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

// blameView is the single-file blame surface: the file's content with a
// per-line gutter naming the commit that last touched it. It reuses navContext
// (defined in history_view.go); rev "" blames HEAD's working content.
type blameView struct {
	ctx     navContext
	lines   []model.BlameLine
	blocks  []blameBlock // grouped runs, recomputed after each load
	sel     int          // line cursor (index into lines)
	loading bool
	err     error
	tag     string // gates stale loads
}

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

// blameAge is a compact relative age (now/5m/3h/2d/3mo/2y) for the gutter.
// ageString in repo_popup.go caps at days and is too wide for a fixed gutter.
func blameAge(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
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
	err   error
}

// loadBlameCmd fetches blame off the UI thread.
func (m Model) loadBlameCmd(ctx navContext, tag string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ls, err := svc.Blame(context.Background(), ctx.rev, ctx.path)
		return blameMsg{tag: tag, lines: ls, err: err}
	}
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

func (b *blameView) render(m Model) string {
	w, scrH := m.overlayDims()
	body := m.blameBodyRows()

	header := truncate("blame: "+b.ctx.path+revSuffix(b.ctx.rev), w)
	hint := truncate("[↑↓] line  [pgup/pgdn] page  [enter] history  [esc/b] back", w)

	gw := blameGutterW
	if gw > w-10 {
		gw = w - 10
	}
	if gw < 0 {
		gw = 0
	}
	codeW := w - gw - 1
	if codeW < 1 {
		codeW = 1
	}

	now := time.Now()
	rows := make([]string, len(b.lines))
	for i, ln := range b.lines {
		gutter := padRight("", gw)
		if i == 0 || b.lines[i-1].Hash != ln.Hash {
			gutter = padRight(truncate(blameGutterText(ln, now), gw), gw)
		}
		// Sanitize tabs/control runes (like the diff pane) so indentation maps to
		// display columns, and pad EVERY row to full width so scrolling never
		// leaves stale cells from a longer prior line (or a moved highlight).
		content := truncate(sanitizeLine(ln.Content), codeW)
		row := padRight(gutter+"│"+content, w)
		if i == b.sel {
			row = selectedRow.Render(row)
		}
		rows[i] = row
	}

	win, _, _ := windowRows(rows, body, b.sel)
	switch {
	case b.loading:
		win = []string{"  (loading…)"}
	case b.err != nil:
		win = []string{truncate("  error: "+b.err.Error(), w)}
	case len(b.lines) == 0:
		win = []string{"  (empty)"}
	}
	for len(win) < body {
		win = append(win, "")
	}

	out := header + "\n" + strings.Join(win[:body], "\n") + "\n" + hint
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
		return "(uncommitted)"
	}
	author := padRight(truncate(ln.Author, 12), 12)
	return shortHash(ln.Hash) + " " + author + " " + blameAge(now, time.Unix(ln.Time, 0))
}

func (b *blameView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	// q is inert here: only the base layout quits on q. esc is the back key;
	// ctrl+c (handled above) remains the universal quit.
	case "esc", "b":
		return m.popSurface(), nil
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
	case "enter":
		blk, ok := blockAt(b.blocks, b.sel)
		if !ok || blk.hash == "" {
			return m, nil
		}
		ctx := navContext{path: b.ctx.path, rev: blk.hash}
		hv := newHistoryView(ctx)
		m = m.pushSurface(hv)
		return m, m.loadHistoryListCmd(ctx, hv.listTag)
	}
	return m, nil
}
