package domain

// The conflict window's in-place tools (per-file mergetools, commands asking
// for <user:…> input) for every frontend: the picker's filters, the GG_*
// environment, the per-run context file, the wrapper script, and — for a
// frontend with no terminal to hand over (gg web) — a session that runs the
// script. The TUI hands its terminal to the same script.

import (
	"context"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/template"
)

// ConflictToolChoices filters conflict commands for the paused op and the
// focused file: when_op must match (empty = any), and a per_file command
// needs a focused both-sides conflict to act on.
func ConflictToolChoices(cmds []config.ToolCommand, op string, focusedBothSides bool) []config.ToolCommand {
	var out []config.ToolCommand
	for _, tc := range cmds {
		if tc.WhenOp != "" && tc.WhenOp != op {
			continue
		}
		if tc.PerFile && !focusedBothSides {
			continue
		}
		out = append(out, tc)
	}
	return out
}

// CompleteToolChoices filters conflict_complete commands: they exist to
// COMPLETE a paused sequencer op, so with no paused op (op == "") there is
// nothing to offer; when_op narrows further when set.
func CompleteToolChoices(cmds []config.ToolCommand, op string) []config.ToolCommand {
	if op == "" {
		return nil
	}
	var out []config.ToolCommand
	for _, tc := range cmds {
		if tc.WhenOp != "" && tc.WhenOp != op {
			continue
		}
		out = append(out, tc)
	}
	return out
}

// ToolEnv is the GG_* environment a conflict tool runs with — all eleven
// always set (GG_CONTEXT_FILE is "" without a context file).
func ToolEnv(ctx template.CmdCtx) []string {
	return []string{
		"GG_OP=" + ctx.Op,
		"GG_SOURCE=" + ctx.Source,
		"GG_TARGET=" + ctx.Target,
		"GG_CONFLICTED_FILES=" + strings.Join(ctx.ConflictedFiles, " "),
		"GG_REPO=" + ctx.Repo,
		"GG_FILE=" + ctx.File,
		"GG_LOCAL=" + ctx.Local,
		"GG_BASE=" + ctx.Base,
		"GG_REMOTE=" + ctx.Remote,
		"GG_MERGED=" + ctx.Merged,
		"GG_CONTEXT_FILE=" + ctx.ContextFile,
	}
}

// WriteToolContextFile writes the per-run context file: the op/source/target
// header and the conflicted paths, byte-exact (template.ConflictContextDoc).
func WriteToolContextFile(ctx template.CmdCtx) (string, error) {
	b := template.ConflictContextDoc(ctx.Op, ctx.Source, ctx.Target, ctx.ConflictedFiles)
	f, err := os.CreateTemp("", "gg-context-*.txt")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(b); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// WriteToolScript writes the resolved command to a temp script (0700) so the
// shell owns all quoting; a non-zero exit (but an interrupt) holds the
// terminal until Enter so a fast failure's text stays readable.
func WriteToolScript(resolved string) (string, error) {
	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".bat"
	}
	var body string
	if runtime.GOOS == "windows" {
		// Windows ctrl-C semantics differ from POSIX signals (no reliable
		// 130/143 exit-code convention), so the hold-on-failure block stays
		// unconditional here; revisit if this becomes a live issue on Windows.
		// cmd.exe cannot run a POSIX continuation or a quoted string that
		// spans lines: it would launch the tool from the first line with a
		// truncated argument and no flags (how "Claude (yolo)" ran without
		// --dangerously-skip-permissions).
		body = template.FlattenForCmd(resolved) + "\r\n" +
			"set RC=%ERRORLEVEL%\r\n" +
			"if %RC% neq 0 (\r\n" +
			"  echo.\r\n" +
			"  echo [gg] tool exited with an error - press any key to return to gg\r\n" +
			"  pause >nul\r\n" +
			")\r\n" +
			"exit /b %RC%\r\n"
	} else {
		body = resolved + "\n" +
			"rc=$?\n" +
			// 130/143 = SIGINT/SIGTERM by POSIX convention (128+signal) — the
			// user quit intentionally, not a tool failure, so don't hold.
			"if [ $rc -ne 0 ] && [ $rc -ne 130 ] && [ $rc -ne 143 ]; then\n" +
			"  printf '\\n[gg] tool exited with status %s - press Enter to return to gg\\n' \"$rc\"\n" +
			"  read -r _ignored\n" +
			"fi\n" +
			"exit $rc\n"
	}
	f, err := os.CreateTemp("", "gg-tool-*"+ext)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	if err := os.Chmod(f.Name(), 0o700); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// ToolScriptArgv runs a script written by WriteToolScript: $SHELL (default
// /bin/sh), or %COMSPEC% /C on Windows (the hook runner's interpreter rule).
func ToolScriptArgv(script string) []string {
	if runtime.GOOS == "windows" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = "cmd"
		}
		return []string{comspec, "/C", script}
	}
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return []string{sh, script}
}

// StartToolSession runs a conflict tool's script in a session (a PTY the web
// console shows) in worktreeDir — gg web's stand-in for the TUI's terminal
// handover. label names the session (the tool's name).
func (s *Service) StartToolSession(ctx context.Context, label, script, worktreeDir, cwd string, cols, rows int, env []string) (*AgentSession, error) {
	repo := s.sessionRepo(ctx, worktreeDir)
	return Sessions().Start(agentsession.StartSpec{
		Label: label, Terminal: true, Repo: repo, Dir: worktreeDir, Cwd: cwd,
		Argv: ToolScriptArgv(script), Env: env, Cols: cols, Rows: rows,
		TracePath: sessionTracePath(os.Getenv("GG_SESSION_TRACE"), label, time.Now()),
	})
}
