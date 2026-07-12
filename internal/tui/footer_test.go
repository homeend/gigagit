package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

func TestFooterActionsAllowlistFiltersAndOrders(t *testing.T) {
	m := footerModel()
	m.loading = false
	m.cfg.UI.FooterActions = []string{"repo", "pull"} // order matters
	line := m.footerLine()
	ri, pi := strings.Index(line, "[R]epo"), strings.Index(line, "[p]ull")
	if ri < 0 || pi < 0 {
		t.Fatalf("allowlisted actions missing: %q", line)
	}
	if ri > pi {
		t.Errorf("order not honored (repo should precede pull): %q", line)
	}
	if strings.Contains(line, "[u]ndo") {
		t.Errorf("non-allowlisted action leaked: %q", line)
	}
	if !strings.Contains(line, "[.] actions") {
		t.Errorf("[.] actions must always stay in the footer: %q", line)
	}
}

func TestFooterBindingIDsUniqueAndPresent(t *testing.T) {
	seen := map[string]string{} // id -> label (first seen)
	nav := map[string]bool{"tab": true, "shift+tab": true, "ctrl+←/→": true}
	// Keys allowed to carry an empty id: "space" has both empty and non-empty
	// ids (stage/unstage on Files, mark on Commits); "enter" gains an empty-id
	// tip-jump row alongside its existing switch-worktree/tag-goto/file-diff
	// ids; "ctrl+g" is empty-id-only — it duplicates the "." menu's own
	// "Go to tip in commits" (id commits-goto-tip) and "Solo this branch" rows,
	// so folding it into the menu under its own id would double them up.
	multiKeyTypes := map[string]bool{"space": true, "enter": true, "ctrl+g": true}
	for _, b := range append(append([]footerBinding{}, contextBindings...), globalBindings...) {
		if nav[b.key] {
			if b.id != "" {
				t.Errorf("navigation key %q must have empty id, got %q", b.key, b.id)
			}
			continue
		}
		// Empty ids are allowed for keys that have mixed usage (e.g., space: stage/unstage on Files, mark on Commits)
		if b.id == "" {
			if !multiKeyTypes[b.key] {
				t.Errorf("binding %q (%s) is missing an id", b.key, b.label)
			}
			continue
		}
		if prev, ok := seen[b.id]; ok {
			t.Errorf("duplicate id %q on %q and %q", b.id, prev, b.label)
		}
		seen[b.id] = b.label
	}
}

// footerModel is an idle fixture: Branches focused (zero value), two branches
// (main is HEAD, selected by default), two worktrees ("/repo" is current,
// selected by default). Every panel except Status/Commits has rows.
// svc is wired with a FakeRunner so diffDiffer() is never nil.
func footerModel() Model {
	return Model{
		svc:       domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()}),
		width:     120,
		height:    40,
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		status:    model.WorkingTreeStatus{Branch: "main"},
		branches: []model.Branch{
			{Name: "main", IsHead: true},
			{Name: "feat/x"},
		},
		worktrees: []model.Worktree{
			{Path: "/repo", Branch: "main"},
			{Path: "/repo/wt-x", Branch: "feat/x"},
		},
		currentWorktree: "/repo",
	}
}

// The honesty tests pin the predicate-sharing contract: when a shared
// predicate is false, the key must be a complete no-op (no op spawned, no
// state change) — these three used to start operations git then rejects.

func TestSwitchKeyNoOpOnHeadBranch(t *testing.T) {
	m := footerModel() // sel 0 = main, the checked-out branch
	u, cmd := m.Update(keyMsg("s"))
	mm := u.(Model)
	if cmd != nil || mm.running {
		t.Fatal("s on the checked-out branch must be a no-op")
	}
}

func TestDeleteKeyNoOpOnHeadBranch(t *testing.T) {
	m := footerModel()
	u, cmd := m.Update(keyMsg("d"))
	mm := u.(Model)
	if cmd != nil || mm.running {
		t.Fatal("d on the checked-out branch must be a no-op")
	}
}

func TestDeleteKeyNoOpOnCurrentWorktree(t *testing.T) {
	m := footerModel()
	m.focus = panelWorktrees // sel 0 = /repo = currentWorktree
	u, cmd := m.Update(keyMsg("d"))
	mm := u.(Model)
	if cmd != nil || mm.running {
		t.Fatal("d on the current worktree must be a no-op")
	}
}

func TestEnterNoOpOnCurrentWorktree(t *testing.T) {
	m := footerModel()
	m.focus = panelWorktrees
	u, cmd := m.Update(keyMsg("enter"))
	mm := u.(Model)
	// reRoot sets loading (not running like startOp)
	if cmd != nil || mm.loading {
		t.Fatal("enter on the current worktree must not re-root")
	}
}

func TestSwitchKeyNoOpWhileLoading(t *testing.T) {
	m := footerModel()
	m.loading = true
	m.sel[panelBranches] = 1 // feat/x would otherwise be switchable
	u, cmd := m.Update(keyMsg("s"))
	if cmd != nil || u.(Model).running {
		t.Fatal("s while loading must be a no-op")
	}
}

func TestMarkKeyNoOpWhileRunning(t *testing.T) {
	m := footerModel()
	m.running = true
	u, _ := m.Update(keyMsg("m"))
	if u.(Model).mark != nil {
		t.Fatal("m while an op runs must not mark")
	}
}

func TestPredicatesOnSelectableRows(t *testing.T) {
	m := footerModel()
	m.sel[panelBranches] = 1 // feat/x (not HEAD)
	if !m.canSwitchBranch() || !m.canDeleteBranch() || !m.canOpenBranchPopup() || !m.canOpenWorktreePopup() {
		t.Error("branch predicates must hold on an idle model with a non-HEAD row selected")
	}
	m.sel[panelWorktrees] = 1 // /repo/wt-x (not current)
	if !m.canDeleteWorktree() || !m.canEnterWorktree() {
		t.Error("worktree predicates must hold on a non-current worktree row")
	}
	if !m.canMark() {
		t.Error("canMark must hold when the focused panel has a selected row")
	}
	m.running = true
	if m.opsIdle() || m.canSwitchBranch() || m.canMark() {
		t.Error("all op predicates must be false while running")
	}
}

func TestFooterBranchesContextNonHead(t *testing.T) {
	m := footerModel()
	m.sel[panelBranches] = 1 // feat/x
	f := m.footerLine()
	for _, want := range []string{
		"[s]witch", "[b]ranch", "[w]orktree", "[d]elete", "[m]ark",
		"•", "[p]ull", "[P]ush", "[q] quit",
	} {
		if !strings.Contains(f, want) {
			t.Errorf("footer %q must contain %q", f, want)
		}
	}
}

func TestFooterHeadBranchHidesSwitchAndDelete(t *testing.T) {
	m := footerModel() // sel 0 = main (HEAD)
	f := m.footerLine()
	if strings.Contains(f, "[s]witch") || strings.Contains(f, "[d]elete") {
		t.Errorf("HEAD branch row must not offer switch/delete: %q", f)
	}
	if !strings.Contains(f, "[b]ranch") || !strings.Contains(f, "[w]orktree") {
		t.Errorf("branch/worktree creation stays available on the HEAD row: %q", f)
	}
}

func TestFooterWorktreesContext(t *testing.T) {
	m := footerModel()
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1 // not the current worktree
	f := m.footerLine()
	if !strings.Contains(f, "[enter] switch") || !strings.Contains(f, "[d]elete") {
		t.Errorf("other-worktree row must offer enter/delete: %q", f)
	}
	if strings.Contains(f, "[s]witch") || strings.Contains(f, "[b]ranch") {
		t.Errorf("branch actions must not show on Worktrees focus: %q", f)
	}
	m.sel[panelWorktrees] = 0 // the current worktree
	f = m.footerLine()
	if strings.Contains(f, "[enter] switch") || strings.Contains(f, "[d]elete") {
		t.Errorf("current-worktree row must not offer enter/delete: %q", f)
	}
}

func TestFooterStatusFocusEmptyHasNoContextSegment(t *testing.T) {
	// With status rows, Status focus advertises [enter] diff (tested above);
	// with NO rows there must be no context segment and no stray separator.
	m := footerModel() // fixture has no status files
	m.focus = panelFiles
	f := m.footerLine()
	if strings.Contains(f, "•") {
		t.Errorf("empty Status focus has no context actions, no separator: %q", f)
	}
	if !strings.HasPrefix(f, "[p]ull") {
		t.Errorf("global tail must lead when there is no context segment: %q", f)
	}
}

func TestFooterMarkStates(t *testing.T) {
	m := footerModel()
	m.sel[panelBranches] = 1 // feat/x
	if f := m.footerLine(); !strings.Contains(f, "[m]ark") || strings.Contains(f, "[m] pair") {
		t.Errorf("no mark yet: want [m]ark only, got %q", f)
	}
	u, _ := m.Update(keyMsg("m")) // mark feat/x
	m = u.(Model)
	if f := m.footerLine(); !strings.Contains(f, "[m] unmark") {
		t.Errorf("cursor on the marked row: want [m] unmark, got %q", f)
	}
	m.sel[panelBranches] = 0 // cursor to main; mark still on feat/x
	if f := m.footerLine(); !strings.Contains(f, "[m] pair") {
		t.Errorf("cursor on another row with a live mark: want [m] pair, got %q", f)
	}
}

func TestFooterRunningCollapses(t *testing.T) {
	m := footerModel()
	m.running = true
	// [enter] tip survives: it's pure navigation on the Branches panel (the
	// fixture's default focus) and its predicate deliberately ignores busy —
	// see TestBranchesFooterAdvertisesTipKeys. Everything mutating (pull,
	// push, commit, ...) still drops, which is the invariant this test guards.
	want := "[enter] tip  •  [tab] focus [?] help [q] quit"
	if f := m.footerLine(); f != want {
		t.Errorf("running footer = %q, want %q", f, want)
	}
}

// While a load is in flight the r key is gated (a reload is already running, or
// a repo switch is mid-flight), so the footer drops the now-inert [r] reload
// hint. Before the soft-reload feature this was moot — loading blanked the whole
// screen, so the footer was never rendered during a load; now the footer is
// visible during a soft reload, so an inert hint would mislead.
func TestFooterLoadingDropsReload(t *testing.T) {
	m := footerModel()
	m.loading = true
	// [enter] tip survives: same busy-tolerant navigation exception as
	// TestFooterRunningCollapses above. [r] reload dropping is what this test
	// actually guards.
	want := "[enter] tip  •  [tab] focus [?] help [q] quit"
	if f := m.footerLine(); f != want {
		t.Errorf("loading footer = %q, want %q", f, want)
	}
}

func TestFooterFilterTypingOverride(t *testing.T) {
	m := footerModel()
	m.filterTyping = true
	want := "filter: type to search  [↑↓] move  [enter] keep  [esc] cancel"
	if f := m.footerLine(); f != want {
		t.Errorf("filter-typing footer = %q, want %q", f, want)
	}
}

func TestFooterRenderedInInterface(t *testing.T) {
	m := footerModel()
	m.sel[panelBranches] = 1 // feat/x → full Branches context
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "[s]witch") {
		t.Errorf("rendered interface must show the contextual footer:\n%s", out)
	}
	m.running = true
	out = ansi.Strip(m.render())
	if strings.Contains(out, "[s]witch") || strings.Contains(out, "[p]ull") {
		t.Errorf("running: gated keys must not appear in the rendered footer:\n%s", out)
	}
}

func TestFooterEmptyPanelsHideRowActions(t *testing.T) {
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	f := m.footerLine()
	for _, banned := range []string{"[s]witch", "[b]ranch", "[w]orktree", "[d]elete", "[m]ark", "[P]ush"} {
		if strings.Contains(f, banned) {
			t.Errorf("empty repo: %q must not appear in %q", banned, f)
		}
	}
	if !strings.Contains(f, "[p]ull") {
		t.Errorf("global tail must survive an empty repo: %q", f)
	}
}

func TestFooterTruncatedToWidth(t *testing.T) {
	m := footerModel()
	m.width, m.height = 40, 24
	m.sel[panelBranches] = 1 // full context → longest footer
	out := ansi.Strip(m.render())
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > 40 {
			t.Errorf("line %d exceeds width 40 (%d): %q", i, w, line)
		}
	}
}

func TestFooterShowsDiffOnStatusFocus(t *testing.T) {
	m := diffModel() // focus = panelFiles, selectable rows
	if !strings.Contains(m.footerLine(), "[enter] diff") {
		t.Fatalf("footer must advertise enter on Status: %q", m.footerLine())
	}
}

func TestFooterHidesDiffOffStatusFocus(t *testing.T) {
	m := diffModel()
	m.focus = panelBranches
	if strings.Contains(m.footerLine(), "[enter] diff") {
		t.Fatalf("diff advertised off the Status panel: %q", m.footerLine())
	}
}

func TestFooterHidesDiffOnConflictedRow(t *testing.T) {
	m := diffModel()
	m.sel[panelFiles] = 2 // conflict.txt (KindUnmerged)
	if strings.Contains(m.footerLine(), "[enter] diff") {
		t.Fatalf("diff advertised on a conflicted row: %q", m.footerLine())
	}
}

func TestFooterHidesDiffWhenNarrow(t *testing.T) {
	m := diffModel()
	m.width = 59
	if strings.Contains(m.footerLine(), "[enter] diff") {
		t.Fatalf("diff advertised below 60 cols: %q", m.footerLine())
	}
}

func TestFooterFilesViewOverrideAdvertisesTreeActions(t *testing.T) {
	m := diffModel()
	m.filesView = &contentPopup{lines: []contentLine{{text: "x", path: "x"}}}
	m.filesTreeFocused = true // tree side: file-scoped actions
	f := m.footerLine()
	// All three tree-row nav actions must be advertised, including blame (b).
	for _, want := range []string{"[enter] diff", "[h] hist", "[b] blame"} {
		if !strings.Contains(f, want) {
			t.Fatalf("files-view tree-side footer must advertise %q: %q", want, f)
		}
	}
}

func TestFooterFilesViewCommitSideAdvertisesMenuAndGraph(t *testing.T) {
	m := diffModel()
	m.filesView = &contentPopup{lines: []contentLine{{text: "x", path: "x"}}}
	m.filesTreeFocused = false // commit-list side: parity with the Commits panel
	f := m.footerLine()
	for _, want := range []string{"[.] actions", "[<>=] graph"} {
		if !strings.Contains(f, want) {
			t.Fatalf("files-view commit-side footer must advertise %q: %q", want, f)
		}
	}
}

func TestSwitchAndWorktreeKeysWorkFromAnyPanel(t *testing.T) {
	// s: only footer visibility is Branches-scoped; the key acts on the
	// Branches selection from any focused panel. footerModel()'s fixture has
	// feat/x checked out in another worktree, so s on it opens the
	// jump-to-worktree modal — which still proves the key acted on the
	// Branches selection while focus was elsewhere.
	repo := newRepo(t)
	m := footerModel()
	m.svc = domain.New(repo)
	m.sel[panelBranches] = 1 // feat/x selected in Branches
	m.focus = panelCommits   // but focus is elsewhere
	u, _ := m.Update(keyMsg("s"))
	if mm := u.(Model); mm.modal == nil || mm.modal.req.ID != "switch-to-worktree" {
		t.Error("s must act on the Branches selection from any panel (jump-to-worktree modal)")
	}

	// w: openWorktreePopup also acts on the Branches selection from any panel.
	m = footerModel()
	m.sel[panelBranches] = 1
	m.focus = panelCommits
	u, _ = m.Update(keyMsg("w"))
	if layerOf[*worktreePopup](u.(Model)) == nil {
		t.Error("w must open the worktree popup from any panel")
	}
}

func TestFooterStageVsUnstage(t *testing.T) {
	base := func(focus panel) Model {
		m := New(nil)
		m.loading = false // opsIdle requires not loading
		m.width, m.height = 80, 30
		m.status.Files = []model.FileStatus{
			{Path: "a.go", Kind: model.KindTracked, Staged: 'M', Unstaged: 'M'},
		}
		m.focus = focus
		return m
	}
	if got := base(panelFiles).footerLine(); !strings.Contains(got, "[space] stage") || strings.Contains(got, "unstage") {
		t.Errorf("Files footer = %q, want [space] stage", got)
	}
	if got := base(panelStaged).footerLine(); !strings.Contains(got, "[space] unstage") {
		t.Errorf("Staged footer = %q, want [space] unstage", got)
	}
}

// bindingByLabel finds a context binding by its (unique) rendered label.
func bindingByLabel(t *testing.T, label string) footerBinding {
	t.Helper()
	for _, b := range contextBindings {
		if b.label == label {
			return b
		}
	}
	t.Fatalf("binding %q not found in contextBindings", label)
	return footerBinding{}
}

// TestBranchesFooterAdvertisesTipKeys: the Branches footer shows the enter and
// ctrl+g tip-jump keys when a branch is selected. Busy gating is asserted on
// the predicates directly — while an op runs the footer swaps to the heartbeat
// line wholesale, so the rendered line can't distinguish per-binding gating.
func TestBranchesFooterAdvertisesTipKeys(t *testing.T) {
	m := footerModel()
	m.loading = false
	m.focus = panelBranches
	line := m.footerLine()
	if !strings.Contains(line, "[enter] tip") || !strings.Contains(line, "[ctrl+g] solo+tip") {
		t.Fatalf("Branches footer missing tip keys: %q", line)
	}
	m.running = true // opsIdle() == false
	if bindingByLabel(t, "[ctrl+g] solo+tip").when(m) {
		t.Fatal("ctrl+g predicate must be false while busy (it mutates the feed scope)")
	}
	if !bindingByLabel(t, "[enter] tip").when(m) {
		t.Fatal("enter tip predicate should ignore busy (pure navigation)")
	}
}

// TestFooterPartsRoundTrip pins the refactor: joining the parts must
// reproduce footerLine byte-for-byte, and exactly one part (the first live
// global after a non-empty context group) carries the bullet separator.
func TestFooterPartsRoundTrip(t *testing.T) {
	m := footerModel()
	if got := joinFooterParts(m.footerParts()); got != m.footerLine() {
		t.Errorf("joinFooterParts(footerParts()) = %q\nfooterLine() = %q", got, m.footerLine())
	}
	if !strings.Contains(m.footerLine(), "  •  ") {
		t.Fatalf("fixture footer must contain the group separator: %q", m.footerLine())
	}
	var starts int
	for _, p := range m.footerParts() {
		if p.groupStart {
			starts++
		}
	}
	if starts != 1 {
		t.Errorf("exactly one groupStart expected, got %d", starts)
	}
}

func TestFooterPartsAllowlistRoundTrip(t *testing.T) {
	m := footerModel()
	m.cfg.UI.FooterActions = []string{"repo", "pull"}
	if got := joinFooterParts(m.footerParts()); got != m.footerLine() {
		t.Errorf("allowlist parts = %q\nfooterLine() = %q", got, m.footerLine())
	}
	parts := m.footerParts()
	if len(parts) == 0 || parts[len(parts)-1].binding.id != "actions" {
		t.Errorf("allowlist parts must end with the actions binding: %+v", parts)
	}
}

// TestFooterOverrideModes pins which states bypass the registry footer.
func TestFooterOverrideModes(t *testing.T) {
	m := footerModel()
	if _, ok := m.footerOverride(); ok {
		t.Error("idle panels must use the registry footer")
	}
	m.filterTyping = true
	if s, ok := m.footerOverride(); !ok || !strings.Contains(s, "filter") {
		t.Errorf("filterTyping must override the footer, got %q ok=%v", s, ok)
	}
}

func TestFitFooterWideUnchanged(t *testing.T) {
	m := footerModel()
	line, hidden := fitFooter(m, 500)
	if line != m.footerLine() {
		t.Errorf("wide fit must be untrimmed:\n%q\n%q", line, m.footerLine())
	}
	if hidden != nil {
		t.Errorf("wide fit must hide nothing: %v", hidden)
	}
}

func TestFitFooterExactWidthUnchanged(t *testing.T) {
	m := footerModel()
	full := m.footerLine()
	line, hidden := fitFooter(m, lipgloss.Width(full))
	if line != full || hidden != nil {
		t.Errorf("exact-width fit must be unchanged: %q hidden=%v", line, hidden)
	}
}

// TestFitFooterNarrowDropsFromEndAndAppendsTail is the core contract: whole
// labels drop from the end, the line ends with the protected tail, fits the
// width, and hidden is exactly the contiguous dropped tail in footer order.
func TestFitFooterNarrowDropsFromEndAndAppendsTail(t *testing.T) {
	m := footerModel()
	full := m.footerLine()
	w := lipgloss.Width(full) - 1 // one column short: at least one label drops
	line, hidden := fitFooter(m, w)
	if lipgloss.Width(line) > w {
		t.Errorf("fitted line overflows: %d > %d (%q)", lipgloss.Width(line), w, line)
	}
	if !strings.HasSuffix(line, footerOverflowTail) {
		t.Errorf("trimmed footer must end with %q: %q", footerOverflowTail, line)
	}
	if len(hidden) == 0 {
		t.Fatal("at least one binding must be reported hidden")
	}
	var nonHelp []footerPart
	for _, p := range m.footerParts() {
		if p.binding.id != "help" {
			nonHelp = append(nonHelp, p)
		}
	}
	cut := len(nonHelp) - len(hidden)
	if cut <= 0 {
		t.Fatalf("expected a visible prefix, all %d parts hidden", len(nonHelp))
	}
	for i, b := range hidden {
		if nonHelp[cut+i].label != b.label {
			t.Fatalf("hidden[%d] = %q, want contiguous tail part %q", i, b.label, nonHelp[cut+i].label)
		}
	}
	want := joinFooterParts(nonHelp[:cut]) + " " + footerOverflowTail
	if line != want {
		t.Errorf("fitted line = %q, want %q", line, want)
	}
}

func TestFitFooterTinyWidthFallsBackToTruncate(t *testing.T) {
	m := footerModel()
	line, hidden := fitFooter(m, 8) // narrower than the tail itself (10 cols)
	if want := truncate(m.footerLine(), 8); line != want {
		t.Errorf("tiny width must fall back to truncation: %q want %q", line, want)
	}
	if hidden != nil {
		t.Errorf("tiny-width fallback must hide nothing: %v", hidden)
	}
}

func TestFitFooterPassesThroughModeFooters(t *testing.T) {
	m := footerModel()
	m.filterTyping = true
	line, hidden := fitFooter(m, 20)
	if want := truncate(m.footerLine(), 20); line != want {
		t.Errorf("mode footer must be truncated as before: %q want %q", line, want)
	}
	if hidden != nil {
		t.Errorf("mode footers hide nothing: %v", hidden)
	}
}

func TestFitFooterAllowlistOverflow(t *testing.T) {
	m := footerModel()
	m.cfg.UI.FooterActions = []string{"repo", "pull", "stashes", "undo", "bookmarks", "find", "order", "view", "settings"}
	full := m.footerLine()
	w := lipgloss.Width(full) - 1
	line, hidden := fitFooter(m, w)
	if !strings.HasSuffix(line, footerOverflowTail) {
		t.Errorf("allowlist overflow must end with the tail: %q", line)
	}
	if len(hidden) == 0 {
		t.Error("allowlist overflow must report hidden bindings")
	}
	if lipgloss.Width(line) > w {
		t.Errorf("allowlist fitted line overflows: %q", line)
	}
}

// TestRenderFooterShowsTailWhenOverflowing pins the view.go wiring: the
// rendered frame's footer must end with the protected tail, never a
// mid-label hard cut.
func TestRenderFooterShowsTailWhenOverflowing(t *testing.T) {
	m := footerModel()
	m.width = 40
	out := ansi.Strip(m.render())
	if !strings.Contains(out, footerOverflowTail) {
		t.Fatalf("narrow render must show the overflow tail:\n%s", out)
	}
}

// TestFitFooterTailOnlyWidth pins the band where nothing but the tail fits:
// at w == the tail's own width every label is hidden and the tail stands alone.
func TestFitFooterTailOnlyWidth(t *testing.T) {
	m := footerModel()
	line, hidden := fitFooter(m, lipgloss.Width(footerOverflowTail))
	if line != footerOverflowTail {
		t.Errorf("tail-only width must render the bare tail: %q", line)
	}
	if len(hidden) == 0 {
		t.Error("tail-only width must hide every non-help binding")
	}
	var nonHelp int
	for _, p := range m.footerParts() {
		if p.binding.id != "help" {
			nonHelp++
		}
	}
	if len(hidden) != nonHelp {
		t.Errorf("hidden = %d bindings, want all %d non-help parts", len(hidden), nonHelp)
	}
}

// TestFitFooterEmptyParts: an allowlist whose ids are all unavailable (and a
// running op gating [.] actions out) yields no parts — the empty line "fits"
// at any width and nothing is hidden.
func TestFitFooterEmptyParts(t *testing.T) {
	m := footerModel()
	m.running = true
	m.cfg.UI.FooterActions = []string{"notices"} // no notices in the fixture
	line, hidden := fitFooter(m, 5)
	if line != "" || hidden != nil {
		t.Errorf("empty parts must fit trivially: %q hidden=%v", line, hidden)
	}
}

func TestFooterPartsAllowlistDeduplicatesActions(t *testing.T) {
	m := footerModel()
	m.cfg.UI.FooterActions = []string{"actions", "pull"}
	var n int
	for _, p := range m.footerParts() {
		if p.binding.id == "actions" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("actions must appear exactly once, got %d", n)
	}
}
