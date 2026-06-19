package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/domain"
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
	mode    dispMode // left list text display mode; z cycles
	hscroll int      // modeScroll horizontal offset
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
	svc := m.svc
	return func() tea.Msg {
		cs, err := svc.FileLog(context.Background(), ctx.rev, ctx.path, historyMaxCommits)
		return historyListMsg{tag: tag, commits: cs, err: err}
	}
}

// loadHistoryDiffCmd builds the right-pane diff for fc: the file at fc vs its
// first parent, addressing the correct (possibly renamed) blob names.
func (m Model) loadHistoryDiffCmd(fc model.FileCommit, tag string) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	body := m.diffBodyRows()
	v := &diffView{title: fc.Path, context: "@ " + shortHash(fc.Hash) + " " + fc.Subject, rev: fc.Hash, partial: m.diffPartial}
	// Immutable: parent(hash)→hash for a path always yields the same bytes.
	key := fc.Hash + "^.." + fc.Hash + ":" + fc.Path

	var oldSrc, newSrc domain.ByteSource
	if fc.Status != "A" {
		p := fc.Path
		if fc.OldPath != "" {
			p = fc.OldPath
		}
		oldSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, fc.Hash+"^", p) }
	}
	if fc.Status != "D" {
		newSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, fc.Hash, fc.Path) }
	}
	return func() tea.Msg {
		out, err := differ.Diff(context.Background(), domain.Request{Key: key, Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return historyDiffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
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
// historyBodyRows is the list/pane height: full height minus header + hint.
func (m Model) historyBodyRows() int {
	_, h := m.overlayDims()
	n := h - 2
	if n < 1 {
		n = 1
	}
	return n
}

func (h *historyView) render(m Model) string {
	w, scrH := m.overlayDims()
	body := m.historyBodyRows()

	header := truncate("history: "+h.ctx.path, w)
	hint := truncate("[↑↓] commit  [esc] back", w)

	// Left list. Right pane shown only when wide enough (>=60); else list-only.
	split := w >= 60
	listW := w
	if split {
		listW = (w - 1) / 2
	}

	wr := make([]winRow, len(h.commits))
	for i, fc := range h.commits {
		line := shortHash(fc.Hash) + "  " + fc.Status + "  " + fc.Subject
		prefix := "  "
		var st lipgloss.Style
		if i == h.sel {
			prefix = "> "
			st = selectedRow
		}
		wr[i] = winRow{text: prefix + line, style: st}
	}
	win := renderWindow(wr, winOpts{w: listW, h: body, mode: h.mode, anchor: h.sel, hscroll: h.hscroll})
	if h.loading {
		win = padLines("  (loading…)", listW, body)
	} else if h.err != nil {
		win = padLines("  error: "+h.err.Error(), listW, body)
	} else if len(h.commits) == 0 {
		win = padLines("  (no history)", listW, body)
	}
	for len(win) < body {
		win = append(win, padRight("", listW))
	}

	var bodyStr string
	if split {
		right := h.renderRightPane(m, w-listW-1, body)
		rightCol := strings.Split(right, "\n")
		leftCol := make([]string, body)
		for i := 0; i < body; i++ {
			l := ""
			if i < len(win) {
				l = win[i]
			}
			r := ""
			if i < len(rightCol) {
				r = rightCol[i]
			}
			leftCol[i] = l + "│" + r
		}
		bodyStr = strings.Join(leftCol, "\n")
	} else {
		bodyStr = strings.Join(win, "\n")
	}

	out := header + "\n" + bodyStr + "\n" + hint
	return clipToHeight(out, scrH)
}

// renderRightPane draws the selected commit's diff using the shared diff pane.
func (h *historyView) renderRightPane(m Model, w, body int) string {
	if h.diff == nil {
		return padBox("  (select a commit)", w, body)
	}
	v := h.diff
	switch {
	case v.loading:
		return padBox("  (loading…)", w, body)
	case v.err != nil:
		return padBox(truncate("  error: "+v.err.Error(), w), w, body)
	case v.binary:
		return padBox("  (binary file)", w, body)
	case v.tooLarge:
		return padBox("  (file too large)", w, body)
	}
	lines := m.diffPaneLines(v, w, body)
	for len(lines) < body {
		lines = append(lines, "")
	}
	return strings.Join(lines[:body], "\n")
}

// padLines returns a body-high block whose first line is s (truncated to w),
// the rest blank — the list-pane placeholder for loading/error/empty states.
func padLines(s string, w, body int) []string {
	if body < 1 {
		body = 1
	}
	out := make([]string, body)
	out[0] = padRight(truncate(s, w), w)
	for i := 1; i < body; i++ {
		out[i] = padRight("", w)
	}
	return out
}

// padBox renders s as the first line of a w×body block, blank-filled.
func padBox(s string, w, body int) string {
	lines := make([]string, body)
	lines[0] = padRight(truncate(s, w), w)
	for i := 1; i < body; i++ {
		lines[i] = padRight("", w)
	}
	return strings.Join(lines, "\n")
}

func (h *historyView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case ".":
		return m.openActionMenu(), nil
	case "g": // global bookmark quick-switcher
		return m.openBookmarkSwitcher()
	case "G": // global shelf quick-switcher
		return m.openShelfSwitcher()
	case "z":
		h.mode = h.mode.next()
		h.hscroll = 0
		return m, nil
	case "shift+left":
		if h.mode == modeScroll && h.hscroll > 0 {
			if h.hscroll -= m.hscrollStep(); h.hscroll < 0 {
				h.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if h.mode == modeScroll {
			h.hscroll += m.hscrollStep()
		}
		return m, nil
	// q is inert here: only the base layout quits on q. esc is the back key;
	// ctrl+c (handled above) remains the universal quit.
	case "esc", "h":
		return m.popSurface(), nil
	case "b":
		if h.sel >= 0 && h.sel < len(h.commits) {
			fc := h.commits[h.sel]
			// Blame the file under the name it had *at this commit* (fc.Path):
			// across a rename/copy the current name need not exist in the
			// commit's tree, which would make `git blame` fail (exit 128).
			path := fc.Path
			if path == "" {
				path = h.ctx.path
			}
			ctx := navContext{path: path, rev: fc.Hash}
			bv := newBlameView(ctx)
			m = m.pushSurface(bv)
			return m, m.loadBlameCmd(ctx, bv.tag)
		}
	case "down", "j":
		if h.sel < len(h.commits)-1 {
			h.sel++
			return m, h.selectCmd(m)
		}
	case "up", "k":
		if h.sel > 0 {
			h.sel--
			return m, h.selectCmd(m)
		}
	}
	return m, nil
}
