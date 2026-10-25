package domain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
)

// DefaultEditor is the editor when neither $VISUAL nor $EDITOR names one.
func DefaultEditor() string {
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

// ResolveEditor picks the editor: $VISUAL, then $EDITOR, then the platform
// default. Values are trimmed first, so a whitespace-only variable is unset.
func ResolveEditor(getenv func(string) string) string {
	if e := strings.TrimSpace(getenv("VISUAL")); e != "" {
		return e
	}
	if e := strings.TrimSpace(getenv("EDITOR")); e != "" {
		return e
	}
	return DefaultEditor()
}

// editorProgram is the editor's program name for the line-flag table: the
// basename of the first field, a Windows .exe/.cmd suffix stripped, lower-cased.
func editorProgram(program string) string {
	p := program
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		p = p[i+1:]
	}
	p = strings.ToLower(p)
	return strings.TrimSuffix(strings.TrimSuffix(p, ".exe"), ".cmd")
}

// EditorArgv is the editor invocation opening absPath, at line when line > 0
// and the program's goto syntax is known: the editor string is split on
// whitespace (program + leading flags; no shell-quote parsing), then
// vi-style editors get `+N path`, VS Code-style editors `--goto path:N`,
// helix/sublime/zed `path:N`, and anything else the plain path (the line is
// dropped, never guessed). An empty editor is the default.
func EditorArgv(editor, absPath string, line int) []string {
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		fields = []string{DefaultEditor()}
	}
	argv := append([]string{}, fields...)
	if line <= 0 {
		return append(argv, absPath)
	}
	switch editorProgram(fields[0]) {
	case "vim", "nvim", "vi", "nano", "emacs", "micro", "kak":
		return append(argv, fmt.Sprintf("+%d", line), absPath)
	case "code", "code-insiders", "codium", "cursor":
		return append(argv, "--goto", fmt.Sprintf("%s:%d", absPath, line))
	case "hx", "subl", "zed":
		// path:N is ambiguous with a Windows drive letter's colon
		// (C:\x\y.go:12), but these editors parse it correctly anyway.
		return append(argv, fmt.Sprintf("%s:%d", absPath, line))
	}
	return append(argv, absPath)
}

// WriteViewCopy writes data to a fresh temp file whose name ENDS in the base
// name of name ("gg-<rand>-foo.go"), so the editor keeps the extension and
// highlights it, and marks it 0400 as a read-only hint (edits are
// discarded). Returns the temp path; RemoveViewCopy deletes it.
func WriteViewCopy(name string, data []byte) (string, error) {
	base := filepath.Base(name)
	if base == "." || base == string(os.PathSeparator) || base == "" {
		base = "file"
	}
	f, err := os.CreateTemp("", "gg-*-"+base)
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		RemoveViewCopy(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		RemoveViewCopy(path)
		return "", err
	}
	_ = os.Chmod(path, 0o400) // best-effort read-only hint
	return path, nil
}

// RemoveViewCopy deletes a view copy, clearing the read-only bit first so
// Windows (which refuses to delete a read-only file) can remove it too.
func RemoveViewCopy(path string) {
	if path == "" {
		return
	}
	_ = os.Chmod(path, 0o600)
	_ = os.Remove(path)
}

// WorktreeAbsPath is the working-tree path of rel (repo-root-relative,
// slash-separated), refused when it escapes the tree — what a live edit
// hands the editor.
func (s *Service) WorktreeAbsPath(ctx context.Context, rel string) (string, error) {
	return s.repo.WorktreeAbsPath(ctx, rel)
}

// StartEditor runs an editor (argv from EditorArgv) as a Terminal session in
// worktreeDir — the console a browser shows in place of the terminal the
// TUI hands over. When the editor exits, done runs (a view removes its
// copy) and the session leaves the list: an editor leaves no trace, as in
// the TUI.
func (s *Service) StartEditor(ctx context.Context, argv []string, label, worktreeDir, cwd string, cols, rows int, done func()) (*AgentSession, error) {
	mgr := Sessions()
	sess, err := mgr.Start(agentsession.StartSpec{
		Label: label, Terminal: true, Repo: s.sessionRepo(ctx, worktreeDir), Dir: worktreeDir, Cwd: cwd,
		Argv: argv, Cols: cols, Rows: rows,
	})
	if err != nil {
		return nil, err
	}
	go func() {
		<-sess.Done()
		if done != nil {
			done()
		}
		id := sess.Info().ID
		for i := 0; i < 50; i++ { // the exit is recorded just after Done closes
			if err := mgr.Remove(id); err != agentsession.ErrRunning {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	return sess, nil
}
