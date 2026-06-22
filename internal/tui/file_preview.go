package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
)

// viewFileRow offers "View file" in the commit files view: show the selected
// file's content AT the commit (no diff) in the right column. Available on the
// tree side of either mode — the full tree or the changed set — on a real file
// row. Compare mode is skipped (two endpoints, no single commit), and a deleted
// (D) row is skipped (the file has no content at this commit).
func (m Model) viewFileRow() (actionRow, bool) {
	if m.filesView == nil || m.filesCompare || !m.filesTreeFocused {
		return actionRow{}, false
	}
	vis := m.filesView.visible()
	if m.filesView.sel < 0 || m.filesView.sel >= len(vis) {
		return actionRow{}, false
	}
	l := vis[m.filesView.sel]
	if l.path == "" || l.status == "D" {
		return actionRow{}, false
	}
	path, hash := l.path, m.filesHash
	return actionRow{
		id:    "view-file",
		label: "View file (content at this commit)",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openFilePreview(hash, path)
		},
	}, true
}

// openFilePreview opens the right-column content preview for path at hash and
// focuses it so the cursor scrolls the content immediately.
func (m Model) openFilePreview(hash, path string) (Model, tea.Cmd) {
	m.filesPreview = &contentPopup{title: path, lines: []contentLine{{text: "(loading…)"}}}
	m.filesPreviewTag = path + "@" + hash
	m.filesTreeFocused = false // land in the preview to scroll
	return m, m.loadFileContentCmd(hash, path)
}

// fileContentMsg carries a previewed file's content lines, tagged so a stale load
// (the user opened another file) is dropped.
type fileContentMsg struct {
	tag   string
	lines []contentLine
	err   error
}

// loadFileContentCmd reads path's bytes at hash and splits them into content
// lines off the UI thread.
func (m Model) loadFileContentCmd(hash, path string) tea.Cmd {
	svc := m.svc
	tag := path + "@" + hash
	return func() tea.Msg {
		data, err := svc.ShowFile(context.Background(), hash, path)
		if err != nil {
			return fileContentMsg{tag: tag, err: err}
		}
		if len(data) > domain.MaxDiffBytes {
			return fileContentMsg{tag: tag, lines: []contentLine{{text: "(file too large to preview)"}}}
		}
		return fileContentMsg{tag: tag, lines: fileContentLines(data)}
	}
}

// fileContentLines splits raw file bytes into one contentLine per line, expanding
// tabs so width math and alignment behave.
func fileContentLines(data []byte) []contentLine {
	s := strings.TrimRight(string(data), "\n")
	if s == "" {
		return []contentLine{{text: "(empty file)"}}
	}
	parts := strings.Split(s, "\n")
	out := make([]contentLine, len(parts))
	for i, ln := range parts {
		out[i] = contentLine{text: strings.ReplaceAll(ln, "\t", "    ")}
	}
	return out
}

// filePreviewRowsCap is how many content lines the preview window shows right
// now; it mirrors renderFilePreview's math (border 2 + title + hint = 4) so the
// pager clamp in the move handler agrees with what is actually rendered.
func (m Model) filePreviewRowsCap() int {
	rowsCap := m.layout().boxH[panelCommits] - 4
	if rowsCap < 1 {
		rowsCap = 1
	}
	return rowsCap
}

// previewClamp clamps a pager top-line index to [0, maxTop]. maxTop keeps the
// last screenful in view (n-rowsCap); wrapped rows have variable height, so wrap
// mode falls back to "scroll until the last line reaches the top" (n-1).
func previewClamp(top, n, rowsCap int, mode dispMode) int {
	maxTop := n - rowsCap
	if mode == modeWrap {
		maxTop = n - 1
	}
	if maxTop < 0 {
		maxTop = 0
	}
	if top > maxTop {
		top = maxTop
	}
	if top < 0 {
		top = 0
	}
	return top
}

// renderFilePreview draws the file content as the right column (replacing the
// Commits panel) while a preview is open. Window-then-build (a file can be large);
// the border follows focus.
func (m Model) renderFilePreview(boxW, boxH int) string {
	p := m.filesPreview
	contentH := boxH - 2 // top/bottom border
	if contentH < 1 {
		contentH = 1
	}
	innerW := boxW - 4 // border (2) + horizontal padding (2)
	if innerW < 1 {
		innerW = 1
	}
	rowsCap := contentH - 2 // title + hint lines
	if rowsCap < 1 {
		rowsCap = 1
	}

	// The preview is a pager: p.sel is the TOP visible line, not a cursor, so every
	// ↑/↓ scrolls the viewport by one (there is no on-screen cursor to walk to an
	// edge first). Top-anchor the window (anchor 0) so renderWindow can't re-center
	// the slice and re-introduce the dead zone.
	vis := p.lines
	start := previewClamp(p.sel, len(vis), rowsCap, p.mode)
	end := start + rowsCap
	if end > len(vis) {
		end = len(vis)
	}
	window := vis[start:end]
	wr := make([]winRow, len(window))
	for i, l := range window {
		wr[i] = winRow{text: l.text}
	}

	lines := make([]string, 0, contentH)
	lines = append(lines, padRight(truncate("View "+p.title, innerW), innerW))
	if len(vis) == 0 {
		lines = append(lines, padRight(truncate("  (empty)", innerW), innerW))
	} else {
		win := renderWindow(wr, winOpts{w: innerW, h: rowsCap, mode: p.mode, anchor: 0, hscroll: p.hscroll})
		lines = append(lines, win...)
	}
	for len(lines) < contentH-1 {
		lines = append(lines, padRight("", innerW))
	}
	hint := fmt.Sprintf("%d/%d  [↑/↓] scroll  [z] view  [esc] close", start+1, len(vis))
	lines = append(lines, padRight(truncate(hint, innerW), innerW))

	style := bluredPanel
	if !m.filesTreeFocused {
		style = focusedPanel
	}
	return style.Render(strings.Join(lines, "\n"))
}
