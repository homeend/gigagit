package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// overlayAt composites fg on top of bg with fg's top-left corner at cell
// (left, top), replacing the cells fg covers while keeping the surrounding bg
// visible. Both are treated as a grid of termW×termH cells; negative
// coordinates clamp to 0 and rows outside the grid are dropped. ANSI styling
// in both layers is preserved (slicing is width-aware).
func overlayAt(bg, fg string, left, top, termW, termH int) string {
	bgLines := strings.Split(bg, "\n")
	for len(bgLines) < termH {
		bgLines = append(bgLines, "")
	}
	fgLines := strings.Split(fg, "\n")

	fgW := 0
	for _, l := range fgLines {
		if w := ansi.StringWidth(l); w > fgW {
			fgW = w
		}
	}
	if top < 0 {
		top = 0
	}
	if left < 0 {
		left = 0
	}

	for i, fl := range fgLines {
		row := top + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		bgLine := bgLines[row]
		// Left slice of the background, padded out to the overlay's left edge.
		leftPart := ansi.Truncate(bgLine, left, "")
		if w := ansi.StringWidth(leftPart); w < left {
			leftPart += strings.Repeat(" ", left-w)
		}
		// Pad the overlay line to a clean rectangle so its right edge is straight.
		if w := ansi.StringWidth(fl); w < fgW {
			fl += strings.Repeat(" ", fgW-w)
		}
		// Background to the right of the overlay (empty if the bg line is shorter).
		rightPart := ansi.TruncateLeft(bgLine, left+fgW, "")
		bgLines[row] = leftPart + fl + rightPart
	}
	return strings.Join(bgLines, "\n")
}

// overlayCenter composites fg centered on top of bg (see overlayAt).
func overlayCenter(bg, fg string, termW, termH int) string {
	fgLines := strings.Split(fg, "\n")
	fgW := 0
	for _, l := range fgLines {
		if w := ansi.StringWidth(l); w > fgW {
			fgW = w
		}
	}
	return overlayAt(bg, fg, (termW-fgW)/2, (termH-len(fgLines))/2, termW, termH)
}

var (
	titleStyle   = lipgloss.NewStyle().Bold(true)
	focusedPanel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("12")).Padding(0, 1)
	bluredPanel  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	selectedRow  = lipgloss.NewStyle().Reverse(true)
	modalStyle   = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("11")).Padding(1, 2)
)

// clipToHeight truncates s to at most h lines (split on "\n"), joining back
// without a trailing newline. This guards against layout() bodyH floors that
// add extra lines at very small terminal heights.
func clipToHeight(s string, h int) string {
	if h <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= h {
		return s
	}
	return strings.Join(lines[:h], "\n")
}

// render draws the interface, compositing the worktree popup centered on top of
// it when one is open. The output never exceeds width×height.
func (m Model) render() string {
	if m.modal != nil {
		return m.renderModal()
	}
	_, h := m.overlayDims()
	bg := clipToHeight(m.renderInterface(), h)
	if m.popup == nil && m.repoPopup == nil && m.settings == nil && m.branchPopup == nil && m.contentPopup == nil && m.pairPopup == nil {
		if lines, x, y, ok := m.tooltip(); ok {
			w, h := m.overlayDims()
			bg = overlayAt(bg, strings.Join(lines, "\n"), x, y, w, h)
		}
	}
	if m.popup != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderWorktreePopup(), w, h)
	}
	if m.repoPopup != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderRepoPopup(), w, h)
	}
	if m.settings != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderSettingsPopup(), w, h)
	}
	if m.branchPopup != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderBranchPopup(), w, h)
	}
	if m.contentPopup != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderContentPopup(), w, h)
	}
	if m.pairPopup != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderPairOpPopup(), w, h)
	}
	return bg
}

// overlayDims returns the terminal size for popup compositing, defaulting to
// 80x24 before the first WindowSizeMsg arrives.
func (m Model) overlayDims() (int, int) {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	return w, h
}

// popupInnerWidth is the standard popup content width: 56 columns, capped to
// the terminal minus borders/margins, floored so the box never collapses.
func popupInnerWidth(w int) int {
	inner := 56
	if max := w - 8; inner > max {
		inner = max
	}
	if inner < 20 {
		inner = 20
	}
	return inner
}

// footerText abbreviates the global keys; TestHelpFooterCoverage enforces
// that every [x] key here has a row in helpContent.
const footerText = "[p]ull [P]ush [s]witch [b]ranch [S]tash [u]ndo [w]orktree [m]ark [d]elete [o]rder [/]filter [R]epo [,] settings  •  [tab] focus  [r] reload  [?] help  [q] quit"

// renderInterface draws the header, the panels, and the footer/status, sized to
// fit the current terminal so the output never exceeds width×height.
func (m Model) renderInterface() string {
	g := m.layout()

	header := m.headerLine(g.w)
	footer := truncate(footerText, g.w)
	statusLine := m.statusMsg
	if m.mark != nil && m.markAlive() {
		hint := "◆ marked: " + m.mark.display
		if statusLine != "" {
			statusLine = hint + " · " + statusLine
		} else {
			statusLine = hint
		}
	}
	if m.running {
		statusLine = "⏳ " + statusLine
	}
	statusLine = truncate(statusLine, g.w)

	// Narrow terminals: a single commits column (two columns won't fit cleanly).
	if g.w < 40 {
		cmRows, _ := m.panelView(panelCommits)
		body := m.renderPanel(panelCommits, m.panelLabel(panelCommits, "Commits"), cmRows, g.w, g.boxH[panelCommits])
		return strings.Join([]string{header, body, footer, statusLine}, "\n")
	}

	brRows, _ := m.panelView(panelBranches)
	wtRows, _ := m.panelView(panelWorktrees)
	stRows, _ := m.panelView(panelStatus)
	cmRows, _ := m.panelView(panelCommits)

	var left string
	if g.boxH[panelWorktrees] > 0 {
		left = lipgloss.JoinVertical(lipgloss.Left,
			m.renderPanel(panelBranches, m.panelLabel(panelBranches, "Branches"), brRows, g.leftW, g.boxH[panelBranches]),
			m.renderPanel(panelWorktrees, m.panelLabel(panelWorktrees, "Worktrees"), wtRows, g.leftW, g.boxH[panelWorktrees]),
			m.renderPanel(panelStatus, m.panelLabel(panelStatus, "Status"), stRows, g.leftW, g.boxH[panelStatus]),
		)
	} else {
		left = lipgloss.JoinVertical(lipgloss.Left,
			m.renderPanel(panelBranches, m.panelLabel(panelBranches, "Branches"), brRows, g.leftW, g.boxH[panelBranches]),
			m.renderPanel(panelStatus, m.panelLabel(panelStatus, "Status"), stRows, g.leftW, g.boxH[panelStatus]),
		)
	}
	right := m.renderPanel(panelCommits, m.panelLabel(panelCommits, "Commits"), cmRows, g.rightW, g.boxH[panelCommits])
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return strings.Join([]string{header, body, footer, statusLine}, "\n")
}

// headerLine renders the bold title plus branch info, truncated to width.
func (m Model) headerLine(w int) string {
	rest := "  branch " + m.status.Branch
	if m.status.Upstream != "" {
		rest += fmt.Sprintf(" (↑%d ↓%d)", m.status.Ahead, m.status.Behind)
	}
	return titleStyle.Render("gigagit") + truncate(rest, w-7)
}

// renderPanel draws one bordered panel of fixed size boxW×boxH, windowing rows
// around the selection and truncating each to fit. Border (2) + padding (2) are
// accounted for so the rendered box matches the requested dimensions.
func (m Model) renderPanel(p panel, label string, rows []string, boxW, boxH int) string {
	contentH := boxH - 2 // top/bottom border
	if contentH < 1 {
		contentH = 1
	}
	innerW := boxW - 4 // border (2) + horizontal padding (2)
	if innerW < 1 {
		innerW = 1
	}
	rowsCap := contentH - 1 // one line reserved for the label
	if rowsCap < 0 {
		rowsCap = 0
	}

	lines := make([]string, 0, contentH)
	lines = append(lines, padRight(truncate(label, innerW), innerW))

	if rowsCap < 1 {
		// No room for any data rows below the label; render the label only so the
		// panel never exceeds boxH (windowRows would otherwise force one row).
	} else if len(rows) == 0 {
		lines = append(lines, padRight(truncate("  (none)", innerW), innerW))
	} else {
		win, selInWin, start := windowRows(rows, rowsCap, m.sel[p])
		markedInWin := -1
		if md := m.markDisplayIndex(p); md >= 0 {
			markedInWin = md - start
		}
		for i, row := range win {
			focused := i == selInWin && p == m.focus
			prefix := "  "
			if i == markedInWin {
				prefix = "◆ "
			} else if focused {
				prefix = "> "
			}
			line := padRight(truncate(prefix+row, innerW), innerW)
			if focused {
				line = selectedRow.Render(line)
			}
			lines = append(lines, line)
		}
	}
	for len(lines) < contentH {
		lines = append(lines, padRight("", innerW))
	}

	style := bluredPanel
	if p == m.focus {
		style = focusedPanel
	}
	return style.Render(strings.Join(lines, "\n"))
}

// windowRows returns at most n rows scrolled so sel stays visible, sel's
// index within the returned window, and the window's start offset.
func windowRows(rows []string, n, sel int) ([]string, int, int) {
	if n <= 0 {
		n = 1
	}
	if len(rows) <= n {
		return rows, sel, 0
	}
	start := sel - n/2
	if start < 0 {
		start = 0
	}
	if start+n > len(rows) {
		start = len(rows) - n
	}
	if start < 0 {
		start = 0
	}
	return rows[start : start+n], sel - start, start
}

// truncate shortens s to at most n display columns, adding an ellipsis. Width is
// measured in display columns (lipgloss.Width), not runes, so wide glyphs like
// the ⏳ spinner cannot push a line one column past the terminal edge.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	// Drop trailing runes until the remainder plus the 1-column ellipsis fits.
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > n {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// padRight right-pads s with spaces to n display columns (no-op if already wider).
func padRight(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// worktreeBranchSet returns the set of branch names checked out in a worktree.
func (m Model) worktreeBranchSet() map[string]bool {
	set := make(map[string]bool, len(m.worktrees))
	for _, w := range m.worktrees {
		if w.Branch != "" {
			set[w.Branch] = true
		}
	}
	return set
}

func (m Model) branchRows() []string {
	hasWt := m.worktreeBranchSet()
	out := make([]string, 0, len(m.branches))
	for _, b := range m.branches {
		marker := "  "
		if b.IsHead {
			marker = "* "
		}
		row := marker + b.Name
		if hasWt[b.Name] {
			row += " ◫"
		}
		out = append(out, row)
	}
	return out
}

func (m Model) worktreeRows() []string {
	out := make([]string, 0, len(m.worktrees))
	for _, w := range m.worktrees {
		marker := "  "
		if w.Path == m.currentWorktree {
			marker = "* "
		}
		branch := w.Branch
		if branch == "" {
			branch = "(detached)"
		}
		out = append(out, marker+branch+"  "+w.Path)
	}
	return out
}

func (m Model) statusRows() []string {
	out := make([]string, 0, len(m.status.Files))
	for _, f := range m.status.Files {
		x := f.Staged
		y := f.Unstaged
		if x == 0 {
			x = ' '
		}
		if y == 0 {
			y = ' '
		}
		out = append(out, fmt.Sprintf("%c%c %s", x, y, f.Path))
	}
	return out
}

func (m Model) commitRows() []string {
	out := make([]string, 0, len(m.commits))
	for _, c := range m.commits {
		h := c.Hash
		if len(h) > 7 {
			h = h[:7]
		}
		out = append(out, h+" "+c.Subject)
	}
	return out
}

func (m Model) renderModal() string {
	var b strings.Builder
	b.WriteString(m.modal.req.Prompt)
	b.WriteString("\n\n")
	for i, opt := range m.modal.req.Options {
		if i == m.modal.sel {
			b.WriteString(selectedRow.Render("> " + opt))
		} else {
			b.WriteString("  " + opt)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n[↑/↓] choose  [enter] confirm  [esc] abort")
	return modalStyle.Render(b.String()) + "\n"
}
