package tui

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
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
	cancel    func()          // drops it; every path that clears m.console goes through dropConsole
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

// showConsole docks session id in the Commits column, focused or not (alt+a /
// alt+t show one unfocused). Only a focused show is a use of the session
// (Touch): cycling through them must not reorder the list it walks.
func (m Model) showConsole(id domain.SessionID, focused bool) (Model, tea.Cmd) {
	s, ok := domain.Sessions().Get(id)
	if !ok {
		m.statusMsg = i18n.T("that agent session is gone")
		return m, nil
	}
	gen := 1
	if m.console != nil {
		gen = m.console.gen + 1
	}
	if m.focus != panelCommits {
		m = m.rememberLeftFocus()
	}
	m.stashView = nil
	m.filesPreview = nil
	// A fullscreen left panel would hide the column the console docks in
	// (and shrink its PTY to nothing); showing the agent is what was asked
	// for, so the pin drops. Unlike the stash list, this is a clear, not a
	// suspend — the console is a peer of the commit list, not a surface the
	// pin yields to (fullscreenYielded).
	m.fullMaxed = false
	m = m.dropConsole()
	if focused {
		s.Touch()
	}
	screen, cancel := s.Subscribe()
	m.console = &consoleState{id: id, focused: focused, gen: gen, screen: screen, cancel: cancel}
	m.focus = panelCommits
	m = m.syncConsoleSizeIfFocused()
	return m, waitSessionCmd(m.console, id, gen)
}

// dropConsole clears the console and its screen subscription. Every place
// that sets m.console = nil goes through it, so a closed console never
// leaves a subscriber behind on its session.
func (m Model) dropConsole() Model {
	if m.console != nil && m.console.cancel != nil {
		m.console.cancel()
	}
	m.console = nil
	return m
}

// closeConsole hides the console; the session keeps running.
func (m Model) closeConsole() Model {
	m = m.dropConsole()
	m.focus = m.lastLeftPanel
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

// consoleTitle is the box title: label · worktree · state.
func consoleTitle(info domain.SessionInfo, focused bool) string {
	state := i18n.T("running %s", formatElapsed(time.Since(info.Started)))
	if info.State == domain.SessionExited {
		state = i18n.T("exited (%d)", info.ExitCode)
	} else if act := sessionActivityText(info.ID); act != "" {
		state += " · " + act
	}
	t := info.Label + " · " + shortWorktreeName(info.Dir) + " · " + state
	if !focused {
		t += "  " + i18n.T("[enter] type  [ctrl+t] maximise  [esc] close")
	}
	return t
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
		lines = append(lines, padRight(truncate(consoleTitle(info, m.console.focused), innerW), innerW))
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
			lines = append(lines, padRight(l, innerW))
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
			if m.console.maximized {
				m.console.maximized = false
				m = m.syncConsoleSize()
			}
		}
		m.statusMsg = i18n.T("%s in %s exited (%d)", info.Label, shortWorktreeName(info.Dir), info.ExitCode)
	}
	m.sessionStates = next
	if m.console != nil {
		if _, ok := next[m.console.id]; !ok {
			// Its session was removed: give the Commits column back rather
			// than dock a box with nothing in it. Focus moves only when the
			// console held it.
			if m.console.focused {
				m = m.closeConsole()
			} else {
				m = m.dropConsole()
				m = m.reconcileFullscreenFocus()
			}
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

// sessionsByLastUsed is the running sessions of one kind (terminals or
// agents), most recently used first.
func sessionsByLastUsed(list []domain.SessionInfo, terminal bool) []domain.SessionInfo {
	var out []domain.SessionInfo
	for _, info := range list {
		if info.State == domain.SessionRunning && info.Terminal == terminal {
			out = append(out, info)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastUsed.After(out[j].LastUsed) })
	return out
}

// cycleSessions is alt+a (agents) / alt+t (terminals): show this
// repository's most recently used session of that kind, unfocused; pressed
// again while one of them is shown unfocused, the next one back in last-used
// order (wrapping). enter then focuses it, which makes it the most recent.
func (m Model) cycleSessions(terminal bool) (Model, tea.Cmd) {
	list := sessionsByLastUsed(m.repoSessions(domain.Sessions().List()), terminal)
	if len(list) == 0 {
		if terminal {
			m.statusMsg = i18n.T("no running terminal in this repository — open one from the . menu of a worktree or a checked-out branch")
		} else {
			m.statusMsg = i18n.T("no running agent session in this repository — start one from the . menu of a worktree or a checked-out branch")
		}
		return m, nil
	}
	next := 0
	if m.console != nil && !m.console.focused {
		for i, info := range list {
			if info.ID == m.console.id {
				next = (i + 1) % len(list)
				break
			}
		}
	}
	info := list[next]
	m, cmd := m.showConsole(info.ID, false)
	m.statusMsg = i18n.T("%s in %s — %d of %d by last use  [enter] focus", info.Label, shortWorktreeName(info.Dir), next+1, len(list))
	return m, cmd
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
	"alt+a": true, "alt+t": true, "R": true, ",": true, "!": true, "E": true, "F": true, "r": true,
	"c": true, "C": true, "p": true, "P": true, "S": true, "u": true, "g": true, "G": true,
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
	// keyboard is gg's again.
	if m.console.focused && m.focus != panelCommits {
		m.console.focused = false
		if m.console.maximized {
			m.console.maximized = false
			m = m.syncConsoleSize()
		}
	}
	if m.console.focused {
		if key == m.stepOutKey() {
			m.console.focused = false
			if m.console.maximized {
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
	if m.focus != panelCommits || (m.filesView != nil && m.filesTreeFocused) {
		return m, nil, false
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
	}
	if consolePassthrough[key] {
		return m, nil, false
	}
	return m, nil, true
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
	m.statusMsg = i18n.T("killing %s in %s…", info.Label, shortWorktreeName(info.Dir))
	return m
}
