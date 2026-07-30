package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/homeend/gigagit/internal/i18n"
)

// bindingScope tells the . action menu how relevant a binding is. The footer
// shows all available bindings regardless of scope; the menu shows only the
// non-global ones (row first, then window), because global actions belong to
// the whole app and already have their own hotkeys in the footer tail.
type bindingScope int

const (
	scopeGlobal bindingScope = iota // whole-app: footer tail only, NOT in the . menu
	scopeWindow                     // acts on the focused panel / a set of rows
	scopeRow                        // acts on the selected row
)

// footerBinding is one advertised key: a canonical key name (consumed by the
// TestHelpFooterCoverage drift guard), the rendered label, the availability
// predicate, and the scope (used by the . action menu to filter and order rows).
// The governing rule: the footer never shows an unavailable key; it may omit
// available ones for brevity (W, B, shift+tab, pgup/pgdn are usable but
// documented only in the ? help window). A when may therefore be stricter than
// the Update gate — never looser.
type footerBinding struct {
	id    string // stable action id ("" for pure-navigation keys); see the . menu
	key   string
	label string
	when  func(Model) bool
	scope bindingScope
}

// contextBindings returns the per-panel footer registry. A function, not a
// package var: labels go through i18n.T, which must re-evaluate when the
// language changes (a var would freeze English at init, before config
// arrives). The three m-bindings and two d-bindings have mutually exclusive
// predicates, so at most one of each key renders at a time.
func contextBindings() []footerBinding {
	return []footerBinding{
		{"switch", "s", i18n.T("[s]witch"), func(m Model) bool { return m.focus == panelBranches && m.canSwitchBranch() }, scopeRow},
		{"branch", "b", i18n.T("[b]ranch"), func(m Model) bool { return m.focus == panelBranches && m.canOpenBranchPopup() }, scopeRow},
		{"worktree", "w", i18n.T("[w]orktree"), func(m Model) bool { return m.focus == panelBranches && m.canOpenWorktreePopup() }, scopeRow},
		{"delete-branch", "d", i18n.T("[d]elete"), func(m Model) bool { return m.focus == panelBranches && m.canDeleteBranch() }, scopeRow},
		{"", "enter", i18n.T("[enter] tip"), func(m Model) bool {
			_, ok := m.selectedBranch()
			return m.focus == panelBranches && ok
		}, scopeRow},
		{"", "ctrl+g", i18n.T("[ctrl+g] solo+tip"), func(m Model) bool {
			_, ok := m.selectedBranch()
			return m.focus == panelBranches && m.opsIdle() && ok
		}, scopeRow},
		{"mark", "m", i18n.T("[m]ark"), func(m Model) bool {
			return m.focus == panelBranches && m.canMark() && !m.markOnFocusedPanel()
		}, scopeRow},
		{"unmark", "m", i18n.T("[m] unmark"), func(m Model) bool {
			return m.focus == panelBranches && m.canMark() && m.markOnFocusedPanel() && m.cursorOnMark()
		}, scopeRow},
		{"pair", "m", i18n.T("[m] pair"), func(m Model) bool {
			return m.focus == panelBranches && m.canMark() && m.markOnFocusedPanel() && !m.cursorOnMark()
		}, scopeRow},
		{"switch-worktree", "enter", i18n.T("[enter] switch"), func(m Model) bool { return m.focus == panelWorktrees && m.canEnterWorktree() }, scopeRow},
		{"delete-worktree", "d", i18n.T("[d]elete"), func(m Model) bool { return m.focus == panelWorktrees && m.canDeleteWorktree() }, scopeRow},
		{"checkout-remote", "c", i18n.T("[c]heckout"), func(m Model) bool { return m.focus == panelRemotes && m.canCheckoutRemote() }, scopeRow},
		{"switch-remote", "s", i18n.T("[s]witch"), func(m Model) bool { return m.focus == panelRemotes && m.canCheckoutRemote() }, scopeRow},
		{"fetch", "f", i18n.T("[f]etch"), func(m Model) bool { return m.canFetchRemotes() }, scopeWindow},
		{"tag-goto", "enter", i18n.T("[enter] go to commit"), func(m Model) bool { return m.focus == panelTags && len(m.tags) > 0 }, scopeRow},
		{"file-diff", "enter", i18n.T("[enter] diff"), func(m Model) bool { return m.canShowFileDiff() }, scopeRow},
		{"stage", "space", i18n.T("[space] stage"), func(m Model) bool { return m.focus == panelFiles && m.canStage() }, scopeRow},
		{"stage-hunks", "H", i18n.T("[H] hunks"), func(m Model) bool { return m.canStageHunks() }, scopeRow},
		{"unstage", "space", i18n.T("[space] unstage"), func(m Model) bool { return m.focus == panelStaged && m.canStage() }, scopeRow},
		{"stash", "s", i18n.T("[s] stash"), func(m Model) bool {
			return m.focus == panelFiles && m.opsIdle() && len(stashCandidates(m.status)) > 0
		}, scopeWindow},
		{"mark-file", "m", i18n.T("[m] mark"), func(m Model) bool { return m.isFilesPanel(m.focus) && m.panelLen(m.focus) > 0 }, scopeRow},
		{"discard", "d", i18n.T("[d]iscard"), func(m Model) bool { return m.focus == panelFiles && m.canDiscard() }, scopeRow},
		{"discard-all", "D", i18n.T("[D] discard all"), func(m Model) bool { return m.focus == panelFiles && m.canDiscardAll() }, scopeWindow},
		{"commit-files", "l", i18n.T("[enter/l] files"), func(m Model) bool {
			// Stricter than the dispatch: the narrow case is a statusMsg no-op
			// there, so don't advertise it. enter drills in (focuses the tree); l
			// opens the same view on the commit-list side.
			return m.focus == panelCommits && m.canShowCommitFiles() && !(m.width > 0 && m.width < 40)
		}, scopeRow},
		{"", "space", i18n.T("[space] mark"), func(m Model) bool {
			// Raw set size ≤ 1 guarantees space will mark (a possible stale key
			// can't force a refusal); ≥ 2 is ambiguous under stale marks, so the
			// footer stays silent there — omitting an available key is allowed,
			// advertising a wrong outcome is not. Raw len keeps this O(1) per
			// frame (validCompareKeys would scan the loaded feed every render).
			if m.focus != panelCommits || !m.opsIdle() || len(m.commitCompareSet) > 1 {
				return false
			}
			key, ok := m.selectedKey(panelCommits)
			return ok && !m.commitCompareSet[key]
		}, scopeRow},
		{"", "space", i18n.T("[space] unmark"), func(m Model) bool {
			if m.focus != panelCommits || !m.opsIdle() {
				return false
			}
			key, ok := m.selectedKey(panelCommits)
			return ok && m.commitCompareSet[key]
		}, scopeRow},
		{"commit-message", "i", i18n.T("[i] message [I] in editor"), func(m Model) bool {
			_, ok := m.commitForMessageView()
			return ok
		}, scopeRow},
		{"", "ctrl+g", i18n.T("[ctrl+g] solo"), func(m Model) bool {
			_, ok := m.commitSoloCommitRow() // gates Commits focus + opsIdle + a real commit
			return ok
		}, scopeRow},
		{"commit-filter", "\\", i18n.T(`[\] filter`), func(m Model) bool {
			return m.focus == panelCommits && !(m.width > 0 && m.width < 40)
		}, scopeWindow},
		{"graph-window", "", i18n.T("[<>] graph [⇧←→] pan [=] center"), func(m Model) bool {
			return m.focus == panelCommits && m.graphActive()
		}, scopeWindow},
		{"maximize", "t", i18n.T("[t] max"), func(m Model) bool {
			// Stricter than the dispatch gate: don't advertise maximizing an empty box.
			return m.opsIdle() && m.canMaximizeLeft() && m.panelLen(m.focus) > 0
		}, scopeWindow},
		{"fullscreen", "ctrl+t", i18n.T("[ctrl+t] full"), func(m Model) bool {
			// Same stricter gate as t: don't advertise fullscreening an empty box.
			// Also gated narrow like the \ filter binding above: below 40 columns
			// the layout is already single-column, so ctrl+t has nothing left to add.
			return m.opsIdle() && m.canFullMaximize() && m.panelLen(m.focus) > 0 && !(m.width > 0 && m.width < 40)
		}, scopeWindow},
	}
}

// globalBindings returns the always-relevant tail, still individually
// predicated (while an op runs everything gated on opsIdle drops out and the
// footer collapses to tab/help/quit). A function for the same reason as
// contextBindings: labels must re-evaluate on a live language switch.
func globalBindings() []footerBinding {
	return []footerBinding{
		{"resolve", "x", i18n.T("[x] resolve"), Model.canEnterConflict, scopeGlobal},
		{"commit", "c", i18n.T("[c] commit"), Model.canCommit, scopeGlobal},
		{"amend", "C", i18n.T("[C] amend"), Model.canAmend, scopeGlobal},
		{"pull", "p", i18n.T("[p]ull"), Model.opsIdle, scopeGlobal},
		{"push", "P", i18n.T("[P]ush"), func(m Model) bool { return m.opsIdle() && m.status.Branch != "" }, scopeGlobal},
		{"stashes", "S", i18n.T("[S]tashes"), Model.opsIdle, scopeGlobal},
		{"undo", "u", i18n.T("[u]ndo"), Model.opsIdle, scopeGlobal},
		{"bookmarks", "g", i18n.T("[g] bookmarks"), Model.opsIdle, scopeGlobal},
		{"shelf", "G", i18n.T("[G] shelf"), Model.opsIdle, scopeGlobal},
		{"notices", "!", i18n.T("[!] notices"), func(m Model) bool { return len(m.notices) > 0 }, scopeGlobal},
		{"last-error", "E", i18n.T("[E] full message"), func(m Model) bool { return m.lastError != "" }, scopeGlobal},
		{"find", "F", i18n.T("[F] find file"), Model.opsIdle, scopeGlobal},
		{"order", "o", i18n.T("[o]rder"), Model.opsIdle, scopeGlobal},
		{"view", "z", i18n.T("[z] view"), Model.opsIdle, scopeGlobal},
		{"load-batch", "ctrl+l", i18n.T("[ctrl+l] more"), Model.opsIdle, scopeGlobal},
		{"eager-find", "ctrl+f", i18n.T("[ctrl+f] find deeper"), Model.opsIdle, scopeGlobal},
		{"filter", "/", i18n.T("[/]filter"), Model.opsIdle, scopeGlobal},
		{"clear-filters", "ctrl+r", i18n.T("[ctrl+r] clear filter"), Model.canClearFilters, scopeGlobal},
		{"repo", "R", i18n.T("[R]epo"), Model.opsIdle, scopeGlobal},
		{"settings", ",", i18n.T("[,] settings"), Model.opsIdle, scopeGlobal},
		{"actions", ".", i18n.T("[.] actions"), Model.opsIdle, scopeGlobal},
		{"commands", "ctrl+p", i18n.T("[ctrl+p] commands"), Model.opsIdle, scopeGlobal},
		{"", "tab", i18n.T("[tab] focus"), func(Model) bool { return true }, scopeGlobal},
		{"", "ctrl+←/→", i18n.T("[ctrl+←/→] tab"), Model.opsIdle, scopeGlobal},
		{"reload", "r", i18n.T("[r] reload"), func(m Model) bool { return !m.running && !m.loading }, scopeGlobal},
		{"help", "?", i18n.T("[?] help"), func(Model) bool { return true }, scopeGlobal},
		{"quit", "q", i18n.T("[q] quit"), func(Model) bool { return true }, scopeGlobal},
	}
}

// footerOverride returns the hand-written footer for the modes that own the
// keyboard, or ok=false when the registry-driven footer applies.
func (m Model) footerOverride() (string, bool) {
	// A process owns the keyboard; the panel footer would advertise keys that do
	// nothing, so show the process's own indicator instead.
	if m.proc != nil {
		return m.proc.indicator(m), true
	}
	if m.filterTyping {
		return i18n.T("filter: type to search  [↑↓] move  [enter] keep  [esc] cancel"), true
	}
	if m.highlightTyping {
		return i18n.T("highlight: type to search  [↑↓] move  [ctrl+↑/↓] prev/next match  [enter] keep  [esc] clear"), true
	}
	// The files view owns the keyboard while open, so the registry footer would
	// lie; show the view's own keys instead. The commit-list side mirrors the
	// Commits panel (. menu + graph keys); the tree side is file-scoped.
	if m.filesView != nil {
		if m.filesPreview != nil && !m.filesTreeFocused {
			return i18n.T("file: [↑/↓] scroll  [z] view  [←/tab] back to tree  [esc] close preview"), true
		}
		// i shows the displayed commit's message — only when canShowFilesViewMessage
		// holds (same gate as the handler, so the footer never advertises a dead i).
		msgHint := ""
		if m.canShowFilesViewMessage() {
			msgHint = i18n.T("  [i] msg")
		}
		if m.filesTreeFocused {
			// [a] mirrors the handler's gate exactly (stash/compare/shelf have no
			// full tree to toggle to) so the footer never advertises a dead key.
			aHint := ""
			if m.stashView == nil && !m.inCompareMode() && m.filesHash != "" {
				aHint = i18n.T("  [a] all files")
			}
			return i18n.T("tree: [↑/↓] move  [enter] diff") + aHint + i18n.T("  [.] view file/copy  [/] search  [h] hist  [b] blame  [z] view") + msgHint + i18n.T("  [esc/l] close"), true
		}
		return i18n.T("commits: [enter/tab] tree  [↑/↓] move  [<>=] graph  [a] all files  [/] search  [.] actions") + msgHint + i18n.T("  [esc/l] close"), true
	}
	// The stash list owns the keyboard while it is the focused right column
	// (no file tree yet). When focus has moved to a left panel, fall through to
	// that panel's normal footer.
	if m.stashView != nil && m.focus == panelCommits {
		return i18n.T("stash: [↑/↓] move  [l] files  [z] view  [←] panels  [enter] apply/pop/drop  [esc/S] close"), true
	}
	return "", false
}

// footerPart is one renderable footer label plus the binding behind it.
// groupStart marks the context→global boundary ("  •  " separator).
type footerPart struct {
	label      string
	binding    footerBinding
	groupStart bool
}

// footerParts returns the registry-driven footer as ordered parts. A
// configured footer_actions allowlist replaces the default two-group layout:
// exactly those ids, in list order, among the available ones; [.] actions
// always stays so the menu remains discoverable.
func (m Model) footerParts() []footerPart {
	if ids := m.cfg.UI.FooterActions; len(ids) > 0 {
		var parts []footerPart
		haveActions := false
		for _, id := range ids {
			if b, ok := bindingByID(id); ok && b.when(m) {
				parts = append(parts, footerPart{label: b.label, binding: b})
				if id == "actions" {
					haveActions = true
				}
			}
		}
		if !haveActions {
			if b, ok := bindingByID("actions"); ok && b.when(m) {
				parts = append(parts, footerPart{label: b.label, binding: b})
			}
		}
		return parts
	}
	var parts []footerPart
	for _, b := range contextBindings() {
		if b.when(m) {
			parts = append(parts, footerPart{label: b.label, binding: b})
		}
	}
	nCtx := len(parts)
	for _, b := range globalBindings() {
		if b.when(m) {
			p := footerPart{label: b.label, binding: b}
			if len(parts) == nCtx && nCtx > 0 {
				p.groupStart = true
			}
			parts = append(parts, p)
		}
	}
	return parts
}

// joinFooterParts renders parts with the standard separators: one space
// within a group, "  •  " at the groupStart boundary.
func joinFooterParts(parts []footerPart) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			if p.groupStart {
				b.WriteString("  •  ")
			} else {
				b.WriteString(" ")
			}
		}
		b.WriteString(p.label)
	}
	return b.String()
}

// footerLine builds the context-sensitive footer: panel/row-specific actions,
// a separator, then the predicated global tail. Mode footers (filter input,
// files view, …) override everything because those modes capture every key.
func (m Model) footerLine() string {
	if s, ok := m.footerOverride(); ok {
		return s
	}
	return joinFooterParts(m.footerParts())
}

// bindingByID finds a registry binding by its action id. Ids are unique
// (TestFooterBindingIDsUniqueAndPresent), so the first match is the only one.
func bindingByID(id string) (footerBinding, bool) {
	for _, b := range contextBindings() {
		if b.id == id {
			return b, true
		}
	}
	for _, b := range globalBindings() {
		if b.id == id {
			return b, true
		}
	}
	return footerBinding{}, false
}

// footerOverflowTail is the protected tail rendered when the footer had to
// drop labels: the ellipsis signals more keys exist, and ? (the help window)
// is where the dropped ones are listed.
const footerOverflowTail = "… [?] help"

// fitFooter renders the footer to at most w display columns without ever
// cutting a label mid-word. When the full line fits it is returned unchanged.
// Otherwise whole labels are dropped from the end (context bindings render
// first, so the stable global keys hide before the panel-specific ones), the
// line ends with footerOverflowTail, and the dropped bindings are returned
// for the ? help window to list. Hand-written mode footers (process, filter
// input, files view, stash list) are width-truncated as before and hide
// nothing; so does the degenerate width where not even the tail fits.
func fitFooter(m Model, w int) (string, []footerBinding) {
	if s, ok := m.footerOverride(); ok {
		return truncate(s, w), nil
	}
	parts := m.footerParts()
	full := joinFooterParts(parts)
	if lipgloss.Width(full) <= w {
		return full, nil
	}
	tailW := lipgloss.Width(footerOverflowTail)
	if tailW > w {
		return truncate(full, w), nil
	}
	cur := ""
	var hidden []footerBinding
	fitting := true
	pendingGroup := false
	for _, p := range parts {
		if p.binding.id == "help" {
			pendingGroup = pendingGroup || p.groupStart
			continue // always visible, inside the tail
		}
		groupStart := p.groupStart || pendingGroup
		pendingGroup = false
		if fitting {
			sep := ""
			if cur != "" {
				sep = " "
				if groupStart {
					sep = "  •  "
				}
			}
			cand := cur + sep + p.label
			if lipgloss.Width(cand)+1+tailW <= w {
				cur = cand
				continue
			}
			// First label that doesn't fit: stop taking — everything from
			// here on is hidden, so labels only ever drop from the end.
			fitting = false
		}
		hidden = append(hidden, p.binding)
	}
	if cur == "" {
		return footerOverflowTail, hidden
	}
	return cur + " " + footerOverflowTail, hidden
}
