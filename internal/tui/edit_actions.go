package tui

import (
	"context"
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

// editorCommand builds the editor invocation: the editor string is split on
// whitespace (binary + leading flags) and absPath is appended. No shell-quote
// parsing (v1) — sufficient for "vim", "code -w", "emacs -nw", etc. Guards the
// empty-fields case as belt-and-braces (resolveEditor already trims).
func editorCommand(editor, absPath string) *exec.Cmd {
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		fields = []string{defaultEditor()}
	}
	args := append(fields[1:], absPath)
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
func editedSummary(rel string) string { return "edited " + filepath.Base(rel) }

// editFileCmd suspends the TUI and opens rel (repo-relative) in the user's
// editor; on exit it yields an editorFinishedMsg. Bubble Tea's ExecProcess owns
// the terminal release/restore and the cmd's stdio — do not set them here.
func (m Model) editFileCmd(rel string) tea.Cmd {
	abs := filepath.Join(m.currentWorktree, rel)
	cmd := editorCommand(resolveEditor(), abs)
	cmd.Dir = m.currentWorktree
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{path: rel, err: err}
	})
}
