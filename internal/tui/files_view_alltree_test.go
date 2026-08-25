package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

// allFilesModel mirrors filesModel but also answers ls-tree (the full tree),
// returning a nested file so we can tell the full-tree list from the changed set.
func allFilesModel() Model {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log (commit files)", gitexec.Result{Stdout: "M\x00internal/tui/model.go\x00"})
	f.SetResponse("git ls-tree (tree files)", gitexec.Result{Stdout: "README.md\x00pkg/sub/x.go\x00internal/tui/model.go\x00"})
	return Model{
		svc:    domain.New(&git.Repo{Runner: f}),
		width:  80,
		height: 24,
		commits: []model.Commit{
			{Hash: "1111111aaaa", Subject: "one"},
			{Hash: "2222222bbbb", Subject: "two"},
		},
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		focus:     panelCommits,
	}
}

func feedFilesView(t *testing.T, m Model, key string) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return updated.(Model), cmd
}

func TestFilesViewAToggleLoadsFullTree(t *testing.T) {
	t.Parallel()
	m := openFilesView(t, allFilesModel()) // changed-files mode
	if m.inFullTree() {
		t.Fatal("files view should open in changed-files mode")
	}
	m, cmd := feedFilesView(t, m, "a")
	if !m.inFullTree() {
		t.Fatal("a should switch to full-tree mode")
	}
	if cmd == nil {
		t.Fatal("a should dispatch the tree load")
	}
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	body := strings.Join(linesText(m.filesView), "\n")
	if !strings.Contains(body, "README.md") || !strings.Contains(body, "x.go") {
		t.Fatalf("full-tree list missing files at the commit:\n%s", body)
	}
	if !strings.Contains(m.filesTitle, "(all files)") {
		t.Fatalf("title should mark full-tree mode: %q", m.filesTitle)
	}
}

func TestFilesViewAToggleBackToChanged(t *testing.T) {
	t.Parallel()
	m := openFilesView(t, allFilesModel())
	m, cmd := feedFilesView(t, m, "a") // → full tree
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	m, cmd = feedFilesView(t, m, "a") // → back to changed
	if m.inFullTree() {
		t.Fatal("second a should return to changed-files mode")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	body := strings.Join(linesText(m.filesView), "\n")
	if strings.Contains(body, "README.md") { // README is tree-only, not in the change set
		t.Fatalf("changed-files mode must not list untouched tree files:\n%s", body)
	}
}

func TestFilesViewAllFilesEnterDiffsVsWorkTree(t *testing.T) {
	t.Parallel()
	m := openFilesView(t, allFilesModel())
	m, cmd := feedFilesView(t, m, "a")
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	m.filesTreeFocused = true
	// Move the cursor onto a real file row (skip dir headings).
	for i, l := range m.filesView.visible() {
		if l.path != "" {
			m.filesView.sel = i
			break
		}
	}
	m, _ = feedFilesView(t, m, "enter")
	if m.diffLayer() == nil {
		t.Fatal("enter in full-tree mode should open a diff")
	}
	if !strings.Contains(m.diffTag, "worktree") {
		t.Fatalf("full-tree enter should diff the commit against the working tree, tag=%q", m.diffTag)
	}
}

func TestFilesViewAllFilesFollowsCommitSelection(t *testing.T) {
	t.Parallel()
	m := openFilesView(t, allFilesModel())
	m, cmd := feedFilesView(t, m, "a")
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	// Move the commit selection (list side) — full-tree mode must stick and load
	// the new commit's tree, not its change set.
	m, cmd = feedFilesView(t, m, "j")
	if m.filesHash != "2222222bbbb" {
		t.Fatalf("j should move to the next commit, got %q", m.filesHash)
	}
	if cmd == nil {
		t.Fatal("moving commits in full-tree mode should reload")
	}
	msg := cmd()
	if _, ok := msg.(treeFilesMsg); !ok {
		t.Fatalf("full-tree mode must reload the TREE on move, got %T", msg)
	}
}

// TestFilesViewWindowsLargeTreeCorrectly guards the window-then-build render: a
// huge list must show exactly the window windowStart() picks around the cursor,
// at the top, middle, and bottom — and nothing far outside it.
func TestFilesViewWindowsLargeTreeCorrectly(t *testing.T) {
	t.Parallel()
	var lines []contentLine
	for i := 0; i < 5000; i++ {
		p := fmt.Sprintf("dir%04d/file%04d.go", i, i)
		lines = append(lines, contentLine{text: p, path: p})
	}
	m := Model{width: 120, height: 50, filesTreeFocused: true,
		filesTitle: "Files x (all files)", filesView: &contentPopup{lines: lines}}
	boxW, boxH := 60, 48
	rowsCap := (boxH - 2) - 2 // contentH-2 (title+hint), no search line — mirrors renderFilesView
	for _, sel := range []int{0, 2500, 4999} {
		m.filesView.sel = sel
		out := m.renderFilesView(boxW, boxH)
		start := windowStart(len(lines), rowsCap, sel)
		for _, want := range []int{start, sel, start + rowsCap - 1} {
			if !strings.Contains(out, lines[want].text) {
				t.Fatalf("sel=%d: window row %d (%q) not rendered", sel, want, lines[want].text)
			}
		}
		if strings.Contains(out, lines[1000].text) && (1000 < start || 1000 >= start+rowsCap) {
			t.Fatalf("sel=%d: row 1000 outside the window [%d,%d) was rendered", sel, start, start+rowsCap)
		}
	}
}

// linesText extracts the row text of a content popup for assertions.
func linesText(p *contentPopup) []string {
	out := make([]string, 0, len(p.lines))
	for _, l := range p.lines {
		out = append(out, l.text)
	}
	return out
}
