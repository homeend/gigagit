package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// remoteTagsMsg carries the result of a remote-tag lookup. manual=true means a
// user-initiated refresh (errors go to the status line); false means a silent
// background poll (errors discarded — see queryQuiet's no-record contract).
// NOTE: a dur time.Duration field will be added in Task 5 for background
// duration recording. The remoteTagsMsg handler in model.go will also be
// extended in Task 5 with recordDuration and the bgActiveItem.isRemoteTags
// lane-free branch.
type remoteTagsMsg struct {
	names  map[string]bool
	err    error
	manual bool
}

// remoteTagsCmd runs the (network) remote-tag lookup off the UI thread. Shared by
// the manual .-menu action and the background scheduler lane.
func (m Model) remoteTagsCmd(ctx context.Context, manual bool) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		names, err := svc.RemoteTags(ctx)
		return remoteTagsMsg{names: names, err: err, manual: manual}
	}
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
