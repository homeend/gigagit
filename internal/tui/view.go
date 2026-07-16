package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/pusherr"
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
	// errorStyle is a plain red foreground for inline popup error lines (e.g. an
	// unresolved commit-ish in the show-commit popup).
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
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
	if msg, ok := friendlyPushError(low); ok {
		return msg
	}
	return "error: " + s
}

// friendlyPushError rewrites git's multi-line push-rejection stderr into one
// actionable status line. A push rejection dumps a "! [rejected] … (reason)"
// line plus a wall of hints — useless in a single-line status bar — so the
// common reasons are mapped to a short sentence that says what to do next. The
// signatures are classified by internal/pusherr, the single source of truth
// shared with the engine's recovery trigger. Order matters: the server-side
// rejection and the force-with-lease "stale info" cases are distinguished
// before the generic non-fast-forward case. The argument is the error text
// (pusherr lowercases internally, so an already-lowercased value is fine).
//
// Messages are kept tight so the remedy survives status-bar truncation on an
// ~80-col terminal — the action is the whole point of the rewrite.
func friendlyPushError(low string) (string, bool) {
	switch {
	case pusherr.IsHookRejection(low):
		return "error: push rejected by the remote (protected branch or server-side hook)", true
	case pusherr.IsStaleInfo(low):
		return "error: force-with-lease refused — remote moved; fetch & review, then retry", true
	case pusherr.IsNonFastForward(low):
		return "error: push rejected — remote has new commits; pull/rebase first, or force-push", true
	}
	return "", false
}

// statusIsError reports whether a status message reports a failure. Keys
// are English, so the English prefixes classify directly; under a non-
// English catalog the same classification is derived per call from the
// translations of the error-prefixed KEYS (the head of each translation up
// to its first verb). A translation that reorders its arguments ahead of
// the topic word yields a too-short head and is skipped — that message
// just renders unstyled, never mis-styled.
func statusIsError(msg string) bool {
	for _, p := range statusErrorPrefixes {
		if strings.HasPrefix(msg, p) {
			return true
		}
	}
	for k, tr := range i18n.ActiveTranslations() {
		if !strings.ContainsRune(k, '%') {
			continue // error-status keys are "<prefix>: %s"-shaped; a verb-less key sharing the prefix (e.g. a footer) is not a status message
		}
		for _, p := range statusErrorPrefixes {
			if !strings.HasPrefix(k, p) {
				continue
			}
			head := tr
			if i := strings.IndexByte(head, '%'); i >= 0 {
				head = head[:i]
			}
			head = strings.TrimSpace(head)
			if len([]rune(head)) >= 2 && strings.HasPrefix(msg, head) {
				return true
			}
			break
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

// layerBase is what the layer stack composites over: the panel interface.
// Surfaces in the stack ignore it (they own the screen); popups composite
// their centered box onto it.
func (m Model) layerBase() string {
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
	if m.layers != nil && len(m.layers.entries) > 0 {
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

// popupFullInnerWidth is the content width for a screen that wants nearly the
// entire terminal (the External-tools wizard, live feedback: the fixed
// popupInnerWidth left the box narrow enough that a long shell command wrapped
// mid-word and the background bled visibly around the box's edges). Unlike
// popupWideInnerWidth's 96-column cap, there is no upper bound here — only the
// same margin/floor so the box never touches the screen edges or collapses on
// a tiny terminal.
func popupFullInnerWidth(w int) int {
	inner := w - 8
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

// formatElapsed renders an op's running duration compactly for the busy line:
// "5s", "1m30s", "2h03m". Rounded to whole seconds.
func formatElapsed(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}

// renderInterface draws the header, the panels, and the footer/status, sized to
// fit the current terminal so the output never exceeds width×height.
func (m Model) renderInterface() string {
	g := m.layout()

	header := m.headerLine(g.w)
	footer, _ := fitFooter(m, g.w)
	errMode := statusIsError(m.statusMsg)
	var notice string
	// Suppressed while the conflict process is active — it draws its own window
	// over this background, so a "press [x] to resolve" notice would be wrong.
	if n := len(m.status.Conflicts()); n > 0 && m.proc == nil {
		notice = conflictNotice(n, describeConflict(m.conflict))
	} else if m.conflict.Op != "" && m.proc == nil {
		// A sequencer op is paused with nothing left unmerged (resolved
		// outside gg, or all handled and the process left open-ended).
		notice = pausedNotice(opDisplayName(m.conflict.Op), describeConflict(m.conflict))
	}
	var markHint string
	if m.mark != nil && m.markAlive() {
		markHint = i18n.T("◆ marked: %s", m.mark.display)
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
		add(m.noticeSegment())
		add(m.reviewSegment())
		add(markHint)
	} else {
		add(m.noticeSegment())
		add(m.reviewSegment())
		add(markHint)
		add(notice)
		add(m.statusMsg)
		add(m.commitBranchHint())
		add(m.bgRefreshHint())
	}
	statusLine := strings.Join(parts, " · ")
	if m.running {
		statusLine = "⏳ " + statusLine
		// Append the elapsed time so a long op (a 20GB worktree checkout that
		// emits no events) visibly advances instead of looking frozen. The
		// heartbeat tick re-renders this once a second.
		if !m.opStart.IsZero() {
			statusLine += " · " + formatElapsed(time.Since(m.opStart))
		}
	}
	if m.anySourceLoading() && !m.running {
		if statusLine == "" {
			statusLine = "⏳ reloading…"
		} else {
			statusLine = "⏳ reloading… · " + statusLine
		}
	}
	statusLine = truncate(oneLine(statusLine), g.w)
	// Style after truncation: truncate slices runes and would corrupt ANSI codes.
	if errMode {
		statusLine = statusErrStyle.Render(statusLine)
	}

	// Narrow terminals: a single commits column (two columns won't fit cleanly).
	if g.w < 40 {
		cmRows, _, decos := m.commitBody(g.w, g.boxH[panelCommits])
		body := m.renderPanel(panelCommits, m.panelLabel(panelCommits, "Commits ("+m.commitScopeLabel()+")"), cmRows, decos, g.w, g.boxH[panelCommits])
		return strings.Join([]string{header, body, footer, statusLine}, "\n")
	}

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
			rows, _ := m.panelViewWindowed(p, g.boxH[p])
			boxes = append(boxes, m.renderPanel(p, m.leftPanelLabel(p), rows, nil, g.leftW, g.boxH[p]))
		}
		left = lipgloss.JoinVertical(lipgloss.Left, boxes...)
	}
	var right string
	switch {
	case g.boxH[panelCommits] <= 0:
		// a fullscreen left panel owns the whole body — no right column at all
	case m.filesPreview != nil:
		right = m.renderFilePreview(g.rightW, g.boxH[panelCommits])
	case m.stashView != nil:
		right = m.renderStashList(g.rightW, g.boxH[panelCommits])
	default:
		cmRows, _, cmDecos := m.commitBody(g.rightW, g.boxH[panelCommits])
		right = m.renderPanel(panelCommits, m.panelLabel(panelCommits, "Commits ("+m.commitScopeLabel()+")"), cmRows, cmDecos, g.rightW, g.boxH[panelCommits])
	}
	// One side can be empty (a ctrl+t fullscreen hides the other column entirely);
	// join only when both exist so no zero-width block leaks artifacts.
	var body string
	switch {
	case left == "":
		body = right
	case right == "":
		body = left
	default:
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}

	return strings.Join([]string{header, body, footer, statusLine}, "\n")
}

// headerLine renders the bold title plus branch info on the left, and the
// repository's full path right-aligned on the right. A path too long for the
// space left between them is middle-elided, always keeping the repo directory
// name (the path's final segment) visible.
func (m Model) headerLine(w int) string {
	rest := "  branch " + m.status.Branch
	if m.status.Upstream != "" {
		rest += fmt.Sprintf(" (↑%d ↓%d)", m.status.Ahead, m.status.Behind)
	}
	// The top-left title is the open repo/worktree's directory name; fall back to
	// the gigagit brand only when no path is known yet (e.g. before first load).
	name := pathLeaf(m.currentWorktree)
	if name == "" {
		name = "gigagit"
	}
	title := titleStyle.Render(name)
	titleW := lipgloss.Width(title)
	leftW := titleW + lipgloss.Width(rest)

	const gap = 2 // minimum spaces between the left text and the path
	const minPath = 4
	path := m.currentWorktree
	if path == "" || w <= 0 || leftW+gap+minPath > w {
		// No path, or no room for a meaningful one: keep the original left line.
		return title + truncate(rest, w-titleW)
	}
	p := elideMiddlePath(path, w-leftW-gap)
	pad := w - leftW - lipgloss.Width(p)
	return title + rest + strings.Repeat(" ", pad) + p
}

// elideMiddlePath shortens a filesystem path to at most n display columns by
// dropping characters from the MIDDLE and inserting a "…", keeping the path's
// head and — most importantly — its final segment (the repo directory name)
// visible. Falls back to a leading ellipsis when even that segment can't fit.
func elideMiddlePath(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	leaf := pathLeaf(s)
	avail := n - 1 // reserve a column for the middle "…"
	if lipgloss.Width(leaf) >= avail {
		// Even the dir name doesn't fit beside a head: show the path's tail.
		return elideLeft(s, n)
	}
	head := avail - lipgloss.Width(leaf)
	r := []rune(s)
	hi := 0
	for hi < len(r) && lipgloss.Width(string(r[:hi+1])) <= head {
		hi++
	}
	return string(r[:hi]) + "…" + leaf
}

// pathLeaf returns the final segment of a path (the directory name), tolerating
// both / and \ separators and trailing separators.
func pathLeaf(s string) string {
	s = strings.TrimRight(s, `/\`)
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		return s[i+1:]
	}
	return s
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

// tabSeg is one clickable tab in a left-column slot header: the panel it
// activates and the exact text rendered for it (brackets included when active).
// joinTabSegs renders the header string from a slot's segs while tabSegAt maps a
// click column back to a tab, so the label and the mouse hit-test share one
// source of truth and cannot drift (the same SYNC-INVARIANT discipline the
// commit-decoration row/colorer pair follows).
type tabSeg struct {
	p    panel
	text string
}

// joinTabSegs renders a slot's header by joining its segment texts with a single
// space — the exact separator tabSegAt accounts for when walking columns.
func joinTabSegs(segs []tabSeg) string {
	parts := make([]string, len(segs))
	for i, s := range segs {
		parts[i] = s.text
	}
	return strings.Join(parts, " ")
}

// tabSegAt maps a click column (relative to the header's first cell) back to the
// tab whose text covers it, mirroring joinTabSegs' single-space separator. ok is
// false in a separator gap or past the last tab (the sort/filter decoration
// panelLabel appends), where the click falls through to plain focus.
func tabSegAt(segs []tabSeg, col int) (panel, bool) {
	if col < 0 {
		return 0, false
	}
	off := 0
	for _, s := range segs {
		w := lipgloss.Width(s.text)
		if col >= off && col < off+w {
			return s.p, true
		}
		off += w + 1 // the single space joinTabSegs inserts between tabs
	}
	return 0, false
}

// topTabSegs builds the shared top-slot tabs (Branches · Remotes · Worktrees):
// the active tab spelled out and bracketed, the inactive ones single-letter
// markers so all three fit the narrow left column (w/3) even at 80 cols, leaving
// room for the sort/filter decoration panelLabel appends. Plain ASCII so
// renderPanel's truncate stays safe.
func topTabSegs(active panel) []tabSeg {
	mark := func(p panel, full, short string) string {
		if p == active {
			return "[" + full + "]"
		}
		return short
	}
	return []tabSeg{
		{panelBranches, mark(panelBranches, "Branches", "B")},
		{panelRemotes, mark(panelRemotes, "Remotes", "R")},
		{panelWorktrees, mark(panelWorktrees, "Worktrees", "W")},
	}
}

// filesTabSegs builds the middle-slot tabs (Files · Tags): the active tab spelled
// out with its row count and bracketed, the inactive tab shown plainly.
func filesTabSegs(active panel, filesN, tagsN int) []tabSeg {
	files := fmt.Sprintf("Files %d", filesN)
	tags := fmt.Sprintf("Tags %d", tagsN)
	if active == panelTags {
		return []tabSeg{{panelFiles, files}, {panelTags, "[" + tags + "]"}}
	}
	return []tabSeg{{panelFiles, "[" + files + "]"}, {panelTags, tags}}
}

// bottomTabSegs builds the bottom-slot tabs (Staged · Reflog): the active tab
// spelled out with its row count and bracketed, the inactive tab shown plainly.
func bottomTabSegs(active panel, stagedN, reflogN int) []tabSeg {
	staged := fmt.Sprintf("Staged %d", stagedN)
	reflog := fmt.Sprintf("Reflog %d", reflogN)
	if active == panelReflog {
		return []tabSeg{{panelStaged, staged}, {panelReflog, "[" + reflog + "]"}}
	}
	return []tabSeg{{panelStaged, "[" + staged + "]"}, {panelReflog, reflog}}
}

// bottomTabLabel is the bottom-left slot header. See bottomTabSegs.
func bottomTabLabel(active panel, stagedN, reflogN int) string {
	return joinTabSegs(bottomTabSegs(active, stagedN, reflogN))
}

// tabBarLabel is the shared top-slot header. See topTabSegs.
func tabBarLabel(active panel) string {
	return joinTabSegs(topTabSegs(active))
}

// filesTabLabel is the middle-slot header. See filesTabSegs.
func filesTabLabel(active panel, filesN, tagsN int) string {
	return joinTabSegs(filesTabSegs(active, filesN, tagsN))
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
		// Only the rows the window will actually show need their (potentially
		// expensive) per-row text built. In cutoff/scroll mode each row is exactly
		// one display line, so the visible span is [start,end) and off-window rows
		// collapse to an empty single line — the visible output is identical (the
		// window is centred on sel just as renderWindow re-derives it) but the work
		// is O(visible) instead of O(len(rows)). This is what keeps a pathological
		// untracked set (e.g. a 40k-file graphify-out/, whose ~92-col paths each hit
		// the O(len²) elideFilePath path) from re-eliding every row every frame and
		// freezing the UI. In wrap mode a row spreads over ≥1 lines, so the rowsCap-
		// line window can never show rows more than rowsCap away from the anchor —
		// [anchor-rowsCap, anchor+rowsCap] is output-identical to building all rows
		// (see renderWindow's wrap windowing; the span here must contain the one it
		// re-derives). commitBody/panelViewWindowed materialize the same wrap span,
		// so every row that reaches renderWindow is populated.
		start, end := 0, len(rows)
		anchor := sel
		if len(rows) > rowsCap {
			// Clamp the window anchor into range so a stale out-of-range sel (the
			// file list can shrink under a discard/stage/refresh before nav re-clamps
			// it) never lands the window on rows we skipped building — which would
			// blank the panel. renderWindow is handed the same clamped anchor so its
			// visible span matches the rows we populated.
			if anchor < 0 {
				anchor = 0
			} else if anchor >= len(rows) {
				anchor = len(rows) - 1
			}
			if m.dispModes[p] != modeWrap {
				start = windowStart(len(rows), rowsCap, anchor)
				end = start + rowsCap
			} else {
				if start = anchor - rowsCap; start < 0 {
					start = 0
				}
				if end = anchor + rowsCap + 1; end > len(rows) {
					end = len(rows)
				}
			}
		}
		// Size wr to the visible window, not the full row count: winRow embeds a
		// lipgloss.Style, so a full-length make on a 40k-row panel zeroes megabytes
		// every frame. renderWindow is handed the anchor rebased into this slice.
		wr := make([]winRow, 0, end-start)
		for i := start; i < end; i++ {
			row := rows[i]
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
			text := row
			// File panels (Files + Staged): middle-elide so a too-long path keeps
			// its filename instead of tail-truncating it off. Cutoff mode only —
			// wrap/scroll already reveal the whole row, and pre-eliding would
			// corrupt them.
			if m.isFilesPanel(p) && m.dispModes[p] == modeCutoff {
				text = elideFilePath(row, innerW-lipgloss.Width(prefix))
			}
			wr = append(wr, winRow{text: prefix + text, style: st, decorate: deco})
		}
		body := renderWindow(wr, winOpts{w: innerW, h: rowsCap, mode: m.dispModes[p], anchor: anchor - start, hscroll: m.hscroll[p]})
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

// elideFilePath shortens a Files-panel row ("<glyph> <path>") to fit n display
// columns, keeping the status glyph + its space as a fixed left column and
// middle-eliding the path so BOTH the beginning of the path and the filename
// survive — the directories nearest the file (the least identifying part) are
// the ones dropped. A plain truncate would cut off exactly the filename. Pre-
// elided to fit so renderWindow won't re-cut it.
func elideFilePath(row string, n int) string {
	if lipgloss.Width(row) <= n {
		return row
	}
	r := []rune(row)
	// Below 3 columns there is no room to keep both the glyph column and a
	// meaningful path; fall back to a plain left-elide (never overflows n).
	if len(r) < 2 || n < 3 {
		return elideLeft(row, n)
	}
	head := string(r[:2]) // "<glyph> "
	return head + elideFileMiddle(string(r[2:]), n-lipgloss.Width(head))
}

// elideFileMiddle shortens a slash-separated path to at most n display columns
// by keeping the filename (final segment) in full plus as much of the path's
// BEGINNING as fits, replacing the directories just before the filename with a
// "…". So "a/b/c/d/view.go" becomes "a/b/c…/view.go" — the leading characters
// fill the column up to the cut, then "…/<filename>". The kept head is trimmed
// at whatever character fills the width (it may stop mid-directory-name) so
// every column is used.
//
// This differs from elideMiddlePath (the header repo-path helper) by keeping the
// final path separator, so the result reads as a path ("…/view.go"); there the
// kept tail is a directory name shown without a leading slash.
func elideFileMiddle(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	slash := strings.LastIndex(s, "/")
	if slash < 0 {
		return truncate(s, n) // a bare filename: nothing to elide around it
	}
	suffix := "…/" + s[slash+1:] // "…" + final separator + filename
	if lipgloss.Width(suffix) > n {
		// Not even "…/<filename>" fits; keep the filename's tail instead.
		return elideLeft(s, n)
	}
	return takeWidth(s[:slash], n-lipgloss.Width(suffix)) + suffix
}

// takeWidth returns the longest leading run of s whose display width is <= w.
func takeWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)) > w {
		r = r[:len(r)-1]
	}
	return string(r)
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
// Blink = style alternation between these two on m.reviewBlink (the review runs
// in the background, not an error — a neutral cyan, not the notice red).
var (
	reviewHotStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	reviewDimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("31"))
)

// reviewSegment renders the blinking "a review is running" status indicator
// while a background review is in flight. Style alternation on m.reviewBlink
// (the noticeSegment idiom), never terminal-native blink. "" when no review is
// running (or the conflict process owns the screen).
func (m Model) reviewSegment() string {
	if !m.reviewRunning || m.proc != nil {
		return ""
	}
	seg := i18n.T("⟳ reviewing %s…", m.reviewRunningLabel)
	if m.reviewBlink {
		return reviewHotStyle.Render(seg)
	}
	return reviewDimStyle.Render(seg)
}

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

// commitBody builds the Commits panel rows + decorators for renderPanel. Only
// the rows the window can show are styled; off-window entries are empty strings,
// so the visible output is identical to styling every row (O(visible) instead
// of O(feed) per frame). In cutoff/scroll a row is one display line and the
// span is the exact window; in wrap mode a row spans ≥1 lines, so the window
// can never show rows more than rowsCap away from the selection — the span is
// [sel-rowsCap, sel+rowsCap], the same one renderPanel materializes (its wrap
// windowing must stay within what is built here). The returned slices are
// full-length, keeping renderPanel's selection/mark indexing unchanged.
func (m Model) commitBody(boxW, boxH int) (rows []string, idx []int, decos []rowDecorator) {
	idx = m.displayIndices(panelCommits)
	rows = make([]string, len(idx))
	rowsCap := boxH - 3 // mirrors renderPanel: borders (2) + label line
	if rowsCap < 1 {
		return rows, idx, nil // panel shows the label only; no rows rendered
	}
	w := m.commitIdentWidth()
	budget := m.commitGroupBudget(boxW, w)
	if len(idx) <= rowsCap {
		for n, i := range idx {
			rows[n] = m.commitIdentRowAt(i, w, false, budget)
		}
		return rows, idx, m.commitDecorators(rows, idx, budget)
	}
	sel := m.sel[panelCommits]
	var start, end int
	if m.dispModes[panelCommits] != modeWrap {
		start = windowStart(len(idx), rowsCap, sel)
		end = start + rowsCap
	} else {
		// Nearest-edge clamp for a stale out-of-range sel, mirroring renderPanel's
		// anchor clamp so the two spans coincide.
		if sel < 0 {
			sel = 0
		} else if sel >= len(idx) {
			sel = len(idx) - 1
		}
		if start = sel - rowsCap; start < 0 {
			start = 0
		}
		end = sel + rowsCap + 1
	}
	if end > len(idx) {
		end = len(idx)
	}
	for n := start; n < end; n++ {
		rows[n] = m.commitIdentRowAt(idx[n], w, false, budget)
	}
	return rows, idx, m.commitDecoratorsRange(rows, idx, start, end, budget)
}

// commitGroupBudget is the max display width the before-subject decoration
// group may occupy before collapsing to (+N). It reserves the fixed left
// columns (the 2-col selection prefix renderPanel prepends, the list/graph
// prefix, the 3-cell marker, the identity column, and the separating space)
// plus a minimum subject width out of the panel content width (boxW minus
// the 2 border columns). A non-positive result is clamped to 0 (collapse
// everything for extremely narrow panels).
func (m Model) commitGroupBudget(boxW, identW int) int {
	const minSubjectW = 12
	content := boxW - 2 // renderPanel left+right border cells
	prefix := 2         // 2-col selection prefix renderPanel prepends
	if m.commitListMode {
		prefix += 2 // "● "
	} else if !m.commitListMode && m.commitGraphOn() && len(m.commitGraphRows) == m.commitsTotal() {
		prefix += m.graphCols()*2 + 1
	}
	budget := content - prefix - commitMarkerW - identW - 1 - minSubjectW
	if budget < 0 {
		budget = 0
	}
	return budget
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
	id := commitIdentOf(c, m.trackedUpstreams())
	group, _, _ := commitDecoGroup(id, -1)
	combined := strings.TrimSpace(id.label() + group)
	if combined == "" {
		return c.Subject
	}
	return combined + " " + c.Subject
}

// trackedUpstreams maps each local branch's upstream short ref ("origin/main")
// to the branch name ("main"), for marking tracked remote-branch tips in the
// commit graph. A branch with no upstream is omitted.
func (m Model) trackedUpstreams() map[string]string {
	out := make(map[string]string, len(m.branches))
	for _, b := range m.branches {
		if b.Upstream != "" {
			out[b.Upstream] = b.Name
		}
	}
	return out
}

// commitIdentWidth is the display width of the branch-identity column: the
// widest branch label currently loaded, capped at commitIdentW. Sizing to the
// loaded names (instead of a fixed 16) removes the padding gap for short common
// names like "master" while still aligning subjects within a feed; a longer name
// paging in grows the column up to the cap.
// The width is O(n) to compute (a lipgloss scan over every loaded commit), and
// it is consulted by listFor/commitBody/decorators on every frame — so it is
// cached (identWCache, maintained by rebuildCommitGraph and invalidated on a
// branches change) and only scanned as a fallback when the cache is not valid
// (e.g. a test assigning m.commits directly).
func (m Model) commitIdentWidth() int {
	if m.identWValid {
		return m.identWCache
	}
	return m.scanCommitIdentWidth(m.commits)
}

// scanCommitIdentWidth measures the widest ident label over commits, capped at
// commitIdentW. The full-feed scan behind the identWCache; also used to extend
// the cache over just an appended page.
func (m Model) scanCommitIdentWidth(commits []model.Commit) int {
	tracked := m.trackedUpstreams()
	w := 0
	for _, c := range commits {
		if lw := lipgloss.Width(commitIdentOf(c, tracked).label()); lw > w {
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
		out[i] = m.commitIdentRowAt(i, w, full, -1)
	}
	return out
}

// commitIdentRowAt builds one Commits-panel display row: the identity token
// (trimmed unless full), the decoration group (extra branches + tags, budgeted),
// the subject, prefixed by the list-mode dot or the windowed graph cells.
// w is the shared identity-column width. budget is the max display width for
// the deco group before collapsing to (+N); budget < 0 means never collapse
// (used by reveal/measurement paths). Single-sourced by both commitIdentRows
// (all rows) and commitList.Full (one row, lazily).
func (m Model) commitIdentRowAt(i, w int, full bool, budget int) string {
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
	id := commitIdentOf(c, m.trackedUpstreams())
	var tok string
	if full {
		tok = id.fullToken(w)
	} else {
		tok, _ = id.token(w)
	}
	group, _, _ := commitDecoGroup(id, budget)
	row := tok + group + " " + c.Subject
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
// must be in natural feed order — no in-memory `/` filter, default sort, and no
// commit-scope path/author/date filter — so rows are contiguous and lane topology
// stays valid (and glyphs stay out of the filter haystack).
func (m Model) commitGraphOn() bool {
	return !m.filterActive(panelCommits) && m.sortModes[panelCommits] == sortDefault &&
		!m.commitFilter.filtered()
}

// commitDecorators returns a per-display-row decorator slice (parallel to rows):
// it dims the identity column on LINEAGE rows, colors each commit's '●' node by
// its lane, and colors tag-label spans yellow. idx maps display row → backing
// commit index (from panelView). Columns include the 2-col selection prefix the
// renderer prepends. budget is the decoration-group collapse budget used when
// building the rows (must be the same value so the group string and its tag spans
// are consistent — the SYNC INVARIANT).
func (m Model) commitDecorators(rows []string, idx []int, budget int) []rowDecorator {
	return m.commitDecoratorsRange(rows, idx, 0, len(rows), budget)
}

// commitDecoratorsRange builds the per-row decorators for display rows [lo,hi)
// only, returning a full-length slice (off-window entries stay nil — renderPanel
// skips a nil decorator). Windowed rendering decorates just the visible rows.
// budget is the decoration-group collapse budget used when building the row
// strings; it must be identical to the value commitIdentRowAt used so that
// commitDecoGroup returns the same string and spans (the SYNC INVARIANT).
func (m Model) commitDecoratorsRange(rows []string, idx []int, lo, hi int, budget int) []rowDecorator {
	if len(rows) == 0 {
		return nil
	}
	laneColorOn := len(m.commitGraphLanes) == m.commitsTotal() && (m.commitListMode || m.commitGraphOn())
	graphPrefix := !m.commitListMode && m.commitGraphOn() && len(m.commitGraphRows) == m.commitsTotal()
	decos := make([]rowDecorator, len(rows))
	identW := m.commitIdentWidth() // loop-invariant: compute once, not per row
	tracked := m.trackedUpstreams()
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
		id := commitIdentOf(m.commits[ci-m.wipCount()], tracked)
		dim := !id.tip && !id.remoteTip && id.name != "" // gray a lineage row's branch name

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
		identStart += commitMarkerW // skip the ↓↑ marker prefix; dim only the name column

		// Build color spans for ⊙tag labels in the deco group. SYNC INVARIANT:
		// commitDecoGroup is called with the SAME budget used by commitIdentRowAt
		// when building the row string — both receive the value commitBody computed
		// once. Any divergence would color phantom columns (group collapsed in row
		// but spans computed for full group, or vice versa).
		var colorSpans []coloredSpan
		_, tagSpans, _ := commitDecoGroup(id, budget)
		if len(tagSpans) > 0 {
			// groupBase: the group string starts immediately after the name column.
			// tok = markerField(commitMarkerW) + name(identW); identStart already
			// accounts for selection prefix + mode prefix + commitMarkerW, so the
			// name column occupies [identStart, identStart+identW) and the group
			// string starts at identStart+identW.
			groupBase := identStart + identW
			for _, s := range tagSpans {
				colorSpans = append(colorSpans, coloredSpan{
					Start:  groupBase + s.Offset,
					Length: s.Length,
					Style:  tagDecoStyle,
				})
			}
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
		if !dim && !hasDot && len(colorSpans) == 0 {
			continue
		}
		decos[j] = commitLineDecorator(hasDot, dotCol, dotColor, dim, identStart, identW, colorSpans)
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
// branch name(s) + tag name(s) + subject, for filter matching. No styling.
func (m Model) commitHaystackAt(i int) string {
	if r, ok := m.wipRowAt(i); ok {
		return r.text()
	}
	c := m.commits[i-m.wipCount()]
	id := commitIdentOf(c, nil) // nil: the filter haystack must not depend on tracked-upstream state
	names := id.label()
	for _, e := range id.extra {
		names += " " + e
	}
	for _, tg := range id.tags {
		names += " " + tg
	}
	return c.Hash + " " + names + " " + c.Subject
}

func (m Model) renderModal() string {
	// Bound content to the terminal so long dynamic text (a long branch name, an
	// export path) wraps instead of overflowing the box and being clipped by
	// overlayCenter. maxW leaves room for modalStyle's double border (2) and
	// horizontal padding (4) plus a 2-column margin off the screen edge; it is
	// only a CAP — short prompts stay compact and the box sizes to its content.
	w, _ := m.overlayDims()
	maxW := w - 8
	if maxW < 20 {
		maxW = 20
	}

	var b strings.Builder
	// Prompt: keep any explicit line breaks (e.g. the hook-approval script),
	// word-wrapping each physical line. wrapWords hard-chunks a single token
	// wider than maxW, so an unbreakable long branch name still fits.
	for _, line := range strings.Split(m.modal.req.Prompt, "\n") {
		if wrapped := wrapWords(line, maxW); len(wrapped) > 0 {
			b.WriteString(strings.Join(wrapped, "\n"))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Options: word-wrap each to leave room for the 2-column selection marker;
	// continuation lines indent under the text, and the selected option's
	// reverse-video highlight is applied to every one of its physical lines.
	optW := maxW - 2
	if optW < 1 {
		optW = 1
	}
	for i, opt := range m.modal.req.Options {
		wrapped := wrapWords(optionDisplayName(opt), optW)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}
		for j, seg := range wrapped {
			marker := "  "
			if j == 0 && i == m.modal.sel {
				marker = "> "
			}
			row := marker + seg
			if i == m.modal.sel {
				row = selectedRow.Render(row)
			}
			b.WriteString(row)
			b.WriteString("\n")
		}
	}

	footer := i18n.T("[↑/↓] choose  [enter] confirm  [esc] abort")
	b.WriteString("\n")
	if lipgloss.Width(footer) > maxW {
		b.WriteString(strings.Join(wrapWords(footer, maxW), "\n"))
	} else {
		b.WriteString(footer)
	}
	return modalStyle.Render(b.String()) + "\n"
}
