package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
)

// viewFileRow offers "View file" in the full-tree files view: show the selected
// file's content AT the commit (no diff) in the right column. Only meaningful on
// the tree side of full-tree mode, on a real file row.
func (m Model) viewFileRow() (actionRow, bool) {
	if m.filesView == nil || !m.filesAllFiles || !m.filesTreeFocused {
		return actionRow{}, false
	}
	vis := m.filesView.visible()
	if m.filesView.sel < 0 || m.filesView.sel >= len(vis) || vis[m.filesView.sel].path == "" {
		return actionRow{}, false
	}
	path, hash := vis[m.filesView.sel].path, m.filesHash
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

	vis := p.lines
	s0, s1, anchor := 0, len(vis), p.sel
	if p.mode != modeWrap && len(vis) > 2*rowsCap+1 {
		if s0 = p.sel - rowsCap; s0 < 0 {
			s0 = 0
		}
		if s1 = p.sel + rowsCap + 1; s1 > len(vis) {
			s1 = len(vis)
		}
		anchor = p.sel - s0
	}
	window := vis[s0:s1]
	wr := make([]winRow, len(window))
	for i, l := range window {
		wr[i] = winRow{text: l.text}
	}

	lines := make([]string, 0, contentH)
	lines = append(lines, padRight(truncate("View "+p.title, innerW), innerW))
	if len(vis) == 0 {
		lines = append(lines, padRight(truncate("  (empty)", innerW), innerW))
	} else {
		win := renderWindow(wr, winOpts{w: innerW, h: rowsCap, mode: p.mode, anchor: anchor, hscroll: p.hscroll})
		lines = append(lines, win...)
	}
	for len(lines) < contentH-1 {
		lines = append(lines, padRight("", innerW))
	}
	hint := fmt.Sprintf("%d/%d  [↑/↓] scroll  [z] view  [esc] close", p.sel+1, len(vis))
	lines = append(lines, padRight(truncate(hint, innerW), innerW))

	style := bluredPanel
	if !m.filesTreeFocused {
		style = focusedPanel
	}
	return style.Render(strings.Join(lines, "\n"))
}
