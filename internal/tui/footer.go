package tui

import "strings"

// footerBinding is one advertised key: a canonical key name (consumed by the
// TestHelpFooterCoverage drift guard), the rendered label, and the
// availability predicate. The governing rule: the footer never shows an
// unavailable key; it may omit available ones for brevity (W, B, shift+tab,
// pgup/pgdn are usable but documented only in the ? help window). A when may
// therefore be stricter than the Update gate — never looser.
type footerBinding struct {
	id    string // stable action id ("" for pure-navigation keys); see the . menu
	key   string
	label string
	when  func(Model) bool
}

// contextBindings are the panel/row-specific actions, rendered first. The
// three m-bindings and two d-bindings have mutually exclusive predicates, so
// at most one of each key renders at a time.
var contextBindings = []footerBinding{
	{"switch", "s", "[s]witch", func(m Model) bool { return m.focus == panelBranches && m.canSwitchBranch() }},
	{"branch", "b", "[b]ranch", func(m Model) bool { return m.focus == panelBranches && m.canOpenBranchPopup() }},
	{"worktree", "w", "[w]orktree", func(m Model) bool { return m.focus == panelBranches && m.canOpenWorktreePopup() }},
	{"delete-branch", "d", "[d]elete", func(m Model) bool { return m.focus == panelBranches && m.canDeleteBranch() }},
	{"mark", "m", "[m]ark", func(m Model) bool {
		return m.focus == panelBranches && m.canMark() && !m.markOnFocusedPanel()
	}},
	{"unmark", "m", "[m] unmark", func(m Model) bool {
		return m.focus == panelBranches && m.canMark() && m.markOnFocusedPanel() && m.cursorOnMark()
	}},
	{"pair", "m", "[m] pair", func(m Model) bool {
		return m.focus == panelBranches && m.canMark() && m.markOnFocusedPanel() && !m.cursorOnMark()
	}},
	{"switch-worktree", "enter", "[enter] switch", func(m Model) bool { return m.focus == panelWorktrees && m.canEnterWorktree() }},
	{"delete-worktree", "d", "[d]elete", func(m Model) bool { return m.focus == panelWorktrees && m.canDeleteWorktree() }},
	{"file-diff", "enter", "[enter] diff", func(m Model) bool { return m.canShowFileDiff() }},
	{"stage", "space", "[space] stage", func(m Model) bool { return m.focus == panelFiles && m.canStage() }},
	{"unstage", "space", "[space] unstage", func(m Model) bool { return m.focus == panelStaged && m.canStage() }},
	{"stash", "s", "[s] stash", func(m Model) bool {
		return m.focus == panelFiles && m.opsIdle() && len(stashCandidates(m.status)) > 0
	}},
	{"mark-file", "m", "[m] mark", func(m Model) bool { return m.isFilesPanel(m.focus) && m.panelLen(m.focus) > 0 }},
	{"commit-files", "l", "[l] files", func(m Model) bool {
		// Stricter than the dispatch: the narrow case is a statusMsg no-op
		// there, so don't advertise it.
		return m.focus == panelCommits && m.canShowCommitFiles() && !(m.width > 0 && m.width < 40)
	}},
}

// globalBindings are the always-relevant tail, still individually predicated
// (while an op runs everything gated on opsIdle drops out and the footer
// collapses to tab/help/quit).
var globalBindings = []footerBinding{
	{"resolve", "x", "[x] resolve", func(m Model) bool { return m.opsIdle() && len(m.status.Conflicts()) > 0 }},
	{"commit", "c", "[c] commit", Model.canCommit},
	{"amend", "C", "[C] amend", Model.canAmend},
	{"pull", "p", "[p]ull", Model.opsIdle},
	{"push", "P", "[P]ush", func(m Model) bool { return m.opsIdle() && m.status.Branch != "" }},
	{"stashes", "S", "[S]tashes", Model.opsIdle},
	{"undo", "u", "[u]ndo", Model.opsIdle},
	{"order", "o", "[o]rder", Model.opsIdle},
	{"view", "z", "[z] view", Model.opsIdle},
	{"filter", "/", "[/]filter", Model.opsIdle},
	{"repo", "R", "[R]epo", Model.opsIdle},
	{"settings", ",", "[,] settings", Model.opsIdle},
	{"actions", ".", "[.] actions", Model.opsIdle},
	{"", "tab", "[tab] focus", func(Model) bool { return true }},
	{"", "ctrl+←/→", "[ctrl+←/→] tab", Model.opsIdle},
	{"reload", "r", "[r] reload", func(m Model) bool { return !m.running }},
	{"help", "?", "[?] help", func(Model) bool { return true }},
	{"quit", "q", "[q] quit", func(Model) bool { return true }},
}

// footerLine builds the context-sensitive footer: panel/row-specific actions,
// a separator, then the predicated global tail. Filter-input mode overrides
// everything because that mode captures every key.
func (m Model) footerLine() string {
	if m.filterTyping {
		return "filter: type to search  [↑↓] move  [enter] keep  [esc] cancel"
	}
	// The files view owns the keyboard while open (action keys are swallowed),
	// so the registry footer would lie; show the view's own keys instead.
	if m.filesView != nil {
		return "files: [←/→ tab] focus  [↑/↓] move  [ctrl+↑/↓] tree  [enter] diff  [/] search  [h] hist  [b] blame  [z] view  [esc/l] close"
	}
	// The stash list owns the keyboard while it is the focused right column
	// (no file tree yet). When focus has moved to a left panel, fall through to
	// that panel's normal footer.
	if m.stashView != nil && m.focus == panelCommits {
		return "stash: [↑/↓] move  [l] files  [z] view  [←] panels  [enter] apply/pop/drop  [esc/S] close"
	}
	// A configured footer_actions allowlist replaces the default two-group
	// layout: show exactly those ids, in list order, among the available ones.
	// [.] actions always stays so the menu remains discoverable.
	if ids := m.cfg.UI.FooterActions; len(ids) > 0 {
		var labels []string
		for _, id := range ids {
			if b, ok := bindingByID(id); ok && b.when(m) {
				labels = append(labels, b.label)
			}
		}
		if b, ok := bindingByID("actions"); ok && b.when(m) {
			labels = append(labels, b.label)
		}
		return strings.Join(labels, " ")
	}
	var ctx, glob []string
	for _, b := range contextBindings {
		if b.when(m) {
			ctx = append(ctx, b.label)
		}
	}
	for _, b := range globalBindings {
		if b.when(m) {
			glob = append(glob, b.label)
		}
	}
	var groups []string
	if len(ctx) > 0 {
		groups = append(groups, strings.Join(ctx, " "))
	}
	if len(glob) > 0 {
		groups = append(groups, strings.Join(glob, " "))
	}
	return strings.Join(groups, "  •  ")
}

// bindingByID finds a registry binding by its action id. Ids are unique
// (TestFooterBindingIDsUniqueAndPresent), so the first match is the only one.
func bindingByID(id string) (footerBinding, bool) {
	for _, b := range contextBindings {
		if b.id == id {
			return b, true
		}
	}
	for _, b := range globalBindings {
		if b.id == id {
			return b, true
		}
	}
	return footerBinding{}, false
}
