package tui

import "strings"

// footerBinding is one advertised key: a canonical key name (consumed by the
// TestHelpFooterCoverage drift guard), the rendered label, and the
// availability predicate. The governing rule: the footer never shows an
// unavailable key; it may omit available ones for brevity (W, B, shift+tab,
// pgup/pgdn are usable but documented only in the ? help window). A when may
// therefore be stricter than the Update gate — never looser.
type footerBinding struct {
	key   string
	label string
	when  func(Model) bool
}

// contextBindings are the panel/row-specific actions, rendered first. The
// three m-bindings and two d-bindings have mutually exclusive predicates, so
// at most one of each key renders at a time.
var contextBindings = []footerBinding{
	{"s", "[s]witch", func(m Model) bool { return m.focus == panelBranches && m.canSwitchBranch() }},
	{"b", "[b]ranch", func(m Model) bool { return m.focus == panelBranches && m.canOpenBranchPopup() }},
	{"w", "[w]orktree", func(m Model) bool { return m.focus == panelBranches && m.canOpenWorktreePopup() }},
	{"d", "[d]elete", func(m Model) bool { return m.focus == panelBranches && m.canDeleteBranch() }},
	{"m", "[m]ark", func(m Model) bool {
		return m.focus == panelBranches && m.canMark() && !m.markOnFocusedPanel()
	}},
	{"m", "[m] unmark", func(m Model) bool {
		return m.focus == panelBranches && m.canMark() && m.markOnFocusedPanel() && m.cursorOnMark()
	}},
	{"m", "[m] pair", func(m Model) bool {
		return m.focus == panelBranches && m.canMark() && m.markOnFocusedPanel() && !m.cursorOnMark()
	}},
	{"enter", "[enter] switch", func(m Model) bool { return m.focus == panelWorktrees && m.canEnterWorktree() }},
	{"d", "[d]elete", func(m Model) bool { return m.focus == panelWorktrees && m.canDeleteWorktree() }},
	{"enter", "[enter] diff", func(m Model) bool { return m.focus == panelFiles && m.canShowFileDiff() }},
	{"space", "[space] stage", Model.canStage},
	{"s", "[s] stash", func(m Model) bool {
		return m.focus == panelFiles && m.opsIdle() && len(stashCandidates(m.status)) > 0
	}},
	{"m", "[m] mark", func(m Model) bool { return m.focus == panelFiles && m.panelLen(panelFiles) > 0 }},
	{"l", "[l] files", func(m Model) bool {
		// Stricter than the dispatch: the narrow case is a statusMsg no-op
		// there, so don't advertise it.
		return m.focus == panelCommits && m.canShowCommitFiles() && !(m.width > 0 && m.width < 40)
	}},
}

// globalBindings are the always-relevant tail, still individually predicated
// (while an op runs everything gated on opsIdle drops out and the footer
// collapses to tab/help/quit).
var globalBindings = []footerBinding{
	{"x", "[x] resolve", func(m Model) bool { return m.opsIdle() && len(m.status.Conflicts()) > 0 }},
	{"c", "[c] commit", Model.canCommit},
	{"C", "[C] amend", Model.canAmend},
	{"p", "[p]ull", Model.opsIdle},
	{"P", "[P]ush", func(m Model) bool { return m.opsIdle() && m.status.Branch != "" }},
	{"S", "[S]tashes", Model.opsIdle},
	{"u", "[u]ndo", Model.opsIdle},
	{"o", "[o]rder", Model.opsIdle},
	{"z", "[z] view", Model.opsIdle},
	{"/", "[/]filter", Model.opsIdle},
	{"R", "[R]epo", Model.opsIdle},
	{",", "[,] settings", Model.opsIdle},
	{"tab", "[tab] focus", func(Model) bool { return true }},
	{"ctrl+←/→", "[ctrl+←/→] tab", Model.opsIdle},
	{"r", "[r] reload", func(m Model) bool { return !m.running }},
	{"?", "[?] help", func(Model) bool { return true }},
	{"q", "[q] quit", func(Model) bool { return true }},
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
