package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/gigagit/gg/internal/model"
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
	// statusErrStyle makes a failure in the status bar stand out (white on red)
	// instead of reading like an ordinary hint.
	statusErrStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("1"))
)

// statusErrorPrefixes are the leading tokens the status-setting sites use when
// the message reports a failure (built from an error). Render-time styling keys
// off these so there is no severity flag to keep in sync across call sites; keep
// this list aligned with the error-setting sites in model.go and the popups.
var statusErrorPrefixes = []string{"error:", "files:", "commits:", "amend:", "interactive rebase:", "cannot create:"}

// statusIsError reports whether a status message reports a failure.
func statusIsError(msg string) bool {
	for _, p := range statusErrorPrefixes {
		if strings.HasPrefix(msg, p) {
			return true
		}
	}
	return false
}

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

// menuBackground is what renders behind the action menu overlay: the topmost
// content window if one is open (so the menu floats over the diff/history/blame
// or file tree), else the panel interface (which itself draws the file tree and
// stash list).
func (m Model) menuBackground() string {
	if s := m.stackTop(); s != nil {
		return s.render(m)
	}
	if m.diffView != nil {
		return m.renderDiffView()
	}
	return m.renderInterface()
}

// render draws the interface, compositing the worktree popup centered on top of
// it when one is open. The output never exceeds width×height.
func (m Model) render() string {
	if m.modal != nil {
		// Overlay the decision modal centered on the interface, like every other
		// popup — not standalone in the top-left corner.
		w, h := m.overlayDims()
		bg := clipToHeight(m.renderInterface(), h)
		return overlayCenter(bg, m.renderModal(), w, h)
	}
	// The action menu is a modal-like overlay: it draws on top of whatever
	// content window is open (file tree, diff, history, blame, stash), checked
	// before those surfaces' own early returns below.
	if m.actionMenu != nil {
		w, h := m.overlayDims()
		return overlayCenter(clipToHeight(m.menuBackground(), h), m.renderActionMenu(), w, h)
	}
	if s := m.stackTop(); s != nil {
		_, h := m.overlayDims()
		return clipToHeight(s.render(m), h)
	}
	// Routing invariant: the diff view comes immediately after the modal —
	// here and in Update's key and mouse arms.
	if m.diffView != nil {
		_, h := m.overlayDims()
		return clipToHeight(m.renderDiffView(), h)
	}
	_, h := m.overlayDims()
	bg := clipToHeight(m.renderInterface(), h)
	if m.popup == nil && m.commitPopup == nil && m.repoPopup == nil && m.settings == nil && m.branchPopup == nil && m.contentPopup == nil && m.pairPopup == nil && m.stashPopup == nil && m.stashAction == nil && m.conflictPopup == nil {
		if lines, x, y, ok := m.tooltip(); ok {
			w, h := m.overlayDims()
			bg = overlayAt(bg, strings.Join(lines, "\n"), x, y, w, h)
		}
	}
	if m.popup != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderWorktreePopup(), w, h)
	}
	if m.commitPopup != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderCommitPopup(), w, h)
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
	if m.conflictPopup != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderConflictPopup(), w, h)
	}
	if m.stashPopup != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderStashPopup(), w, h)
	}
	if m.stashAction != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderStashActionPopup(), w, h)
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

// popupTextWidth is the usable text width inside a modal frame: inner minus
// modalStyle's horizontal padding. lipgloss soft-wraps content at this width
// (not at inner), so popup body/header/hint lines must be laid out / truncated
// to it — otherwise long lines spill onto ugly continuation lines.
func popupTextWidth(inner int) int {
	if tw := inner - modalStyle.GetHorizontalPadding(); tw > 1 {
		return tw
	}
	return 1
}

// popupBox frames content in the modal style, truncating every line to the
// frame's text width so nothing wraps. Body lines built via renderWindow at
// popupTextWidth already fit (truncate is width-aware, so it is a no-op on them
// and never cuts their styling); plain header/hint/subtitle lines are clamped
// here. Use this instead of modalStyle.Width(inner).Render for popups.
func popupBox(inner int, content string) string {
	tw := popupTextWidth(inner)
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		lines[i] = truncate(l, tw)
	}
	return modalStyle.Width(inner).Render(strings.Join(lines, "\n")) + "\n"
}

// wrapParts greedily packs sep-joined parts into lines no wider than width, so a
// hint with more keys than fit on one line wraps onto a few instead of being
// truncated. A single part wider than width still gets its own line.
func wrapParts(parts []string, width int, sep string) []string {
	if len(parts) == 0 {
		return nil
	}
	var lines []string
	cur := parts[0]
	for _, p := range parts[1:] {
		if lipgloss.Width(cur)+lipgloss.Width(sep)+lipgloss.Width(p) <= width {
			cur += sep + p
		} else {
			lines = append(lines, cur)
			cur = p
		}
	}
	return append(lines, cur)
}

// renderInterface draws the header, the panels, and the footer/status, sized to
// fit the current terminal so the output never exceeds width×height.
func (m Model) renderInterface() string {
	g := m.layout()

	header := m.headerLine(g.w)
	footer := truncate(m.footerLine(), g.w)
	errMode := statusIsError(m.statusMsg)
	var notice string
	if n := len(m.status.Conflicts()); n > 0 {
		notice = fmt.Sprintf("⚠ %d conflict", n)
		if n != 1 {
			notice += "s"
		}
		if src := m.conflict.Describe(); src != "" {
			notice += " " + src
		}
		notice += " — press [x] to resolve"
	}
	var markHint string
	if m.mark != nil && m.markAlive() {
		markHint = "◆ marked: " + m.mark.display
	}
	// Assemble the segments. In error mode the message LEADS so truncation can
	// never hide it behind the persistent conflict/mark hints; otherwise the
	// hints lead and the transient message trails.
	var parts []string
	add := func(s string) {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if errMode {
		add(m.statusMsg)
		add(notice)
		add(markHint)
	} else {
		add(markHint)
		add(notice)
		add(m.statusMsg)
	}
	statusLine := strings.Join(parts, " · ")
	if m.running {
		statusLine = "⏳ " + statusLine
	}
	statusLine = truncate(oneLine(statusLine), g.w)
	// Style after truncation: truncate slices runes and would corrupt ANSI codes.
	if errMode {
		statusLine = statusErrStyle.Render(statusLine)
	}

	// Narrow terminals: a single commits column (two columns won't fit cleanly).
	if g.w < 40 {
		cmRows, _ := m.panelView(panelCommits)
		body := m.renderPanel(panelCommits, m.panelLabel(panelCommits, "Commits"), cmRows, g.w, g.boxH[panelCommits])
		return strings.Join([]string{header, body, footer, statusLine}, "\n")
	}

	cmRows, _ := m.panelView(panelCommits)

	var left string
	if m.filesView != nil {
		left = m.renderFilesView(g.leftW, g.bodyH)
	} else {
		// The Branches/Worktrees tab slot (active tab shows its content; the tab
		// bar lives in the label line) over Files, then Staged (when it fits).
		active := m.activeLeftTab
		atRows, _ := m.panelView(active)
		fRows, _ := m.panelView(panelFiles)
		boxes := []string{
			m.renderPanel(active, m.panelLabel(active, tabBarLabel(active)), atRows, g.leftW, g.boxH[active]),
			m.renderPanel(panelFiles, m.filesLabel(panelFiles, "Files"), fRows, g.leftW, g.boxH[panelFiles]),
		}
		if g.boxH[panelStaged] > 0 {
			sRows, _ := m.panelView(panelStaged)
			boxes = append(boxes, m.renderPanel(panelStaged, m.filesLabel(panelStaged, "Staged"), sRows, g.leftW, g.boxH[panelStaged]))
		}
		left = lipgloss.JoinVertical(lipgloss.Left, boxes...)
	}
	var right string
	if m.stashView != nil {
		right = m.renderStashList(g.rightW, g.boxH[panelCommits])
	} else {
		right = m.renderPanel(panelCommits, m.panelLabel(panelCommits, "Commits"), cmRows, g.rightW, g.boxH[panelCommits])
	}
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

// filesLabel decorates a Files/Staged panel label with its visible row count
// (the at-a-glance "how many staged" signal) plus the shared sort/filter marks.
func (m Model) filesLabel(p panel, base string) string {
	return m.panelLabel(p, base+" "+strconv.Itoa(m.panelLen(p)))
}

// tabBarLabel is the shared left-slot header: the active tab spelled out and
// bracketed, the inactive tabs shown as single-letter markers so all three fit
// the narrow left column (w/3) even at 80 cols, leaving room for the sort/filter
// decoration panelLabel appends. Plain ASCII so renderPanel's truncate stays safe.
func tabBarLabel(active panel) string {
	mark := func(p panel, full, short string) string {
		if p == active {
			return "[" + full + "]"
		}
		return short
	}
	return mark(panelBranches, "Branches", "B") + " " +
		mark(panelRemotes, "Remotes", "R") + " " +
		mark(panelWorktrees, "Worktrees", "W")
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
		marked := m.markedDisplayIndices(p)
		sel := m.sel[p]
		isFocused := m.panelFocused(p)
		wr := make([]winRow, len(rows))
		for i, row := range rows {
			prefix := "  "
			var st lipgloss.Style
			if marked[i] {
				prefix = "◆ "
			} else if i == sel && isFocused {
				prefix = "> "
			}
			if i == sel && isFocused {
				st = selectedRow
			}
			wr[i] = winRow{text: prefix + row, style: st}
		}
		body := renderWindow(wr, winOpts{w: innerW, h: rowsCap, mode: m.dispModes[p], anchor: sel, hscroll: m.hscroll[p]})
		lines = append(lines, body...)
	}
	for len(lines) < contentH {
		lines = append(lines, padRight("", innerW))
	}

	style := bluredPanel
	if m.panelFocused(p) {
		style = focusedPanel
	}
	return style.Render(strings.Join(lines, "\n"))
}

// renderListBox draws a bordered boxW×boxH list that is not backed by a panel
// (used by the stash window). focused selects the border + highlight styles.
func (m Model) renderListBox(label string, rows []string, sel, boxW, boxH int, focused bool, mode dispMode, hscroll int) string {
	contentH := boxH - 2
	if contentH < 1 {
		contentH = 1
	}
	innerW := boxW - 4
	if innerW < 1 {
		innerW = 1
	}
	rowsCap := contentH - 1
	if rowsCap < 0 {
		rowsCap = 0
	}
	lines := []string{padRight(truncate(label, innerW), innerW)}
	if rowsCap >= 1 && len(rows) > 0 {
		wr := make([]winRow, len(rows))
		for i, row := range rows {
			prefix := "  "
			var st lipgloss.Style
			if i == sel && focused {
				prefix = "> "
				st = selectedRow
			}
			wr[i] = winRow{text: prefix + row, style: st}
		}
		body := renderWindow(wr, winOpts{w: innerW, h: rowsCap, mode: mode, anchor: sel, hscroll: hscroll})
		lines = append(lines, body...)
	}
	for len(lines) < contentH {
		lines = append(lines, padRight("", innerW))
	}
	style := bluredPanel
	if focused {
		style = focusedPanel
	}
	return style.Render(strings.Join(lines, "\n"))
}

// panelFocused reports whether p should render as the focused panel. While
// the files view's tree side is focused, the Commits panel renders blurred
// even though m.focus still points at it (focus stays there so selection
// and follow-live machinery keep working).
func (m Model) panelFocused(p panel) bool {
	return p == m.focus && !(m.filesView != nil && m.filesTreeFocused)
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
	start := windowStart(len(rows), n, sel)
	return rows[start : start+n], sel - start, start
}

// windowStart is the scroll offset windowRows applies: the first display row
// shown when total rows are windowed to n around sel. Shared with the mouse
// hit-test (panelRowAt) so a click can never select a different row than the
// one rendered on that screen line.
func windowStart(total, n, sel int) int {
	if n <= 0 {
		n = 1
	}
	if total <= n {
		return 0
	}
	start := sel - n/2
	if start < 0 {
		start = 0
	}
	if start+n > total {
		start = total - n
	}
	if start < 0 {
		start = 0
	}
	return start
}

// oneLine collapses every whitespace run (including the newlines and tabs that
// git writes to stderr) into a single space, so a multi-line error renders
// legibly on the one-line status bar instead of breaking the layout or being
// cut off at the first newline.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
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
		if b.Behind > 0 {
			row += " (↓" + strconv.Itoa(b.Behind) + ")"
		}
		out = append(out, row)
	}
	return out
}

// remoteRows builds the Remotes tab rows: one short ref per line.
func (m Model) remoteRows() []string {
	out := make([]string, 0, len(m.remoteBranches))
	for _, rb := range m.remoteBranches {
		out = append(out, "  "+rb.Name)
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

// statusRows builds panel p's file rows. Each file panel shows a SINGLE status
// letter for its OWN side, not git's two-byte XY — once the panels are split,
// the other byte is noise. The Files panel shows the working-tree state (a new
// untracked file → A, a conflict → U, otherwise the unstaged byte M/D); the
// Staged panel shows the index state (the staged byte A/M/D/R/C/T).
func (m Model) statusRows(p panel) []string {
	out := make([]string, 0, len(m.status.Files))
	for _, f := range m.status.Files {
		out = append(out, fmt.Sprintf("%c %s", fileGlyph(p, f), f.Path))
	}
	return out
}

// fileGlyph returns the single status letter for file f in panel p.
func fileGlyph(p panel, f model.FileStatus) byte {
	if p == panelStaged {
		if f.Staged == 0 {
			return ' '
		}
		return f.Staged // index vs HEAD: A / M / D / R / C / T
	}
	// Files panel: working-tree state.
	switch {
	case f.Kind == model.KindUntracked:
		return 'A' // a new file present in the tree, not staged
	case f.Kind == model.KindUnmerged:
		return 'U' // conflicted — resolve with x
	case f.Unstaged == 0:
		return ' '
	default:
		return f.Unstaged // worktree vs index: M / D
	}
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
