package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

func branchesPanelModel(names ...string) Model {
	m := footerModel()
	m.branches = nil // overwrite footerModel's seed so sel 0 = names[0]
	for _, n := range names {
		m.branches = append(m.branches, model.Branch{Name: n})
	}
	m.focus = panelBranches
	m.sel[panelBranches] = 0
	return m
}

func TestCommitSoloSetsAndClearsScope(t *testing.T) {
	m := branchesPanelModel("feat", "main")
	r, ok := findRow(availableActions(m), "commits-solo")
	if !ok {
		t.Fatalf("Solo this branch missing on Branches panel")
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 1 || m.commitScopeBranches[0] != "feat" {
		t.Fatalf("solo should scope to feat, got %v", m.commitScopeBranches)
	}
	// Re-solo the same branch → un-solo (back to all).
	r2, _ := findRow(availableActions(m), "commits-solo")
	mm, _ = r2.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 0 {
		t.Fatalf("re-solo should clear scope, got %v", m.commitScopeBranches)
	}
}

func TestCommitShowAllVisibilityAndClear(t *testing.T) {
	m := branchesPanelModel("feat")
	if _, ok := findRow(availableActions(m), "commits-showall"); ok {
		t.Fatalf("Show all should be absent in all-mode")
	}
	m.commitScopeBranches = []string{"feat"}
	r, ok := findRow(availableActions(m), "commits-showall")
	if !ok {
		t.Fatalf("Show all should be present when scoped")
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 0 {
		t.Fatalf("show-all should clear scope")
	}
}

func TestCommitShowAllOnCommitsPanel(t *testing.T) {
	m := footerModel()
	m.focus = panelCommits
	m.commitScopeBranches = []string{"feat"}
	if _, ok := findRow(availableActions(m), "commits-showall"); !ok {
		t.Fatalf("Show all should be offered from the Commits panel menu when scoped")
	}
}

// TestCommitSoloReloadEndToEnd drives the full chain: the menu row's run handler
// returns a reload cmd; executing it reloads the (scoped) feed; the resulting
// commitsReloadedMsg flows back through Update and paints the commits.
func TestCommitSoloReloadEndToEnd(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: "h1\x1f\x1fAda\x1f0\x1fsubject\x1fHEAD -> feat\n"})
	svc := domain.New(&git.Repo{Runner: f})
	m := branchesPanelModel("feat")
	m.svc = svc
	m.feed = svc.CommitFeed()

	r, ok := findRow(availableActions(m), "commits-solo")
	if !ok {
		t.Fatal("Solo this branch missing")
	}
	mm, cmd := r.run(m)
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("solo should return a reload cmd")
	}
	msg := cmd() // executes reloadFeedCmd → feed.LoadInitial against the fake
	mm, _ = m.Update(msg)
	m = mm.(Model)
	if len(m.commits) != 1 || m.commits[0].Hash != "h1" {
		t.Fatalf("after solo reload, commits = %+v", m.commits)
	}
	if m.commitScopeLabel() != "solo: feat" {
		t.Fatalf("scope label = %q", m.commitScopeLabel())
	}
	rows := m.commitRows()
	if len(rows) != 1 || !strings.Contains(rows[0], "*feat") {
		t.Fatalf("commit row should carry the head-branch identity (*feat): %q", rows)
	}
}

func TestClearFilterRowPresentOnlyWhenFiltered(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelCommits
	if _, ok := m.commitClearFilterRow(); ok {
		t.Fatal("no clear-filter row when unfiltered")
	}
	m.commitFilter = commitFilterFields{Grep: "race"}
	row, ok := m.commitClearFilterRow()
	if !ok {
		t.Fatal("clear-filter row should appear when filtered")
	}
	mm, _ := row.run(m)
	if mm.(Model).commitFilter.filtered() {
		t.Fatal("running clear-filter must empty the filter")
	}
}

func TestCtrlRClearsOnlyFocusedWindow(t *testing.T) {
	// A `/` filter bound to the Branches panel, plus an @ highlight and a `\`
	// commit filter on the Commits panel.
	base := func() Model {
		m := loadedModel(t)
		m.filterPanel = panelBranches
		m.filterQuery = "foo"
		m.highlightQuery = "bar"
		m.commitFilter = commitFilterFields{Grep: "baz"}
		return m
	}

	// Focused on Commits → clears the @ highlight and the commit filter, but
	// leaves the Branches `/` filter alone.
	m := base()
	m.focus = panelCommits
	c1, _ := m.Update(keyMsg("ctrl+r"))
	mm := c1.(Model)
	if mm.commitFilter.filtered() {
		t.Error("ctrl+r on Commits should clear the commit filter")
	}
	if mm.highlightQuery != "" {
		t.Error("ctrl+r on Commits should clear the @ highlight")
	}
	if mm.filterQuery != "foo" {
		t.Errorf("ctrl+r on Commits must NOT clear another window's / filter, got %q", mm.filterQuery)
	}
	if !mm.commitsLoading {
		t.Error("clearing an active commit filter should reload the feed")
	}

	// Focused on Branches → clears only the Branches `/` filter; the Commits
	// commit filter and @ highlight survive.
	m = base()
	m.focus = panelBranches
	c2, _ := m.Update(keyMsg("ctrl+r"))
	mm = c2.(Model)
	if mm.filterQuery != "" {
		t.Errorf("ctrl+r on Branches should clear its / filter, got %q", mm.filterQuery)
	}
	if !mm.commitFilter.filtered() {
		t.Error("ctrl+r on Branches must NOT clear the Commits commit filter")
	}
	if mm.highlightQuery != "bar" {
		t.Errorf("ctrl+r on Branches must NOT clear the Commits @ highlight, got %q", mm.highlightQuery)
	}
	if mm.commitsLoading {
		t.Error("clearing a non-Commits filter should not reload the feed")
	}
}

func TestCanClearFiltersGating(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelCommits
	if m.canClearFilters() {
		t.Fatal("nothing filtered → no clear hint")
	}
	m.highlightQuery = "x"
	if !m.canClearFilters() {
		t.Fatal("an active @ highlight on the focused Commits panel should enable the hint")
	}

	// A `/` filter bound to another window must not enable the hint for the
	// focused panel.
	m2 := loadedModel(t)
	m2.focus = panelCommits
	m2.filterPanel = panelBranches
	m2.filterQuery = "y"
	if m2.canClearFilters() {
		t.Fatal("another window's / filter must not enable the focused window's clear hint")
	}
}

func TestCommitToggleAddsBranch(t *testing.T) {
	m := branchesPanelModel("feat", "main")
	r, ok := findRow(availableActions(m), "commits-toggle")
	if !ok {
		t.Fatalf("toggle action missing on Branches panel")
	}
	if r.label != "Add to commit view" {
		t.Fatalf("label for unselected branch = %q, want Add to commit view", r.label)
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 1 || m.commitScopeBranches[0] != "feat" {
		t.Fatalf("toggle-add should scope to [feat], got %v", m.commitScopeBranches)
	}
}

func TestCommitToggleRemovesBranch(t *testing.T) {
	m := branchesPanelModel("feat", "main")
	m.commitScopeBranches = []string{"feat", "main"}
	r, ok := findRow(availableActions(m), "commits-toggle")
	if !ok {
		t.Fatalf("toggle action missing")
	}
	if r.label != "Remove from commit view" {
		t.Fatalf("label for selected branch = %q, want Remove from commit view", r.label)
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 1 || m.commitScopeBranches[0] != "main" {
		t.Fatalf("toggle-remove should leave [main], got %v", m.commitScopeBranches)
	}
}

func TestCommitToggleRemoveLastReturnsToAll(t *testing.T) {
	m := branchesPanelModel("feat")
	m.commitScopeBranches = []string{"feat"}
	r, _ := findRow(availableActions(m), "commits-toggle")
	mm, _ := r.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 0 {
		t.Fatalf("removing the last branch should clear scope, got %v", m.commitScopeBranches)
	}
	if m.commitScopeLabel() != "all" {
		t.Fatalf("empty scope label = %q, want all", m.commitScopeLabel())
	}
}

func TestCommitScopeLabel(t *testing.T) {
	m := footerModel()
	if m.commitScopeLabel() != "all" {
		t.Fatalf("empty scope label = %q, want all", m.commitScopeLabel())
	}
	m.commitScopeBranches = []string{"feat"}
	if m.commitScopeLabel() != "solo: feat" {
		t.Fatalf("solo label = %q", m.commitScopeLabel())
	}
}

func TestBranchRowsMarkAllScopedBranches(t *testing.T) {
	m := branchesPanelModel("a", "b", "c")
	m.commitScopeBranches = []string{"a", "c"}
	rows := m.branchRows()
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	if !strings.Contains(rows[0], "◉") {
		t.Fatalf("row a should be marked: %q", rows[0])
	}
	if strings.Contains(rows[1], "◉") {
		t.Fatalf("row b should NOT be marked: %q", rows[1])
	}
	if !strings.Contains(rows[2], "◉") {
		t.Fatalf("row c should be marked: %q", rows[2])
	}
}

// TestCommitToggleReloadEndToEnd drives toggle → reload cmd → commitsReloadedMsg
// → Update, and confirms the multi-branch label paints.
func TestCommitToggleReloadEndToEnd(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: "h1\x1f\x1fAda\x1f0\x1fsubject\x1fHEAD -> feat\n"})
	svc := domain.New(&git.Repo{Runner: f})
	m := branchesPanelModel("feat", "main")
	m.svc = svc
	m.feed = svc.CommitFeed()
	m.commitScopeBranches = []string{"main"} // pre-existing one-branch set

	r, ok := findRow(availableActions(m), "commits-toggle")
	if !ok {
		t.Fatal("toggle row missing")
	}
	if r.label != "Add to commit view" {
		t.Fatalf("feat not in set → label = %q, want Add to commit view", r.label)
	}
	mm, cmd := r.run(m)
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("toggle should return a reload cmd")
	}
	msg := cmd()
	mm, _ = m.Update(msg)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 2 {
		t.Fatalf("set should now hold 2 branches, got %v", m.commitScopeBranches)
	}
	if m.commitScopeLabel() != "2 branches" {
		t.Fatalf("scope label = %q, want 2 branches", m.commitScopeLabel())
	}
	if len(m.commits) != 1 || m.commits[0].Hash != "h1" {
		t.Fatalf("after toggle reload, commits = %+v", m.commits)
	}
}

func TestCommitRowsRenderIdentityColumn(t *testing.T) {
	m := footerModel()
	m.commits = []model.Commit{
		{Hash: "a1b2c3d4", Subject: "do a thing", Refs: []model.Ref{
			{Name: "main", Kind: model.RefLocal, Head: true},
			{Name: "feature", Kind: model.RefLocal},
			{Name: "origin/main", Kind: model.RefRemote},
		}},
		{Hash: "ffff0000", Subject: "plain", Source: "dev"},
	}
	rows := m.commitRows()
	// Tip of main(head)+feature: *main in the identity column, feature as a pill.
	if !strings.Contains(rows[0], "*main") || !strings.Contains(rows[0], "feature") {
		t.Fatalf("row0 should show *main (column) + feature (pill): %q", rows[0])
	}
	// The commit id no longer appears in the row — it moved to the status bar.
	if strings.Contains(rows[0], "a1b2c3d") {
		t.Fatalf("commit id must not appear in the row: %q", rows[0])
	}
	if strings.Contains(rows[0], "origin/main") {
		t.Fatalf("remote labels not rendered: %q", rows[0])
	}
	// Lineage row: its Source branch fills the column (grayed at render time); no pills.
	if !strings.Contains(rows[1], "dev") {
		t.Fatalf("lineage row should show its Source branch: %q", rows[1])
	}
	if strings.Contains(rows[1], "‹") {
		t.Fatalf("a single-identity row has no pills: %q", rows[1])
	}
}

func TestBranchRowsGutterOneColumnWhenNoSet(t *testing.T) {
	m := branchesPanelModel("main", "feat")
	m.branches[0].IsHead = true // main is head
	rows := m.branchRows()
	// No set active → 1-column gutter (head only), same as before.
	if !strings.HasPrefix(rows[0], "* main") {
		t.Fatalf("head row = %q, want '* main' prefix", rows[0])
	}
	if !strings.HasPrefix(rows[1], "  feat") {
		t.Fatalf("non-head row = %q, want '  feat' prefix", rows[1])
	}
}

func TestBranchRowsGutterTwoColumnsWhenSetActive(t *testing.T) {
	m := branchesPanelModel("main", "feat")
	m.branches[0].IsHead = true
	m.commitScopeBranches = []string{"feat"} // feat in the set
	rows := m.branchRows()
	// Set active → 2-column gutter [set][head] + separator. Names aligned at col 3.
	if !strings.HasPrefix(rows[0], " * main") { // head, not in set
		t.Fatalf("main row = %q, want ' * main' prefix", rows[0])
	}
	if !strings.HasPrefix(rows[1], "◉  feat") { // in set, not head
		t.Fatalf("feat row = %q, want '◉  feat' prefix", rows[1])
	}
}

func TestBranchRowsSetMarkerIsOnTheLeft(t *testing.T) {
	m := branchesPanelModel("feat")
	m.commitScopeBranches = []string{"feat"}
	row := m.branchRows()[0]
	// The fix: ◉ precedes the name and the row no longer ENDS with ◉.
	if strings.HasSuffix(row, "◉") {
		t.Fatalf("set marker must not be a right-hand suffix: %q", row)
	}
	dot := strings.IndexRune(row, '◉')
	name := strings.Index(row, "feat")
	if dot < 0 || dot >= name {
		t.Fatalf("◉ should precede the name: dot=%d name=%d in %q", dot, name, row)
	}
}

func TestCommitBranchHint(t *testing.T) {
	m := footerModel()
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "aaaaaaabbbb", Subject: "x", Source: "feat"}}
	m.sel[panelCommits] = 0
	// The status line carries the branch AND the short id (the id left the row).
	if got := m.commitBranchHint(); got != "⎇ feat · # aaaaaaa" {
		t.Fatalf("hint = %q, want '⎇ feat · # aaaaaaa'", got)
	}
	m.focus = panelBranches // off the commits panel → no hint
	if got := m.commitBranchHint(); got != "" {
		t.Fatalf("hint off-panel = %q, want empty", got)
	}
	m.focus = panelCommits
	m.commits[0].Source = "" // no source → just the id remains
	if got := m.commitBranchHint(); got != "# aaaaaaa" {
		t.Fatalf("hint without source = %q, want '# aaaaaaa'", got)
	}
}

func TestCommitGotoTipJumpsAndFocuses(t *testing.T) {
	m := branchesPanelModel("feat", "main") // Branches focused, feat selected
	m.branches[0].Hash = "t1deadbeef"       // feat's tip (short SHA, as for-each-ref gives)
	m.commits = []model.Commit{
		{Hash: "b0aaaaaaaaaa", Subject: "base"},
		{Hash: "t1deadbeefcafe", Subject: "tip", Refs: []model.Ref{{Name: "feat", Kind: model.RefLocal}}},
	}
	r, ok := findRow(availableActions(m), "commits-goto-tip")
	if !ok {
		t.Fatal("go-to-tip row missing on Branches panel")
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	if m.focus != panelCommits {
		t.Fatalf("focus = %v, want panelCommits", m.focus)
	}
	if m.sel[panelCommits] != 1 {
		t.Fatalf("sel[panelCommits] = %d, want 1 (the feat tip)", m.sel[panelCommits])
	}
}

// TestCommitGotoTipSlashBranchByHash is the regression for the reported bug:
// a slash-named local branch (feat/x) whose tip commit IS loaded must be found.
// The match is by tip HASH, so it works even though the commit carries no
// decoration here (and historically the slash name was misclassified as remote).
func TestCommitGotoTipSlashBranchByHash(t *testing.T) {
	m := branchesPanelModel("feat/x", "main")
	m.branches[0].Hash = "abc1234" // feat/x tip
	m.commits = []model.Commit{
		{Hash: "0000000000", Subject: "base"},
		{Hash: "abc1234def0", Subject: "tip"}, // loaded, undecorated
	}
	r, ok := findRow(availableActions(m), "commits-goto-tip")
	if !ok {
		t.Fatal("go-to-tip row missing on Branches panel")
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	if m.focus != panelCommits || m.sel[panelCommits] != 1 {
		t.Fatalf("slash branch goto-tip: focus=%v sel=%d, want panelCommits/1", m.focus, m.sel[panelCommits])
	}
}

// TestCommitGotoTipNotLoadedNotifies: with no feed to page (nil = cannot load
// more), the eager fallback reports exhaustion instead of silently stopping.
func TestCommitGotoTipNotLoadedNotifies(t *testing.T) {
	m := branchesPanelModel("feat", "main")
	m.branches[0].Hash = "t1deadbeef"
	m.commits = []model.Commit{{Hash: "b0aaaaaaaaaa", Subject: "base"}} // no feat tip loaded
	r, _ := findRow(availableActions(m), "commits-goto-tip")
	mm, _ := r.run(m)
	m = mm.(Model)
	if m.focus != panelBranches {
		t.Fatalf("focus should stay on Branches, got %v", m.focus)
	}
	if !strings.Contains(m.statusMsg, "not found in full history") {
		t.Fatalf("statusMsg = %q, want the eager 'not found in full history' report", m.statusMsg)
	}
}

// TestCommitGotoTipFallsBackToEagerSearch: a tip missing from the loaded page
// with a loadable feed starts the ctrl+f deep search on the tip hash.
func TestCommitGotoTipFallsBackToEagerSearch(t *testing.T) {
	m := newTestModelForReload(t) // Branches focused ("main" selected), real FakeRunner feed
	m.branches[0].Hash = "t1deadbeef"
	m.commits = []model.Commit{{Hash: "b0aaaaaaaaaa", Subject: "base"}}
	r, ok := findRow(availableActions(m), "commits-goto-tip")
	if !ok {
		t.Fatal("go-to-tip row missing on Branches panel")
	}
	mm, cmd := r.run(m)
	m = mm.(Model)
	if !m.eager.active || m.eager.query != "t1deadbeef" {
		t.Fatalf("eager = %+v, want active search for the tip hash", m.eager)
	}
	if !m.commitsLoading || cmd == nil {
		t.Fatalf("loading=%v cmd=%v, want a page load dispatched", m.commitsLoading, cmd != nil)
	}
}

// TestCommitGotoTipFindsFilteredTip: a /-filter hiding an already-loaded tip no
// longer dead-ends — the eager fallback clears the filter and lands on the tip.
func TestCommitGotoTipFindsFilteredTip(t *testing.T) {
	m := branchesPanelModel("feat", "main")
	m.branches[0].Hash = "t1deadbeef"
	m.commits = []model.Commit{
		{Hash: "b0aaaaaaaaaa", Subject: "base"},
		{Hash: "t1deadbeefcafe", Subject: "tip"},
	}
	m.filterPanel = panelCommits
	m.filterQuery = "zzz" // hides every row from displayIndices
	r, _ := findRow(availableActions(m), "commits-goto-tip")
	mm, _ := r.run(m)
	m = mm.(Model)
	if m.filterQuery != "" {
		t.Fatalf("filterQuery = %q, want cleared (go-to semantics)", m.filterQuery)
	}
	if m.focus != panelCommits || m.sel[panelCommits] != 1 {
		t.Fatalf("focus=%v sel=%d, want panelCommits/1", m.focus, m.sel[panelCommits])
	}
}

// TestCommitGotoTipPreservesBranchesFilter: entering the eager fallback from a
// /-filtered Branches list must not clear THAT panel's filter — the go-to
// semantics only ever clear a Commits-panel filter.
func TestCommitGotoTipPreservesBranchesFilter(t *testing.T) {
	m := newTestModelForReload(t)
	m.branches[0].Hash = "t1deadbeef"
	m.commits = []model.Commit{{Hash: "b0aaaaaaaaaa", Subject: "base"}} // tip not loaded
	m.filterPanel = panelBranches
	m.filterQuery = "ma" // the user narrowed the Branches list
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if !m.eager.active {
		t.Fatal("fallback should still start the eager search")
	}
	if m.filterPanel != panelBranches || m.filterQuery != "ma" {
		t.Fatalf("Branches filter clobbered: panel=%v query=%q, want panelBranches/\"ma\"", m.filterPanel, m.filterQuery)
	}
}

func TestCommitCreateBranchRowOpensPopup(t *testing.T) {
	m := footerModel()
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	m.focus = panelCommits
	full := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 40 hex
	m.commits = []model.Commit{{Hash: full, Subject: "x"}}
	m.sel[panelCommits] = 0
	r, ok := findRow(availableActions(m), "commit-create-branch")
	if !ok {
		t.Fatal("create-branch row missing on the Commits panel")
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	bp, ok := m.topLayer().(*branchPopup)
	if !ok {
		t.Fatalf("expected a branchPopup overlay, got %T", m.topLayer())
	}
	if bp.startPoint != full {
		t.Fatalf("startPoint = %q, want the full hash (unambiguous start-point)", bp.startPoint)
	}
}

func TestDisplayStartShortensSHA(t *testing.T) {
	full := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if got := displayStart(full); got != "aaaaaaa" {
		t.Fatalf("displayStart(sha) = %q, want 7 chars", got)
	}
	if got := displayStart("feature/x"); got != "feature/x" {
		t.Fatalf("displayStart(branch) = %q, want unchanged", got)
	}
}

func TestCommitCherryPickRowGating(t *testing.T) {
	m := footerModel()
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	full := "cccccccccccccccccccccccccccccccccccccccc"
	m.commits = []model.Commit{{Hash: full, Subject: "x"}}
	m.sel[panelCommits] = 0

	// present on the Commits panel
	m.focus = panelCommits
	r, ok := findRow(availableActions(m), "commit-cherry-pick")
	if !ok {
		t.Fatal("cherry-pick row missing on the Commits panel")
	}
	if r.label != "Cherry-pick here" {
		t.Fatalf("label = %q", r.label)
	}

	// absent off the Commits panel
	m.focus = panelBranches
	if _, ok := findRow(availableActions(m), "commit-cherry-pick"); ok {
		t.Fatal("cherry-pick row must not appear off the Commits panel")
	}
}

// The ◉ marked set wins over the cursor, and targets come out oldest→newest
// by feed rank regardless of (random) map iteration order.
func TestCommitCherryPickMarkedTargetsOrderedOldestFirst(t *testing.T) {
	m := footerModel()
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	newest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	middle := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	oldest := "cccccccccccccccccccccccccccccccccccccccc"
	// feed is newest-first
	m.commits = []model.Commit{{Hash: newest}, {Hash: middle}, {Hash: oldest}}
	m.sel[panelCommits] = 1 // cursor elsewhere; marks must win
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{middle: true, oldest: true, newest: true}

	targets, fromMarks := m.cherryPickTargets()
	if !fromMarks {
		t.Fatal("marked commits must win over the cursor")
	}
	want := []string{oldest, middle, newest}
	if len(targets) != 3 || targets[0] != want[0] || targets[1] != want[1] || targets[2] != want[2] {
		t.Fatalf("targets = %v, want oldest→newest %v", targets, want)
	}

	// A single mark still wins over the cursor.
	m.commitCompareSet = map[string]bool{oldest: true}
	targets, fromMarks = m.cherryPickTargets()
	if !fromMarks || len(targets) != 1 || targets[0] != oldest {
		t.Fatalf("single mark: targets = %v fromMarks=%v, want [%s] true", targets, fromMarks, oldest)
	}

	// No marks → the cursor commit.
	m.commitCompareSet = nil
	targets, fromMarks = m.cherryPickTargets()
	if fromMarks || len(targets) != 1 || targets[0] != middle {
		t.Fatalf("no marks: targets = %v fromMarks=%v, want cursor [%s] false", targets, fromMarks, middle)
	}

	// Stale marks (rows gone from the feed) don't count; cursor wins again.
	m.commitCompareSet = map[string]bool{"dddddddddddddddddddddddddddddddddddddddd": true}
	targets, fromMarks = m.cherryPickTargets()
	if fromMarks || len(targets) != 1 || targets[0] != middle {
		t.Fatalf("stale marks: targets = %v fromMarks=%v, want cursor [%s] false", targets, fromMarks, middle)
	}
}

func TestCommitCherryPickRowMarkedLabelsAndGuards(t *testing.T) {
	m := footerModel()
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	a := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	b := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	m.commits = []model.Commit{{Hash: a, Parents: []string{"p"}}, {Hash: b, Parents: []string{"p"}}}
	m.sel[panelCommits] = 0
	m.focus = panelCommits

	// 2+ marks → plural label
	m.commitCompareSet = map[string]bool{a: true, b: true}
	r, ok := findRow(availableActions(m), "commit-cherry-pick")
	if !ok {
		t.Fatal("cherry-pick row missing")
	}
	if r.label != "Cherry-pick 2 marked commits here" {
		t.Fatalf("label = %q", r.label)
	}

	// exactly 1 mark → singular marked label
	m.commitCompareSet = map[string]bool{b: true}
	r, _ = findRow(availableActions(m), "commit-cherry-pick")
	if r.label != "Cherry-pick the marked commit here" {
		t.Fatalf("label = %q", r.label)
	}

	// a WIP row in the marks: refused with a clean message, no op started
	m.wipRows = []wipRow{{kind: wipWorktree}}
	m.commitCompareSet = map[string]bool{a: true, wipKey(wipRow{kind: wipWorktree}): true}
	r, _ = findRow(availableActions(m), "commit-cherry-pick")
	mm, cmd := r.run(m)
	if cmd != nil {
		t.Fatal("a WIP mark must not start an op")
	}
	if got := mm.(Model).statusMsg; got != "cherry-pick is commits-only; remove the working tree / staged row" {
		t.Fatalf("statusMsg = %q", got)
	}

	// a merge commit among the targets: refused with a clean message
	m.wipRows = nil
	merge := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	m.commits = append(m.commits, model.Commit{Hash: merge, Subject: "m", Parents: []string{"p1", "p2"}})
	m.commitCompareSet = map[string]bool{a: true, merge: true}
	r, _ = findRow(availableActions(m), "commit-cherry-pick")
	mm, cmd = r.run(m)
	if cmd != nil {
		t.Fatal("a merge commit in the targets must not start an op")
	}
	if got := mm.(Model).statusMsg; got != "cannot cherry-pick a merge commit" {
		t.Fatalf("statusMsg = %q", got)
	}

	// the merge guard also covers the cursor path
	m.commitCompareSet = nil
	m.sel[panelCommits] = 2 // cursor on the merge commit
	r, _ = findRow(availableActions(m), "commit-cherry-pick")
	mm, cmd = r.run(m)
	if cmd != nil {
		t.Fatal("cursor on a merge commit must not start an op")
	}
	if got := mm.(Model).statusMsg; got != "cannot cherry-pick a merge commit" {
		t.Fatalf("statusMsg = %q", got)
	}
}

func TestCommitRevertRowGatingAndMergeGuard(t *testing.T) {
	m := footerModel()
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	full := "dddddddddddddddddddddddddddddddddddddddd"
	m.commits = []model.Commit{{Hash: full, Subject: "x", Parents: []string{"p1"}}}
	m.sel[panelCommits] = 0

	// present on the Commits panel
	m.focus = panelCommits
	r, ok := findRow(availableActions(m), "commit-revert")
	if !ok {
		t.Fatal("revert row missing on the Commits panel")
	}
	if r.label != "Revert this commit" {
		t.Fatalf("label = %q", r.label)
	}
	// absent off the Commits panel
	m.focus = panelBranches
	if _, ok := findRow(availableActions(m), "commit-revert"); ok {
		t.Fatal("revert row must not appear off the Commits panel")
	}

	// merge commit (2 parents): the row's run refuses with a clean message and
	// starts no op (synchronously observable — no startOp goroutine).
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: full, Subject: "merge", Parents: []string{"p1", "p2"}}}
	r, _ = findRow(availableActions(m), "commit-revert")
	mm, cmd := r.run(m)
	if cmd != nil {
		t.Fatal("reverting a merge commit must not start an op")
	}
	if got := mm.(Model).statusMsg; got != "cannot revert a merge commit (v1)" {
		t.Fatalf("statusMsg = %q, want the merge-guard message", got)
	}
}

func TestCommitResetRowGating(t *testing.T) {
	m := footerModel()
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	full := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee0"
	m.commits = []model.Commit{{Hash: full, Subject: "x"}}
	m.sel[panelCommits] = 0

	m.focus = panelCommits
	r, ok := findRow(availableActions(m), "commit-reset")
	if !ok {
		t.Fatal("reset row missing on the Commits panel")
	}
	if r.label != "Reset to this commit" {
		t.Fatalf("label = %q", r.label)
	}
	m.focus = panelBranches
	if _, ok := findRow(availableActions(m), "commit-reset"); ok {
		t.Fatal("reset row must not appear off the Commits panel")
	}
}

func TestCommitCreateWorktreeRowOpensInEdit(t *testing.T) {
	m := footerModel()
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	m.focus = panelCommits
	full := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	m.commits = []model.Commit{{Hash: full, Subject: "x", Parents: []string{"aaaaaaaa"}}}
	m.status.Branch = "main"
	m.sel[panelCommits] = 0
	r, ok := findRow(availableActions(m), "commit-create-worktree")
	if !ok {
		t.Fatal("create-worktree row missing on the Commits panel")
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	p, ok := m.topLayer().(*worktreePopup)
	if !ok {
		t.Fatalf("expected a worktreePopup overlay, got %T", m.topLayer())
	}
	if !p.fromCommit || p.startPoint != full {
		t.Fatalf("fromCommit=%v startPoint=%q (want true + full hash)", p.fromCommit, p.startPoint)
	}
	if p.state != stEdit || p.editBuf.Value() != "main_"+full[:7] {
		t.Fatalf("should open in branch-edit prefilled; state=%v buf=%q", p.state, p.editBuf.Value())
	}
	if !p.keepOffered || p.keepLocked {
		t.Fatalf("keepOffered=%v keepLocked=%v, want offered and unlocked", p.keepOffered, p.keepLocked)
	}
}

func TestCommitCreateWorktreeRowRootCommitLocksKeep(t *testing.T) {
	m := footerModel()
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	m.focus = panelCommits
	full := "dddddddddddddddddddddddddddddddddddddddd"
	m.commits = []model.Commit{{Hash: full, Subject: "root"}} // no parents
	m.sel[panelCommits] = 0
	m.status.Branch = "(detached)"
	r, _ := findRow(availableActions(m), "commit-create-worktree")
	mm, _ := r.run(m)
	m = mm.(Model)
	p := m.topLayer().(*worktreePopup)
	if !p.keepLocked {
		t.Fatal("a root commit must lock the keep mode")
	}
	if p.editBuf.Value() != "wt_"+full[:7] {
		t.Fatalf("detached prefill = %q, want wt_%s", p.editBuf.Value(), full[:7])
	}
}

func TestWorktreeFromCommitRequiresBranchName(t *testing.T) {
	m := footerModel()
	p := &worktreePopup{fromCommit: true, branchOverride: ""} // user typed nothing
	m, cmd := m.startCreateFromPopup(p, false)
	if m.statusMsg == "" {
		t.Fatal("expected a 'branch name required' message")
	}
	if cmd != nil {
		t.Fatal("must not launch the create op without a branch name")
	}
}

func TestFeedScopeFoldsFilterAndBranches(t *testing.T) {
	var m Model
	m.commitScopeBranches = []string{"main"}
	m.commitFilter = commitFilterFields{Paths: []string{"sub"}, Author: "alice", Grep: "race"}
	s := m.feedScope()
	if len(s.Branches) != 1 || s.Branches[0] != "main" {
		t.Fatalf("branches not carried: %+v", s.Branches)
	}
	if len(s.Paths) != 1 || s.Paths[0] != "sub" || s.Author != "alice" || s.Grep != "race" {
		t.Fatalf("filter not folded: %+v", s)
	}
	if !m.commitFilter.filtered() {
		t.Fatal("filtered() should be true")
	}
	if (commitFilterFields{}).filtered() {
		t.Fatal("empty filter must not be filtered")
	}
}

func TestCommitScopeLabelShowsFilterChips(t *testing.T) {
	var m Model
	m.commitFilter = commitFilterFields{Paths: []string{"sub"}, Grep: "race", Author: "alice"}
	got := m.commitScopeLabel()
	for _, want := range []string{"path=sub", "msg=race", "@alice"} {
		if !strings.Contains(got, want) {
			t.Fatalf("label %q missing chip %q", got, want)
		}
	}
}

func TestCommitScopeLabelPlainWhenUnfiltered(t *testing.T) {
	var m Model
	if got := m.commitScopeLabel(); got != "all" {
		t.Fatalf("unfiltered label should be \"all\", got %q", got)
	}
}

func TestGraphSuppressedWhenFiltered(t *testing.T) {
	var m Model
	// Default: graph allowed (no filter, default sort, in-memory filter off).
	if !m.commitGraphOn() {
		t.Fatal("precondition: graph should be on with no filter")
	}
	m.commitFilter = commitFilterFields{Grep: "race"}
	if m.commitGraphOn() {
		t.Fatal("graph must be suppressed while a commit filter is active")
	}
}

// TestFilesViewRowsExcludeDeletedFile anchors the status == "D" exclusion in
// viewFileRow and openExternalRow. A deleted file has no content at the commit,
// so both rows must return ok==false for it. A normal (non-deleted) file must
// return ok==true so the test cannot pass vacuously.
func TestFilesViewRowsExcludeDeletedFile(t *testing.T) {
	m := openFilesView(t, filesModel())
	m.filesTreeFocused = true

	// Inject a deleted-file line at index 0 and a normal file line at index 1
	// so we can test both without depending on the fixture's existing status values.
	deletedLine := contentLine{text: "D  gone.go", path: "gone.go", status: "D"}
	normalLine := contentLine{text: "M  model.go", path: "internal/tui/model.go", status: "M"}
	m.filesView.lines = []contentLine{deletedLine, normalLine}

	// --- deleted file: both rows must return ok==false ---
	m.filesView.sel = 0
	if _, ok := m.viewFileRow(); ok {
		t.Error("viewFileRow must return ok==false for a deleted (D) file")
	}
	if _, ok := m.openExternalRow(); ok {
		t.Error("openExternalRow must return ok==false for a deleted (D) file")
	}

	// --- normal file: both rows must return ok==true (non-vacuous counter-check) ---
	m.filesView.sel = 1
	if _, ok := m.viewFileRow(); !ok {
		t.Error("viewFileRow must return ok==true for a normal (non-deleted) file")
	}
	if _, ok := m.openExternalRow(); !ok {
		t.Error("openExternalRow must return ok==true for a normal (non-deleted) file")
	}
}

func TestFilesViewCommitsTouchingSeedsFilter(t *testing.T) {
	m := openFilesView(t, filesModel())
	// openFilesView lands on the commit-list side; switch to the tree side so
	// the guard passes.
	m.filesTreeFocused = true
	m.filesView.sel = 0
	// Find the first visible row that is a real file (non-heading, non-empty path).
	vis := m.filesView.visible()
	want := ""
	for _, l := range vis {
		if !l.heading && l.path != "" {
			want = l.path
			break
		}
	}
	if want == "" {
		t.Fatal("test fixture has no selectable file rows — fix filesModel()")
	}
	// Set sel to that row.
	for i, l := range vis {
		if l.path == want {
			m.filesView.sel = i
			break
		}
	}
	row, ok := m.commitsTouchingFileRow()
	if !ok {
		t.Fatal("commitsTouchingFileRow must be available with a tree-side file selected")
	}
	mm, _ := row.run(m)
	got := mm.(Model)
	if len(got.commitFilter.Paths) != 1 || got.commitFilter.Paths[0] != want {
		t.Fatalf("path not seeded correctly: got %v, want [%q]", got.commitFilter.Paths, want)
	}
	if got.filesView != nil {
		t.Fatal("running the row should close the files view")
	}
	if got.focus != panelCommits {
		t.Fatalf("focus should be panelCommits after seeding, got %v", got.focus)
	}
}

func TestFeedUpstreamsFiltersToExistingRemoteRefs(t *testing.T) {
	m := Model{
		branches: []model.Branch{
			{Name: "main", Upstream: "origin/main"},
			{Name: "feat", Upstream: "origin/feat"}, // upstream configured but ref gone
			{Name: "local-only"},                    // no upstream
		},
		remoteBranches: []model.RemoteBranch{{Name: "origin/main"}}, // only origin/main exists
	}
	got := m.feedUpstreams()
	if !slices.Equal(got, []string{"origin/main"}) {
		t.Fatalf("feedUpstreams() = %v, want [origin/main]", got)
	}
}

func TestFeedUpstreamsRespectsSoloScope(t *testing.T) {
	m := Model{
		commitScopeBranches: []string{"feat"}, // soloed
		branches: []model.Branch{
			{Name: "main", Upstream: "origin/main"},
			{Name: "feat", Upstream: "origin/feat"},
		},
		remoteBranches: []model.RemoteBranch{{Name: "origin/main"}, {Name: "origin/feat"}},
	}
	got := m.feedUpstreams()
	if !slices.Equal(got, []string{"origin/feat"}) {
		t.Fatalf("feedUpstreams() = %v, want only the soloed branch's upstream [origin/feat]", got)
	}
}

// newTestModelForReload builds a Model with a real svc+feed (fake runner) for
// testing the dataLoadedMsg reload path. Mirrors TestCommitSoloReloadEndToEnd.
func newTestModelForReload(t *testing.T) Model {
	t.Helper()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: "h1\x1f\x1fAda\x1f0\x1fsubject\x1fHEAD -> main\n"})
	svc := domain.New(&git.Repo{Runner: f})
	m := branchesPanelModel("main")
	m.svc = svc
	m.feed = svc.CommitFeed()
	return m
}

func TestDataLoadedTriggersUpstreamReload(t *testing.T) {
	m := newTestModelForReload(t)
	msg := dataLoadedMsg{
		gen:            m.loadGen,
		branches:       []model.Branch{{Name: "main", IsHead: true, Upstream: "origin/main"}},
		remoteBranches: []model.RemoteBranch{{Name: "origin/main"}},
	}
	nm, _ := m.Update(msg)
	if !nm.(Model).commitsLoading {
		t.Fatal("expected a feed reload (commitsLoading=true) when tracked upstreams exist")
	}
}

func TestDataLoadedNoReloadWithoutTrackedUpstreams(t *testing.T) {
	m := newTestModelForReload(t)
	msg := dataLoadedMsg{
		gen:      m.loadGen,
		branches: []model.Branch{{Name: "main", IsHead: true}}, // no upstream
	}
	nm, _ := m.Update(msg)
	if nm.(Model).commitsLoading {
		t.Fatal("no tracked upstreams must NOT trigger a reload (preserve the fast initial walk)")
	}
}

// TestDataLoadedNoRedundantReloadOnSecondLoad proves that a second dataLoadedMsg
// with the same upstream set does NOT trigger another feed reload. The first
// delivery fires the reload (scope differs from the zero value); the second must
// be a no-op because feedScopeApplied already matches feedScopeSig().
func TestReloadFeedRestoresOnFilterClear(t *testing.T) {
	f := gitexec.NewFakeRunner()
	svc := domain.New(&git.Repo{Runner: f})
	m := branchesPanelModel("main")
	m.svc = svc
	m.feed = svc.CommitFeed()
	m.feed.SetPageSizes(50, 50)
	m.sel = map[panel]int{}

	f.SetResponse("git log", gitexec.Result{Stdout: "h1\x1f\x1fAda\x1f0\x1fa\x1f\x1f\nh2\x1f\x1fAda\x1f0\x1fb\x1f\x1f\nh3\x1f\x1fAda\x1f0\x1fc\x1f\x1f\n"})
	m.feed.LoadInitial(context.Background()) // base: 3

	f.SetResponse("git log", gitexec.Result{Stdout: "h1\x1f\x1fAda\x1f0\x1fa\x1f\x1f\n"})
	m.commitFilter = commitFilterFields{Grep: "a"}
	m.feed.ApplyScope(context.Background(), m.feedScope()) // filtered: 1
	calls := len(f.Calls)

	// Clear the filter and run reloadFeedCmd → it must ApplyScope back to base and
	// restore from cache (no new git log).
	m.commitFilter = commitFilterFields{}
	msg := m.reloadFeedCmd()()
	rm, ok := msg.(commitsReloadedMsg)
	if !ok {
		t.Fatalf("want commitsReloadedMsg, got %T", msg)
	}
	if len(f.Calls) != calls {
		t.Fatalf("clear re-walked: calls %d → %d", calls, len(f.Calls))
	}
	if len(rm.state.Commits) != 3 {
		t.Fatalf("restored base = %d, want 3", len(rm.state.Commits))
	}
}

func TestDataLoadedNoRedundantReloadOnSecondLoad(t *testing.T) {
	m := newTestModelForReload(t)
	msg := dataLoadedMsg{
		gen:            m.loadGen,
		branches:       []model.Branch{{Name: "main", IsHead: true, Upstream: "origin/main"}},
		remoteBranches: []model.RemoteBranch{{Name: "origin/main"}},
	}

	// First delivery: scope differs from "" → reload fires.
	nm, _ := m.Update(msg)
	m = nm.(Model)
	if !m.commitsLoading {
		t.Fatal("first dataLoadedMsg with upstreams should trigger a reload")
	}

	// Simulate the reload having completed: clear the loading flag and update
	// loadGen so the next dataLoadedMsg is not dropped.
	m.commitsLoading = false
	m.loadGen++
	msg.gen = m.loadGen

	// Second delivery with the same branches/upstreams: feedScopeApplied already
	// matches feedScopeSig() → must NOT fire a second reload.
	nm, _ = m.Update(msg)
	m = nm.(Model)
	if m.commitsLoading {
		t.Fatal("second dataLoadedMsg with the same upstream set must NOT trigger a redundant reload")
	}
}

// TestBranchesEnterJumpsToTip: enter on the Branches panel = the .-menu
// "Go to tip in commits" (same code path, so they cannot drift).
func TestBranchesEnterJumpsToTip(t *testing.T) {
	m := branchesPanelModel("feat", "main")
	m.branches[0].Hash = "t1deadbeef"
	m.commits = []model.Commit{
		{Hash: "b0aaaaaaaaaa", Subject: "base"},
		{Hash: "t1deadbeefcafe", Subject: "tip"},
	}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if m.focus != panelCommits || m.sel[panelCommits] != 1 {
		t.Fatalf("enter: focus=%v sel=%d, want panelCommits/1", m.focus, m.sel[panelCommits])
	}
}

// TestBranchesEnterNoBranchNoOp: enter with nothing selectable must not panic
// or fall through to another panel's enter behavior.
func TestBranchesEnterNoBranchNoOp(t *testing.T) {
	m := branchesPanelModel() // empty Branches list
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if m.focus != panelBranches || cmd != nil {
		t.Fatalf("enter on empty Branches: focus=%v cmd=%v, want no-op", m.focus, cmd != nil)
	}
}

// TestCtrlGSoloSetsPendingAndReloads: ctrl+g on Branches solos the branch and
// remembers its tip for the post-reload jump.
func TestCtrlGSoloSetsPendingAndReloads(t *testing.T) {
	m := newTestModelForReload(t) // Branches focused, "main" selected
	m.branches[0].Hash = "t1deadbeef"
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = nm.(Model)
	if len(m.commitScopeBranches) != 1 || m.commitScopeBranches[0] != "main" {
		t.Fatalf("scope = %v, want [main]", m.commitScopeBranches)
	}
	if m.pendingGotoTip != "t1deadbeef" {
		t.Fatalf("pendingGotoTip = %q, want the tip hash", m.pendingGotoTip)
	}
	if !m.commitsLoading || cmd == nil {
		t.Fatalf("loading=%v cmd=%v, want a scope reload dispatched", m.commitsLoading, cmd != nil)
	}
}

// TestReloadedMsgDrainsPendingGotoTip: the scope reload landing finishes the
// ctrl+g gesture — cursor on the tip, Commits focused, pending cleared. The
// tip sits at a NON-zero index: index 0 is also the sel-clamp default, so
// only a non-zero landing spot proves the drain targeted the tip hash.
func TestReloadedMsgDrainsPendingGotoTip(t *testing.T) {
	m := newTestModelForReload(t)
	m.branches[0].Hash = "t1deadbeef"
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = nm.(Model)
	msg := commitsReloadedMsg{gen: m.feed.Gen(), state: domain.FeedState{Commits: []model.Commit{
		{Hash: "b0aaaaaaaaaa", Subject: "base"},
		{Hash: "t1deadbeefcafe", Subject: "tip"},
	}}}
	nm, _ = m.Update(msg)
	m = nm.(Model)
	if m.pendingGotoTip != "" {
		t.Fatalf("pendingGotoTip = %q, want drained", m.pendingGotoTip)
	}
	if m.focus != panelCommits || m.sel[panelCommits] != 1 {
		t.Fatalf("focus=%v sel=%d, want panelCommits/1 (the tip row)", m.focus, m.sel[panelCommits])
	}
}

// TestCtrlGOnSoloedBranchUnsolos: ctrl+g preserves solo's toggle — a second
// press un-solos, and the pending jump still chains.
func TestCtrlGOnSoloedBranchUnsolos(t *testing.T) {
	m := newTestModelForReload(t)
	m.branches[0].Hash = "t1deadbeef"
	m.commitScopeBranches = []string{"main"} // already soloed to the selected branch
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = nm.(Model)
	if len(m.commitScopeBranches) != 0 {
		t.Fatalf("scope = %v, want cleared (un-solo)", m.commitScopeBranches)
	}
	if m.pendingGotoTip != "t1deadbeef" {
		t.Fatalf("pendingGotoTip = %q, want the tip hash even on un-solo", m.pendingGotoTip)
	}
}

// TestCtrlGBusyNoOp: ctrl+g inherits solo's opsIdle gate — nothing mutates
// while an operation runs.
func TestCtrlGBusyNoOp(t *testing.T) {
	m := newTestModelForReload(t)
	m.branches[0].Hash = "t1deadbeef"
	m.running = true // opsIdle() == false
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = nm.(Model)
	if len(m.commitScopeBranches) != 0 || m.pendingGotoTip != "" || cmd != nil {
		t.Fatalf("busy ctrl+g mutated state: scope=%v pending=%q cmd=%v", m.commitScopeBranches, m.pendingGotoTip, cmd != nil)
	}
}

// commitsPanelSoloModel is an idle fixture focused on the Commits panel with
// one loaded commit (full 40-hex sha) selected.
func commitsPanelSoloModel(full string) Model {
	m := footerModel()
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: full, Subject: "x"}}
	m.sel[panelCommits] = 0
	return m
}

// TestCommitSoloCommitTogglesScope: the Commits-panel "Solo from this commit"
// row scopes the feed to the selected commit's sha (the commit-anchored twin
// of branch solo), remembers the commit for the post-reload cursor landing,
// and un-solos on a second run.
func TestCommitSoloCommitTogglesScope(t *testing.T) {
	full := strings.Repeat("a", 40)
	m := commitsPanelSoloModel(full)
	r, ok := findRow(availableActions(m), "commits-solo-commit")
	if !ok {
		t.Fatal("Solo from this commit missing on the Commits panel")
	}
	mm, cmd := r.run(m)
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("solo should dispatch a scope reload")
	}
	if len(m.commitScopeBranches) != 1 || m.commitScopeBranches[0] != full {
		t.Fatalf("scope = %v, want [%s]", m.commitScopeBranches, full)
	}
	if m.pendingGotoTip != full {
		t.Fatalf("pendingGotoTip = %q, want the commit hash", m.pendingGotoTip)
	}
	// Re-solo the same commit → un-solo (back to all); the cursor still lands
	// on the commit in the expanded feed.
	r2, _ := findRow(availableActions(m), "commits-solo-commit")
	mm, _ = r2.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 0 {
		t.Fatalf("re-solo should clear scope, got %v", m.commitScopeBranches)
	}
	if m.pendingGotoTip != full {
		t.Fatalf("un-solo pendingGotoTip = %q, want the commit hash", m.pendingGotoTip)
	}
}

// TestCommitSoloCommitRowGating: the row is offered only on the Commits panel,
// only when idle, and only with a real commit under the cursor.
func TestCommitSoloCommitRowGating(t *testing.T) {
	full := strings.Repeat("a", 40)
	m := commitsPanelSoloModel(full)
	m.focus = panelFiles
	if _, ok := findRow(availableActions(m), "commits-solo-commit"); ok {
		t.Fatal("row must not appear off the Commits panel")
	}
	m = commitsPanelSoloModel(full)
	m.running = true // opsIdle() == false
	if _, ok := findRow(availableActions(m), "commits-solo-commit"); ok {
		t.Fatal("row must not appear while an op runs")
	}
	m = commitsPanelSoloModel(full)
	m.commits = nil // nothing selectable
	if _, ok := findRow(availableActions(m), "commits-solo-commit"); ok {
		t.Fatal("row must not appear with no commit selected")
	}
}

// TestCtrlGOnCommitsSolosSelectedCommit: ctrl+g on the Commits panel runs the
// solo-from-commit row — the commit-window twin of the Branches ctrl+g.
func TestCtrlGOnCommitsSolosSelectedCommit(t *testing.T) {
	full := strings.Repeat("b", 40)
	m := commitsPanelSoloModel(full)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = nm.(Model)
	if len(m.commitScopeBranches) != 1 || m.commitScopeBranches[0] != full {
		t.Fatalf("scope = %v, want [%s]", m.commitScopeBranches, full)
	}
	if m.pendingGotoTip != full {
		t.Fatalf("pendingGotoTip = %q, want the commit hash", m.pendingGotoTip)
	}
	if !m.commitsLoading || cmd == nil {
		t.Fatalf("loading=%v cmd=%v, want a scope reload dispatched", m.commitsLoading, cmd != nil)
	}
}

// TestCtrlGOnSoloedCommitUnsolos: ctrl+g keeps solo's toggle on the Commits
// panel — a second press on the same commit restores the all-branches scope.
func TestCtrlGOnSoloedCommitUnsolos(t *testing.T) {
	full := strings.Repeat("b", 40)
	m := commitsPanelSoloModel(full)
	m.commitScopeBranches = []string{full} // already soloed on the selected commit
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = nm.(Model)
	if len(m.commitScopeBranches) != 0 {
		t.Fatalf("scope = %v, want cleared (un-solo)", m.commitScopeBranches)
	}
	if m.pendingGotoTip != full {
		t.Fatalf("pendingGotoTip = %q, want the commit hash even on un-solo", m.pendingGotoTip)
	}
}

// TestCommitScopeLabelShortensFullSha: a commit-solo scope entry (a full
// 40-hex sha) renders shortened in the panel header; branch/tag names pass
// through untouched.
func TestCommitScopeLabelShortensFullSha(t *testing.T) {
	full := strings.Repeat("c", 40)
	m := Model{commitScopeBranches: []string{full}}
	if got := m.commitScopeLabel(); got != "solo: "+full[:7] {
		t.Fatalf("label = %q, want solo: %s", got, full[:7])
	}
	m = Model{commitScopeBranches: []string{"feat"}}
	if got := m.commitScopeLabel(); got != "solo: feat" {
		t.Fatalf("label = %q, want solo: feat", got)
	}
}
