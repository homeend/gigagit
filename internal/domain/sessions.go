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
	SessionID    = agentsession.ID
	SessionInfo  = agentsession.Info
	SessionState = agentsession.State
	AgentSession = agentsession.Session
	// SessionManager is the manager type behind Sessions() (frontends hold
	// it to notice a swapped manager, never to construct one).
	SessionManager = agentsession.Manager
	SessionScreen  = agentsession.Screen
	SessionKey     = agentsession.Key
	// The console's mouse pass-through and the child's clipboard writes.
	SessionMouse      = agentsession.Mouse
	SessionInputModes = agentsession.InputModes
	SessionClip       = agentsession.Clip
	// Scroll mode's frozen view of a console's history.
	SessionHistory  = agentsession.History
	SessionRowMarks = agentsession.RowMarks
	// The web console's frame (styled runs) and a start spec for callers
	// that assemble one themselves (tests).
	ScreenRuns       = agentsession.ScreenRuns
	ScreenRun        = agentsession.Run
	ScreenRunRow     = agentsession.RunRow
	SessionStartSpec = agentsession.StartSpec
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
// returns a func restoring the previous one. The spawn registry (records,
// tokens, reports, wait marks) is keyed by session ids the new manager
// hands out again from s1, so it is swapped for a fresh one too.
func UseSessionManager(m *agentsession.Manager) func() {
	sessionsMu.Lock()
	prev := sessionsMgr
	sessionsMgr = m
	sessionsMu.Unlock()
	restoreReg := useSpawnRegistry()
	resetSessionStates() // the state watcher follows the manager
	return func() {
		sessionsMu.Lock()
		sessionsMgr = prev
		sessionsMu.Unlock()
		restoreReg()
		resetSessionStates()
	}
}

// ensureMu serialises the first-run detect's check-then-append
// (EnsureSessionCommands) across the frontends of this process.
var ensureMu sync.Mutex

func hasSessionCommand(cfg config.Config) bool {
	for _, tc := range cfg.Tools.Command {
		if tc.Category == string(exttool.CatSession) {
			return true
		}
	}
	return false
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
//
// cfg may be STALE: the terminal and the page it hosts each hold their own
// loaded config, and the other one's first run may have written the file
// since. So the global file is read again here, under ensureMu, before
// anything is appended — a caller that gets nil re-loads its config to see
// what is there.
func EnsureSessionCommands(cfg config.Config, globalPath string, detect func() []exttool.Detection) ([]string, error) {
	if hasSessionCommand(cfg) {
		return nil, nil // configured (even if hidden from this frontend or invalid): never second-guess the user's file
	}
	ensureMu.Lock()
	defer ensureMu.Unlock()
	if onDisk, err := config.Load(globalPath, ""); err == nil && hasSessionCommand(onDisk) {
		return nil, nil
	}
	var blocks []config.ToolCommand
	var names []string
	for _, det := range detect() {
		for _, ct := range InstallTemplates(context.Background(), det, true) {
			if ct.Category != exttool.CatSession || ct.OptIn {
				continue
			}
			blocks = append(blocks, NewToolBlock(det, ct))
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
	return s.startSessionPrompt(ctx, tc, worktreeDir, cwd, cols, rows, env, "", "")
}

func (s *Service) startSessionPrompt(ctx context.Context, tc config.ToolCommand, worktreeDir, cwd string, cols, rows int, env []string, prompt, name string) (*AgentSession, error) {
	resolved, err := template.ResolveCommand(tc.Command, nil, template.CmdCtx{Repo: worktreeDir, Prompt: prompt})
	if err != nil {
		return nil, err
	}
	sess, err := s.startLine(ctx, Sessions(), tc.Name, name, agentIDFor(tc), resolved, worktreeDir, cwd, cols, rows, env)
	if err != nil {
		return nil, err
	}
	// A command with its own screen_* lists is classified by them (an
	// invalid list leaves the agent's built-ins; SessionRulesWarning says so).
	if p, custom, _ := SessionProfile(tc); custom {
		bindSessionProfile(sess.Info().ID, p)
	}
	return sess, nil
}

// StartAgentSession is StartSession for an AGENT the agent channel can
// reach: with mcpURL set the child gets GG_MCP_URL and a fresh
// GG_SESSION_TOKEN, bound to the new session with rec. prompt fills the
// command's <prompt> slot (AgentKickoff for a spawned worker, "" for a
// manual start). rec.Name is the user's name for the session (cleaned
// here). Returns the token ("" without a URL).
func (s *Service) StartAgentSession(ctx context.Context, tc config.ToolCommand, dir, cwd string, cols, rows int, env []string, mcpURL string, rec SpawnRecord, prompt string) (*AgentSession, string, error) {
	tok := ""
	if mcpURL != "" {
		tok = mintToken()
		env = append(append([]string(nil), env...), "GG_MCP_URL="+mcpURL, "GG_SESSION_TOKEN="+tok)
	}
	if rec.Parent != "" {
		env = append(env, "GG_PARENT_SESSION="+rec.Parent)
	}
	rec.Name = CleanAgentName(rec.Name)
	sess, err := s.startSessionPrompt(ctx, tc, dir, cwd, cols, rows, env, prompt, rec.Name)
	if err != nil {
		return nil, "", err
	}
	full := FullSessionID(sess.Info().ID)
	bindRecord(full, rec)
	if tok != "" {
		bindToken(tok, full)
	}
	return sess, tok, nil
}

// MainCheckoutName is the repository's local name: the directory of its MAIN
// checkout, whichever worktree this Service sits in (git lists the main
// worktree first). Falls back to this worktree's own directory, "" when
// even that is unknown.
func (s *Service) MainCheckoutName(ctx context.Context) string {
	if wts, err := s.Worktrees(ctx); err == nil && len(wts) > 0 && wts[0].Path != "" {
		// A foreign backslash-notation record ends in \.git (see reroot).
		p := strings.TrimSuffix(strings.TrimSuffix(wts[0].Path, `\.git`), "/.git")
		return path.Base(strings.ReplaceAll(p, `\`, "/"))
	}
	if top, err := s.TopLevel(ctx); err == nil && top != "" {
		return filepath.Base(top)
	}
	return ""
}

// sessionRepo is the name a session is grouped under: the remote's
// repository name, else the main checkout's directory — never the linked
// worktree's own, or one repository's sessions would split per worktree.
func (s *Service) sessionRepo(ctx context.Context, worktreeDir string) string {
	if repo, err := s.RepoName(ctx); err == nil && repo != "" {
		return repo
	}
	if name := s.MainCheckoutName(ctx); name != "" {
		return name
	}
	return filepath.Base(worktreeDir)
}

// startLine runs an already-resolved command line as a session on mgr (the
// AI-task path hands it a line Prepare resolved against its temp files —
// resolving it again would misread any <…> in a path).
func (s *Service) startLine(ctx context.Context, mgr *agentsession.Manager, label, name, agentID, line, worktreeDir, cwd string, cols, rows int, env []string) (*AgentSession, error) {
	repo := s.sessionRepo(ctx, worktreeDir)
	argv, cmdline := sessionShell(line, runtime.GOOS, os.Getenv)
	return mgr.Start(agentsession.StartSpec{
		Label: label, Name: name, AgentID: agentID, Repo: repo, Dir: worktreeDir,
		Cwd: cwd, Argv: argv, CmdLine: cmdline, Env: env, Cols: cols, Rows: rows,
		TracePath: sessionTracePath(os.Getenv("GG_SESSION_TRACE"), label, time.Now()),
	})
}

// StartTerminal runs an interactive shell in worktreeDir as a session
// labelled "Terminal" — argv directly, no `-c` wrapper: the shell IS the
// program. shell is the [console] shell override ("" = pick one); cwd and env
// as for StartSession.
func (s *Service) StartTerminal(ctx context.Context, shell, worktreeDir, cwd string, cols, rows int, env []string) (*AgentSession, error) {
	repo := s.sessionRepo(ctx, worktreeDir)
	return Sessions().Start(agentsession.StartSpec{
		Label: "Terminal", Terminal: true, Repo: repo, Dir: worktreeDir, Cwd: cwd,
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

// SessionStartRequest is a frontend asking for a session in a worktree: the
// page's start (web) handed to whoever owns the start — the web server
// itself, or the terminal hosting the page. Command is ignored for a
// terminal.
type SessionStartRequest struct {
	Worktree   string
	Command    config.ToolCommand
	Name       string // the user's name for an agent ("" = unnamed); ignored for a terminal
	Terminal   bool
	Cols, Rows int
}

// SessionProgram is the program a session command runs: its first word, or
// the double-quoted first word of a Windows install path. "" for an empty
// command.
func SessionProgram(tc config.ToolCommand) string {
	prog := strings.TrimSpace(tc.Command)
	if strings.HasPrefix(prog, `"`) {
		if end := strings.Index(prog[1:], `"`); end >= 0 {
			return prog[1 : 1+end]
		}
		return prog
	}
	if f := strings.Fields(prog); len(f) > 0 {
		return f[0]
	}
	return ""
}

// agentIDFor maps a command to its catalog tool id by its program (the
// first word, or the double-quoted first word of a Windows install path),
// "" for a custom command.
func agentIDFor(tc config.ToolCommand) string {
	prog := SessionProgram(tc)
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
