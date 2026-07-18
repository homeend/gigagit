package tui

import (
	"context"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/repos"
)

// Run launches the TUI for svc, taking over the alternate screen until the
// user quits. It returns the directory the shell should switch to (the worktree
// the user switched into during the session, or "" if none) so a wrapper can
// cd there on exit.
func Run(svc *domain.Service) (string, error) {
	m := New(svc)
	m.statePath = repos.DefaultStatePath()
	if home, err := os.UserHomeDir(); err == nil {
		m.initHomeDir = home
	}
	// Honor [debug] log_operations at startup. Wired here — the real entry point
	// that unit tests bypass (like statePath above) — rather than in loadCmd, so
	// enabling the operation log never performs a global SetSpanSink side effect
	// during a test's model load. The , Settings toggle drives it thereafter.
	cfg := config.Defaults()
	if top, err := svc.TopLevel(context.Background()); err == nil && top != "" {
		if c, cerr := config.Load(config.DefaultGlobalPath(), filepath.Join(top, ".gg.toml")); cerr == nil {
			cfg = c
		}
	}
	if cfg.Debug.LogOperations {
		if err := m.opLog.enable(); err != nil {
			m.statusMsg = i18n.T("operation log: %s", err.Error())
		}
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := p.Run()
	if fm, ok := final.(Model); ok && fm.opCancel != nil {
		fm.opCancel()
	}
	if err != nil {
		return "", err
	}
	if m, ok := final.(Model); ok {
		return m.switchTarget, nil
	}
	return "", nil
}
