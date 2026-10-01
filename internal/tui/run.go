package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repos"
	"github.com/homeend/gigagit/internal/theme"
)

// Run launches the TUI for svc, taking over the alternate screen until the
// user quits. at is the `gg open` landing link (the zero value = none): it is
// converted into a navigate and applied once every startAtReady precondition
// has landed. It returns the directory the shell should switch to (the
// worktree the user switched into during the session, or "" if none) so a
// wrapper can cd there on exit.
// RunOptions is the launch configuration cmd/gg hands Run.
type RunOptions struct {
	RecordPath string     // --record: dump keystrokes to this file
	At         model.Link // `gg open`'s landing link (zero = none)
	Web        bool       // --web: serve the web page from this process at launch
	WebAddr    string     // --web-addr: the page's listen address (beats [web] addr)
}

// prepareModel is the model set-up tui.Run and the headless driver share
// (headless.go), so the two cannot drift: config, theme, branch filters,
// tasks config, snapshot target. It also returns the startup config, which
// Run reads for the operation log.
func prepareModel(svc *domain.Service) (Model, config.Config) {
	m := New(svc)
	cfg := config.Defaults()
	if top, err := svc.TopLevel(context.Background()); err == nil && top != "" {
		if c, cerr := config.Load(config.DefaultGlobalPath(), filepath.Join(top, ".gg.toml")); cerr == nil {
			cfg = c
		}
	}
	// Paint the very first frame in the configured theme, with its
	// [themes.<name>] overrides — mirroring applyTheme so startup and the
	// configReadyMsg that follows never disagree for a frame. Complaints
	// (unknown name, invalid override value) are applyTheme's job: it runs
	// once the registry loads and has a status bar to put them in.
	th, _ := theme.Lookup(cfg.UI.Theme)
	th, _ = theme.Overlay(th, cfg.Themes[th.Name])
	setTheme(th)
	// The startup path's config, on the model: the live-steering gate below
	// reads it, and dataLoadedMsg overwrites it with the same value moments
	// later. Every pre-load fallback helper (Model.wheelStep and friends)
	// treats a defaults-filled cfg exactly as it treats the zero value.
	m.cfg = cfg
	// Compile the branch-filter slots from the startup config. The remembered
	// ACTIVE slot cannot load yet (the health probe has not resolved the common
	// dir); loadBranchFilterSlots is a no-op until it does, and notify.go's
	// applyRepoHealth runs it then.
	m = m.applyBranchFilterConfig()
	m, _ = m.applyTasksConfig() // a warning comes back with configReadyMsg
	m = m.initSnapshotTarget()
	return m, cfg
}

func Run(svc *domain.Service, opts RunOptions) (string, error) {
	recordPath, at := opts.RecordPath, opts.At
	m, cfg := prepareModel(svc)
	m.webOpts = webLaunchOptions{Web: opts.Web, WebAddr: opts.WebAddr}
	if at.Repo.Name != "" || at.Repo.Abs != "" {
		m.startAt, m.startAtPending = at, true
	}
	m.statePath = repos.DefaultStatePath()
	if home, err := os.UserHomeDir(); err == nil {
		m.initHomeDir = home
	}
	// Honor [debug] log_operations at startup. Wired here — the real entry point
	// that unit tests bypass (like statePath above) — rather than in loadCmd, so
	// enabling the operation log never performs a global SetSpanSink side effect
	// during a test's model load. The , Settings toggle drives it thereafter.
	if cfg.Debug.LogOperations {
		if err := m.opLog.enable(); err != nil {
			m.statusMsg = i18n.T("operation log: %s", err.Error())
		}
	}
	// The inbox is keyed by worktree under the session dir the snapshot just
	// resolved; the watcher itself starts from Init().
	m.steerDir = steerDirFor(m.snapshotCommonDir, m.snapshotWorktree)
	m = m.initSteerInbox()
	if recordPath != "" {
		repo := ""
		if top, err := svc.TopLevel(context.Background()); err == nil {
			repo = top
		}
		rec, rerr := newRecorder(recordPath, repo)
		if rerr != nil {
			return "", fmt.Errorf("--record: %w", rerr)
		}
		m.recorder = rec
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithFilter(quitFilter))
	// Wrap off for the TUI's lifetime (see autowrapOff): a glyph the terminal
	// draws wider than gg measured must clip at the right edge, never wrap
	// and scramble the frame. Stdout is the program's output.
	var final tea.Model
	err := autowrapOff(os.Stdout, func() error {
		var rerr error
		final, rerr = p.Run()
		return rerr
	})
	// Safety net: whatever ended the program (a confirmed quit, a panic
	// recovered by Run, a killed terminal), no agent outlives gg.
	killCtx, killCancel := context.WithTimeout(context.Background(), killAllGrace)
	domain.Tasks().KillAll(killCtx)
	domain.Sessions().KillAll(killCtx)
	killCancel()
	if fm, ok := final.(Model); ok {
		if fm.opCancel != nil {
			fm.opCancel()
		}
		removeSnapshotFile(fm.snapshotPath)
		fm = fm.closeSteerInbox()
		fm = fm.releaseKeptInboxes()
		fm.recorder.close()
		fm.closeWeb() // the pages get their shutdown message; the port closes
	}
	if err != nil {
		return "", err
	}
	if m, ok := final.(Model); ok {
		return m.switchTarget, nil
	}
	return "", nil
}
