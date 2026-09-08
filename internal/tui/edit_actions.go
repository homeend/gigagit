package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

func defaultEditor() string {
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

// resolveEditor picks the editor: $VISUAL, then $EDITOR, then a platform
// default. Values are trimmed first, so a whitespace-only env var is treated as
// unset (otherwise editorCommand's Fields split would be empty and panic).
func resolveEditor() string {
	if e := strings.TrimSpace(os.Getenv("VISUAL")); e != "" {
		return e
	}
	if e := strings.TrimSpace(os.Getenv("EDITOR")); e != "" {
		return e
	}
	return defaultEditor()
}

// editorCommand builds the plain editor invocation (no line): see editorCommandAt.
func editorCommand(editor, absPath string) *exec.Cmd { return editorCommandAt(editor, absPath, 0) }

// editorProgram is the editor's program name for the line-flag table: the
// basename of the first field, a Windows .exe/.cmd suffix stripped, lower-cased.
func editorProgram(editor string) string {
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		return ""
	}
	p := fields[0]
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		p = p[i+1:]
	}
	p = strings.ToLower(p)
	p = strings.TrimSuffix(strings.TrimSuffix(p, ".exe"), ".cmd")
	return p
}

// editorCommandAt builds the editor invocation, opening absPath at line when
// line > 0 and the program's goto syntax is known: the editor string is split
// on whitespace (binary + leading flags; no shell-quote parsing — v1), then
// vi-style editors get `+N path`, VS Code-style editors `--goto path:N`,
// helix/sublime/zed `path:N`, and anything else the plain path (the line is
// dropped, never guessed). Guards the empty-fields case as belt-and-braces.
func editorCommandAt(editor, absPath string, line int) *exec.Cmd {
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		fields = []string{defaultEditor()}
	}
	args := append([]string{}, fields[1:]...)
	if line > 0 {
		// Decide the program from fields[0] (post empty-editor substitution),
		// not the raw editor string: an empty editor falls back to
		// defaultEditor() above, and that fallback must still get the goto
		// treatment when it is one of the known programs.
		switch editorProgram(fields[0]) {
		case "vim", "nvim", "vi", "nano", "emacs", "micro", "kak":
			args = append(args, fmt.Sprintf("+%d", line), absPath)
		case "code", "code-insiders", "codium", "cursor":
			args = append(args, "--goto", fmt.Sprintf("%s:%d", absPath, line))
		case "hx", "subl", "zed":
			// path:N is ambiguous with a Windows drive letter's colon
			// (C:\x\y.go:12), but these editors parse it correctly anyway.
			args = append(args, fmt.Sprintf("%s:%d", absPath, line))
		default:
			args = append(args, absPath)
		}
	} else {
		args = append(args, absPath)
	}
	return exec.Command(fields[0], args...)
}

// fileEditRow offers "Edit in editor" on the selected Files-panel file. Every
// Files-panel row is a real working-tree file on disk (modified, untracked, or
// conflicted), so no file-kind restriction applies.
func (m Model) fileEditRow() (actionRow, bool) {
	if m.focus != panelFiles || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelFiles)
	if !ok {
		return actionRow{}, false
	}
	p := m.status.Files[bi].Path
	return actionRow{
		id:    "edit-file",
		label: i18n.T("Edit in editor"),
		run:   func(m Model) (tea.Model, tea.Cmd) { return m, m.editFileCmd(p) },
	}, true
}

// stagedOpenExternalRow offers "Open staged version in external editor" on the
// Staged panel: the index blob (`git show :path`), which differs from the
// working-tree file the Files panel's live "Edit in editor" opens. A staged
// deletion has no index blob, so it is skipped.
func (m Model) stagedOpenExternalRow() (actionRow, bool) {
	if m.focus != panelStaged || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelStaged)
	if !ok {
		return actionRow{}, false
	}
	f := m.status.Files[bi]
	if f.Staged == 'D' { // staged for deletion: no content at the index
		return actionRow{}, false
	}
	p, svc := f.Path, m.svc
	return actionRow{
		id:    "open-external-staged",
		label: i18n.T("Open staged version in external editor"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.openInEditorCmd(p, func(ctx context.Context) ([]byte, error) {
				return svc.ShowFile(ctx, "", p) // `git show :path` = the index blob
			})
		},
	}, true
}

// editedSummary is the status-bar message after a successful edit.
func editedSummary(rel string) string { return i18n.T("edited %s", filepath.Base(rel)) }

// editFileCmd suspends the TUI and opens rel (repo-relative) in the user's
// editor; on exit it yields an editorFinishedMsg. See editFileAtCmd.
func (m Model) editFileCmd(rel string) tea.Cmd { return m.editFileAtCmd(rel, 0) }

// editFileAtCmd is editFileCmd positioned on line (0 = no position). Bubble
// Tea's ExecProcess owns the terminal release/restore and the cmd's stdio —
// do not set them here.
func (m Model) editFileAtCmd(rel string, line int) tea.Cmd {
	abs := filepath.Join(m.currentWorktree, rel)
	cmd := editorCommandAt(resolveEditor(), abs, line)
	cmd.Dir = m.currentWorktree
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{path: rel, err: err}
	})
}
