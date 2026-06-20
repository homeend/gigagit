package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/repos"
)

// seededModel returns a loaded model whose statePath is a temp registry
// containing otherRepo (older) and the model's own repo (newest, via load Touch).
func seededModel(t *testing.T) (Model, string, string) {
	t.Helper()
	_, repo := newRepoDir(t)
	// The "other" entry only needs to exist on disk; a deterministic,
	// non-numeric name makes filter queries collision-proof.
	otherDir := filepath.Join(t.TempDir(), "other-zebra")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(state, otherDir, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	m := New(domain.New(repo))
	m.statePath = state
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	return m, state, otherDir
}

func TestLoadTouchesRegistry(t *testing.T) {
	m, state, _ := seededModel(t)
	entries := repos.Load(state)
	if len(entries) != 2 {
		t.Fatalf("load should have touched the registry: %+v", entries)
	}
	resolvedWant, _ := filepath.EvalSymlinks(m.currentWorktree)
	resolvedGot, _ := filepath.EvalSymlinks(entries[0].Path)
	if resolvedGot != resolvedWant {
		t.Fatalf("MRU head = %q, want the current repo %q", entries[0].Path, m.currentWorktree)
	}
}

func TestRKeyOpensPopupMRUFirst(t *testing.T) {
	m, _, otherDir := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	p := overlayOf[*repoPopup](m)
	if p == nil {
		t.Fatal("R should open the repo popup")
	}
	if len(p.entries) != 2 {
		t.Fatalf("popup entries = %+v", p.entries)
	}
	resolvedOther, _ := filepath.EvalSymlinks(otherDir)
	resolvedSecond, _ := filepath.EvalSymlinks(p.entries[1].Path)
	if resolvedSecond != resolvedOther {
		t.Fatalf("second entry = %q, want %q", p.entries[1].Path, otherDir)
	}
}

func TestPopupFilterAndSwitch(t *testing.T) {
	m, _, otherDir := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	for _, r := range "zebra" {
		u, _ = m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	p := overlayOf[*repoPopup](m)
	if got := len(p.visible()); got != 1 {
		t.Fatalf("filtered visible = %d, want 1 (query %q)", got, p.query)
	}
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if overlayOf[*repoPopup](m) != nil {
		t.Fatal("enter should close the popup")
	}
	resolvedWant, _ := filepath.EvalSymlinks(otherDir)
	resolvedGot, _ := filepath.EvalSymlinks(m.switchTarget)
	if resolvedGot != resolvedWant {
		t.Fatalf("switchTarget = %q, want %q", m.switchTarget, otherDir)
	}
}

func TestEnterOnCurrentRepoIsNoOp(t *testing.T) {
	m, _, _ := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	// Selection starts at 0 = MRU head = the current repo.
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if overlayOf[*repoPopup](m) != nil {
		t.Fatal("enter should close the popup")
	}
	if m.switchTarget != "" {
		t.Fatalf("must not re-root into the current repo, switchTarget = %q", m.switchTarget)
	}
}

func TestCtrlDRemovesEntry(t *testing.T) {
	m, state, otherDir := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("down")) // select the older (other) repo
	m = u.(Model)
	u, _ = m.Update(keyMsg("ctrl+d"))
	m = u.(Model)
	p := overlayOf[*repoPopup](m)
	if len(p.entries) != 1 {
		t.Fatalf("popup should drop the entry, got %+v", p.entries)
	}
	for _, e := range repos.Load(state) {
		resolvedE, _ := filepath.EvalSymlinks(e.Path)
		resolvedOther, _ := filepath.EvalSymlinks(otherDir)
		if resolvedE == resolvedOther {
			t.Fatal("ctrl+d must remove the entry from the state file")
		}
	}
}

func TestPopupEscCancelsAndSwallowsKeys(t *testing.T) {
	m, _, _ := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("p")) // would start SmartPull in normal mode
	m = u.(Model)
	if m.running {
		t.Fatal("popup leaked a global key")
	}
	p := overlayOf[*repoPopup](m)
	if p.query != "p" {
		t.Fatalf("typed key should filter, query = %q", p.query)
	}
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if overlayOf[*repoPopup](m) != nil {
		t.Fatal("esc should close the popup")
	}
}

func TestPopupRendersAndFits(t *testing.T) {
	m, _, _ := seededModel(t)
	m.width, m.height = 80, 24
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	out := m.View()
	if !strings.Contains(out, "Switch repository") {
		t.Fatalf("popup title missing:\n%s", out)
	}
	for i, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := lipgloss.Width(ln); w > m.width {
			t.Fatalf("line %d is %d cols, want <= %d", i, w, m.width)
		}
	}
}

func TestAgeString(t *testing.T) {
	now := time.Unix(100000, 0)
	cases := []struct {
		t    time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-3 * time.Hour), "3h ago"},
		{now.Add(-49 * time.Hour), "2d ago"},
	}
	for _, c := range cases {
		if got := ageString(now, c.t); got != c.want {
			t.Errorf("ageString(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestRepoPopupDoesNotWrapLongPath(t *testing.T) {
	m := Model{width: 80, height: 24}
	long := "/very/deeply/nested/path/that/is/way/longer/than/the/popup/box/myrepo"
	m = m.pushOverlay(&repoPopup{
		entries: []repos.Entry{{Path: long, LastOpened: time.Now()}},
		now:     time.Now(),
	})
	p := overlayOf[*repoPopup](m)
	out := p.box(m)
	// No line may exceed the terminal width.
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("popup line exceeds width (%d): %q", w, line)
		}
	}
	// The long path must occupy exactly ONE line (truncated, not wrapped onto
	// continuation lines). Only the entry row contains a path separator.
	slashLines := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "/") {
			slashLines++
		}
	}
	if slashLines != 1 {
		t.Errorf("path rendered across %d lines, want 1 (no wrap):\n%s", slashLines, out)
	}
}
