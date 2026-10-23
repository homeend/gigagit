package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gitwatch"
)

// A sleeping member keeps its own repository-level watcher (spec §6): its
// Branches and Worktrees rows in the grouped tabs stay current while
// another member is on screen. The live member is watched by the ordinary
// watcher (watch.go) through the kick, so a member's own watcher is closed
// the moment it becomes live and started again when it goes to sleep. A
// member whose git dir cannot be watched (WSL2 9p, a failed build) polls
// its two lists on memberPollInterval instead. Every message is addressed
// by the member's service and its own watch generation, which moves on
// whenever its watcher closes, so nothing stale lands.

// memberPollInterval is the fallback cadence for an unwatchable member.
const memberPollInterval = 30 * time.Second

// memberWatchReadyMsg is a member's watcher built off-thread (nil = none:
// unsupported or a failed build → polling).
type memberWatchReadyMsg struct {
	svc       *domain.Service
	gen       int
	watcher   *gitwatch.Watcher
	supported bool
}

// memberWatchEventMsg is one debounced change in a member's git dir.
type memberWatchEventMsg struct {
	svc    *domain.Service
	gen    int
	source sourceKey
}

// memberWatchClosedMsg: the member's watcher channel closed.
type memberWatchClosedMsg struct {
	svc *domain.Service
	gen int
}

// memberPollTickMsg is the polling fallback's beat.
type memberPollTickMsg struct {
	svc *domain.Service
	gen int
}

// closeWatch stops a member's watcher or poll and moves its generation on.
func (r *repoView) closeWatch() {
	if r.watcher != nil {
		_ = r.watcher.Close()
		r.watcher = nil
	}
	r.watchGen++
	r.polling = false
	r.watchStarting = false
}

// memberWatchStarts builds a watcher for every loaded, sleeping member that
// has none yet (the Update tail runs it on memberWatchKick: after a member
// load, after a switch put a member to sleep). Idempotent: a member whose
// build is in flight is skipped.
func (m Model) memberWatchStarts() tea.Cmd {
	if m.ws == nil || m.quiet {
		return nil
	}
	var cmds []tea.Cmd
	for _, r := range m.ws.members {
		if r == m.liveRepo() || !r.loaded || r.svc == nil || r.watcher != nil || r.polling || r.watchStarting {
			continue
		}
		r.watchStarting = true
		cmds = append(cmds, startMemberWatchCmd(r))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// startMemberWatchCmd builds member r's watcher off-thread: its common
// git dir, the Branches and Worktrees sources only (the two lists its
// group shows). Always answers with a memberWatchReadyMsg.
func startMemberWatchCmd(r *repoView) tea.Cmd {
	svc, gen := r.svc, r.watchGen
	return func() tea.Msg {
		ctx := context.Background()
		common, err := svc.GitCommonDir(ctx)
		if err != nil || common == "" {
			return memberWatchReadyMsg{svc: svc, gen: gen}
		}
		if !gitwatch.Supported(common) {
			return memberWatchReadyMsg{svc: svc, gen: gen}
		}
		w, werr := gitwatch.New(gitwatch.Plan(common, common, []gitwatch.Source{gitwatch.Branches, gitwatch.Worktrees}), watchDebounce)
		if werr != nil {
			return memberWatchReadyMsg{svc: svc, gen: gen, supported: true}
		}
		return memberWatchReadyMsg{svc: svc, gen: gen, watcher: w, supported: true}
	}
}

// memberWatchListenCmd waits for the member's next event.
func memberWatchListenCmd(svc *domain.Service, w *gitwatch.Watcher, gen int) tea.Cmd {
	return func() tea.Msg {
		s, ok := <-w.Events()
		if !ok {
			return memberWatchClosedMsg{svc: svc, gen: gen}
		}
		return memberWatchEventMsg{svc: svc, gen: gen, source: watchSourceKey(s)}
	}
}

// memberPollCmd is the polling fallback's next beat (m.tick: virtual under
// the headless driver).
func (m Model) memberPollCmd(svc *domain.Service, gen int) tea.Cmd {
	return m.tick(memberPollInterval, func(time.Time) tea.Msg { return memberPollTickMsg{svc: svc, gen: gen} })
}

// onMemberWatchReady lands a member's watcher: discarded when the build is
// stale or the member went live meanwhile (the live member is the ordinary
// watcher's); kept and listened to otherwise; none → polling.
func (m Model) onMemberWatchReady(msg memberWatchReadyMsg) (Model, tea.Cmd) {
	r := m.memberBySvc(msg.svc)
	if r == nil || msg.gen != r.watchGen || r == m.liveRepo() {
		if msg.watcher != nil {
			_ = msg.watcher.Close()
		}
		if r != nil && msg.gen == r.watchGen {
			r.watchStarting = false
		}
		return m, nil
	}
	r.watchStarting = false
	if msg.watcher == nil {
		r.polling = true
		return m, m.memberPollCmd(r.svc, r.watchGen)
	}
	r.watcher = msg.watcher
	return m, memberWatchListenCmd(r.svc, r.watcher, r.watchGen)
}

// onMemberWatchEvent re-reads the member (memberLoadCmd: both lists) and
// re-arms the listener. A stale event, or one for a member that is live
// now, is dropped.
func (m Model) onMemberWatchEvent(msg memberWatchEventMsg) (Model, tea.Cmd) {
	r := m.memberBySvc(msg.svc)
	if r == nil || msg.gen != r.watchGen || r.watcher == nil || r == m.liveRepo() {
		return m, nil
	}
	return m, tea.Batch(memberLoadCmd(r), memberWatchListenCmd(r.svc, r.watcher, r.watchGen))
}

// onMemberPollTick is the fallback's read, while the member still polls.
func (m Model) onMemberPollTick(msg memberPollTickMsg) (Model, tea.Cmd) {
	r := m.memberBySvc(msg.svc)
	if r == nil || msg.gen != r.watchGen || !r.polling || r == m.liveRepo() {
		return m, nil
	}
	return m, tea.Batch(memberLoadCmd(r), m.memberPollCmd(r.svc, r.watchGen))
}
