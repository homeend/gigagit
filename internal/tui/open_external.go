package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

// editorViewMsg carries a throwaway temp file written for read-only external
// viewing, ready to hand to the editor. Kept distinct from editorFinishedMsg
// (the live "Edit in editor" path) so its finish handler deletes the temp and
// skips the working-tree status reload — viewing a historical file changes
// nothing, and that reload is the slow path on a large worktree.
type editorViewMsg struct {
	path string // temp file to open (empty on err)
	name string // the real file name (display + temp suffix)
	err  error
}

// editorViewFinishedMsg signals the external viewer exited; the temp file is
// then removed and a "viewed <name>" notice shown.
type editorViewFinishedMsg struct {
	path string
	name string
	err  error
}

// openInEditorCmd resolves a file's bytes off the UI thread, writes them to a
// read-only temp file (real extension preserved for editor syntax
// highlighting), and yields an editorViewMsg. name is the real file name;
// resolve fetches the bytes by source (commit / shelf / bookmark / …).
func (m Model) openInEditorCmd(name string, resolve func(context.Context) ([]byte, error)) tea.Cmd {
	return func() tea.Msg {
		data, err := resolve(context.Background())
		if err != nil {
			return editorViewMsg{name: name, err: err}
		}
		if len(data) > domain.MaxDiffBytes {
			return editorViewMsg{name: name, err: fmt.Errorf("file too large to open (%d bytes)", len(data))}
		}
		path, err := writeReadOnlyTempFile(name, data)
		return editorViewMsg{path: path, name: name, err: err}
	}
}

// writeReadOnlyTempFile writes data to a fresh temp file whose name ENDS in the
// base name of `name` ("gg-<rand>-foo.go"), so the editor keeps the .go
// extension and highlights it. Marks it 0400 as a read-only hint (edits are
// discarded). Returns the temp path.
func writeReadOnlyTempFile(name string, data []byte) (string, error) {
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
		removeTempFile(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		removeTempFile(path)
		return "", err
	}
	_ = os.Chmod(path, 0o400) // best-effort read-only hint
	return path, nil
}

// viewExternalCmd launches the editor on a prepared temp file; the temp is
// cleaned up when the editor exits. Returned by the editorViewMsg handler so
// Bubble Tea suspends the TUI for the editor.
func viewExternalCmd(path, name string) tea.Cmd {
	cmd := editorCommand(resolveEditor(), path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorViewFinishedMsg{path: path, name: name, err: err}
	})
}

// removeTempFile deletes a read-only temp file, clearing the read-only bit first
// so Windows (which refuses to delete a read-only file) can remove it too.
func removeTempFile(path string) {
	if path == "" {
		return
	}
	_ = os.Chmod(path, 0o600)
	_ = os.Remove(path)
}

// viewedSummary is the status-bar message after a successful external view.
func viewedSummary(name string) string { return "viewed " + filepath.Base(name) }

// surfaceExternalRow builds the "Open in external editor" action for the history
// or blame surface on top (the file at the selected commit / the blamed file at
// its rev). Single source of truth for both the `.`-menu row and the `e` key, so
// the blame working-tree rule (rev=="" → the on-disk file, not the index blob)
// lives in one place. ok=false when the top layer is neither surface.
func (m Model) surfaceExternalRow() (actionRow, bool) {
	row := func(path string, resolve func(context.Context) ([]byte, error)) (actionRow, bool) {
		return actionRow{
			id:    "open-external",
			label: "Open in external editor",
			run: func(m Model) (tea.Model, tea.Cmd) {
				return m, m.openInEditorCmd(path, resolve)
			},
		}, true
	}
	switch s := m.topLayer().(type) {
	case *historyView:
		if s.sel < 0 || s.sel >= len(s.commits) {
			return actionRow{}, false
		}
		fc := s.commits[s.sel]
		path := fc.Path
		if path == "" {
			path = s.ctx.path
		}
		hash, svc := fc.Hash, m.svc
		return row(path, func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, hash, path) })
	case *blameView:
		path, rev, svc := s.ctx.path, s.ctx.rev, m.svc
		if rev == "" { // blame of the working-tree file: open the on-disk file, not the index blob
			return row(path, func(ctx context.Context) ([]byte, error) { return svc.WorktreeFile(ctx, path) })
		}
		return row(path, func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, rev, path) })
	}
	return actionRow{}, false
}
