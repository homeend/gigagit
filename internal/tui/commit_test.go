package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
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
	if layerOf[*commitPopup](m) == nil {
		t.Fatal("c must open the commit popup when something is staged")
	}
	// type a title through the real dispatch (routes to the popup)
	upd, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("add n")})
	m = upd.(Model)
	upd, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = upd.(Model)
	if layerOf[*commitPopup](m) != nil {
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
	if layerOf[*commitPopup](m) != nil {
		t.Fatal("c must not open the popup when nothing is staged")
	}
}

// While the popup is open it owns the keyboard: a global key (e.g. "p" = pull)
// is routed into the focused field, not dispatched as its normal action.
func TestCommitPopupSwallowsGlobalKeys(t *testing.T) {
	dir, repo := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "n.txt"), []byte("hi\n"), 0o644)
	gitInDir(t, dir, "add", "n.txt")

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)

	m = pressRune(t, m, "c")
	if layerOf[*commitPopup](m) == nil {
		t.Fatal("c must open the commit popup")
	}
	upd, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = upd.(Model)
	if layerOf[*commitPopup](m) == nil {
		t.Fatal("a global key must not close the popup")
	}
	if m.running {
		t.Fatal("a global key must not start an op while the popup is open")
	}
	if layerOf[*commitPopup](m).title.Value() != "p" {
		t.Fatalf("global key should type into the field: title = %q", layerOf[*commitPopup](m).title.Value())
	}
}

// gitSubj returns HEAD's subject line in dir.
func gitSubj(t *testing.T, dir string) string {
	t.Helper()
	out, _ := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%s").Output()
	return strings.TrimSpace(string(out))
}

func TestCapCKeyAmendsLastCommit(t *testing.T) {
	dir, repo := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gitInDir(t, dir, "add", "a.txt")
	gitInDir(t, dir, "commit", "-m", "original")
	// stage a further change to fold into the amend
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitInDir(t, dir, "add", "b.txt")

	countBefore := func() string {
		out, _ := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD").Output()
		return strings.TrimSpace(string(out))
	}()

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)

	// C dispatches the prefill cmd; run it and feed the resulting msg back.
	upd, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	m = upd.(Model)
	if cmd == nil {
		t.Fatal("C must dispatch the amend prefill cmd")
	}
	upd, _ = m.Update(cmd())
	m = upd.(Model)
	p := layerOf[*commitPopup](m)
	if p == nil || !p.amend {
		t.Fatal("amend prefill must open the popup in amend mode")
	}
	if p.title.Value() != "original" {
		t.Fatalf("prefill title = %q, want 'original'", p.title.Value())
	}

	// reword by appending, then commit the amend
	upd, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" reworded")})
	m = upd.(Model)
	upd, opcmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = upd.(Model)
	if layerOf[*commitPopup](m) != nil {
		t.Fatal("ctrl+s must close the popup and start the amend")
	}
	m = driveOp(t, m, opcmd)

	if got := gitSubj(t, dir); got != "original reworded" {
		t.Fatalf("subject = %q, want 'original reworded'", got)
	}
	countAfter, _ := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD").Output()
	if strings.TrimSpace(string(countAfter)) != countBefore {
		t.Fatalf("commit count = %q, want %q (amend must not add a commit)", strings.TrimSpace(string(countAfter)), countBefore)
	}
	// the staged b.txt folded into the amended commit
	if err := exec.Command("git", "-C", dir, "cat-file", "-e", "HEAD:b.txt").Run(); err != nil {
		t.Fatalf("b.txt should be in the amended commit: %v", err)
	}
}
