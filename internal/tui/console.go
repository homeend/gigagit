package tui

import (
	"github.com/charmbracelet/lipgloss"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// consoleRepaint is the minimum spacing of console repaints: a chatty agent
// must not turn every output chunk into a full frame.
const consoleRepaint = 33 * time.Millisecond

// consoleState is the agent console shown in the Commits column (docked) or
// over the whole body (maximised). The session itself lives in
// domain.Sessions(); closing the console never ends it.
type consoleState struct {
	id        domain.SessionID
	focused   bool
	maximized bool
	gen       int
	highHalf  rune            // a UTF-16 high surrogate waiting for its low half (Windows input)
	screen    <-chan struct{} // this console's own subscription to its session's screen
	cancel    func()          // drops it; every path that clears m.console goes through detachConsole
	ret       *consoleReturn  // the screen to go back to (never nil on a shown console)
	clipSeq   int             // the child's last OSC 52 write this console has taken (consumeConsoleClip)
	clicks    consoleClicks   // the last left press, for double / triple clicks
	press     *consolePress   // a left press at the live view not yet a drag (a plain click stays live)
	scroll    *consoleScroll  // scroll mode's frozen view; nil = live
	held      tea.MouseButton // a button forwarded to the child and not yet released (MouseButtonNone = none)
	heldAt    [2]int          // the emulator cell of the last press or motion forwarded
}

// consoleReturn is the screen a console was shown over: where esc, the
// step-out key twice and the last stop of an alt+a / alt+t cycle go back
// to. Captured by the first console shown, carried over when a console
// replaces a console, so "the screen before the agent" survives a cycle.
type consoleReturn struct {
	full      bool // a full-screen view or a ctrl+t pin: consoles show maximised
	fullMaxed bool
	fullMax   panel
	focus     panel
	view      model.CheckoutKey // the viewed worktree the console was shown over (where a close returns)
	// What the console DISPLACED (the full-screen views it covers, the stash
	// list, the preview) is not here: it waits on the worktree's slot
	// (windowState.consoleParked), so a return to another worktree brings
	// back only that worktree's own.
}

// sessionWatch is the TUI's subscription to the session LIST, on a pointer
// field so the value-receiver Model shares it. It follows the manager the
// TUI reads now (tests swap it), re-subscribing when that changes.
type sessionWatch struct {
	mgr    *domain.SessionManager
	ch     <-chan struct{}
	cancel func()
}

// current returns the list channel for the live manager.
func (w *sessionWatch) current() <-chan struct{} {
	mgr := domain.Sessions()
	if w.mgr != mgr {
		if w.cancel != nil {
			w.cancel()
		}
		w.mgr = mgr
		w.ch, w.cancel = mgr.Subscribe()
	}
	return w.ch
}

type consoleChangedMsg struct {
	id  domain.SessionID
	gen int
}

type sessionsChangedMsg struct{}

func (m Model) consoleSession() (*domain.AgentSession, bool) {
	if m.console == nil {
		return nil, false
	}
	return domain.Sessions().Get(m.console.id)
}

// openConsole shows session id docked in the Commits column with keyboard
// focus. The left panel that had focus is remembered for closeConsole.
func (m Model) openConsole(id domain.SessionID) (Model, tea.Cmd) {
	return m.showConsole(id, true)
}

// showConsole shows session id, focused or not (alt+a / alt+t show one
// unfocused): docked in the Commits column, or maximised when the screen it
// covers is full-screen. What it covers — the pin, the stash list or file
// preview, a parked layer stack, focus — goes into its return point, which
// a console replacing a console carries over. Only a focused show is a use
// of the session (Touch): cycling through them must not reorder the list it
// walks.
func (m Model) showConsole(id domain.SessionID, focused bool) (Model, tea.Cmd) {
	return m.showConsoleBy(id, focused, false)
}

// showConsoleBy is showConsole with the caller's intent: the user's own
// alt+a / alt+t (cycleSessions) may park a parkable popup for the swap to
// the console's worktree; a show asked by an agent, a task or a settle
// waits under any popup (the swap queues).
func (m Model) showConsoleBy(id domain.SessionID, focused, byUser bool) (Model, tea.Cmd) {
	s, ok := domain.Sessions().Get(id)
	if !ok {
		m.statusMsg = i18n.T("that agent session is gone")
		return m, nil
	}
	gen := 1
	if m.console != nil {
		gen = m.console.gen + 1
	}
	var ret *consoleReturn
	if m.console != nil {
		ret = m.console.ret // a console replacing a console keeps the way back
	}
	if ret == nil {
		m, ret = m.captureReturn()
	}
	if m.focus != panelCommits {
		m = m.rememberLeftFocus()
	}
	// What the console covers is in ret. A pinned panel would hide the
	// column the console docks in (and shrink its PTY to nothing), so the
	// pin drops while the console shows — maximised, as the pin was full —
	// and comes back with the return point.
	m.stashView = nil
	m.filesPreview = nil
	m.fullMaxed = false
	m = m.detachConsole()
	if focused {
		s.Touch()
	}
	screen, cancel := s.Subscribe()
	m.console = &consoleState{id: id, focused: focused, maximized: ret.full, gen: gen, screen: screen, cancel: cancel, ret: ret,
		clipSeq: s.Clipboard().Seq} // a copy made before it showed is not replayed
	m.focus = panelCommits
	m = m.syncConsoleSizeIfFocused()
	// A shown console ⇔ the viewed worktree is the console's: tab out of
	// it and the panels are already that tree's. A refusal (an op running,
	// a surface) keeps the view for now and says so; the console shows
	// regardless and the swap is QUEUED — the Update tail performs it once
	// the op or the surface clears, as a console's return is.
	m.pendingReturnView = "" // a return queued by an earlier close is moot: this console's own return point rules
	if dir := s.Info().Dir; model.KeyOf(dir) != m.viewed && m.isRepoWorktree(dir) {
		// The console's size is the business of the worktree it shows IN:
		// the full-screen view captured here waits in the worktree being
		// left, so only the pin carries over; loadView's displacement raises
		// it again if the arriving worktree has a full-screen view of its own.
		wasFull := ret.full
		ret.full = ret.fullMaxed
		m.console.maximized = ret.full
		var ok bool
		if m, ok = m.switchViewBy(dir, byUser); !ok {
			m.pendingReturnView = model.KeyOf(dir)
			ret.full = wasFull // still over the worktree whose view was captured
			m.console.maximized = wasFull
		}
		m = m.syncConsoleSizeIfFocused()
	}
	return m, waitSessionCmd(m.console, id, gen)
}

// returnView brings the worktree a console was shown over back when the
// console closes. An operation running refuses the swap for now: the
// return is kept (pendingReturnView) and happens when the op ends. A
// return point already on screen cancels whatever was queued — a show
// whose swap never happened.
func (m Model) returnView(r *consoleReturn) Model {
	if r == nil || r.view == "" {
		return m
	}
	if r.view == m.viewed {
		m.pendingReturnView = "" // a show queued while the panels could not swap is moot: the return point is on screen
		return m
	}
	path := m.viewPath(r.view)
	if path == "" || !m.isRepoWorktree(path) {
		m.statusMsg = i18n.T("%s is gone — showing %s", shortWorktreeName(string(r.view)), shortWorktreeName(m.viewPath(m.viewed)))
		return m
	}
	nm, ok := m.switchView(path)
	if !ok {
		nm.pendingReturnView = r.view // an op or a surface: the Update tail takes it once clear
	}
	return nm
}

// captureReturn records the screen a console is about to cover. A
// full-screen VIEW on top (diff, history, blame, file viewer) is parked off
// the live stack — it would draw over the console and keep the keys — and
// makes the return point full, as an active pin does. Anything else on top
// (a popup being typed into, an editor holding an operation's input) stays
// live and keeps the keyboard: a console opening on its own (an agent
// started, an AI task) must never take it.
func (m Model) captureReturn() (Model, *consoleReturn) {
	r := &consoleReturn{
		fullMaxed: m.fullMaxed, fullMax: m.fullMax,
		focus: m.focus,
		full:  m.fullMaxActive(),
		view:  m.viewed,
	}
	cp := &consoleParked{stashView: m.stashView, filesView: m.filesView, filesPreview: m.filesPreview}
	switch m.topLayer().(type) {
	case *diffView, *historyView, *blameView, *fileViewer:
		cp.layers = m.layers.entries
		m.layers.entries = nil
		r.full = true
	}
	m.consoleParked = cp // on the viewed worktree's slot: it travels with its windows
	return m, r
}

// displaceUnderConsole is captureReturn for a worktree ARRIVING under a
// shown console (a console opened into a worktree whose parked windows
// wait on its pile, a queued return landing while a console shows): the
// whole pile, the stash list and the preview go under the console's copy —
// a focused console takes every key, so a view left live over it would
// feed the user's keystrokes to an agent they cannot see. They come back
// with restoreConsoleParked when the console closes. A full-screen view
// among them shows the console maximised, as a captured one does.
func (m Model) displaceUnderConsole() Model {
	if m.console == nil || m.layers == nil && m.stashView == nil && m.filesPreview == nil {
		return m
	}
	cp := m.consoleParked
	if cp == nil {
		cp = &consoleParked{}
		m.consoleParked = cp
	}
	if m.layers != nil && len(m.layers.entries) > 0 {
		switch m.topLayer().(type) {
		case *diffView, *historyView, *blameView, *fileViewer:
			if r := m.console.ret; r != nil && !r.full {
				r.full = true
				m.console.maximized = true
			}
		}
		cp.layers = append(cp.layers, m.layers.entries...)
		m.layers.entries = nil
	}
	if m.stashView != nil {
		cp.stashView = m.stashView
		m.stashView = nil
	}
	if m.filesPreview != nil {
		cp.filesView, cp.filesPreview = m.filesView, m.filesPreview
		m.filesPreview = nil
	}
	return m
}

// restoreConsoleParked puts back what a console displaced in the worktree
// on screen: its parked views beneath whatever is live (a popup opened
// over the console stays on top), its stash list, its preview (only
// inside the files view it belongs to).
func (m Model) restoreConsoleParked() Model {
	cp := m.consoleParked
	if cp == nil {
		return m
	}
	m.consoleParked = nil
	m = m.restoreLayersBeneath(cp.layers)
	m.stashView = cp.stashView
	if cp.filesPreview != nil && cp.filesView != nil && m.filesView == cp.filesView {
		m.filesPreview = cp.filesPreview
	}
	return m
}

// forgetConsoleReturn drops what the console covers that belongs to the
// checkout gg is leaving (a repo switch): the parked views, the stash list,
// the preview. The pin and focus are panel states and stay; a console that
// was full only for its parked views docks again.
func (m Model) forgetConsoleReturn() Model {
	if m.console == nil || m.console.ret == nil {
		return m
	}
	r := m.console.ret
	m.consoleParked = nil // the displaced views were the old checkout's
	r.view = ""           // the viewed worktree was the old repository's
	r.full = r.fullMaxed
	m.console.maximized = r.full
	return m.syncConsoleSizeIfFocused()
}

// dispatchParkedAware delivers a message. A non-input one (a load result, a
// resize) is handled with the console's parked views put back beneath the
// live stack, so a view parked under a console still receives what it
// asked for; afterwards whatever of them is still there is parked again.
// Keys and mouse never see parked views — the console covers them.
func (m Model) dispatchParkedAware(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyMsg, tea.MouseMsg:
		return m.dispatch(msg)
	}
	cp := m.consoleParked
	if m.console == nil || cp == nil || len(cp.layers) == 0 {
		return m.dispatch(msg)
	}
	parked := cp.layers
	cp.layers = nil // a close while handling finds them live already
	m = m.restoreLayersBeneath(parked)
	view := m.viewed
	nm, cmd := m.dispatch(msg)
	out, ok := nm.(Model)
	if !ok {
		return nm, cmd
	}
	if out.viewed != view {
		// The handler moved the panels: the views put back live went into
		// the leaving slot's pile with its group (saveView). They were
		// displaced by the console there: back under its parked copy.
		if v := out.views[view]; v != nil {
			v.windows.reparkConsole(parked)
		}
		return out, cmd
	}
	if out.console == nil || out.layers == nil {
		return out, cmd // the console went: its views are live now
	}
	out.windowState.reparkConsole(parked)
	return out, cmd
}

// consoleFull reports a console shown over a full-screen return point.
func (m Model) consoleFull() bool {
	return m.console != nil && m.console.ret != nil && m.console.ret.full
}

// restoreLayersBeneath puts a parked stack back UNDER whatever is live now
// (a popup opened over the console stays on top).
func (m Model) restoreLayersBeneath(parked []layer) Model {
	if len(parked) == 0 {
		return m
	}
	if m.layers == nil {
		m.layers = &layerStack{}
	}
	m.layers.entries = append(append([]layer{}, parked...), m.layers.entries...)
	return m
}

// detachConsole clears the console and its screen subscription. Every place
// that sets m.console = nil goes through it, so a closed console never
// leaves a subscriber behind on its session.
func (m Model) detachConsole() Model {
	if m.console != nil && m.console.cancel != nil {
		m.console.cancel()
	}
	m.console = nil
	return m
}

// dropConsole is a console stepping aside for another right-column owner
// (stash list, file preview, a solo): the session keeps running and the
// return point is dropped — except a parked view, which is never lost.
func (m Model) dropConsole() Model {
	if m.console != nil {
		m = m.restoreConsoleParked() // the slot on screen: its own displaced views only
	}
	return m.detachConsole()
}

// closeConsole hides the console (the session keeps running) and puts back
// the screen it was shown over. Focus returns only when it sat in the
// console's column: a click elsewhere already moved it.
func (m Model) closeConsole() Model {
	if m.console == nil {
		return m
	}
	r := m.console.ret
	m = m.detachConsole()
	m = m.returnView(r)
	if r == nil {
		m.focus = m.lastLeftPanel
		return m.reconcileFullscreenFocus()
	}
	m = m.restoreConsoleParked() // the worktree on screen NOW (the return point, or where the user went)
	m.fullMaxed, m.fullMax = r.fullMaxed, r.fullMax
	if m.focus == panelCommits {
		m.focus = r.focus
	}
	return m.reconcileFullscreenFocus()
}

// consoleBox is the box the console occupies now: the Commits column, or the
// whole body when maximised.
func (m Model) consoleBox() (w, h int) {
	g := m.layout()
	if m.console != nil && m.console.maximized {
		return g.w, g.bodyH
	}
	if g.w < 40 {
		return g.w, g.boxH[panelCommits]
	}
	return g.rightW, g.boxH[panelCommits]
}

// consoleInner is the emulator size for a box: border (2) + padding (2)
// across, border (2) + the title line down.
func consoleInner(boxW, boxH int) (cols, rows int) {
	return max(boxW-4, 1), max(boxH-3, 1)
}

// syncConsoleSize resizes the shown session's PTY to its box (a no-op when
// unchanged, so it is safe to call after every layout-affecting event).
func (m Model) syncConsoleSize() Model {
	s, ok := m.consoleSession()
	if !ok {
		return m
	}
	w, h := m.consoleBox()
	cols, rows := consoleInner(w, h)
	if in := s.Input(); m.console.scroll != nil && (in.Cols != max(cols, 20) || in.Rows != max(rows, 5)) {
		m.console.scroll = nil // the snapshot is the old size
	}
	_ = s.Resize(cols, rows)
	return m
}

// syncConsoleSizeIfFocused is the window-resize rule: only a focused (or
// maximised) console owns the session's size. An unfocused one follows
// whatever viewer is typing — the web page, or another gg — and clips.
func (m Model) syncConsoleSizeIfFocused() Model {
	if m.console == nil || !m.console.focused {
		return m
	}
	return m.syncConsoleSize()
}

// waitSessionCmd blocks until the console's session changes, then waits out
// the repaint spacing (absorbing further changes) before asking for a frame.
// The channel is the console's own subscription: a browser console on the
// same session in this process has its own and neither steals a wakeup.
func waitSessionCmd(c *consoleState, id domain.SessionID, gen int) tea.Cmd {
	ch := c.screen
	if ch == nil {
		return nil // a console built as a literal (tests) has no session behind it
	}
	return func() tea.Msg {
		<-ch
		time.Sleep(consoleRepaint)
		return consoleChangedMsg{id: id, gen: gen}
	}
}

// waitSessionsCmd reports list-level changes (start, exit, remove).
func (m Model) waitSessionsCmd() tea.Cmd {
	if m.quiet {
		return nil // headless: never-ending (headless.go)
	}
	if m.sessWatch == nil {
		return nil // a Model built as a literal (tests)
	}
	ch := m.sessWatch.current()
	return func() tea.Msg {
		<-ch
		return sessionsChangedMsg{}
	}
}

// consoleTitle is the box title: label · worktree · state. full = shown
// over a full-screen return point (its unfocused keys differ).
func consoleTitle(info domain.SessionInfo, focused, full bool) string {
	label, wt, state := consoleTitleParts(info)
	t := label + " · " + wt + " · " + state
	if !focused {
		t += consoleTitleHints(info.State == domain.SessionExited, full)
	}
	return t
}

// consoleTitleHints: an exited agent has nothing to type into, so its console
// offers [x] close (remove the session) and esc only hides it. A full-screen
// one is already maximised and esc goes back to the view it covers.
func consoleTitleHints(exited, full bool) string {
	switch {
	case exited && full:
		return "  " + i18n.T("[x] close  [esc] back")
	case full:
		return "  " + i18n.T("[enter] type  [X] kill+remove  [esc] back")
	case exited:
		return "  " + i18n.T("[x] close  [ctrl+t] maximise  [esc] hide")
	}
	return "  " + i18n.T("[enter] type  [X] kill+remove  [ctrl+t] maximise  [esc] close")
}

// consoleTitleParts: the label, the worktree's short name and the state (the
// running age or exit, then the activity).
func consoleTitleParts(info domain.SessionInfo) (label, wt, state string) {
	state = i18n.T("running %s", formatElapsed(time.Since(info.Started)))
	if info.State == domain.SessionExited {
		state = i18n.T("exited (%d)", info.ExitCode)
	} else if act := sessionActivityText(info.ID); act != "" {
		state += " · " + act
	}
	return info.Title(), shortWorktreeName(info.Dir), state
}

// consoleTitleFit is the title in w columns, keeping what it is for — the
// label and the state with its activity: the key hints go first, then the
// worktree name is cut in the middle, then dropped; only then is the end cut.
func consoleTitleFit(info domain.SessionInfo, focused, full bool, w int) string {
	if t := consoleTitle(info, focused, full); lipgloss.Width(t) <= w {
		return t
	}
	label, wt, state := consoleTitleParts(info)
	if room := w - lipgloss.Width(label+" ·  · "+state); room >= lipgloss.Width(wt) {
		return label + " · " + wt + " · " + state
	} else if room >= 4 {
		return label + " · " + elideNameMiddle(wt, room) + " · " + state
	}
	return truncate(label+" · "+state, w)
}

// fitConsoleRow cuts an emulator row of cols cells to the box's w columns:
// an unfocused console keeps its PTY size, which can be wider than the box
// (a wide glyph across the edge is dropped; styles stay balanced).
func fitConsoleRow(row string, cols, w int) string {
	if cols <= w {
		return row
	}
	return ansi.Truncate(row, w, "")
}

// renderConsole draws the console box. The emulator lines are ANSI strings
// sized to the inner width; Render drops trailing blanks, so each is padded.
func (m Model) renderConsole(boxW, boxH int) string {
	s := st()
	contentH := max(boxH-2, 1)
	innerW := max(boxW-4, 1)
	sess, ok := m.consoleSession()
	var lines []string
	if !ok {
		lines = []string{padRight(i18n.T("(agent session gone)"), innerW)}
	} else {
		info := sess.Info()
		if sc := m.console.scroll; sc != nil {
			lines = append(lines, padRight(truncate(m.consoleScrollTitle(sess), innerW), innerW))
			for i := sc.top; i < sc.hist.Len() && len(lines) < contentH; i++ {
				lines = append(lines, padRight(fitConsoleRow(sc.hist.Row(i, m.consoleRowMarks(i)), sc.hist.Width(), innerW), innerW))
			}
		} else {
			lines = append(lines, padRight(consoleTitleFit(info, m.console.focused, m.consoleFull(), innerW), innerW))
			var sc domain.SessionScreen
			if m.console.focused && info.State == domain.SessionRunning {
				sc = sess.ScreenWithCursor()
			} else {
				sc = sess.Screen()
			}
			for _, l := range sc.Lines {
				if len(lines) >= contentH {
					break
				}
				lines = append(lines, padRight(fitConsoleRow(l, sc.Cols, innerW), innerW))
			}
		}
	}
	for len(lines) < contentH {
		lines = append(lines, padRight("", innerW))
	}
	style := s.bluredPanel
	if m.focus == panelCommits {
		style = s.focusedPanel
	}
	return style.Render(strings.Join(lines, "\n"))
}

// consumeConsoleClip copies the child's newest OSC 52 write (a fullscreen
// agent's own selection) through the TUI's one clipboard writer. Only the
// shown console's session is read, and only while it has focus: a copy needs
// the user's mouse or keys in that console (a click focuses it). A write
// made while unfocused is spent, never copied later.
func (m Model) consumeConsoleClip() (Model, tea.Cmd) {
	s, ok := m.consoleSession()
	if !ok {
		return m, nil
	}
	c := s.Clipboard()
	if c.Seq == m.console.clipSeq {
		return m, nil
	}
	m.console.clipSeq = c.Seq
	if !m.console.focused {
		return m, nil
	}
	if c.Over {
		m.statusMsg = i18n.T("copy too large — dropped")
		return m, nil
	}
	return m, m.copyToClipboardCmd(copiedLines(strings.Count(strings.TrimSuffix(c.Text, "\n"), "\n")+1), c.Text)
}

// copiedLines is the status line of a console copy of n lines.
func copiedLines(n int) string {
	if n == 1 {
		return i18n.T("Copied 1 line")
	}
	return i18n.T("Copied %d lines", n)
}

// onSessionsChanged reacts to a session-list change (start, exit, remove):
// the Worktrees sub-rows re-derive on render; re-arm the waiter.
func (m Model) onSessionsChanged() (Model, tea.Cmd) {
	prev := m.sessionStates
	next := map[domain.SessionID]domain.SessionState{}
	for _, info := range domain.Sessions().List() {
		next[info.ID] = info.State
		was, seen := prev[info.ID]
		if !seen || was != domain.SessionRunning || info.State != domain.SessionExited {
			continue
		}
		if m.console != nil && m.console.id == info.ID && m.console.focused {
			// Nothing is left to type into: step out as ctrl+] would, so
			// gg's keys work again without the user asking.
			m.console.focused = false
			if m.console.maximized && !m.consoleFull() {
				m.console.maximized = false
				m = m.syncConsoleSize()
			}
		}
		m.statusMsg = i18n.T("%s in %s exited (%d)", info.Title(), shortWorktreeName(info.Dir), info.ExitCode)
	}
	m.sessionStates = next
	if m.console != nil {
		if _, ok := next[m.console.id]; !ok {
			// Its session was removed: give the screen it covered back
			// rather than show a box with nothing in it.
			m = m.closeConsole()
		}
	}
	// A session started, exited or went: the Worktrees claim marks (a claim
	// dies with its session) and the picker's live markers follow.
	m, reload := m.reloadSourcesCmd([]sourceKey{srcWorktrees}, reloadOpts{})
	return m, tea.Batch(m.waitSessionsCmd(), reload)
}

// runningSessionIn reports a running agent session whose cwd is dir.
func runningSessionIn(dir string) (domain.SessionInfo, bool) {
	for _, info := range domain.Sessions().List() {
		if info.State == domain.SessionRunning && filepath.Clean(info.Dir) == filepath.Clean(dir) {
			return info, true
		}
	}
	return domain.SessionInfo{}, false
}

// worktreeOrder is the BRANCHES tab's order of the worktrees — the branch
// rows that name a checkout, top to bottom as the panel shows them (its
// sort, by date newest first unless changed; its filter), then any
// worktree without such a row (detached, hidden by a filter), in list
// order — as indices into m.worktrees: what alt+w walks and what orders the
// session ring, so "next" is the row below in the tab the user walks.
func (m Model) worktreeOrder() []int {
	seen := make([]bool, len(m.worktrees))
	out := make([]int, 0, len(m.worktrees))
	ents := m.branchEntries()
	for _, u := range m.displayIndices(panelBranches) {
		if u >= len(ents) || ents[u].sub() {
			continue
		}
		name := m.branches[ents[u].br].Name
		for i, w := range m.worktrees {
			if w.Branch == name && !seen[i] && !w.Bare {
				seen[i] = true
				out = append(out, i)
				break
			}
		}
	}
	for i, w := range m.worktrees {
		if !seen[i] && !w.Bare { // the bare repository has no working tree: no ring stop
			out = append(out, i)
		}
	}
	return out
}

// worktreeIndex is a directory's position in the Branches tab's order of
// the worktrees (worktreeOrder), or len(m.worktrees) for one not listed.
func (m Model) worktreeIndex(dir string) int {
	key := model.KeyOf(dir)
	for pos, i := range m.worktreeOrder() {
		if model.KeyOf(m.worktrees[i].Path) == key {
			return pos
		}
	}
	return len(m.worktrees)
}

// sessionRing is what alt+a (agents) / alt+t (terminals) walk: this
// repository's running sessions of one kind in the Worktrees list's order
// — the order the eye walks — oldest first within a worktree, so the walk
// is the same whoever was used last.
func (m Model) sessionRing(terminal bool) []domain.SessionInfo {
	var out []domain.SessionInfo
	for _, info := range m.repoSessions(domain.Sessions().List()) {
		if info.State == domain.SessionRunning && info.Terminal == terminal {
			out = append(out, info)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if wa, wb := m.worktreeIndex(a.Dir), m.worktreeIndex(b.Dir); wa != wb {
			return wa < wb
		}
		if !a.Started.Equal(b.Started) {
			return a.Started.Before(b.Started)
		}
		return a.ID < b.ID
	})
	return out
}

// cycleSessions is alt+a (agents) / alt+t (terminals): walk this
// repository's running sessions of that kind, in the Worktrees list's
// order, binding the one shown (the keyboard is the agent's at once). From
// a bound session of the kind: the next one, around again past the last —
// never the screen the walk started from (alt+w or the step-out key leave
// a console). With gg's keyboard the walk starts at the viewed worktree:
// its own session first (shown unbound → bound, hidden → shown), with none
// there the nearest below in the list, wrapping. The only session of its
// kind, already bound: nothing but a status line.
func (m Model) cycleSessions(terminal bool) (Model, tea.Cmd) {
	list := m.sessionRing(terminal)
	if len(list) == 0 {
		if terminal {
			m.statusMsg = i18n.T("no running terminal in this repository — open one from the . menu of a worktree or a checked-out branch")
		} else {
			m.statusMsg = i18n.T("no running agent session in this repository — start one from the . menu of a worktree or a checked-out branch")
		}
		return m, nil
	}
	// A view pushed over a shown console (the palette opened a diff from a
	// full-screen agent) is a new starting screen: the console goes back
	// first — its parked views slot in beneath — and the walk restarts
	// from the viewed worktree with the whole stack as its return point.
	if m.console != nil && m.topLayer() != nil {
		m = m.closeConsole()
	}
	shown := -1
	if m.console != nil {
		for i, info := range list {
			if info.ID == m.console.id {
				shown = i
				break
			}
		}
	}
	var next int
	switch {
	case shown >= 0 && m.console.focused && len(list) == 1:
		if terminal {
			m.statusMsg = i18n.T("the only running terminal in this repository — already focused")
		} else {
			m.statusMsg = i18n.T("the only running agent session in this repository — already focused")
		}
		return m, nil
	case shown >= 0 && m.console.focused:
		next = (shown + 1) % len(list)
	default:
		// gg's keyboard (no console of the kind, or an unbound one): the
		// first session at or below the viewed worktree in list order,
		// wrapping to the top — a shown unbound console is bound when it
		// is that one.
		at := m.worktreeIndex(m.viewPath(m.viewed))
		for i, info := range list {
			if m.worktreeIndex(info.Dir) >= at {
				next = i
				break
			}
		}
	}
	info := list[next]
	var cmd tea.Cmd
	if m.console != nil && m.console.id == info.ID {
		m.console.focused = true
		m.touchConsole()
		m.focus = panelCommits
		m = m.syncConsoleSize()
	} else {
		m, cmd = m.showConsoleBy(info.ID, true, true) // the user's own alt+a / alt+t
	}
	if terminal {
		m.statusMsg = i18n.T("%s in %s — terminal %d of %d", info.Title(), shortWorktreeName(info.Dir), next+1, len(list))
	} else {
		m.statusMsg = i18n.T("%s in %s — agent %d of %d", info.Title(), shortWorktreeName(info.Dir), next+1, len(list))
	}
	return m, cmd
}

// consoleWorktreeHint is the docked console's worktree path for the status
// row — only when it is NOT the worktree the panels show (the user switched
// the panels elsewhere under a docked console, or the session runs outside
// the repository). A console in the viewed worktree needs no reminder: the
// panels follow it and the header names it.
func (m Model) consoleWorktreeHint() string {
	sess, ok := m.consoleSession()
	if !ok {
		return ""
	}
	dir := sess.Info().Dir
	if filepath.Clean(dir) == filepath.Clean(m.currentWorktree) {
		return ""
	}
	return dir
}

// withConsoleWorktree trails the status row with the console's worktree path
// (when it differs from the panels') and fits the whole row to w columns. The path keeps up to half the row:
// the text before it is cut first, then the path in the middle (its start
// and its directory name survive); dropped only when not even a stub fits.
func (m Model) withConsoleWorktree(row string, w int) string {
	dir := m.consoleWorktreeHint()
	if dir == "" {
		return row
	}
	sep := ""
	if row != "" {
		sep = " · "
	}
	label := lipgloss.Width(i18n.T("worktree: %s", ""))
	room := max(w-lipgloss.Width(row+sep)-label, min(lipgloss.Width(dir), w/2-lipgloss.Width(sep)-label))
	if room < 8 {
		return row
	}
	path := i18n.T("worktree: %s", elidePath(dir, room))
	if row == "" {
		return path
	}
	return truncate(row, w-lipgloss.Width(path+sep)) + sep + path
}

// shortWorktreeName is the worktree's directory name, for titles and rows.
func shortWorktreeName(path string) string { return filepath.Base(path) }

// Default reserved keys; [console] step_out_key / sessions_key override
// them. m.cfg is the zero value until the first load, hence the fallback.
const (
	defaultStepOutKey  = "ctrl+]"
	defaultSessionsKey = "ctrl+\\"
)

func (m Model) stepOutKey() string {
	if k := m.cfg.Console.StepOutKey; k != "" {
		return k
	}
	return defaultStepOutKey
}

func (m Model) sessionsKey() string {
	if k := m.cfg.Console.SessionsKey; k != "" {
		return k
	}
	return defaultSessionsKey
}

// consolePassthrough is what an UNFOCUSED docked console lets through to
// gg while its column has focus: moving focus away, quitting, and the
// repo-wide globals. Every other key is swallowed — the Commits panel it
// covers would otherwise act on a list the user cannot see (j/k, /, o,
// ctrl+w, ctrl+f, ctrl+r, space…).
var consolePassthrough = map[string]bool{
	"tab": true, "shift+tab": true, "left": true, "h": true, "ctrl+left": true, "ctrl+right": true,
	"q": true, "ctrl+c": true, "?": true, ".": true, "ctrl+p": true, "ctrl+o": true,
	"alt+a": true, "alt+t": true, "alt+w": true, "R": true, ",": true, "!": true, "E": true, "F": true, "r": true,
	"c": true, "C": true, "p": true, "P": true, "S": true, "u": true, "g": true, "G": true,
}

// consoleFullPassthrough is what an unfocused FULL-SCREEN console lets
// through: quitting, help, the shell escape and the cycle. It covers every
// panel and the parked view, so focus moves would land on hidden panels and
// an opener (F, S, c, p…) would open something behind it.
var consoleFullPassthrough = map[string]bool{
	"q": true, "ctrl+c": true, "?": true, "ctrl+o": true, "alt+a": true, "alt+t": true, "alt+w": true,
}

// updateConsoleKey routes a key to/around the console per the state table
// (spec). handled=false lets the key continue down gg's normal dispatch.
func (m Model) updateConsoleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	key := msg.String()
	if key == m.sessionsKey() && m.proc == nil {
		if _, open := m.topLayer().(*sessionsPopup); open {
			return m.popLayer(), nil, true // toggle: never stack a second popup
		}
		nm, cmd := m.openSessionsPopup(false)
		return nm, cmd, true
	}
	if m.console == nil {
		return m, nil, false
	}
	// Anything layered above the console (the sessions popup opened from it,
	// the . menu) owns the keyboard; closing it returns to the console.
	if m.topLayer() != nil || m.actionMenu != nil {
		return m, nil, false
	}
	// Focus left the console's column (a mouse click on another panel): the
	// keyboard is gg's again. A full-screen console hides every panel, so
	// the click cannot have meant one: focus snaps back.
	if m.focus != panelCommits && m.consoleFull() {
		m.focus = panelCommits
	}
	if m.console.focused && m.focus != panelCommits {
		m.console.focused = false
		if m.console.maximized {
			m.console.maximized = false
			m = m.syncConsoleSize()
		}
	}
	if m.console.scroll != nil && m.consoleOwnsKeys() {
		var cmd tea.Cmd
		var done bool
		if m, cmd, done = m.consoleScrollKey(msg); done {
			return m, cmd, true
		}
	}
	if m.console.scroll == nil && m.console.focused && key == "alt+pgup" {
		return m.enterConsoleScroll(), nil, true
	}
	if m.console.scroll == nil && !m.console.focused && m.consoleOwnsKeys() && key == "pgup" {
		return m.enterConsoleScroll(), nil, true
	}
	if m.console.focused {
		if key == "x" && m.consoleExited() {
			return m.removeConsoleSession(), nil, true
		}
		// alt+a / alt+t are gg's even here: the cycle starts from this
		// agent and comes back to the screen it was shown over.
		if key == "alt+a" || key == "alt+t" {
			nm, cmd := m.cycleSessions(key == "alt+t")
			return nm, cmd, true
		}
		if key == "alt+w" {
			nm, cmd := m.cycleWorktrees()
			return nm, cmd, true
		}
		if key == m.stepOutKey() {
			m.console.focused = false
			// Over a full-screen return point the console stays full-screen
			// (a second press or esc goes back); a ctrl+t-maximised docked
			// one docks again.
			if m.console.maximized && !m.consoleFull() {
				m.console.maximized = false
				m = m.syncConsoleSize()
			}
			return m, nil, true
		}
		if msg.Type == tea.KeyRunes {
			msg.Runes, m.console.highHalf = joinSurrogates(m.console.highHalf, msg.Runes)
			if len(msg.Runes) == 0 {
				return m, nil, true
			}
		} else {
			m.console.highHalf = 0
		}
		if s, ok := m.consoleSession(); ok && s.Info().State == domain.SessionRunning {
			in := encodeConsoleKey(msg)
			switch {
			case in.drop:
			case in.paste != "":
				s.Paste(in.paste)
			case in.text != "":
				s.SendText(in.text)
			case in.key != nil:
				s.SendKey(in.key)
			}
		}
		return m, nil, true // an exited console swallows keys; ctrl+] still steps out
	}
	// Unfocused: the console answers only while its column has focus — and a
	// files view's focused tree (a commit's or a review's files, which keep
	// focus on the Commits column) owns the keyboard, not the console.
	if !m.consoleFull() && (m.focus != panelCommits || (m.filesView != nil && m.filesTreeFocused)) {
		return m, nil, false // a full-screen console covers the tree: its keys are the console's
	}
	switch key {
	case "enter":
		m.console.focused = true
		m.touchConsole()
		return m.syncConsoleSize(), nil, true // gaining focus takes the size back
	case "ctrl+t":
		m.console.maximized, m.console.focused = true, true
		m.touchConsole()
		return m.syncConsoleSize(), nil, true
	case "esc", m.stepOutKey(): // the step-out key twice = out, then away
		return m.closeConsole(), nil, true
	case "X":
		// The session row's X (kill after a confirm, then remove; the list
		// change closes this console). On an exited agent it is x.
		if m.consoleExited() {
			return m.removeConsoleSession(), nil, true
		}
		if s, ok := m.consoleSession(); ok {
			return m.killRemoveSessionRow(s.Info()), nil, true
		}
	case "x":
		if m.consoleExited() {
			return m.removeConsoleSession(), nil, true
		}
	}
	if m.consoleFull() {
		return m, nil, !consoleFullPassthrough[key]
	}
	if consolePassthrough[key] {
		return m, nil, false
	}
	return m, nil, true
}

// consoleExited reports a docked console whose agent has exited.
func (m Model) consoleExited() bool {
	s, ok := m.consoleSession()
	return ok && s.Info().State == domain.SessionExited
}

// removeConsoleSession is an exited console's x: the session goes from the
// list (as the ctrl+\ popup's x) and the Commits column comes back.
func (m Model) removeConsoleSession() Model {
	if err := domain.Sessions().Remove(m.console.id); err != nil {
		m.statusMsg = i18n.T("only an exited session can be removed — kill it first (k)")
		return m
	}
	return m.closeConsole()
}

// touchConsole marks the docked console's session used — it just gained focus.
func (m Model) touchConsole() {
	if s, ok := m.consoleSession(); ok {
		s.Touch()
	}
}

// killSession signals a session's process group and says so at once; the
// exit notice follows when the process is gone.
func (m Model) killSession(id domain.SessionID) Model {
	s, ok := domain.Sessions().Get(id)
	if !ok || domain.Sessions().Kill(id) != nil {
		return m
	}
	info := s.Info()
	m.statusMsg = i18n.T("killing %s in %s…", info.Title(), shortWorktreeName(info.Dir))
	return m
}
