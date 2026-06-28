package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// remoteTagsMsg carries the result of a remote-tag lookup. manual=true means a
// user-initiated refresh (errors go to the status line); false means a silent
// background poll (errors discarded — see queryQuiet's no-record contract).
// dur is the wall-clock time of the lookup, used by the background lane for
// rolling duration stats (informational only, never affects scheduling).
type remoteTagsMsg struct {
	names  map[string]bool
	err    error
	dur    time.Duration
	manual bool
}

// remoteTagsCmd runs the (network) remote-tag lookup off the UI thread. Shared by
// the manual .-menu action and the background scheduler lane.
func (m Model) remoteTagsCmd(ctx context.Context, manual bool) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		start := time.Now()
		names, err := svc.RemoteTags(ctx)
		return remoteTagsMsg{names: names, err: err, dur: time.Since(start), manual: manual}
	}
}

// autoRemoteTagsEnabled reports whether a tag-window change should auto-trigger
// a background remote-tag lookup (default on; inverted config flag).
func (m Model) autoRemoteTagsEnabled() bool {
	return !m.cfg.Refresh.DisableRemoteTagsAuto
}

// applyPendingRemoteTag folds a pending optimistic add/remove into the set on op
// success, then clears the pending fields. Lazy-inits the map for an add.
func (m Model) applyPendingRemoteTag() Model {
	if m.pendingRemoteTagSet != "" {
		if m.remoteTagNames == nil {
			m.remoteTagNames = map[string]bool{}
		}
		m.remoteTagNames[m.pendingRemoteTagSet] = true
		m.pendingRemoteTagSet = ""
	}
	if m.pendingRemoteTagUnset != "" {
		delete(m.remoteTagNames, m.pendingRemoteTagUnset)
		m.pendingRemoteTagUnset = ""
	}
	return m
}
