package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

// historyMaxCommits bounds file-history depth on huge repos.
const historyMaxCommits = 200

// navContext identifies the file + revision a history/blame view explores.
// rev "" means the working-tree context (history from HEAD).
type navContext struct {
	path string
	rev  string
}

// historyView is the file-history surface: commits left, the file's diff at the
// selected commit on the right (reusing the diff pane).
type historyView struct {
	ctx     navContext
	commits []model.FileCommit
	sel     int
	loading bool
	err     error
	diff    *diffView // right pane (reuses diffView rendering + guards)
	listTag string    // gates stale list loads
	diffTag string    // gates stale right-pane loads
}

func newHistoryView(ctx navContext) *historyView {
	return &historyView{ctx: ctx, loading: true, listTag: "histlist:" + ctx.rev + ":" + ctx.path}
}

// historyListMsg / historyDiffMsg carry async results, tag-gated like diffMsg.
type historyListMsg struct {
	tag     string
	commits []model.FileCommit
	err     error
}
type historyDiffMsg struct {
	tag  string
	view *diffView
}

// loadHistoryListCmd fetches the commit list off the UI thread.
func (m Model) loadHistoryListCmd(ctx navContext, tag string) tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		cs, err := repo.FileLog(context.Background(), ctx.rev, ctx.path, historyMaxCommits)
		return historyListMsg{tag: tag, commits: cs, err: err}
	}
}

// loadHistoryDiffCmd builds the right-pane diff for fc: the file at fc vs its
// first parent, addressing the correct (possibly renamed) blob names.
func (m Model) loadHistoryDiffCmd(fc model.FileCommit, tag string) tea.Cmd {
	repo := m.repo
	v := &diffView{title: fc.Path, context: "@ " + shortHash(fc.Hash) + " " + fc.Subject, partial: m.diffPartial}
	return func() tea.Msg {
		var oldB, newB []byte
		if fc.Status != "A" {
			p := fc.Path
			if fc.OldPath != "" {
				p = fc.OldPath
			}
			b, err := repo.ShowFile(context.Background(), fc.Hash+"^", p)
			if err != nil {
				v.err = err
				return historyDiffMsg{tag: tag, view: v}
			}
			oldB = b
		}
		if fc.Status != "D" {
			b, err := repo.ShowFile(context.Background(), fc.Hash, fc.Path)
			if err != nil {
				v.err = err
				return historyDiffMsg{tag: tag, view: v}
			}
			newB = b
		}
		fillDiff(v, oldB, newB)
		return historyDiffMsg{tag: tag, view: v}
	}
}

// selectCmd (re)loads the right pane for the current selection.
func (h *historyView) selectCmd(m Model) tea.Cmd {
	if h.sel < 0 || h.sel >= len(h.commits) {
		return nil
	}
	fc := h.commits[h.sel]
	h.diffTag = "histdiff:" + fc.Hash + ":" + h.ctx.path
	h.diff = &diffView{title: fc.Path, context: "@ " + shortHash(fc.Hash) + " " + fc.Subject, loading: true}
	return m.loadHistoryDiffCmd(fc, h.diffTag)
}

// Temporary stubs so the package compiles; real implementations land in the
// next task.
func (h *historyView) render(m Model) string                           { return "" }
func (h *historyView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) { return m, nil }
