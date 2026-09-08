package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// editLine is the NEW-side line `e` opens: the cursor row's RightNo, or on a
// Del row (no new-side number) the next row that has one, else the last row
// that has one. 0 when the view has no numbered row at all.
func (v *diffView) editLine() int {
	r, ok := v.cursorRow()
	if ok && r.RightNo > 0 {
		return r.RightNo
	}
	for i := v.curLine + 1; i < len(v.lines); i++ {
		if ln := v.lines[i]; ln.Fold == 0 && ln.Row.RightNo > 0 {
			return ln.Row.RightNo
		}
	}
	for i := len(v.lines) - 1; i >= 0; i-- {
		if ln := v.lines[i]; ln.Fold == 0 && ln.Row.RightNo > 0 {
			return ln.Row.RightNo
		}
	}
	return 0
}

// diffEditRow is "Open in editor at line N" for the diff view on top: a
// working-tree diff (rev == "") opens the real file for live editing — the
// staged diff's new side is the index, so its line is an approximation of the
// working file; a commit diff opens a read-only temp copy of rev:path at the
// line. Absent on a two-sided compare (no single file) and while an op runs.
func (m Model) diffEditRow() (actionRow, bool) {
	v, ok := m.topLayer().(*diffView)
	if !ok || v.compare || v.loading || v.err != nil || v.binary || v.tooLarge || !m.opsIdle() {
		return actionRow{}, false
	}
	line := v.editLine()
	path, rev, svc := v.title, v.rev, m.svc
	return actionRow{
		id:    "diff-edit-at-line",
		label: i18n.T("Open in editor at line %d", line),
		run: func(m Model) (tea.Model, tea.Cmd) {
			if rev == "" {
				return m, m.editFileAtCmd(path, line)
			}
			return m, m.openInEditorAtCmd(path, line, func(ctx context.Context) ([]byte, error) {
				return svc.ShowFile(ctx, rev, path)
			})
		},
	}, true
}
