package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/repos"
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
	p := layerOf[*repoPopup](m)
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
	// Navigation-first: press / to start filtering, then type.
	u, _ = m.Update(keyMsg("/"))
	m = u.(Model)
	for _, r := range "zebra" {
		u, _ = m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	p := layerOf[*repoPopup](m)
	if !p.filtering {
		t.Fatal("/ should enter filter mode")
	}
	if got := len(p.visible()); got != 1 {
		t.Fatalf("filtered visible = %d, want 1 (query %q)", got, p.query)
	}
	// First enter locks the filter (leaves input mode, popup stays open).
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if p := layerOf[*repoPopup](m); p == nil || p.filtering {
		t.Fatalf("first enter should lock the filter and keep the popup open; p=%v", p)
	}
	// Second enter switches to the single filtered repo.
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if layerOf[*repoPopup](m) != nil {
		t.Fatal("second enter should close the popup")
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
	if layerOf[*repoPopup](m) != nil {
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
	p := layerOf[*repoPopup](m)
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
	// Navigation-first: a plain letter is swallowed but does NOT filter (you press
	// / first). This is also what fixes the z-collision (z cycles mode, not query).
	p := layerOf[*repoPopup](m)
	if p.query != "" || p.filtering {
		t.Fatalf("plain key must not filter; query = %q filtering = %v", p.query, p.filtering)
	}
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if layerOf[*repoPopup](m) != nil {
		t.Fatal("esc should close the popup")
	}
}

// TestRepoPopupSlashFilterAndZNotCollision pins the navigation-first contract for
// the repo switcher: / enters filter mode where `z` is a literal query character
// (in nav mode z cycles the display mode), and arrows move the selection while
// typing — the same model as the finder and bookmark/shelf switchers.
func TestRepoPopupSlashFilterAndZNotCollision(t *testing.T) {
	m, _, _ := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	// In nav mode, z cycles the display mode (not a query char).
	p := layerOf[*repoPopup](m)
	mode0 := p.mode
	u, _ = m.Update(keyMsg("z"))
	m = u.(Model)
	p = layerOf[*repoPopup](m)
	if p.mode == mode0 || p.query != "" {
		t.Fatalf("nav-mode z should cycle display mode, not type a query; mode==%v query=%q", p.mode, p.query)
	}
	// / then a z-containing query types literally.
	u, _ = m.Update(keyMsg("/"))
	m = u.(Model)
	for _, r := range "zeb" {
		u, _ = m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	p = layerOf[*repoPopup](m)
	if p.query != "zeb" {
		t.Fatalf("/zeb should type literally in filter mode; query=%q", p.query)
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
	m = m.pushLayer(&repoPopup{
		entries: []repos.Entry{{Path: long, LastOpened: time.Now()}},
		now:     time.Now(),
	})
	p := layerOf[*repoPopup](m)
	out := p.box(m)
	// No line may exceed the terminal width.
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("popup line exceeds width (%d): %q", w, line)
		}
	}
	// The long path must occupy exactly ONE line (truncated, not wrapped onto
	// continuation lines). Match a path-specific token (the header's `/` hint and
	// the `[/] filter` footer also contain a slash, so a bare "/" over-counts).
	pathLines := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "/very") {
			pathLines++
		}
	}
	if pathLines != 1 {
		t.Errorf("path rendered across %d lines, want 1 (no wrap):\n%s", pathLines, out)
	}
}

func TestRepoPopupMaximizeWidensAndLiftsRowCap(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &repoPopup{}
	for i := 0; i < 30; i++ { // more than the fixed cap of 12
		p.entries = append(p.entries, repos.Entry{Path: fmt.Sprintf("/home/user/repos/project-number-%d", i)})
	}

	normal := p.box(m)
	p.maximized = true
	maxed := p.box(m)

	if lipgloss.Width(maxed) <= lipgloss.Width(normal) {
		t.Fatalf("maximized width %d must exceed normal %d", lipgloss.Width(maxed), lipgloss.Width(normal))
	}
	if lipgloss.Height(maxed) <= lipgloss.Height(normal) {
		t.Fatalf("maximized must show more rows: height %d vs %d", lipgloss.Height(maxed), lipgloss.Height(normal))
	}
}

func TestRepoPopupTKeyDoesNotMaximizeWhileFiltering(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &repoPopup{filtering: true}
	p.update(m, runeKey("T"))
	if p.maximized {
		t.Fatal(`"T" while filtering must not maximize`)
	}
	if p.query != "T" {
		t.Fatalf(`"T" while filtering must be a literal char; query=%q`, p.query)
	}
}
