package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/template"
)

// toolCommandHash keys first-run approval memory: the TEMPLATE text (not the
// per-run resolved values), so approving once covers every run until the
// config text changes.
func toolCommandHash(command string) string {
	sum := sha256.Sum256([]byte(command))
	return hex.EncodeToString(sum[:])[:16]
}

// toolRepoKey scopes approvals per repo: the git common dir when the repo
// health probe has resolved it (startup/reRoot), else the worktree path.
func (m Model) toolRepoKey() string {
	if m.repoHealth.GitCommonDir != "" {
		return m.repoHealth.GitCommonDir
	}
	return m.currentWorktree
}

// toolEnv renders the CmdCtx as GG_* env entries — the no-placeholder path
// for wrapper scripts (the post-create-hook pattern). All eleven are always
// set (GG_CONTEXT_FILE is "" when the caller has no context file, e.g. a
// direct unit-test CmdCtx that never went through toolContextFile).
func toolEnv(ctx template.CmdCtx) []string {
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

// cQuotePath renders p the way git prints a path containing control
// characters: double-quoted, with \n \r \t \" \\ as their usual C escapes
// and every other control byte (< 0x20) as a \NNN octal escape. A path with
// no control bytes is returned byte-exact and unquoted — quoting is applied
// only when needed, so an ordinary path (including one with a backtick or a
// dollar sign or non-ASCII bytes) never changes shape. Byte-wise, not
// rune-wise: a git path is a byte string, and UTF-8 continuation/lead bytes
// are always >= 0x80, so they can never be mistaken for a control byte.
func cQuotePath(p string) string {
	needsQuote := false
	for i := 0; i < len(p); i++ {
		if p[i] < 0x20 {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return p
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch c {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			if c < 0x20 {
				fmt.Fprintf(&b, `\%03o`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// toolContextFile writes the per-run context file: op/source/target header
// lines, then the conflicted paths one per line. Every value is byte-exact
// unless it contains a control character (newline/CR are legal in git
// paths, and Source/Target for cherry-pick/revert come from a commit
// subject via git %s — %s collapses an embedded \n to a space, so no line
// forgery there, but a raw \r would still land unquoted), in which case it
// is C-quoted via cQuotePath so one entry can never forge additional lines
// or otherwise corrupt the header (see the design spec's "Placeholders and
// environment" section). Created for conflict-category runs only, the only
// category stage 1 has.
func toolContextFile(ctx template.CmdCtx) (string, error) {
	var b strings.Builder
	b.WriteString("op: " + cQuotePath(ctx.Op) + "\n")
	b.WriteString("source: " + cQuotePath(ctx.Source) + "\n")
	b.WriteString("target: " + cQuotePath(ctx.Target) + "\n")
	b.WriteString("conflicted:\n")
	for _, f := range ctx.ConflictedFiles {
		b.WriteString(cQuotePath(f) + "\n")
	}
	f, err := os.CreateTemp("", "gg-context-*.txt")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(b.String()); err != nil {
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

// toolScript writes the resolved command to a temp script (0700) so the shell
// owns all quoting semantics — the same trick ShellHookRunner uses; a raw
// `sh -c`/`cmd /C` argv would re-open quoting problems on Windows.
//
// The script wraps the command so a NON-ZERO exit holds the terminal until
// Enter, then propagates the real exit code: a fast-failing tool (e.g. an
// agent CLI that errors out in under a second) would otherwise have its
// error text vanish the instant tea.ExecProcess returns and gg repaints the
// TUI over the terminal — the human never sees why it failed. A zero exit
// returns immediately with no hold, so the common case stays snappy. Exit
// codes 130/143 (SIGINT/SIGTERM — e.g. ctrl-C out of an interactive agent)
// are a normal user-initiated quit rather than a failure, so the POSIX
// wrapper skips the hold for those too (see toolInterruptExit, which mirrors
// this on the Go side for toolFinished's error classification).
func toolScript(resolved string) (string, error) {
	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".bat"
	}
	var body string
	if runtime.GOOS == "windows" {
		// Windows ctrl-C semantics differ from POSIX signals (no reliable
		// 130/143 exit-code convention), so the hold-on-failure block stays
		// unconditional here; revisit if this becomes a live issue on Windows.
		body = resolved + "\r\n" +
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

// toolExecCmd builds the interpreter invocation for the script (mirrors
// engine.hookShellArgv): $SHELL (default /bin/sh), or %COMSPEC% /C on Windows.
func toolExecCmd(script, dir string, extraEnv []string) *exec.Cmd {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = "cmd"
		}
		cmd = exec.Command(comspec, "/C", script)
	} else {
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/sh"
		}
		cmd = exec.Command(sh, script)
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	return cmd
}

// toolReadyMsg delivers an async-built pendingToolRun (per-file quartet).
type toolReadyMsg struct {
	pending *pendingToolRun
	err     error
}

// toolFinishedMsg signals the handed-over tool process exited.
type toolFinishedMsg struct {
	pending  *pendingToolRun
	script   string
	preMtime time.Time
	start    time.Time // when the process was handed the terminal (execToolCmd)
	err      error
}

// toolInterruptExit reports whether err represents a user-initiated
// ctrl-C/terminate quit rather than a tool failure. Two shapes qualify:
//
//  1. An *exec.ExitError carrying exit code 130 (SIGINT) or 143 (SIGTERM) —
//     the POSIX 128+signal convention for a shell that SURVIVED the signal
//     and propagated the code (see toolScript's wrapper).
//  2. An *exec.ExitError whose process was itself killed BY the signal — the
//     common ctrl-C case, since ctrl-C delivers SIGINT to the whole
//     foreground process group, including the wrapper shell. Go reports this
//     as a signal death (ExitCode() == -1, err.Error() == "signal:
//     interrupt"), not a 128+signal exit code, so case 1 alone never catches
//     it. exitErr.Sys().(syscall.WaitStatus) exposes Signaled()/Signal() on
//     every GOOS — Windows' WaitStatus.Signaled() is hardcoded false (no
//     POSIX signal-death concept there), so this branch is simply inert on
//     Windows rather than needing a build tag.
//
// Quitting an interactive agent CLI either way is a normal user-initiated
// quit, not a tool failure, so toolFinished treats it like a clean exit
// instead of surfacing an error box. A nil err (errors.As returns false on
// nil) or a non-exec.ExitError yields false, as does any other exit code or
// signal (e.g. SIGKILL, a crash).
func toolInterruptExit(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	if code := exitErr.ExitCode(); code == 130 || code == 143 {
		return true
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		switch ws.Signal() {
		case syscall.SIGINT, syscall.SIGTERM:
			return true
		}
	}
	return false
}

// toolDisposition renders the outcome of a tool run for the operation log:
// "ok" for a clean exit, the friendly label for an interrupt-quit (see
// toolInterruptExit), else the raw error text — Go renders that as "exit
// status N" for a propagated code or "signal: <name>" for a signal death
// toolInterruptExit didn't classify as a quit (e.g. SIGKILL).
func toolDisposition(err error) string {
	if err == nil {
		return "ok"
	}
	if toolInterruptExit(err) {
		return "interrupted (treated as quit)"
	}
	return err.Error()
}

// toolExitName returns the configured tool name for a finished run (e.g.
// "Junie", "Meld"), or a generic fallback if pending is unexpectedly nil.
func toolExitName(pending *pendingToolRun) string {
	if pending == nil || pending.tc.Name == "" {
		return "tool"
	}
	return pending.tc.Name
}

// logToolExit records exactly one operation-log line per tool run: the
// command name, its disposition (ok / an interrupt-quit / the raw exit
// error), and how long it held the terminal. observ.EmitSpan is a no-op
// unless `[debug] log_operations` has wired a sink (oplog.enable), so this
// costs nothing in the common case — the same always-on-but-gated channel
// domain.Execute uses to log every engine-op span, success or failure alike.
// Called on EVERY toolFinishedMsg (ok, interrupted, or a genuine failure) so
// a user auditing operations.log can see how a tool actually ended, not just
// the failures already surfaced in the error box / errors.log.
func logToolExit(msg toolFinishedMsg) {
	var dur time.Duration
	if !msg.start.IsZero() {
		dur = time.Since(msg.start)
	}
	span := observ.Span{
		Name:     "tool " + toolExitName(msg.pending),
		Args:     []string{"disposition=" + toolDisposition(msg.err)},
		Start:    msg.start,
		Duration: dur,
	}
	if msg.err != nil {
		span.Err = msg.err.Error()
		var exitErr *exec.ExitError
		if errors.As(msg.err, &exitErr) {
			span.ExitCode = exitErr.ExitCode()
		} else {
			span.ExitCode = -1
		}
	}
	observ.EmitSpan(span)
}

// execToolCmd suspends the TUI and runs the pending command with the real
// terminal (the editor-handover precedent). preMtime snapshots the per-file
// target so the return path can offer mark-resolved only on a real change.
func (m Model) execToolCmd(pending *pendingToolRun) tea.Cmd {
	start := time.Now()
	script, err := toolScript(pending.resolved)
	if err != nil {
		return func() tea.Msg { return toolFinishedMsg{pending: pending, start: start, err: err} }
	}
	var preMtime time.Time
	if pending.merged != "" {
		if fi, err := os.Stat(pending.merged); err == nil {
			preMtime = fi.ModTime()
		}
	}
	cmd := toolExecCmd(script, m.currentWorktree, pending.env)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return toolFinishedMsg{pending: pending, script: script, preMtime: preMtime, start: start, err: err}
	})
}
