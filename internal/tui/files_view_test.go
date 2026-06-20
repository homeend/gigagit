package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/gigagit/gg/internal/domain"
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
		{text: "M  CHANGELOG.md", path: "CHANGELOG.md", status: "M"},
		{text: "internal/engine/", heading: true},
		{text: "  A  smart_merge.go", path: "internal/engine/smart_merge.go", status: "A"},
		{text: "internal/tui/", heading: true},
		{text: "  A  mark.go", path: "internal/tui/mark.go", status: "A"},
		{text: "  M  model.go", path: "internal/tui/model.go", status: "M"},
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
		{text: "  R  a/old.go → new.go", path: "b/new.go", oldPath: "a/old.go", status: "R"},
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
// answers the commit-files query with a two-directory file list.
func filesModel() Model {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log (commit files)", gitexec.Result{
		Stdout: "M\tinternal/tui/model.go\nA\tCHANGELOG.md\n",
	})
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
	m = pressType(t, m, tea.KeyLeft) // focus the tree so / filters it, not commits
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

// With the commit list focused (the view opens this way), / must filter the
// COMMITS, not the file tree — routing to the base panel filter.
func TestFilesViewSlashFiltersCommitsWhenListFocused(t *testing.T) {
	m := openFilesView(t, filesModel()) // opens commit-list focused
	m = pressRune(t, m, "/")
	if !m.filterTyping || m.filterPanel != panelCommits {
		t.Fatalf("/ on the list side must start the commit filter: typing=%v panel=%v", m.filterTyping, m.filterPanel)
	}
	m = pressRune(t, m, "two")
	if m.filterQuery != "two" {
		t.Fatalf("filterQuery = %q, want the typed commit query", m.filterQuery)
	}
	if m.filesView.query != "" || m.filesView.typing {
		t.Fatalf("the tree filter must stay untouched: query=%q typing=%v", m.filesView.query, m.filesView.typing)
	}
}

// With the tree focused, / must filter the file tree, not the commits.
func TestFilesViewSlashFiltersTreeWhenTreeFocused(t *testing.T) {
	m := openFilesView(t, filesModel())
	u, _ := m.Update(keyMsg("left")) // focus the tree
	m = u.(Model)
	m = pressRune(t, m, "/")
	if !m.filesView.typing {
		t.Fatal("/ on the tree side must start the tree filter")
	}
	m = pressRune(t, m, "model")
	if m.filesView.query != "model" {
		t.Fatalf("tree query = %q, want the typed file query", m.filesView.query)
	}
	if m.filterTyping || m.filterQuery != "" {
		t.Fatalf("the commit filter must stay untouched: typing=%v query=%q", m.filterTyping, m.filterQuery)
	}
}

// Committing a commit filter (enter) must sync the tree to the now-selected
// commit so "search commits → see its files" needs no extra keypress.
func TestFilesViewCommitFilterSyncsTree(t *testing.T) {
	m := openFilesView(t, filesModel()) // opens on commit 1111111 "one"
	m = pressRune(t, m, "/")
	m = pressRune(t, m, "two") // narrows to commit 2222222 "two" at filtered index 0
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.filterTyping {
		t.Fatal("enter must commit the filter")
	}
	if cmd == nil {
		t.Fatal("committing a commit filter must fire a tree reload for the selected commit")
	}
	if m.filesHash != "2222222bbbb" {
		t.Fatalf("filesHash = %q, want the filtered commit", m.filesHash)
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.filesTitle != "Files 2222222 two" {
		t.Fatalf("title = %q after the sync reload", m.filesTitle)
	}
}

// The literal thing the user sees: with the files view open and the commit
// list focused, typing a commit filter narrows the right column (label + rows)
// while the left tree is untouched.
func TestFilesViewCommitFilterRendersInRightColumn(t *testing.T) {
	m := openFilesView(t, filesModel())
	m = pressRune(t, m, "/")
	m = pressRune(t, m, "two")
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "/two█") {
		t.Fatalf("right column must show the commit filter label:\n%s", out)
	}
	if !strings.Contains(out, "2222222 two") {
		t.Fatalf("the commit list must show the match:\n%s", out)
	}
	// Commit "one" is filtered out of the list — "1111111 one" survives only as
	// the (untouched) tree title, so it must appear exactly once.
	if n := strings.Count(out, "1111111 one"); n != 1 {
		t.Fatalf("commit \"one\" must drop from the list (count=%d, want 1):\n%s", n, out)
	}
	if !strings.Contains(out, "Files 1111111 one") || !strings.Contains(out, "model.go") {
		t.Fatalf("the file tree must stay put while the commits filter:\n%s", out)
	}
}

func TestFilesViewQuerySurvivesCommitChange(t *testing.T) {
	m := openFilesView(t, filesModel())
	m = pressType(t, m, tea.KeyLeft) // focus the tree to filter it
	m = pressRune(t, m, "/")
	m = pressRune(t, m, "model")
	m = pressType(t, m, tea.KeyEnter) // commit the search
	m = pressType(t, m, tea.KeyRight) // back to the commit list to move it
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
	m = pressType(t, m, tea.KeyLeft) // focus the tree so / filters it
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

// q no longer quits from the files view — only the base layout quits on q.
func TestFilesViewQInert(t *testing.T) {
	m := openFilesView(t, filesModel())
	if m.filesView == nil {
		t.Fatal("precondition: files view should be open")
	}
	u, cmd := m.Update(keyMsg("q"))
	if cmd != nil {
		t.Fatal("q must not quit from the files view (inert)")
	}
	if u.(Model).filesView == nil {
		t.Fatal("q must leave the files view open")
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
	for _, key := range []string{"p", "s", "m", "d", "w", "o", "R", ",", "r", "?"} {
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		mm := updated.(Model)
		if cmd != nil || mm.running || mm.mark != nil || layerOf[*contentPopup](mm) != nil {
			t.Fatalf("key %q must be swallowed while the view is open", key)
		}
	}
	// tab is no longer swallowed — it toggles focus (covered by
	// TestFilesViewArrowsAndTabSwitchFocus); m.focus itself must never move.
	m = pressType(t, m, tea.KeyTab)
	if m.focus != panelCommits || m.filesView == nil {
		t.Fatal("tab inside the view must not move m.focus off the commits panel")
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

func TestFilesViewRenderReplacesLeftColumn(t *testing.T) {
	m := openFilesView(t, filesModel())
	out := m.render()
	for _, want := range []string{
		"Files 1111111 one", // title
		"internal/tui/",     // directory heading
		"M  model.go",       // file row with status letter
		"[/] search",        // hint line
		"Commits",           // the right panel is still there
		"2222222 two",       // and still lists commits
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
	for _, gone := range []string{"Branches", "Worktrees", "Status"} {
		if strings.Contains(out, gone) {
			t.Fatalf("render still shows the %s panel:\n%s", gone, out)
		}
	}
}

func TestFilesViewRenderShowsSearchQuery(t *testing.T) {
	m := openFilesView(t, filesModel())
	m = pressType(t, m, tea.KeyLeft) // focus the tree so / filters it
	m = pressRune(t, m, "/")
	m = pressRune(t, m, "mo")
	out := m.render()
	if !strings.Contains(out, "/mo█") {
		t.Fatalf("render missing the typing-mode query cursor:\n%s", out)
	}
}

func TestFilesViewRenderFitsTerminal(t *testing.T) {
	m := openFilesView(t, filesModel())
	out := m.render()
	lines := strings.Split(out, "\n")
	if len(lines) > 24 {
		t.Fatalf("render = %d lines, must fit height 24", len(lines))
	}
}

func TestFilesViewClosesOnNarrowResize(t *testing.T) {
	m := openFilesView(t, filesModel())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 30, Height: 24})
	m = updated.(Model)
	if m.filesView != nil {
		t.Fatal("resizing below 40 cols must close the files view")
	}
	if !strings.Contains(m.statusMsg, "narrow") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}

	m2 := openFilesView(t, filesModel())
	updated, _ = m2.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if updated.(Model).filesView == nil {
		t.Fatal("a wide resize must keep the view open")
	}
}

func TestFilesViewNoOpWhileRunningOrLoading(t *testing.T) {
	m := filesModel()
	m.running = true
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if updated.(Model).filesView != nil || cmd != nil {
		t.Fatal("l must no-op while an operation is running")
	}
	m = filesModel()
	m.loading = true
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if updated.(Model).filesView != nil || cmd != nil {
		t.Fatal("l must no-op while loading")
	}
}

func TestFilesViewLoadErrorKeepsViewAndSetsStatus(t *testing.T) {
	m := filesModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(Model)
	updated, _ = m.Update(commitFilesMsg{hash: m.filesHash, err: fmt.Errorf("boom")})
	m = updated.(Model)
	if m.filesView == nil {
		t.Fatal("a load error must not close the view")
	}
	if !strings.Contains(m.statusMsg, "boom") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
	if len(m.filesView.lines) != 1 || m.filesView.lines[0].text != "(load failed)" {
		t.Fatalf("lines = %+v, want the failed placeholder", m.filesView.lines)
	}
}

func TestFooterShowsFilesBindingOnCommitsPanel(t *testing.T) {
	m := filesModel()
	if f := m.footerLine(); !strings.Contains(f, "[l] files") {
		t.Fatalf("footer = %q, must advertise [l] files on the commits panel", f)
	}
	m.focus = panelBranches
	if f := m.footerLine(); strings.Contains(f, "[l] files") {
		t.Fatalf("footer = %q, must not advertise [l] files off the commits panel", f)
	}
	m.focus = panelCommits
	m.width = 30
	if f := m.footerLine(); strings.Contains(f, "[l] files") {
		t.Fatalf("footer = %q, must not advertise [l] files on a narrow terminal", f)
	}
}

func TestFooterSwitchesToFilesViewMode(t *testing.T) {
	m := openFilesView(t, filesModel())
	f := m.footerLine()
	if !strings.Contains(f, "[esc/l] close") {
		t.Fatalf("footer = %q, must show the files-view keys", f)
	}
	if strings.Contains(f, "[p]ull") {
		t.Fatalf("footer = %q, must not advertise swallowed keys", f)
	}
}

func TestFilesViewArrowsAndTabSwitchFocus(t *testing.T) {
	m := openFilesView(t, filesModel())
	if m.filesTreeFocused {
		t.Fatal("the view must open with the commit list focused")
	}
	u, _ := m.Update(keyMsg("left"))
	m = u.(Model)
	if !m.filesTreeFocused {
		t.Fatal("left must focus the tree")
	}
	u, _ = m.Update(keyMsg("left")) // already leftmost: no-op
	m = u.(Model)
	if !m.filesTreeFocused {
		t.Fatal("left on the tree must keep it focused")
	}
	u, _ = m.Update(keyMsg("right"))
	m = u.(Model)
	if m.filesTreeFocused {
		t.Fatal("right must focus the commit list")
	}
	u, _ = m.Update(keyMsg("tab"))
	m = u.(Model)
	if !m.filesTreeFocused {
		t.Fatal("tab must toggle to the tree")
	}
	u, _ = m.Update(keyMsg("shift+tab"))
	m = u.(Model)
	if m.filesTreeFocused {
		t.Fatal("shift+tab must toggle back to the commit list")
	}
}

func TestFilesViewMovementFollowsFocus(t *testing.T) {
	m := openFilesView(t, filesModel())
	// Commits focused: j moves the commit selection and fires a reload.
	u, cmd := m.Update(keyMsg("j"))
	m = u.(Model)
	if m.sel[panelCommits] != 1 || cmd == nil {
		t.Fatalf("sel = %d cmd-nil=%v, want commit move + reload", m.sel[panelCommits], cmd == nil)
	}
	u, _ = m.Update(cmd())
	m = u.(Model)
	// Tree focused: j moves the tree cursor, not the commit selection.
	u, _ = m.Update(keyMsg("left"))
	m = u.(Model)
	u, cmd = m.Update(keyMsg("j"))
	m = u.(Model)
	if m.filesView.sel != 1 {
		t.Fatalf("tree sel = %d after j, want 1", m.filesView.sel)
	}
	if m.sel[panelCommits] != 1 || cmd != nil {
		t.Fatal("j with the tree focused must not touch commits or fire a reload")
	}
}

func TestFilesViewPagingFollowsFocus(t *testing.T) {
	m := openFilesView(t, filesModel())
	// Commits focused: pgdown pages the commit selection via ONE reload.
	u, cmd := m.Update(keyMsg("pgdown"))
	m = u.(Model)
	if m.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d after pgdown, want 1 (clamped page jump)", m.sel[panelCommits])
	}
	if cmd == nil {
		t.Fatal("paging commits must fire a follow-live reload")
	}
	u, _ = m.Update(cmd())
	m = u.(Model)
	// Tree focused: pgdown pages the tree.
	u, _ = m.Update(keyMsg("left"))
	m = u.(Model)
	before := m.sel[panelCommits]
	u, cmd = m.Update(keyMsg("pgdown"))
	m = u.(Model)
	if m.filesView.sel == 0 {
		t.Fatal("pgdown with the tree focused must move the tree cursor")
	}
	if m.sel[panelCommits] != before || cmd != nil {
		t.Fatal("pgdown with the tree focused must not touch commits")
	}
}

func TestFilesViewCtrlArrowsAlwaysScrollTree(t *testing.T) {
	m := openFilesView(t, filesModel())
	u, _ := m.Update(keyMsg("ctrl+down")) // commits focused
	m = u.(Model)
	if m.filesView.sel != 1 {
		t.Fatalf("tree sel = %d, ctrl+down must scroll the tree from the commits side", m.filesView.sel)
	}
	u, _ = m.Update(keyMsg("left")) // tree focused
	m = u.(Model)
	u, _ = m.Update(keyMsg("ctrl+down"))
	m = u.(Model)
	if m.filesView.sel != 2 {
		t.Fatalf("tree sel = %d, ctrl+down must scroll the tree from the tree side too", m.filesView.sel)
	}
}

func TestFilesViewCloseResetsTreeFocus(t *testing.T) {
	for _, close := range []string{"l", "esc"} {
		m := openFilesView(t, filesModel())
		u, _ := m.Update(keyMsg("left"))
		m = u.(Model)
		u, _ = m.Update(keyMsg(close))
		m = u.(Model)
		if m.filesView != nil {
			t.Fatalf("%s must close the view", close)
		}
		m = openFilesView(t, m)
		if m.filesTreeFocused {
			t.Fatalf("reopening after %s-close must start commits-focused", close)
		}
	}
}

func TestFilesViewNarrowResizeResetsTreeFocus(t *testing.T) {
	m := openFilesView(t, filesModel())
	u, _ := m.Update(keyMsg("left"))
	m = u.(Model)
	u, _ = m.Update(tea.WindowSizeMsg{Width: 30, Height: 24})
	m = u.(Model)
	if m.filesView != nil || m.filesTreeFocused {
		t.Fatal("the narrow auto-close must clear the view AND the tree focus")
	}
}

func TestFilesViewFocusIsVisible(t *testing.T) {
	m := openFilesView(t, filesModel())
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "> 1111111 one") {
		t.Fatalf("commits focused: the selected commit must carry the > prefix:\n%s", out)
	}
	u, _ := m.Update(keyMsg("left"))
	m = u.(Model)
	out = ansi.Strip(m.render())
	if strings.Contains(out, "> 1111111 one") {
		t.Fatalf("tree focused: the commits row must lose the > prefix:\n%s", out)
	}
	if !strings.Contains(out, "> ") {
		t.Fatalf("tree focused: the tree cursor must still render:\n%s", out)
	}
}

func TestPanelFocusedRespectsTreeFocus(t *testing.T) {
	m := openFilesView(t, filesModel())
	if !m.panelFocused(panelCommits) {
		t.Fatal("commits must read focused while the commit side is active")
	}
	u, _ := m.Update(keyMsg("left"))
	m = u.(Model)
	if m.panelFocused(panelCommits) {
		t.Fatal("commits must read blurred while the tree side is active")
	}
	u, _ = m.Update(keyMsg("right"))
	if !u.(Model).panelFocused(panelCommits) {
		t.Fatal("right must hand focus back to commits")
	}
}

func TestTooltipSuppressedWhileTreeFocused(t *testing.T) {
	m := filesModel()
	m.commits[0].Subject = strings.Repeat("x", 200) // force row truncation
	m = openFilesView(t, m)
	if _, _, _, ok := m.tooltip(); !ok {
		t.Fatal("setup: tooltip expected for the truncated commit row")
	}
	u, _ := m.Update(keyMsg("left"))
	m = u.(Model)
	if _, _, _, ok := m.tooltip(); ok {
		t.Fatal("tooltip must be suppressed while the tree is focused")
	}
}

func TestCommitFileLinesCarryPayload(t *testing.T) {
	files := []model.CommitFile{
		{Status: "M", Path: "root.txt"},
		{Status: "R", Path: "pkg/new.go", OldPath: "pkg/old.go"},
		{Status: "A", Path: "pkg/added.go"},
	}
	lines := commitFileLines(files)
	byPath := map[string]contentLine{}
	for _, l := range lines {
		if l.path != "" {
			byPath[l.path] = l
		}
		if l.heading && (l.path != "" || l.oldPath != "" || l.status != "") {
			t.Fatalf("heading row must carry no payload: %+v", l)
		}
	}
	if len(byPath) != 3 {
		t.Fatalf("expected 3 payload rows, got %d", len(byPath))
	}
	if l := byPath["root.txt"]; l.status != "M" || l.oldPath != "" {
		t.Fatalf("root.txt payload wrong: %+v", l)
	}
	if l := byPath["pkg/new.go"]; l.status != "R" || l.oldPath != "pkg/old.go" {
		t.Fatalf("rename payload wrong: %+v", l)
	}
	if l := byPath["pkg/added.go"]; l.status != "A" {
		t.Fatalf("added payload wrong: %+v", l)
	}
}

func TestPayloadSurvivesFilter(t *testing.T) {
	files := []model.CommitFile{
		{Status: "M", Path: "pkg/match_me.go"},
		{Status: "M", Path: "pkg/other.go"},
	}
	p := &contentPopup{lines: commitFileLines(files), query: "match"}
	vis := p.visible()
	var found bool
	for _, l := range vis {
		if l.path == "pkg/match_me.go" {
			found = true
		}
	}
	if !found {
		t.Fatal("filtered visible() lost the payload row")
	}
}

func TestFilesViewWrapMode(t *testing.T) {
	m := New(nil)
	m.width, m.height = 100, 30
	p := &contentPopup{lines: []contentLine{{text: strings.Repeat("q", 80), path: "x"}}, mode: modeWrap}
	m.filesView = p
	m.filesTitle = "Files"
	out := m.renderFilesView(20, 12)
	if strings.Count(out, "q") < 40 {
		t.Errorf("files view wrap mode did not expand the long row:\n%s", out)
	}
}
