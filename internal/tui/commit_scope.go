package tui

import (
	"context"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// commitsReloadedMsg carries a scope-reload's page-0 state. gen is THIS load's
// generation (gen0); the handler drops it when a newer reload bumped feed.Gen().
type commitsReloadedMsg struct {
	gen   int
	state domain.FeedState
}

// commitFilterFields holds the active non-branch narrowing of the Commits feed.
type commitFilterFields struct {
	Paths  []string
	Author string
	Grep   string
	Since  string
	Until  string
}

func (f commitFilterFields) filtered() bool {
	return len(f.Paths) > 0 || f.Author != "" || f.Grep != "" || f.Since != "" || f.Until != ""
}

// clearFilteringForFocus removes the filtering that belongs to the FOCUSED
// window only: the `/` filter (if it is bound to this panel), and — on the
// Commits panel — the `@` highlight and the `\` commit-scope filter. Filtering
// on other windows is left untouched. It reports whether the commit-scope
// filter was cleared, so the caller can reload the feed (the `/`/`@` states are
// display-only and need no git walk).
func (m Model) clearFilteringForFocus() (Model, bool) {
	if m.filterPanel == m.focus {
		m.filterTyping = false
		if m.filterQuery != "" {
			// Dropping the filter expands the list: keep the cursor on the
			// same row rather than on a raw display position.
			anchor := m.filterAnchor(m.filterPanel)
			m.filterQuery = ""
			m = m.snapFilterSel(m.filterPanel, anchor)
		}
	}
	reload := false
	if m.focus == panelCommits {
		m.highlightTyping = false
		m.highlightQuery = ""
		reload = m.commitFilter.filtered()
		m.commitFilter = commitFilterFields{}
	}
	return m, reload
}

// canClearFilters reports whether the FOCUSED window has any filtering to clear
// (drives the ctrl+r footer hint). Only committed states count — the typing
// modes capture keys themselves, so ctrl+r can't reach them.
func (m Model) canClearFilters() bool {
	if !m.opsIdle() {
		return false
	}
	if m.filterPanel == m.focus && m.filterQuery != "" {
		return true
	}
	return m.focus == panelCommits && (m.highlightQuery != "" || m.commitFilter.filtered())
}

// startFeedReload sets the Commits loading indicator and returns the scope
// reload cmd, so the title shows it is working while the (possibly slow) re-walk
// runs. The indicator clears when commitsReloadedMsg arrives.
func (m Model) startFeedReload() (Model, tea.Cmd) {
	m.commitsLoading = true
	m.feedScopeApplied = m.feedScopeSig()
	return m, m.reloadFeedCmd()
}

// feedUpstreams returns the deduped upstream refs of the local branches in the
// current feed scope (all local branches when the scope is empty), restricted to
// refs that actually exist as remote-tracking branches — git log errors on a
// missing ref, so a configured-but-unfetched upstream must be dropped.
func (m Model) feedUpstreams() []string {
	exists := make(map[string]bool, len(m.remoteBranches))
	for _, rb := range m.remoteBranches {
		exists[rb.Name] = true
	}
	inScope := func(name string) bool {
		return len(m.commitScopeBranches) == 0 || slices.Contains(m.commitScopeBranches, name)
	}
	var out []string
	seen := map[string]bool{}
	for _, b := range m.branches {
		if b.Upstream == "" || !inScope(b.Name) || !exists[b.Upstream] || seen[b.Upstream] {
			continue
		}
		seen[b.Upstream] = true
		out = append(out, b.Upstream)
	}
	return out
}

// feedScope is the LogScope the commit feed should walk: the scoped branches,
// their tracked resolvable upstreams, and the active path/author/message/date
// filter. Fresh slices: the value-receiver Model shares slice backings.
func (m Model) feedScope() domain.LogScope {
	return domain.LogScope{
		Branches:  append([]string(nil), m.commitScopeBranches...),
		Upstreams: m.feedUpstreams(),
		Paths:     append([]string(nil), m.commitFilter.Paths...),
		Author:    m.commitFilter.Author,
		Grep:      m.commitFilter.Grep,
		Since:     m.commitFilter.Since,
		Until:     m.commitFilter.Until,
	}
}

// feedScopeSig is a stable signature of the scope the feed should walk, used to
// detect when the desired scope (branches + tracked upstreams + filter) differs
// from what was last applied, so the feed is reloaded only when it would
// actually change.
func (m Model) feedScopeSig() string {
	s := m.feedScope()
	return strings.Join(s.Branches, ",") + "|" + strings.Join(s.Upstreams, ",") +
		"|" + strings.Join(s.Paths, ",") + "|" + s.Author + "|" + s.Grep +
		"|" + s.Since + "|" + s.Until
}

// reloadFeedCmd applies the model's scope to the feed off the UI thread. It uses
// ApplyScope so toggling a filter/solo back to a previously-walked scope restores
// the cached accumulation instantly; a genuinely new scope walks page 0. (A hard
// data refresh goes through loadCmd → feed.LoadInitial, which clears the cache.)
func (m Model) reloadFeedCmd() tea.Cmd {
	feed := m.feed
	scope := m.feedScope()
	return func() tea.Msg {
		st, _ := feed.ApplyScope(context.Background(), scope)
		return commitsReloadedMsg{gen: st.Gen, state: st}
	}
}

// commitSoloRow offers "Solo this branch" on the Branches panel: scope the
// Commits feed to the selected branch, or un-solo if it is already the sole one.
func (m Model) commitSoloRow() (actionRow, bool) {
	if m.focus != panelBranches || !m.opsIdle() {
		return actionRow{}, false
	}
	b, ok := m.selectedBranch()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commits-solo",
		label: i18n.T("Solo this branch"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			if len(m.commitScopeBranches) == 1 && m.commitScopeBranches[0] == b.Name {
				m.commitScopeBranches = nil // re-solo → un-solo
			} else {
				m.commitScopeBranches = []string{b.Name}
			}
			return m.startFeedReload()
		},
	}, true
}

// commitToggleRow offers "Add to commit view" / "Remove from commit view" on the
// Branches panel: add or remove the selected branch from the multi-branch
// Commits-feed scope. Removing the last branch returns the feed to all branches.
func (m Model) commitToggleRow() (actionRow, bool) {
	if m.focus != panelBranches || !m.opsIdle() {
		return actionRow{}, false
	}
	b, ok := m.selectedBranch()
	if !ok {
		return actionRow{}, false
	}
	in := slices.Contains(m.commitScopeBranches, b.Name)
	label := i18n.T("Add to commit view")
	if in {
		label = i18n.T("Remove from commit view")
	}
	return actionRow{
		id:    "commits-toggle",
		label: label,
		run: func(m Model) (tea.Model, tea.Cmd) {
			if in {
				m.commitScopeBranches = without(m.commitScopeBranches, b.Name)
			} else {
				m.commitScopeBranches = append(append([]string(nil), m.commitScopeBranches...), b.Name)
			}
			return m.startFeedReload()
		},
	}, true
}

// without returns a new slice with the first occurrence of s removed, preserving
// the order of the remaining elements. A fresh allocation is deliberate: the
// value-receiver Model shares its slice backing with the prior copy, so an
// in-place delete would corrupt it.
func without(ss []string, s string) []string {
	out := make([]string, 0, len(ss))
	for _, x := range ss {
		if x == s {
			continue
		}
		out = append(out, x)
	}
	return out
}

// graphWindowRows offers the commit-graph window controls in the . menu when the
// windowed lane graph is active in the Commits panel.
func (m Model) graphWindowRows() []actionRow {
	if m.focus != panelCommits || !m.graphActive() {
		return nil
	}
	return []actionRow{
		{id: "graph-widen", label: i18n.T("Widen graph"), run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitGraphCols = m.clampCols(m.graphCols() + m.graphStep())
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
			return m, nil
		}},
		{id: "graph-narrow", label: i18n.T("Narrow graph"), run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitGraphCols = m.clampCols(m.graphCols() - m.graphStep())
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
			return m, nil
		}},
		{id: "graph-pan-left", label: i18n.T("Pan graph left"), run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll - m.graphPanStep())
			return m, nil
		}},
		{id: "graph-pan-right", label: i18n.T("Pan graph right"), run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll + m.graphPanStep())
			return m, nil
		}},
		{id: "graph-center", label: i18n.T("Center on selected commit"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.snapGraphToSelected(), nil
		}},
	}
}

// commitViewModeRow toggles the Commits feed between the lane graph and a flat
// ●-gutter list. Offered from the Branches or Commits panel.
func (m Model) commitViewModeRow() (actionRow, bool) {
	if m.focus != panelBranches && m.focus != panelCommits {
		return actionRow{}, false
	}
	label := i18n.T("Show as list")
	if m.commitListMode {
		label = i18n.T("Show as graph")
	}
	return actionRow{
		id:    "commits-viewmode",
		label: label,
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitListMode = !m.commitListMode
			return m, nil
		},
	}, true
}

// commitGotoTipRow offers "Go to tip in commits" on the Branches panel: move the
// Commits cursor to the selected branch's tip commit (matched by tip HASH, so it
// works regardless of how %D decorated the row) and focus the Commits panel.
// A tip that isn't loaded falls back to the ctrl+f eager deep-search.
// Gated only on Branches focus + a selected branch (no opsIdle — navigation).
func (m Model) commitGotoTipRow() (actionRow, bool) {
	if m.focus != panelBranches {
		return actionRow{}, false
	}
	b, ok := m.selectedBranch()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commits-goto-tip",
		label: i18n.T("Go to tip in commits"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			nm, cmd := m.gotoCommitByHash(b.Hash)
			return nm, cmd
		},
	}, true
}

// gotoCommitByHash moves the Commits cursor to the loaded row matching hash
// (display-index space; hash compare, not decoration parsing) and focuses the
// Commits panel. A miss falls back to the ctrl+f eager deep-search — it clears
// any /-filter ("go to" semantics), pages history under the search budget, and
// prompts before scanning deeper. Shared by the goto-tip row (enter / .-menu)
// and the ctrl+g pendingGotoTip drain in the commitsReloadedMsg handler.
func (m Model) gotoCommitByHash(hash string) (Model, tea.Cmd) {
	idx := m.displayIndices(panelCommits)
	for di, bi := range idx {
		if c, ok := m.commitAtUnified(bi); ok && commitIsHash(c, hash) {
			m.sel[panelCommits] = di
			m = m.focusCommitsPanel()
			return m, nil
		}
	}
	return m.startEagerSearch(hash)
}

// commitCreateBranchRow offers "Create branch here" on the Commits panel: open
// the create-branch dialog with the selected commit as the start point. The whole
// create stack (branchPopup → startOp → engine.CreateBranch) already exists.
func (m Model) commitCreateBranchRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash // full SHA → unambiguous start-point
	return actionRow{
		id:    "commit-create-branch",
		label: i18n.T("Create branch here"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			m = m.pushLayer(&branchPopup{startPoint: hash})
			return m, nil
		},
	}, true
}

// commitCreateTagRow offers "Create tag here" on the Commits panel: open the
// create-tag dialog targeting the selected commit (name + optional message).
func (m Model) commitCreateTagRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash
	return actionRow{
		id:    "commit-create-tag",
		label: i18n.T("Create tag here"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.pushLayer(&tagPopup{commit: hash}), nil
		},
	}, true
}

// commitCompareWorktreeRow / commitCompareStagedRow open the files view as a
// whole-tree comparison of the selected commit against the working tree / the
// index. No marking needed — the common "what does my working copy look like
// vs this commit" case.
func (m Model) commitCompareWorktreeRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash
	return actionRow{
		id:    "commit-compare-worktree",
		label: i18n.T("Compare against working tree"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openCompareFiles(
				model.Endpoint{Kind: model.EndpointCommit, Hash: hash},
				model.Endpoint{Kind: model.EndpointWorkTree})
		},
	}, true
}

func (m Model) commitCompareStagedRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash
	return actionRow{
		id:    "commit-compare-staged",
		label: i18n.T("Compare against staged"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openCompareFiles(
				model.Endpoint{Kind: model.EndpointCommit, Hash: hash},
				model.Endpoint{Kind: model.EndpointIndex})
		},
	}, true
}

// compareSetDisplayIndices returns the display-row indices in panel p that are
// in the commit compare selection (◉). Empty unless p is the Commits panel and
// the set is non-empty.
func (m Model) compareSetDisplayIndices(p panel) map[int]bool {
	out := map[int]bool{}
	if p != panelCommits || len(m.commitCompareSet) == 0 {
		return out
	}
	l := m.listFor(p)
	idx := m.displayIndices(p) // idx only; avoid materializing styled rows
	for n, i := range idx {
		if m.commitCompareSet[l.Key(i)] {
			out[n] = true
		}
	}
	return out
}

// commitCompareToggleRow adds/removes the selected commit to the ◉ compare
// selection (the multi-commit analog of marking).
func (m Model) commitCompareToggleRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	key, ok := m.selectedKey(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	in := m.commitCompareSet[key]
	label := i18n.T("Add to compare selection (space)")
	if in {
		label = i18n.T("Unmark commit")
	}
	return actionRow{
		id:    "commit-compare-toggle",
		label: label,
		run: func(m Model) (tea.Model, tea.Cmd) {
			if m.commitCompareSet == nil {
				m.commitCompareSet = map[string]bool{}
			}
			if in {
				delete(m.commitCompareSet, key)
			} else {
				m.commitCompareSet[key] = true
			}
			return m, nil
		},
	}, true
}

// commitCompareClearRow unmarks the ◉ compare selection: "Unmark all commits
// (N)" with 2+ in the set; with exactly one mark it shows "Unmark the marked
// commit" ONLY when the cursor is elsewhere — the toggle row's "Unmark
// commit" covers cursor-on-mark, and without this row a lone off-cursor or
// stale mark would be menu-unreachable.
func (m Model) commitCompareClearRow() (actionRow, bool) {
	if m.focus != panelCommits || len(m.commitCompareSet) == 0 {
		return actionRow{}, false
	}
	label := i18n.T("Unmark all commits (%d)", len(m.validCompareKeys()))
	if len(m.commitCompareSet) < 2 {
		key, ok := m.selectedKey(panelCommits)
		if ok && m.commitCompareSet[key] {
			return actionRow{}, false
		}
		label = i18n.T("Unmark the marked commit")
	}
	return actionRow{
		id:    "commit-compare-clear",
		label: label,
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitCompareSet = nil
			return m, nil
		},
	}, true
}

// compareSelectionEndpoints resolves the ◉ selection into a (left, right)
// endpoint pair, ordered older→newer by feed position:
//   - exactly 2  → a tree-to-tree diff of the two commits (GitKraken's exact
//     2-commit semantic; no ancestry needed).
//   - 3 or more  → the combined diff of the RANGE: git diff oldest^ newest.
//     This is a range approximation — exact only on a topological chain — and
//     is refused when the oldest selected commit is a root (no parent).
//
// ok is false (with a note) when fewer than 2 are selected or the root guard
// trips.
// compareKeyValid reports whether a ◉-set key still resolves to a live row —
// an existing commit in the loaded feed, or a present WIP (working tree /
// staged) sentinel. The set is deliberately stale-tolerant (keys persist
// across scope/solo changes and history rewrites), so any code that shows a
// count or gates on the selection size must count only valid keys; otherwise a
// stale entry (e.g. a commit dropped by a rebase) inflates the count.
func (m Model) compareKeyValid(key string) bool {
	switch key {
	case wipKey(wipRow{kind: wipWorktree}), wipKey(wipRow{kind: wipStaged}):
		for _, r := range m.wipRows {
			if wipKey(r) == key {
				return true
			}
		}
		return false
	default:
		for i := range m.commits {
			if m.commits[i].Hash == key {
				return true
			}
		}
		return false
	}
}

// validCompareKeys returns the ◉-set keys whose row still exists (see
// compareKeyValid). Use its length — not len(m.commitCompareSet) — for any
// user-visible count or size gate.
func (m Model) validCompareKeys() []string {
	var out []string
	for k := range m.commitCompareSet {
		if m.compareKeyValid(k) {
			out = append(out, k)
		}
	}
	return out
}

func (m Model) compareSelectionEndpoints() (left, right model.Endpoint, note string, ok bool) {
	// Iterate the SET directly (not displayIndices) so the selection is
	// independent of the active / filter; drop keys whose row no longer exists
	// (a committed/cleared index leaves a stale "staged" key, etc.).
	type sk struct {
		key  string
		rank int
	}
	var sel []sk
	for k := range m.commitCompareSet {
		if m.compareKeyValid(k) {
			sel = append(sel, sk{k, m.compareKeyRank(k)})
		}
	}
	if len(sel) < 2 {
		return left, right, i18n.T("select at least 2 rows to compare"), false
	}
	// older = max rank, newer = min rank (working tree/staged rank negative = newest).
	oldest, newest := sel[0], sel[0]
	hasWip := false
	for _, s := range sel {
		if s.rank < 0 {
			hasWip = true
		}
		if s.rank > oldest.rank {
			oldest = s
		}
		if s.rank < newest.rank {
			newest = s
		}
	}
	if len(sel) == 2 {
		return m.compareKeyEndpoint(oldest.key), m.compareKeyEndpoint(newest.key), "", true
	}
	if hasWip {
		return left, right, i18n.T("range compare (3+) is commits-only; remove the working tree / staged row"), false
	}
	// 3+ commits: squash from oldest^. Refuse if the oldest is a root commit.
	if oi := oldest.rank; oi >= 0 && oi < len(m.commits) && len(m.commits[oi].Parents) == 0 {
		return left, right, i18n.T("can't squash a range from the root commit"), false
	}
	return model.Endpoint{Kind: model.EndpointCommit, Hash: oldest.key + "^"},
		model.Endpoint{Kind: model.EndpointCommit, Hash: newest.key}, "", true
}

// commitCompareSelectionRow offers "Compare selection" when 2+ commits are in
// the ◉ set. The label is honest about the 3+ range semantic.
func (m Model) commitCompareSelectionRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	n := len(m.validCompareKeys())
	if n < 2 {
		return actionRow{}, false
	}
	label := i18n.T("Compare the 2 selected commits")
	if n >= 3 {
		label = i18n.T("Compare range of %d commits (combined diff)", n)
	}
	return actionRow{
		id:    "commit-compare-selection",
		label: label,
		run: func(m Model) (tea.Model, tea.Cmd) {
			left, right, note, ok := m.compareSelectionEndpoints()
			if !ok {
				m.statusMsg = note
				return m, nil
			}
			return m.openCompareFiles(left, right)
		},
	}, true
}

// commitSquashRow offers "Squash N commits" when 2+ commits are in the ◉
// selection and a branch is checked out. The run validates the selection
// (commits-only, on the current branch, adjacent) after loading the range; the
// oldest selected commit (by feed rank) seeds the rebase base onto..HEAD, and
// rebaseplan.BuildSquash enforces membership + adjacency from the true range.
func (m Model) commitSquashRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() || m.status.Branch == "" {
		return actionRow{}, false
	}
	n := len(m.validCompareKeys())
	if n < 2 {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commit-squash",
		label: i18n.T("Squash %d commits", n),
		run: func(m Model) (tea.Model, tea.Cmd) {
			var targets []string
			oldest, oldestRank := "", -1
			for k := range m.commitCompareSet {
				switch k {
				case wipKey(wipRow{kind: wipWorktree}), wipKey(wipRow{kind: wipStaged}):
					m.statusMsg = i18n.T("squash is commits-only; remove the working tree / staged row")
					return m, nil
				}
				targets = append(targets, k)
				if r := m.compareKeyRank(k); r > oldestRank {
					oldest, oldestRank = k, r
				}
			}
			if oldest == "" {
				m.statusMsg = i18n.T("select at least 2 commits to squash")
				return m, nil
			}
			// Root guard: the oldest commit needs a parent to rebase onto.
			if oldestRank >= 0 && oldestRank < len(m.commits) && len(m.commits[oldestRank].Parents) == 0 {
				m.statusMsg = i18n.T("can't squash from the root commit")
				return m, nil
			}
			return m, m.loadSquashRangeCmd(m.status.Branch, oldest+"^", targets)
		},
	}, true
}

// unmarkOffBranchTargets removes from the ◉ compare selection every target hash
// that is not present in rangeCommits (the loaded onto..HEAD range — the commits
// on the current branch), returning the number unmarked. A squash that names
// commits not on the current branch fails the membership check in
// rebaseplan.BuildSquash; clearing those marks lets the user retry from a valid
// (on-branch) selection instead of hunting down the stray rows by hand. The
// compare set is keyed by commit hash, exactly like rangeCommits/targets, so the
// membership test mirrors squashTargets'.
func (m Model) unmarkOffBranchTargets(rangeCommits []model.RangeCommit, targets []string) int {
	if m.commitCompareSet == nil {
		return 0
	}
	inRange := make(map[string]bool, len(rangeCommits))
	for _, c := range rangeCommits {
		inRange[c.Hash] = true
	}
	n := 0
	for _, t := range targets {
		if !inRange[t] && m.commitCompareSet[t] {
			delete(m.commitCompareSet, t)
			n++
		}
	}
	return n
}

// commitDropSelectionRow offers "Drop N selected commits" when 2+ commits are
// in the ◉ selection and a branch is checked out — the multi-commit analog of
// the single-cursor "Drop commit" row. The run validates the selection
// (commits-only, on the current branch) after loading the range; the oldest
// selected commit (by feed rank) seeds the rebase base onto..HEAD, and
// rebaseplan.BuildDrop marks every target Drop (no adjacency requirement).
func (m Model) commitDropSelectionRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() || m.status.Branch == "" {
		return actionRow{}, false
	}
	n := len(m.validCompareKeys())
	if n < 2 {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commit-drop-selection",
		label: i18n.T("Drop %d selected commits", n),
		run: func(m Model) (tea.Model, tea.Cmd) {
			var targets []string
			oldest, oldestRank := "", -1
			for k := range m.commitCompareSet {
				if !m.compareKeyValid(k) {
					continue
				}
				switch k {
				case wipKey(wipRow{kind: wipWorktree}), wipKey(wipRow{kind: wipStaged}):
					m.statusMsg = i18n.T("drop is commits-only; remove the working tree / staged row")
					return m, nil
				}
				targets = append(targets, k)
				if r := m.compareKeyRank(k); r > oldestRank {
					oldest, oldestRank = k, r
				}
			}
			if len(targets) < 2 {
				m.statusMsg = i18n.T("select at least 2 commits to drop")
				return m, nil
			}
			// Root guard: the oldest commit needs a parent to rebase onto.
			if oldestRank >= 0 && oldestRank < len(m.commits) && len(m.commits[oldestRank].Parents) == 0 {
				m.statusMsg = i18n.T("can't drop a range that includes the root commit")
				return m, nil
			}
			return m, m.loadDropRangeCmd(m.status.Branch, oldest+"^", targets)
		},
	}, true
}

// commitCreateWorktreeRow offers "Create worktree here" on the Commits panel:
// open the create-worktree dialog based at the selected commit, with a user-typed
// (non-templated) branch name.
func (m Model) commitCreateWorktreeRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash
	return actionRow{
		id:    "commit-create-worktree",
		label: i18n.T("Create worktree here"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openWorktreeAt(hash, ""), nil
		},
	}, true
}

// commitCherryPickRow offers "Cherry-pick here" on the Commits panel: apply the
// selected commit onto the current branch as a new commit. A conflict drops into
// the existing conflict resolver (continue/abort).
func (m Model) commitCherryPickRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash // full SHA → unambiguous
	return actionRow{
		id:    "commit-cherry-pick",
		label: i18n.T("Cherry-pick here"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.CherryPick{Commit: hash})
		},
	}, true
}

// commitRevertRow offers "Revert this commit" on the Commits panel: create a new
// commit on the current branch that undoes the selected commit. A conflict drops
// into the existing conflict resolver. A merge commit is refused with a clean
// message (reverting a merge needs -m <parent>, out of scope for v1).
func (m Model) commitRevertRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	c := m.commits[bi]
	hash := c.Hash
	isMerge := len(c.Parents) > 1
	return actionRow{
		id:    "commit-revert",
		label: i18n.T("Revert this commit"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			if isMerge {
				m.statusMsg = i18n.T("cannot revert a merge commit (v1)")
				return m, nil
			}
			return m.startOp(engine.Revert{Commit: hash})
		},
	}, true
}

// commitFastForwardRow offers "Fast-forward <branch> to here" on the Commits
// panel: advance the current branch to the selected commit when that commit is a
// descendant of the branch's tip (git merge --ff-only, non-destructive). Gating
// is computed in-memory from the loaded feed's parent DAG (feedDescendant); when
// the walk leaves the loaded window the row is still offered and the op's
// IsAncestor guard decides. Hidden when HEAD is detached.
func (m Model) commitFastForwardRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	branch := m.status.Branch
	// Detached HEAD: porcelain v2 reports "(detached)" (and "" defensively).
	// There is no current branch to fast-forward, so hide the row.
	if branch == "" || branch == "(detached)" {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	selHash := m.commits[bi].Hash

	// Find the current branch's tip in the loaded feed (its decorated commit).
	tipHash := ""
	for _, c := range m.commits {
		if commitHasLocalRef(c, branch) {
			tipHash = c.Hash
			break
		}
	}
	if tipHash != "" {
		if descendant, conclusive := feedDescendant(m.commits, selHash, tipHash); conclusive && !descendant {
			return actionRow{}, false // conclusively not ahead → hide
		}
	}
	// descendant, or inconclusive, or tip not loaded → offer it; the op guards.

	return actionRow{
		id:    "commit-fast-forward",
		label: i18n.T("Fast-forward %s to here", branch),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmOp(engine.FastForward{Commit: selHash}, i18n.T("Fast-forward to this commit?"))
		},
	}, true
}

// commitResetRow offers "Reset to this commit" on the Commits panel: move the
// current branch to the selected commit. The op asks for the mode (soft/mixed/
// hard) via the modal, and confirms when the target is not on the current branch.
func (m Model) commitResetRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash // full SHA → unambiguous
	return actionRow{
		id:    "commit-reset",
		label: i18n.T("Reset to this commit"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmOp(engine.Reset{Commit: hash}, i18n.T("Reset to %s? This moves the current branch ref.", shortHash(hash)))
		},
	}, true
}

// commitIsHash reports whether commit c is the one identified by short — a
// (possibly abbreviated) SHA such as Branch.Hash (%(objectname:short)). c.Hash is
// the full %H, so a prefix match resolves the abbreviation. Empty short never
// matches (so an unknown branch tip is "not loaded", not the first commit).
func commitIsHash(c model.Commit, short string) bool {
	return short != "" && strings.HasPrefix(c.Hash, short)
}

// commitHasLocalRef reports whether commit c is decorated with a local branch ref
// named name (ignoring remote/tag kinds and the Head flag). A branch ref
// decorates only its tip, so this identifies the branch's tip commit.
func commitHasLocalRef(c model.Commit, name string) bool {
	for _, r := range c.Refs {
		if r.Kind == model.RefLocal && r.Name == name {
			return true
		}
	}
	return false
}

// commitClearFilterRow offers "Clear filter" on the Commits panel when a
// path/author/message/date filter is active.
func (m Model) commitClearFilterRow() (actionRow, bool) {
	if !m.opsIdle() || m.focus != panelCommits || !m.commitFilter.filtered() {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commits-clear-filter",
		label: i18n.T("Clear filter"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitFilter = commitFilterFields{}
			return m.startFeedReload()
		},
	}, true
}

// commitShowAllRow offers "Show all branches" — present only when the feed is
// scoped — from either the Branches or the Commits panel menu.
func (m Model) commitShowAllRow() (actionRow, bool) {
	if !m.opsIdle() || len(m.commitScopeBranches) == 0 {
		return actionRow{}, false
	}
	if m.focus != panelBranches && m.focus != panelCommits {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commits-showall",
		label: i18n.T("Show all branches"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitScopeBranches = nil
			return m.startFeedReload()
		},
	}, true
}

// commitBranchRows offers Rename/Delete branch on the Commits panel for every
// local branch whose tip is the selected commit. Rename applies to every local
// tip (including the current branch); Delete is suppressed for any branch
// checked out in a worktree (this worktree's HEAD or another), since
// engine.DeleteBranch refuses those — no point offering a row that can't
// succeed. Reuses the renameBranchPopup and engine.DeleteBranch backends.
func (m Model) commitBranchRows() []actionRow {
	if m.focus != panelCommits || !m.opsIdle() {
		return nil
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return nil
	}
	checkedOut := map[string]bool{}
	for _, w := range m.worktrees {
		if w.Branch != "" {
			checkedOut[w.Branch] = true
		}
	}
	var rows []actionRow
	for _, r := range m.commits[bi].Refs {
		if r.Kind != model.RefLocal {
			continue
		}
		name := r.Name
		rows = append(rows, actionRow{
			id:    "rename-branch",
			label: i18n.T("Rename branch %s", name),
			run: func(m Model) (tea.Model, tea.Cmd) {
				return m.pushLayer(&renameBranchPopup{old: name, name: newTextField(name)}), nil
			},
		})
		if !r.Head && !checkedOut[name] {
			rows = append(rows, actionRow{
				id:    "delete-branch",
				label: i18n.T("Delete branch %s", name),
				run: func(m Model) (tea.Model, tea.Cmd) {
					return m.startOp(engine.DeleteBranch{Name: name})
				},
			})
		}
	}
	return rows
}
