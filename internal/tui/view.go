package tui

import (
	"fmt"
	"slices"
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

// friendlyOpError turns an operation failure into the status-bar message. gg
// runs git with GIT_TERMINAL_PROMPT=0 (so a credential prompt can never freeze
// the TUI), which makes git fail with "could not read Username … terminal
// prompts disabled" when a remote needs auth and no helper is configured. That
// raw text reads like a gg bug, so it is rewritten into actionable guidance.
// Everything else passes through as the cleaned git message.
func friendlyOpError(err error) string {
	s := err.Error()
	low := strings.ToLower(s)
	if strings.Contains(low, "terminal prompts disabled") ||
		strings.Contains(low, "could not read username") ||
		strings.Contains(low, "could not read password") {
		return "error: remote needs credentials — configure a git credential helper (gg cannot prompt for them)"
	}
	return "error: " + s
}

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

// layerBase is what the layer stack composites over: the open full-screen diff,
// else the panel interface. Surfaces in the stack ignore it (they own the
// screen); popups composite their centered box onto it.
func (m Model) layerBase() string {
	if m.diffLayer() != nil {
		return m.renderDiffView()
	}
	return m.renderInterface()
}

// renderLayers draws the layer stack over layerBase. Only the TOP layer is drawn
// over a backdrop built from the full-screen surfaces beneath it — non-top
// centered popups are not composited (a child popup replaces its parent visually,
// exactly as before the overlay/surface stacks merged; the parent stays live on
// the stack so esc returns to it). A surface backdrop shows through a popup's
// centered box; surfaces ignore the backdrop and own the screen. An empty stack
// returns layerBase unchanged. This is also the background the action menu floats
// over (it replaces the old menuBackground helper).
func (m Model) renderLayers() string {
	below := m.layerBase()
	if m.layers == nil || len(m.layers.entries) == 0 {
		return below
	}
	top := len(m.layers.entries) - 1
	// Surfaces are always below popups (no surface is ever pushed over a live
	// popup), so folding the full-screen surfaces beneath the top yields the top
	// surface as the popup's backdrop — or, when the top layer is itself a
	// surface, the loop is all-surface and the top surface renders full-screen.
	for i := 0; i < top; i++ {
		if l := m.layers.entries[i]; isFullScreenLayer(l) {
			below = l.render(m, below)
		}
	}
	return m.layers.entries[top].render(m, below)
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
	// A process owns the screen: it draws its current window over the panel
	// interface (a centered list) or replaces it (a full-screen editor). Sits
	// just below the modal, above every other window.
	if m.proc != nil {
		_, h := m.overlayDims()
		return clipToHeight(m.proc.render(m, clipToHeight(m.renderInterface(), h)), h)
	}
	// The action menu is a modal-like overlay: it draws on top of whatever
	// content window is open (file tree, diff, history, blame, stash), checked
	// before those surfaces' own early returns below.
	if m.actionMenu != nil {
		w, h := m.overlayDims()
		return overlayCenter(clipToHeight(m.renderLayers(), h), m.renderActionMenu(), w, h)
	}
	// The layer stack (full-screen surfaces + centered popups) and the diff view
	// it composites over. renderLayers walks bottom→top: surfaces own the screen,
	// popups composite their box on top. The bookmark/shelf switchers, their child
	// popups, and the help / `?` cheat-sheet viewer all live here, so esc on the
	// cheat-sheet returns to the switcher beneath. With an empty stack but an open
	// diff, renderLayers is just the diff; with neither, fall through to the
	// panel/tooltip base below.
	if (m.layers != nil && len(m.layers.entries) > 0) || m.diffLayer() != nil {
		_, h := m.overlayDims()
		return m.withDiffFileNotice(m.withRecall(clipToHeight(m.renderLayers(), h)))
	}
	_, h := m.overlayDims()
	bg := clipToHeight(m.renderInterface(), h)
	if lines, x, y, ok := m.tooltip(); ok {
		w, h := m.overlayDims()
		bg = overlayAt(bg, strings.Join(lines, "\n"), x, y, w, h)
	}
	return m.withRecall(bg)
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

// popupWideInnerWidth is the content width for the path-list switcher popups
// (bookmark/shelf). Their rows are file addresses whose path tail — the part
// the user cares about — sits at the end, so they scale wider than the standard
// prose popup (up to 96 columns), capped to the terminal and floored like
// popupInnerWidth. z still handles paths longer than even this.
func popupWideInnerWidth(w int) int {
	inner := 96
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
	// Suppressed while the conflict process is active — it draws its own window
	// over this background, so a "press [x] to resolve" notice would be wrong.
	if n := len(m.status.Conflicts()); n > 0 && m.proc == nil {
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
		add(m.commitBranchHint())
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
		cmRows, _, decos := m.commitBody(g.boxH[panelCommits])
		body := m.renderPanel(panelCommits, m.panelLabel(panelCommits, "Commits ("+m.commitScopeLabel()+")"), cmRows, decos, g.w, g.boxH[panelCommits])
		return strings.Join([]string{header, body, footer, statusLine}, "\n")
	}

	cmRows, _, cmDecos := m.commitBody(g.boxH[panelCommits])

	var left string
	if m.filesView != nil {
		left = m.renderFilesView(g.leftW, g.bodyH)
	} else {
		// The left column: the Branches/Remotes/Worktrees tab slot, the Files/Tags
		// slot, and Staged. Render the logical left-panel set, skipping any the
		// layout hides (boxH<=0) — the inactive Staged on a short terminal, or the
		// non-pinned panels while one is maximized. A maximized panel renders at the
		// full body height because layout gave it boxH==bodyH.
		var boxes []string
		for _, p := range m.leftColumnPanels() {
			if g.boxH[p] <= 0 {
				continue
			}
			rows, _ := m.panelView(p)
			boxes = append(boxes, m.renderPanel(p, m.leftPanelLabel(p), rows, nil, g.leftW, g.boxH[p]))
		}
		left = lipgloss.JoinVertical(lipgloss.Left, boxes...)
	}
	var right string
	switch {
	case m.filesPreview != nil:
		right = m.renderFilePreview(g.rightW, g.boxH[panelCommits])
	case m.stashView != nil:
		right = m.renderStashList(g.rightW, g.boxH[panelCommits])
	default:
		right = m.renderPanel(panelCommits, m.panelLabel(panelCommits, "Commits ("+m.commitScopeLabel()+")"), cmRows, cmDecos, g.rightW, g.boxH[panelCommits])
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

// leftPanelLabel is the header for a left-column panel, dispatched by which slot
// p occupies: the top tab slot shows the tab bar, the middle slot the files tab
// bar, Staged its count. It single-sources the labels the render loop draws so
// each box keeps the exact title it had before the loop existed.
func (m Model) leftPanelLabel(p panel) string {
	switch p {
	case panelStaged, panelReflog:
		return m.panelLabel(p, bottomTabLabel(p, m.panelLen(panelStaged), m.panelLen(panelReflog)))
	case panelFiles, panelTags:
		return m.panelLabel(p, filesTabLabel(p, m.panelLen(panelFiles), m.panelLen(panelTags)))
	default: // the Branches/Remotes/Worktrees tab slot
		return m.panelLabel(p, tabBarLabel(p))
	}
}

// bottomTabLabel is the bottom-left slot header: the active tab spelled out with
// its row count, the inactive tab shown plainly. Mirrors filesTabLabel.
func bottomTabLabel(active panel, stagedN, reflogN int) string {
	staged := fmt.Sprintf("Staged %d", stagedN)
	reflog := fmt.Sprintf("Reflog %d", reflogN)
	if active == panelReflog {
		return staged + " [" + reflog + "]"
	}
	return "[" + staged + "] " + reflog
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

// filesTabLabel is the middle-slot header: the active tab spelled out with its
// row count, the inactive tab shown plainly. Mirrors tabBarLabel for the top slot.
func filesTabLabel(active panel, filesN, tagsN int) string {
	files := fmt.Sprintf("Files %d", filesN)
	tags := fmt.Sprintf("Tags %d", tagsN)
	if active == panelTags {
		return files + " [" + tags + "]"
	}
	return "[" + files + "] " + tags
}

// renderPanel draws one bordered panel of fixed size boxW×boxH, windowing rows
// around the selection and truncating each to fit. Border (2) + padding (2) are
// accounted for so the rendered box matches the requested dimensions.
func (m Model) renderPanel(p panel, label string, rows []string, decos []rowDecorator, boxW, boxH int) string {
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
		cmpSet := m.compareSetDisplayIndices(p)
		sel := m.sel[p]
		isFocused := m.panelFocused(p)
		wr := make([]winRow, len(rows))
		for i, row := range rows {
			prefix := "  "
			var st lipgloss.Style
			if cmpSet[i] {
				prefix = "◉ "
			} else if marked[i] {
				prefix = "◆ "
			} else if i == sel && isFocused {
				prefix = "> "
			}
			if i == sel && isFocused {
				st = selectedRow
			}
			var deco rowDecorator
			if i != sel || !isFocused {
				if i < len(decos) {
					deco = decos[i]
				}
			}
			wr[i] = winRow{text: prefix + row, style: st, decorate: deco}
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

// elideLeft shortens s to at most n display columns by dropping from the FRONT
// and prefixing a "…", so the meaningful tail stays visible. Used for directory
// paths, whose leaf (the part that distinguishes nested dirs) is at the end; a
// plain truncate would cut exactly that off and make sibling/child dirs read as
// duplicates.
func elideLeft(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	// Drop leading runes until the remainder plus the 1-column ellipsis fits.
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > n {
		r = r[1:]
	}
	return "…" + string(r)
}

// padRight right-pads s with spaces to n display columns (no-op if already wider).
func padRight(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// worktreePathOf returns the path of the worktree that has branch checked out,
// if any (git allows a branch in at most one worktree). Includes the current
// worktree.
func (m Model) worktreePathOf(branch string) (string, bool) {
	if branch == "" {
		return "", false
	}
	for _, w := range m.worktrees {
		if w.Branch == branch {
			return w.Path, true
		}
	}
	return "", false
}

func (m Model) branchRows() []string {
	inScope := func(b model.Branch) bool { return slices.Contains(m.commitScopeBranches, b.Name) }
	// Ordered left-to-right; each maps a branch to its glyph or ' '. Indicators
	// live in a left gutter so the set marker is never truncated in a narrow
	// panel; the gutter width is dynamic (see below).
	indicators := []func(model.Branch) rune{
		func(b model.Branch) rune { // set / solo
			if inScope(b) {
				return '◉'
			}
			return ' '
		},
		func(b model.Branch) rune { // head
			if b.IsHead {
				return '*'
			}
			return ' '
		},
	}
	// A column is in play iff some branch yields a non-space glyph for it.
	active := make([]bool, len(indicators))
	for i, ind := range indicators {
		for _, b := range m.branches {
			if ind(b) != ' ' {
				active[i] = true
				break
			}
		}
	}
	out := make([]string, 0, len(m.branches))
	for _, b := range m.branches {
		gutter := make([]rune, 0, len(indicators)+1)
		for i, ind := range indicators {
			if active[i] {
				gutter = append(gutter, ind(b))
			}
		}
		if len(gutter) > 0 {
			gutter = append(gutter, ' ') // one separator before the name
		}
		row := string(gutter) + b.Name
		if b.Behind > 0 {
			row += " (↓" + strconv.Itoa(b.Behind) + ")"
		}
		if path, ok := m.worktreePathOf(b.Name); ok {
			row += " (" + path + ")"
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

// commitBranchHint returns "⎇ <branch> · # <id>" for the selected commit when
// the Commits panel is focused, else "". The branch is the ref the commit was
// reached from in the feed walk (model.Commit.Source, via `git log --source`/%S);
// the short id lives here because the commit list rows show the branch column
// instead of the id. Shown in the status line, occluding no commit row.
func (m Model) commitBranchHint() string {
	if m.focus != panelCommits {
		return ""
	}
	if r, ok := m.wipRowAt(m.commitSelUnified()); ok { // pseudo-row: no commit id
		return fmt.Sprintf("%s · %d files", strings.ToLower(r.label()), r.count)
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok || bi < 0 || bi >= len(m.commits) {
		return ""
	}
	c := m.commits[bi]
	var parts []string
	if c.Source != "" {
		parts = append(parts, "⎇ "+c.Source)
	}
	if h := c.Hash; h != "" { // the commit id moved here from the row
		if len(h) > 7 {
			h = h[:7]
		}
		parts = append(parts, "# "+h)
	}
	return strings.Join(parts, " · ")
}

// commitRows builds the Commits-panel display rows. The left column is the
// branch-identity token (replacing the old 7-char commit id): the branch name,
// trimmed to commitIdentW. commitFullRows is the same with the UNtrimmed name
// (the reveal-tooltip source). The commit id itself moved to the status bar.
func (m Model) commitRows() []string     { return m.commitIdentRows(false) }
func (m Model) commitFullRows() []string { return m.commitIdentRows(true) }

// commitBody builds the Commits panel rows + decorators for renderPanel. In
// cutoff/scroll display mode only the rows the window will show are styled;
// off-window entries are empty strings that occupy the same single display line,
// so renderPanel's windowing math and visible output are identical to styling
// every row (O(visible) instead of O(feed) per frame). In wrap mode a row may
// span several lines, so every row is styled for exactness. The returned slices
// are full-length, keeping renderPanel's selection/mark indexing unchanged.
func (m Model) commitBody(boxH int) (rows []string, idx []int, decos []rowDecorator) {
	idx = m.displayIndices(panelCommits)
	rows = make([]string, len(idx))
	rowsCap := boxH - 3 // mirrors renderPanel: borders (2) + label line
	if rowsCap < 1 {
		return rows, idx, nil // panel shows the label only; no rows rendered
	}
	if m.dispModes[panelCommits] == modeWrap || len(idx) <= rowsCap {
		w := m.commitIdentWidth()
		for n, i := range idx {
			rows[n] = m.commitIdentRowAt(i, w, false)
		}
		return rows, idx, m.commitDecorators(rows, idx)
	}
	sel := m.sel[panelCommits]
	start := windowStart(len(idx), rowsCap, sel)
	end := start + rowsCap
	if end > len(idx) {
		end = len(idx)
	}
	w := m.commitIdentWidth()
	for n := start; n < end; n++ {
		rows[n] = m.commitIdentRowAt(idx[n], w, false)
	}
	return rows, idx, m.commitDecoratorsRange(rows, idx, start, end)
}

// commitTextReveals is the per-commit reveal text the tooltip renders: the full
// (untrimmed) branch label + any pills + the subject — with NO graph prefix and
// NO fixed-width identity padding. The graph is positional, so revealing its
// lanes in a horizontal strip is meaningless, and the 16-col padding would leave
// a gap between a short branch name and the subject. (commitFullRows still drives
// the WHEN-to-reveal decision; this is only WHAT gets drawn.)
func (m Model) commitTextReveals() []string {
	out := make([]string, m.commitsTotal())
	for i := range out {
		out[i] = m.commitTextRevealAt(i)
	}
	return out
}

// commitTextRevealAt is the per-index form of commitTextReveals.
func (m Model) commitTextRevealAt(i int) string {
	if r, ok := m.wipRowAt(i); ok {
		return r.text()
	}
	c := m.commits[i-m.wipCount()]
	id := commitIdentOf(c)
	label := id.label()
	if label != "" {
		label += " "
	}
	return label + id.pills() + c.Subject
}

// commitIdentWidth is the display width of the branch-identity column: the
// widest branch label currently loaded, capped at commitIdentW. Sizing to the
// loaded names (instead of a fixed 16) removes the padding gap for short common
// names like "master" while still aligning subjects within a feed; a longer name
// paging in grows the column up to the cap.
func (m Model) commitIdentWidth() int {
	w := 0
	for _, c := range m.commits {
		if lw := lipgloss.Width(commitIdentOf(c).label()); lw > w {
			if w = lw; w >= commitIdentW {
				return commitIdentW
			}
		}
	}
	return w
}

func (m Model) commitIdentRows(full bool) []string {
	w := m.commitIdentWidth()
	out := make([]string, m.commitsTotal())
	for i := range out {
		out[i] = m.commitIdentRowAt(i, w, full)
	}
	return out
}

// commitIdentRowAt builds one Commits-panel display row: the identity token
// (trimmed unless full), pills, subject, prefixed by the list-mode dot or the
// windowed graph cells. w is the shared identity-column width. Single-sourced by
// both commitIdentRows (all rows) and commitList.Full (one row, lazily).
func (m Model) commitIdentRowAt(i, w int, full bool) string {
	if r, ok := m.wipRowAt(i); ok {
		row := r.text()
		switch {
		case m.commitListMode:
			row = wipNodeGlyph + " " + row
		case !m.commitListMode && m.commitGraphOn() && len(m.commitGraphRows) == m.commitsTotal():
			win, _, _ := m.graphWindow(m.commitGraphRows[i])
			row = win + " " + row
		}
		return row
	}
	c := m.commits[i-m.wipCount()]
	id := commitIdentOf(c)
	var tok string
	if full {
		tok = id.fullToken(w)
	} else {
		tok, _ = id.token(w)
	}
	row := tok + " " + id.pills() + c.Subject
	switch {
	case m.commitListMode:
		row = "● " + row
	case !m.commitListMode && m.commitGraphOn() && len(m.commitGraphRows) == m.commitsTotal():
		win, _, _ := m.graphWindow(m.commitGraphRows[i])
		row = win + " " + row
	}
	return row
}

// commitGraphOn reports whether the graph is coherent to draw: the Commits panel
// must be in natural feed order (no filter, default sort) so rows are contiguous
// and the lane topology stays valid (and the glyphs stay out of the filter
// haystack).
func (m Model) commitGraphOn() bool {
	return !m.filterActive(panelCommits) && m.sortModes[panelCommits] == sortDefault
}

// commitDecorators returns a per-display-row decorator slice (parallel to rows):
// it dims the identity column on LINEAGE rows (always, in every mode) and colors
// each commit's '●' node by its lane (only when lane coloring is active). idx
// maps display row → backing commit index (from panelView). Columns include the
// 2-col selection prefix the renderer prepends.
func (m Model) commitDecorators(rows []string, idx []int) []rowDecorator {
	return m.commitDecoratorsRange(rows, idx, 0, len(rows))
}

// commitDecoratorsRange builds the per-row decorators for display rows [lo,hi)
// only, returning a full-length slice (off-window entries stay nil — renderPanel
// skips a nil decorator). Windowed rendering decorates just the visible rows.
func (m Model) commitDecoratorsRange(rows []string, idx []int, lo, hi int) []rowDecorator {
	if len(rows) == 0 {
		return nil
	}
	laneColorOn := len(m.commitGraphLanes) == m.commitsTotal() && (m.commitListMode || m.commitGraphOn())
	graphPrefix := !m.commitListMode && m.commitGraphOn() && len(m.commitGraphRows) == m.commitsTotal()
	decos := make([]rowDecorator, len(rows))
	identW := m.commitIdentWidth() // loop-invariant: compute once, not per row
	for j := lo; j < hi; j++ {
		ci := j
		if j < len(idx) {
			ci = idx[j]
		}
		if ci < 0 || ci >= m.commitsTotal() {
			continue
		}
		// @-highlight: a non-matching row is dimmed whole; matching rows keep the
		// normal lane/lineage decoration below. Selection style still wins in
		// renderPanel, so the cursor row is never dimmed.
		if m.highlightActive() && m.highlightQuery != "" && !m.commitMatchesHighlight(ci) {
			decos[j] = dimRowDecorator()
			continue
		}
		if m.isWipRow(ci) {
			continue // ◇ node lives in the graph cells; no lineage/lane decoration
		}
		id := commitIdentOf(m.commits[ci-m.wipCount()])
		dim := !id.tip && id.name != "" // gray a lineage row's branch name

		// identStart = the 2-col selection prefix + this row's leading glyphs. In
		// graph mode the prefix is the fixed-width window (cols*2) + a trailing
		// space; the ⋯ edge markers replace columns inside it, so they add no
		// width. This single value also drives dotCol below (no independent math).
		identStart := 2
		if m.commitListMode {
			identStart += 2 // "● "
		} else if graphPrefix {
			identStart += m.graphCols()*2 + 1
		}

		hasDot := false
		dotCol := 0
		var dotColor lipgloss.Color
		if laneColorOn {
			lane := m.commitGraphLanes[ci]
			if m.commitListMode {
				dotCol = 2 // ● at content col 0 + 2 prefix
				dotColor = laneColor(lane)
				hasDot = true
			} else {
				// Graph mode: the node is drawn only when its lane is inside the
				// window. Suppress it when it lands exactly on the left ⋯ marker.
				cols := m.graphCols()
				scroll := m.commitGraphScroll
				if lane >= scroll && lane < scroll+cols && !(scroll > 0 && lane == scroll) {
					dotCol = 2 + (lane-scroll)*2
					dotColor = laneColor(lane)
					hasDot = true
				}
			}
		}
		if !dim && !hasDot {
			continue
		}
		decos[j] = commitLineDecorator(hasDot, dotCol, dotColor, dim, identStart, identW)
	}
	return decos
}

// commitHaystacks is the filter-match text per commit, decoupled from the
// (trimmed, id-less) display row so searching still works: the FULL commit id +
// the full branch name(s) + the subject. commitList.Haystack exposes it to
// panelView, which prefers it over Row(i) for matching.
func (m Model) commitHaystacks() []string {
	out := make([]string, m.commitsTotal())
	for i := range out {
		out[i] = m.commitHaystackAt(i)
	}
	return out
}

// commitHaystackAt is the per-index form of commitHaystacks: full hash + full
// branch name(s) + subject, for filter matching. No styling.
func (m Model) commitHaystackAt(i int) string {
	if r, ok := m.wipRowAt(i); ok {
		return r.text()
	}
	c := m.commits[i-m.wipCount()]
	id := commitIdentOf(c)
	names := id.label()
	for _, e := range id.extra {
		names += " " + e
	}
	return c.Hash + " " + names + " " + c.Subject
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
