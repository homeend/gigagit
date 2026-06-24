package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
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
	m.commits = []model.Commit{
		{Hash: "b0", Subject: "base"},
		{Hash: "t1", Subject: "tip", Refs: []model.Ref{{Name: "feat", Kind: model.RefLocal}}},
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

func TestCommitGotoTipNotLoadedNotifies(t *testing.T) {
	m := branchesPanelModel("feat", "main")
	m.commits = []model.Commit{{Hash: "b0", Subject: "base"}} // no feat tip loaded
	r, _ := findRow(availableActions(m), "commits-goto-tip")
	mm, _ := r.run(m)
	m = mm.(Model)
	if m.focus != panelBranches {
		t.Fatalf("focus should stay on Branches, got %v", m.focus)
	}
	if m.statusMsg == "" {
		t.Fatal("expected a 'tip not loaded' status message")
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
	m.commits = []model.Commit{{Hash: full, Subject: "x"}}
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
	if p.state != stEdit || p.editBuf.Value() != "" {
		t.Fatalf("should open in branch-edit with an empty buffer; state=%v buf=%q", p.state, p.editBuf.Value())
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
