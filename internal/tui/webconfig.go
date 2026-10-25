package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
)

// The hosted page wrote config (its External tools wizard added blocks, or
// its Repo settings location moved the per-repo file): the terminal re-reads
// what it caches, the way its own Settings paths do — or its wizard would
// append the page's blocks again, and after a page move-to-private its next
// Settings write would recreate the moved .gg.toml.

// webConfigChangedMsg is the page's config write; relocated = the per-repo
// file moved (the write target rebinds), else tool blocks were added.
type webConfigChangedMsg struct {
	relocated bool
}

// configChangedFor is the function the host calls after a page config write.
// It never blocks the page's request: a full buffer already carries a
// reload, and a closed page drops it.
func configChangedFor(w *webHostState) func(relocated bool) {
	configs, stop := w.configs, w.stop
	return func(relocated bool) {
		select {
		case <-stop:
			return
		default:
		}
		select {
		case configs <- webConfigChangedMsg{relocated: relocated}:
		default:
		}
	}
}

// waitWebConfigCmd waits for the page's next config write; nil once the page
// is closed.
func waitWebConfigCmd(w *webHostState) tea.Cmd {
	configs, stop := w.configs, w.stop
	return func() tea.Msg {
		select {
		case msg := <-configs:
			return msg
		case <-stop:
			return nil
		}
	}
}

// onWebConfigChanged is repoConfigPopup.run's tail (a relocation) or the
// wizard's reload (tools), then the next wait.
func (m Model) onWebConfigChanged(msg webConfigChangedMsg) (Model, tea.Cmd) {
	var rearm tea.Cmd
	if m.web != nil {
		rearm = waitWebConfigCmd(m.web)
	}
	if !msg.relocated {
		return m.reloadToolConfig(), rearm
	}
	if m.ws == nil || m.liveRepo() == m.settingsRepo() {
		committed, private := "", ""
		if m.currentWorktree != "" {
			committed = filepath.Join(m.currentWorktree, ".gg.toml")
		}
		if len(m.worktrees) > 0 && m.worktrees[0].Path != "" {
			private = config.PrivateRepoPath(m.worktrees[0].Path)
		}
		m.repoConfigPath = config.ActiveRepoConfigPath(committed, private)
	}
	if m.ws != nil {
		return m.reloadOwnConfig(), rearm
	}
	m.loadGen++
	return m, tea.Batch(m.loadCmd(), rearm)
}
