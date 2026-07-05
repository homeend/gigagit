package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

// toolContextFile writes the per-run context file: op/source/target header
// lines, then the conflicted paths one per line, byte-exact — no quoting or
// escaping, since a newline-delimited list needs none and this is the
// channel the injection-posture design uses specifically to avoid needing
// any (see the design spec's "Placeholders and environment" section).
// Created for conflict-category runs only, the only category stage 1 has.
func toolContextFile(ctx template.CmdCtx) (string, error) {
	var b strings.Builder
	b.WriteString("op: " + ctx.Op + "\n")
	b.WriteString("source: " + ctx.Source + "\n")
	b.WriteString("target: " + ctx.Target + "\n")
	b.WriteString("conflicted:\n")
	for _, f := range ctx.ConflictedFiles {
		b.WriteString(f + "\n")
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
func toolScript(resolved string) (string, error) {
	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".bat"
	}
	f, err := os.CreateTemp("", "gg-tool-*"+ext)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(resolved + "\n"); err != nil {
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
	err      error
}

// execToolCmd suspends the TUI and runs the pending command with the real
// terminal (the editor-handover precedent). preMtime snapshots the per-file
// target so the return path can offer mark-resolved only on a real change.
func (m Model) execToolCmd(pending *pendingToolRun) tea.Cmd {
	script, err := toolScript(pending.resolved)
	if err != nil {
		return func() tea.Msg { return toolFinishedMsg{pending: pending, err: err} }
	}
	var preMtime time.Time
	if pending.merged != "" {
		if fi, err := os.Stat(pending.merged); err == nil {
			preMtime = fi.ModTime()
		}
	}
	cmd := toolExecCmd(script, m.currentWorktree, pending.env)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return toolFinishedMsg{pending: pending, script: script, preMtime: preMtime, err: err}
	})
}
