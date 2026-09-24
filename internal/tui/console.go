package tui

import (
	"path/filepath"
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
	m.console = &consoleState{id: id, focused: true, gen: gen}
	m.focus = panelCommits
	m = m.syncConsoleSize()
	return m, waitSessionCmd(s, id, gen)
}

// closeConsole hides the console; the session keeps running.
func (m Model) closeConsole() Model {
	m.console = nil
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

// waitSessionCmd blocks until session s changes, then waits out the repaint
// spacing (absorbing further changes) before asking for a frame.
func waitSessionCmd(s *domain.AgentSession, id domain.SessionID, gen int) tea.Cmd {
	return func() tea.Msg {
		<-s.Changed()
		time.Sleep(consoleRepaint)
		return consoleChangedMsg{id: id, gen: gen}
	}
}

// waitSessionsCmd reports list-level changes (start, exit, remove).
func waitSessionsCmd() tea.Cmd {
	ch := domain.Sessions().Changed()
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
	return m, waitSessionsCmd()
}

// shortWorktreeName is the worktree's directory name, for titles and rows.
func shortWorktreeName(path string) string { return filepath.Base(path) }

// Default reserved keys; Task 9's [console] config overrides them.
const (
	defaultStepOutKey  = "ctrl+]"
	defaultSessionsKey = "ctrl+\\"
)

func (m Model) stepOutKey() string  { return defaultStepOutKey }
func (m Model) sessionsKey() string { return defaultSessionsKey }

// consolePassthrough is what an UNFOCUSED docked console lets through to
// gg while its column has focus: moving focus away, quitting, and the
// repo-wide globals. Every other key is swallowed — the Commits panel it
// covers would otherwise act on a list the user cannot see (j/k, /, o,
// ctrl+w, ctrl+f, ctrl+r, space…).
var consolePassthrough = map[string]bool{
	"tab": true, "shift+tab": true, "left": true, "h": true, "ctrl+left": true, "ctrl+right": true,
	"q": true, "ctrl+c": true, "?": true, ".": true, "ctrl+p": true, "ctrl+o": true,
	"R": true, ",": true, "!": true, "E": true, "F": true, "r": true,
	"c": true, "C": true, "p": true, "P": true, "S": true, "u": true, "g": true, "G": true,
}

// updateConsoleKey routes a key to/around the console per the state table
// (spec). handled=false lets the key continue down gg's normal dispatch.
func (m Model) updateConsoleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	key := msg.String()
	if key == m.sessionsKey() && m.proc == nil {
		nm, cmd := m.openSessionsPopup(false)
		return nm, cmd, true
	}
	if m.console == nil {
		return m, nil, false
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
	// Unfocused: the console answers only while its column has focus and
	// nothing is layered above it.
	if m.focus != panelCommits || m.topLayer() != nil || m.actionMenu != nil {
		return m, nil, false
	}
	switch key {
	case "enter":
		m.console.focused = true
		return m, nil, true
	case "ctrl+t":
		m.console.maximized, m.console.focused = true, true
		return m.syncConsoleSize(), nil, true
	case "esc":
		return m.closeConsole(), nil, true
	}
	if consolePassthrough[key] {
		return m, nil, false
	}
	return m, nil, true
}

// openSessionsPopup opens the ctrl+\ sessions popup (Task 7).
func (m Model) openSessionsPopup(quitMode bool) (Model, tea.Cmd) {
	_ = quitMode
	m.statusMsg = i18n.T("no agent sessions — start one from a worktree's . menu")
	return m, nil
}
