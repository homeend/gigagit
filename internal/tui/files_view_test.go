package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

func TestCommitFileLinesGroupsByDirectory(t *testing.T) {
	files := []model.CommitFile{
		{Status: "M", Path: "internal/tui/model.go"},
		{Status: "A", Path: "internal/engine/smart_merge.go"},
		{Status: "M", Path: "CHANGELOG.md"},
		{Status: "A", Path: "internal/tui/mark.go"},
	}
	got := commitFileLines(files)
	want := []contentLine{
		{text: "M  CHANGELOG.md"},
		{text: "internal/engine/", heading: true},
		{text: "  A  smart_merge.go"},
		{text: "internal/tui/", heading: true},
		{text: "  A  mark.go"},
		{text: "  M  model.go"},
	}
	if len(got) != len(want) {
		t.Fatalf("lines = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCommitFileLinesEmitsEachDirHeadingOnce(t *testing.T) {
	// Path-sorted these interleave dir "a" with its subdirs (a/b/f < a/c.go
	// < a/d/g < a/e.go); the dir-major sort must emit heading "a/" once.
	files := []model.CommitFile{
		{Status: "M", Path: "a/c.go"},
		{Status: "M", Path: "a/b/f.go"},
		{Status: "M", Path: "a/e.go"},
		{Status: "M", Path: "a/d/g.go"},
	}
	got := commitFileLines(files)
	count := 0
	for _, l := range got {
		if l.heading && l.text == "a/" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("heading \"a/\" emitted %d times, want 1: %+v", count, got)
	}
}

func TestCommitFileLinesRename(t *testing.T) {
	files := []model.CommitFile{{Status: "R", Path: "b/new.go", OldPath: "a/old.go"}}
	got := commitFileLines(files)
	want := []contentLine{
		{text: "b/", heading: true},
		{text: "  R  a/old.go → new.go"},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("lines = %+v, want %+v", got, want)
	}
}

func TestCommitFileLinesEmpty(t *testing.T) {
	got := commitFileLines(nil)
	if len(got) != 1 || got[0].heading || got[0].text != "(no files)" {
		t.Fatalf("lines = %+v, want one non-heading \"(no files)\"", got)
	}
}

// filesModel returns a model focused on the Commits panel whose FakeRunner
// answers diff-tree with a two-directory file list.
func filesModel() Model {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff-tree", gitexec.Result{
		Stdout: "M\tinternal/tui/model.go\nA\tCHANGELOG.md\n",
	})
	return Model{
		repo:   &git.Repo{Runner: f},
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

// openFilesView presses l and feeds the async result back into Update.
func openFilesView(t *testing.T, m Model) Model {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("l on the commits panel must fire the files load")
	}
	updated, _ = m.Update(cmd())
	return updated.(Model)
}

func TestFilesViewOpensOnCommitsPanel(t *testing.T) {
	m := openFilesView(t, filesModel())
	if m.filesView == nil {
		t.Fatal("filesView must be open")
	}
	if m.filesTitle != "Files 1111111 one" {
		t.Fatalf("title = %q", m.filesTitle)
	}
	joined := ""
	for _, l := range m.filesView.lines {
		joined += l.text + "\n"
	}
	if !strings.Contains(joined, "M  model.go") || !strings.Contains(joined, "A  CHANGELOG.md") {
		t.Fatalf("lines = %q", joined)
	}
}

func TestFilesViewNoOpOffCommitsPanel(t *testing.T) {
	m := filesModel()
	m.focus = panelBranches
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(Model)
	if m.filesView != nil || cmd != nil {
		t.Fatal("l must no-op off the commits panel")
	}
}

func TestFilesViewNoOpOnEmptyCommits(t *testing.T) {
	m := filesModel()
	m.commits = nil
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if updated.(Model).filesView != nil || cmd != nil {
		t.Fatal("l must no-op with no commits")
	}
}

func TestFilesViewNarrowTerminalNoOp(t *testing.T) {
	m := filesModel()
	m.width = 30 // layout has no left column below 40
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(Model)
	if m.filesView != nil || cmd != nil {
		t.Fatal("l must not open on a narrow terminal")
	}
	if !strings.Contains(m.statusMsg, "narrow") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

func TestFilesViewFollowsCommitSelection(t *testing.T) {
	m := openFilesView(t, filesModel())
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(Model)
	if m.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want 1 (j must keep moving commits)", m.sel[panelCommits])
	}
	if m.filesHash != "2222222bbbb" {
		t.Fatalf("filesHash = %q, want the new commit", m.filesHash)
	}
	if cmd == nil {
		t.Fatal("moving the selection must fire a follow-live reload")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.filesTitle != "Files 2222222 two" {
		t.Fatalf("title = %q after follow-live reload", m.filesTitle)
	}
}

func TestFilesViewDropsStaleResult(t *testing.T) {
	m := openFilesView(t, filesModel())
	updated, _ := m.Update(commitFilesMsg{
		hash:    "zzzstale",
		subject: "stale",
		files:   []model.CommitFile{{Status: "A", Path: "stale.txt"}},
	})
	m = updated.(Model)
	if strings.Contains(m.filesTitle, "stale") {
		t.Fatalf("stale result applied: title = %q", m.filesTitle)
	}
	for _, l := range m.filesView.lines {
		if strings.Contains(l.text, "stale.txt") {
			t.Fatal("stale result applied: lines updated")
		}
	}
}

func TestFilesViewSearchNarrowsAndKeepsHeading(t *testing.T) {
	m := openFilesView(t, filesModel())
	m = pressRune(t, m, "/")
	m = pressRune(t, m, "model")
	vis := m.filesView.visible()
	joined := ""
	for _, l := range vis {
		joined += l.text + "\n"
	}
	if !strings.Contains(joined, "internal/tui/") || !strings.Contains(joined, "model.go") {
		t.Fatalf("visible = %q, want heading + match", joined)
	}
	if strings.Contains(joined, "CHANGELOG") {
		t.Fatalf("visible = %q, must not contain non-match", joined)
	}
}

func TestFilesViewQuerySurvivesCommitChange(t *testing.T) {
	m := openFilesView(t, filesModel())
	m = pressRune(t, m, "/")
	m = pressRune(t, m, "model")
	m = pressType(t, m, tea.KeyEnter) // commit the search
	m.filesView.sel = 3               // pretend the cursor moved
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.filesView.query != "model" {
		t.Fatalf("query = %q, must survive the commit change", m.filesView.query)
	}
	if m.filesView.sel != 0 {
		t.Fatalf("sel = %d, must reset on new content", m.filesView.sel)
	}
}

func TestFilesViewEscClearsSearchThenCloses(t *testing.T) {
	m := openFilesView(t, filesModel())
	m = pressRune(t, m, "/")
	m = pressRune(t, m, "mo")
	m = pressType(t, m, tea.KeyEnter)
	m = pressType(t, m, tea.KeyEsc) // 1st esc: clear the committed search
	if m.filesView == nil || m.filesView.query != "" {
		t.Fatal("first esc must clear the search, not close")
	}
	m = pressType(t, m, tea.KeyEsc) // 2nd esc: close
	if m.filesView != nil {
		t.Fatal("second esc must close the view")
	}
}

func TestFilesViewToggleClosesOnL(t *testing.T) {
	m := openFilesView(t, filesModel())
	m = pressRune(t, m, "l")
	if m.filesView != nil {
		t.Fatal("l must toggle the view closed")
	}
}

func TestFilesViewSwallowsActionKeys(t *testing.T) {
	m := openFilesView(t, filesModel())
	for _, key := range []string{"p", "s", "m", "b", "d", "w", "o", "R", ",", "r", "?"} {
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		mm := updated.(Model)
		if cmd != nil || mm.running || mm.mark != nil || mm.contentPopup != nil {
			t.Fatalf("key %q must be swallowed while the view is open", key)
		}
	}
	before := m.focus
	m = pressType(t, m, tea.KeyTab)
	if m.focus != before || m.filesView == nil {
		t.Fatal("tab must be swallowed while the view is open")
	}
}

func TestFilesViewScrollKeys(t *testing.T) {
	// keyMsg is the existing helper in model_test.go that builds a
	// tea.KeyMsg from its String() form (it supports ctrl+up/ctrl+down).
	m := openFilesView(t, filesModel())
	u, _ := m.Update(keyMsg("ctrl+down"))
	m = u.(Model)
	if m.filesView.sel != 1 {
		t.Fatalf("sel = %d after ctrl+down, want 1", m.filesView.sel)
	}
	u, _ = m.Update(keyMsg("ctrl+up"))
	m = u.(Model)
	if m.filesView.sel != 0 {
		t.Fatalf("sel = %d after ctrl+up, want 0", m.filesView.sel)
	}
}

func TestReRootClearsFilesView(t *testing.T) {
	m := openFilesView(t, filesModel())
	updated, _ := m.reRoot(t.TempDir())
	if updated.(Model).filesView != nil {
		t.Fatal("reRoot must clear the files view")
	}
}
