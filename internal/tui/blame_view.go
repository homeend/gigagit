package tui

import (
	"context"
	"fmt"
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

// render/update implemented in Task 6 — stubs so *blameView satisfies surface.
func (b *blameView) render(m Model) string                          { return "" }
func (b *blameView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) { return m, nil }
