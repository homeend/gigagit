package tui

import (
	"os"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/model"
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
	// The remove is not redundant: Touch only Chtimes a file that already
	// exists, so a crashed or SIGKILLed session's tui.json would keep ITS pid
	// and start time while this session refreshed the mtime — and
	// `gg session status` would print the dead session's numbers as ours.
	steer.Remove(m.steerDir, steer.TUIPresence)
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
	switch {
	case m.steerDir == "":
	case m.holdsInbox(m.steerDir):
		// A running child was handed this inbox as GG_INBOX: keep answering it
		// (steer_kept.go) instead of vanishing under the agent.
		m.keptSteer[m.steerDir] = true
	default:
		steer.Remove(m.steerDir, steer.TUIPresence)
	}
	if m.steerWatch != nil {
		m.steerWatch.Close()
		m.steerWatch = nil
	}
	m.steerDir = ""
	m.steerClaimed = false
	// A parked navigate must go with the claim. drainSteer — the only thing
	// that expires one — is gated on steerActive(), so a pending left here (the
	// config-turned-off path; reRoot clears its own) would sit forever and then
	// fire on the next ORDINARY status refresh, moving the user's view with no
	// reply possible and nobody to send one to.
	m.pendingSteer = nil
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
	var hexp tea.Cmd
	m, hexp = m.expirePendingHint(time.Now())
	if hexp != nil {
		cmds = append(cmds, hexp)
	}
	dirs := []string{m.steerDir}
	for dir := range m.keptSteer {
		if dir != m.steerDir {
			dirs = append(dirs, dir)
		}
	}
	for _, dir := range dirs {
		for _, c := range steer.Drain(dir) {
			var cmd tea.Cmd
			m, cmd = m.applySteer(c)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
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
	case m.filesPreview != nil && m.filesPreview.p.typing:
		return "the user is typing"
	case m.stashView != nil && m.stashView.typing:
		return "the user is typing"
	}
	switch l := m.topLayer().(type) {
	case nil, *diffView, *historyView, *blameView:
		return ""
	case *fileViewer:
		// Poppable, unless its in-view search is being typed.
		if l.p.search.typing {
			return "the user is typing"
		}
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

// startAtFailMsg carries a --at startup navigate's refusal back to the UI
// thread. The startup command posts no reply file (nobody is waiting for
// one), so without this the user would watch nothing happen and be told
// nothing.
type startAtFailMsg struct{ reason string }

// answerSteer writes a reply off-thread. A command posted with wait:false gets
// none — nothing would ever read it, and the file would only have to be swept.
func (m Model) answerSteer(c steer.Command, r steer.Reply) tea.Cmd {
	if c.ID == "" {
		// Only a LOCAL navigate has no id — the `--at` startup landing and the
		// # prompt's pasted link: steer.Post fills one in, and Drain discards
		// any command that arrived without one. Their refusals have no CLI to
		// print them, so they go to the status bar.
		if !r.OK {
			reason := r.Error
			return func() tea.Msg { return startAtFailMsg{reason: reason} }
		}
		return nil
	}
	dir := m.steerDir
	if c.From != "" {
		dir = c.From // a kept inbox (steer_kept.go): answer where the sender waits
	}
	if !c.Wait || dir == "" {
		return nil
	}
	return func() tea.Msg {
		_ = steer.PostReply(dir, r)
		return nil
	}
}

// steerEnumRefusal validates the two wire enums that more than one verb reads,
// mirroring the web endpoint's toSteerWire so the same command is answered the
// same way whichever consumer picks it up. "" is the documented default on
// both fields (unstaged / new) and stays accepted; anything else must be
// refused rather than silently defaulted — an unrecognised target.state keys a
// band no open diff can ever match (answered ok:true for a band that will never
// paint) and reads as "not staged" in navigate, and an unrecognised side
// silently means "new". The prose is English protocol, like every reply.
func steerEnumRefusal(c steer.Command) string {
	if c.Target != nil {
		switch c.Target.State {
		case "", "unstaged", "staged", "untracked", "commit":
		case "preview":
			// A preview names a branch PAIR, and both halves are load-bearing: a
			// half-filled target would silently degrade into "some preview".
			if c.Target.Source == "" || c.Target.Target == "" {
				return "a preview target needs source and target"
			}
			// A preview is not a commit: mirrors the web endpoint's
			// toSteerWire so the same command is refused the same way
			// whichever consumer picks it up.
			if c.Commit != "" {
				return "a preview target cannot also carry a commit"
			}
		case "ref":
			// The NAME is load-bearing: steerNavigateRef resolves it at apply
			// time (ruling R2), so an empty one has nothing to resolve.
			if c.Target.Ref == "" {
				return "a ref target needs ref"
			}
		case "pair":
			// Both halves are load-bearing: a half-filled pair would silently
			// degrade into "some commit", exactly the preview target's rule.
			if c.Target.A == "" || c.Target.B == "" {
				return "a pair target needs a and b"
			}
		default:
			return "unknown target state " + strconv.Quote(c.Target.State)
		}
	}
	if c.Line != nil {
		switch c.Line.Side {
		case "", "new", "old":
		default:
			return "unknown side " + strconv.Quote(c.Line.Side)
		}
	}
	// The hint's closed set (model.LinkHint's grammar already closed it:
	// model.LinkHintKindOK is the one definition) mirrors the web endpoint's toSteerWire
	// so the same command is refused the same way whichever consumer picks
	// it up (S13 point 2).
	if c.HintKind != "" {
		if !model.LinkHintKindOK(c.HintKind) {
			return "unknown hint kind " + strconv.Quote(c.HintKind)
		}
		if c.HintID == "" {
			return "a hint needs an id"
		}
	}
	return ""
}

// applySteer runs one command. Refusals are checked once, before dispatch, so
// every verb inherits them.
func (m Model) applySteer(c steer.Command) (Model, tea.Cmd) {
	if why := m.steerRefusal(); why != "" {
		return m, m.answerSteer(c, steerFail(c, why))
	}
	if why := steerEnumRefusal(c); why != "" {
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
