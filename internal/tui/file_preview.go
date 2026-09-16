package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
)

// filesViewSelectedLine returns the currently-selected content line in the
// files-view tree side, guarding all preconditions: view open, not compare
// mode, tree focused, selection in bounds, non-heading row with a path. Callers
// apply additional per-action status filters (e.g. skip deleted files for
// content-opening actions).
func (m Model) filesViewSelectedLine() (contentLine, bool) {
	if m.filesView == nil || m.inCompareMode() || !m.filesTreeFocused {
		return contentLine{}, false
	}
	vis := m.filesView.visible()
	if m.filesView.sel < 0 || m.filesView.sel >= len(vis) {
		return contentLine{}, false
	}
	l := vis[m.filesView.sel]
	if l.path == "" {
		return contentLine{}, false
	}
	return l, true
}

// viewFileRow offers "View file" in the commit files view: show the selected
// file's content AT the commit (no diff) in the right column. Available on the
// tree side of either mode — the full tree or the changed set — on a real file
// row. Compare mode is skipped (two endpoints, no single commit), and a deleted
// (D) row is skipped (the file has no content at this commit).
func (m Model) viewFileRow() (actionRow, bool) {
	l, ok := m.filesViewSelectedLine()
	if !ok || l.status == "D" {
		return actionRow{}, false
	}
	path, hash := l.path, m.lineHash(l)
	if m.inShelfFiles() {
		// Shelf mode: the frozen member bytes, not ShowFile — filesHash is empty
		// here and `git show :path` would silently preview the INDEX blob.
		ref := model.FileRef{Source: model.SourceShelf, Locator: m.filesShelfID, Path: path}
		svc, tag := m.svc, path+"@shelf:"+m.filesShelfID
		return actionRow{
			id:    "view-file",
			label: i18n.T("View file (frozen shelf content)"),
			run: func(m Model) (tea.Model, tea.Cmd) {
				return m.openPreviewSrc(tag, path, func(ctx context.Context) ([]byte, error) {
					return svc.ResolveBytes(ctx, ref)
				})
			},
		}, true
	}
	return actionRow{
		id:    "view-file",
		label: i18n.T("View file (content at this commit)"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openPreview(hash, path)
		},
	}, true
}

// openExternalRow offers "Open in external editor" in the commit files view:
// resolve the selected file's content AT the commit into a read-only temp file
// and open it in $EDITOR. Same gating as viewFileRow — tree side, a real file
// row, not compare mode, and not a deleted (D) row.
func (m Model) openExternalRow() (actionRow, bool) {
	l, ok := m.filesViewSelectedLine()
	if !ok || l.status == "D" {
		return actionRow{}, false
	}
	path, hash, svc := l.path, m.lineHash(l), m.svc
	if m.inShelfFiles() {
		// Shelf mode: same INDEX-blob trap as viewFileRow — resolve the member.
		ref := model.FileRef{Source: model.SourceShelf, Locator: m.filesShelfID, Path: path}
		return actionRow{
			id:    "open-external",
			label: i18n.T("Open in external editor"),
			run: func(m Model) (tea.Model, tea.Cmd) {
				return m, m.openInEditorCmd(path, func(ctx context.Context) ([]byte, error) {
					return svc.ResolveBytes(ctx, ref)
				})
			},
		}, true
	}
	return actionRow{
		id:    "open-external",
		label: i18n.T("Open in external editor"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.openInEditorCmd(path, func(ctx context.Context) ([]byte, error) {
				return svc.ShowFile(ctx, hash, path)
			})
		},
	}, true
}

// commitsTouchingFileRow seeds the Commits feed with a path filter for the
// files-view selected file, closes the files view, and focuses Commits. Unlike
// viewFileRow/openExternalRow, deleted (D) rows are intentionally included —
// the path has history (including the deletion commit) which is exactly what
// the user wants to browse.
func (m Model) commitsTouchingFileRow() (actionRow, bool) {
	l, ok := m.filesViewSelectedLine()
	if !ok {
		return actionRow{}, false
	}
	filePath := l.path
	return actionRow{
		id:    "files-commits-touching",
		label: i18n.T("Commits touching this"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			m = m.closeFilesView()
			m.commitFilter = commitFilterFields{Paths: []string{filePath}}
			m = m.focusCommitsPanel()
			return m.startFeedReload()
		},
	}, true
}

// openPreview opens the right-column content preview for path at hash and
// focuses it so the cursor scrolls the content immediately. A files-view
// transition (paired with closePreview).
func (m Model) openPreview(hash, path string) (Model, tea.Cmd) {
	svc := m.svc
	return m.openPreviewSrc(path+"@"+hash, path, func(ctx context.Context) ([]byte, error) {
		return svc.ShowFile(ctx, hash, path)
	})
}

// openPreviewSrc is the source-agnostic preview open: tag gates stale results,
// load resolves the bytes off the UI thread. Backs both the commit preview
// (ShowFile) and the shelf-member preview (ResolveBytes).
func (m Model) openPreviewSrc(tag, path string, load func(context.Context) ([]byte, error)) (Model, tea.Cmd) {
	m.filesPreview = &contentPopup{title: path, lines: []contentLine{{text: i18n.T("(loading…)")}}}
	m.filesPreviewTag = tag
	m.filesTreeFocused = false // land in the preview to scroll
	return m, loadFileContentSrcCmd(tag, path, m.cfg.UI.SyntaxOn(), load)
}

// fileContentMsg carries a previewed file's content lines, tagged so a stale load
// (the user opened another file) is dropped.
type fileContentMsg struct {
	tag   string
	lines []contentLine
	err   error
}

// loadFileContentSrcCmd resolves a preview's bytes via load and splits them
// into content lines off the UI thread — lexing them there too (chroma costs
// ~1 s at domain.MaxSyntaxBytes, which must never land on the UI thread).
// path picks the lexer; syntaxOn is the [ui] diff_syntax switch, read at
// dispatch time so a config reload mid-load cannot flip it under the closure.
func loadFileContentSrcCmd(tag, path string, syntaxOn bool, load func(context.Context) ([]byte, error)) tea.Cmd {
	return func() tea.Msg {
		data, err := load(context.Background())
		if err != nil {
			return fileContentMsg{tag: tag, err: err}
		}
		if len(data) > domain.MaxDiffBytes {
			return fileContentMsg{tag: tag, lines: []contentLine{{text: i18n.T("(file too large to preview)")}}}
		}
		return fileContentMsg{tag: tag, lines: fileContentLinesTok(data, lexPreview(path, data, syntaxOn))}
	}
}

// lexPreview lexes a previewed file, or returns nil (plain rendering) when the
// switch is off, the path has no lexer, the file is past domain.MaxSyntaxBytes,
// or it holds a BARE \r — fileContentLines turns a lone \r into a line break
// while syntax.Lex keeps it inside its line, so every line after it would
// receive the previous line's runs. CRLF is safe: both sides count one line.
func lexPreview(path string, data []byte, syntaxOn bool) [][]syntax.Tok {
	if !syntaxOn || len(data) > domain.MaxSyntaxBytes || hasBareCR(data) {
		return nil
	}
	lang := syntax.Detect(path)
	if lang == "" {
		return nil
	}
	return syntax.Lex(lang, data)
}

// fileContentLines splits raw file bytes into one contentLine per line, expanding
// tabs so width math and alignment behave.
func fileContentLines(data []byte) []contentLine {
	return fileContentLinesTok(data, nil)
}

// fileContentLinesTok is fileContentLines with an optional class mask: tok is
// syntax.Lex's output over the SAME bytes (index = source line−1), and each
// line's mask is built by the very normalization that produces its text, so
// the two can never drift. tok nil is the plain path, byte-identical to what
// fileContentLines produced before the mask existed
// (TestFileContentLinesPlainPathUnchanged pins it).
func fileContentLinesTok(data []byte, tok [][]syntax.Tok) []contentLine {
	s := strings.TrimRight(string(data), "\n")
	if s == "" {
		return []contentLine{{text: i18n.T("(empty file)")}}
	}
	// Line-break normalization BEFORE the C0 sweep: CRLF collapses to one
	// break, a lone \r becomes one (web-pasted commit bodies separate their
	// paragraphs with bare \r\r — dropping the \r would glue the words
	// together). sanitizeForDisplay then expands tabs and strips whatever
	// controls remain, so a \r can never reach the terminal, where it jumps
	// to column 0 and overwrites the popup's own border (invisible to width
	// math — the error_popup.go story).
	s = normalizeLineBreaks(s)
	parts := strings.Split(sanitizeForDisplay(s), "\n")
	out := make([]contentLine, len(parts))
	for i, ln := range parts {
		out[i] = contentLine{text: ln}
	}
	if tok == nil {
		return out
	}
	// The mask is derived from the RAW line (pre-sanitize): token offsets are
	// rune indices into it, and only the raw line knows which runes the sweep
	// expanded (a tab) or dropped (a control).
	raw := strings.Split(s, "\n")
	for i := range out {
		if i >= len(raw) {
			break
		}
		out[i].cls = displayCls(raw[i], tokAt(tok, i+1))
	}
	return out
}

// displayCls is sanitizeForDisplay's per-rune map expressed as a class mask:
// one entry per rune it KEEPS, four for a tab (its fixed 4-space expansion),
// none for a dropped control. The result therefore has exactly one class per
// display rune of sanitizeForDisplay(raw).
func displayCls(raw string, toks []syntax.Tok) []syntax.Class {
	runes := []rune(raw)
	classes := classMask(len(runes), toks)
	out := make([]syntax.Class, 0, len(runes))
	for i, r := range runes {
		switch {
		case r == '\t':
			out = append(out, classes[i], classes[i], classes[i], classes[i])
		case r < 0x20 || r == 0x7f:
			// dropped by sanitizeForDisplay: no display rune, no class
		default:
			out = append(out, classes[i])
		}
	}
	return out
}

// normalizeLineBreaks maps CRLF and bare-CR line endings to \n.
func normalizeLineBreaks(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
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

// filePreviewInnerW mirrors renderFilePreview's innerW (border 2 + horizontal
// padding 2), so the search's horizontal pan agrees with what is rendered.
func (m Model) filePreviewInnerW() int {
	n := m.layout().rightW - 4
	if n < 1 {
		n = 1
	}
	return n
}

// previewSearchLines is every preview line's display text. contentLine.text is
// already sanitized (fileContentLines drops control runes and expands tabs), so
// a hit's rune offsets index the class mask and the window slices directly.
func previewSearchLines(p *contentPopup) []searchLine {
	out := make([]searchLine, len(p.lines))
	for i, l := range p.lines {
		out[i] = searchLine{row: i, side: 0, text: l.text}
	}
	return out
}

// searchPos is where ] and [ measure from. The preview has no cursor, so before
// there is a current hit it is the top visible line — the first ] then finds the
// first hit on screen rather than jumping back to the top of the file.
func (p *contentPopup) searchPos() searchPos {
	if p.search.cur >= 0 && p.search.cur < len(p.search.hits) {
		h := p.search.hits[p.search.cur]
		return searchPos{row: h.row, side: h.side, col: h.start}
	}
	return searchPos{row: p.sel, side: 0, col: -1}
}

// snapHit scrolls the pager so the current hit is visible, moving as little as
// possible: a hit above the window becomes the top line, one below it becomes
// the last. There is no cursor to place — the current hit's own colouring is
// the marker. In scroll mode the columns are panned to as well.
func (p *contentPopup) snapHit(rowsCap, innerW int) {
	if p.search.cur < 0 || p.search.cur >= len(p.search.hits) {
		return
	}
	h := p.search.hits[p.search.cur]
	switch {
	case h.row < p.sel:
		p.sel = h.row
	case h.row >= p.sel+rowsCap:
		p.sel = h.row - rowsCap + 1
	}
	p.sel = previewClamp(p.sel, len(p.lines), rowsCap, p.mode)
	if p.mode == modeScroll && h.row < len(p.lines) {
		cs, ce := hitCols(p.lines[h.row].text, h)
		p.hscroll = panFor(p.hscroll, innerW, cs, ce)
	}
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
		wr[i] = winRow{text: l.text, cls: l.cls}
		if p.search.active() {
			if hs := p.search.hitsOn(start+i, 0); len(hs) > 0 {
				wr[i].emph = overlayHits(nil, 0, len([]rune(l.text)), hs)
			}
		}
	}

	title := i18n.T("View %s", p.title)
	if bd := p.search.badge(); bd != "" { // right-aligned on the title line
		avail := innerW - lipgloss.Width(bd) - 2
		if avail < 1 {
			avail = 1
		}
		title = padRight(truncate(title, avail), avail) + "  " + bd
	}
	lines := make([]string, 0, contentH)
	lines = append(lines, padRight(truncate(title, innerW), innerW))
	if len(vis) == 0 {
		lines = append(lines, padRight(truncate(i18n.T("  (empty)"), innerW), innerW))
	} else {
		win := renderWindow(wr, winOpts{w: innerW, h: rowsCap, mode: p.mode, anchor: 0, hscroll: p.hscroll})
		lines = append(lines, win...)
	}
	for len(lines) < contentH-1 {
		lines = append(lines, padRight("", innerW))
	}
	hint := i18n.T("%d/%d  [↑/↓] scroll  [ctrl+w] view  [/] find  [esc] close", start+1, len(vis))
	lines = append(lines, padRight(truncate(hint, innerW), innerW))

	style := st().bluredPanel
	if !m.filesTreeFocused {
		style = st().focusedPanel
	}
	return style.Render(strings.Join(lines, "\n"))
}
