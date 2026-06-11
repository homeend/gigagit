package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true)
	focusedPanel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("12")).Padding(0, 1)
	bluredPanel  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	selectedRow  = lipgloss.NewStyle().Reverse(true)
	modalStyle   = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("11")).Padding(1, 2)
)

// render draws the header, the three panels, and the footer/status, sized to fit
// the current terminal so the output never exceeds width×height.
func (m Model) render() string {
	if m.modal != nil {
		return m.renderModal()
	}

	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	header := m.headerLine(w)
	footer := truncate("[p]ull [P]ush [s]witch [S]tash [u]ndo  •  [tab] focus  [r] reload  [q] quit", w)
	statusLine := m.statusMsg
	if m.running {
		statusLine = "⏳ " + statusLine
	}
	statusLine = truncate(statusLine, w)

	// Rows available for the panel body, between header and footer/status.
	bodyH := h - 3
	if bodyH < 6 {
		bodyH = 6
	}

	// Narrow terminals: a single commits column (two columns won't fit cleanly).
	if w < 40 {
		body := m.renderPanel(panelCommits, "Commits", m.commitRows(), w, bodyH)
		return strings.Join([]string{header, body, footer, statusLine}, "\n")
	}

	// Two columns: a narrow left (branches over status) and a wide commits panel.
	leftW := w / 3
	if leftW < 16 {
		leftW = 16
	}
	if leftW > w-24 {
		leftW = w - 24
	}
	rightW := w - leftW

	var left string
	if bodyH >= 9 {
		// Three stacked left panels: Branches, Worktrees, Status. Each bordered
		// panel needs >=3 rows, so this layout requires bodyH >= 9.
		h1 := bodyH / 3
		h2 := bodyH / 3
		h3 := bodyH - h1 - h2
		left = lipgloss.JoinVertical(lipgloss.Left,
			m.renderPanel(panelBranches, "Branches", m.branchRows(), leftW, h1),
			m.renderPanel(panelWorktrees, "Worktrees", m.worktreeRows(), leftW, h2),
			m.renderPanel(panelStatus, "Status", m.statusRows(), leftW, h3),
		)
	} else {
		// Short terminal: fall back to two left panels (Branches over Status).
		branchesH := bodyH / 2
		statusH := bodyH - branchesH
		left = lipgloss.JoinVertical(lipgloss.Left,
			m.renderPanel(panelBranches, "Branches", m.branchRows(), leftW, branchesH),
			m.renderPanel(panelStatus, "Status", m.statusRows(), leftW, statusH),
		)
	}
	right := m.renderPanel(panelCommits, "Commits", m.commitRows(), rightW, bodyH)
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
		win, selInWin := windowRows(rows, rowsCap, m.sel[p])
		for i, row := range win {
			focused := i == selInWin && p == m.focus
			prefix := "  "
			if focused {
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

// windowRows returns at most n rows scrolled so sel stays visible, plus sel's
// index within the returned window.
func windowRows(rows []string, n, sel int) ([]string, int) {
	if n <= 0 {
		n = 1
	}
	if len(rows) <= n {
		return rows, sel
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
	return rows[start : start+n], sel - start
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
