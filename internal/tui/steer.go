package tui

import (
	"os"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/steer"
)

// steerStartedMsg carries a freshly opened inbox watcher back onto the Update
// goroutine; gen drops one that a repo switch superseded.
type steerStartedMsg struct {
	gen int
	w   *steer.Watcher
}

// steerWakeMsg says a command file landed in the inbox.
type steerWakeMsg struct{ gen int }

// steerActive reports whether this session participates in live steering: it
// needs a resolved inbox AND the [ui] agent_steering key left on. With it off
// the TUI writes no presence, so `gg session` reports "no gg session" and
// nothing here ever runs.
func (m Model) steerActive() bool {
	return m.steerDir != "" && m.cfg.UI.SteeringOn()
}

// steerDirFor resolves this session's inbox from the identity the snapshot
// already resolved. "" means steering is off for this session.
func steerDirFor(commonDir, worktree string) string {
	return config.SessionSteerDir(commonDir, worktree)
}

// initSteerInbox claims the inbox for this session: write the presence file,
// then DISCARD every command and reply already in it. Those belong to a
// session that crashed — replaying them would move a window the agent asked
// about minutes or days ago.
func (m Model) initSteerInbox() Model {
	if !m.steerActive() {
		return m
	}
	steer.Discard(m.steerDir)
	_ = steer.Touch(m.steerDir, steer.TUIPresence, steer.Presence{
		PID:      os.Getpid(),
		Worktree: m.snapshotWorktree,
		Started:  time.Now().UTC().Format(time.RFC3339),
	})
	m.steerClaimed = true
	return m
}

// reconcileSteer re-syncs the inbox with a config that just landed, in BOTH
// directions. A repo switch resolves the new inbox (snapshotTargetMsg) before
// the new repo's config arrives, so a switch INTO a steering-on repo reaches
// this point with a resolved-but-unclaimed dir: without the second arm that
// session would never sweep the new inbox (a crashed session's commands would
// replay on the first heartbeat drain) and would run watcher-less for its whole
// life. The returned cmd starts the watcher and must be batched by the caller.
func (m Model) reconcileSteer() (Model, tea.Cmd) {
	switch {
	case !m.steerActive():
		return m.closeSteerInbox(), nil // config says off: drop the presence at once
	case !m.steerClaimed:
		m = m.initSteerInbox()
		return m, m.startSteerCmd(m.steerGen)
	}
	return m, nil
}

// closeSteerInbox ends this session's claim: the presence goes away at once
// (rather than aging out over five seconds) and the watcher is stopped.
// Called on clean exit and on every repo switch.
func (m Model) closeSteerInbox() Model {
	if m.steerDir != "" {
		steer.Remove(m.steerDir, steer.TUIPresence)
	}
	if m.steerWatch != nil {
		m.steerWatch.Close()
		m.steerWatch = nil
	}
	m.steerDir = ""
	m.steerClaimed = false
	return m
}

// startSteerCmd opens the inbox watcher off-thread. A failure is silent: the
// heartbeat poll already drains the inbox once a second, so a missing watcher
// costs latency, not the feature.
func (m Model) startSteerCmd(gen int) tea.Cmd {
	if !m.steerActive() {
		return nil
	}
	dir := m.steerDir
	return func() tea.Msg {
		w, err := steer.Watch(dir)
		if err != nil {
			return nil
		}
		return steerStartedMsg{gen: gen, w: w}
	}
}

// steerListenCmd blocks on the watcher until a command lands, then hands the
// Update goroutine a wake. A closed channel ends the loop by returning nil.
func steerListenCmd(w *steer.Watcher, gen int) tea.Cmd {
	if w == nil {
		return nil
	}
	return func() tea.Msg {
		if _, ok := <-w.Events(); !ok {
			return nil
		}
		return steerWakeMsg{gen: gen}
	}
}

// touchSteerPresence refreshes this session's mtime. Liveness IS the mtime, so
// this one syscall per second is the whole "is a TUI running" protocol.
func (m Model) touchSteerPresence() {
	if !m.steerActive() {
		return
	}
	_ = steer.Touch(m.steerDir, steer.TUIPresence, steer.Presence{
		PID:      os.Getpid(),
		Worktree: m.snapshotWorktree,
		Started:  time.Now().UTC().Format(time.RFC3339),
	})
}

// drainSteer applies every command waiting in the inbox, in post order, and
// batches the replies. It runs on the Update goroutine (so it may touch the
// Model freely); only the reply writes go off-thread.
func (m Model) drainSteer() (Model, tea.Cmd) {
	if !m.steerActive() {
		return m, nil
	}
	cmds := make([]tea.Cmd, 0, 4)
	// An expired pending is answered FIRST: a navigate that arrived in the same
	// tick must find the slot free rather than be refused by a corpse.
	var exp tea.Cmd
	m, exp = m.expirePendingSteer(time.Now())
	if exp != nil {
		cmds = append(cmds, exp)
	}
	for _, c := range steer.Drain(m.steerDir) {
		var cmd tea.Cmd
		m, cmd = m.applySteer(c)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return m, nil
	}
	return m, tea.Batch(cmds...)
}

// steerRefusal reports, in English protocol prose, why the model cannot act on
// a steering command right now — or "" when it can. Two principles: never
// answer a decision on the user's behalf, and never throw away what the user is
// in the middle of. The layer whitelist is the same idea as paletteReachable's:
// a diff, history, blame or plain content popup is something the pipeline pops
// to reach the panels (a content popup only when its own filter is not being
// typed into); anything else owns the keyboard and its own text state.
func (m Model) steerRefusal() string {
	switch {
	case !m.opsIdle():
		return "an operation is running"
	case m.modal != nil:
		return "a decision is waiting for the user"
	case m.proc != nil:
		return "an interactive process owns the screen"
	case m.actionMenu != nil:
		return "the action menu is open"
	case m.filterTyping, m.highlightTyping, m.recallOpen:
		return "the user is typing"
	case m.filesView != nil && m.filesView.typing:
		return "the user is typing"
	case m.filesPreview != nil && m.filesPreview.typing:
		return "the user is typing"
	case m.stashView != nil && m.stashView.typing:
		return "the user is typing"
	}
	switch l := m.topLayer().(type) {
	case nil, *diffView, *historyView, *blameView:
		return ""
	case *contentPopup:
		// A content popup is poppable — unless its own / filter has focus, in
		// which case popping it would throw away what the user is typing.
		if l.typing {
			return "the user is typing"
		}
		return ""
	}
	return "a window is open that owns the keyboard"
}

// steerOK / steerFail build the two reply shapes. Detail and Error are English
// protocol prose an agent parses — never i18n keys.
func steerOK(c steer.Command, detail string) steer.Reply {
	return steer.Reply{ID: c.ID, OK: true, Detail: detail}
}

func steerFail(c steer.Command, reason string) steer.Reply {
	return steer.Reply{ID: c.ID, OK: false, Error: reason}
}

// answerSteer writes a reply off-thread. A command posted with wait:false gets
// none — nothing would ever read it, and the file would only have to be swept.
func (m Model) answerSteer(c steer.Command, r steer.Reply) tea.Cmd {
	if !c.Wait || m.steerDir == "" {
		return nil
	}
	dir := m.steerDir
	return func() tea.Msg {
		_ = steer.PostReply(dir, r)
		return nil
	}
}

// applySteer runs one command. Refusals are checked once, before dispatch, so
// every verb inherits them.
func (m Model) applySteer(c steer.Command) (Model, tea.Cmd) {
	if why := m.steerRefusal(); why != "" {
		return m, m.answerSteer(c, steerFail(c, why))
	}
	// Only navigate parks a pendingSteer, and only one can be in flight: a
	// second would either overwrite the first (leaving its CLI to hang out its
	// wait) or land on the other's diff. reload/focus/highlight park nothing —
	// notably the automatic `reload notes` a concurrent `gg note add` posts must
	// still apply while a navigate is loading.
	if c.Cmd == "navigate" && m.pendingSteer != nil {
		return m, m.answerSteer(c, steerFail(c, "a previous navigate is still loading"))
	}
	switch c.Cmd {
	case "navigate":
		return m.steerNavigate(c)
	case "focus":
		return m.steerFocus(c)
	case "reload":
		return m.steerReload(c)
	case "highlight":
		return m.steerHighlight(c)
	case "highlight_clear":
		return m.steerHighlightClear(c)
	default:
		return m, m.answerSteer(c, steerFail(c, "unknown command "+strconv.Quote(c.Cmd)))
	}
}
