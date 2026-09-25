package domain

import (
	"context"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

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
//
// cwd, when not "", is where the process runs instead of worktreeDir (a
// translated foreign-notation worktree path); worktreeDir stays the
// session's identity. env is appended to the child's environment (the
// frontend's GG_INBOX).
func (s *Service) StartSession(ctx context.Context, tc config.ToolCommand, worktreeDir, cwd string, cols, rows int, env []string) (*AgentSession, error) {
	resolved, err := template.ResolveCommand(tc.Command, nil, template.CmdCtx{Repo: worktreeDir})
	if err != nil {
		return nil, err
	}
	repo, err := s.RepoName(ctx)
	if err != nil || repo == "" {
		repo = filepath.Base(worktreeDir)
	}
	argv, cmdline := sessionShell(resolved, runtime.GOOS, os.Getenv)
	return Sessions().Start(agentsession.StartSpec{
		Label: tc.Name, AgentID: agentIDFor(tc), Repo: repo, Dir: worktreeDir,
		Cwd: cwd, Argv: argv, CmdLine: cmdline, Env: env, Cols: cols, Rows: rows,
		TracePath: sessionTracePath(os.Getenv("GG_SESSION_TRACE"), tc.Name, time.Now()),
	})
}

// StartTerminal runs an interactive shell in worktreeDir as a session
// labelled "Terminal" — argv directly, no `-c` wrapper: the shell IS the
// program. shell is the [console] shell override ("" = pick one); cwd and env
// as for StartSession.
func (s *Service) StartTerminal(ctx context.Context, shell, worktreeDir, cwd string, cols, rows int, env []string) (*AgentSession, error) {
	repo, err := s.RepoName(ctx)
	if err != nil || repo == "" {
		repo = filepath.Base(worktreeDir)
	}
	return Sessions().Start(agentsession.StartSpec{
		Label: "Terminal", Repo: repo, Dir: worktreeDir, Cwd: cwd,
		Argv: terminalShell(runtime.GOOS, os.Getenv, exec.LookPath, shell), Env: env,
		Cols: cols, Rows: rows,
		TracePath: sessionTracePath(os.Getenv("GG_SESSION_TRACE"), "Terminal", time.Now()),
	})
}

// terminalShell picks Open terminal's shell: the configured override first
// (run as given — a missing program fails at start with its own error),
// then $SHELL (else /bin/sh) on Unix, and pwsh → powershell → %COMSPEC%
// (else cmd.exe) on Windows.
func terminalShell(goos string, getenv func(string) string, lookPath func(string) (string, error), override string) []string {
	if override != "" {
		return []string{override}
	}
	if goos != "windows" {
		if sh := getenv("SHELL"); sh != "" {
			return []string{sh}
		}
		return []string{"/bin/sh"}
	}
	for _, name := range []string{"pwsh", "powershell"} {
		if p, err := lookPath(name); err == nil {
			return []string{p}
		}
	}
	if cs := getenv("COMSPEC"); cs != "" {
		return []string{cs}
	}
	return []string{"cmd.exe"}
}

// sessionShell builds how the resolved command line runs, the way external
// tools run (tui's toolExecCmd). POSIX: $SHELL -c <line> — the shell exits
// with the last command's status and the session's process-group kill
// reaches the agent under it; no `exec` prefix, which on a compound line
// would run only the FIRST command. Windows: a raw command line
// `"%COMSPEC%" /S /C "<line>"` (returned as cmdline, argv only names the
// program) — /S makes cmd strip exactly the outer quotes and run the rest
// verbatim, so a quoted install path survives; composing it from argv would
// escape its quotes as \" and cmd would not parse them.
func sessionShell(line, goos string, getenv func(string) string) (argv []string, cmdline string) {
	if goos == "windows" {
		comspec := getenv("COMSPEC")
		if comspec == "" {
			comspec = "cmd.exe"
		}
		// One command line cannot carry a line break: the lines FlattenForCmd
		// leaves separate (a genuine multi-line script) run in sequence via &.
		flat := strings.ReplaceAll(strings.TrimRight(template.FlattenForCmd(line), "\r\n"), "\r\n", " & ")
		return []string{comspec, "/S", "/C", flat}, `"` + comspec + `" /S /C "` + flat + `"`
	}
	sh := getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return []string{sh, "-c", line}, ""
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

// sessionTracePath is where a session's raw output is recorded when
// GG_SESSION_TRACE names a directory ("" = no recording): one
// <time>-<label>.raw file per session, the evidence for replaying a
// terminal-emulation mismatch (e.g. a ConPTY repaint) offline.
func sessionTracePath(dir, label string, now time.Time) string {
	if dir == "" {
		return ""
	}
	safe := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '_'
	}, label)
	return filepath.Join(dir, now.Format("20060102-150405.000")+"-"+safe+".raw")
}
