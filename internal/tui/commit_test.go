package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
)

func TestCKeyCommitsStagedIndex(t *testing.T) {
	dir, repo := newRepoDir(t)
	gitInDir(t, dir, "checkout", "-b", "work")
	os.WriteFile(filepath.Join(dir, "n.txt"), []byte("hi\n"), 0o644)
	gitInDir(t, dir, "add", "n.txt")

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)

	m = pressRune(t, m, "c")
	if m.commitPopup == nil {
		t.Fatal("c must open the commit popup when something is staged")
	}
	// type a title through the real dispatch (routes to the popup)
	upd, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("add n")})
	m = upd.(Model)
	upd, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = upd.(Model)
	if m.commitPopup != nil {
		t.Fatal("ctrl+s must close the popup and start the commit")
	}
	m = driveOp(t, m, cmd)

	out, _ := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s").Output()
	if strings.TrimSpace(string(out)) != "add n" {
		t.Fatalf("subject = %q, want 'add n'", strings.TrimSpace(string(out)))
	}
}

func TestCKeyNoOpWhenNothingStaged(t *testing.T) {
	_, repo := newRepoDir(t)
	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m = pressRune(t, m, "c")
	if m.commitPopup != nil {
		t.Fatal("c must not open the popup when nothing is staged")
	}
}
