package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
)

func TestWorktreeFileListDropsDeletedAddsUntracked(t *testing.T) {
	t.Parallel()
	st := model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "b.go", Staged: '.', Unstaged: 'D'},
		{Path: "n.txt", Kind: model.KindUntracked},
		{Path: "a.go", Staged: '.', Unstaged: 'M'},
	}}
	paths, untracked := worktreeFileList([]string{"d.go", "a.go", "b.go"}, st)
	if got := fmt.Sprint(paths); got != "[a.go d.go n.txt]" {
		t.Fatalf("paths = %s, want [a.go d.go n.txt]", got)
	}
	if len(untracked) != 1 || !untracked["n.txt"] {
		t.Fatalf("untracked = %v, want {n.txt}", untracked)
	}
}

func TestWorktreeFileListStagedDeletionIsGone(t *testing.T) {
	t.Parallel()
	st := model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "gone.go", Staged: 'D', Unstaged: '.'}}}
	paths, _ := worktreeFileList([]string{"gone.go", "kept.go", "kept.go"}, st)
	if got := fmt.Sprint(paths); got != "[kept.go]" {
		t.Fatalf("paths = %s, want [kept.go]", got)
	}
}

// wtWindow opens F's working-tree files window over paths.
func wtWindow(t *testing.T, paths ...string) Model {
	t.Helper()
	m := loadedNavModel(t)
	m.focus = panelBranches
	tm, _ := m.Update(keyMsg("F"))
	m = tm.(Model)
	tm, _ = m.Update(lsFilesMsg{paths: paths})
	return tm.(Model)
}

func wtRows(m Model) []string {
	var out []string
	for _, l := range m.filesView.visible() {
		out = append(out, l.path)
	}
	return out
}

func TestFOpensTheWorktreeFilesWindow(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "b.go", "a.go")
	if m.filesView == nil || !m.inWorktreeFiles() {
		t.Fatal("F did not open the working-tree files window")
	}
	if layerOf[*fileFinderPopup](m) != nil {
		t.Fatal("F still opened the popup")
	}
	if got := fmt.Sprint(wtRows(m)); got != "[a.go b.go]" {
		t.Fatalf("rows = %s, want [a.go b.go]", got)
	}
}

func TestWorktreeFilterIsFuzzy(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "internal/foo/view.go", "a.txt")
	m = fvKeys(t, m, keyMsg("/"), keyMsg("f"), keyMsg("v"), keyMsg("g"))
	if got := fmt.Sprint(wtRows(m)); got != "[internal/foo/view.go]" {
		t.Fatalf("filtered rows = %s", got)
	}
	m = fvKeys(t, m, keyMsg("enter"))
	if m.wtFiles.typing || m.wtFiles.query != "fvg" {
		t.Fatalf("enter: typing=%v query=%q, want the query kept", m.wtFiles.typing, m.wtFiles.query)
	}
	m = fvKeys(t, m, keyMsg("esc"))
	if m.wtFiles.query != "" || len(wtRows(m)) != 2 {
		t.Fatal("esc did not clear the committed filter")
	}
	m = fvKeys(t, m, keyMsg("esc"))
	if m.filesView != nil || m.focus != panelBranches {
		t.Fatalf("second esc: view open=%v focus=%v, want closed and back on Branches", m.filesView != nil, m.focus)
	}
}

func TestWorktreeFilterEscWhileTypingClears(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "a.go", "b.go")
	m = fvKeys(t, m, keyMsg("/"), keyMsg("a"), keyMsg("esc"))
	if m.wtFiles.typing || m.wtFiles.query != "" || len(wtRows(m)) != 2 || m.filesView == nil {
		t.Fatal("esc while typing must clear the filter and keep the window")
	}
}

func TestWorktreeFilesRenderFits(t *testing.T) {
	t.Parallel()
	for _, sz := range [][2]int{{80, 24}, {200, 50}} {
		m := wtWindow(t, "a.go", "internal/very/long/path/to/some/file_with_a_long_name.go")
		m.width, m.height = sz[0], sz[1]
		out := m.View()
		if !strings.Contains(out, "Files (working tree)") {
			t.Fatalf("%v: no window title in\n%s", sz, out)
		}
		lines := strings.Split(out, "\n")
		if len(lines) > m.height {
			t.Fatalf("%v: %d lines, want ≤ %d", sz, len(lines), m.height)
		}
		for _, l := range lines {
			if w := lipgloss.Width(l); w > m.width {
				t.Fatalf("%v: line width %d > %d: %q", sz, w, m.width, l)
			}
		}
	}
}
