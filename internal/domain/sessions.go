package domain

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/template"
)

// Aliases so frontends use sessions without importing agentsession
// (archtest forbids that edge).
type (
	SessionID     = agentsession.ID
	SessionInfo   = agentsession.Info
	SessionState  = agentsession.State
	AgentSession  = agentsession.Session
	SessionScreen = agentsession.Screen
	SessionKey    = agentsession.Key
)

const (
	SessionRunning = agentsession.Running
	SessionExited  = agentsession.Exited
)

var (
	sessionsMu  sync.Mutex
	sessionsMgr *agentsession.Manager
)

// Sessions is the process-global session manager. It lives outside every
// Service because the TUI reopens its Service on each worktree/repo switch
// (reRoot) while sessions must keep running — the repogate registry
// precedent.
func Sessions() *agentsession.Manager {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if sessionsMgr == nil {
		sessionsMgr = agentsession.NewManager()
	}
	return sessionsMgr
}

// UseSessionManager installs m as the process-global manager (tests) and
// returns a func restoring the previous one.
func UseSessionManager(m *agentsession.Manager) func() {
	sessionsMu.Lock()
	prev := sessionsMgr
	sessionsMgr = m
	sessionsMu.Unlock()
	return func() {
		sessionsMu.Lock()
		sessionsMgr = prev
		sessionsMu.Unlock()
	}
}

// SessionCommands returns the effective config's valid `session` commands
// offered in frontend ("tui"|"web"|"cli"), in config order. An invalid block
// is inert here, as in every other tool lane.
func SessionCommands(cfg config.Config, frontend string) []config.ToolCommand {
	var out []config.ToolCommand
	for _, tc := range cfg.Tools.Command {
		if tc.Category != string(exttool.CatSession) || !config.ToolVisibleIn(tc, frontend) {
			continue
		}
		if config.ValidateToolCommand(tc) != nil || template.ValidateCommandTokens(tc.Command, false) != nil {
			continue
		}
		out = append(out, tc)
	}
	return out
}

// EnsureSessionCommands is the first-run auto-configure: when cfg has no
// session command, detect installed agents and append their SAFE session
// templates (never an OptIn/yolo one) to the global config at globalPath.
// Returns the names added; nil when already configured or nothing found.
func EnsureSessionCommands(cfg config.Config, globalPath string, detect func() []exttool.Detection) ([]string, error) {
	for _, tc := range cfg.Tools.Command {
		if tc.Category == string(exttool.CatSession) {
			return nil, nil // configured (even if hidden from this frontend or invalid): never second-guess the user's file
		}
	}
	var blocks []config.ToolCommand
	var names []string
	for _, det := range detect() {
		for _, ct := range det.Tool.Commands {
			if ct.Category != exttool.CatSession || ct.OptIn {
				continue
			}
			blocks = append(blocks, config.ToolCommand{
				Category: string(ct.Category), Name: ct.Name, Mode: string(ct.Mode),
				Frontends: ct.Frontends, Command: exttool.GenerateCommand(ct, det.Bin),
			})
			names = append(names, ct.Name)
		}
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	if err := config.AppendToolCommands(globalPath, blocks); err != nil {
		return nil, err
	}
	return names, nil
}

// StartSession resolves tc (runtime tokens such as <repo>) and runs it in
// worktreeDir through the user's shell, registered with Sessions(). The
// session is grouped under the repository NAME (RepoName), falling back to
// the directory's base name.
func (s *Service) StartSession(ctx context.Context, tc config.ToolCommand, worktreeDir string, cols, rows int) (*AgentSession, error) {
	resolved, err := template.ResolveCommand(tc.Command, nil, template.CmdCtx{Repo: worktreeDir})
	if err != nil {
		return nil, err
	}
	repo, err := s.RepoName(ctx)
	if err != nil || repo == "" {
		repo = filepath.Base(worktreeDir)
	}
	return Sessions().Start(agentsession.StartSpec{
		Label: tc.Name, AgentID: agentIDFor(tc), Repo: repo, Dir: worktreeDir,
		Argv: shellArgv(resolved), Cols: cols, Rows: rows,
	})
}

// shellArgv runs the resolved command line the way external tools run
// (tui's toolExecCmd): $SHELL -c on POSIX — the shell exits with the last
// command's status, and the session's process-group kill reaches the agent
// under it; %COMSPEC% /C on Windows, with a multi-line template flattened
// for cmd.exe. No `exec` prefix: on a compound line it would replace the
// shell with the FIRST command and silently drop the rest.
func shellArgv(cmdline string) []string {
	if runtime.GOOS == "windows" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = "cmd"
		}
		return []string{comspec, "/C", template.FlattenForCmd(cmdline)}
	}
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return []string{sh, "-c", cmdline}
}

// agentIDFor maps a command to its catalog tool id by its program (the
// first word, or the double-quoted first word of a Windows install path),
// "" for a custom command.
func agentIDFor(tc config.ToolCommand) string {
	prog := strings.TrimSpace(tc.Command)
	if strings.HasPrefix(prog, `"`) {
		if end := strings.Index(prog[1:], `"`); end >= 0 {
			prog = prog[1 : 1+end]
		}
	} else if f := strings.Fields(prog); len(f) > 0 {
		prog = f[0]
	}
	if prog == "" {
		return ""
	}
	base := strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(prog, `\`, "/"))), ".exe")
	for _, tl := range exttool.Builtins() {
		for _, b := range tl.Bins {
			if base == b {
				return tl.ID
			}
		}
	}
	return ""
}
