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

// render draws the three panels and a footer.
func (m Model) render() string {
	if m.modal != nil {
		return m.renderModal()
	}
	header := titleStyle.Render("gigagit") + "  branch " + m.status.Branch
	if m.status.Upstream != "" {
		header += fmt.Sprintf(" (↑%d ↓%d)", m.status.Ahead, m.status.Behind)
	}

	branches := m.renderList(panelBranches, "Branches", m.branchRows())
	status := m.renderList(panelStatus, "Status", m.statusRows())
	commits := m.renderList(panelCommits, "Commits", m.commitRows())

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.JoinVertical(lipgloss.Left, branches, status),
		commits,
	)
	footer := "[tab] focus  [↑/↓ or k/j] move  [r] reload  [q] quit"
	return strings.Join([]string{header, body, footer}, "\n") + "\n"
}

func (m Model) renderList(p panel, label string, rows []string) string {
	var b strings.Builder
	b.WriteString(label)
	b.WriteString("\n")
	if len(rows) == 0 {
		b.WriteString("  (none)")
	}
	for i, row := range rows {
		if i == m.sel[p] && p == m.focus {
			b.WriteString(selectedRow.Render("> " + row))
		} else {
			b.WriteString("  " + row)
		}
		if i < len(rows)-1 {
			b.WriteString("\n")
		}
	}
	style := bluredPanel
	if p == m.focus {
		style = focusedPanel
	}
	return style.Render(b.String())
}

func (m Model) branchRows() []string {
	out := make([]string, 0, len(m.branches))
	for _, b := range m.branches {
		marker := "  "
		if b.IsHead {
			marker = "* "
		}
		out = append(out, marker+b.Name)
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
