package tui

import (
	"os"
	"sort"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// A session gg starts is handed GG_INBOX = the steer inbox gg had then. After
// a switch to another worktree that inbox is no longer gg's current one, yet
// the child still talks to it — so gg keeps a presence there, drains it, and
// answers each command where it came from (Command.From), until the last
// running child holding it ends. Commands act on gg as it is; a worktree-bound
// one from elsewhere raises the switch notice (steer_switch_ask.go).

// keptInboxes are the distinct inboxes running children hold, minus gg's
// current one (which initSteerInbox already claims), sorted.
func (m Model) keptInboxes() []string {
	seen := map[string]bool{}
	for id, dir := range m.childInbox {
		if dir == "" || dir == m.steerDir {
			continue
		}
		if s, ok := domain.Sessions().Get(id); ok && s.Info().State == domain.SessionRunning {
			seen[dir] = true
		}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// holdsInbox reports whether a running child still talks to dir.
func (m Model) holdsInbox(dir string) bool {
	for id, d := range m.childInbox {
		if d != dir {
			continue
		}
		if s, ok := domain.Sessions().Get(id); ok && s.Info().State == domain.SessionRunning {
			return true
		}
	}
	return false
}

// tendKeptInboxes runs on the heartbeat: forget children that are gone,
// refresh the presence of every kept inbox gg may claim (never one another
// live gg owns), and release the ones no running child holds any more.
func (m Model) tendKeptInboxes() Model {
	for id := range m.childInbox {
		if s, ok := domain.Sessions().Get(id); !ok || s.Info().State != domain.SessionRunning {
			delete(m.childInbox, id)
		}
	}
	kept := map[string]bool{}
	for _, dir := range m.keptInboxes() {
		if p, live := steer.Live(dir, steer.TUIPresence); live && p.PID != os.Getpid() {
			continue // another gg shows that worktree: its inbox, its commands
		}
		_ = steer.Touch(dir, steer.TUIPresence, steer.Presence{
			PID:      os.Getpid(),
			Worktree: m.snapshotWorktree,
			Started:  time.Now().UTC().Format(time.RFC3339),
		})
		kept[dir] = true
	}
	for dir := range m.keptSteer {
		if kept[dir] {
			continue
		}
		if dir != m.steerDir {
			releaseOwnPresence(dir)
		}
		delete(m.keptSteer, dir)
	}
	for dir := range kept {
		m.keptSteer[dir] = true
	}
	return m
}

// releaseKeptInboxes drops every kept presence (clean exit).
func (m Model) releaseKeptInboxes() Model {
	for dir := range m.keptSteer {
		releaseOwnPresence(dir)
		delete(m.keptSteer, dir)
	}
	return m
}

// releaseOwnPresence removes dir's TUI presence only when it is this
// process's — a second gg may have claimed the inbox meanwhile.
func releaseOwnPresence(dir string) {
	if p, live := steer.Live(dir, steer.TUIPresence); !live || p.PID == os.Getpid() {
		steer.Remove(dir, steer.TUIPresence)
	}
}
